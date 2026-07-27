package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sternrassler/vidscribe/internal/runtimeenv"
	"github.com/sternrassler/vidscribe/internal/subprocess"
)

const (
	maxRetries    = 3
	retryBaseWait = 5 * time.Second
)

// Download retrieves the audio track for the given URL and returns the local
// path to the downloaded audio file together with video metadata.
// The caller is responsible for deleting the file when done.
func Download(ctx context.Context, cfg *Config, logw io.Writer) (audioPath string, meta *Metadata, err error) {
	tmpDir, err := os.MkdirTemp("", "vidscribe-dl-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}

	if cfg.CaptionMode != "off" {
		captionPath, captionErr := downloadCaptions(ctx, cfg, tmpDir, logw)
		if captionErr == nil {
			meta, err = parseInfoJSON(tmpDir)
			if err != nil {
				os.RemoveAll(tmpDir)
				return "", nil, fmt.Errorf("metadata: %w", err)
			}
			return captionPath, meta, nil
		}
		if !cfg.AllowFallback {
			os.RemoveAll(tmpDir)
			return "", nil, fmt.Errorf("captions unavailable and fallback is disabled: %w", captionErr)
		}
		cfg.SourceFallbackReason = captionErr.Error()
		fmt.Fprintf(logw, "[vidscribe] captions unavailable (%v) — degraded fallback to ASR\n", captionErr)
	}

	// Single yt-dlp call: download native audio + write metadata JSON side-by-side.
	audioPath, err = downloadAudio(ctx, cfg, tmpDir, logw)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, err
	}

	meta, err = parseInfoJSON(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("metadata: %w", err)
	}

	return audioPath, meta, nil
}

func downloadCaptions(ctx context.Context, cfg *Config, destDir string, logw io.Writer) (string, error) {
	args := buildBaseArgs(cfg)
	args = append(args, "--no-playlist", "--skip-download", "--write-info-json",
		"--sub-format", "vtt", "--sub-langs", cfg.Language,
		"--max-filesize", cfg.MaxFileSize,
		"--match-filter", fmt.Sprintf("duration <= %d", cfg.MaxDuration),
		"--output", destDir+"/%(id)s.%(ext)s")
	if cfg.CaptionMode == "manual" {
		args = append(args, "--write-subs")
	} else {
		args = append(args, "--write-subs", "--write-auto-subs")
	}
	args = append(args, cfg.URL)
	cmd := YtdlpCmd(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp captions: %s", meaningfulError(stderr.String(), err))
	}
	matches, _ := filepath.Glob(filepath.Join(destDir, "*.vtt"))
	if len(matches) == 0 {
		return "", fmt.Errorf("no %s captions for language %s", cfg.CaptionMode, cfg.Language)
	}
	return matches[0], nil
}

// downloadAudio downloads the audio track with --write-info-json so metadata
// can be parsed from the sidecar file, eliminating a separate yt-dlp call.
// Includes retry logic for HTTP 403/429.
func downloadAudio(ctx context.Context, cfg *Config, destDir string, logw io.Writer) (string, error) {
	args := buildBaseArgs(cfg)
	args = append(args,
		"--no-playlist",
		"--format", "bestaudio/best",
		"--write-info-json",
		"--max-filesize", cfg.MaxFileSize,
		"--match-filter", fmt.Sprintf("duration <= %d", cfg.MaxDuration),
		"--output", destDir+"/%(id)s.%(ext)s",
		cfg.URL,
	)

	var lastErr error
	secretstorageRetried := false

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * retryBaseWait
			if cfg.Verbose {
				fmt.Fprintf(logw, "[vidscribe] retry %d/%d in %s…\n", attempt, maxRetries-1, wait)
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
		}

		effectiveArgs := args
		// On secretstorage retry, switch from browser cookies to file cookies.
		if secretstorageRetried && cfg.CookiesFile != "" {
			effectiveArgs = removeBrowserCookies(args)
			effectiveArgs = append(effectiveArgs, "--cookies", cfg.CookiesFile)
		}

		var stderr bytes.Buffer
		cmd := YtdlpCmd(ctx, effectiveArgs...)
		cmd.Stderr = &stderr

		if cfg.Verbose {
			fmt.Fprintf(logw, "[vidscribe] yt-dlp: %s\n", strings.Join(cmd.Args, " "))
		}

		err := cmd.Run()
		if err == nil {
			return findAudioFile(destDir)
		}

		errMsg := stderr.String()
		errLine := meaningfulError(errMsg, err)
		if errLine == "" {
			errLine = err.Error()
		}
		lastErr = fmt.Errorf("yt-dlp: %s", errLine)

		if isKeyringError(errMsg) && !secretstorageRetried {
			if cfg.CookiesFile != "" {
				fmt.Fprintf(logw, "[vidscribe] keyring error — retrying with cookie file %s\n", cfg.CookiesFile)
				secretstorageRetried = true
				attempt-- // don't count this as a retry
				continue
			}
			return "", fmt.Errorf("keyring unavailable — export browser cookies to a file and use --cookies-file")
		}

		if isHTTP429(errMsg) {
			fmt.Fprintf(logw, "[vidscribe] HTTP 429 (rate limited) — will retry\n")
			continue
		}

		if isHTTP403(errMsg) {
			if cfg.CookiesBrowser == "" && cfg.CookiesFile == "" {
				return "", fmt.Errorf("HTTP 403 — try --cookies-browser chrome (or --cookies-file)")
			}
			// Already have auth; one more retry.
			continue
		}

		// Non-retryable error.
		return "", lastErr
	}

	return "", fmt.Errorf("download failed after %d attempts: %w", maxRetries, lastErr)
}

