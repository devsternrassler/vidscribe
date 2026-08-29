# vidscribe

Reproducible media transcription with pinned `yt-dlp`, Whisper/Parakeet runtimes,
quality profiles, provenance manifests, a CLI, local MCP server, and a durable
HTTP job service for workflow automation.

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
        "VIDSCRIBE_MAX_RUNTIME": "2h",
        "VIDSCRIBE_REMOTE_URL": "http://127.0.0.1:18083",
        "VIDSCRIBE_REMOTE_TOKEN_FILE": "/absolute/path/to/remote-token"
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

When both remote variables are configured, eligible MCP jobs run remote-first
through a private SSH tunnel. `VIDSCRIBE_REMOTE_URL` deliberately accepts plain
HTTP only on loopback; the token file must be absolute, regular, non-symlinked,
at least 32 characters long, and mode `0600` or stricter. Cookies are never sent
to the service. YouTube targets are routed directly to the local pipeline because
datacenter extraction is unreliable. Transport failures, service `5xx` responses,
and failed remote jobs visibly fall back to the local pipeline. Authentication,
validation, security, protocol, cancellation, and local-output errors fail loud
without fallback. The result and provenance manifest expose the actual backend
and any fallback reason.

## HTTP job service

`vidscribe serve` exposes the same tested pipeline as an asynchronous,
single-worker HTTP service. Jobs and artifacts are persisted atomically, running
jobs are re-queued after a restart, and identical requests reuse their completed
result instead of transcribing twice.

```bash
VIDSCRIBE_API_TOKEN=local-test \
  vidscribe serve --listen 127.0.0.1:8080 --data-dir ./vidscribe-data
```

Submit a direct podcast enclosure or audio URL:

```bash
curl -sS http://127.0.0.1:8080/v1/jobs \
  -H 'Authorization: Bearer local-test' \
  -H 'Content-Type: application/json' \
  -d '{
    "source_type": "podcast",
    "source_url": "https://example.com/episode.mp3",
    "canonical_id": "feed-guid-or-stable-id",
    "title": "Episode title",
    "language": "en",
    "profile": "balanced"
  }'
```

The response contains a deterministic job ID. Poll and fetch its artifacts:

```text
GET /v1/jobs/{id}
GET /v1/jobs/{id}/transcript
GET /v1/jobs/{id}/manifest
GET /v1/jobs/{id}/artifacts/{txt|md|json|srt|vtt|manifest}
GET /healthz
GET /readyz
GET /metrics
```

`source_type=video` retains the captions-first/yt-dlp path.
`source_type=podcast` and `source_type=audio` download the complete direct media
response, validate each HTTP redirect, enforce the configured byte limit, probe
the real duration with FFprobe, and then pass the intact file to the common ASR
pipeline. Arbitrary byte-range truncation is never used.

The service defaults to loopback. Listening on another interface requires
`VIDSCRIBE_API_TOKEN` or `VIDSCRIBE_MCP_API_TOKEN`; both tokens are accepted
independently so n8n and MCP credentials can be rotated separately. Health and
Prometheus metrics remain unauthenticated for
container orchestration. Media URLs must resolve exclusively to public IPs.

### Local container

```bash
export VIDSCRIBE_API_TOKEN=local-test
docker compose -f compose.local.yaml up --build -d
curl -fsS http://127.0.0.1:18082/healthz
```

The container runs without root privileges, drops all Linux capabilities, uses
a read-only root filesystem, stores jobs/model caches in `/data`, and confines
temporary media to a size-limited `/tmp`. The local compose file publishes the
API on loopback only and budgets 4 vCPU plus 8 GB RAM for long recordings.
Production deployment is intentionally separate.

### Production container

Tagged releases publish `ghcr.io/sternrassler/vidscribe:vX.Y.Z`. Production
uses [`compose.prod.yaml`](compose.prod.yaml), pins that immutable release tag,
and requires an explicit bind address and API token:

```bash
VIDSCRIBE_IMAGE=ghcr.io/sternrassler/vidscribe:v0.4.0 \
VIDSCRIBE_BIND_ADDRESS=10.20.0.3 \
VIDSCRIBE_API_TOKEN='replace-me' \
VIDSCRIBE_MCP_API_TOKEN='replace-with-a-distinct-token' \
  docker compose -f compose.prod.yaml config
```

The production API should bind only to a private network address. The reference
container is capped at 4 vCPU and 8 GB RAM, persists jobs and artifacts in a
named volume, and survives host or container restarts.

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
supported platform whose selected GPU has at least 1 GiB of free VRAM;
otherwise it chooses CPU and a compatible compute type. An explicit
`device=cuda` remains fail-loud and bypasses this automatic readiness check.
Cross-compilation proves the launcher builds, not that every Python runtime
wheel exists.

For MCP calls, a relative `output_dir` is resolved below
`VIDSCRIBE_OUTPUT_ROOT`; an absolute path is accepted only when it is already
contained below that root. Traversal and symlink escapes are rejected.

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
make test-service-e2e # full HTTP path; requires VIDSCRIBE_SERVICE_TEST_AUDIO_URL
make test-quality  # transcribe the public gold smoke and evaluate it
```

CI runs unit, vet, vulnerability, and cross-build gates on every change. The
scheduled integration workflow runs dependency smoke plus real faster-CPU,
Parakeet, and captions E2E checks. Release publication repeats unit, vet, and
vulnerability gates before GoReleaser.
