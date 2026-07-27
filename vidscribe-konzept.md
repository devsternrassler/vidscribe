# vidscribe – aktuelle Architektur

## Datenfluss

1. CLI oder MCP erzeugt eine strikt validierte `pipeline.Config`.
2. Das Qualitätsprofil wird anhand der real verfügbaren Hardware aufgelöst.
3. Der MCP prüft URL, Output-Root, Dauer, Größe, Laufzeit und Worker-Slot.
4. Captions-first lädt auf Wunsch Untertitel; andernfalls lädt yt-dlp das beste
   native Audioformat ohne MP3-Zwischenencode.
5. Die gewählte Engine transkribiert. Fallbacks sind opt-in und werden als
   `FallbackEvent` sichtbar protokolliert.
6. Die Pipeline schreibt die angeforderten Formate atomar und ergänzt immer ein
   `vidscribe-manifest/v1` mit Video-, Laufzeit- und Provenienzdaten.

## Source of truth

- Profile, Validierung und Limits: `internal/pipeline/config.go`
- Runtime-Pins: `internal/runtimeenv/packages.go`
- Download/Captions: `internal/pipeline/download.go`
- Engine/Fallbacks: `internal/pipeline/transcribe.go`
- Outputs/Manifest: `internal/pipeline/format.go`
- MCP-Sicherheitsgrenzen: `internal/mcp/security.go`, `internal/mcp/server.go`
- Qualitätsmetrik: `internal/quality/evaluate.go`, `quality/corpus.json`

Historische Default- und MP3-Entwürfe sind nicht mehr maßgeblich; Code, README
und CHANGELOG bilden den aktuellen Vertrag.
