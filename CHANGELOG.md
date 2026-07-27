# Changelog

Alle wesentlichen Änderungen an vidscribe werden in dieser Datei dokumentiert.

## [Unreleased]

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

[Unreleased]: https://github.com/Sternrassler/vidscribe/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/Sternrassler/vidscribe/releases/tag/v0.2.1
