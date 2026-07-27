package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteMD(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.md")

	meta := &Metadata{
		Title:      "Test Video",
		WebpageURL: "https://youtube.com/watch?v=test",
		Channel:    "Test Channel",
		Duration:   3661,
		UploadDate: "20240315",
	}

	if err := writeMD(dst, meta, testExecution(), "Hello world transcript."); err != nil {
		t.Fatalf("writeMD: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	content := string(data)

	checks := []string{
		"# Test Video",
		"https://youtube.com/watch?v=test",
		"Test Channel",
		"1:01:01",
		"2024-03-15",
		"Hello world transcript.",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("writeMD output missing %q\n\nFull output:\n%s", want, content)
		}
	}
}

func TestWriteMDFallbackToUploader(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.md")

	meta := &Metadata{
		Title:    "Video",
		Uploader: "SomeUploader",
		Duration: 60,
	}

	if err := writeMD(dst, meta, testExecution(), "text"); err != nil {
		t.Fatalf("writeMD: %v", err)
	}

	data, _ := os.ReadFile(dst)
	if !strings.Contains(string(data), "SomeUploader") {
		t.Errorf("expected Uploader as fallback, got:\n%s", data)
	}
}

func testExecution() *ExecutionInfo {
	return &ExecutionInfo{ActualEngine: "faster", Model: "small", Device: "cpu", ComputeType: "int8", DetectedLanguage: "en"}
}

func TestWriteOutputs(t *testing.T) {
	// Prepare a fake transcription temp dir with whisper-like output files.
	txDir := t.TempDir()
	baseName := "testvideo"

	for _, ext := range []string{"txt", "srt", "vtt", "json"} {
		if err := os.WriteFile(
			filepath.Join(txDir, baseName+"."+ext),
			[]byte("content of "+ext),
			0o644,
		); err != nil {
			t.Fatalf("setup: write %s: %v", ext, err)
		}
	}

	outDir := t.TempDir()
	cfg := &Config{
		OutputDir: outDir,
		Formats:   []string{"txt", "srt", "md"},
	}
	tx := &TranscribeResult{TempDir: txDir, BaseName: baseName}
	meta := &Metadata{
		ID:         "abc",
		Title:      "My Video",
		Duration:   120,
		UploadDate: "20240101",
	}

	paths, err := WriteOutputs(cfg, tx, meta, nil)
	if err != nil {
		t.Fatalf("WriteOutputs: %v", err)
	}

	// Expect txt, srt, md and the mandatory provenance manifest.
	wantExts := map[string]bool{"txt": true, "srt": true, "md": true, "manifest": true}
	gotExts := map[string]bool{}
	for _, p := range paths {
		ext := filepath.Ext(p)[1:]
		if strings.HasSuffix(p, ".manifest.json") {
			ext = "manifest"
		}
		gotExts[ext] = true
	}

	for ext := range wantExts {
		if !gotExts[ext] {
			t.Errorf("expected .%s in output, got %v", ext, paths)
		}
	}
	for ext := range gotExts {
		if !wantExts[ext] {
			t.Errorf("unexpected .%s in output", ext)
		}
	}

	// Verify files actually exist.
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("output file missing: %s", p)
		}
	}
	manifestPath := filepath.Join(outDir, "My Video [abc].manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"vidscribe-manifest/v1", "actual_engine", "detected_language"} {
		if !strings.Contains(string(manifest), want) {
			t.Errorf("manifest missing %q: %s", want, manifest)
		}
	}
}

func TestWriteOutputsFailsWhenRequestedFormatMissing(t *testing.T) {
	txDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(txDir, "clip.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := WriteOutputs(&Config{OutputDir: t.TempDir(), Formats: []string{"srt"}},
		&TranscribeResult{TempDir: txDir, BaseName: "clip", ActualEngine: "faster"}, &Metadata{ID: "id", Title: "Title"}, nil)
	if err == nil || !strings.Contains(err.Error(), "did not produce required") {
		t.Fatalf("expected fail-loud format error, got %v", err)
	}
}

func TestWriteOutputsRequiresExplicitOverwrite(t *testing.T) {
	txDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(txDir, "clip.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	cfg := &Config{OutputDir: outDir, Formats: []string{"txt"}}
	tx := &TranscribeResult{TempDir: txDir, BaseName: "clip", ActualEngine: "faster"}
	meta := &Metadata{ID: "id", Title: "Title"}
	if _, err := WriteOutputs(cfg, tx, meta, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOutputs(cfg, tx, meta, nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected collision error, got %v", err)
	}
	cfg.Overwrite = true
	if _, err := WriteOutputs(cfg, tx, meta, nil); err != nil {
		t.Fatalf("explicit overwrite failed: %v", err)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "hello" {
		t.Errorf("copyFile result = %q, %v", data, err)
	}
}
