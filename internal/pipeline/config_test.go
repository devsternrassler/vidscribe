package pipeline

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSafeTitle(t *testing.T) {
	tests := []struct {
		name     string
		meta     Metadata
		contains string // substring the result must contain
		notEmpty bool
	}{
		{
			name:     "normal title",
			meta:     Metadata{Title: "Hello World"},
			contains: "Hello World",
		},
		{
			name:     "special chars stripped",
			meta:     Metadata{Title: `A/B\C:D*E?F"G<H>I|J`},
			contains: "A_B_C_D_E_F_G_H_I_J",
		},
		{
			name:     "empty title falls back to ID",
			meta:     Metadata{ID: "abc123"},
			contains: "abc123",
		},
		{
			name: "long title truncated",
			meta: Metadata{Title: strings.Repeat("x", 200)},
		},
		{
			name:     "newline removed",
			meta:     Metadata{Title: "line1\nline2"},
			contains: "line1_line2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.meta.SafeTitle()
			if got == "" {
				t.Fatal("SafeTitle returned empty string")
			}
			if len(got) > 120 {
				t.Errorf("SafeTitle too long: %d chars", len(got))
			}
			if tt.contains != "" && !strings.Contains(got, tt.contains) {
				t.Errorf("SafeTitle(%q) = %q, want substring %q", tt.meta.Title, got, tt.contains)
			}
		})
	}
}

func TestConfigNormalizeRejectsUnknownValues(t *testing.T) {
	tests := []Config{
		{Profile: "mystery", Formats: []string{"txt"}},
		{Profile: "custom", Engine: "magic", Formats: []string{"txt"}},
		{Profile: "custom", Engine: "faster", Model: "small", Device: "cpu", ComputeType: "int8", Formats: []string{"pdf"}},
		{Profile: "custom", Engine: "faster", Model: "small", Device: "cpu", ComputeType: "float16", Formats: []string{"txt"}},
		{Profile: "custom", Engine: "faster", Model: "small", Device: "cpu", ComputeType: "int8", Language: "not a language", Formats: []string{"txt"}},
		{Profile: "custom", Engine: "faster", Model: "small", Device: "cpu", ComputeType: "int8", MaxFileSize: "--exec", Formats: []string{"txt"}},
		{Profile: "custom", Engine: "faster", Model: "small", Device: "cpu", ComputeType: "int8", JSRuntime: "python:/tmp/x", Formats: []string{"txt"}},
	}
	for i := range tests {
		if err := tests[i].Normalize(context.Background()); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestConfigNormalizeGPUFreeProfile(t *testing.T) {
	cfg := &Config{Profile: "gpu-free", Formats: []string{"txt"}}
	if err := cfg.Normalize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cfg.Engine != "parakeet" || cfg.Device != "cpu" || cfg.ComputeType != "int8" {
		t.Fatalf("unexpected profile resolution: %+v", cfg)
	}
	if !cfg.HasFormat("manifest") {
		t.Fatal("manifest must always be enabled")
	}
}

func TestConfigNormalizeBalancedWithoutCUDAUsesParakeet(t *testing.T) {
	original := cudaProbe
	cudaProbe = func(context.Context) bool { return false }
	t.Cleanup(func() { cudaProbe = original })
	cfg := &Config{Profile: "balanced", Formats: []string{"txt"}}
	if err := cfg.Normalize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cfg.Engine != "parakeet" || cfg.Device != "cpu" {
		t.Fatalf("unexpected CPU balance: %+v", cfg)
	}
}

func TestSafeTitleIncludesIDAndPreservesUTF8(t *testing.T) {
	got := (&Metadata{Title: strings.Repeat("ä", 80), ID: "xyz"}).SafeTitle()
	if !strings.HasSuffix(got, " [xyz]") || !utf8.ValidString(got) {
		t.Fatalf("unsafe title: %q", got)
	}
}

func TestHasFormat(t *testing.T) {
	cfg := &Config{Formats: []string{"txt", "md", "json"}}

	for _, f := range []string{"txt", "md", "json"} {
		if !cfg.HasFormat(f) {
			t.Errorf("HasFormat(%q) = false, want true", f)
		}
	}
	for _, f := range []string{"srt", "vtt", ""} {
		if cfg.HasFormat(f) {
			t.Errorf("HasFormat(%q) = true, want false", f)
		}
	}
}

func TestHasFormatWithSpaces(t *testing.T) {
	cfg := &Config{Formats: []string{" txt ", " md"}}
	if !cfg.HasFormat("txt") {
		t.Error("HasFormat should trim spaces in stored formats")
	}
}
