package service

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionSweepDeletesOnlyExpiredTerminalJobs(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	srv, err := New(Config{
		DataDir: t.TempDir(), RetentionCompleted: 24 * time.Hour,
		RetentionFailed: 12 * time.Hour, RetentionInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-48 * time.Hour)
	young := now.Add(-time.Hour)
	addRetentionJob(t, srv, &Job{ID: "old-completed", Status: StatusCompleted, CreatedAt: old, FinishedAt: &old}, true)
	addRetentionJob(t, srv, &Job{ID: "old-failed", Status: StatusFailed, CreatedAt: old, FinishedAt: &old}, false)
	addRetentionJob(t, srv, &Job{ID: "young-completed", Status: StatusCompleted, CreatedAt: young, FinishedAt: &young}, true)
	addRetentionJob(t, srv, &Job{ID: "queued", Status: StatusQueued, CreatedAt: old, FinishedAt: &old}, true)
	addRetentionJob(t, srv, &Job{ID: "running", Status: StatusRunning, CreatedAt: old, FinishedAt: &old}, true)

	report := srv.sweepRetention(now)
	if report.Candidates != 2 || report.Deleted != 2 || len(report.Errors) != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if strings.Join(report.CandidateIDs, ",") != "old-completed,old-failed" || strings.Join(report.DeletedIDs, ",") != "old-completed,old-failed" {
		t.Fatalf("retention order is not deterministic: %+v", report)
	}
	for _, id := range []string{"old-completed", "old-failed"} {
		if srv.job(id) != nil {
			t.Fatalf("job %s remains in memory", id)
		}
		if err := verifyAbsent(
			filepath.Join(srv.cfg.DataDir, "jobs", id+".json"),
			filepath.Join(srv.cfg.DataDir, "artifacts", id),
		); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"young-completed", "queued", "running"} {
		if srv.job(id) == nil {
			t.Fatalf("job %s was incorrectly deleted", id)
		}
	}
}

func TestRetentionDryRunIsNonMutatingAndObservable(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	srv, err := New(Config{
		DataDir: t.TempDir(), RetentionCompleted: time.Hour,
		RetentionInterval: time.Hour, RetentionDryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := now.Add(-2 * time.Hour)
	addRetentionJob(t, srv, &Job{ID: "candidate", Status: StatusCompleted, CreatedAt: finished, FinishedAt: &finished}, true)

	report := srv.sweepRetention(now)
	if !report.DryRun || report.Candidates != 1 || report.Deleted != 0 || len(report.Errors) != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if srv.job("candidate") == nil {
		t.Fatal("dry-run deleted job from memory")
	}
	if _, err := os.Stat(filepath.Join(srv.cfg.DataDir, "jobs", "candidate.json")); err != nil {
		t.Fatalf("dry-run deleted job metadata: %v", err)
	}

	recorder := httptest.NewRecorder()
	srv.metrics(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	for _, want := range []string{
		`vidscribe_retention_candidates_total{status="completed"} 1`,
		`vidscribe_retention_deleted_total{status="completed"} 0`,
		`vidscribe_retention_errors_total 0`,
		`vidscribe_retention_last_run_timestamp_seconds 1788688800`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q:\n%s", want, body)
		}
	}
}

func TestRetentionReportsTerminalJobWithoutFinishedAt(t *testing.T) {
	now := time.Now().UTC()
	srv, err := New(Config{DataDir: t.TempDir(), RetentionFailed: time.Hour, RetentionInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	addRetentionJob(t, srv, &Job{ID: "malformed", Status: StatusFailed, CreatedAt: now.Add(-2 * time.Hour)}, false)
	report := srv.sweepRetention(now)
	if len(report.Errors) != 1 || report.Deleted != 0 || srv.job("malformed") == nil {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRetentionRejectsNegativeDurations(t *testing.T) {
	if _, err := New(Config{DataDir: t.TempDir(), RetentionCompleted: -time.Hour}); err == nil {
		t.Fatal("expected negative retention duration to fail")
	}
}

func TestRetentionRequiresPositiveIntervalWhenEnabled(t *testing.T) {
	if _, err := New(Config{DataDir: t.TempDir(), RetentionCompleted: time.Hour}); err == nil {
		t.Fatal("expected missing retention interval to fail")
	}
}

func TestRetentionPreservesUnmarkedStagingDataForManualRecovery(t *testing.T) {
	srv, err := New(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	recoveryDir := filepath.Join(srv.cfg.DataDir, ".retention-trash", "needs-recovery")
	if err := os.MkdirAll(recoveryDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recoveryDir, "job.json"), []byte("data"), 0o640); err != nil {
		t.Fatal(err)
	}
	report := srv.sweepRetention(time.Now().UTC())
	if len(report.Errors) != 1 || !strings.Contains(report.Errors[0], "manual recovery") {
		t.Fatalf("unexpected report: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(recoveryDir, "job.json")); err != nil {
		t.Fatalf("recovery data was removed: %v", err)
	}
}

func addRetentionJob(t *testing.T, srv *Server, job *Job, withArtifacts bool) {
	t.Helper()
	srv.mu.Lock()
	srv.jobs[job.ID] = job
	if err := srv.persistLocked(job); err != nil {
		srv.mu.Unlock()
		t.Fatal(err)
	}
	srv.mu.Unlock()
	if withArtifacts {
		dir := filepath.Join(srv.cfg.DataDir, "artifacts", job.ID)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "artifact.txt"), []byte("data"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
}
