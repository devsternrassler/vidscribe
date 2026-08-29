package mcp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sternrassler/vidscribe/internal/deps"
	"github.com/sternrassler/vidscribe/internal/pipeline"
	"github.com/sternrassler/vidscribe/internal/remote"
)

var version = "dev"
var transcribeSlot = make(chan struct{}, 1)

func SetVersion(value string) {
	if value != "" {
		version = value
	}
}

func Serve() error {
	s := server.NewMCPServer("vidscribe", version, server.WithToolCapabilities(false))
	s.AddTool(transcribeVideoTool(), handleTranscribeVideo)
	s.AddTool(checkDependenciesTool(), handleCheckDependencies)
	s.AddTool(listSupportedSitesTool(), handleListSupportedSites)
	return server.ServeStdio(s)
}

func transcribeVideoTool() mcplib.Tool {
	return mcplib.NewTool("transcribe_video",
		mcplib.WithDescription("Download captions or best native audio and produce a provenance-rich transcript."),
		mcplib.WithString("url", mcplib.Required(), mcplib.Description("Public video URL")),
		mcplib.WithString("profile", mcplib.Description("quality | balanced | gpu-free | fast | custom (default: balanced)")),
		mcplib.WithString("model", mcplib.Description("Model override: tiny | base | small | medium | large-v3 | turbo")),
		mcplib.WithString("language", mcplib.Description("Language code or auto (default: auto)")),
		mcplib.WithString("output_dir", mcplib.Description("Directory below VIDSCRIBE_OUTPUT_ROOT")),
		mcplib.WithString("cookies_browser", mcplib.Description("chrome | firefox | safari | edge | chromium | brave | opera | vivaldi")),
		mcplib.WithString("cookies_file", mcplib.Description("Netscape-format cookie file")),
		mcplib.WithString("engine", mcplib.Description("Engine override: faster | openai | parakeet")),
		mcplib.WithString("format", mcplib.Description("txt,md,json,srt,vtt; a manifest is always written")),
		mcplib.WithString("js_runtime", mcplib.Description("node:/path or deno:/path")),
		mcplib.WithString("device", mcplib.Description("auto | cpu | cuda")),
		mcplib.WithString("compute_type", mcplib.Description("int8 | int8_float16 | float16 | float32")),
		mcplib.WithString("captions", mcplib.Description("off | manual | auto; captions require explicit language")),
		mcplib.WithBoolean("allow_fallback", mcplib.Description("Allow a visible degraded engine fallback (default: false)")),
		mcplib.WithNumber("max_duration", mcplib.Description("Maximum duration in seconds (default: 14400)")),
		mcplib.WithString("max_filesize", mcplib.Description("yt-dlp size limit (default: 2G)")),
		mcplib.WithBoolean("overwrite", mcplib.Description("Explicitly replace existing output files (default: false)")),
		mcplib.WithBoolean("word_timestamps", mcplib.Description("Include engine word timestamps in JSON output")),
	)
}

var allowedBrowsers = map[string]bool{
	"chrome": true, "firefox": true, "safari": true, "edge": true,
	"chromium": true, "brave": true, "opera": true, "vivaldi": true,
}

