// Package runtime implements a mock AWS Lambda Runtime API server.
// Lambda functions communicate with this server to handle invocations locally.
// See: https://docs.aws.amazon.com/lambda/latest/dg/runtimes-api.html
//
//nolint:revive
package runtime

import (
	"bufio"
	"bytes"
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

type invocationReq struct {
	payload []byte
	respCh  chan invocationResp
}

type invocationResp struct {
	body   []byte
	stream *invocationStreamResp
	err    error
}

// invocationStreamResp carries a streaming Lambda response.
// The runtime handler keeps the request body open until done is closed,
// so Body remains readable for as long as the caller needs it.
type invocationStreamResp struct {
	resp StreamResp
	done chan struct{}
}

// StreamPrelude is the JSON metadata that precedes the 8-null-byte separator
// in a Lambda RESPONSE_STREAM invocation.
type StreamPrelude struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Cookies    []string          `json:"cookies"`
}

// StreamResp is the decoded streaming response returned by InvokeStream.
type StreamResp struct {
	StatusCode int
	Headers    map[string]string
	Cookies    []string
	Body       io.Reader
}

// Server implements the AWS Lambda Runtime API, supporting multiple sequential invocations.
// The Lambda process stays alive between requests, calling GET /next each time.
type Server struct {
	listener      net.Listener
	server        *http.Server
	port          int
	invokeCh      chan invocationReq
	closeCh       chan struct{}
	closeOnce     sync.Once
	mu            sync.Mutex
	currentRespCh chan invocationResp
}

// NewServer creates a new Lambda Runtime API server on a random port.
func NewServer() (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	return &Server{
		listener: listener,
		port:     listener.Addr().(*net.TCPAddr).Port,
		invokeCh: make(chan invocationReq),
		closeCh:  make(chan struct{}),
	}, nil
}

// Port returns the port the runtime API server is listening on.
func (s *Server) Port() int {
	return s.port
}

// Start starts the runtime API server in the background.
func (s *Server) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/2018-06-01/runtime/invocation/next", s.handleInvocationNext)
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

// Invoke sends payload to the Lambda and blocks until the Lambda posts a response.
// The Lambda process stays alive between calls; it calls GET /next each invocation.
func (s *Server) Invoke(ctx context.Context, payload []byte) ([]byte, error) {
	respCh := make(chan invocationResp, 1)
	select {
	case s.invokeCh <- invocationReq{payload: payload, respCh: respCh}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closeCh:
		return nil, fmt.Errorf("runtime server closed")
	}
	select {
	case resp := <-respCh:
		if resp.stream != nil {
			// Lambda sent a streaming response without invoke_mode configured.
			// Drain and release the handler to avoid a goroutine leak.
			_, _ = io.Copy(io.Discard, resp.stream.resp.Body)
			close(resp.stream.done)
			return nil, fmt.Errorf("lambda sent a streaming response but invoke_mode is not RESPONSE_STREAM")
		}
		return resp.body, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closeCh:
		return nil, fmt.Errorf("runtime server closed")
	}
}

// InvokeStream sends payload to the Lambda and returns a StreamResp for incremental
// consumption. The caller MUST call the returned done function after fully reading
// Body — this unblocks the runtime handler and lets the Lambda proceed.
func (s *Server) InvokeStream(ctx context.Context, payload []byte) (*StreamResp, func(), error) {
	respCh := make(chan invocationResp, 1)
	select {
	case s.invokeCh <- invocationReq{payload: payload, respCh: respCh}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-s.closeCh:
		return nil, nil, fmt.Errorf("runtime server closed")
	}
	select {
	case resp := <-respCh:
		if resp.err != nil {
			return nil, nil, resp.err
		}
		if resp.stream == nil {
			// Lambda sent a buffered response; wrap it so the caller still works.
			return &StreamResp{
				StatusCode: http.StatusOK,
				Body:       bytes.NewReader(resp.body),
			}, func() {}, nil
		}
		done := func() { close(resp.stream.done) }
		return &resp.stream.resp, done, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-s.closeCh:
		return nil, nil, fmt.Errorf("runtime server closed")
	}
}

// handleInvocationNext blocks until Invoke enqueues a payload, then streams it to the Lambda.
func (s *Server) handleInvocationNext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req invocationReq
	select {
	case req = <-s.invokeCh:
	case <-s.closeCh:
		http.Error(w, "No more invocations", http.StatusGone)
		return
	case <-r.Context().Done():
		return
	}

	s.mu.Lock()
	s.currentRespCh = req.respCh
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Lambda-Runtime-Aws-Request-Id", "mock-request-id")
	w.Header().Set("Lambda-Runtime-Deadline-Ms", "9999999999999")
	w.Header().Set("Lambda-Runtime-Invoked-Function-Arn", "arn:aws:lambda:us-east-1:000000000000:function:test")
	w.Header().Set("Lambda-Runtime-Trace-Id", "Root=mock-trace-id")
	w.WriteHeader(http.StatusOK)
	bytesWritten, _ := w.Write(req.payload)
	log.Printf("[Runtime API] Sent %d bytes payload", bytesWritten)
}

