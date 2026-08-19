package pipeline

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultProfile     = "balanced"
	DefaultMaxDuration = 4 * 60 * 60
	DefaultMaxFileSize = "2G"
	minCUDAFreeMiB     = 1024
)

var (
	validProfiles    = set("quality", "balanced", "gpu-free", "fast", "custom")
	validEngines     = set("faster", "openai", "parakeet")
	validModels      = set("tiny", "base", "small", "medium", "large", "large-v1", "large-v2", "large-v3", "turbo")
	validDevices     = set("auto", "cpu", "cuda")
	validCompute     = set("int8", "int8_float16", "float16", "float32")
	validFormats     = set("txt", "md", "json", "srt", "vtt", "manifest")
	sizePattern      = regexp.MustCompile(`(?i)^[0-9]+(?:\.[0-9]+)?[kmgtp]?$`)
	languagePattern  = regexp.MustCompile(`(?i)^[a-z]{2,3}(?:-[a-z0-9]{2,8})*$`)
	whisperLanguages = set(strings.Fields("af am ar as az ba be bg bn bo br bs ca cs cy da de el en es et eu fa fi fo fr gl gu ha haw he hi hr ht hu hy id is it ja jw ka kk km kn ko la lb ln lo lt lv mg mi mk ml mn mr ms mt my ne nl nn no oc pa pl ps pt ro ru sa sd si sk sl sn so sq sr su sv sw ta te tg th tk tl tr tt uk ur uz vi yi yo yue zh")...)
)

// ProgressEvent describes an observable pipeline stage.
type ProgressEvent struct {
	Step    int
	Total   int
	Stage   string
	Message string
}

// Config holds all parameters for a single transcription run.
type Config struct {
	URL                  string
	SourceType           string
	SourceID             string
	Title                string
	Creator              string
	PublishedAt          string
	RequestedEngine      string
	Profile              string
	Model                string
	Language             string
	OutputDir            string
	CookiesBrowser       string
	CookiesFile          string
	JSRuntime            string
	Formats              []string
	Engine               string
	Device               string
	ComputeType          string
	CaptionMode          string
	AllowFallback        bool
	MaxDuration          int
	MaxFileSize          string
	Verbose              bool
	Overwrite            bool
	WordTimestamps       bool
	Progress             func(ProgressEvent)
	DependencyVersion    map[string]string
	SourceFallbackReason string
}

// Metadata holds video information retrieved from yt-dlp.
type Metadata struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Uploader   string  `json:"uploader"`
	Channel    string  `json:"channel"`
	Duration   float64 `json:"duration"`
	UploadDate string  `json:"upload_date"`
	WebpageURL string  `json:"webpage_url"`
}

