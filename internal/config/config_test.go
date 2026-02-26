package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			content: `[{
				"name": "test-service",
				"port": 8080,
				"binary": "./testdata/test-binary",
				"cors": ["*"],
				"methods": ["GET", "POST"]
			}]`,
			wantErr: false,
		},
		{
			name:    "empty file",
			content: "[]",
			wantErr: true,
			errMsg:  "no services defined",
		},
		{
			name:    "invalid json",
			content: "{not valid json}",
			wantErr: true,
			errMsg:  "failed to parse",
		},
		{
			name: "missing name",
			content: `[{
				"port": 8080,
				"binary": "./testdata/test-binary"
			}]`,
			wantErr: true,
			errMsg:  "service name cannot be empty",
		},
		{
			name: "invalid port",
			content: `[{
				"name": "test",
				"port": 99999,
				"binary": "./testdata/test-binary"
			}]`,
			wantErr: true,
			errMsg:  "out of range",
		},
		{
			name: "binary not found",
			content: `[{
				"name": "test",
				"port": 8080,
				"binary": "/nonexistent/binary"
			}]`,
			wantErr: true,
			errMsg:  "not found",
		},
		{
			name: "duplicate ports",
			content: `[
				{
					"name": "service1",
					"port": 8080,
					"binary": "./testdata/test-binary"
				},
				{
					"name": "service2",
					"port": 8080,
					"binary": "./testdata/test-binary"
				}
			]`,
			wantErr: true,
			errMsg:  "duplicate port",
		},
		{
			name: "invalid http method",
			content: `[{
				"name": "test",
				"port": 8080,
				"binary": "./testdata/test-binary",
				"methods": ["INVALID"]
			}]`,
			wantErr: true,
			errMsg:  "invalid HTTP method",
		},
		{
			name: "invalid response mode",
			content: `[{
				"name": "test",
				"port": 8080,
				"binary": "./testdata/test-binary",
				"response_mode": "invalid"
			}]`,
			wantErr: true,
			errMsg:  "response_mode must be",
		},
		{
			name: "env file not found",
			content: `[{
				"name": "test",
				"port": 8080,
				"binary": "./testdata/test-binary",
				"env_file": "/nonexistent/env.txt"
			}]`,
			wantErr: true,
			errMsg:  "env_file",
		},
		{
			name: "max_age too large",
			content: `[{
				"name": "test",
				"port": 8080,
				"binary": "./testdata/test-binary",
				"max_age": 100000
			}]`,
			wantErr: true,
			errMsg:  "max_age must be between 0 and 86400",
		},
		{
			name: "max_age negative",
			content: `[{
				"name": "test",
				"port": 8080,
				"binary": "./testdata/test-binary",
				"max_age": -1
			}]`,
			wantErr: true,
			errMsg:  "max_age must be between 0 and 86400",
		},
		{
			name: "valid CORS config with all fields",
			content: `[{
				"name": "test",
				"port": 8080,
				"binary": "./testdata/test-binary",
				"cors": ["*"],
				"allow_headers": ["Content-Type", "Authorization"],
				"expose_headers": ["X-Request-Id"],
				"max_age": 3600,
				"allow_credentials": true
			}]`,
			wantErr: false,
		},
		{
			name: "debug_port out of range",
			content: `[{
				"name": "test",
				"port": 8080,
				"binary": "./testdata/test-binary",
				"debug_port": 99999
			}]`,
			wantErr: true,
			errMsg:  "debug_port 99999 is out of range",
		},
		{
			name: "debug_port same as service port",
			content: `[{
				"name": "test",
				"port": 8080,
				"binary": "./testdata/test-binary",
				"debug_port": 8080
			}]`,
			wantErr: true,
			errMsg:  "debug_port 8080 cannot be the same as service port",
		},
		{
			name: "valid debug_port",
			content: `[{
				"name": "test",
				"port": 8080,
				"binary": "./testdata/test-binary",
				"debug_port": 2345
			}]`,
			wantErr: false,
		},
	}

	setupTestBinary := func(t *testing.T) {
		testDataDir := "testdata"
		os.RemoveAll(testDataDir)
		if err := os.MkdirAll(testDataDir, 0755); err != nil {
			t.Fatalf("Failed to create testdata dir: %v", err)
		}
		binaryPath := filepath.Join(testDataDir, "test-binary")
		if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\necho test"), 0755); err != nil {
			t.Fatalf("Failed to create test binary: %v", err)
		}
		t.Cleanup(func() {
			os.RemoveAll(testDataDir)
		})
	}

	setupTestBinary(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write temp config: %v", err)
			}

			cfg, err := Load(tmpFile)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Load() expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Load() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Load() unexpected error = %v", err)
				}
				if len(cfg.Services) == 0 {
					t.Errorf("Load() returned empty services")
				}
			}
		})
	}
}