func handleTranscribeVideo(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return mcplib.NewToolResultError("url is required"), nil
	}

	rawCfg := &pipeline.Config{
		URL: rawURL, Profile: stringArg(args, "profile", pipeline.DefaultProfile),
		Model: stringArg(args, "model", ""), Language: stringArg(args, "language", "auto"),
		Engine: stringArg(args, "engine", ""), Formats: strings.Split(stringArg(args, "format", "txt,md"), ","),
		Device: stringArg(args, "device", ""), ComputeType: stringArg(args, "compute_type", ""),
		CaptionMode: stringArg(args, "captions", "off"), AllowFallback: boolArg(args, "allow_fallback", false),
		MaxDuration:    int(numberArg(args, "max_duration", pipeline.DefaultMaxDuration)),
		MaxFileSize:    stringArg(args, "max_filesize", pipeline.DefaultMaxFileSize),
		Overwrite:      boolArg(args, "overwrite", false),
		WordTimestamps: boolArg(args, "word_timestamps", false),
	}
	if rawCfg.Engine != "" {
		rawCfg.RequestedEngine = rawCfg.Engine
	} else {
		rawCfg.RequestedEngine = "profile:" + rawCfg.Profile
	}
	rawCfg.CookiesBrowser, _ = args["cookies_browser"].(string)
	rawCfg.CookiesFile, _ = args["cookies_file"].(string)
	rawCfg.JSRuntime, _ = args["js_runtime"].(string)
	if rawCfg.CookiesBrowser != "" && !allowedBrowsers[strings.ToLower(rawCfg.CookiesBrowser)] {
		return mcplib.NewToolResultError("unsupported browser: " + rawCfg.CookiesBrowser), nil
	}
	localCfg := *rawCfg
	if rawCfg.CaptionMode != "off" && rawCfg.Language == "auto" {
		return mcplib.NewToolResultError("captions require an explicit language"), nil
	}
	if err := validatePublicURL(ctx, rawURL); err != nil {
		return mcplib.NewToolResultError("unsafe or invalid URL: " + err.Error()), nil
	}

	root := os.Getenv("VIDSCRIBE_OUTPUT_ROOT")
	if root == "" {
		root = "./transcripts"
	}
	out, err := containedPath(root, stringArg(args, "output_dir", "."))
	if err != nil {
		return mcplib.NewToolResultError("invalid output_dir: " + err.Error()), nil
	}
	localCfg.OutputDir = out
	remoteCfg := *rawCfg
	remoteCfg.OutputDir = out
	remoteCfg.Formats = formatsWithManifest(rawCfg.Formats)

	select {
	case transcribeSlot <- struct{}{}:
		defer func() { <-transcribeSlot }()
	case <-ctx.Done():
		return mcplib.NewToolResultError("transcription cancelled while waiting for worker"), nil
	}
	runtimeLimit := 2 * time.Hour
	if raw := os.Getenv("VIDSCRIBE_MAX_RUNTIME"); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed <= 0 {
			return mcplib.NewToolResultError("invalid VIDSCRIBE_MAX_RUNTIME"), nil
		}
		runtimeLimit = parsed
	}
	runCtx, cancel := context.WithTimeout(ctx, runtimeLimit)
	defer cancel()
	var token any
	if req.Params.Meta != nil {
		token = req.Params.Meta.ProgressToken
	}
	localCfg.Progress = func(event pipeline.ProgressEvent) {
		if token == nil {
			return
		}
		if srv := server.ServerFromContext(runCtx); srv != nil {
			_ = srv.SendNotificationToClient(runCtx, "notifications/progress", map[string]any{
				"progressToken": token, "progress": event.Step, "total": event.Total,
				"message": event.Stage + ": " + event.Message,
			})
		}
	}

	var logBuf strings.Builder
	var runResult *pipeline.RunResult
	var remoteClient *remote.Client
	if !routesLocally(rawURL) {
		remoteClient, err = remote.NewFromEnvironment()
		if err != nil {
			return mcplib.NewToolResultError("invalid remote configuration: " + err.Error()), nil
		}
	}
	if remoteClient != nil {
		runResult, err = remoteClient.Run(runCtx, &remoteCfg)
		if err != nil {
			reason, allowed := remote.FallbackReason(err)
			if !allowed {
				return mcplib.NewToolResultError("Remote transcription failed without local fallback: " + err.Error()), nil
			}
			localCfg.Backend = "local"
			localCfg.BackendFallbackReason = reason
		}
	}
	if runResult == nil {
		if localCfg.CookiesFile != "" {
			if info, statErr := os.Stat(localCfg.CookiesFile); statErr != nil || !info.Mode().IsRegular() {
				return mcplib.NewToolResultError("cookies_file not found or not a regular file: " + localCfg.CookiesFile), nil
			}
		}
		if err := localCfg.Normalize(runCtx); err != nil {
			return mcplib.NewToolResultError("invalid configuration: " + err.Error()), nil
		}
		if err := deps.Check(localCfg.Engine); err != nil {
			return mcplib.NewToolResultError(err.Error()), nil
		}
		localCfg.Backend = "local"
		runResult, err = pipeline.RunDetailed(runCtx, &localCfg, &logBuf)
	}
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Transcription failed: %v\n\nLog:\n%s", err, logBuf.String())), nil
	}
	resultText := fmt.Sprintf("Transcription complete.\nBackend: %s\nEngine: %s (requested: %s)\nLanguage: %s\nDegraded: %t\n\nFiles written:\n%s",
		runResult.Execution.Backend,
		runResult.Execution.ActualEngine, runResult.Execution.RequestedEngine,
		runResult.Execution.DetectedLanguage, runResult.Execution.Degraded, pipeline.FormatReport(runResult.Paths))
	if preview := transcriptPreview(runResult.Paths); preview != "" {
		resultText += "\nPreview:\n" + preview + "\n"
	}
	if log := strings.TrimSpace(logBuf.String()); log != "" {
		resultText += "\nLog:\n" + log
	}
	toolResult := mcplib.NewToolResultText(resultText)
	toolResult.StructuredContent = runResult
	return toolResult, nil
}

