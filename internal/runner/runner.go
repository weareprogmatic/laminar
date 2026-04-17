package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weareprogmatic/laminar/internal/runtime"
)

// filteredWriter filters out noisy AWS Lambda SDK log messages
type filteredWriter struct {
	serviceName string
	buffer      []byte
}

func (f *filteredWriter) Write(p []byte) (n int, err error) {
	written := len(p)
	f.buffer = append(f.buffer, p...)

	// Process complete lines
	for {
		idx := bytes.IndexByte(f.buffer, '\n')
		if idx == -1 {
			break
		}

		line := string(bytes.TrimSpace(f.buffer[:idx]))
		f.buffer = f.buffer[idx+1:]

		// Filter out expected/noisy AWS SDK messages
		if strings.Contains(line, "got unexpected status code: 410") ||
			(strings.Contains(line, "failed to GET") && strings.Contains(line, "/runtime/invocation/next")) ||
			strings.Contains(line, "expected AWS Lambda environment variables") {
			continue // Suppressed
		}

		// Strip Lambda's timestamp prefix (format: "YYYY/MM/DD HH:MM:SS ")
		// to avoid double timestamps since Laminar adds its own
		if len(line) >= 20 && line[4] == '/' && line[7] == '/' && line[10] == ' ' && line[13] == ':' && line[16] == ':' {
			line = line[20:] // Skip "2006/01/02 15:04:05 " prefix
		}

		// Pass through other logs with service name prefix
		if line != "" {
			log.Printf("[%s] %s", f.serviceName, line)
		}
	}

	return written, nil
}

// lambdaCmd holds a started Lambda process and associated resources.
type lambdaCmd struct {
	cmd    *exec.Cmd
	server *runtime.Server
	ctx    context.Context
	cancel context.CancelFunc
}

// debugMinTimeout is the minimum timeout in seconds applied when debug_port is set.
const debugMinTimeout = 300

// waitForDebugPort polls addr until it accepts a TCP connection, the context is done,
// or the process exits (processDone closed). Returns nil when the port is ready.
func waitForDebugPort(ctx context.Context, addr string, processDone <-chan struct{}) error {
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond) //nolint:noctx
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("debug port %s never became ready: %w", addr, ctx.Err())
		case <-processDone:
			return fmt.Errorf("debug port %s never became ready: process exited", addr)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// startProcess creates and starts the Lambda binary, optionally wrapped with a debugger.
//
// For dlv: dlv wraps and owns the binary; returns the dlv cmd.
// For lldb: the binary is started normally and its PID is logged so the developer
// can attach VS Code (CodeLLDB) using the process picker. No server is started.
// For no debug: the binary is started as-is.
func startProcess(ctx context.Context, binary, workingDir string, env []string, debugPort int, debugger string) (*exec.Cmd, error) {
	logWriter := &filteredWriter{serviceName: "lambda"}
	var cmd *exec.Cmd
	if debugPort > 0 && debugger != "lldb" {
		// dlv wraps the binary; the dlv process IS the thing we monitor for liveness.
		killDebuggerOnPort(debugPort, debugger)
		cmd = exec.CommandContext(ctx, "dlv", "exec", //nolint:gosec // args are controlled
			"--headless",
			"--listen=127.0.0.1:"+strconv.Itoa(debugPort),
			"--accept-multiclient",
			"--continue", // start the Lambda immediately; IDE can attach & hit breakpoints at any time
			"--", binary)
	} else {
		cmd = exec.CommandContext(ctx, binary) //nolint:gosec // path is from config
	}
	cmd.Env = env
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	setProcAttrs(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start binary %s: %w", binary, err)
	}
	if debugger == "lldb" {
		log.Printf("[Lambda] PID %d — attach CodeLLDB now (use 'Attach by PID' or search for '%s')",
			cmd.Process.Pid, binary)
	}
	return cmd, nil
}

// awaitDebugPort waits for the debug port to be ready and logs when it is.
// Returns nil immediately when debugPort is 0 or debugger is "lldb" (lldb attaches
// directly by PID — no port is opened).
// processDone, if non-nil, is closed when the process exits — this lets us
// fail fast instead of polling until ctx expires. When processDone fires the
// caller is responsible for cmd.Wait(), so we skip it here.
func awaitDebugPort(ctx context.Context, cmd *exec.Cmd, srv *runtime.Server, cancel context.CancelFunc, debugPort int, debugger string, processDone <-chan struct{}) error {
	if debugPort == 0 || debugger == "lldb" {
		return nil
	}
	addr := "127.0.0.1:" + strconv.Itoa(debugPort)
	err := waitForDebugPort(ctx, addr, processDone)
	if err != nil {
		_ = killProcess(cmd)
		// Only wait if processDone is nil (no external goroutine is waiting on the process).
		// If processDone is non-nil, the caller's goroutine handles cmd.Wait().
		if processDone == nil {
			_ = cmd.Wait()
		}
		cancel()
		_ = srv.Close()
		return fmt.Errorf("%s debugger failed to start on %s: %w", effectiveDebugger(debugger), addr, err)
	}
	log.Printf("[Lambda] Debugger (%s) ready on 127.0.0.1:%d – attach your IDE now", effectiveDebugger(debugger), debugPort)
	return nil
}

