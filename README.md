# vidscribe

Video transcription via **yt-dlp** + **whisper-ctranslate2** (faster-whisper engine) — as CLI or MCP server for Claude.

## Requirements

- [uv](https://docs.astral.sh/uv/) (provides `uvx` — no separate yt-dlp or whisper install needed)
- `ffmpeg`

## Usage

```bash
# Transcribe a YouTube video
vidscribe "https://youtube.com/watch?v=XYZ"

# With browser cookie auth (for age-restricted / private videos)
vidscribe "https://youtube.com/watch?v=XYZ" --cookies-browser chrome

# Larger model, German language, all output formats
vidscribe "https://youtube.com/watch?v=XYZ" \
  --model medium \
  --language de \
  --format txt,md,json,srt,vtt

# Check dependencies
vidscribe --mcp  # then call check_dependencies via MCP
```

## MCP server (Claude integration)

Add to `~/.claude.json`:

```json
{
  "mcpServers": {
    "vidscribe": {
      "command": "vidscribe",
      "args": ["--mcp"]
    }
  }
}
```

**Available MCP tools:**

| Tool | Description |
|------|-------------|
| `transcribe_video` | Download + transcribe a video URL |
| `check_dependencies` | Verify uvx, ffmpeg, yt-dlp, whisper-ctranslate2 |
| `list_supported_sites` | List all 1000+ yt-dlp platforms |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--model` | `small` | Whisper model: tiny\|base\|small\|medium\|large |
| `--language` | `auto` | ISO 639-1 language code or `auto` |
| `--output-dir` | `./transcripts` | Output directory |
| `--cookies-browser` | — | Browser for cookie auth: chrome\|firefox\|safari\|edge |
| `--cookies-file` | — | Netscape cookie file (fallback if secretstorage unavailable) |
| `--format` | `txt,md` | Output formats: txt, md, json, srt, vtt |
| `--engine` | `faster` | Whisper engine: `faster` or `openai` |
| `--mcp` | — | Start as MCP server (stdio) |
| `--verbose` | — | Verbose output |
