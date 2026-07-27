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
