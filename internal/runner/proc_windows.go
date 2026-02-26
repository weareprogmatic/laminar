//go:build windows

package runner

import (
	"os/exec"
)

// setProcAttrs is a no-op on Windows.
// Windows does not support Unix-style process groups via SysProcAttr.
func setProcAttrs(_ *exec.Cmd) {}

// killProcess kills the process directly on Windows.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
