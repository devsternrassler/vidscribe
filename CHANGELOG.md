# Changelog

Alle wesentlichen Änderungen an vidscribe werden in dieser Datei dokumentiert.

## [Unreleased]

### Added

- Dauerhafter HTTP-Jobdienst (`vidscribe serve`) mit Bearer-Authentifizierung,
  atomarer Dateipersistenz, Restart-Recovery, idempotentem Ergebnis-Cache,
  Fortschritt, Health-/Readiness-Endpunkten und Prometheus-Metriken.
- Direkter Podcast-/Audio-Adapter, der vollständige Medien lädt, Redirect-Ziele
  gegen SSRF prüft, Größenlimits erzwingt und die Dauer mit FFprobe validiert.
- Gehärtetes, nicht privilegiertes Container-Image und lokaler Compose-Stack.
- Versioniertes GHCR-Image und Produktions-Compose für einen privaten
  Vidscribe-Worker mit 4 vCPU und 8 GB RAM.
- Separater Langform-Service-E2E für reale Podcast-Regressionsdateien.

### Changed

- Die öffentliche URL-Prüfung ist nun ein gemeinsamer Baustein für MCP, HTTP-API
  und direkte Medien-Downloads; Redirects werden bei jedem Hop neu validiert.

## [0.3.0] - 2026-07-27

### Added

- Qualitätsprofile (`quality`, `balanced`, `gpu-free`, `fast`, `custom`) und
  ein versioniertes WER/CER-/Fachbegriffs-Goldset-Format.
- Provenienzmanifest mit tatsächlicher Engine, Modell, Device, Sprache,
  Abhängigkeiten, Laufzeit und sichtbaren Fallbackgründen.
- MCP-Progress, Laufzeit-/Größen-/Dauerlimits, öffentlicher URL-Check,
  Output-Root und serialisierte Transkriptionsjobs.
- Expliziter Captions-first-Modus für menschliche und automatische Untertitel.

### Changed

- Runtime-Pakete sind zentral auf die getesteten Versionen festgelegt.
- Audio wird im besten nativen Format geladen und erst enginespezifisch
  verlustfrei normalisiert.
- `balanced` ist der neue CLI-/MCP-Standard; auf Systemen ohne CUDA wird
  Parakeet verwendet.
- Ausgaben werden atomar und mit Video-ID im Dateinamen geschrieben.

### Security

- Go-Patchlevel auf 1.26.5 angehoben und `govulncheck` blockierend in CI und
  Release-Preflight aufgenommen.
- Private/localhost MCP-Ziele und Ausgabepfade außerhalb des konfigurierten
  Roots werden abgelehnt.

### Removed

- Implizite GitHub-EJS-Downloads und das stille Überschreiben von
  `~/.claude/commands` beim MCP-Start.

## [0.2.1] - 2026-07-12

### Added

- Parakeet-v3-Engine über onnx-asr mit Silero-VAD und CPU-only-Langformpfad.

### Fixed

- GoReleaser-Owner nach dem Repository-Umzug korrigiert.

[Unreleased]: https://github.com/Sternrassler/vidscribe/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/Sternrassler/vidscribe/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/Sternrassler/vidscribe/releases/tag/v0.2.1
