//go:build windows

package runner

import (
	"os/exec"
	"strconv"
	"time"
)

// killDebuggerOnPort kills any existing dlv process listening on the given port.
// Uses PowerShell WMI on Windows to locate the process by command line.
func killDebuggerOnPort(port int) {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	// Match the dlv process by its --listen argument and kill it forcefully.
	script := `Get-WmiObject Win32_Process | ` +
		`Where-Object { $_.CommandLine -like "*dlv*--listen=` + addr + `*" } | ` +
		`ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script) //nolint:gosec,noctx
	_ = cmd.Run()                                                                          // ignore error — no process is fine
	// Give the OS a moment to release the port after the kill.
	time.Sleep(100 * time.Millisecond)
}
