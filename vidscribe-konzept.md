# vidscribe — Konzept

**Abgeleitet von:** nhatvu148/video-transcriber-mcp (TypeScript)
**Ziel:** Go-CLI-Tool für vollständige Audio-Transkription von YouTube und anderen Plattformen —
mit Browser-Cookie-Auth, aktuellem yt-dlp via uvx, faster-whisper und MCP-Server-Modus.

---

## 1. Projektidee

Die manuelle CLI-Kette `yt-dlp | whisper` funktioniert zuverlässig, ist aber ein
Zwei-Schritt-Prozess ohne direkte Claude-Integration. Der TypeScript-MCP-Server
`nhatvu148/video-transcriber-mcp` wäre die natürliche Lösung — hat aber kritische Lücken,
die ihn für YouTube-Downloads in der Praxis unbrauchbar machen.

**vidscribe** ist ein schlankes Go-Binary das:
- Die bewährte CLI-Kette kapselt (yt-dlp + ffmpeg + whisper als Subprozesse)
- Browser-Cookie-Auth eingebaut hat
- Als standalone CLI nutzbar ist
- Als MCP-Server läuft (für direkte Claude-Integration)

Repository: eigenes GitHub-Repo (nicht im claude-app-Monorepo)

---

## 2. Lücken der Referenzimplementierung

| Problem | Referenz (TS) | vidscribe |
|---------|--------------|-----------|
| Browser-Cookie-Auth | ❌ fehlt → HTTP 403 | ✅ `--cookies-from-browser chrome\|firefox` |
| yt-dlp-Version | ❌ System-yt-dlp (veraltet) | ✅ `uvx yt-dlp` (immer aktuell) |
| secretstorage-Fehler | ❌ nicht behandelt | ✅ Auto-Fallback auf Cookie-Datei |
| Retry bei 429/403 | ⚠️ nur Netzwerkfehler | ✅ auch HTTP 429/403 mit Backoff |
| Whisper-Geschwindigkeit | ❌ openai-whisper (langsam) | ✅ faster-whisper (bis 4x schneller) |
| Whisper-Binary | ❌ global installiert | ✅ `uvx` (kein Install nötig) |
| JS-Runtime-Warnung | ❌ ignoriert | ✅ optional `--js-runtime deno\|node` |

---

## 3. openai-whisper vs. faster-whisper

| Eigenschaft | openai-whisper | faster-whisper |
|------------|---------------|----------------|
| Engine | PyTorch | CTranslate2 |
| Geschwindigkeit | Basis (1x) | bis 4x schneller |
| RAM-Verbrauch | höher | deutlich geringer |
| Wort-Timestamps | nein | ja |
| Quantisierung (int8) | nein | ja (noch schneller auf CPU) |
| CLI-Befehl | `whisper` | `whisper-ctranslate2` |
| uvx-Aufruf | `uvx --from openai-whisper whisper` | `uvx --from whisper-ctranslate2 whisper-ctranslate2 --device cpu --compute_type int8` |

**Entscheidung:** faster-whisper als Standard, openai-whisper als Fallback-Option (`--engine openai`).

---

## 4. Architektur

```
vidscribe [URL] [flags]
        │
        ├─► yt-dlp (via uvx) — Audio-Download
        │     └─ --cookies-from-browser chrome
        │     └─ --audio-format mp3
        │     └─ Retry bei 403/429 mit Backoff
        │
        ├─► ffmpeg — Format-Konvertierung (falls nötig)
        │
        └─► faster-whisper (via uvx) — Transkription
              └─ --model small
              └─ --language de
              └─ --output_dir ./transcripts
```

**MCP-Server-Modus (`vidscribe --mcp`):**

```
stdio JSON-RPC (mark3labs/mcp-go)
  └─ Tool: transcribe_video(url, model, language, cookies_browser, cookies_file, engine)
  └─ Tool: check_dependencies()
  └─ Tool: list_supported_sites()
```

**MCP-Library:** `mark3labs/mcp-go` (8.5k Stars, aktiv maintained, vollständige MCP-Spec)

---

## 5. CLI-Interface