// effectiveDebugger returns the canonical debugger name, defaulting to "dlv".
func effectiveDebugger(debugger string) string {
	if debugger == "" {
		return "dlv"
	}
	return debugger
}

// launch creates a runtime API server, builds the environment, and starts the Lambda binary.
// On error all resources are cleaned up before returning.
func launch(ctx context.Context, binary, envFile string, envVars map[string]string, workingDir string, timeoutSeconds, debugPort int, debugger string) (*lambdaCmd, error) {
	// Enforce a minimum timeout whenever a debugger is configured — the developer
	// needs time to attach before the first invocation times out.
	if (debugPort > 0 || debugger == "lldb") && timeoutSeconds < debugMinTimeout {
		timeoutSeconds = debugMinTimeout
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)

	srv, err := runtime.NewServer()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create runtime API server: %w", err)
	}
	srv.Start()

	env, err := buildEnv(envFile, envVars, srv.Port())
	if err != nil {
		cancel()
		_ = srv.Close()
		return nil, fmt.Errorf("failed to build environment: %w", err)
	}

	cmd, err := startProcess(timeoutCtx, binary, workingDir, env, debugPort, debugger)
	if err != nil {
		cancel()
		_ = srv.Close()
		return nil, err
	}

	if err := awaitDebugPort(timeoutCtx, cmd, srv, cancel, debugPort, debugger, nil); err != nil {
		return nil, err
	}

	return &lambdaCmd{cmd: cmd, server: srv, ctx: timeoutCtx, cancel: cancel}, nil
}

// StreamResp is the streaming Lambda response type. Re-exported so that the
// server package does not need to import the runtime package directly.
type StreamResp = runtime.StreamResp

// Run executes a binary with the given environment and payload, returning the Lambda response.
// It starts a mock AWS Lambda Runtime API server that the Lambda will call back to.
// Use StartWarm for persistent (keep-alive) execution; Run is for single-shot invocations.
func Run(ctx context.Context, binary, envFile string, envVars map[string]string, workingDir string, timeoutSeconds, debugPort int, debugger string, payloadBytes []byte) ([]byte, error) {
	lc, err := launch(ctx, binary, envFile, envVars, workingDir, timeoutSeconds, debugPort, debugger)
	if err != nil {
		return nil, err
	}
	defer lc.cancel()
	defer func() { _ = lc.server.Close() }()

	result, invokeErr := lc.server.Invoke(lc.ctx, payloadBytes)
	go func() { _ = lc.cmd.Wait() }()
	if invokeErr != nil {
		if lc.ctx.Err() != nil {
			return nil, fmt.Errorf("binary %s timed out after %d seconds", binary, timeoutSeconds)
		}
		return nil, invokeErr
	}
	return result, nil
}

// RunStream executes a binary for a single streaming invocation.
// Use StartWarm for persistent execution; RunStream is for single-shot invocations (e.g. tests).
func RunStream(ctx context.Context, binary, envFile string, envVars map[string]string, workingDir string, timeoutSeconds, debugPort int, debugger string, payloadBytes []byte) (*StreamResp, func(), error) {
	lc, err := launch(ctx, binary, envFile, envVars, workingDir, timeoutSeconds, debugPort, debugger)
	if err != nil {
		return nil, nil, err
	}

	resp, done, invokeErr := lc.server.InvokeStream(lc.ctx, payloadBytes)
	if invokeErr != nil {
		lc.cancel()
		_ = lc.server.Close()
		go func() { _ = lc.cmd.Wait() }()
		if lc.ctx.Err() != nil {
			return nil, nil, fmt.Errorf("binary %s timed out after %d seconds", binary, timeoutSeconds)
		}
		return nil, nil, invokeErr
	}

	cleanup := func() {
		done()
		lc.cancel()
		_ = lc.server.Close()
		go func() { _ = lc.cmd.Wait() }()
	}
	return resp, cleanup, nil
}

