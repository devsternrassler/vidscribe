package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sternrassler/vidscribe/internal/deps"
	"github.com/sternrassler/vidscribe/internal/mcp"
	"github.com/sternrassler/vidscribe/internal/pipeline"
	"github.com/sternrassler/vidscribe/internal/quality"
)

var (
	model          string
	language       string
	outputDir      string
	cookiesBrowser string
	cookiesFile    string
	jsRuntime      string
	format         string
	engine         string
	device         string
	computeType    string
	profile        string
	captionMode    string
	allowFallback  bool
	maxDuration    int
	maxFileSize    string
	overwrite      bool
	wordTimestamps bool
	mcpMode        bool
	verbose        bool
)

var rootCmd = &cobra.Command{
	Use:   "vidscribe [URL]",
	Short: "Transcribe audio from YouTube and 1000+ platforms",
	Long: `vidscribe downloads audio from any yt-dlp-supported platform and
transcribes it using faster-whisper (or openai-whisper as fallback).

Run 'vidscribe --mcp' to start in MCP server mode for Claude integration.`,
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          run,
}

// SetVersion injects build-time version info from goreleaser ldflags.
func SetVersion(ver, commit, date string) {
	rootCmd.Version = ver + " (" + commit + ", " + date + ")"
	mcp.SetVersion(ver)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVar(&profile, "profile", pipeline.DefaultProfile, "Quality profile: quality|balanced|gpu-free|fast|custom")
	rootCmd.Flags().StringVar(&model, "model", "", "Whisper model override: tiny|base|small|medium|large-v3|turbo")
	rootCmd.Flags().StringVar(&language, "language", "auto", "Language (ISO 639-1) or 'auto'")
	rootCmd.Flags().StringVar(&outputDir, "output-dir", "./transcripts", "Output directory")
	rootCmd.Flags().StringVar(&cookiesBrowser, "cookies-browser", "", "Browser for cookie auth: chrome|firefox|safari|edge")
	rootCmd.Flags().StringVar(&cookiesFile, "cookies-file", "", "Path to Netscape cookie file")
	rootCmd.Flags().StringVar(&jsRuntime, "js-runtime", "", "JS runtime for yt-dlp extractor args: deno|node")
	rootCmd.Flags().StringVar(&format, "format", "txt,md", "Output formats: txt,md,json,srt,vtt (comma-separated)")
	rootCmd.Flags().StringVar(&engine, "engine", "", "Engine override: faster|openai|parakeet")
	rootCmd.Flags().StringVar(&device, "device", "", "Compute device override: cpu|cuda|auto")
	rootCmd.Flags().StringVar(&computeType, "compute-type", "", "Compute type override: int8|int8_float16|float16|float32")
	rootCmd.Flags().StringVar(&captionMode, "captions", "off", "Caption source: off|manual|auto (requires explicit language)")
	rootCmd.Flags().BoolVar(&allowFallback, "allow-fallback", false, "Allow visible degraded engine fallback")
	rootCmd.Flags().IntVar(&maxDuration, "max-duration", pipeline.DefaultMaxDuration, "Maximum video duration in seconds")
	rootCmd.Flags().StringVar(&maxFileSize, "max-filesize", pipeline.DefaultMaxFileSize, "Maximum media download size")
	rootCmd.Flags().BoolVar(&overwrite, "overwrite", false, "Explicitly replace existing output files")
	rootCmd.Flags().BoolVar(&wordTimestamps, "word-timestamps", false, "Include engine word timestamps in JSON output")
	rootCmd.Flags().BoolVar(&mcpMode, "mcp", false, "Start as MCP server (stdio)")
	rootCmd.Flags().BoolVar(&verbose, "verbose", false, "Verbose output")
	rootCmd.AddCommand(newQualityEvalCommand())
}

func newQualityEvalCommand() *cobra.Command {
	var corpusPath, hypothesesDir string
	command := &cobra.Command{
		Use: "quality-eval", Short: "Evaluate transcript hypotheses against the versioned gold corpus",
		RunE: func(_ *cobra.Command, _ []string) error {
			report, err := quality.EvaluateFile(corpusPath, hypothesesDir)
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			if !report.Passed {
				return fmt.Errorf("quality thresholds failed: %s", strings.Join(report.Violations, "; "))
			}
			return nil
		},
	}
	command.Flags().StringVar(&corpusPath, "corpus", "quality/corpus.json", "Gold corpus JSON")
	command.Flags().StringVar(&hypothesesDir, "hypotheses", "quality-results", "Directory containing <case-id>.txt")
	return command
}

func run(cmd *cobra.Command, args []string) error {
	if mcpMode {
		return mcp.Serve()
	}

	if len(args) == 0 {
		return fmt.Errorf("URL required\n\nUsage: vidscribe [URL] [flags]\nRun 'vidscribe --mcp' for MCP server mode")
	}

	url := args[0]

	cfg := &pipeline.Config{
		URL:            url,
		Profile:        profile,
		Model:          model,
		Language:       language,
		OutputDir:      outputDir,
		CookiesBrowser: cookiesBrowser,
		CookiesFile:    cookiesFile,
		JSRuntime:      jsRuntime,
		Formats:        strings.Split(format, ","),
		Engine:         engine,
		Device:         device,
		ComputeType:    computeType,
		CaptionMode:    captionMode,
		AllowFallback:  allowFallback,
		MaxDuration:    maxDuration,
		MaxFileSize:    maxFileSize,
		Overwrite:      overwrite,
		WordTimestamps: wordTimestamps,
		Verbose:        verbose,
	}
	if engine != "" {
		cfg.RequestedEngine = engine
	} else {
		cfg.RequestedEngine = "profile:" + profile
	}
	if err := cfg.Normalize(context.Background()); err != nil {
		return err
	}
	if captionMode != "off" && language == "auto" {
		return fmt.Errorf("captions require an explicit language")
	}
	if err := deps.Check(cfg.Engine); err != nil {
		return err
	}

	result, err := pipeline.RunDetailed(context.Background(), cfg, os.Stderr)
	if err != nil {
		return err
	}

	fmt.Printf("Transcription complete. Engine: %s; language: %s; degraded: %t\nFiles written:\n",
		result.Execution.ActualEngine, result.Execution.DetectedLanguage, result.Execution.Degraded)
	for _, p := range result.Paths {
		fmt.Println(" ", p)
	}
	return nil
}
