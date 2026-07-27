//go:build !windows

package subprocess

import (
	"context"
	"testing"
	"time"
)

func TestCommandContextCancelsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := CommandContext(ctx, "sh", "-c", "sleep 60 & wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cancel()
	started := time.Now()
	if err := cmd.Wait(); err == nil {
		t.Fatal("cancelled process unexpectedly succeeded")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("process group did not terminate promptly")
	}
}
