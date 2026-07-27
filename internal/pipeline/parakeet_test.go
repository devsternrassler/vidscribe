package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatSRTTime(t *testing.T) {
	cases := []struct {
		sec  float64
		want string
	}{
		{0, "00:00:00,000"},
		{1.5, "00:00:01,500"},
		{61.042, "00:01:01,042"},
		{3661.999, "01:01:01,999"},
		{7325.25, "02:02:05,250"},
	}
	for _, c := range cases {
		if got := formatSRTTime(c.sec); got != c.want {
			t.Errorf("formatSRTTime(%v) = %q, want %q", c.sec, got, c.want)
		}
	}
}

func TestFormatVTTTime(t *testing.T) {
	if got := formatVTTTime(61.042); got != "00:01:01.042" {
		t.Errorf("formatVTTTime(61.042) = %q, want 00:01:01.042", got)
	}
}

var testSegments = []segment{
	{Start: 0.1, End: 4.6, Text: "Moin Leute."},
	{Start: 4.6, End: 24.6, Text: "Schaut euch erst das Video an."},
}

func TestSegmentsToTXT(t *testing.T) {
	got := segmentsToTXT(testSegments)
	want := "Moin Leute.\nSchaut euch erst das Video an.\n"
	if got != want {
		t.Errorf("segmentsToTXT = %q, want %q", got, want)
	}
}

func TestSegmentsToSRT(t *testing.T) {
	got := segmentsToSRT(testSegments)
	if !strings.HasPrefix(got, "1\n00:00:00,100 --> 00:00:04,600\nMoin Leute.\n\n") {
		t.Errorf("SRT block 1 malformed:\n%s", got)
	}
	if !strings.Contains(got, "2\n00:00:04,600 --> 00:00:24,600\nSchaut euch erst das Video an.\n") {
		t.Errorf("SRT block 2 malformed:\n%s", got)
	}
}

func TestSegmentsToVTT(t *testing.T) {
	got := segmentsToVTT(testSegments)
	if !strings.HasPrefix(got, "WEBVTT\n\n") {
		t.Errorf("VTT missing header:\n%s", got)
	}
	if !strings.Contains(got, "00:00:00.100 --> 00:00:04.600\nMoin Leute.\n") {
		t.Errorf("VTT cue malformed:\n%s", got)
	}
}

func TestParseSegmentsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "segs.json")
	data := `[{"start":0.1,"end":4.6,"text":"Moin Leute."},{"start":4.6,"end":24.6,"text":"Weiter."}]`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	segs, err := parseSegmentsFile(path)
	if err != nil {
		t.Fatalf("parseSegmentsFile: %v", err)
	}
	if len(segs) != 2 || segs[0].Text != "Moin Leute." || segs[1].End != 24.6 {
		t.Errorf("unexpected segments: %+v", segs)
	}
}

func TestParseSegmentsFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "segs.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSegmentsFile(path); err == nil {
		t.Error("expected error for empty segment list, got nil")
	}
}

func TestWriteParakeetOutputs(t *testing.T) {
	dir := t.TempDir()
	if err := writeParakeetOutputs(dir, "clip", testSegments); err != nil {
		t.Fatalf("writeParakeetOutputs: %v", err)
	}
	for _, ext := range []string{"txt", "srt", "vtt", "json"} {
		p := filepath.Join(dir, "clip."+ext)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing output %s: %v", p, err)
		}
	}
	jsonData, _ := os.ReadFile(filepath.Join(dir, "clip.json"))
	if !strings.Contains(string(jsonData), `"text"`) || !strings.Contains(string(jsonData), `"segments"`) {
		t.Errorf("json output missing text/segments keys: %s", jsonData)
	}
}

func TestParseVTTFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "captions.vtt")
	content := "WEBVTT\n\n00:00:00.100 --> 00:00:01.500\n<c>Hello &amp; welcome</c>\n\n00:01.500 --> 00:03.000\nHello &amp; welcome\n\n00:03.000 --> 00:04.000\nNext line\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	segs, err := parseVTTFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 || segs[0].Text != "Hello & welcome" || segs[0].End != 3 {
		t.Fatalf("unexpected segments: %+v", segs)
	}
}

func TestDetectProvenance(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.json"), []byte(`{"language":"de","segments":[{"avg_logprob":-0.1},{"avg_logprob":-0.3}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	language, confidence := detectProvenance(dir, "clip", "auto")
	if language != "de" || confidence == nil || *confidence <= 0 || *confidence > 1 {
		t.Fatalf("unexpected provenance: %s %v", language, confidence)
	}
}
