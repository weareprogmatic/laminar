package server

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
	"github.com/weareprogmatic/laminar/internal/payload"
	"github.com/weareprogmatic/laminar/internal/response"
	"github.com/weareprogmatic/laminar/internal/runner"
)

// Server represents an HTTP server for a Lambda service.
type Server struct {
	config config.ServiceConfig
	server *http.Server
	warm   *runner.WarmLambda // nil only in unit tests that call New() directly
}

// New creates a new Server instance.
func New(cfg config.ServiceConfig) *Server {
	return &Server{config: cfg}
}

// invokeLambda runs the Lambda with the given payload. Uses the warm process if available,
// otherwise falls back to a per-request runner.Run (used in tests via New() directly).
func (s *Server) invokeLambda(ctx context.Context, payload []byte) ([]byte, error) {
	if s.warm != nil {
		return s.warm.Invoke(ctx, payload)
	}
	return runner.Run(ctx, s.config.Binary, s.config.EnvFile, s.config.Env, s.config.WorkingDir, s.config.Timeout, s.config.DebugPort, payload)
}

// invokeLambdaStream runs the Lambda and returns a streaming response.
func (s *Server) invokeLambdaStream(ctx context.Context, payload []byte) (*runner.StreamResp, func(), error) {
	if s.warm != nil {
		return s.warm.InvokeStream(ctx, payload)
	}
	return runner.RunStream(ctx, s.config.Binary, s.config.EnvFile, s.config.Env, s.config.WorkingDir, s.config.Timeout, s.config.DebugPort, payload)
}

// listenWithRetry tries to bind the TCP port up to maxAttempts times, waiting
// between each attempt. This handles the brief window where the OS hasn't yet
// released the port from a previous laminar process.
func listenWithRetry(ctx context.Context, port int) (net.Listener, error) {
	const maxAttempts = 20
	const retryDelay = 500 * time.Millisecond
	addr := fmt.Sprintf(":%d", port)
	for i := range maxAttempts {
		l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
		if err == nil {
			return l, nil
		}
		if i < maxAttempts-1 {
			log.Printf("Port %d busy (attempt %d/%d), retrying in %s…", port, i+1, maxAttempts, retryDelay)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay):
			}
		}
	}
	return nil, fmt.Errorf("port %d still in use after %d attempts", port, maxAttempts)
}

// Start starts the HTTP server and blocks until context is cancelled.
func Start(ctx context.Context, cfg config.ServiceConfig) error {
	srv := New(cfg)

	warm, err := runner.StartWarm(ctx, cfg.Binary, cfg.EnvFile, cfg.Env, cfg.WorkingDir, cfg.DebugPort, true)
	if err != nil {
		return fmt.Errorf("failed to start lambda process: %w", err)
	}
	srv.warm = warm
	defer warm.Close()

	handler := srv.buildHandler()

	listener, err := listenWithRetry(ctx, cfg.Port)
	if err != nil {
		return fmt.Errorf("failed to bind port for %s: %w", cfg.Name, err)
	}

	srv.server = &http.Server{
		Handler:           handler,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down server for %s: %v", cfg.Name, err)
		}
	}()

	log.Printf("Starting %s on :%d -> %s", cfg.Name, cfg.Port, cfg.Binary)
	if err := srv.server.Serve(listener); err != http.ErrServerClosed {
		return fmt.Errorf("server error for %s: %w", cfg.Name, err)
	}

	return nil
}

