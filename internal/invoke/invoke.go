// Package invoke implements a mock AWS Lambda Service API.
// It intercepts lambda.Invoke / lambda.InvokeAsync calls made by a locally-running
// Lambda function and routes them to the appropriate Laminar service by matching
// the function name against the service "name" field in laminar.json.
//
// Supported invocation types:
//   - RequestResponse (default): runs the Lambda and returns its output synchronously.
//   - Event (async): fires the Lambda in the background and immediately returns 202.
//
// Set AWS_ENDPOINT_URL_LAMBDA (or AWS_LAMBDA_ENDPOINT for older SDKs) to point at
// this server so the SDK routes calls here instead of real AWS.
//
//nolint:revive
package invoke

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/weareprogmatic/laminar/internal/config"
	"github.com/weareprogmatic/laminar/internal/runner"
)

// invocationPath is the AWS Lambda Service API route prefix for function invocations.
const invocationPath = "/2015-03-31/functions/"

// Server is a mock AWS Lambda Service API that routes invocations to local binaries.
type Server struct {
	services map[string]config.ServiceConfig // keyed by lower-cased function name
	listener net.Listener
	server   *http.Server
	port     int
}

// NewServer creates and binds a new invoke Server on a random port.
func NewServer(services []config.ServiceConfig) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("failed to bind lambda service API: %w", err)
	}

	svcMap := make(map[string]config.ServiceConfig, len(services))
	for _, svc := range services {
		svcMap[strings.ToLower(svc.Name)] = svc
	}

	s := &Server{
		services: svcMap,
		listener: listener,
		port:     listener.Addr().(*net.TCPAddr).Port,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleInvocation)

	s.server = &http.Server{
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      0, // Lambda may take a while
	}

	return s, nil
}

// Start begins serving requests in a background goroutine.
func (s *Server) Start() {
	go func() {
		if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[Lambda API] Server error: %v", err)
		}
	}()
}

// Port returns the TCP port the server is listening on.
func (s *Server) Port() int {
	return s.port
}

// Addr returns the full host:port address of the server.
func (s *Server) Addr() string {
	return fmt.Sprintf("127.0.0.1:%d", s.port)
}

// Close gracefully shuts down the server.
func (s *Server) Close() error {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.server.Shutdown(ctx)
	}
	return s.listener.Close()
}

// handleInvocation handles POST /2015-03-31/functions/{FunctionName}/invocations.
func (s *Server) handleInvocation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name, ok := parseFunctionName(r.URL.Path)
	if !ok {
		http.Error(w, "Invalid invocation path", http.StatusNotFound)
		return
	}

	svc, ok := s.services[strings.ToLower(name)]
	if !ok {
		log.Printf("[Lambda API] Unknown function %q - not found in laminar.json", name)
		http.Error(w, fmt.Sprintf("Function not found: %s", name), http.StatusNotFound)
		return
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read payload", http.StatusBadRequest)
		return
	}

	invocationType := r.Header.Get("X-Amz-Invocation-Type")
	if invocationType == "" {
		invocationType = "RequestResponse"
	}

	log.Printf("[Lambda API] %s invocation of %q (%s)", invocationType, name, svc.Binary)

	switch invocationType {
	case "Event":
		s.invokeAsync(svc, payload)
		w.WriteHeader(http.StatusAccepted)

	case "RequestResponse":
		s.invokeSync(w, r.Context(), svc, payload)

	default:
		http.Error(w, fmt.Sprintf("Unsupported invocation type: %s", invocationType), http.StatusBadRequest)
	}
}

// invokeAsync fires the Lambda in a background goroutine and discards the response.
func (s *Server) invokeAsync(svc config.ServiceConfig, payload []byte) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(svc.Timeout)*time.Second)
		defer cancel()
		if _, err := runner.Run(ctx, svc.Binary, svc.EnvFile, svc.Env, svc.WorkingDir, svc.Timeout, svc.DebugPort, payload); err != nil {
			log.Printf("[Lambda API] Async invocation of %q failed: %v", svc.Name, err)
		}
	}()
}

// invokeSync runs the Lambda, waits for its response, and writes the raw output back.
func (s *Server) invokeSync(w http.ResponseWriter, ctx context.Context, svc config.ServiceConfig, payload []byte) {
	output, err := runner.Run(ctx, svc.Binary, svc.EnvFile, svc.Env, svc.WorkingDir, svc.Timeout, svc.DebugPort, payload)
	if err != nil {
		log.Printf("[Lambda API] Sync invocation of %q failed: %v", svc.Name, err)
		errBody, _ := json.Marshal(map[string]string{
			"errorMessage": err.Error(),
			"errorType":    "InvocationError",
		})
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Amz-Function-Error", "Unhandled")
		w.WriteHeader(http.StatusOK) // AWS returns 200 even for function errors
		_, _ = w.Write(errBody)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output)
}

// parseFunctionName extracts the plain function name from a Lambda API invocation path.
// Input: /2015-03-31/functions/{name}/invocations
// Handles plain names, name:qualifier, and full ARNs.
func parseFunctionName(path string) (string, bool) {
	after, ok := strings.CutPrefix(path, invocationPath)
	if !ok {
		return "", false
	}

	name, _, found := strings.Cut(after, "/invocations")
	if !found || name == "" {
		return "", false
	}

	// Handle full ARN: arn:aws:lambda:region:account:function:name[:qualifier]
	if strings.HasPrefix(name, "arn:") {
		parts := strings.Split(name, ":")
		if len(parts) >= 7 {
			return parts[6], true
		}
		return parts[len(parts)-1], true
	}

	// Strip qualifier: name:qualifier -> name
	name, _, _ = strings.Cut(name, ":")
	return name, true
}