func TestLoadGlobalSecrets(t *testing.T) {
	testDataDir := "testdata"
	os.MkdirAll(testDataDir, 0755)                                                     //nolint:errcheck
	os.WriteFile(filepath.Join(testDataDir, "test-binary"), []byte("#!/bin/sh"), 0755) //nolint:errcheck
	t.Cleanup(func() { os.RemoveAll(testDataDir) })

	t.Run("new object format with global secrets", func(t *testing.T) {
		content := `{
			"services": [{"name":"svc","port":8080,"binary":"./testdata/test-binary"}],
			"secrets": {"my-key": "my-value", "other-key": "other-value"}
		}`
		tmpFile := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(tmpFile, []byte(content), 0644) //nolint:errcheck
		cfg, err := Load(tmpFile)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Secrets["my-key"] != "my-value" {
			t.Errorf("Secrets[my-key] = %q, want %q", cfg.Secrets["my-key"], "my-value")
		}
		if cfg.Secrets["other-key"] != "other-value" {
			t.Errorf("Secrets[other-key] = %q, want %q", cfg.Secrets["other-key"], "other-value")
		}
	})

	t.Run("legacy array format merges per-service secrets", func(t *testing.T) {
		content := `[{"name":"svc","port":8080,"binary":"./testdata/test-binary","secrets":{"a":"1"}}]`
		tmpFile := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(tmpFile, []byte(content), 0644) //nolint:errcheck
		cfg, err := Load(tmpFile)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Secrets["a"] != "1" {
			t.Errorf("Secrets[a] = %q, want %q", cfg.Secrets["a"], "1")
		}
	})

	t.Run("global secrets override per-service secrets for same key", func(t *testing.T) {
		content := `{
			"services": [{"name":"svc","port":8080,"binary":"./testdata/test-binary","secrets":{"key":"from-service"}}],
			"secrets": {"key": "from-global"}
		}`
		tmpFile := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(tmpFile, []byte(content), 0644) //nolint:errcheck
		cfg, err := Load(tmpFile)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Secrets["key"] != "from-global" {
			t.Errorf("Secrets[key] = %q, want global value %q", cfg.Secrets["key"], "from-global")
		}
	})
}

func TestApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    ServiceConfig
		expected ServiceConfig
	}{
		{
			name: "apply all defaults",
			input: ServiceConfig{
				Name:   "test",
				Port:   8080,
				Binary: "/bin/test",
			},
			expected: ServiceConfig{
				Name:         "test",
				Port:         8080,
				Binary:       "/bin/test",
				ResponseMode: "lambda",
				Timeout:      30,
				Methods:      []string{},
			},
		},
		{
			name: "preserve custom values",
			input: ServiceConfig{
				Name:         "test",
				Port:         8080,
				Binary:       "/bin/test",
				ResponseMode: "raw",
				Timeout:      60,
				Methods:      []string{"get", "post"},
			},
			expected: ServiceConfig{
				Name:         "test",
				Port:         8080,
				Binary:       "/bin/test",
				ResponseMode: "raw",
				Timeout:      60,
				Methods:      []string{"GET", "POST"},
			},
		},
		{
			name: "apply CORS defaults when CORS enabled",
			input: ServiceConfig{
				Name:   "test",
				Port:   8080,
				Binary: "/bin/test",
				Cors:   []string{"*"},
			},
			expected: ServiceConfig{
				Name:         "test",
				Port:         8080,
				Binary:       "/bin/test",
				Cors:         []string{"*"},
				ResponseMode: "lambda",
				Timeout:      30,
				Methods:      []string{},
				AllowHeaders: []string{"Content-Type"},
			},
		},
		{
			name: "preserve custom CORS headers",
			input: ServiceConfig{
				Name:         "test",
				Port:         8080,
				Binary:       "/bin/test",
				Cors:         []string{"*"},
				AllowHeaders: []string{"Authorization", "X-Api-Key"},
			},
			expected: ServiceConfig{
				Name:         "test",
				Port:         8080,
				Binary:       "/bin/test",
				Cors:         []string{"*"},
				ResponseMode: "lambda",
				Timeout:      30,
				Methods:      []string{},
				AllowHeaders: []string{"Authorization", "X-Api-Key"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyDefaults(&tt.input)

			if tt.input.ResponseMode != tt.expected.ResponseMode {
				t.Errorf("ResponseMode = %v, want %v", tt.input.ResponseMode, tt.expected.ResponseMode)
			}
			if tt.input.Timeout != tt.expected.Timeout {
				t.Errorf("Timeout = %v, want %v", tt.input.Timeout, tt.expected.Timeout)
			}
			for i, method := range tt.input.Methods {
				if method != tt.expected.Methods[i] {
					t.Errorf("Methods[%d] = %v, want %v", i, method, tt.expected.Methods[i])
				}
			}
			if len(tt.input.AllowHeaders) != len(tt.expected.AllowHeaders) {
				t.Errorf("AllowHeaders = %v, want %v", tt.input.AllowHeaders, tt.expected.AllowHeaders)
			}
			for i, header := range tt.input.AllowHeaders {
				if i < len(tt.expected.AllowHeaders) && header != tt.expected.AllowHeaders[i] {
					t.Errorf("AllowHeaders[%d] = %v, want %v", i, header, tt.expected.AllowHeaders[i])
				}
			}
		})
	}
}

func TestIsValidHTTPMethod(t *testing.T) {
	validMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "TRACE", "CONNECT"}
	for _, method := range validMethods {
		if !isValidHTTPMethod(method) {
			t.Errorf("isValidHTTPMethod(%s) = false, want true", method)
		}
	}

	invalidMethods := []string{"INVALID", "get", "FOO", ""}
	for _, method := range invalidMethods {
		if isValidHTTPMethod(method) {
			t.Errorf("isValidHTTPMethod(%s) = true, want false", method)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