// WarmLambda keeps a Lambda process alive between invocations, matching real Lambda warm-start
// behaviour. The debug port (when set) is open from the moment Start returns, so the IDE can
// attach before the first request arrives.
// When watch is true the binary's modification time is polled; a rebuild triggers an automatic
// restart so the next request hits the new code.
type WarmLambda struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	server    *runtime.Server
	cancel    context.CancelFunc
	deadCh    chan struct{}
	binary    string
	envFile   string
	envVars   map[string]string
	workDir   string
	timeout   int
	debugPort int
	debugger  string
	parentCtx context.Context
}

// StartWarm starts a persistent Lambda process.
// When debugPort > 0 the binary is wrapped with dlv; this function returns only after the
// debug port is confirmed open so the IDE can attach immediately.
// When debugger is "lldb" the binary PID is logged and the developer attaches via CodeLLDB.
// When watch is true a background goroutine monitors the binary for changes and restarts.
func StartWarm(ctx context.Context, binary, envFile string, envVars map[string]string, workingDir string, timeout, debugPort int, debugger string, watch bool) (*WarmLambda, error) {
	wl := &WarmLambda{
		binary:    binary,
		envFile:   envFile,
		envVars:   envVars,
		workDir:   workingDir,
		timeout:   timeout,
		debugPort: debugPort,
		debugger:  debugger,
		parentCtx: ctx,
	}

	if err := wl.startProcess(); err != nil {
		return nil, err
	}

	if watch {
		go wl.watchBinary()
	}

	return wl, nil
}

// startProcess boots the Lambda (and optional dlv), wiring up runtime API and deadCh.
// Caller must NOT hold wl.mu.
func (wl *WarmLambda) startProcess() error {
	srv, err := runtime.NewServer()
	if err != nil {
		return fmt.Errorf("failed to create runtime API server: %w", err)
	}
	srv.Start()

	env, err := buildEnv(wl.envFile, wl.envVars, srv.Port())
	if err != nil {
		_ = srv.Close()
		return fmt.Errorf("failed to build environment: %w", err)
	}

	cancelCtx, cancel := context.WithCancel(wl.parentCtx)
	cmd, err := startProcess(cancelCtx, wl.binary, wl.workDir, env, wl.debugPort, wl.debugger)
	if err != nil {
		cancel()
		_ = srv.Close()
		return err
	}

	// processDone lets awaitDebugPort detect a crashed dlv immediately rather than
	// polling until context expiry. deadCh reuses the same goroutine.
	deadCh := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(deadCh)
		_ = srv.Close()
	}()

	if err := awaitDebugPort(cancelCtx, cmd, srv, cancel, wl.debugPort, wl.debugger, deadCh); err != nil {
		return err
	}

	wl.mu.Lock()
	wl.cmd = cmd
	wl.server = srv
	wl.cancel = cancel
	wl.deadCh = deadCh
	wl.mu.Unlock()

	return nil
}

// watchBinary polls the binary's modification time and restarts when it changes.
// It debounces by waiting for the mod time to stabilise before restarting,
// which avoids launching a half-written binary during a build.
func (wl *WarmLambda) watchBinary() {
	modTime := binaryModTime(wl.binary)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-wl.parentCtx.Done():
			return
		case <-ticker.C:
			newMod := binaryModTime(wl.binary)
			if newMod.Equal(modTime) || newMod.IsZero() {
				continue
			}
			// Debounce: wait, then confirm the mod time has stopped changing.
			select {
			case <-wl.parentCtx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			stableMod := binaryModTime(wl.binary)
			if !stableMod.Equal(newMod) {
				continue // still being written — check again next tick
			}
			modTime = stableMod
			log.Printf("[Lambda] Binary %s changed on disk — restarting", wl.binary)
			wl.restart()
		}
	}
}

// binaryModTime returns the binary's modification time, or zero on error.
func binaryModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// restart kills the current process and starts a fresh one, with retries.
func (wl *WarmLambda) restart() {
	wl.mu.Lock()
	if wl.cancel != nil {
		wl.cancel()
	}
	_ = wl.server.Close()
	wl.mu.Unlock()

	// Give the old process (and dlv) enough time to die and release the debug port.
	time.Sleep(500 * time.Millisecond)

	const maxAttempts = 3
	for attempt := range maxAttempts {
		if wl.parentCtx.Err() != nil {
			return // app is shutting down
		}
		if err := wl.startProcess(); err != nil {
			log.Printf("[Lambda] Failed to restart %s (attempt %d/%d): %v", wl.binary, attempt+1, maxAttempts, err)
			if attempt < maxAttempts-1 {
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			}
			continue
		}
		log.Printf("[Lambda] Restarted %s successfully", wl.binary)
		return
	}
	log.Printf("[Lambda] Giving up restarting %s after %d attempts", wl.binary, maxAttempts)
}

