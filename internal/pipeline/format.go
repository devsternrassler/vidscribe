package pipeline

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"
)

const mdTemplate = `# {{.Title}}

**URL:** {{.URL}}
**Kanal:** {{.Channel}}
**Dauer:** {{.Duration}}
**Datum:** {{.Date}}
**Engine:** {{.Engine}} / {{.Model}} ({{.Device}}, {{.ComputeType}})
**Sprache:** {{.Language}}
**Degradiert:** {{.Degraded}}

---

{{.Transcript}}
`

// WriteOutputs remains available for focused formatter tests and callers.
func WriteOutputs(cfg *Config, tx *TranscribeResult, meta *Metadata, logw io.Writer) ([]string, error) {
	execution := &ExecutionInfo{
		RequestedEngine: cfg.Engine, ActualEngine: firstNonEmpty(tx.ActualEngine, cfg.Engine),
		Profile: cfg.Profile, Model: cfg.Model, Device: cfg.Device, ComputeType: cfg.ComputeType,
		DetectedLanguage: tx.DetectedLanguage, Fallbacks: tx.Fallbacks, Degraded: len(tx.Fallbacks) > 0,
		StartedAt: time.Now().UTC(), Dependencies: cfg.DependencyVersion,
		Backend: firstNonEmpty(cfg.Backend, "local"),
	}
	return WriteOutputsDetailed(cfg, tx, meta, execution, logw)
}

// WriteOutputsDetailed writes every requested output atomically. A requested
// format missing from the engine output is an error instead of a silent skip.
func WriteOutputsDetailed(cfg *Config, tx *TranscribeResult, meta *Metadata, execution *ExecutionInfo, _ io.Writer) ([]string, error) {
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}
	outBase := filepath.Join(cfg.OutputDir, meta.SafeTitle())
	written := make([]string, 0, len(cfg.Formats))
	if err := validateEngineOutputs(cfg, tx, execution.ActualEngine); err != nil {
		return nil, err
	}
	if !cfg.Overwrite {
		for _, path := range plannedOutputPaths(cfg, outBase) {
			if _, err := os.Stat(path); err == nil {
				return nil, fmt.Errorf("output already exists: %s (use overwrite explicitly)", path)
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("inspect output %s: %w", path, err)
			}
		}
	}

	for _, ext := range []string{"txt", "srt", "vtt", "json"} {
		if !cfg.HasFormat(ext) {
			continue
		}
		src := filepath.Join(tx.TempDir, tx.BaseName+"."+ext)
		if _, err := os.Stat(src); err != nil {
			return written, fmt.Errorf("engine %s did not produce requested %s output", execution.ActualEngine, ext)
		}
		dst := outBase + "." + ext
		if err := copyFile(src, dst); err != nil {
			return written, fmt.Errorf("copy %s: %w", ext, err)
		}
		written = append(written, dst)
	}

	if cfg.HasFormat("md") {
		transcript, err := os.ReadFile(filepath.Join(tx.TempDir, tx.BaseName+".txt"))
		if err != nil {
			return written, fmt.Errorf("read transcript for md: %w", err)
		}
		dst := outBase + ".md"
		if err := writeMD(dst, meta, execution, string(transcript)); err != nil {
			return written, err
		}
		written = append(written, dst)
	}

	execution.DurationMS = time.Since(execution.StartedAt).Milliseconds()
	if execution.DurationMS > 0 {
		execution.RealtimeFactor = meta.Duration / (float64(execution.DurationMS) / 1000)
	}
	manifest := struct {
		Schema    string         `json:"schema"`
		Video     *Metadata      `json:"video"`
		Execution *ExecutionInfo `json:"execution"`
	}{Schema: "vidscribe-manifest/v1", Video: meta, Execution: execution}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return written, fmt.Errorf("marshal manifest: %w", err)
	}
	manifestPath := outBase + ".manifest.json"
	if err := atomicWrite(manifestPath, append(data, '\n'), 0o644); err != nil {
		return written, fmt.Errorf("write manifest: %w", err)
	}
	written = append(written, manifestPath)
	return written, nil
}

func validateEngineOutputs(cfg *Config, tx *TranscribeResult, engine string) error {
	required := []string{}
	for _, ext := range []string{"txt", "srt", "vtt", "json"} {
		if cfg.HasFormat(ext) {
			required = append(required, ext)
		}
	}
	if cfg.HasFormat("md") && !cfg.HasFormat("txt") {
		required = append(required, "txt")
	}
	for _, ext := range required {
		if _, err := os.Stat(filepath.Join(tx.TempDir, tx.BaseName+"."+ext)); err != nil {
			return fmt.Errorf("engine %s did not produce required %s output", engine, ext)
		}
	}
	return nil
}

func plannedOutputPaths(cfg *Config, outBase string) []string {
	paths := []string{outBase + ".manifest.json"}
	for _, ext := range []string{"txt", "srt", "vtt", "json", "md"} {
		if cfg.HasFormat(ext) {
			paths = append(paths, outBase+"."+ext)
		}
	}
	return paths
}

func writeMD(dst string, meta *Metadata, execution *ExecutionInfo, transcript string) error {
	tmpl, err := template.New("md").Parse(mdTemplate)
	if err != nil {
		return err
	}
	var b strings.Builder
	data := struct {
		Title, URL, Channel, Duration, Date, Transcript string
		Engine, Model, Device, ComputeType, Language    string
		Degraded                                        bool
	}{
		Title: meta.Title, URL: meta.WebpageURL, Channel: channelName(meta),
		Duration: formatDuration(meta.Duration), Date: formatDate(meta.UploadDate),
		Transcript: strings.TrimSpace(transcript), Engine: execution.ActualEngine,
		Model: execution.Model, Device: execution.Device, ComputeType: execution.ComputeType,
		Language: execution.DetectedLanguage, Degraded: execution.Degraded,
	}
	if err := tmpl.Execute(&b, data); err != nil {
		return err
	}
	if err := atomicWrite(dst, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

func channelName(m *Metadata) string {
	if m.Channel != "" {
		return m.Channel
	}
	return m.Uploader
}

func formatDuration(seconds float64) string {
	d := time.Duration(seconds) * time.Second
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatDate(uploadDate string) string {
	if len(uploadDate) == 8 {
		return uploadDate[:4] + "-" + uploadDate[4:6] + "-" + uploadDate[6:]
	}
	return uploadDate
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return atomicWrite(dst, data, 0o644)
}

func atomicWrite(dst string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(dst), ".vidscribe-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(tmp, dst)
}

func FormatReport(paths []string) string {
	var sb strings.Builder
	for _, p := range paths {
		fmt.Fprintf(&sb, "  %s\n", p)
	}
	return sb.String()
}