```
vidscribe [URL] [flags]

Flags:
  --model           string   Whisper-Modell: tiny|base|small|medium|large (default: small)
  --language        string   Sprache (ISO 639-1) oder "auto" (default: auto)
  --output-dir      string   Ausgabeverzeichnis (default: ./transcripts)
  --cookies-browser string   Browser für Cookie-Auth: chrome|firefox|safari|edge
  --cookies-file    string   Pfad zu Netscape-Cookie-Datei (Fallback wenn secretstorage fehlt)
  --js-runtime      string   JS-Runtime für yt-dlp: deno|node (optional)
  --format          string   Ausgabeformate: txt,json,md,srt,vtt (default: txt,md)
  --engine          string   Whisper-Engine: faster|openai (default: faster)
  --mcp             bool     Als MCP-Server starten (stdio)
  --verbose         bool     Ausführliche Ausgabe
  --version                  Version anzeigen

Beispiele:
  vidscribe "https://youtube.com/watch?v=XYZ" --cookies-browser chrome
  vidscribe "https://youtube.com/watch?v=XYZ" --model medium --language de
  vidscribe --mcp
```

---

## 6. Ausgabeformate

| Format | Inhalt |
|--------|--------|
| `.txt` | Reines Transkript |
| `.md` | Transkript + Metadaten (Titel, Kanal, Dauer, Datum) |
| `.json` | Strukturiert: Segmente mit Timestamps |
| `.srt` | Untertitel-Format für Videoplayer |
| `.vtt` | WebVTT-Format für Browser |

---

## 7. Abhängigkeiten

Alle als externe Subprozesse — kein eingebettetes Go-Whisper oder yt-dlp:

| Tool | Verwendung | Beschaffung |
|------|-----------|-------------|
| `yt-dlp` | Audio-Download | `uvx yt-dlp` (kein Install nötig) |
| `ffmpeg` | Audio-Konvertierung | `apt install ffmpeg` |
| `whisper-ctranslate2` | Transkription (faster-whisper-Engine) | `uvx --from whisper-ctranslate2 whisper-ctranslate2` |

`check_dependencies()` prüft Verfügbarkeit und gibt klare Fehlermeldungen.

---

## 8. Cookie-Fallback-Logik

```
--cookies-browser chrome angegeben?
  ├── Ja → yt-dlp --cookies-from-browser chrome
  │         └── secretstorage-Fehler? → warnen + Cookie-Datei-Export vorschlagen
  │             └── --cookies-file angegeben? → yt-dlp --cookies DATEI
  └── Nein → ohne Cookie-Auth versuchen
              └── HTTP 403/429? → Hinweis: --cookies-browser verwenden
```

---

## 9. Implementierungsplan

### Phase 1 — Projekt-Scaffolding
- GitHub-Repo anlegen: `vidscribe`
- Go-Modul initialisieren (`go mod init github.com/USER/vidscribe`)
- CLI via `cobra`, Dependency-Check

### Phase 2 — Download-Pipeline
- yt-dlp-Wrapper: alle Flags, uvx-Modus, Retry-Logik
- Cookie-Auth mit secretstorage-Fallback

### Phase 3 — Transkriptions-Pipeline
- faster-whisper-Wrapper, openai-whisper als Fallback
- Ausgabedatei-Parsing und Kopie in Output-Dir

### Phase 4 — Output-Generierung
- Markdown-Template, JSON-Strukturierung, SRT/VTT-Passthrough

### Phase 5 — MCP-Server-Modus
- `mark3labs/mcp-go` integrieren
- Tools: `transcribe_video`, `check_dependencies`, `list_supported_sites`
- Progress-Notifications via MCP

### Phase 6 — Release
- `goreleaser` für Cross-Compilation (Linux/macOS/Windows)
- GitHub Actions CI
- MCP-Konfigurationsbeispiel für `~/.claude.json`

---

## 10. Abgrenzung zur Referenzimplementierung

| Eigenschaft | nhatvu148/video-transcriber-mcp | vidscribe |
|------------|--------------------------------|-----------|
| Sprache | TypeScript/Node | Go |
| Distribution | npm / npx | Einzelnes Binary |
| Cookie-Auth | ❌ | ✅ |
| uvx-Support | ❌ | ✅ |
| faster-whisper | ❌ | ✅ (Standard) |
| Standalone CLI | ❌ (nur MCP) | ✅ |
| MCP-Server | ✅ | ✅ |
| Plattformen | 1000+ via yt-dlp | 1000+ via yt-dlp |
