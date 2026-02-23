// Package runtime implements a mock AWS Lambda Runtime API server.
// Lambda functions communicate with this server to handle invocations locally.
// See: https://docs.aws.amazon.com/lambda/latest/dg/runtimes-api.html
//
//nolint:revive
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// Server implements the AWS Lambda Runtime API.
type Server struct {
	listener net.Listener
	server   *http.Server
	port     int
	payload  []byte
	response []byte
	err      error
	done     chan struct{}
	served   bool // Track if we've already served one invocation
	mu       sync.Mutex
}

// NewServer creates a new Lambda Runtime API server on a random port.
func NewServer(payload []byte) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port

	s := &Server{
		listener: listener,
		port:     port,
		payload:  payload,
		done:     make(chan struct{}),
	}

	return s, nil
}

// Port returns the port the runtime API server is listening on.
func (s *Server) Port() int {
	return s.port
}

// Start starts the runtime API server in the background.
func (s *Server) Start() {
	mux := http.NewServeMux()

	// GET /2018-06-01/runtime/invocation/next
	mux.HandleFunc("/2018-06-01/runtime/invocation/next", s.handleInvocationNext)

	// POST /2018-06-01/runtime/invocation/{requestId}/response
	// POST /2018-06-01/runtime/invocation/{requestId}/error
	mux.HandleFunc("/2018-06-01/runtime/invocation/", s.handleInvocationResponse)

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Runtime API server error: %v", err)
		}
	}()
}

// handleInvocationNext returns the next invocation (our payload).
// After first invocation is consumed, subsequent requests will hang until we get a response.
func (s *Server) handleInvocationNext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	if s.served {
		// Lambda is polling for next invocation after finishing the first one
		// We want single-shot behavior, so just block until done
		s.mu.Unlock()
		<-s.done // Block until we're done
		http.Error(w, "No more invocations", http.StatusGone)
		return
	}
	s.served = true
	s.mu.Unlock()

	// Set required headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Lambda-Runtime-Aws-Request-Id", "mock-request-id")
	w.Header().Set("Lambda-Runtime-Deadline-Ms", "9999999999999")
	w.Header().Set("Lambda-Runtime-Invoked-Function-Arn", "arn:aws:lambda:us-east-1:000000000000:function:test")
	w.Header().Set("Lambda-Runtime-Trace-Id", "Root=mock-trace-id")

	w.WriteHeader(http.StatusOK)
	bytesWritten, _ := w.Write(s.payload)
	log.Printf("[Runtime API] Sent %d bytes payload", bytesWritten)
}

// handleInvocationResponse handles both response and error endpoints.
func (s *Server) handleInvocationResponse(w http.ResponseWriter, r *http.Request) {
	log.Printf("[Runtime API] POST %s from %s", r.URL.Path, r.RemoteAddr)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if this is an error endpoint (ends with "/error")
	urlPath := r.URL.Path
	if len(urlPath) >= 6 && urlPath[len(urlPath)-6:] == "/error" {
		s.handleInvocationError(w, r)
		return
	}

	// Read the full response body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	log.Printf("[Runtime API] Received %d bytes: %s", len(bodyBytes), string(bodyBytes))

	s.mu.Lock()
	s.response = bodyBytes
	s.mu.Unlock()

	w.WriteHeader(http.StatusAccepted)
	s.signal()
}

// handleInvocationError processes Lambda error responses.
func (s *Server) handleInvocationError(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read error response", http.StatusInternalServerError)
		return
	}

	var errorPayload map[string]any
	if err := json.Unmarshal(bodyBytes, &errorPayload); err != nil {
		http.Error(w, "Invalid error payload", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.err = fmt.Errorf("lambda error: %v", errorPayload)
	s.mu.Unlock()

	w.WriteHeader(http.StatusAccepted)
	s.signal()
}

// signal closes the done channel once.
func (s *Server) signal() {
	select {
	case <-s.done:
	default:
		close(s.done)
		log.Printf("[Runtime API] Signaled done")
	}
}

// Wait blocks until the Lambda sends a buffered response or the channel is closed.
func (s *Server) Wait() ([]byte, error) {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.response, s.err
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
