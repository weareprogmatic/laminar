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
		name             string
		cors             []string
		origin           string
		allowHeaders     []string
		exposeHeaders    []string
		maxAge           int
		allowCredentials bool
		wantOrigin       string
		wantMethods      bool
		wantCredentials  bool
		wantAllowHeaders string
		wantExposeHdrs   string
		wantMaxAge       string
	}{
		{
			name:             "wildcard cors with credentials",
			cors:             []string{"*"},
			origin:           "http://example.com",
			allowHeaders:     []string{"Content-Type", "Authorization"},
			exposeHeaders:    []string{"X-Request-Id"},
			maxAge:           3600,
			allowCredentials: true,
			wantOrigin:       "http://example.com",
			wantMethods:      true,
			wantCredentials:  true,
			wantAllowHeaders: "Content-Type, Authorization",
			wantExposeHdrs:   "X-Request-Id",
			wantMaxAge:       "3600",
		},
		{
			name:             "wildcard cors without credentials",
			cors:             []string{"*"},
			origin:           "http://example.com",
			allowHeaders:     []string{"Content-Type"},
			allowCredentials: false,
			wantOrigin:       "http://example.com",
			wantMethods:      true,
			wantCredentials:  false,
			wantAllowHeaders: "Content-Type",
			wantExposeHdrs:   "",
			wantMaxAge:       "",
		},
		{
			name:             "specific origin allowed",
			cors:             []string{"http://example.com"},
			origin:           "http://example.com",
			allowHeaders:     []string{"X-Api-Key"},
			maxAge:           7200,
			allowCredentials: false,
			wantOrigin:       "http://example.com",
			wantMethods:      true,
			wantCredentials:  false,
			wantAllowHeaders: "X-Api-Key",
			wantMaxAge:       "7200",
		},
		{
			name:             "specific origin not allowed",
			cors:             []string{"http://example.com"},
			origin:           "http://evil.com",
			wantOrigin:       "",
			wantMethods:      false,
			wantCredentials:  false,
			wantAllowHeaders: "",
		},
		{
			name:             "no cors config",
			cors:             []string{},
			origin:           "http://example.com",
			wantOrigin:       "",
			wantMethods:      false,
			wantCredentials:  false,
			wantAllowHeaders: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ServiceConfig{
				Name:             "test",
				Port:             8080,
				Cors:             tt.cors,
				Methods:          []string{"GET", "POST", "OPTIONS"},
				AllowHeaders:     tt.allowHeaders,
				ExposeHeaders:    tt.exposeHeaders,
				MaxAge:           tt.maxAge,
				AllowCredentials: tt.allowCredentials,
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
			if allowOrigin != tt.wantOrigin {
				t.Errorf("Access-Control-Allow-Origin = %v, want %v", allowOrigin, tt.wantOrigin)
			}

			allowMethods := w.Header().Get("Access-Control-Allow-Methods")
			hasMethods := allowMethods != ""
			if hasMethods != tt.wantMethods {
				t.Errorf("Has Access-Control-Allow-Methods = %v, want %v", hasMethods, tt.wantMethods)
			}

			allowCredentials := w.Header().Get("Access-Control-Allow-Credentials")
			hasCredentials := allowCredentials == "true"
			if hasCredentials != tt.wantCredentials {
				t.Errorf("Access-Control-Allow-Credentials = %v, want credentials=%v", allowCredentials, tt.wantCredentials)
			}

			allowHeaders := w.Header().Get("Access-Control-Allow-Headers")
			if allowHeaders != tt.wantAllowHeaders {
				t.Errorf("Access-Control-Allow-Headers = %v, want %v", allowHeaders, tt.wantAllowHeaders)
			}

			exposeHeaders := w.Header().Get("Access-Control-Expose-Headers")
			if exposeHeaders != tt.wantExposeHdrs {
				t.Errorf("Access-Control-Expose-Headers = %v, want %v", exposeHeaders, tt.wantExposeHdrs)
			}

			maxAge := w.Header().Get("Access-Control-Max-Age")
			if maxAge != tt.wantMaxAge {
				t.Errorf("Access-Control-Max-Age = %v, want %v", maxAge, tt.wantMaxAge)
			}

			// Verify Referrer-Policy is only set when CORS is enabled
			referrerPolicy := w.Header().Get("Referrer-Policy")
			wantReferrer := ""
			if len(tt.cors) > 0 {
				wantReferrer = "no-referrer"
			}
			if referrerPolicy != wantReferrer {
				t.Errorf("Referrer-Policy = %v, want %v", referrerPolicy, wantReferrer)
			}
		})
	}
}

