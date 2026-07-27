package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/sternrassler/vidscribe/internal/cuda"
	"github.com/sternrassler/vidscribe/internal/runtimeenv"
	"github.com/sternrassler/vidscribe/internal/subprocess"
)

// TranscribeResult holds the paths of files produced by the transcription step.
type TranscribeResult struct {
	// TempDir is the directory containing the raw transcription output (caller must clean up).
	TempDir string
	// BaseName is the stem of the output files (without extension).
	BaseName string
	// ActualEngine is the engine that produced the output after visible fallback handling.
	ActualEngine string
	// DetectedLanguage is best-effort provenance read from the engine JSON output.
	DetectedLanguage string
	Confidence       *float64
	Fallbacks        []FallbackEvent
}

type FallbackEvent struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// Transcribe runs the configured whisper engine on audioPath and returns
// paths to the produced transcript files.
func Transcribe(ctx context.Context, cfg *Config, audioPath string, logw io.Writer) (*TranscribeResult, error) {
	tmpDir, err := os.MkdirTemp("", "vidscribe-tx-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	baseName := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	if strings.EqualFold(filepath.Ext(audioPath), ".vtt") {
		segs, parseErr := parseVTTFile(audioPath)
		if parseErr != nil {
			os.RemoveAll(tmpDir)
			return nil, parseErr
		}
		if writeErr := writeParakeetOutputs(tmpDir, baseName, segs); writeErr != nil {
			os.RemoveAll(tmpDir)
			return nil, writeErr
		}
		return &TranscribeResult{TempDir: tmpDir, BaseName: baseName, ActualEngine: "captions-" + cfg.CaptionMode, DetectedLanguage: cfg.Language}, nil
	}

	var runErr error
	actualEngine := cfg.Engine
	var fallbacks []FallbackEvent
	switch {
	case cfg.Engine == "openai":
		runErr = runOpenAIWhisper(ctx, cfg, audioPath, tmpDir, logw)
	case cfg.Engine == "parakeet":
		runErr = runParakeet(ctx, cfg, audioPath, tmpDir, logw)
		if runErr != nil {
			if !cfg.AllowFallback {
				os.RemoveAll(tmpDir)
				return nil, fmt.Errorf("parakeet failed and fallback is disabled: %w", runErr)
			}
			fallbacks = append(fallbacks, FallbackEvent{From: "parakeet", To: "faster", Reason: runErr.Error()})
			fmt.Fprintf(logw, "[vidscribe] parakeet failed (%v) — falling back to faster-whisper\n", runErr)
			actualEngine = "faster"
			runErr, fallbacks = runFasterWhisperWithFallback(ctx, cfg, audioPath, tmpDir, baseName, logw, fallbacks)
		}
	case cfg.Engine == "faster":
		runErr, fallbacks = runFasterWhisperWithFallback(ctx, cfg, audioPath, tmpDir, baseName, logw, fallbacks)
		if len(fallbacks) > 0 {
			actualEngine = fallbacks[len(fallbacks)-1].To
		}
	default:
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("unsupported engine %q", cfg.Engine)
	}

	if runErr != nil {
		os.RemoveAll(tmpDir)
		return nil, runErr
	}

	language, confidence := detectProvenance(tmpDir, baseName, cfg.Language)
	return &TranscribeResult{
		TempDir: tmpDir, BaseName: baseName, ActualEngine: actualEngine,
		DetectedLanguage: language, Confidence: confidence, Fallbacks: fallbacks,
	}, nil
}

