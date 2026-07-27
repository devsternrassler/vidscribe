//go:build e2e

package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

func testVideoURL() string {
	if u := os.Getenv("VIDSCRIBE_TEST_URL"); u != "" {
		return u
	}
	return "https://www.youtube.com/watch?v=jNQXAC9IVRw"
}

func testCookiesBrowser() string {
	if b := os.Getenv("VIDSCRIBE_TEST_BROWSER"); b != "" {
		return b
	}
	return ""
}

func TestE2E_MCP_TranscribeVideo(t *testing.T) {
	done := showProgress("MCP transcribe CPU")
	defer done()
	s := startMCPServer(t)
	outDir := filepath.Join(s.outputRoot, "cpu")
	s.handshake(t)

	start := time.Now()
	text, isError, progressEvents := s.callToolWithProgress(t, "transcribe_video", map[string]any{
		"url":             testVideoURL(),
		"model":           "tiny",
		"language":        "auto",
		"output_dir":      outDir,
		"engine":          "faster",
		"device":          "cpu",
		"cookies_browser": testCookiesBrowser(),
		"format":          "txt,md",
	}, 120*time.Second)
	elapsed := time.Since(start)

	if isError {
		t.Fatalf("transcribe_video failed:\n%s", text)
	}
	if progressEvents < 4 {
		t.Errorf("received %d progress events, want at least 4", progressEvents)
	}

	t.Logf("MCP transcribe_video: %s", elapsed.Round(time.Millisecond))
	t.Logf("Response:\n%s", text)

	if !strings.Contains(text, "Transcription complete") {
		t.Errorf("expected 'Transcription complete' in response")
	}

	for _, ext := range []string{"txt", "md"} {
		matches, _ := filepath.Glob(filepath.Join(outDir, "*."+ext))
		if len(matches) == 0 {
			t.Errorf("no .%s file found in %s", ext, outDir)
		} else {
			data, _ := os.ReadFile(matches[0])
			t.Logf("  %s: %d bytes", ext, len(data))
		}
	}
}

func TestE2E_MCP_TranscribeVideo_CUDA(t *testing.T) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		t.Skip("no nvidia-smi")
	}
	done := showProgress("MCP transcribe CUDA")
	defer done()
	s := startMCPServer(t)
	outDir := filepath.Join(s.outputRoot, "cuda")
	s.handshake(t)

	start := time.Now()
	text, isError := s.callToolTimeout(t, "transcribe_video", map[string]any{
		"url":             testVideoURL(),
		"model":           "tiny",
		"output_dir":      outDir,
		"device":          "cuda",
		"compute_type":    "float16",
		"cookies_browser": testCookiesBrowser(),
		"format":          "txt",
	}, 120*time.Second)
	elapsed := time.Since(start)

	if isError {
		t.Fatalf("transcribe_video CUDA failed:\n%s", text)
	}

	t.Logf("MCP transcribe_video CUDA: %s", elapsed.Round(time.Millisecond))

	matches, _ := filepath.Glob(filepath.Join(outDir, "*.txt"))
	if len(matches) == 0 {
		t.Error("no .txt file written")
	}
}

func TestE2E_MCP_InvalidURL(t *testing.T) {
	s := startMCPServer(t)
	s.handshake(t)

	text, isError := s.callTool(t, "transcribe_video", map[string]any{
		"url": "file:///etc/passwd",
	})
	if !isError {
		t.Errorf("expected error for file:// URL, got: %s", text)
	}
}
