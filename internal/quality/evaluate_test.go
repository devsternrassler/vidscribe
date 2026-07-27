package quality

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluate(t *testing.T) {
	result := Evaluate("x", "Hallo Path of Exile", "Hallo path exile", []string{"Path of Exile", "Hallo"})
	if result.WER != 0.25 {
		t.Fatalf("WER=%v, want .25", result.WER)
	}
	if result.KeywordRecall != 0.5 {
		t.Fatalf("recall=%v, want .5", result.KeywordRecall)
	}
}

func TestEvaluateFileAppliesThresholds(t *testing.T) {
	dir := t.TempDir()
	corpus := `{"schema":"vidscribe-quality-corpus/v1","thresholds":{"max_mean_wer":0.1,"max_mean_cer":0.1,"min_keyword_recall":1},"cases":[{"id":"x","reference":"hello world","keywords":["world"]}]}`
	if err := os.WriteFile(filepath.Join(dir, "corpus.json"), []byte(corpus), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("hello there"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateFile(filepath.Join(dir, "corpus.json"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || len(report.Violations) == 0 {
		t.Fatalf("threshold failure not reported: %+v", report)
	}
}

func TestEvaluateTimestampDrift(t *testing.T) {
	dir := t.TempDir()
	corpus := `{"schema":"vidscribe-quality-corpus/v1","thresholds":{"max_timestamp_drift_ms":50},"cases":[{"id":"x","reference":"hello","reference_timestamps":[1,2]}]}`
	if err := os.WriteFile(filepath.Join(dir, "corpus.json"), []byte(corpus), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.json"), []byte(`{"segments":[{"start":1.02},{"start":2.04}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateFile(filepath.Join(dir, "corpus.json"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.MeanTimestampDriftMS == nil || *report.MeanTimestampDriftMS < 29 {
		t.Fatalf("unexpected timestamp report: %+v", report)
	}
}

func TestEvaluateExact(t *testing.T) {
	result := Evaluate("x", "Hello, World!", "hello world", nil)
	if result.WER != 0 || result.CER != 0 || result.KeywordRecall != 1 {
		t.Fatalf("unexpected: %+v", result)
	}
}