// Invoke sends a payload to the warm Lambda and waits for the response.
// If the warm process is already handling a request, a temporary cold-start
// process is spawned to handle the overflow — matching real Lambda concurrency.
//
// Overflow processes use a detached (background) context so that client-side
// disconnects do not kill the Lambda mid-handler. The configured timeout still
// caps the process lifetime and prevents leaked processes.
func (wl *WarmLambda) Invoke(ctx context.Context, payload []byte) ([]byte, error) {
	wl.mu.Lock()
	srv := wl.server
	wl.mu.Unlock()

	resp, err := srv.TryInvoke(ctx, payload)
	if errors.Is(err, runtime.ErrBusy) {
		log.Printf("[Lambda] Warm process busy — cold-starting overflow for %s", wl.binary)
		return Run(context.Background(), wl.binary, wl.envFile, wl.envVars, wl.workDir, wl.timeout, 0, "", payload)
	}
	return resp, err
}

// InvokeStream sends a payload to the warm Lambda and returns a streaming response.
// If the warm process is busy, a temporary cold-start process handles the overflow.
// The caller MUST call the returned done function after consuming the response Body.
//
// Like Invoke, overflow processes use a detached context so that client disconnects
// (e.g. a CVI provider closing after receiving all streamed data) do not kill the
// Lambda while it runs post-response cleanup (saving state, provider teardown).
func (wl *WarmLambda) InvokeStream(ctx context.Context, payload []byte) (*StreamResp, func(), error) {
	wl.mu.Lock()
	srv := wl.server
	wl.mu.Unlock()

	resp, done, err := srv.TryInvokeStream(ctx, payload)
	if errors.Is(err, runtime.ErrBusy) {
		log.Printf("[Lambda] Warm process busy — cold-starting overflow for %s", wl.binary)
		return RunStream(context.Background(), wl.binary, wl.envFile, wl.envVars, wl.workDir, wl.timeout, 0, "", payload)
	}
	return resp, done, err
}

// Close shuts down the warm Lambda process and its runtime API server.
func (wl *WarmLambda) Close() {
	wl.mu.Lock()
	defer wl.mu.Unlock()
	if wl.cancel != nil {
		wl.cancel()
	}
	_ = wl.server.Close()
}

func buildEnv(envFile string, envVars map[string]string, runtimeAPIPort int) ([]string, error) {
	env := os.Environ()

	// Load from env_file first (if specified)
	if envFile != "" {
		fileVars, err := loadEnvFile(envFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load env file %s: %w", envFile, err)
		}
		env = mergeEnv(env, fileVars)
	}

	// Merge env vars from JSON config (these override env_file)
	if len(envVars) > 0 {
		env = mergeEnv(env, envVars)
	}

	env = setOrReplaceEnv(env, "LAMINAR_LOCAL", "true")

	// Set AWS_LAMBDA_RUNTIME_API to our mock runtime server
	env = setOrReplaceEnv(env, "AWS_LAMBDA_RUNTIME_API", fmt.Sprintf("127.0.0.1:%d", runtimeAPIPort))

	// Also set AWS_LAMBDA_FUNCTION_NAME - another common used variable to check if we're running in Lambda.
	// This is not strictly required but can help with some libraries that check for it.
	env = setOrReplaceEnv(env, "AWS_LAMBDA_FUNCTION_NAME", "laminar-function")

	if !hasEnvVar(env, "AWS_REGION") {
		env = append(env, "AWS_REGION=us-east-1")
	}

	return env, nil
}

func loadEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to open env file: %w", err)
	}
	defer func() { _ = file.Close() }()

	vars := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid line %d in env file: %s", lineNum, line)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		vars[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env file: %w", err)
	}

	return vars, nil
}

func mergeEnv(base []string, envVars map[string]string) []string {
	result := make([]string, 0, len(base)+len(envVars))
	existing := make(map[string]int)

	for i, env := range base {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) > 0 {
			existing[parts[0]] = i
		}
		result = append(result, env)
	}

	for key, value := range envVars {
		envStr := key + "=" + value
		if idx, ok := existing[key]; ok {
			result[idx] = envStr
		} else {
			result = append(result, envStr)
		}
	}

	return result
}

func setOrReplaceEnv(env []string, key, value string) []string {
	newEnv := key + "=" + value
	for i, e := range env {
		if strings.HasPrefix(e, key+"=") {
			env[i] = newEnv
			return env
		}
	}
	return append(env, newEnv)
}

func hasEnvVar(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}
