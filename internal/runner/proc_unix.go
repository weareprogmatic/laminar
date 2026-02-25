//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// setProcAttrs configures the command to run in its own process group.
// On Unix, PGID == PID of the child, so killProcess can kill the group
// (which includes any sub-processes spawned by dlv) with a single signal.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcess kills the process and all children in its process group.
// Falls back to a direct process kill if the group kill fails.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Negative PID targets the entire process group (PGID == child's PID
	// because Setpgid was set above).
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
