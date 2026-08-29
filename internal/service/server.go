package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sternrassler/vidscribe/internal/deps"
	"github.com/sternrassler/vidscribe/internal/netguard"
	"github.com/sternrassler/vidscribe/internal/pipeline"
)

const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

type JobRequest struct {
	SourceType     string   `json:"source_type"`
	SourceURL      string   `json:"source_url"`
	CanonicalID    string   `json:"canonical_id,omitempty"`
	Title          string   `json:"title,omitempty"`
	Creator        string   `json:"creator,omitempty"`
	PublishedAt    string   `json:"published_at,omitempty"`
	Profile        string   `json:"profile,omitempty"`
	Engine         string   `json:"engine,omitempty"`
	Model          string   `json:"model,omitempty"`
	Language       string   `json:"language,omitempty"`
	Device         string   `json:"device,omitempty"`
	ComputeType    string   `json:"compute_type,omitempty"`
	Captions       string   `json:"captions,omitempty"`
	AllowFallback  bool     `json:"allow_fallback,omitempty"`
	MaxDuration    int      `json:"max_duration,omitempty"`
	MaxFileSize    string   `json:"max_filesize,omitempty"`
	WordTimestamps bool     `json:"word_timestamps,omitempty"`
	Formats        []string `json:"formats,omitempty"`
}

