package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePublicURLRejectsLocalTargets(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1/x", "http://[::1]/x", "http://169.254.169.254/latest", "http://localhost/x", "file:///etc/passwd"} {
		if err := validatePublicURL(context.Background(), raw); err == nil {
			t.Errorf("expected %s to be rejected", raw)
		}
	}
}

func TestContainedPathRejectsSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := containedPath(root, filepath.Join(link, "nested")); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestContainedPath(t *testing.T) {
	root := t.TempDir()
	inside, err := containedPath(root, filepath.Join(root, "nested"))
	if err != nil || inside == "" {
		t.Fatalf("inside path rejected: %v", err)
	}
	if _, err := containedPath(root, filepath.Join(root, "..", "escape")); err == nil {
		t.Fatal("escape path accepted")
	}
}

func TestContainedPathResolvesRelativePathBelowRoot(t *testing.T) {
	root := t.TempDir()
	inside, err := containedPath(root, filepath.Join("diagnostics", "smoke"))
	if err != nil {
		t.Fatalf("relative path rejected: %v", err)
	}
	want := filepath.Join(root, "diagnostics", "smoke")
	if inside != want {
		t.Fatalf("relative path resolved to %q, want %q", inside, want)
	}
}

func TestContainedPathDefaultDotUsesRelativeRootOnce(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	inside, err := containedPath("./transcripts", ".")
	if err != nil {
		t.Fatalf("default output path rejected: %v", err)
	}
	want := filepath.Join(workDir, "transcripts")
	if inside != want {
		t.Fatalf("default output path resolved to %q, want %q", inside, want)
	}
}

func TestContainedPathRejectsRelativeTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := containedPath(root, filepath.Join("..", "escape")); err == nil {
		t.Fatal("relative traversal accepted")
	}
}
