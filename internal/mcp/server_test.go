package mcp

import (
	"context"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func makeReq(args map[string]any) mcplib.CallToolRequest {
	return mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{Arguments: args},
	}
}

// ── transcribe_video input validation (no external deps) ────────────────────

func TestTranscribeVideo_MissingURL(t *testing.T) {
	res, err := handleTranscribeVideo(context.Background(), makeReq(map[string]any{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for missing url")
	}
	text := toolResultText(t, res)
	if !strings.Contains(text, "url is required") {
		t.Errorf("expected 'url is required' in %q", text)
	}
}

func TestTranscribeVideo_InvalidScheme(t *testing.T) {
	for _, url := range []string{"file:///etc/passwd", "ftp://example.com/v.mp4", "javascript:alert(1)", ""} {
		name := url
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			res, err := handleTranscribeVideo(context.Background(), makeReq(map[string]any{
				"url": url,
			}))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url == "" {
				if !res.IsError {
					t.Fatal("expected IsError for empty url")
				}
			} else {
				if !res.IsError {
					t.Fatal("expected IsError for non-http scheme")
				}
				text := toolResultText(t, res)
				if !strings.Contains(text, "http") {
					t.Errorf("expected URL scheme hint in %q", text)
				}
			}
		})
	}
}

func TestTranscribeVideo_PrivateURL(t *testing.T) {
	res, err := handleTranscribeVideo(context.Background(), makeReq(map[string]any{"url": "http://127.0.0.1/video"}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolResultText(t, res), "private") {
		t.Fatalf("private URL was not rejected: %s", toolResultText(t, res))
	}
}

func TestTranscribeVideo_InvalidEngineFailsLoud(t *testing.T) {
	res, err := handleTranscribeVideo(context.Background(), makeReq(map[string]any{"url": "https://example.com/video", "engine": "magic"}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolResultText(t, res), "unsupported engine") {
		t.Fatalf("invalid engine was not rejected: %s", toolResultText(t, res))
	}
}

func TestTranscribeVideo_UnsupportedBrowser(t *testing.T) {
	res, err := handleTranscribeVideo(context.Background(), makeReq(map[string]any{
		"url":             "https://example.com/video",
		"cookies_browser": "malicious-browser",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for unsupported browser")
	}
	text := toolResultText(t, res)
	if !strings.Contains(text, "unsupported browser") {
		t.Errorf("expected 'unsupported browser' in %q", text)
	}
}

func TestAllowedBrowsers(t *testing.T) {
	for _, b := range []string{"chrome", "firefox", "safari", "edge", "chromium", "brave", "opera", "vivaldi"} {
		if !allowedBrowsers[b] {
			t.Errorf("%q not in allowedBrowsers", b)
		}
	}
}

func TestRoutesYouTubeLocally(t *testing.T) {
	for _, target := range []string{"https://youtube.com/watch?v=x", "https://www.youtube.com/watch?v=x", "https://youtu.be/x", "https://music.youtube.com/watch?v=x"} {
		if !routesLocally(target) {
			t.Errorf("expected local route for %s", target)
		}
	}
	if routesLocally("https://notyoutube.com/video") {
		t.Fatal("lookalike domain must remain remote-eligible")
	}
}

func TestTranscribeVideo_CookiesFileNotFound(t *testing.T) {
	res, err := handleTranscribeVideo(context.Background(), makeReq(map[string]any{
		"url":          "https://example.com/video",
		"cookies_file": "/nonexistent/cookies.txt",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for missing cookies_file")
	}
	text := toolResultText(t, res)
	if !strings.Contains(text, "cookies_file") {
		t.Errorf("expected 'cookies_file' mention in %q", text)
	}
}

func TestTranscribeVideo_DeviceDefaults(t *testing.T) {
	args := map[string]any{}
	if got := stringArg(args, "device", "auto"); got != "auto" {
		t.Errorf("device default = %q, want auto", got)
	}
	if got := stringArg(args, "compute_type", ""); got != "" {
		t.Errorf("compute_type without value = %q, want empty", got)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func toolResultText(t *testing.T, res *mcplib.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("result is nil")
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcplib.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}