// handleInvocationResponse handles both response and error endpoints.
func (s *Server) handleInvocationResponse(w http.ResponseWriter, r *http.Request) {
	log.Printf("[Runtime API] POST %s from %s", r.URL.Path, r.RemoteAddr)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if len(r.URL.Path) >= 6 && r.URL.Path[len(r.URL.Path)-6:] == "/error" {
		s.handleInvocationError(w, r)
		return
	}

	if r.Header.Get("Lambda-Runtime-Function-Response-Mode") == "streaming" ||
		r.Header.Get("Content-Type") == "application/vnd.awslambda.http-integration-response" {
		s.handleStreamingResponse(w, r)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	log.Printf("[Runtime API] Received %d bytes: %s", len(bodyBytes), truncateJSONFields(bodyBytes, 64))

	s.mu.Lock()
	respCh := s.currentRespCh
	s.mu.Unlock()

	if respCh != nil {
		respCh <- invocationResp{body: bodyBytes}
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleStreamingResponse handles a Lambda RESPONSE_STREAM POST.
// It parses the prelude JSON (before the 8-null separator), forwards a StreamResp
// to the waiting InvokeStream caller, then blocks until that caller signals done.
func (s *Server) handleStreamingResponse(w http.ResponseWriter, r *http.Request) {
	prelude, body, err := readStreamPrelude(r.Body)
	if err != nil {
		log.Printf("[Runtime API] Failed to read stream prelude: %v", err)
		http.Error(w, "Failed to read stream prelude", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	respCh := s.currentRespCh
	s.mu.Unlock()

	if respCh == nil {
		_, _ = io.Copy(io.Discard, body)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	done := make(chan struct{})
	respCh <- invocationResp{
		stream: &invocationStreamResp{
			resp: StreamResp{
				StatusCode: prelude.StatusCode,
				Headers:    prelude.Headers,
				Cookies:    prelude.Cookies,
				Body:       body,
			},
			done: done,
		},
	}

	// Keep this handler alive so r.Body stays open and readable by the caller.
	<-done
	w.WriteHeader(http.StatusAccepted)
}

// readStreamPrelude reads r byte-by-byte until the 8-null-byte separator is found,
// parses the preceding bytes as StreamPrelude JSON, and returns a *bufio.Reader
// positioned immediately after the separator for zero-copy streaming of the body.
func readStreamPrelude(r io.Reader) (StreamPrelude, io.Reader, error) {
	const maxPrelude = 64 * 1024 // guard against oversized preludes
	// Use a minimal 1-byte bufio.Reader so it does not pre-read body bytes
	// past the 8-null separator. This ensures the returned io.Reader delivers
	// body data incrementally (enabling chunked transfer encoding downstream)
	// rather than buffering up to 4096 bytes.
	br := bufio.NewReaderSize(r, 1)
	var preludeBytes []byte
	var nullRun int
	for len(preludeBytes)+nullRun < maxPrelude {
		b, err := br.ReadByte()
		if err == io.EOF {
			return StreamPrelude{}, nil, fmt.Errorf("stream ended before 8-null separator")
		}
		if err != nil {
			return StreamPrelude{}, nil, fmt.Errorf("error reading stream prelude: %w", err)
		}
		if b == 0x00 {
			nullRun++
			if nullRun == 8 {
				// Separator complete — br is now positioned at the first body byte.
				var prelude StreamPrelude
				if err := json.Unmarshal(preludeBytes, &prelude); err != nil {
					return StreamPrelude{}, nil, fmt.Errorf("invalid stream prelude JSON: %w", err)
				}
				return prelude, br, nil
			}
		} else {
			// Not a null byte — flush any accumulated partial run into preludeBytes.
			if nullRun > 0 {
				preludeBytes = append(preludeBytes, bytes.Repeat([]byte{0x00}, nullRun)...)
				nullRun = 0
			}
			preludeBytes = append(preludeBytes, b)
		}
	}
	return StreamPrelude{}, nil, fmt.Errorf("stream prelude exceeds %d byte maximum", maxPrelude)
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
	respCh := s.currentRespCh
	s.mu.Unlock()

	if respCh != nil {
		respCh <- invocationResp{err: fmt.Errorf("lambda error: %v", errorPayload)}
	}
	w.WriteHeader(http.StatusAccepted)
}

// truncateJSONFields parses JSON and truncates each string field value to maxBytes bytes.
// If the input is not valid JSON, the raw bytes are returned as-is.
func truncateJSONFields(data []byte, maxBytes int) string {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return string(data)
	}
	for k, v := range m {
		if s, ok := v.(string); ok && len(s) > maxBytes {
			m[k] = s[:maxBytes] + "..."
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return string(data)
	}
	return string(out)
}

// Close gracefully shuts down the server.
func (s *Server) Close() error {
	s.closeOnce.Do(func() { close(s.closeCh) })
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.server.Shutdown(ctx)
	}
	return s.listener.Close()
}
