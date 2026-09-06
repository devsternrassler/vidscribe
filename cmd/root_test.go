package cmd

import (
	"testing"
	"time"
)

func TestServeCommandReadsRetentionEnvironmentDefaults(t *testing.T) {
	t.Setenv("VIDSCRIBE_RETENTION_COMPLETED", "720h")
	t.Setenv("VIDSCRIBE_RETENTION_FAILED", "168h")
	t.Setenv("VIDSCRIBE_RETENTION_INTERVAL", "12h")
	command := newServeCommand()
	for name, want := range map[string]string{
		"retention-completed": "720h",
		"retention-failed":    "168h",
		"retention-interval":  "12h",
	} {
		if got := command.Flag(name).DefValue; got != want {
			t.Fatalf("flag %s default=%q, want %q", name, got, want)
		}
	}
}

func TestParseNonNegativeDuration(t *testing.T) {
	if got, err := parseNonNegativeDuration("retention", "24h"); err != nil || got != 24*time.Hour {
		t.Fatalf("duration=%v error=%v", got, err)
	}
	for _, value := range []string{"-1h", "tomorrow"} {
		if _, err := parseNonNegativeDuration("retention", value); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
}
