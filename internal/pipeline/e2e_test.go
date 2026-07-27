//go:build e2e

package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// showProgress prints elapsed time to stderr every 5s for long-running tests.
// Returns a stop function. Output bypasses Go's test buffering.
func showProgress(label string) func() {
	stop := make(chan struct{})
	go func() {
		start := time.Now()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "  ⏳ %s … %s\n", label, time.Since(start).Round(time.Second))
			case <-stop:
				return
			}
		}
	}()
	return func() { close(stop) }
}

// testVideoURL returns the video URL for E2E tests.
// Override via VIDSCRIBE_TEST_URL for a different/longer video.
// Default: "Me at the zoo" — first YouTube video ever (19s, speech, permanent).
func testVideoURL() string {
	if u := os.Getenv("VIDSCRIBE_TEST_URL"); u != "" {
		return u
	}
	return "https://www.youtube.com/watch?v=jNQXAC9IVRw"
}

// testCookiesBrowser returns the browser for cookie auth in E2E tests.
// Override via VIDSCRIBE_TEST_BROWSER when authenticated extraction is needed.
func testCookiesBrowser() string {
	if b := os.Getenv("VIDSCRIBE_TEST_BROWSER"); b != "" {
		return b
	}
	return ""
}

func skipIfNoDeps(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("uvx"); err != nil {
		t.Skip("uvx not in PATH")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
}

func hasCUDA() bool {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return false
	}
	out, err := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// baseConfig returns a Config with sensible E2E defaults.
// Callers override individual fields as needed.
func baseConfig(outDir string) *Config {
	return &Config{
		URL:            testVideoURL(),
		Model:          "tiny",
		Language:       "auto",
		OutputDir:      outDir,
		CookiesBrowser: testCookiesBrowser(),
		Engine:         "faster",
		Device:         "cpu",
		ComputeType:    "int8",
		Formats:        []string{"txt", "md"},
	}
}

// ── E2E: full pipeline with different engine/device combinations ────────────

func TestE2E_FasterWhisper_CPU(t *testing.T) {
	skipIfNoDeps(t)
	done := showProgress("faster-whisper CPU")
	defer done()
	outDir := t.TempDir()
	cfg := baseConfig(outDir)
	cfg.Formats = []string{"txt", "md", "srt", "vtt", "json"}

	start := time.Now()
	paths, err := Run(context.Background(), cfg, os.Stderr)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("pipeline.Run (faster/cpu): %v", err)
	}
	t.Logf("faster-whisper CPU: %s (%d files)", elapsed.Round(time.Millisecond), len(paths))

	verifyOutputFiles(t, paths, outDir, []string{"txt", "md", "srt", "vtt", "json"})
	verifyTranscriptContent(t, paths)
}

func TestE2E_FasterWhisper_CUDA(t *testing.T) {
	skipIfNoDeps(t)
	if !hasCUDA() {
		t.Skip("no NVIDIA GPU")
	}
	done := showProgress("faster-whisper CUDA")
	defer done()
	outDir := t.TempDir()
	cfg := baseConfig(outDir)
	cfg.Device = "cuda"
	cfg.ComputeType = "float16"

	start := time.Now()
	paths, err := Run(context.Background(), cfg, os.Stderr)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("pipeline.Run (faster/cuda): %v", err)
	}
	t.Logf("faster-whisper CUDA: %s (%d files)", elapsed.Round(time.Millisecond), len(paths))

	verifyOutputFiles(t, paths, outDir, []string{"txt", "md"})
	verifyTranscriptContent(t, paths)
}

func TestE2E_OpenAIWhisper(t *testing.T) {
	skipIfNoDeps(t)
	done := showProgress("openai-whisper CPU")
	defer done()
	outDir := t.TempDir()
	cfg := baseConfig(outDir)
	cfg.Engine = "openai"

	start := time.Now()
	paths, err := Run(context.Background(), cfg, os.Stderr)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("pipeline.Run (openai): %v", err)
	}
	t.Logf("openai-whisper CPU: %s (%d files)", elapsed.Round(time.Millisecond), len(paths))

	verifyOutputFiles(t, paths, outDir, []string{"txt", "md"})
	verifyTranscriptContent(t, paths)
}