func TestCORSMiddlewareOPTIONS(t *testing.T) {
	t.Run("options intercepted when cors enabled", func(t *testing.T) {
		cfg := config.ServiceConfig{
			Name: "test", Port: 8080, Cors: []string{"http://example.com"},
		}
		srv := New(cfg)
		called := false
		handler := srv.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))
		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		req.Header.Set("Origin", "http://example.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
		if called {
			t.Error("next handler should NOT be called for OPTIONS when CORS is enabled")
		}
	})

	t.Run("options passed through when cors disabled", func(t *testing.T) {
		cfg := config.ServiceConfig{Name: "test", Port: 8080}
		srv := New(cfg)
		called := false
		handler := srv.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}))
		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		req.Header.Set("Origin", "http://example.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if !called {
			t.Error("next handler SHOULD be called for OPTIONS when CORS is disabled")
		}
		if w.Header().Get("Referrer-Policy") != "" {
			t.Error("Referrer-Policy should not be set when CORS is disabled")
		}
	})
}

func TestMatchCORSOrigin(t *testing.T) {
	tests := []struct {
		name   string
		cors   []string
		origin string
		want   string
	}{
		{"wildcard with origin", []string{"*"}, "http://example.com", "http://example.com"},
		{"wildcard no origin", []string{"*"}, "", "*"},
		{"exact match", []string{"http://example.com"}, "http://example.com", "http://example.com"},
		{"no match", []string{"http://example.com"}, "http://evil.com", ""},
		{"multiple origins first match", []string{"http://a.com", "http://b.com"}, "http://b.com", "http://b.com"},
		{"empty cors list", []string{}, "http://example.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New(config.ServiceConfig{Name: "test", Port: 8080, Cors: tt.cors})
			got := srv.matchCORSOrigin(tt.origin)
			if got != tt.want {
				t.Errorf("matchCORSOrigin(%q) = %q, want %q", tt.origin, got, tt.want)
			}
		})
	}
}

func TestSetCORSHeaders(t *testing.T) {
	tests := []struct {
		name             string
		responseOrigin   string
		methods          []string
		allowHeaders     []string
		exposeHeaders    []string
		maxAge           int
		allowCredentials bool
		wantOrigin       string
		wantCredentials  string
		wantMethods      string
		wantAllowHdrs    string
		wantExposeHdrs   string
		wantMaxAge       string
	}{
		{
			name:             "all fields set",
			responseOrigin:   "http://example.com",
			methods:          []string{"GET", "POST"},
			allowHeaders:     []string{"Content-Type", "X-Api-Key"},
			exposeHeaders:    []string{"X-Request-Id"},
			maxAge:           3600,
			allowCredentials: true,
			wantOrigin:       "http://example.com",
			wantCredentials:  "true",
			wantMethods:      "GET, POST",
			wantAllowHdrs:    "Content-Type, X-Api-Key",
			wantExposeHdrs:   "X-Request-Id",
			wantMaxAge:       "3600",
		},
		{
			name:           "minimal - origin only",
			responseOrigin: "http://example.com",
			wantOrigin:     "http://example.com",
		},
		{
			name:           "zero maxage not set",
			responseOrigin: "http://example.com",
			maxAge:         0,
			wantOrigin:     "http://example.com",
			wantMaxAge:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New(config.ServiceConfig{
				Name:             "test",
				Port:             8080,
				Methods:          tt.methods,
				AllowHeaders:     tt.allowHeaders,
				ExposeHeaders:    tt.exposeHeaders,
				MaxAge:           tt.maxAge,
				AllowCredentials: tt.allowCredentials,
			})
			w := httptest.NewRecorder()
			srv.setCORSHeaders(w, tt.responseOrigin)

			if got := w.Header().Get("Access-Control-Allow-Origin"); got != tt.wantOrigin {
				t.Errorf("Allow-Origin = %q, want %q", got, tt.wantOrigin)
			}
			if got := w.Header().Get("Access-Control-Allow-Credentials"); got != tt.wantCredentials {
				t.Errorf("Allow-Credentials = %q, want %q", got, tt.wantCredentials)
			}
			if got := w.Header().Get("Access-Control-Allow-Methods"); got != tt.wantMethods {
				t.Errorf("Allow-Methods = %q, want %q", got, tt.wantMethods)
			}
			if got := w.Header().Get("Access-Control-Allow-Headers"); got != tt.wantAllowHdrs {
				t.Errorf("Allow-Headers = %q, want %q", got, tt.wantAllowHdrs)
			}
			if got := w.Header().Get("Access-Control-Expose-Headers"); got != tt.wantExposeHdrs {
				t.Errorf("Expose-Headers = %q, want %q", got, tt.wantExposeHdrs)
			}
			if got := w.Header().Get("Access-Control-Max-Age"); got != tt.wantMaxAge {
				t.Errorf("Max-Age = %q, want %q", got, tt.wantMaxAge)
			}
		})
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
		AllowHeaders: []string{"Content-Type"},
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
