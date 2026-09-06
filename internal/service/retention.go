package service

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type RetentionReport struct {
	DryRun       bool      `json:"dry_run"`
	StartedAt    time.Time `json:"started_at"`
	Candidates   int       `json:"candidates"`
	CandidateIDs []string  `json:"candidate_ids,omitempty"`
	Deleted      int       `json:"deleted"`
	DeletedIDs   []string  `json:"deleted_ids,omitempty"`
	Errors       []string  `json:"errors,omitempty"`
}

func (s *Server) retentionLoop(ctxDone <-chan struct{}) {
	s.runRetention(time.Now().UTC())
	ticker := time.NewTicker(s.cfg.RetentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctxDone:
			return
		case now := <-ticker.C:
			s.runRetention(now.UTC())
		}
	}
}

func (s *Server) runRetention(now time.Time) RetentionReport {
	report := s.sweepRetention(now)
	if report.Candidates > 0 || len(report.Errors) > 0 {
		log.Printf("vidscribe retention dry_run=%t candidates=%d candidate_ids=%v deleted=%d deleted_ids=%v errors=%d", report.DryRun, report.Candidates, report.CandidateIDs, report.Deleted, report.DeletedIDs, len(report.Errors))
	}
	return report
}

func (s *Server) sweepRetention(now time.Time) RetentionReport {
	report := RetentionReport{DryRun: s.cfg.RetentionDryRun, StartedAt: now}
	if !report.DryRun {
		for _, err := range s.cleanupRetentionTrash() {
			report.Errors = append(report.Errors, err.Error())
			s.mu.Lock()
			s.retentionErrorsTotal++
			s.mu.Unlock()
		}
	}

	s.mu.RLock()
	ids := make([]string, 0, len(s.jobs))
	for id := range s.jobs {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	sort.Strings(ids)
	for _, id := range ids {
		s.mu.Lock()
		job := s.jobs[id]
		if job == nil {
			s.mu.Unlock()
			continue
		}
		ttl := s.retentionTTL(job.Status)
		if ttl == 0 {
			s.mu.Unlock()
			continue
		}
		if job.FinishedAt == nil {
			report.Errors = append(report.Errors, fmt.Sprintf("job %s has status %s without finished_at", id, job.Status))
			s.retentionErrorsTotal++
			s.mu.Unlock()
			continue
		}
		if now.Before(job.FinishedAt.Add(ttl)) {
			s.mu.Unlock()
			continue
		}

		report.Candidates++
		report.CandidateIDs = append(report.CandidateIDs, id)
		s.retentionCandidatesTotal[job.Status]++
		if report.DryRun {
			s.mu.Unlock()
			continue
		}
		trashDir, err := s.stageJobDeletionLocked(job, now)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("job %s: %v", id, err))
			s.retentionErrorsTotal++
			s.mu.Unlock()
			continue
		}
		delete(s.jobs, id)
		report.Deleted++
		report.DeletedIDs = append(report.DeletedIDs, id)
		s.retentionDeletedTotal[job.Status]++
		s.mu.Unlock()

		if err := os.RemoveAll(trashDir); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("job %s: active data deleted, but removing staged job data failed: %v", id, err))
			s.mu.Lock()
			s.retentionErrorsTotal++
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	s.retentionLastRun = now
	s.mu.Unlock()
	return report
}

func (s *Server) retentionTTL(status string) time.Duration {
	switch status {
	case StatusCompleted:
		return s.cfg.RetentionCompleted
	case StatusFailed:
		return s.cfg.RetentionFailed
	default:
		return 0
	}
}

// stageJobDeletionLocked atomically removes a terminal job from the active
// namespace. The caller must hold s.mu until it also removes the in-memory job.
func (s *Server) stageJobDeletionLocked(job *Job, now time.Time) (string, error) {
	jobPath := filepath.Join(s.cfg.DataDir, "jobs", job.ID+".json")
	artifactPath := filepath.Join(s.cfg.DataDir, "artifacts", job.ID)
	trashDir := filepath.Join(s.cfg.DataDir, ".retention-trash", fmt.Sprintf("%d-%s", now.UnixNano(), job.ID))
	if err := os.MkdirAll(trashDir, 0o750); err != nil {
		return "", fmt.Errorf("create retention staging directory: %w", err)
	}
	stagedJob := filepath.Join(trashDir, "job.json")
	if err := os.Rename(jobPath, stagedJob); err != nil {
		return "", errors.Join(fmt.Errorf("stage job metadata: %w", err), os.RemoveAll(trashDir))
	}

	stagedArtifacts := filepath.Join(trashDir, "artifacts")
	artifactStaged := false
	if _, err := os.Stat(artifactPath); err == nil {
		if err := os.Rename(artifactPath, stagedArtifacts); err != nil {
			return "", retentionRollbackError(fmt.Errorf("stage artifacts: %w", err), rollbackStaged(jobPath, stagedJob, artifactPath, stagedArtifacts, false, trashDir), trashDir)
		}
		artifactStaged = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", retentionRollbackError(fmt.Errorf("inspect artifacts: %w", err), rollbackStaged(jobPath, stagedJob, artifactPath, stagedArtifacts, false, trashDir), trashDir)
	}

	if err := verifyAbsent(jobPath, artifactPath); err != nil {
		return "", retentionRollbackError(err, rollbackStaged(jobPath, stagedJob, artifactPath, stagedArtifacts, artifactStaged, trashDir), trashDir)
	}
	if err := os.WriteFile(filepath.Join(trashDir, ".purge-ready"), nil, 0o640); err != nil {
		return "", retentionRollbackError(fmt.Errorf("mark staged data purge-ready: %w", err), rollbackStaged(jobPath, stagedJob, artifactPath, stagedArtifacts, artifactStaged, trashDir), trashDir)
	}
	return trashDir, nil
}

func (s *Server) cleanupRetentionTrash() []error {
	trashRoot := filepath.Join(s.cfg.DataDir, ".retention-trash")
	entries, err := os.ReadDir(trashRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []error{fmt.Errorf("inspect previous retention staging data: %w", err)}
	}
	var cleanupErrors []error
	for _, entry := range entries {
		staged := filepath.Join(trashRoot, entry.Name())
		marker, markerErr := os.Lstat(filepath.Join(staged, ".purge-ready"))
		if !entry.IsDir() || markerErr != nil || !marker.Mode().IsRegular() {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("retention staging data requires manual recovery: %s", staged))
			continue
		}
		if err := os.RemoveAll(staged); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove previous purge-ready retention data %s: %w", staged, err))
		}
	}
	return cleanupErrors
}

func rollbackStaged(jobPath, stagedJob, artifactPath, stagedArtifacts string, artifactStaged bool, trashDir string) error {
	var rollbackErrors []error
	if artifactStaged {
		if err := os.Rename(stagedArtifacts, artifactPath); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore artifacts: %w", err))
		}
	}
	if err := os.Rename(stagedJob, jobPath); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore job metadata: %w", err))
	}
	if len(rollbackErrors) == 0 {
		if err := os.RemoveAll(trashDir); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove empty staging directory: %w", err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func retentionRollbackError(cause, rollbackErr error, trashDir string) error {
	if rollbackErr == nil {
		return cause
	}
	return fmt.Errorf("%w; rollback failed: %v; staged data retained for manual recovery at %s", cause, rollbackErr, trashDir)
}

func verifyAbsent(paths ...string) error {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("deletion read-back failed: %s still exists", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("deletion read-back for %s: %w", path, err)
		}
	}
	return nil
}
