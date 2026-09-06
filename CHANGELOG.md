# Changelog

Alle wesentlichen Änderungen an vidscribe werden in dieser Datei dokumentiert.

## [Unreleased]

## [0.7.0] - 2026-09-06

### Added

- Sichere, standardmäßig deaktivierte Retention für terminale HTTP-Jobs mit
  getrennten Fristen für erfolgreiche und fehlgeschlagene Jobs, Dry-run,
  Prometheus-Metriken und Lösch-Read-back für Metadaten plus Artefakte.

## [0.6.0] - 2026-09-02

### Added

- Opt-in-Allowlist `VIDSCRIBE_SERVICE_ALLOWED_ENGINES` fuer Remote-Dienste.
  Profile werden vor der Entscheidung auf ihre tatsaechliche Engine aufgeloest;
  nicht erlaubte neue und wiederaufgenommene Jobs starten weder Download noch
  Transkription.
- Dokumentierte Neubewertung der produktiven Transkriptions-Backends samt
  realem CX33-Vergleich als Grundlage fuer den Parakeet-only-Betrieb.

## [0.5.1] - 2026-08-29

### Fixed

- Der gepinnte yt-dlp-Runtime-Vertrag installiert nun auch das offizielle
  `curl-cffi`-Extra, damit Remote-Worker Seiten mit verpflichtender
  Browser-Impersonation (unter anderem Dailymotion) laden koennen.

## [0.5.0] - 2026-08-29

### Added

- Remote-first MCP-Ausfuehrung ueber einen privaten Loopback-SSH-Tunnel mit
  separater Token-Datei, sichtbarem lokalem Fallback und Backend-Provenienz.
- Formatparitaet fuer Remote-Auftraege und ein generischer, typisierter
  Artefakt-Endpunkt fuer `txt`, `md`, `json`, `srt`, `vtt` und Manifest.
- Separates `VIDSCRIBE_MCP_API_TOKEN`, das parallel zum bestehenden n8n-Token
  akzeptiert und unabhaengig rotiert werden kann.

### Security

- Remote-MCP akzeptiert nur HTTP-Loopback-Ziele und streng geschuetzte,
  nicht verlinkte Token-Dateien; Cookies verlassen den lokalen Rechner nie.
- Authentifizierungs-, Validierungs-, Sicherheits-, Protokoll-, Abbruch- und
  lokale Ausgabefehler werden ohne stillen lokalen Fallback gemeldet.

### Fixed

- MCP-Ausgabeunterverzeichnisse werden relativ zu `VIDSCRIBE_OUTPUT_ROOT`
  statt zum Prozess-Arbeitsverzeichnis aufgeloest; Traversal- und
  Symlink-Ausbrueche bleiben blockiert. Auch der vollstaendig unkonfigurierte
  Default schreibt genau nach `./transcripts` statt in ein doppeltes
  Unterverzeichnis.
- `device=auto` waehlt eine praktisch belegte NVIDIA-GPU mit weniger als 1 GiB
  freiem VRAM nicht mehr aus und vermeidet damit den reproduzierten CUDA-OOM;
  explizites `device=cuda` bleibt unveraendert fail-loud.
- Go-Build und Container auf 1.26.6 angehoben, um die vom blockierenden
  `govulncheck` als erreichbar gemeldeten Standardbibliotheksluecken zu
  schliessen.

## [0.4.0] - 2026-07-29

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

[Unreleased]: https://github.com/Sternrassler/vidscribe/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/Sternrassler/vidscribe/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/Sternrassler/vidscribe/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/Sternrassler/vidscribe/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/Sternrassler/vidscribe/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/Sternrassler/vidscribe/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/Sternrassler/vidscribe/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/Sternrassler/vidscribe/releases/tag/v0.2.1
