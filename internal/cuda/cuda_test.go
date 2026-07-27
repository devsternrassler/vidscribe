package cuda

import (
	"strings"
	"testing"
)

func TestScriptConstants(t *testing.T) {
	// Verify scripts contain expected Python imports and aren't empty.
	tests := []struct {
		name   string
		script string
		must   []string
	}{
		{
			name:   "CheckScript",
			script: CheckScript,
			must:   []string{"nvidia.cublas", "ctypes", "libcublas.so.12", "print(\"ok\")"},
		},
		{
			name:   "WhisperWrapperScript",
			script: WhisperWrapperScript,
			must:   []string{"nvidia.cublas", "LD_LIBRARY_PATH", "whisper-ctranslate2", "subprocess"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.script == "" {
				t.Fatal("script is empty")
			}
			for _, s := range tt.must {
				if !strings.Contains(tt.script, s) {
					t.Errorf("script missing %q", s)
				}
			}
		})
	}
}

func TestUvxCublasFlag(t *testing.T) {
	if !strings.HasPrefix(UvxCublasFlag, "nvidia-cublas-cu12==") {
		t.Errorf("UvxCublasFlag = %q, want a pinned nvidia-cublas-cu12 package", UvxCublasFlag)
	}
}
