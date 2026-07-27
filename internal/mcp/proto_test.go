//go:build smoke

package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProto_ToolsList(t *testing.T) {
	s := startMCPServer(t)
	s.handshake(t)

	s.send("tools/list", map[string]any{})
	resp := s.recv(t)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %s", resp.Error.Message)
	}

	var result struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("parse tools/list result: %v", err)
	}

	// Verify exactly the 3 expected tools are registered.
	wantTools := map[string]bool{
		"transcribe_video":     false,
		"check_dependencies":   false,
		"list_supported_sites": false,
	}
	for _, tool := range result.Tools {
		wantTools[tool.Name] = true
	}
	for name, found := range wantTools {
		if !found {
			t.Errorf("tool %q missing from tools/list", name)
		}
	}

	// Verify transcribe_video schema.
	for _, tool := range result.Tools {
		if tool.Name != "transcribe_video" {
			continue
		}
		hasURLRequired := false
		for _, r := range tool.InputSchema.Required {
			if r == "url" {
				hasURLRequired = true
			}
		}
		if !hasURLRequired {
			t.Errorf("transcribe_video: 'url' not in required list %v", tool.InputSchema.Required)
		}
		for _, param := range []string{"profile", "model", "language", "cookies_browser", "cookies_file", "engine", "format", "js_runtime", "device", "compute_type", "captions", "allow_fallback", "max_duration", "max_filesize", "overwrite", "word_timestamps"} {
			if _, ok := tool.InputSchema.Properties[param]; !ok {
				t.Errorf("transcribe_video: optional param %q missing from schema", param)
			}
		}
	}
}

func TestProto_ServerUsesBuildVersion(t *testing.T) {
	s := startMCPServer(t)
	s.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "test", "version": "1"},
	})
	response := s.recv(t)
	var result struct {
		ServerInfo struct {
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.ServerInfo.Version != "dev" {
		t.Fatalf("server version=%q, want dev build version", result.ServerInfo.Version)
	}
}

func TestProto_CheckDependencies(t *testing.T) {
	s := startMCPServer(t)
	s.handshake(t)

	text, isError := s.callTool(t, "check_dependencies", map[string]any{})
	if isError {
		t.Fatalf("check_dependencies returned error: %s", text)
	}
	for _, want := range []string{"uvx", "yt-dlp"} {
		if !strings.Contains(text, want) {
			t.Errorf("check_dependencies: missing %q in:\n%s", want, text)
		}
	}
	t.Logf("check_dependencies response:\n%s", text)
}

func TestProto_ListSupportedSites(t *testing.T) {
	s := startMCPServer(t)
	s.handshake(t)

	text, isError := s.callTool(t, "list_supported_sites", map[string]any{})
	if isError {
		t.Fatalf("list_supported_sites returned error: %s", text)
	}
	if !strings.Contains(strings.ToLower(text), "youtube") {
		t.Errorf("list_supported_sites: 'youtube' not found in output")
	}
}

// ── transcribe_video validation (no network needed) ─────────────────────────

func TestProto_TranscribeVideo_MissingURL(t *testing.T) {
	s := startMCPServer(t)
	s.handshake(t)

	text, isError := s.callTool(t, "transcribe_video", map[string]any{})
	if !isError {
		t.Fatalf("expected error for missing url, got: %s", text)
	}
	if !strings.Contains(text, "url is required") {
		t.Errorf("expected 'url is required' in %q", text)
	}
}

func TestProto_TranscribeVideo_InvalidScheme(t *testing.T) {
	s := startMCPServer(t)
	s.handshake(t)

	text, isError := s.callTool(t, "transcribe_video", map[string]any{
		"url": "file:///etc/passwd",
	})
	if !isError {
		t.Fatalf("expected error for file:// URL, got: %s", text)
	}
	if !strings.Contains(text, "http") {
		t.Errorf("expected URL scheme hint in %q", text)
	}
}

func TestProto_TranscribeVideo_UnsupportedBrowser(t *testing.T) {
	s := startMCPServer(t)
	s.handshake(t)

	text, isError := s.callTool(t, "transcribe_video", map[string]any{
		"url":             "https://example.com/video",
		"cookies_browser": "evil",
	})
	if !isError {
		t.Fatalf("expected error for unsupported browser, got: %s", text)
	}
	if !strings.Contains(text, "unsupported browser") {
		t.Errorf("expected 'unsupported browser' in %q", text)
	}
}