// Normalize validates the public configuration contract, applies a profile and
// resolves auto hardware selection before any external process is started.
func (c *Config) Normalize(ctx context.Context) error {
	c.SourceType = strings.ToLower(strings.TrimSpace(c.SourceType))
	if c.SourceType == "" {
		c.SourceType = "video"
	}
	if c.SourceType != "video" && c.SourceType != "podcast" && c.SourceType != "audio" {
		return fmt.Errorf("unsupported source type %q (want video|podcast|audio)", c.SourceType)
	}
	c.Profile = strings.ToLower(strings.TrimSpace(c.Profile))
	if c.Profile == "" {
		c.Profile = "custom"
	}
	if !validProfiles[c.Profile] {
		return fmt.Errorf("unsupported profile %q (want quality|balanced|gpu-free|fast|custom)", c.Profile)
	}

	applyProfileDefaults(c)
	c.Engine = strings.ToLower(strings.TrimSpace(c.Engine))
	c.Model = strings.ToLower(strings.TrimSpace(c.Model))
	c.Device = strings.ToLower(strings.TrimSpace(c.Device))
	c.ComputeType = strings.ToLower(strings.TrimSpace(c.ComputeType))
	c.CaptionMode = strings.ToLower(strings.TrimSpace(c.CaptionMode))
	if c.Language == "" {
		c.Language = "auto"
	}
	if c.Language != "auto" && !languagePattern.MatchString(c.Language) {
		return fmt.Errorf("invalid language code %q", c.Language)
	}
	if c.OutputDir == "" {
		c.OutputDir = "./transcripts"
	}
	if c.CaptionMode == "" {
		c.CaptionMode = "off"
	}
	if c.MaxDuration == 0 {
		c.MaxDuration = DefaultMaxDuration
	}
	if c.MaxFileSize == "" {
		c.MaxFileSize = DefaultMaxFileSize
	}

	if !validEngines[c.Engine] {
		return fmt.Errorf("unsupported engine %q (want faster|openai|parakeet)", c.Engine)
	}
	if c.Language != "auto" && c.Engine != "parakeet" && !whisperLanguages[strings.ToLower(c.Language)] && (c.CaptionMode == "off" || c.AllowFallback) {
		return fmt.Errorf("language %q is not supported by the selected ASR fallback", c.Language)
	}
	if c.Engine != "parakeet" && !validModels[c.Model] {
		return fmt.Errorf("unsupported model %q", c.Model)
	}
	if !validDevices[c.Device] {
		return fmt.Errorf("unsupported device %q (want auto|cpu|cuda)", c.Device)
	}
	if c.CaptionMode != "off" && c.CaptionMode != "manual" && c.CaptionMode != "auto" {
		return fmt.Errorf("unsupported captions mode %q (want off|manual|auto)", c.CaptionMode)
	}
	if c.MaxDuration < 1 {
		return fmt.Errorf("max duration must be positive")
	}
	if !sizePattern.MatchString(c.MaxFileSize) {
		return fmt.Errorf("invalid max file size %q", c.MaxFileSize)
	}
	if c.JSRuntime != "" {
		kind, path, ok := strings.Cut(c.JSRuntime, ":")
		if !ok || (kind != "node" && kind != "deno") || path == "" {
			return fmt.Errorf("invalid js runtime %q (want node:/path or deno:/path)", c.JSRuntime)
		}
	}

	formats := make([]string, 0, len(c.Formats)+1)
	seen := map[string]bool{}
	for _, format := range c.Formats {
		format = strings.ToLower(strings.TrimSpace(format))
		if !validFormats[format] {
			return fmt.Errorf("unsupported output format %q", format)
		}
		if !seen[format] {
			formats = append(formats, format)
			seen[format] = true
		}
	}
	if len(formats) == 0 {
		return fmt.Errorf("at least one output format is required")
	}
	// Provenance is a contract, not an optional side effect.
	if !seen["manifest"] {
		formats = append(formats, "manifest")
	}
	c.Formats = formats

	if c.Engine == "parakeet" {
		c.Device = "cpu"
		c.ComputeType = "int8"
		return nil
	}

	if c.Device == "auto" {
		if cudaProbe(ctx) {
			c.Device = "cuda"
		} else {
			c.Device = "cpu"
			if c.Profile == "balanced" {
				c.Engine = "parakeet"
				c.ComputeType = "int8"
				return nil
			}
		}
	}
	if c.Device == "cuda" && !cudaSupportedPlatform() {
		return fmt.Errorf("cuda is not supported by vidscribe on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if c.ComputeType == "" {
		if c.Device == "cuda" {
			c.ComputeType = "float16"
		} else {
			c.ComputeType = "int8"
		}
	}
	if !validCompute[c.ComputeType] {
		return fmt.Errorf("unsupported compute type %q", c.ComputeType)
	}
	if c.Device == "cpu" && c.ComputeType == "float16" {
		return fmt.Errorf("compute type float16 is not supported on CPU; use int8 or float32")
	}
	return nil
}

func applyProfileDefaults(c *Config) {
	switch c.Profile {
	case "quality":
		defaults(c, "faster", "large-v3", "auto")
	case "balanced":
		defaults(c, "faster", "medium", "auto")
	case "gpu-free":
		defaults(c, "parakeet", "medium", "cpu")
	case "fast":
		defaults(c, "faster", "small", "auto")
	default:
		defaults(c, "faster", "small", "auto")
	}
}

func defaults(c *Config, engine, model, device string) {
	if c.Engine == "" {
		c.Engine = engine
	}
	if c.Model == "" {
		c.Model = model
	}
	if c.Device == "" {
		c.Device = device
	}
}

func cudaAvailable(ctx context.Context) bool {
	if !cudaSupportedPlatform() {
		return false
	}
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, path, "--query-gpu=memory.free", "--format=csv,noheader,nounits").Output()
	return err == nil && hasSufficientCUDAFreeMemory(string(out))
}

func hasSufficientCUDAFreeMemory(output string) bool {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	freeMiB, err := strconv.Atoi(strings.TrimSpace(line))
	return err == nil && freeMiB >= minCUDAFreeMiB
}

var cudaProbe = cudaAvailable

func cudaSupportedPlatform() bool {
	return runtime.GOOS == "linux" || (runtime.GOOS == "windows" && runtime.GOARCH == "amd64")
}

func set(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, value := range values {
		m[value] = true
	}
	return m
}

// SafeTitle returns a portable, UTF-8-safe output stem with the video ID so
// equal titles do not collide.
func (m *Metadata) SafeTitle() string {
	title := strings.TrimSpace(m.Title)
	if title == "" {
		title = m.ID
	}
	var b strings.Builder
	for _, r := range title {
		if unicode.IsControl(r) || strings.ContainsRune(`/\\:*?"<>|`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	s := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	for len(s) > 100 {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	if s == "" {
		s = "video"
	}
	if m.ID != "" && !strings.HasSuffix(s, " ["+m.ID+"]") {
		s += " [" + m.ID + "]"
	}
	return s
}

func (c *Config) HasFormat(format string) bool {
	for _, candidate := range c.Formats {
		if strings.TrimSpace(candidate) == format {
			return true
		}
	}
	return false
}

func (c *Config) emit(step, total int, stage, message string) {
	if c.Progress != nil {
		c.Progress(ProgressEvent{Step: step, Total: total, Stage: stage, Message: message})
	}
}
