package deps

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sternrassler/vidscribe/internal/cuda"
	"github.com/sternrassler/vidscribe/internal/runtimeenv"
)

// Check verifies that the minimum required tools are in PATH.
func Check(engine string) error {
	if _, err := exec.LookPath("uvx"); err != nil {
		return fmt.Errorf("uvx not found — install uv: https://docs.astral.sh/uv/")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found — %s", ffmpegInstallHint())
	}
	return nil
}

type DepStatus struct {
	Name    string
	OK      bool
	Version string
	Note    string
}

// Report probes all dependencies and returns their status.
// Probes run in parallel to minimize wall-clock time.
func Report(engine string) []DepStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	type indexedResult struct {
		idx    int
		status DepStatus
	}

	whisperProbe := func() DepStatus {
		switch engine {
		case "openai":
			return probeUvxFrom(ctx, "openai-whisper", runtimeenv.OpenAIWhisper, "whisper", "--help")
		case "parakeet":
			return probeUvxFrom(ctx, "onnx-asr", runtimeenv.ONNXASR, "python3", "-c", "import onnx_asr; print('usage')")
		default:
			return probeUvxFrom(ctx, "whisper-ctranslate2", runtimeenv.WhisperCTranslate2, "whisper-ctranslate2", "--help")
		}
	}

	probes := []func() DepStatus{
		func() DepStatus { return probe(ctx, "uvx", "uvx", "--version") },
		func() DepStatus { return probe(ctx, "ffmpeg", "ffmpeg", "-version") },
		func() DepStatus { return probeUvx(ctx, "yt-dlp", runtimeenv.YTDLP, "--version") },
		whisperProbe,
	}

	ch := make(chan indexedResult, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Add(1)
		go func(idx int, fn func() DepStatus) {
			defer wg.Done()
			ch <- indexedResult{idx, fn()}
		}(i, p)
	}
	go func() { wg.Wait(); close(ch) }()

	results := make([]DepStatus, len(probes))
	for r := range ch {
		results[r.idx] = r.status
	}

	// CUDA (only when NVIDIA GPU is present)
	if gpuStatus := probeCUDA(ctx); gpuStatus != nil {
		results = append(results, *gpuStatus)
	}

	return results
}

// probeCUDA checks GPU availability and libcublas. Returns nil if no NVIDIA GPU is found.
func probeCUDA(ctx context.Context) *DepStatus {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil
	}
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return nil
	}
	gpuName := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if !cuda.NeedsBundledCublas("cuda") {
		return &DepStatus{Name: "cuda", OK: true, Version: gpuName, Note: "native CUDA runtime path"}
	}

	// Check if libcublas.so.12 is loadable via nvidia-cublas-cu12
	cmd := exec.CommandContext(ctx, "uvx", "--with", cuda.UvxCublasFlag,
		"--from", runtimeenv.WhisperCTranslate2, "python3", "-c", cuda.CheckScript)
	cublasOut, cublasErr := cmd.Output()
	if cublasErr != nil || !strings.Contains(string(cublasOut), "ok") {
		return &DepStatus{
			Name: "cuda",
			OK:   false,
			Note: fmt.Sprintf("GPU: %s — libcublas.so.12 not loadable (install libcublas12 or nvidia-cublas-cu12)", gpuName),
		}
	}
	return &DepStatus{
		Name:    "cuda",
		OK:      true,
		Version: gpuName,
		Note:    "float16 available",
	}
}

func probe(ctx context.Context, name, cmd string, args ...string) DepStatus {
	out, err := exec.CommandContext(ctx, cmd, args...).Output()
	if err != nil {
		return DepStatus{Name: name, OK: false, Note: err.Error()}
	}
	ver := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return DepStatus{Name: name, OK: true, Version: ver}
}

func probeUvx(ctx context.Context, name, tool string, args ...string) DepStatus {
	cmdArgs := append([]string{tool}, args...)
	out, err := exec.CommandContext(ctx, "uvx", cmdArgs...).Output()
	if err != nil {
		return DepStatus{Name: name, OK: false, Note: "not available via uvx: " + err.Error()}
	}
	ver := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return DepStatus{Name: name, OK: true, Version: ver}
}

func ffmpegInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "install via: brew install ffmpeg"
	case "windows":
		return "install via: winget install ffmpeg  (or download from https://ffmpeg.org/download.html)"
	default:
		return "install via: apt install ffmpeg  (or your distro's package manager)"
	}
}

func probeUvxFrom(ctx context.Context, name, pkg, tool string, args ...string) DepStatus {
	cmdArgs := append([]string{"--from", pkg, tool}, args...)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "uvx", cmdArgs...)
	cmd.Stderr = &stderr
	err := cmd.Run()
	// whisper --help exits with non-zero, so check stderr for meaningful output
	if err != nil && !strings.Contains(stderr.String(), "usage") && !strings.Contains(stderr.String(), "Usage") {
		return DepStatus{Name: name, OK: false, Note: "not available via uvx: " + err.Error()}
	}
	return DepStatus{Name: name, OK: true, Note: "available via uvx"}
}