type Job struct {
	ID         string                  `json:"id"`
	Status     string                  `json:"status"`
	Request    JobRequest              `json:"request"`
	CreatedAt  time.Time               `json:"created_at"`
	StartedAt  *time.Time              `json:"started_at,omitempty"`
	FinishedAt *time.Time              `json:"finished_at,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Progress   *pipeline.ProgressEvent `json:"progress,omitempty"`
	Result     *pipeline.RunResult     `json:"result,omitempty"`
}

type Runner func(context.Context, *pipeline.Config, io.Writer) (*pipeline.RunResult, error)

type Config struct {
	DataDir         string
	APIToken        string
	APITokens       []string
	MaxRuntime      time.Duration
	Runner          Runner
	DependencyCheck func(string) error
}

type Server struct {
	cfg   Config
	mu    sync.RWMutex
	jobs  map[string]*Job
	queue chan string
}

func New(cfg Config) (*Server, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = "./vidscribe-data"
	}
	if cfg.MaxRuntime <= 0 {
		cfg.MaxRuntime = 6 * time.Hour
	}
	if cfg.Runner == nil {
		cfg.Runner = pipeline.RunDetailed
	}
	if cfg.DependencyCheck == nil {
		cfg.DependencyCheck = deps.Check
	}
	absDataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve service data directory: %w", err)
	}
	cfg.DataDir = absDataDir
	for _, dir := range []string{cfg.DataDir, filepath.Join(cfg.DataDir, "jobs"), filepath.Join(cfg.DataDir, "artifacts")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create service data directory: %w", err)
		}
	}
	s := &Server{cfg: cfg, jobs: map[string]*Job{}, queue: make(chan string, 128)}
	if err := s.loadJobs(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) Start(ctx context.Context) {
	go s.worker(ctx)
	s.mu.RLock()
	var pending []string
	for id, job := range s.jobs {
		if job.Status == StatusQueued {
			pending = append(pending, id)
		}
	}
	s.mu.RUnlock()
	sort.Strings(pending)
	for _, id := range pending {
		s.enqueue(ctx, id)
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.health)
	mux.HandleFunc("GET /metrics", s.metrics)
	mux.Handle("POST /v1/jobs", s.authorize(http.HandlerFunc(s.createJob)))
	mux.Handle("GET /v1/jobs/{id}", s.authorize(http.HandlerFunc(s.getJob)))
	mux.Handle("GET /v1/jobs/{id}/transcript", s.authorize(http.HandlerFunc(s.getTranscript)))
	mux.Handle("GET /v1/jobs/{id}/manifest", s.authorize(http.HandlerFunc(s.getManifest)))
	mux.Handle("GET /v1/jobs/{id}/artifacts/{format}", s.authorize(http.HandlerFunc(s.getArtifact)))
	return mux
}

func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r.Header.Get("Authorization")) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(header string) bool {
	tokens := make([]string, 0, len(s.cfg.APITokens)+1)
	for _, token := range s.cfg.APITokens {
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	if s.cfg.APIToken != "" {
		tokens = append(tokens, s.cfg.APIToken)
	}
	if len(tokens) == 0 {
		return true
	}
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	presented := []byte(strings.TrimPrefix(header, "Bearer "))
	for _, token := range tokens {
		if subtle.ConstantTimeCompare(presented, []byte(token)) == 1 {
			return true
		}
	}
	return false
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var req JobRequest
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := normalizeRequest(r.Context(), &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := jobID(req)

	s.mu.Lock()
	if existing := s.jobs[id]; existing != nil {
		status := http.StatusAccepted
		requeue := false
		if existing.Status == StatusCompleted {
			status = http.StatusOK
		} else if existing.Status == StatusFailed {
			existing.Status = StatusQueued
			existing.Error = ""
			existing.StartedAt = nil
			existing.FinishedAt = nil
			existing.Progress = nil
			_ = s.persistLocked(existing)
			requeue = true
		}
		copy := *existing
		s.mu.Unlock()
		if requeue {
			s.enqueue(r.Context(), id)
		}
		writeJSON(w, status, &copy)
		return
	}
	job := &Job{ID: id, Status: StatusQueued, Request: req, CreatedAt: time.Now().UTC()}
	s.jobs[id] = job
	if err := s.persistLocked(job); err != nil {
		delete(s.jobs, id)
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "persist job: "+err.Error())
		return
	}
	copy := *job
	s.mu.Unlock()
	s.enqueue(r.Context(), id)
	writeJSON(w, http.StatusAccepted, &copy)
}

func normalizeRequest(ctx context.Context, req *JobRequest) error {
	req.SourceType = strings.ToLower(strings.TrimSpace(req.SourceType))
	if req.SourceType == "" {
		req.SourceType = "video"
	}
	if req.SourceURL == "" {
		return fmt.Errorf("source_url is required")
	}
	if err := netguard.ValidatePublicURL(ctx, req.SourceURL); err != nil {
		return fmt.Errorf("unsafe or invalid source_url: %w", err)
	}
	if req.Profile == "" {
		req.Profile = pipeline.DefaultProfile
	}
	if req.Language == "" {
		req.Language = "auto"
	}
	if req.Captions == "" {
		req.Captions = "off"
	}
	if req.MaxDuration == 0 {
		req.MaxDuration = pipeline.DefaultMaxDuration
	}
	if req.MaxFileSize == "" {
		req.MaxFileSize = pipeline.DefaultMaxFileSize
	}
	cfg := requestConfig(*req, "")
	if err := cfg.Normalize(ctx); err != nil {
		return fmt.Errorf("invalid transcription configuration: %w", err)
	}
	if req.Captions != "off" && req.Language == "auto" {
		return fmt.Errorf("captions require an explicit language")
	}
	return nil
}

func jobID(req JobRequest) string {
	data, _ := json.Marshal(req)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job := s.job(r.PathValue("id"))
	if job == nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) getTranscript(w http.ResponseWriter, r *http.Request) {
	s.serveArtifact(w, r, ".txt", "text/plain; charset=utf-8")
}

func (s *Server) getManifest(w http.ResponseWriter, r *http.Request) {
	s.serveArtifact(w, r, ".manifest.json", "application/json")
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	format := strings.ToLower(r.PathValue("format"))
	suffixes := map[string]string{
		"txt": ".txt", "md": ".md", "json": ".json", "srt": ".srt",
		"vtt": ".vtt", "manifest": ".manifest.json",
	}
	contentTypes := map[string]string{
		"txt": "text/plain; charset=utf-8", "md": "text/markdown; charset=utf-8",
		"json": "application/json", "srt": "application/x-subrip; charset=utf-8",
		"vtt": "text/vtt; charset=utf-8", "manifest": "application/json",
	}
	suffix, ok := suffixes[format]
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported artifact format")
		return
	}
	s.serveArtifact(w, r, suffix, contentTypes[format])
}

func (s *Server) serveArtifact(w http.ResponseWriter, r *http.Request, suffix, contentType string) {
	job := s.job(r.PathValue("id"))
	if job == nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if job.Status != StatusCompleted || job.Result == nil {
		writeError(w, http.StatusConflict, "job is not completed")
		return
	}
	for _, path := range job.Result.Paths {
		if strings.HasSuffix(path, suffix) {
			w.Header().Set("Content-Type", contentType)
			http.ServeFile(w, r, path)
			return
		}
	}
	writeError(w, http.StatusNotFound, "artifact not found")
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	counts := map[string]int{StatusQueued: 0, StatusRunning: 0, StatusCompleted: 0, StatusFailed: 0}
	s.mu.RLock()
	for _, job := range s.jobs {
		counts[job.Status]++
	}
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	for _, status := range []string{StatusQueued, StatusRunning, StatusCompleted, StatusFailed} {
		fmt.Fprintf(w, "vidscribe_jobs{status=%q} %d\n", status, counts[status])
	}
}

func (s *Server) job(id string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job := s.jobs[id]
	if job == nil {
		return nil
	}
	copy := *job
	return &copy
}

func (s *Server) enqueue(ctx context.Context, id string) {
	select {
	case s.queue <- id:
	case <-ctx.Done():
	}
}

func (s *Server) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-s.queue:
			s.process(ctx, id)
		}
	}
}

func (s *Server) process(parent context.Context, id string) {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil || job.Status != StatusQueued {
		s.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	job.Status, job.StartedAt, job.Error = StatusRunning, &now, ""
	_ = s.persistLocked(job)
	req := job.Request
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(parent, s.cfg.MaxRuntime)
	defer cancel()
	outDir := filepath.Join(s.cfg.DataDir, "artifacts", id)
	if err := os.RemoveAll(outDir); err != nil {
		s.finish(id, nil, fmt.Errorf("clean artifact directory: %w", err))
		return
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		s.finish(id, nil, fmt.Errorf("create artifact directory: %w", err))
		return
	}
	cfg := requestConfig(req, outDir)
	cfg.Progress = func(event pipeline.ProgressEvent) {
		s.mu.Lock()
		if current := s.jobs[id]; current != nil && current.Status == StatusRunning {
			copy := event
			current.Progress = &copy
			_ = s.persistLocked(current)
		}
		s.mu.Unlock()
	}
	if err := s.cfg.DependencyCheck(cfg.Engine); err != nil {
		s.finish(id, nil, err)
		return
	}
	var logs strings.Builder
	result, err := s.cfg.Runner(ctx, cfg, &logs)
	if err != nil && logs.Len() > 0 {
		err = fmt.Errorf("%w; log: %s", err, strings.TrimSpace(logs.String()))
	}
	if errors.Is(ctx.Err(), context.Canceled) && parent.Err() != nil {
		s.requeueInterrupted(id)
		return
	}
	s.finish(id, result, err)
}

func requestConfig(req JobRequest, outputDir string) *pipeline.Config {
	requested := req.Engine
	if requested == "" {
		requested = "profile:" + req.Profile
	}
	formats := req.Formats
	if len(formats) == 0 {
		formats = []string{"txt", "json", "manifest"}
	}
	return &pipeline.Config{
		URL: req.SourceURL, SourceType: req.SourceType, SourceID: req.CanonicalID,
		Title: req.Title, Creator: req.Creator, PublishedAt: req.PublishedAt,
		Profile: req.Profile, Engine: req.Engine, RequestedEngine: requested,
		Model: req.Model, Language: req.Language, Device: req.Device,
		ComputeType: req.ComputeType, CaptionMode: req.Captions,
		AllowFallback: req.AllowFallback, MaxDuration: req.MaxDuration,
		MaxFileSize: req.MaxFileSize, WordTimestamps: req.WordTimestamps,
		Formats: formats, OutputDir: outputDir, Backend: "remote",
	}
}

func (s *Server) finish(id string, result *pipeline.RunResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job == nil {
		return
	}
	now := time.Now().UTC()
	job.FinishedAt, job.Progress, job.Result = &now, nil, result
	if err != nil {
		job.Status, job.Error = StatusFailed, err.Error()
	} else {
		job.Status, job.Error = StatusCompleted, ""
	}
	_ = s.persistLocked(job)
}

func (s *Server) requeueInterrupted(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job := s.jobs[id]; job != nil {
		job.Status, job.StartedAt, job.Progress = StatusQueued, nil, nil
		job.Error = "interrupted by service shutdown; queued for restart"
		_ = s.persistLocked(job)
	}
}

func (s *Server) loadJobs() error {
	entries, err := os.ReadDir(filepath.Join(s.cfg.DataDir, "jobs"))
	if err != nil {
		return fmt.Errorf("read jobs: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "jobs", entry.Name()))
		if err != nil {
			return fmt.Errorf("read job %s: %w", entry.Name(), err)
		}
		var job Job
		if err := json.Unmarshal(data, &job); err != nil {
			return fmt.Errorf("parse job %s: %w", entry.Name(), err)
		}
		if job.Status == StatusRunning {
			job.Status, job.StartedAt, job.Progress = StatusQueued, nil, nil
			job.Error = "recovered after service restart"
			if err := s.persistLocked(&job); err != nil {
				return err
			}
		}
		s.jobs[job.ID] = &job
	}
	return nil
}

func (s *Server) persistLocked(job *Job) error {
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(s.cfg.DataDir, "jobs")
	tmp, err := os.CreateTemp(dir, ".job-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o640); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, job.ID+".json"))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
