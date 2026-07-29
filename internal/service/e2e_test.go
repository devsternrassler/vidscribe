//go:build service_e2e

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestServiceE2E_DirectAudio(t *testing.T) {
	mediaURL := os.Getenv("VIDSCRIBE_SERVICE_TEST_AUDIO_URL")
	if mediaURL == "" {
		t.Skip("VIDSCRIBE_SERVICE_TEST_AUDIO_URL is not set")
	}
	profile := os.Getenv("VIDSCRIBE_SERVICE_TEST_PROFILE")
	if profile == "" {
		profile = "fast"
	}
	srv, err := New(Config{DataDir: t.TempDir(), MaxRuntime: 5 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.Start(ctx)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	body, _ := json.Marshal(JobRequest{
		SourceType: "podcast", SourceURL: mediaURL, CanonicalID: "failed-range-audio",
		Title: "Failed range audio regression", Profile: profile,
		Language: "en", MaxDuration: 4 * 60 * 60, MaxFileSize: "512M",
	})
	job := postJob(t, httpServer.URL, string(body), "")
	deadline := time.Now().Add(5 * time.Hour)
	for time.Now().Before(deadline) {
		resp, err := http.Get(httpServer.URL + "/v1/jobs/" + job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		resp.Body.Close()
		t.Logf("status=%s progress=%+v", job.Status, job.Progress)
		if job.Status == StatusFailed {
			t.Fatalf("service job failed: %s", job.Error)
		}
		if job.Status == StatusCompleted {
			break
		}
		time.Sleep(10 * time.Second)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("job did not complete: %+v", job)
	}
	resp, err := http.Get(httpServer.URL + "/v1/jobs/" + job.ID + "/transcript")
	if err != nil {
		t.Fatal(err)
	}
	transcript, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(strings.Fields(string(transcript))) < 1000 {
		t.Fatalf("transcript is unexpectedly small: status=%d bytes=%d", resp.StatusCode, len(transcript))
	}
}
