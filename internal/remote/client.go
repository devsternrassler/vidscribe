package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sternrassler/vidscribe/internal/pipeline"
	"github.com/sternrassler/vidscribe/internal/service"
)

const maxArtifactBytes = 128 << 20

type Config struct {
	BaseURL      string
	TokenFile    string
	PollInterval time.Duration
	HTTPClient   *http.Client
}

type Client struct {
	baseURL      string
	token        string
	pollInterval time.Duration
	httpClient   *http.Client
}

type Error struct {
	Kind          string
	Reason        string
	AllowFallback bool
	Err           error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Reason
	}
	return e.Reason + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

func FallbackReason(err error) (string, bool) {
	var remoteErr *Error
	if !errors.As(err, &remoteErr) || !remoteErr.AllowFallback {
		return "", false
	}
	return remoteErr.Reason, true
}

func NewFromEnvironment() (*Client, error) {
	baseURL := strings.TrimSpace(os.Getenv("VIDSCRIBE_REMOTE_URL"))
	if baseURL == "" {
		return nil, nil
	}
	pollInterval := 2 * time.Second
	if raw := strings.TrimSpace(os.Getenv("VIDSCRIBE_REMOTE_POLL_INTERVAL")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("invalid VIDSCRIBE_REMOTE_POLL_INTERVAL")
		}
		pollInterval = parsed
	}
	return New(Config{
		BaseURL: baseURL, TokenFile: strings.TrimSpace(os.Getenv("VIDSCRIBE_REMOTE_TOKEN_FILE")),
		PollInterval: pollInterval,
	})
}

func New(cfg Config) (*Client, error) {
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("VIDSCRIBE_REMOTE_URL must be a plain HTTP loopback URL")
	}
	if host := parsed.Hostname(); host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, fmt.Errorf("VIDSCRIBE_REMOTE_URL must use loopback through the private SSH tunnel")
	}
	if cfg.TokenFile == "" || !filepath.IsAbs(cfg.TokenFile) {
		return nil, fmt.Errorf("VIDSCRIBE_REMOTE_TOKEN_FILE must be an absolute path")
	}
	info, err := os.Lstat(cfg.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("inspect VIDSCRIBE_REMOTE_TOKEN_FILE: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("VIDSCRIBE_REMOTE_TOKEN_FILE must be a regular non-symlink file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("VIDSCRIBE_REMOTE_TOKEN_FILE permissions must be 0600 or stricter")
	}
	tokenBytes, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("read VIDSCRIBE_REMOTE_TOKEN_FILE: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) < 32 {
		return nil, fmt.Errorf("VIDSCRIBE_REMOTE_TOKEN_FILE contains an invalid token")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
		}}
	}
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"), token: token,
		pollInterval: pollInterval, httpClient: client,
	}, nil
}

func (c *Client) Run(ctx context.Context, cfg *pipeline.Config) (*pipeline.RunResult, error) {
	jobRequest := service.JobRequest{
		SourceType: cfg.SourceType, SourceURL: cfg.URL, Profile: cfg.Profile,
		Engine: cfg.Engine, Model: cfg.Model, Language: cfg.Language,
		Device: cfg.Device, ComputeType: cfg.ComputeType, Captions: cfg.CaptionMode,
		AllowFallback: cfg.AllowFallback, MaxDuration: cfg.MaxDuration,
		MaxFileSize: cfg.MaxFileSize, WordTimestamps: cfg.WordTimestamps,
		Formats: append([]string{}, cfg.Formats...),
	}
	body, err := json.Marshal(jobRequest)
	if err != nil {
		return nil, &Error{Kind: "configuration", Reason: "encode remote request", Err: err}
	}
	var job service.Job
	status, err := c.requestJSON(ctx, http.MethodPost, "/v1/jobs", body, &job)
	if err != nil {
		return nil, err
	}
	if status != http.StatusAccepted && status != http.StatusOK {
		return nil, classifyStatus(status, "create remote job")
	}
	if job.ID == "" {
		return nil, &Error{Kind: "protocol", Reason: "remote service returned an empty job id"}
	}

	for {
		switch job.Status {
		case service.StatusCompleted:
			if job.Result == nil || job.Result.Metadata == nil {
				return nil, &Error{Kind: "protocol", Reason: "remote service returned an incomplete result"}
			}
			paths, err := c.materialize(ctx, job.ID, cfg, job.Result.Metadata)
			if err != nil {
				return nil, err
			}
			result := *job.Result
			result.Paths = paths
			return &result, nil
		case service.StatusFailed:
			return nil, &Error{Kind: "remote_job", Reason: "remote transcription failed", AllowFallback: true}
		case service.StatusQueued, service.StatusRunning:
		case "":
			return nil, &Error{Kind: "protocol", Reason: "remote service returned an empty job status"}
		default:
			return nil, &Error{Kind: "protocol", Reason: "remote service returned an unknown job status"}
		}

		select {
		case <-ctx.Done():
			return nil, &Error{Kind: "cancelled", Reason: "remote transcription cancelled", Err: ctx.Err()}
		case <-time.After(c.pollInterval):
		}
		status, err = c.requestJSON(ctx, http.MethodGet, "/v1/jobs/"+url.PathEscape(job.ID), nil, &job)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, classifyStatus(status, "poll remote job")
		}
	}
}

