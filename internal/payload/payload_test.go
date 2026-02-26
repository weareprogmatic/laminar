package payload

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"testing"
)

func TestMapToLambda(t *testing.T) {
	tests := []struct {
		name    string
		req     *http.Request
		wantErr bool
	}{
		{
			name: "simple GET request",
			req: &http.Request{
				Method:     "GET",
				Host:       "localhost:8080",
				URL:        mustParseURL("http://localhost:8080/test?foo=bar"),
				Header:     http.Header{"User-Agent": []string{"test"}},
				Proto:      "HTTP/1.1",
				RemoteAddr: "127.0.0.1:12345",
				Body:       io.NopCloser(bytes.NewReader([]byte{})),
			},
			wantErr: false,
		},
		{
			name: "POST with body",
			req: &http.Request{
				Method:     "POST",
				URL:        mustParseURL("http://localhost:8080/api/users"),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Proto:      "HTTP/1.1",
				RemoteAddr: "127.0.0.1:12345",
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"name":"test"}`))),
			},
			wantErr: false,
		},
		{
			name: "request with cookies",
			req: &http.Request{
				Method:     "GET",
				URL:        mustParseURL("http://localhost:8080/"),
				Header:     http.Header{"Cookie": []string{"session=abc123; token=xyz"}},
				Proto:      "HTTP/1.1",
				RemoteAddr: "192.168.1.1:54321",
				Body:       io.NopCloser(bytes.NewReader([]byte{})),
			},
			wantErr: false,
		},
		{
			name: "request with query parameters",
			req: &http.Request{
				Method:     "GET",
				URL:        mustParseURL("http://localhost:8080/search?q=test&limit=10"),
				Header:     http.Header{},
				Proto:      "HTTP/1.1",
				RemoteAddr: "127.0.0.1:12345",
				Body:       io.NopCloser(bytes.NewReader([]byte{})),
			},
			wantErr: false,
		},
		{
			name: "body size at limit",
			req: &http.Request{
				Method:     "POST",
				URL:        mustParseURL("http://localhost:8080/"),
				Header:     http.Header{},
				Proto:      "HTTP/1.1",
				RemoteAddr: "127.0.0.1:12345",
				Body:       io.NopCloser(bytes.NewReader(make([]byte, MaxBodySize))),
			},
			wantErr: false,
		},
		{
			name: "body exceeds limit",
			req: &http.Request{
				Method:     "POST",
				URL:        mustParseURL("http://localhost:8080/"),
				Header:     http.Header{},
				Proto:      "HTTP/1.1",
				RemoteAddr: "127.0.0.1:12345",
				Body:       io.NopCloser(bytes.NewReader(make([]byte, MaxBodySize+1))),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := MapToLambda(tt.req)

			if tt.wantErr {
				if err == nil {
					t.Errorf("MapToLambda() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("MapToLambda() unexpected error = %v", err)
			}

			if payload.Version != "2.0" {
				t.Errorf("Version = %v, want 2.0", payload.Version)
			}

			if payload.RouteKey != "$default" {
				t.Errorf("RouteKey = %v, want $default", payload.RouteKey)
			}

			if payload.RequestContext.Stage != "$default" {
				t.Errorf("Stage = %v, want $default", payload.RequestContext.Stage)
			}

			if payload.RequestContext.HTTP.Method != tt.req.Method {
				t.Errorf("Method = %v, want %v", payload.RequestContext.HTTP.Method, tt.req.Method)
			}

			if payload.RawPath != tt.req.URL.Path {
				t.Errorf("RawPath = %v, want %v", payload.RawPath, tt.req.URL.Path)
			}
		})
	}
}

func TestSourceIPExtraction(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		wantIP     string
	}{
		{
			name:       "IPv4 with port",
			remoteAddr: "192.168.1.1:12345",
			wantIP:     "192.168.1.1",
		},
		{
			name:       "localhost with port",
			remoteAddr: "127.0.0.1:8080",
			wantIP:     "127.0.0.1",
		},
		{
			name:       "IPv6 with port",
			remoteAddr: "[::1]:8080",
			wantIP:     "[::1]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				Method:     "GET",
				URL:        mustParseURL("http://localhost:8080/"),
				Header:     http.Header{},
				Proto:      "HTTP/1.1",
				RemoteAddr: tt.remoteAddr,
				Body:       io.NopCloser(bytes.NewReader([]byte{})),
			}

			payload, err := MapToLambda(req)
			if err != nil {
				t.Fatalf("MapToLambda() error = %v", err)
			}

			if payload.RequestContext.HTTP.SourceIP != tt.wantIP {
				t.Errorf("SourceIP = %v, want %v", payload.RequestContext.HTTP.SourceIP, tt.wantIP)
			}
		})
	}
}

func TestDomainName(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		wantDomain string
	}{
		{"host with port", "localhost:8080", "localhost:8080"},
		{"host without port", "example.local", "example.local"},
		{"empty host fallback", "", "localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				Method:     "GET",
				Host:       tt.host,
				URL:        mustParseURL("http://localhost:8080/"),
				Header:     http.Header{},
				Proto:      "HTTP/1.1",
				RemoteAddr: "127.0.0.1:12345",
				Body:       io.NopCloser(bytes.NewReader([]byte{})),
			}

			p, err := MapToLambda(req)
			if err != nil {
				t.Fatalf("MapToLambda() error = %v", err)
			}

			if p.RequestContext.DomainName != tt.wantDomain {
				t.Errorf("DomainName = %q, want %q", p.RequestContext.DomainName, tt.wantDomain)
			}
		})
	}
}

func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return u
}
