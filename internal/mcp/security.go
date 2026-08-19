package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sternrassler/vidscribe/internal/netguard"
)

func validatePublicURL(ctx context.Context, raw string) error {
	return netguard.ValidatePublicURL(ctx, raw)
}

func containedPath(root, requested string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create output root: %w", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	canonicalRoot, err = filepath.Abs(canonicalRoot)
	if err != nil {
		return "", err
	}
	canonicalRequested, err := canonicalizeFuturePath(requested, canonicalRoot)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(canonicalRoot, canonicalRequested)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("output_dir must stay inside %s", canonicalRoot)
	}
	return canonicalRequested, nil
}

func canonicalizeFuturePath(path, base string) (string, error) {
	abs := path
	if !filepath.IsAbs(path) {
		abs = filepath.Join(base, path)
	}
	abs = filepath.Clean(abs)
	existing := abs
	var suffix []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("no existing ancestor for %s", path)
		}
		suffix = append([]string{filepath.Base(existing)}, suffix...)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	parts := append([]string{resolved}, suffix...)
	return filepath.Clean(filepath.Join(parts...)), nil
}