func (c *Client) materialize(ctx context.Context, jobID string, cfg *pipeline.Config, metadata *pipeline.Metadata) ([]string, error) {
	type artifact struct {
		path string
		data []byte
	}
	formats := append([]string{}, cfg.Formats...)
	artifacts := make([]artifact, 0, len(formats))
	base := filepath.Join(cfg.OutputDir, metadata.SafeTitle())
	for _, format := range formats {
		suffix := "." + format
		if format == "manifest" {
			suffix = ".manifest.json"
		}
		path := base + suffix
		if !cfg.Overwrite {
			if _, err := os.Stat(path); err == nil {
				return nil, &Error{Kind: "local_output", Reason: "remote result cannot be written because output already exists", Err: fmt.Errorf("%s", path)}
			} else if !os.IsNotExist(err) {
				return nil, &Error{Kind: "local_output", Reason: "inspect remote output path", Err: err}
			}
		}
		data, err := c.requestArtifact(ctx, jobID, format)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact{path: path, data: data})
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return nil, &Error{Kind: "local_output", Reason: "create remote output directory", Err: err}
	}
	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if err := atomicWrite(artifact.path, artifact.data); err != nil {
			return paths, &Error{Kind: "local_output", Reason: "write remote output", Err: err}
		}
		paths = append(paths, artifact.path)
	}
	return paths, nil
}

func (c *Client) requestArtifact(ctx context.Context, jobID, format string) ([]byte, error) {
	path := "/v1/jobs/" + url.PathEscape(jobID) + "/artifacts/" + url.PathEscape(format)
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, &Error{Kind: "configuration", Reason: "build remote artifact request", Err: err}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &Error{Kind: "transport", Reason: "remote service is unreachable", AllowFallback: true, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, classifyStatus(resp.StatusCode, "download remote artifact")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes+1))
	if err != nil {
		return nil, &Error{Kind: "transport", Reason: "read remote artifact", AllowFallback: true, Err: err}
	}
	if len(data) > maxArtifactBytes {
		return nil, &Error{Kind: "protocol", Reason: "remote artifact exceeds the 128 MiB client limit"}
	}
	return data, nil
}

func (c *Client) requestJSON(ctx context.Context, method, path string, body []byte, target any) (int, error) {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return 0, &Error{Kind: "configuration", Reason: "build remote request", Err: err}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, &Error{Kind: "transport", Reason: "remote service is unreachable", AllowFallback: true, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(target); err != nil {
		return resp.StatusCode, &Error{Kind: "protocol", Reason: "decode remote response", Err: err}
	}
	return resp.StatusCode, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func classifyStatus(status int, action string) error {
	switch {
	case status == http.StatusBadRequest:
		return &Error{Kind: "validation", Reason: action + " was rejected as invalid"}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return &Error{Kind: "authentication", Reason: action + " was rejected by authentication"}
	case status >= 500:
		return &Error{Kind: "remote_service", Reason: action + " failed because the remote service is unavailable", AllowFallback: true}
	default:
		return &Error{Kind: "protocol", Reason: fmt.Sprintf("%s returned HTTP %d", action, status)}
	}
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".vidscribe-remote-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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
	return os.Rename(tmpPath, path)
}
