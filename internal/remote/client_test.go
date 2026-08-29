package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sternrassler/vidscribe/internal/pipeline"
	"github.com/sternrassler/vidscribe/internal/service"
)

const testToken = "0123456789abcdef0123456789abcdef"

func tokenFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunCompletedJobMaterializesArtifacts(t *testing.T) {
	var request service.JobRequest
	var requestBody []byte
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/jobs":
			requestBody, _ = io.ReadAll(r.Body)
			_ = json.Unmarshal(requestBody, &request)
			job := service.Job{ID: "job-1", Status: service.StatusCompleted, Result: &pipeline.RunResult{
				Metadata:  &pipeline.Metadata{ID: "clip", Title: "Remote clip"},
				Execution: pipeline.ExecutionInfo{Backend: "remote", ActualEngine: "faster"},
			}}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(job)
		case strings.HasSuffix(r.URL.Path, "/artifacts/txt"):
			_, _ = w.Write([]byte("remote transcript"))
		case strings.HasSuffix(r.URL.Path, "/artifacts/manifest"):
			_, _ = w.Write([]byte(`{"backend":"remote"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	server := httptest.NewServer(h)
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, TokenFile: tokenFile(t), PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	result, err := client.Run(context.Background(), &pipeline.Config{
		URL: "https://example.com/clip", OutputDir: out, Formats: []string{"txt", "manifest"},
		CookiesBrowser: "chrome", CookiesFile: "/private/cookies.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.SourceURL == "" || len(request.Formats) != 2 || result.Execution.Backend != "remote" || len(result.Paths) != 2 {
		t.Fatalf("request=%+v result=%+v", request, result)
	}
	if bytes.Contains(requestBody, []byte("cookie")) || bytes.Contains(requestBody, []byte("/private/cookies.txt")) {
		t.Fatalf("remote request leaked local cookie configuration: %s", requestBody)
	}
	data, err := os.ReadFile(result.Paths[0])
	if err != nil || string(data) != "remote transcript" {
		t.Fatalf("materialized artifact=%q err=%v", data, err)
	}
}

func TestStatusFallbackPolicy(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   bool
	}{{http.StatusBadRequest, false}, {http.StatusUnauthorized, false}, {http.StatusForbidden, false}, {http.StatusInternalServerError, true}} {
		err := classifyStatus(tc.status, "test")
		_, got := FallbackReason(err)
		if got != tc.want {
			t.Errorf("status %d fallback=%t, want %t", tc.status, got, tc.want)
		}
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	file := tokenFile(t)
	if _, err := New(Config{BaseURL: "http://10.20.0.3:8080", TokenFile: file}); err == nil {
		t.Fatal("expected non-loopback URL rejection")
	}
	if _, err := New(Config{BaseURL: "http://127.0.0.1:18083/unexpected", TokenFile: file}); err == nil {
		t.Fatal("expected base path rejection")
	}
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{BaseURL: "http://127.0.0.1:18083", TokenFile: file}); err == nil {
		t.Fatal("expected permissive token mode rejection")
	}
}
