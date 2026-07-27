package pipeline

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sternrassler/vidscribe/internal/runtimeenv"
	"github.com/sternrassler/vidscribe/internal/subprocess"
)

var captionTag = regexp.MustCompile(`<[^>]+>`)

// parakeetScript runs NVIDIA parakeet-tdt-0.6b-v3 via onnx-asr on CPU.
// Long audio is chunked with the built-in Silero VAD (required: the model
// OOMs on files beyond ~20 minutes without it) which also yields segment
// timestamps. argv: <wav-path> <segments-json-out>.
const parakeetScript = `import json, sys
import onnx_asr
model = onnx_asr.load_model("` + runtimeenv.ParakeetModel + `")
vad = onnx_asr.load_vad("silero")
segs = [{"start": float(s.start), "end": float(s.end), "text": s.text.strip()}
        for s in model.with_vad(vad).recognize(sys.argv[1])]
with open(sys.argv[2], "w") as f:
    json.dump(segs, f, ensure_ascii=False)
`

// segment is one VAD-delimited transcript chunk with timestamps in seconds.
type segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// runParakeet transcribes audioPath with parakeet-tdt-0.6b-v3 and writes
// txt/srt/vtt/json into outDir under the same base name whisper would use,
// so WriteOutputs picks them up unchanged.
func runParakeet(ctx context.Context, cfg *Config, audioPath, outDir string, logw io.Writer) error {
	if cfg.Language != "" && cfg.Language != "auto" {
		fmt.Fprintf(logw, "[vidscribe] parakeet detects language automatically — ignoring --language %s\n", cfg.Language)
	}

	// onnx-asr reads wav only; the pipeline downloads mp3 → convert to 16k mono.
	wavPath := filepath.Join(outDir, "parakeet-input.wav")
	ffmpeg := subprocess.CommandContext(ctx, "ffmpeg", "-v", "error", "-y",
		"-i", audioPath, "-vn", "-ar", "16000", "-ac", "1", wavPath)
	if out, err := ffmpeg.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg wav conversion failed: %s", firstLine(strings.TrimSpace(string(out))))
	}
	defer os.Remove(wavPath)

	segFile := filepath.Join(outDir, "parakeet-segments.json")
	args := []string{
		"--with", runtimeenv.ONNXASR,
		"python3", "-c", parakeetScript,
		wavPath, segFile,
	}
	if err := runUvx(ctx, cfg, "parakeet", args, logw); err != nil {
		return err
	}
	defer os.Remove(segFile)

	segs, err := parseSegmentsFile(segFile)
	if err != nil {
		return err
	}

	baseName := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	return writeParakeetOutputs(outDir, baseName, segs)
}

// parseSegmentsFile reads the wrapper's JSON output. An empty segment list is
// an error: silence-only input is indistinguishable from a broken run, and
// downstream consumers rely on a non-empty transcript (fail loud).
func parseSegmentsFile(path string) ([]segment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read parakeet segments: %w", err)
	}
	var segs []segment
	if err := json.Unmarshal(data, &segs); err != nil {
		return nil, fmt.Errorf("parse parakeet segments: %w", err)
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("parakeet produced no segments")
	}
	return segs, nil
}

func parseVTTFile(path string) ([]segment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read captions: %w", err)
	}
	defer f.Close()
	var segs []segment
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, "-->") {
			continue
		}
		parts := strings.SplitN(line, "-->", 2)
		start, err1 := parseVTTClock(strings.TrimSpace(parts[0]))
		endField := strings.Fields(strings.TrimSpace(parts[1]))
		if len(endField) == 0 {
			continue
		}
		end, err2 := parseVTTClock(endField[0])
		if err1 != nil || err2 != nil {
			continue
		}
		var textLines []string
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				break
			}
			textLines = append(textLines, text)
		}
		text := strings.TrimSpace(html.UnescapeString(captionTag.ReplaceAllString(strings.Join(textLines, " "), "")))
		if text == "" {
			continue
		}
		if len(segs) > 0 && segs[len(segs)-1].Text == text {
			segs[len(segs)-1].End = end
			continue
		}
		segs = append(segs, segment{Start: start, End: end, Text: text})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan captions: %w", err)
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("captions contained no transcript cues")
	}
	return segs, nil
}

func parseVTTClock(value string) (float64, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, fmt.Errorf("invalid VTT time %q", value)
	}
	seconds, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.ParseFloat(parts[len(parts)-2], 64)
	if err != nil {
		return 0, err
	}
	hours := 0.0
	if len(parts) == 3 {
		hours, err = strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, err
		}
	}
	return hours*3600 + minutes*60 + seconds, nil
}

// writeParakeetOutputs materializes txt/srt/vtt/json for the given segments.
func writeParakeetOutputs(outDir, baseName string, segs []segment) error {
	files := map[string]string{
		"txt": segmentsToTXT(segs),
		"srt": segmentsToSRT(segs),
		"vtt": segmentsToVTT(segs),
	}

	fullText := make([]string, len(segs))
	for i, s := range segs {
		fullText[i] = s.Text
	}
	jsonDoc := struct {
		Text     string    `json:"text"`
		Segments []segment `json:"segments"`
	}{Text: strings.Join(fullText, " "), Segments: segs}
	jsonBytes, err := json.Marshal(jsonDoc)
	if err != nil {
		return fmt.Errorf("marshal parakeet json: %w", err)
	}
	files["json"] = string(jsonBytes)

	for ext, content := range files {
		if err := os.WriteFile(filepath.Join(outDir, baseName+"."+ext), []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s output: %w", ext, err)
		}
	}
	return nil
}

func segmentsToTXT(segs []segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func segmentsToSRT(segs []segment) string {
	var b strings.Builder
	for i, s := range segs {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, formatSRTTime(s.Start), formatSRTTime(s.End), s.Text)
	}
	return b.String()
}

func segmentsToVTT(segs []segment) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, s := range segs {
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n", formatVTTTime(s.Start), formatVTTTime(s.End), s.Text)
	}
	return b.String()
}

func formatSRTTime(sec float64) string {
	return formatClockTime(sec, ",")
}

func formatVTTTime(sec float64) string {
	return formatClockTime(sec, ".")
}

func formatClockTime(sec float64, msSep string) string {
	ms := int64(sec*1000 + 0.5)
	h := ms / 3_600_000
	m := ms % 3_600_000 / 60_000
	s := ms % 60_000 / 1000
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", h, m, s, msSep, ms%1000)
}
