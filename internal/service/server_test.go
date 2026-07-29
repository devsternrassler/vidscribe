package service

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
)

func TestJobLifecycleAndIdempotentCache(t *testing.T) {
	dataDir := t.TempDir()
	runs := 0
	runner := func(_ context.Context, cfg *pipeline.Config, _ io.Writer) (*pipeline.RunResult, error) {
		runs++
		if err := os.MkdirAll(cfg.OutputDir, 0o750); err != nil {
			return nil, err
		}
		txt := filepath.Join(cfg.OutputDir, "episode.txt")
		manifest := filepath.Join(cfg.OutputDir, "episode.manifest.json")
		if err := os.WriteFile(txt, []byte("complete transcript"), 0o640); err != nil {
			return nil, err
		}
		if err := os.WriteFile(manifest, []byte(`{"schema":"vidscribe-manifest/v1"}`), 0o640); err != nil {
			return nil, err
		}
		return &pipeline.RunResult{Paths: []string{txt, manifest}}, nil
	}
	srv, err := New(Config{DataDir: dataDir, Runner: runner, DependencyCheck: func(string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.Start(ctx)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	body := `{"source_type":"podcast","source_url":"https://1.1.1.1/episode.mp3","canonical_id":"episode-1","profile":"fast"}`
	job := postJob(t, httpServer.URL, body, "")
	waitForStatus(t, httpServer.URL, job.ID, StatusCompleted, "")

	resp, err := http.Get(httpServer.URL + "/v1/jobs/" + job.ID + "/transcript")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(data) != "complete transcript" {
		t.Fatalf("transcript status=%d body=%q", resp.StatusCode, data)
	}

	second := postJob(t, httpServer.URL, body, "")
	if second.ID != job.ID || second.Status != StatusCompleted {
		t.Fatalf("cache miss: first=%+v second=%+v", job, second)
	}
	if runs != 1 {
		t.Fatalf("runner called %d times, want 1", runs)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "jobs", job.ID+".json")); err != nil {
		t.Fatalf("job was not persisted: %v", err)
	}
}

func TestBearerAuthentication(t *testing.T) {
	srv, err := New(Config{DataDir: t.TempDir(), APIToken: "secret", Runner: func(context.Context, *pipeline.Config, io.Writer) (*pipeline.RunResult, error) {
		return nil, nil
	}, DependencyCheck: func(string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	resp, err := http.Post(httpServer.URL+"/v1/jobs", "application/json", strings.NewReader(`{"source_url":"https://1.1.1.1/video"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
	postJob(t, httpServer.URL, `{"source_url":"https://1.1.1.1/video"}`, "secret")
}

func TestRunningJobIsRecoveredAsQueued(t *testing.T) {
	dataDir := t.TempDir()
	jobsDir := filepath.Join(dataDir, "jobs")
	if err := os.MkdirAll(jobsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "recover-me", Status: StatusRunning, Request: JobRequest{SourceType: "video", SourceURL: "https://1.1.1.1/video"}, CreatedAt: time.Now()}
	data, _ := json.Marshal(job)
	if err := os.WriteFile(filepath.Join(jobsDir, job.ID+".json"), data, 0o640); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{DataDir: dataDir, DependencyCheck: func(string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	recovered := srv.job(job.ID)
	if recovered == nil || recovered.Status != StatusQueued || !strings.Contains(recovered.Error, "recovered") {
		t.Fatalf("unexpected recovered job: %+v", recovered)
	}
}

func postJob(t *testing.T, baseURL, body, token string) Job {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/jobs", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST status=%d body=%s", resp.StatusCode, data)
	}
	var job Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	return job
}

func waitForStatus(t *testing.T, baseURL, id, want, token string) Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/v1/jobs/"+id, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var job Job
		_ = json.NewDecoder(resp.Body).Decode(&job)
		resp.Body.Close()
		if job.Status == want {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s", id, want)
	return Job{}
}
