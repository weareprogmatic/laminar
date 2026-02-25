//go:build !windows

package runner

import (
	"os/exec"
	"strconv"
	"time"
)

// killDebuggerOnPort kills any existing dlv process listening on the given port.
// Uses pkill which is available on Linux and macOS.
func killDebuggerOnPort(port int) {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	// pkill -f matches the full command line; -9 ensures the process is gone immediately.
	cmd := exec.Command("pkill", "-9", "-f", "dlv.*--listen="+addr) //nolint:gosec,noctx // port is from config
	_ = cmd.Run()                                                   // ignore error — no process is fine
	// Give the OS a moment to release the port after the kill.
	time.Sleep(100 * time.Millisecond)
}
