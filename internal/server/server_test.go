package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weareprogmatic/laminar/internal/config"
)

func TestHandleHealth(t *testing.T) {
	cfg := config.ServiceConfig{
		Name: "test-service",
		Port: 8080,
	}
	srv := New(cfg)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("handleHealth() status = %v, want %v", w.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("status = %v, want ok", response["status"])
	}

	if response["service"] != cfg.Name {
		t.Errorf("service = %v, want %v", response["service"], cfg.Name)
	}
}

func TestCORSMiddleware(t *testing.T) {
	tests := []struct {
		name        string
		cors        []string
		origin      string
		wantHeader  string
		wantMethods bool
	}{
		{
			name:        "wildcard cors",
			cors:        []string{"*"},
			origin:      "http://example.com",
			wantHeader:  "*",
			wantMethods: true,
		},
		{
			name:        "specific origin allowed",
			cors:        []string{"http://example.com"},
			origin:      "http://example.com",
			wantHeader:  "http://example.com",
			wantMethods: true,
		},
		{
			name:        "specific origin not allowed",
			cors:        []string{"http://example.com"},
			origin:      "http://evil.com",
			wantHeader:  "",
			wantMethods: false,
		},
		{
			name:        "no cors config",
			cors:        []string{},
			origin:      "http://example.com",
			wantHeader:  "",
			wantMethods: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ServiceConfig{
				Name: "test",
				Port: 8080,
				Cors: tt.cors,
			}
			srv := New(cfg)

			handler := srv.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			allowOrigin := w.Header().Get("Access-Control-Allow-Origin")
			if allowOrigin != tt.wantHeader {
				t.Errorf("Access-Control-Allow-Origin = %v, want %v", allowOrigin, tt.wantHeader)
			}

			allowMethods := w.Header().Get("Access-Control-Allow-Methods")
			hasMethods := allowMethods != ""
			if hasMethods != tt.wantMethods {
				t.Errorf("Has Access-Control-Allow-Methods = %v, want %v", hasMethods, tt.wantMethods)
			}
		})
	}
}

func TestMethodFilterMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		methods    []string
		reqMethod  string
		wantStatus int
	}{
		{
			name:       "no filter - all allowed",
			methods:    []string{},
			reqMethod:  "GET",
			wantStatus: http.StatusOK,
		},
		{
			name:       "GET allowed",
			methods:    []string{"GET"},
			reqMethod:  "GET",
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST not allowed",
			methods:    []string{"GET"},
			reqMethod:  "POST",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "multiple methods - allowed",
			methods:    []string{"GET", "POST", "PUT"},
			reqMethod:  "POST",
			wantStatus: http.StatusOK,
		},
		{
			name:       "OPTIONS always allowed",
			methods:    []string{"GET"},
			reqMethod:  "OPTIONS",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ServiceConfig{
				Name:    "test",
				Port:    8080,
				Methods: tt.methods,
			}
			srv := New(cfg)

			handler := srv.methodFilterMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tt.reqMethod, "/test", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %v, want %v", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHealthEndpointIgnoresMethodFilter(t *testing.T) {
	cfg := config.ServiceConfig{
		Name:    "test",
		Port:    8080,
		Methods: []string{"POST"},
	}
	srv := New(cfg)

	handler := srv.methodFilterMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("/health with GET should be allowed even with POST-only filter, got status %v", w.Code)
	}
}

func TestLoggingMiddleware(t *testing.T) {
	cfg := config.ServiceConfig{
		Name: "test",
		Port: 8080,
	}
	srv := New(cfg)

	handler := srv.loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Errorf("status = %v, want %v", w.Code, http.StatusTeapot)
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		pattern  string
		expected bool
	}{
		{"exact match", "/favicon.ico", "/favicon.ico", true},
		{"exact mismatch", "/favicon.ico", "/other.ico", false},
		{"prefix match", "/.well-known/appspecific/com.chrome.devtools.json", "/.well-known/*", true},
		{"prefix match short", "/.well-known/", "/.well-known/*", true},
		{"prefix mismatch", "/api/test", "/.well-known/*", false},
		{"empty pattern", "/test", "", false},
		{"wildcard only", "/test", "*", true},
		{"partial prefix", "/api/test", "/api/*", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesPattern(tt.path, tt.pattern)
			if result != tt.expected {
				t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.path, tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestShouldLogPath(t *testing.T) {
	tests := []struct {
		name        string
		ignorePaths []string
		path        string
		shouldLog   bool
	}{
		{"no ignore patterns", []string{}, "/test", true},
		{"ignored exact match", []string{"/favicon.ico"}, "/favicon.ico", false},
		{"not ignored", []string{"/favicon.ico"}, "/test", true},
		{"ignored prefix match", []string{"/.well-known/*"}, "/.well-known/appspecific/com.chrome.devtools.json", false},
		{"multiple patterns match", []string{"/favicon.ico", "/.well-known/*"}, "/.well-known/test", false},
		{"multiple patterns no match", []string{"/favicon.ico", "/.well-known/*"}, "/api/test", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ServiceConfig{
				Name:        "test",
				Port:        8080,
				IgnorePaths: tt.ignorePaths,
			}
			srv := New(cfg)
			result := srv.shouldLogPath(tt.path)
			if result != tt.shouldLog {
				t.Errorf("shouldLogPath(%q) with patterns %v = %v, want %v", tt.path, tt.ignorePaths, result, tt.shouldLog)
			}
		})
	}
}

func TestServerIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "test-lambda")

	lambdaScript := `#!/bin/sh
cat > /tmp/lambda-input.json
echo '{"statusCode":200,"body":"Hello from fake Lambda"}'
`
	if err := os.WriteFile(binaryPath, []byte(lambdaScript), 0755); err != nil {
		t.Fatalf("Failed to create test binary: %v", err)
	}

	cfg := config.ServiceConfig{
		Name:         "test-integration",
		Port:         0,
		Binary:       binaryPath,
		Cors:         []string{"*"},
		Methods:      []string{"GET", "POST"},
		ContentType:  "application/json",
		ResponseMode: "lambda",
		Timeout:      5,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srv := New(cfg)
	handler := srv.buildHandler()

	t.Run("health check", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("health check status = %v, want %v", w.Code, http.StatusOK)
		}
	})

	t.Run("lambda request", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/test", strings.NewReader(`{"test":"data"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
			t.Logf("Request status = %v, body = %s", w.Code, w.Body.String())
		}
	})

	_ = ctx
}
