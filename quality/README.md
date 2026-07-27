# Qualitätskorpus

`corpus.json` ist das versionierte, maschinenlesbare Goldset. Zu jedem Fall wird
eine Hypothese `<id>.txt` in einem separaten Ergebnisordner erwartet. Die
Auswertung normalisiert Großschreibung und Interpunktion und meldet WER, CER,
Fachbegriffs-Recall und optional Zeitstempelabweichung. Für letztere enthält ein
Fall `reference_timestamps` und neben `<id>.txt` wird `<id>.json` mit Whisper-
kompatiblen Segment-Starts bereitgestellt.

Der eingecheckte Fall ist der kurze, öffentlich reproduzierbare Netzwerk-Smoke.
Private oder lizenzierte Langform-/PoE-Referenzen gehören nicht ins Repository;
sie können im gleichen Schema lokal ergänzt werden. Für Default-Entscheidungen
sind mindestens Deutsch-Fachvideo, Englisch, Sprachwechsel, Stille/Musik,
Kurzschlusssegment und Langform erforderlich.

```bash
vidscribe quality-eval --corpus quality/corpus.json --hypotheses ./quality-results
```