func formatsWithManifest(formats []string) []string {
	result := append([]string{}, formats...)
	for _, format := range result {
		if strings.EqualFold(strings.TrimSpace(format), "manifest") {
			return result
		}
	}
	return append(result, "manifest")
}

func routesLocally(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "youtube.com" || host == "www.youtube.com" || host == "m.youtube.com" || host == "youtu.be" || strings.HasSuffix(host, ".youtube.com")
}

func checkDependenciesTool() mcplib.Tool {
	return mcplib.NewTool("check_dependencies", mcplib.WithDescription("Check the pinned vidscribe runtime toolchain."),
		mcplib.WithString("engine", mcplib.Description("faster | openai | parakeet (default: faster)")))
}

func handleCheckDependencies(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	engine := stringArg(req.GetArguments(), "engine", "faster")
	if engine != "faster" && engine != "openai" && engine != "parakeet" {
		return mcplib.NewToolResultError("unsupported engine: " + engine), nil
	}
	statuses := deps.Report(engine)
	var sb strings.Builder
	allOK := true
	for _, status := range statuses {
		if status.OK {
			sb.WriteString("OK ")
		} else {
			sb.WriteString("ERROR ")
			allOK = false
		}
		sb.WriteString(status.Name)
		if status.Version != "" {
			sb.WriteString(": " + status.Version)
		}
		if status.Note != "" {
			sb.WriteString(" (" + status.Note + ")")
		}
		sb.WriteByte('\n')
	}
	if allOK {
		sb.WriteString("\nAll dependencies OK.")
	} else {
		sb.WriteString("\nSome dependencies are missing or incompatible.")
	}
	return mcplib.NewToolResultText(sb.String()), nil
}

func listSupportedSitesTool() mcplib.Tool {
	return mcplib.NewTool("list_supported_sites", mcplib.WithDescription("List yt-dlp extractors from the pinned runtime."))
}

func handleListSupportedSites(ctx context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	out, err := pipeline.YtdlpCmd(ctx, "--list-extractors").Output()
	if err != nil {
		return mcplib.NewToolResultError("could not list extractors: " + err.Error()), nil
	}
	return mcplib.NewToolResultText(string(out)), nil
}

func stringArg(args map[string]any, key, def string) string {
	if value, ok := args[key].(string); ok && value != "" {
		return value
	}
	return def
}
func boolArg(args map[string]any, key string, def bool) bool {
	if value, ok := args[key].(bool); ok {
		return value
	}
	return def
}
func numberArg(args map[string]any, key string, def int) float64 {
	if value, ok := args[key].(float64); ok {
		return value
	}
	return float64(def)
}
func transcriptPreview(paths []string) string {
	for _, path := range paths {
		if filepath.Ext(path) != ".txt" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		words := strings.Fields(string(data))
		if len(words) > 80 {
			words = words[:80]
		}
		return strings.Join(words, " ")
	}
	return ""
}
