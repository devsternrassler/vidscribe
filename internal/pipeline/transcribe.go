package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TranscribeResult holds the paths of files produced by the transcription step.
type TranscribeResult struct {
	// TempDir is the directory containing the raw transcription output (caller must clean up).
	TempDir string
	// BaseName is the stem of the output files (without extension).
	BaseName string
}

// Transcribe runs the configured whisper engine on audioPath and returns
// paths to the produced transcript files.
func Transcribe(ctx context.Context, cfg *Config, audioPath string, logw io.Writer) (*TranscribeResult, error) {
	tmpDir, err := os.MkdirTemp("", "vidscribe-tx-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	baseName := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))

	var runErr error
	if cfg.Engine == "openai" {
		runErr = runOpenAIWhisper(ctx, cfg, audioPath, tmpDir, logw)
	} else {
		runErr = runFasterWhisper(ctx, cfg, audioPath, tmpDir, logw)
		// whisper-ctranslate2 may exit 0 on silent CUDA errors without writing output.
		if runErr == nil {
			expected := filepath.Join(tmpDir, baseName+".txt")
			if _, statErr := os.Stat(expected); statErr != nil {
				runErr = fmt.Errorf("faster-whisper produced no output (possible CUDA/library error)")
			}
		}
		if runErr != nil {
			fmt.Fprintf(logw, "[vidscribe] faster-whisper failed (%v) — falling back to openai-whisper\n", runErr)
			runErr = runOpenAIWhisper(ctx, cfg, audioPath, tmpDir, logw)
		}
	}

	if runErr != nil {
		os.RemoveAll(tmpDir)
		return nil, runErr
	}

	return &TranscribeResult{TempDir: tmpDir, BaseName: baseName}, nil
}

func runFasterWhisper(ctx context.Context, cfg *Config, audioPath, outDir string, logw io.Writer) error {
	lang := cfg.Language
	if lang == "auto" {
		lang = "auto"
	}

	// faster-whisper has no standalone CLI; whisper-ctranslate2 wraps the same
	// CTranslate2 engine with an identical interface.
	args := []string{
		"--from", "whisper-ctranslate2", "whisper-ctranslate2",
		audioPath,
		"--model", cfg.Model,
		"--language", lang,
		"--device", cfg.Device,
		"--compute_type", cfg.ComputeType,
		"--output_dir", outDir,
		"--output_format", "all",
	}

	return runUvx(ctx, cfg, "faster-whisper", args, logw)
}

func runOpenAIWhisper(ctx context.Context, cfg *Config, audioPath, outDir string, logw io.Writer) error {
	lang := cfg.Language
	if lang == "auto" {
		lang = ""
	}

	args := []string{
		"--from", "openai-whisper", "whisper",
		audioPath,
		"--model", cfg.Model,
		"--output_dir", outDir,
		"--output_format", "all",
	}
	if lang != "" {
		args = append(args, "--language", lang)
	}

	return runUvx(ctx, cfg, "openai-whisper", args, logw)
}

func runUvx(ctx context.Context, cfg *Config, label string, args []string, logw io.Writer) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "uvx", args...)
	if cfg.Verbose {
		cmd.Stdout = logw
		fmt.Fprintf(logw, "[vidscribe] %s: %s\n", label, strings.Join(cmd.Args, " "))
	}
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s failed: %s", label, firstLine(msg))
	}
	return nil
}