// runFasterWhisperWithFallback runs whisper-ctranslate2 and falls back to
// openai-whisper when it fails or silently produces no output.
func runFasterWhisperWithFallback(ctx context.Context, cfg *Config, audioPath, tmpDir, baseName string, logw io.Writer, fallbacks []FallbackEvent) (error, []FallbackEvent) {
	runErr := runFasterWhisper(ctx, cfg, audioPath, tmpDir, logw)
	// whisper-ctranslate2 may exit 0 on silent CUDA errors without writing output.
	if runErr == nil {
		expected := filepath.Join(tmpDir, baseName+".txt")
		if _, statErr := os.Stat(expected); statErr != nil {
			runErr = fmt.Errorf("faster-whisper produced no output (possible CUDA/library error)")
		}
	}
	if runErr != nil {
		if !cfg.AllowFallback {
			return fmt.Errorf("faster-whisper failed and fallback is disabled: %w", runErr), fallbacks
		}
		fallbacks = append(fallbacks, FallbackEvent{From: "faster", To: "openai", Reason: runErr.Error()})
		fmt.Fprintf(logw, "[vidscribe] faster-whisper failed (%v) — falling back to openai-whisper\n", runErr)
		runErr = runOpenAIWhisper(ctx, cfg, audioPath, tmpDir, logw)
	}
	return runErr, fallbacks
}

func detectProvenance(dir, baseName, requested string) (string, *float64) {
	data, err := os.ReadFile(filepath.Join(dir, baseName+".json"))
	if err == nil {
		var doc struct {
			Language string `json:"language"`
			Segments []struct {
				AvgLogprob float64 `json:"avg_logprob"`
			} `json:"segments"`
		}
		if json.Unmarshal(data, &doc) == nil {
			var confidence *float64
			if len(doc.Segments) > 0 {
				sum := 0.0
				for _, segment := range doc.Segments {
					sum += math.Exp(segment.AvgLogprob)
				}
				value := math.Min(1, math.Max(0, sum/float64(len(doc.Segments))))
				confidence = &value
			}
			if doc.Language != "" {
				return doc.Language, confidence
			}
		}
	}
	if requested != "" && requested != "auto" {
		return requested, nil
	}
	return "unknown", nil
}

func runFasterWhisper(ctx context.Context, cfg *Config, audioPath, outDir string, logw io.Writer) error {
	// faster-whisper has no standalone CLI; whisper-ctranslate2 wraps the same
	// CTranslate2 engine with an identical interface.
	// nvidia-cublas-cu12 provides libcublas.so.12; a Python wrapper sets LD_LIBRARY_PATH
	// so ctranslate2 can find it without a system-wide CUDA toolkit install.
	args := []string{}
	if cuda.NeedsBundledCublas(cfg.Device) {
		args = append(args, "--with", cuda.UvxCublasFlag,
			"--from", runtimeenv.WhisperCTranslate2, "python3", "-c", cuda.WhisperWrapperScript)
	} else {
		args = append(args, "--from", runtimeenv.WhisperCTranslate2, "whisper-ctranslate2")
	}
	args = append(args,
		audioPath,
		"--model", cfg.Model,
		"--device", cfg.Device,
		"--compute_type", cfg.ComputeType,
		"--output_dir", outDir,
		"--output_format", "all",
	)
	// whisper-ctranslate2 auto-detects language when --language is omitted.
	if cfg.Language != "" && cfg.Language != "auto" {
		args = append(args, "--language", cfg.Language)
	}
	if cfg.WordTimestamps {
		args = append(args, "--word_timestamps", "True")
	}

	return runUvx(ctx, cfg, "faster-whisper", args, logw)
}

func runOpenAIWhisper(ctx context.Context, cfg *Config, audioPath, outDir string, logw io.Writer) error {
	lang := cfg.Language
	if lang == "auto" {
		lang = ""
	}

	args := []string{
		"--from", runtimeenv.OpenAIWhisper, "whisper",
		audioPath,
		"--model", cfg.Model,
		"--output_dir", outDir,
		"--output_format", "all",
	}
	if lang != "" {
		args = append(args, "--language", lang)
	}
	if cfg.WordTimestamps {
		args = append(args, "--word_timestamps", "True")
	}

	return runUvx(ctx, cfg, "openai-whisper", args, logw)
}

func runUvx(ctx context.Context, cfg *Config, label string, args []string, logw io.Writer) error {
	var stderr bytes.Buffer
	cmd := subprocess.CommandContext(ctx, "uvx", args...)
	if cfg.Verbose {
		cmd.Stdout = logw
		fmt.Fprintf(logw, "[vidscribe] %s: %s\n", label, strings.Join(cmd.Args, " "))
	}
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %s", label, meaningfulError(stderr.String(), err))
	}
	return nil
}