func TestE2E_Parakeet(t *testing.T) {
	skipIfNoDeps(t)
	done := showProgress("parakeet CPU")
	defer done()
	cfg := baseConfig(t.TempDir())
	cfg.Engine = "parakeet"
	paths, err := Run(context.Background(), cfg, os.Stderr)
	if err != nil {
		t.Fatalf("pipeline.Run (parakeet): %v", err)
	}
	verifyOutputFiles(t, paths, cfg.OutputDir, []string{"txt", "md"})
	verifyTranscriptContent(t, paths)
}

func TestE2E_CaptionsAuto(t *testing.T) {
	skipIfNoDeps(t)
	done := showProgress("automatic captions")
	defer done()
	cfg := baseConfig(t.TempDir())
	cfg.Language = "en"
	cfg.CaptionMode = "auto"
	paths, err := Run(context.Background(), cfg, os.Stderr)
	if err != nil {
		t.Fatalf("pipeline.Run (captions): %v", err)
	}
	verifyOutputFiles(t, paths, cfg.OutputDir, []string{"txt", "md"})
	verifyTranscriptContent(t, paths)
}

func TestE2E_CaptionFallbackProvenance(t *testing.T) {
	skipIfNoDeps(t)
	cfg := baseConfig(t.TempDir())
	cfg.Language = "yi"
	cfg.CaptionMode = "manual"
	cfg.AllowFallback = true
	result, err := RunDetailed(context.Background(), cfg, os.Stderr)
	if err != nil {
		t.Fatalf("pipeline.Run (caption fallback): %v", err)
	}
	if !result.Execution.Degraded || len(result.Execution.Fallbacks) == 0 {
		t.Fatalf("fallback not exposed: %+v", result.Execution)
	}
	if result.Execution.Fallbacks[0].From != "captions-manual" || result.Execution.ActualEngine != "faster" {
		t.Fatalf("unexpected fallback provenance: %+v", result.Execution)
	}
}

func TestE2E_AllFormats(t *testing.T) {
	skipIfNoDeps(t)
	done := showProgress("all formats")
	defer done()
	outDir := t.TempDir()
	cfg := baseConfig(outDir)
	cfg.Formats = []string{"txt", "md", "srt", "vtt", "json"}

	paths, err := Run(context.Background(), cfg, os.Stderr)
	if err != nil {
		t.Fatalf("pipeline.Run: %v", err)
	}

	verifyOutputFiles(t, paths, outDir, []string{"txt", "md", "srt", "vtt", "json"})

	// Verify SRT has timing markers.
	for _, p := range paths {
		if filepath.Ext(p) == ".srt" {
			data, _ := os.ReadFile(p)
			if !strings.Contains(string(data), "-->") {
				t.Errorf("SRT file %s has no timing markers", p)
			}
		}
	}
}

// ── Performance comparison ──────────────────────────────────────────────────

func TestE2E_PerformanceComparison(t *testing.T) {
	skipIfNoDeps(t)
	done := showProgress("performance comparison")
	defer done()
	url := testVideoURL()

	type result struct {
		label   string
		elapsed time.Duration
		words   int
		err     error
	}

	modes := []struct {
		label       string
		engine      string
		device      string
		computeType string
		skip        func() bool
	}{
		{"faster-whisper CPU (int8)", "faster", "cpu", "int8", nil},
		{"faster-whisper CUDA (float16)", "faster", "cuda", "float16", func() bool { return !hasCUDA() }},
		{"openai-whisper CPU", "openai", "cpu", "", nil},
	}

	var results []result

	for _, m := range modes {
		if m.skip != nil && m.skip() {
			results = append(results, result{label: m.label + " [SKIPPED]"})
			continue
		}

		outDir := t.TempDir()
		cfg := baseConfig(outDir)
		cfg.Engine = m.engine
		cfg.Device = m.device
		cfg.ComputeType = m.computeType
		cfg.Formats = []string{"txt"}

		start := time.Now()
		paths, err := Run(context.Background(), cfg, os.Stderr)
		elapsed := time.Since(start)

		r := result{label: m.label, elapsed: elapsed, err: err}
		if err == nil {
			for _, p := range paths {
				if filepath.Ext(p) == ".txt" {
					data, _ := os.ReadFile(p)
					r.words = len(strings.Fields(string(data)))
				}
			}
		}
		results = append(results, r)
	}

	// Print comparison table.
	t.Logf("\n╔══════════════════════════════════════╦══════════╦═══════╗")
	t.Logf("║ %-36s ║ %-8s ║ %-5s ║", "Mode", "Time", "Words")
	t.Logf("╠══════════════════════════════════════╬══════════╬═══════╣")
	for _, r := range results {
		if r.err != nil {
			t.Logf("║ %-36s ║ %-8s ║ %-5s ║", r.label, "FAILED", "-")
		} else if r.elapsed == 0 {
			t.Logf("║ %-36s ║ %-8s ║ %-5s ║", r.label, "-", "-")
		} else {
			t.Logf("║ %-36s ║ %8s ║ %5d ║", r.label, r.elapsed.Round(time.Millisecond), r.words)
		}
	}
	t.Logf("╚══════════════════════════════════════╩══════════╩═══════╝")
	t.Logf("Test video: %s", url)
}

