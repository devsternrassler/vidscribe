# Transkriptions-Backend-Re-Eval vom 31. August 2026

Status: Zwischenstand zu [Issue #7](https://github.com/Sternrassler/vidscribe/issues/7),
noch keine Freigabe fuer eine Produktionsaenderung.

## Ziel und Betriebsbezug

Die Messung bezieht sich auf den primaeren Vidscribe-HTTP-Backenddienst auf dem
dedizierten Hetzner-Host `vidscribe-worker` (CPX32, 4 vCPU, 8 GB RAM). Sie
bezieht sich ausdruecklich nicht auf den Host mit dem Namen `backend`.

Verglichen werden die im Release `v0.5.1` gepinnten lokalen Engines

- Parakeet TDT 0.6B v3 via `onnx-asr[cpu,hub]==0.12.0`, und
- Whisper Large-v3 via `whisper-ctranslate2==0.5.7`, CPU/int8.

OpenAI `gpt-transcribe` wurde als dritter Kandidat mit derselben OpenAI-
Credential getestet, die bereits in n8n eingerichtet ist. Der Schluessel ist
seit diesem Re-Eval als `OPENAI_API_KEY` in der rootgeschuetzten
Vidscribe-`.env` hinterlegt. Fuer die Messung wurde er dem Einmalprozess im
Produktionscontainer bereitgestellt. Release `v0.5.1` konsumiert ihn nicht;
nach der Messung wurde der Container deshalb ohne `OPENAI_API_KEY` neu
erstellt. Der Schluessel bleibt auf dem Host vorbereitet, ohne im ungenutzten
Container-Blast-Radius zu liegen. Er wurde zu keinem Zeitpunkt in Logs oder
Ausgaben geschrieben.

Damit ist nur die Credential-Basis eingerichtet. Release `v0.5.1` hat noch
keinen `gpt-transcribe`-Enginepfad; die Messung lief auf `vidscribe-worker` als
kontrollierter Einmaltest mit dem offiziellen OpenAI-Python-Client im selben
Produktionscontainer und damit ueber den spaeter relevanten Netzwerkpfad.

## Testmaterial und Reproduzierbarkeit

Die Audiodateien werden nicht eingecheckt.

| Fall | Quelle und Ausschnitt | SHA-256 des 16-kHz-Mono-WAV |
| --- | --- | --- |
| DE | Deutschlandfunk, *Die Maschine*, Quelldatei 00:00-03:00 | `9b0114e248070f9e89f9b51e15e523f0bb82227c67b94d3e5ce530017ab88717` |
| EN | Lex Fridman Podcast #500, Quelldatei 11:42.786-14:42.786 | `2e3c81712cdd8ca143386ca5e0ab4a552030672f56476eb8bdbb0db034ad61a3` |

Quellen:

- DE Audio und Produktionsmanuskript: <https://www.deutschlandfunk.de/das-dunkle-in-der-black-box-die-maschine-100.html>
- EN Audio: <https://media.blubrry.com/takeituneasy/ins.blubrry.com/takeituneasy/lex_ai_khabib_nurmagomedov.mp3>
- EN menschlich erstelltes Referenztranskript: <https://lexfridman.com/?p=6492>

Die Podcast-MP3 enthaelt dynamisch eingefuegte Werbung. Deshalb entspricht die
Transkriptmarke 04:53 in der aktuell ausgelieferten MP3 ungefaehr 11:42.786.
Ein erster, nicht korrekt ausgerichteter Werbeausschnitt wurde verworfen.

## Messergebnisse auf dem CPX32

Jede Laufzeit umfasst die vollstaendige 180-Sekunden-Datei. Die
Qualitaetsfenster wurden an belastbare Referenzgrenzen gekuerzt.
WER, CER und Keyword-Recall wurden mit einem Ad-hoc-Auswertungsskript berechnet,
das Normalisierung und Levenshtein-Logik aus `internal/quality/evaluate.go`
spiegelt; die Re-Eval-Faelle sind noch nicht in einem versionierten Corpus.

| Sprache | Engine | Laufzeit | Audio/Laufzeit | WER | CER | Keyword-Recall |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| DE | Parakeet | 29,629 s | 6,08x | 12,56 % | 7,12 % | 100 % |
| DE | Whisper Large-v3 | 170,640 s | 1,05x | 2,24 % | 1,45 % | 100 % |
| DE | `gpt-transcribe` | 7,574 s | 23,77x | 5,38 % | 1,84 % | 100 % |
| EN | Parakeet | 26,224 s | 6,86x | 1,91 % | 1,00 % | 75,0 % |
| EN | Whisper Large-v3 | 150,260 s | 1,20x | 2,87 % | 2,14 % | 62,5 % |
| EN | `gpt-transcribe` | 6,005 s | 29,98x | 0,76 % | 0,74 % | 87,5 % |

Stichproben aus `docker stats` lagen bei etwa 4,4 GiB fuer Parakeet und
5,2 GiB fuer Whisper Large-v3. Das sind keine instrumentierten Peak-Werte,
zeigen aber, dass beide Engines fuer die 3-Minuten-Faelle in die aktuelle
8-GiB-Grenze passen. Der Dienst verarbeitet Jobs seriell. Ein bereits
abgeschlossener Produktionsjob transkribierte die rund 200-minuetige Lex-Folge
mit Parakeet in etwa 21,9 Minuten; ein instrumentierter Speicher-Peak und ein
entsprechender Langformlauf mit Whisper Large-v3 fehlen noch.

Eine manuelle Stichprobe der Zeitstempel beider lokalen Engines an englischen
Sprecherwechseln zeigte Abweichungen im Bereich weniger Zehntelsekunden; das
ist noch keine formale Driftmessung. Beide
erzeugen im aktuellen Vidscribe-Pfad TXT, JSON, SRT und VTT. Eine formale
Zeitstempel-Driftmetrik ueber den gesamten Korpus steht noch aus.
`gpt-transcribe` akzeptierte dagegen bei einem realen 10-Sekunden-Test mit dem
Datei-Endpoint und der aktuellen SDK-Version weder `verbose_json` noch `srt`
oder `vtt`; alle drei konkreten Requests wurden mit HTTP 400 abgewiesen. Der
erfolgreiche JSON-Pfad liefert nur den Gesamttext, keine Segmentzeitstempel.

### Grenzen der Qualitaetsmessung

- Die englische Referenz ist menschlich erstellt und mit dem Audioausschnitt
  ausgerichtet. Sie ist fuer diesen Zwischenstand die staerkere WER-Basis.
- Die deutsche Referenz wurde aus dem offiziellen Produktionsmanuskript an den
  tatsaechlich gesendeten Wortlaut angepasst. Da die Anpassung beide
  lokalen Hypothesen zur Kontrolle heranzog, sind die deutschen WER-/CER-Werte
  vorlaeufig und duerfen bis zur unabhaengigen Hoerpruefung keine
  Produktionsentscheidung tragen.
- Der Korpus umfasst bislang nur zwei Ausschnitte. Akzente, Musik/Stille,
  Fachsprache, Zahlen, lange Episoden und Chunk-Grenzen brauchen weitere Faelle.
- Exakter Keyword-Recall ist bei Schreibweisen von Zahlen und Eigennamen
  sprunghaft. In Englisch verfehlten beide Engines insbesondere `Kirovaul` und
  `Makhachkala`; Whisper schrieb ausserdem `200` statt `two hundred`.

## `gpt-transcribe`: Fakten und reale Messung

Die aktuelle OpenAI-Modellseite nennt `gpt-transcribe` fuer Datei- und
Realtime-Transkription, den Endpoint `/v1/audio/transcriptions`, Kontext-,
Keyword- und Sprachhinweise sowie einen Preis von 0,0045 USD pro Audiominute:
<https://developers.openai.com/api/docs/models/gpt-transcribe>.

Daraus folgen diese Kosten:

- 3 Minuten: 0,0135 USD
- 60 Minuten: 0,27 USD
- die rund 200-minuetige Lex-Folge: rund 0,90 USD

Die zwei regulaeren 3-Minuten-Vergleichslaeufe und zwei erfolgreiche
3-Minuten-Groessentests entsprechen zusammen voraussichtlich 0,054 USD. Die
gemessene reine API-Antwortzeit betrug 7,574 Sekunden fuer DE und 6,005
Sekunden fuer EN. Paketinstallation und lokaler Clientstart sind darin nicht
enthalten.

Der im Issue genannte 25-MB-Grenzwert war in den aktuell gefundenen
autoritativen Modell- und API-Seiten nicht explizit ausgewiesen und gilt fuer
den getesteten Pfad nicht: Ein gueltiges 180-Sekunden-WAV mit 34.560.290 Byte
wurde zweimal erfolgreich akzeptiert. Die tatsaechliche Obergrenze ist damit
noch nicht bestimmt. Fuer lange Episoden bleibt deterministisches Chunking
trotzdem sinnvoll, um Upload-Risiko, Wiederholungen und Fehlerradius zu
begrenzen. Da `gpt-transcribe` keine Segmentzeitstempel lieferte, wuerden
Ueberlappungs-Deduplizierung und globale SRT/VTT-Zeitstempel einen separaten
Alignment-Schritt oder eine zweite Engine erfordern.

OpenAI nutzt API-Ein- und Ausgaben standardmaessig nicht zum Training. Ohne
freigeschaltete Aufbewahrungskontrollen koennen Abuse-Monitoring-Logs jedoch
standardmaessig bis zu 30 Tage vorgehalten werden; Zero Data Retention muss auf
Organisations- oder Projektebene geprueft werden:
<https://platform.openai.com/docs/models/default-usage-policies-by-endpoint>.

## Betriebsoptionen: vorlaeufige Einordnung

| Option | Zwischenbewertung |
| --- | --- |
| CPX32 unveraendert | Einzige kurzfristig durch reale Messungen gedeckte Option; 3-Minuten-Laeufe beider Engines und ein rund 200-minuetiger Parakeet-Job funktionieren, der instrumentierte Whisper-Langformlauf fehlt. Der Tarif ist nach der Preisanpassung teuer. |
| CX33 | Gleiche nominelle 4-vCPU-/8-GB-Klasse und deutlich guenstiger, aber Shared-Cost-Optimized statt AMD-Regular; vor Migration ist derselbe Benchmark auf einer temporaeren CX33 noetig. |
| Nur on-demand | Spart Leerlaufkosten, erhoeht aber Betriebs- und Startkomplexitaet und widerspricht einem jederzeit erreichbaren primaeren Backend, solange keine robuste Aktivierungslogik existiert. |
| `gpt-transcribe` Opt-in | Sehr schnell und insbesondere fuer EN qualitativ stark, erfuellt aber ohne zusaetzliches Alignment den bestehenden SRT/VTT-/Zeitstempelvertrag nicht. Fuer reine TXT-Jobs bleibt ein budgetbegrenztes Opt-in plausibel. |

Seit dem 15. Juni 2026 nennt Hetzner fuer Deutschland/Finnland monatlich
35,49 EUR netto fuer CPX32 und 8,49 EUR netto fuer CX33, jeweils ohne IPv4:
<https://docs.hetzner.com/general/infrastructure-and-availability/price-adjustment/>.

## Vorlaeufige Empfehlung

1. Den remote-first HTTP-Backendpfad auf `vidscribe-worker` unveraendert
   weiterbetreiben; Parakeet bleibt fuer schnelle Standardjobs sinnvoll.
2. Whisper Large-v3 als explizites Qualitaetsprofil fuer Deutsch und schwierige
   Inhalte beibehalten; der CPX32 schafft es ungefaehr in Echtzeit.
3. Vor einem Tarifwechsel denselben Container und Benchmark auf einer
   temporaeren CX33 zu mehreren Tageszeiten messen, damit Shared-CPU-Streuung
   sichtbar wird. Bei stabiler Laufzeit und ohne OOM ist CX33 der
   wirtschaftlich naheliegende Zielhost.
4. Noch keine OpenAI-Engine in den allgemeinen Profilpfad implementieren. Ein
   TXT-only-Opt-in ist erst nach einem expliziten Outputvertrag, einem
   dedizierten budgetbegrenzten API-Projekt und einem Chunking-Test sinnvoll.
   Der jetzt wiederverwendete n8n-Schluessel reicht fuer den Benchmark, trennt
   aber weder Budget noch Berechtigungen. Fuer SRT/VTT ist vorab ein
   Alignment-Design noetig.

## Naechste Nachweise

- unabhaengige Hoerpruefung der deutschen Goldreferenz
- Obergrenze und Chunk-Rekonstruktion oberhalb der bereits bestaetigten
  34.560.290 Byte bestimmen
- Alignment-Konzept fuer SRT/VTT und Zeitstempel bei `gpt-transcribe`
- temporaere CX33-Laeufe zu mehreren Tageszeiten mit identischem Image und
  denselben Fixture-Hashes
- instrumentierter Whisper-Langformlauf mit Speicher-Peak und OOM-Marge
- mindestens ein langer und ein musik-/stillehaltiger Fall
