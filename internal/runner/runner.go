package runner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
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
			(strings.Contains(line, "failed to GET") && strings.Contains(line, "/runtime/invocation/next")) {
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

// launch creates a runtime API server, builds the environment, and starts the Lambda binary.
// On error all resources are cleaned up before returning.
func launch(ctx context.Context, binary, envFile string, envVars map[string]string, workingDir string, timeoutSeconds int, payloadBytes []byte) (*lambdaCmd, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)

	srv, err := runtime.NewServer(payloadBytes)
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

	logWriter := &filteredWriter{serviceName: "lambda"}
	cmd := exec.CommandContext(timeoutCtx, binary)
	cmd.Env = env
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	if err := cmd.Start(); err != nil {
		cancel()
		_ = srv.Close()
		return nil, fmt.Errorf("failed to start binary %s: %w", binary, err)
	}

	return &lambdaCmd{cmd: cmd, server: srv, ctx: timeoutCtx, cancel: cancel}, nil
}

// Run executes a binary with the given environment and payload, returning its stdout.
// It starts a mock AWS Lambda Runtime API server that the Lambda will call back to.
func Run(ctx context.Context, binary, envFile string, envVars map[string]string, workingDir string, timeoutSeconds int, payloadBytes []byte) ([]byte, error) {
	lc, err := launch(ctx, binary, envFile, envVars, workingDir, timeoutSeconds, payloadBytes)
	if err != nil {
		return nil, err
	}
	defer lc.cancel()
	defer func() { _ = lc.server.Close() }()

	responseChan := make(chan struct {
		response []byte
		err      error
	}, 1)
	go func() {
		response, err := lc.server.Wait()
		responseChan <- struct {
			response []byte
			err      error
		}{response, err}
	}()

	select {
	case result := <-responseChan:
		_ = lc.cmd.Wait()
		if result.err != nil {
			return nil, fmt.Errorf("lambda error: %w", result.err)
		}
		return result.response, nil
	case <-lc.ctx.Done():
		_ = lc.cmd.Process.Kill()
		_ = lc.cmd.Wait()
		return nil, fmt.Errorf("binary %s timed out after %d seconds", binary, timeoutSeconds)
	}
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
