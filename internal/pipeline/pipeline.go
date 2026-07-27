package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sternrassler/vidscribe/internal/runtimeenv"
)

type RunResult struct {
	Paths     []string      `json:"paths"`
	Metadata  *Metadata     `json:"video"`
	Execution ExecutionInfo `json:"execution"`
}

type ExecutionInfo struct {
	RequestedEngine  string            `json:"requested_engine"`
	ActualEngine     string            `json:"actual_engine"`
	Profile          string            `json:"profile"`
	Model            string            `json:"model"`
	Device           string            `json:"device"`
	ComputeType      string            `json:"compute_type"`
	DetectedLanguage string            `json:"detected_language"`
	Confidence       *float64          `json:"confidence,omitempty"`
	CaptionMode      string            `json:"caption_mode"`
	Degraded         bool              `json:"degraded"`
	Fallbacks        []FallbackEvent   `json:"fallbacks"`
	Dependencies     map[string]string `json:"dependencies"`
	StartedAt        time.Time         `json:"started_at"`
	DurationMS       int64             `json:"duration_ms"`
	RealtimeFactor   float64           `json:"realtime_factor"`
}

// Run is the compatibility entry point for callers interested only in files.
func Run(ctx context.Context, cfg *Config, logw io.Writer) ([]string, error) {
	result, err := RunDetailed(ctx, cfg, logw)
	if result == nil {
		return nil, err
	}
	return result.Paths, err
}

// RunDetailed executes the full pipeline and returns reproducibility metadata.
func RunDetailed(ctx context.Context, cfg *Config, logw io.Writer) (*RunResult, error) {
	if logw == nil {
		logw = io.Discard
	}
	requestedEngine := cfg.RequestedEngine
	if requestedEngine == "" {
		requestedEngine = cfg.Engine
	}
	if requestedEngine == "" {
		requestedEngine = "profile:" + firstNonEmpty(cfg.Profile, DefaultProfile)
	}
	if err := cfg.Normalize(ctx); err != nil {
		return nil, fmt.Errorf("configuration: %w", err)
	}
	cfg.SourceFallbackReason = ""
	if cfg.DependencyVersion == nil {
		cfg.DependencyVersion = runtimeenv.Versions()
	}

	started := time.Now().UTC()
	cfg.emit(1, 4, "download", "Metadaten und bestes natives Audio werden geladen")
	audioPath, meta, err := Download(ctx, cfg, logw)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(audioPath))

	if meta.Duration > float64(cfg.MaxDuration) {
		return nil, fmt.Errorf("video duration %.0fs exceeds configured maximum %ds", meta.Duration, cfg.MaxDuration)
	}
	cfg.emit(2, 4, "transcribe", fmt.Sprintf("Transkription mit %s/%s", cfg.Engine, cfg.Model))
	tx, err := Transcribe(ctx, cfg, audioPath, logw)
	if err != nil {
		return nil, fmt.Errorf("transcribe: %w", err)
	}
	defer os.RemoveAll(tx.TempDir)

	fallbacks := append([]FallbackEvent{}, tx.Fallbacks...)
	if cfg.SourceFallbackReason != "" {
		fallbacks = append([]FallbackEvent{{From: "captions-" + cfg.CaptionMode, To: tx.ActualEngine, Reason: cfg.SourceFallbackReason}}, fallbacks...)
	}
	execution := ExecutionInfo{
		RequestedEngine: requestedEngine, ActualEngine: tx.ActualEngine,
		Profile: cfg.Profile, Model: cfg.Model, Device: cfg.Device, ComputeType: cfg.ComputeType,
		DetectedLanguage: tx.DetectedLanguage, Confidence: tx.Confidence, CaptionMode: cfg.CaptionMode,
		Degraded: len(fallbacks) > 0, Fallbacks: fallbacks,
		Dependencies: cfg.DependencyVersion, StartedAt: started,
	}
	cfg.emit(3, 4, "write", "Transkript und Provenienz werden atomar geschrieben")
	paths, err := WriteOutputsDetailed(cfg, tx, meta, &execution, logw)
	execution.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		return &RunResult{Paths: paths, Metadata: meta, Execution: execution}, fmt.Errorf("write outputs: %w", err)
	}
	cfg.emit(4, 4, "complete", "Transkription abgeschlossen")
	return &RunResult{Paths: paths, Metadata: meta, Execution: execution}, nil
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
