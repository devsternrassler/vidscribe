# vidscribe

Reproducible video transcription with pinned `yt-dlp`, Whisper/Parakeet runtimes,
quality profiles, provenance manifests, and a CLI plus local MCP server.

## Runtime requirements

- the released `vidscribe` Go launcher
- [uv](https://docs.astral.sh/uv/) / `uvx`
- `ffmpeg`
- Node.js or Deno for current YouTube extraction

The Go launcher is a static binary; transcription still downloads pinned Python
packages and model weights on demand. Package pins live in
`internal/runtimeenv/packages.go` and are updated only after integration tests.

## Quick start

```bash
# Balanced default: medium/CUDA when CUDA is usable, otherwise Parakeet/CPU
vidscribe "https://youtube.com/watch?v=XYZ"

# Explicit profiles
vidscribe URL --profile quality
vidscribe URL --profile gpu-free
vidscribe URL --profile fast

# Manual low-level configuration
vidscribe URL --profile custom --engine faster --model large-v3 --device cuda

# Prefer human captions for a known language; fail if they do not exist
vidscribe URL --captions manual --language de

# Permit a visible, manifest-recorded fallback to ASR or another engine
vidscribe URL --captions auto --language de --allow-fallback
```

Every successful run writes the selected transcript formats plus
`<title> [<video-id>].manifest.json`. The manifest records requested and actual
engine, quality profile, model, device, compute type, detected language, pinned
runtime versions, confidence when exposed by the engine, wall-clock and realtime
factor, and every degraded fallback reason.

## Quality profiles

| Profile | CUDA host | CPU-only host | Intent |
|---|---|---|---|
| `quality` | faster / large-v3 / float16 | faster / large-v3 / int8 | best Whisper quality |
| `balanced` (default) | faster / medium / float16 | Parakeet / CPU | quality, speed, robustness |
| `gpu-free` | Parakeet / CPU | Parakeet / CPU | leave GPU free |
| `fast` | faster / small | faster / small | minimum latency |
| `custom` | explicit flags or legacy faster/small defaults | same | full control |

Explicit `--engine`, `--model`, `--device`, and `--compute-type` values override
the selected profile. Invalid values and unsupported CPU `float16` combinations
fail before downloading anything. Automatic engine fallback is off by default.

## CLI flags

| Flag | Default | Description |
|---|---|---|
| `--profile` | `balanced` | `quality`, `balanced`, `gpu-free`, `fast`, `custom` |
| `--engine` | profile | `faster`, `openai`, `parakeet` |
| `--model` | profile | Whisper model override |
| `--language` | `auto` | ISO language code or automatic detection |
| `--device` | profile | `auto`, `cpu`, `cuda` |
| `--compute-type` | hardware | `int8`, `int8_float16`, `float16`, `float32` |
| `--captions` | `off` | `manual` or `auto`, requires explicit language |
| `--allow-fallback` | false | allow and visibly record degraded fallback |
| `--format` | `txt,md` | `txt,md,json,srt,vtt`; manifest is mandatory |
| `--output-dir` | `./transcripts` | output directory |
| `--max-duration` | `14400` | maximum media duration in seconds |
| `--max-filesize` | `2G` | yt-dlp media size limit |
| `--overwrite` | false | explicitly replace an existing output set |
| `--word-timestamps` | false | include engine word timing data in JSON |
| `--cookies-browser` | none | supported browser cookie source |
| `--cookies-file` | none | Netscape cookie file |
| `--js-runtime` | auto | explicit `node:/path` or `deno:/path` |
| `--mcp` | false | start the stdio MCP server |

## MCP server

```json
{
  "mcpServers": {
    "vidscribe": {
      "command": "vidscribe",
      "args": ["--mcp"],
      "env": {
        "VIDSCRIBE_OUTPUT_ROOT": "/absolute/path/to/transcripts",
        "VIDSCRIBE_MAX_RUNTIME": "2h"
      }
    }
  }
}
```

Tools:

- `transcribe_video`
- `check_dependencies`
- `list_supported_sites`

The MCP server accepts public HTTP(S) targets only, resolves host addresses
before execution, rejects localhost/private/link-local destinations, serializes
resource-heavy jobs, enforces a configurable timeout, and confines output below
`VIDSCRIBE_OUTPUT_ROOT` (default `./transcripts`). It emits native MCP progress
notifications when the client supplies a progress token. Cancellation propagates
to yt-dlp, ffmpeg, uvx, and transcription subprocesses; Unix builds terminate the
complete process group, while Windows uses the native direct-process cancellation.

MCP startup has no installer side effects and does not write Claude command files.

## Audio and captions

The ASR path downloads the best native audio stream without an intermediate MP3
encode. Parakeet alone converts its input once to lossless 16 kHz mono WAV.

`--captions manual` uses publisher captions. `--captions auto` accepts publisher
or automatic captions. Both require an explicit language and preserve cue
timestamps. If captions are absent, the run fails unless `--allow-fallback` is
set; that transition is marked degraded in the manifest.

## Platform status

| Platform | Status | Notes |
|---|---|---|
| Linux x86_64 | verified | CPU and NVIDIA CUDA E2E |
| Linux arm64 | build-tested | CPU engines require compatible Python wheels |
| macOS x86_64/arm64 | build-tested | CPU only; no NVIDIA package injection |
| Windows x86_64 | build-tested | CPU; CUDA path requires native compatible runtime |
| Windows arm64 | partial | Go launcher works; ASR wheel availability is limited |

`device=auto` executes a real `nvidia-smi` probe. It chooses CUDA only on a
supported platform with a usable GPU; otherwise it chooses CPU and a compatible
compute type. Cross-compilation proves the launcher builds, not that every Python
runtime wheel exists.

## Quality evaluation

The versioned corpus schema is in `quality/corpus.json`. Evaluate generated
`<case-id>.txt` hypotheses with:

```bash
vidscribe quality-eval --corpus quality/corpus.json --hypotheses quality-results
```

The JSON report includes word error rate (WER), character error rate (CER), and
domain-keyword recall. See `quality/README.md` for the required corpus categories
before changing defaults.

Real benchmark from 2026-07-12 on one 21.4 minute German video (cached models,
Ryzen 7 5800X, RTX 5060 8 GB):

| Mode | Time | Quality observation |
|---|---:|---|
| whisper small CUDA fp16 | 33.5 s | lowest in this sample |
| whisper medium CUDA fp16 | 64 s | good |
| Parakeet CPU with VAD | 82 s | between medium and large-v3 |
| whisper large-v3 CUDA fp16 | 100 s | best in this sample |
| whisper small CPU int8 | 281 s | lowest in this sample |

This is a performance observation, not a cross-model accuracy proof; default
changes require the checked-in evaluator and a representative local corpus.

## Testing

```bash
make test          # unit tests
make vet
make vuln          # reachable-vulnerability gate
make test-smoke    # pinned dependency and MCP protocol smoke
make test-e2e      # real network/engine matrix
make test-quality  # transcribe the public gold smoke and evaluate it
```

CI runs unit, vet, vulnerability, and cross-build gates on every change. The
scheduled integration workflow runs dependency smoke plus real faster-CPU,
Parakeet, and captions E2E checks. Release publication repeats unit, vet, and
vulnerability gates before GoReleaser.
