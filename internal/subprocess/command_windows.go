//go:build windows

package subprocess

import "os/exec"

// Windows uses os/exec's native process termination. The runtime wrappers do
// not invoke a shell, so the direct process is the relevant cancellation unit.
func configureCancellation(_ *exec.Cmd) {}