// ── Go benchmarks ───────────────────────────────────────────────────────────

func BenchmarkTranscribe_FasterCPU(b *testing.B) {
	if _, err := exec.LookPath("uvx"); err != nil {
		b.Skip("uvx not in PATH")
	}
	for b.Loop() {
		outDir := b.TempDir()
		cfg := baseConfig(outDir)
		cfg.Formats = []string{"txt"}
		if _, err := Run(context.Background(), cfg, os.Stderr); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTranscribe_FasterCUDA(b *testing.B) {
	if _, err := exec.LookPath("uvx"); err != nil {
		b.Skip("uvx not in PATH")
	}
	if !hasCUDA() {
		b.Skip("no NVIDIA GPU")
	}
	for b.Loop() {
		outDir := b.TempDir()
		cfg := baseConfig(outDir)
		cfg.Device = "cuda"
		cfg.ComputeType = "float16"
		cfg.Formats = []string{"txt"}
		if _, err := Run(context.Background(), cfg, os.Stderr); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTranscribe_OpenAI(b *testing.B) {
	if _, err := exec.LookPath("uvx"); err != nil {
		b.Skip("uvx not in PATH")
	}
	for b.Loop() {
		outDir := b.TempDir()
		cfg := baseConfig(outDir)
		cfg.Engine = "openai"
		cfg.Formats = []string{"txt"}
		if _, err := Run(context.Background(), cfg, os.Stderr); err != nil {
			b.Fatal(err)
		}
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func verifyOutputFiles(t *testing.T, paths []string, outDir string, wantExts []string) {
	t.Helper()
	gotExts := map[string]bool{}
	for _, p := range paths {
		ext := strings.TrimPrefix(filepath.Ext(p), ".")
		gotExts[ext] = true
		if _, err := os.Stat(p); err != nil {
			t.Errorf("output file missing: %s", p)
		}
		data, _ := os.ReadFile(p)
		if len(data) == 0 {
			t.Errorf("output file empty: %s", p)
		}
	}
	for _, ext := range wantExts {
		if !gotExts[ext] {
			t.Errorf("expected .%s in output, got files: %v", ext, paths)
		}
	}
}

func verifyTranscriptContent(t *testing.T, paths []string) {
	t.Helper()
	for _, p := range paths {
		if filepath.Ext(p) != ".txt" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read transcript: %v", err)
			continue
		}
		text := string(data)
		words := len(strings.Fields(text))
		if words < 3 {
			t.Errorf("transcript too short (%d words): %s", words, p)
		}
		t.Logf("Transcript (%d words): %.200s…", words, strings.TrimSpace(text))
	}
}

// ── MCP E2E via protocol ────────────────────────────────────────────────────

func TestE2E_Download(t *testing.T) {
	skipIfNoDeps(t)
	done := showProgress("download")
	defer done()
	cfg := baseConfig(t.TempDir())

	start := time.Now()
	audioPath, meta, err := Download(context.Background(), cfg, os.Stderr)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(audioPath))

	t.Logf("Download: %s", elapsed.Round(time.Millisecond))
	t.Logf("  Title:    %s", meta.Title)
	t.Logf("  ID:       %s", meta.ID)
	t.Logf("  Channel:  %s", channelName(meta))
	t.Logf("  Duration: %s", formatDuration(meta.Duration))
	t.Logf("  Audio:    %s", audioPath)

	if meta.ID == "" {
		t.Error("metadata ID is empty")
	}
	if meta.Title == "" {
		t.Error("metadata Title is empty")
	}

	fi, err := os.Stat(audioPath)
	if err != nil {
		t.Fatalf("audio file not found: %v", err)
	}
	if fi.Size() < 1000 {
		t.Errorf("audio file too small: %d bytes", fi.Size())
	}

	// Verify .info.json was written alongside.
	infoMatches, _ := filepath.Glob(filepath.Join(filepath.Dir(audioPath), "*.info.json"))
	if len(infoMatches) == 0 {
		t.Error("no .info.json sidecar found alongside audio")
	}
}

func TestE2E_Transcribe(t *testing.T) {
	skipIfNoDeps(t)
	done := showProgress("transcribe")
	defer done()
	cfg := baseConfig(t.TempDir())

	audioPath, _, err := Download(context.Background(), cfg, os.Stderr)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(audioPath))

	// Then transcribe.
	start := time.Now()
	tx, err := Transcribe(context.Background(), cfg, audioPath, os.Stderr)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	defer os.RemoveAll(tx.TempDir)

	t.Logf("Transcribe: %s", elapsed.Round(time.Millisecond))
	t.Logf("  BaseName: %s", tx.BaseName)

	// Verify output files.
	for _, ext := range []string{"txt", "srt", "vtt", "json"} {
		p := filepath.Join(tx.TempDir, tx.BaseName+"."+ext)
		fi, err := os.Stat(p)
		if err != nil {
			t.Errorf("missing %s: %v", ext, err)
			continue
		}
		t.Logf("  %s: %d bytes", ext, fi.Size())
	}

	// Verify transcript has content.
	txtPath := filepath.Join(tx.TempDir, tx.BaseName+".txt")
	data, err := os.ReadFile(txtPath)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	words := len(strings.Fields(string(data)))
	t.Logf("  Words: %d", words)
	if words < 3 {
		t.Errorf("transcript too short: %d words", words)
	}
}

func TestE2E_ModelComparison(t *testing.T) {
	skipIfNoDeps(t)
	done := showProgress("model comparison")
	defer done()
	url := testVideoURL()

	models := []string{"tiny", "base", "small"}

	type result struct {
		model   string
		elapsed time.Duration
		words   int
		err     error
	}

	var results []result
	for _, model := range models {
		outDir := t.TempDir()
		cfg := baseConfig(outDir)
		cfg.Model = model
		cfg.Formats = []string{"txt"}

		start := time.Now()
		paths, err := Run(context.Background(), cfg, os.Stderr)
		elapsed := time.Since(start)

		r := result{model: model, elapsed: elapsed, err: err}
		if err == nil {
			for _, p := range paths {
				if filepath.Ext(p) == ".txt" {
					data, _ := os.ReadFile(p)
					r.words = len(strings.Fields(string(data)))
				}
			}
		}
		results = append(results, r)
	}

	t.Logf("\n╔════════════╦══════════╦═══════╗")
	t.Logf("║ %-10s ║ %-8s ║ %-5s ║", "Model", "Time", "Words")
	t.Logf("╠════════════╬══════════╬═══════╣")
	for _, r := range results {
		if r.err != nil {
			t.Logf("║ %-10s ║ %-8s ║ %-5s ║", r.model, "FAILED", "-")
			t.Errorf("model %s failed: %v", r.model, r.err)
		} else {
			t.Logf("║ %-10s ║ %8s ║ %5d ║", r.model, r.elapsed.Round(time.Millisecond), r.words)
		}
	}
	t.Logf("╚════════════╩══════════╩═══════╝")
	t.Logf("Test video: %s", url)
}

func init() {
	// Suppress unused import for fmt if only used conditionally.
	_ = fmt.Sprintf
}
