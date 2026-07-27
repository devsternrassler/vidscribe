package subprocess

import (
	"context"
	"os/exec"
)

// CommandContext creates a cancellable command whose platform-specific setup
// also terminates child processes spawned by uvx, yt-dlp, ffmpeg or Python.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	configureCancellation(cmd)
	return cmd
}
