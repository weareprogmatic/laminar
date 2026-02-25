//go:build windows

package runner

import (
	"os/exec"
	"syscall"
)

// setProcAttrs configures the command to run in a new process group on Windows.
// This prevents the child from inheriting the parent's Ctrl+C handler.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// killProcess kills the process directly on Windows.
// Windows does not propagate kill to children via process groups,
// so only the direct process is terminated here.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