func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/", s.handleRequest)

	handler := s.corsMiddleware(mux)
	handler = s.loggingMiddleware(handler)

	return handler
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": s.config.Name,
	})
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	lambdaPayload, err := payload.MapToLambda(r)
	if err != nil {
		log.Printf("[%s] Error creating payload: %v", s.config.Name, err)
		http.Error(w, fmt.Sprintf("Error creating payload: %v", err), http.StatusBadRequest)
		return
	}

	payloadBytes, err := json.Marshal(lambdaPayload)
	if err != nil {
		log.Printf("[%s] Error marshaling payload: %v", s.config.Name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if s.config.InvokeMode == "RESPONSE_STREAM" {
		s.handleStreamRequest(w, r, payloadBytes)
		return
	}

	output, err := s.invokeLambda(ctx, payloadBytes)
	if err != nil {
		log.Printf("[%s] Error executing binary: %v", s.config.Name, err)
		http.Error(w, fmt.Sprintf("Error executing binary: %v", err), http.StatusInternalServerError)
		return
	}

	if s.config.ResponseMode == "lambda" {
		s.handleLambdaResponse(w, output)
	} else {
		s.handleRawResponse(w, output)
	}
}

// handleStreamRequest proxies a RESPONSE_STREAM Lambda invocation to the HTTP client.
// For SSE (text/event-stream) it streams body bytes with chunked transfer encoding.
// For all other content types it buffers the full body and writes a normal response,
// which prevents hangs for endpoints that return short JSON payloads from a Lambda
// registered with invoke_mode: "RESPONSE_STREAM".
func (s *Server) handleStreamRequest(w http.ResponseWriter, r *http.Request, payloadBytes []byte) {
	ctx := r.Context()
	resp, done, err := s.invokeLambdaStream(ctx, payloadBytes)
	if err != nil {
		log.Printf("[%s] Error executing streaming binary: %v", s.config.Name, err)
		http.Error(w, fmt.Sprintf("Error executing binary: %v", err), http.StatusInternalServerError)
		return
	}
	defer done()

	if isStreamingContentType(resp.Headers) {
		s.writeStreamingResponse(w, resp)
	} else {
		s.writeBufferedStreamResponse(w, resp)
	}
}

// isStreamingContentType returns true when the prelude headers indicate a
// genuinely streaming response (e.g. SSE) that should be forwarded with
// chunked transfer encoding instead of being buffered.
func isStreamingContentType(headers map[string]string) bool {
	for key, value := range headers {
		if strings.EqualFold(key, "content-type") && strings.HasPrefix(value, "text/event-stream") {
			return true
		}
	}
	return false
}

// writeStreamingResponse streams the body to the client using chunked transfer encoding.
func (s *Server) writeStreamingResponse(w http.ResponseWriter, resp *runner.StreamResp) {
	for key, value := range resp.Headers {
		w.Header().Set(key, value)
	}
	for _, cookie := range resp.Cookies {
		w.Header().Add("Set-Cookie", cookie)
	}

	statusCode := resp.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)

	flusher, canFlush := w.(http.Flusher)
	// Flush immediately after WriteHeader to force chunked transfer encoding.
	// Without this, Go's net/http may sniff a Content-Length if the first Read
	// returns all the data before Flush is called in the loop.
	if canFlush {
		flusher.Flush()
	}
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				log.Printf("[%s] Stream write error: %v", s.config.Name, writeErr)
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			log.Printf("[%s] Stream read error: %v", s.config.Name, readErr)
			return
		}
	}
}

// writeBufferedStreamResponse reads the entire body from a streaming Lambda response,
// then writes it as a normal HTTP response with Content-Length. This is used for
// non-SSE endpoints (e.g. JSON APIs) that share a Lambda registered with
// invoke_mode: "RESPONSE_STREAM".
func (s *Server) writeBufferedStreamResponse(w http.ResponseWriter, resp *runner.StreamResp) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] Error reading stream body: %v", s.config.Name, err)
		http.Error(w, "Error reading response body", http.StatusInternalServerError)
		return
	}

	for key, value := range resp.Headers {
		w.Header().Set(key, value)
	}
	for _, cookie := range resp.Cookies {
		w.Header().Add("Set-Cookie", cookie)
	}

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}

	statusCode := resp.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

func (s *Server) handleLambdaResponse(w http.ResponseWriter, output []byte) {
	lambdaResp, err := response.Parse(output)
	if err != nil {
		log.Printf("[%s] Error parsing Lambda response: %v", s.config.Name, err)
		http.Error(w, fmt.Sprintf("Error parsing response: %v", err), http.StatusInternalServerError)
		return
	}

	if lambdaResp == nil {
		s.handleRawResponse(w, output)
		return
	}

	for key, value := range lambdaResp.Headers {
		w.Header().Set(key, value)
	}

	for _, cookie := range lambdaResp.Cookies {
		w.Header().Add("Set-Cookie", cookie)
	}

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}

	w.WriteHeader(lambdaResp.StatusCode)
	_, _ = w.Write([]byte(lambdaResp.Body))
}

func (s *Server) handleRawResponse(w http.ResponseWriter, output []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output)
}

//nolint:gocognit
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.config.Cors) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		// Set Referrer-Policy to allow cross-origin requests
		w.Header().Set("Referrer-Policy", "no-referrer")

		origin := r.Header.Get("Origin")
		if responseOrigin := s.matchCORSOrigin(origin); responseOrigin != "" {
			s.setCORSHeaders(w, responseOrigin)
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// matchCORSOrigin returns the response origin value if the given origin matches a configured CORS origin,
// or an empty string if there is no match.
func (s *Server) matchCORSOrigin(origin string) string {
	for _, allowedOrigin := range s.config.Cors {
		if allowedOrigin == "*" {
			if origin != "" {
				return origin
			}
			return "*"
		}
		if allowedOrigin == origin {
			return origin
		}
	}
	return ""
}

// setCORSHeaders sets Access-Control-* response headers for a matched origin.
func (s *Server) setCORSHeaders(w http.ResponseWriter, responseOrigin string) {
	w.Header().Set("Access-Control-Allow-Origin", responseOrigin)

	if s.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	if len(s.config.Methods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(s.config.Methods, ", "))
	}

	if len(s.config.AllowHeaders) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(s.config.AllowHeaders, ", "))
	}

	if len(s.config.ExposeHeaders) > 0 {
		w.Header().Set("Access-Control-Expose-Headers", strings.Join(s.config.ExposeHeaders, ", "))
	}

	if s.config.MaxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", s.config.MaxAge))
	}
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		duration := time.Since(start)

		log.Printf("[%s] %s %s %d %v", s.config.Name, r.Method, r.URL.Path, wrapped.statusCode, duration)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush delegates to the underlying ResponseWriter so streaming responses
// can use chunked transfer encoding through the logging middleware wrapper.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