// findAudioFile locates the downloaded audio file in destDir.
func findAudioFile(destDir string) (string, error) {
	for _, ext := range []string{"m4a", "opus", "webm", "ogg", "wav", "flac", "mp3", "aac", "mp4", "mkv"} {
		matches, _ := filepath.Glob(filepath.Join(destDir, "*."+ext))
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf("download completed but audio file not found in %s", destDir)
}

// parseInfoJSON reads the .info.json sidecar written by --write-info-json.
func parseInfoJSON(dir string) (*Metadata, error) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.info.json"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("no .info.json found in %s", dir)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, fmt.Errorf("read info json: %w", err)
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse info json: %w", err)
	}
	return &meta, nil
}

// YtdlpCmd constructs the yt-dlp exec.Cmd using uvx.
func YtdlpCmd(ctx context.Context, args ...string) *exec.Cmd {
	uvxArgs := append([]string{"--with", "secretstorage", runtimeenv.YTDLP}, args...)
	return subprocess.CommandContext(ctx, "uvx", uvxArgs...)
}

// buildBaseArgs returns the common yt-dlp flags derived from cfg.
func buildBaseArgs(cfg *Config) []string {
	var args []string

	if cfg.CookiesBrowser != "" {
		args = append(args, "--cookies-from-browser", cfg.CookiesBrowser)
	} else if cfg.CookiesFile != "" {
		args = append(args, "--cookies", cfg.CookiesFile)
	}

	jsRuntime := cfg.JSRuntime
	if jsRuntime == "" {
		if p, err := exec.LookPath("node"); err == nil {
			jsRuntime = "node:" + p
		}
	}
	if jsRuntime != "" {
		args = append(args, "--js-runtimes", jsRuntime)
	}

	return args
}

// removeBrowserCookies strips --cookies-from-browser from args.
func removeBrowserCookies(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--cookies-from-browser" {
			if i+1 < len(args) {
				i++ // skip the value
			}
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// isKeyringError detects platform-specific keyring/cookie-decryption failures
// reported by yt-dlp across Linux (secretstorage), macOS (Keychain), and
// Windows (DPAPI / win32crypt).
func isKeyringError(s string) bool {
	patterns := []string{
		// Linux
		"secretstorage",
		"Failed to unlock keyring",
		"No module named 'secretstorage'",
		// macOS
		"Keychain",
		"OSStatus",
		"security: SecKeychainSearchCopyNext",
		"cannot be found in the keychain",
		// Windows
		"CryptUnprotectData",
		"win32crypt",
		"No module named 'win32crypt'",
	}
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func isHTTP403(s string) bool {
	return strings.Contains(s, "HTTP Error 403") || strings.Contains(s, "403 Forbidden")
}

func isHTTP429(s string) bool {
	return strings.Contains(s, "HTTP Error 429") || strings.Contains(s, "429 Too Many Requests")
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return strings.TrimSpace(s)
}

func meaningfulError(stderr string, fallback error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "ERROR:") {
			return line
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	if fallback != nil {
		return fallback.Error()
	}
	return "unknown error"
}
