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

			services, err := Load(tmpFile)

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
				if len(services) == 0 {
					t.Errorf("Load() returned empty services")
				}
			}
		})
	}
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
				ContentType:  "application/json",
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
				ContentType:  "text/plain",
				ResponseMode: "raw",
				Timeout:      60,
				Methods:      []string{"get", "post"},
			},
			expected: ServiceConfig{
				Name:         "test",
				Port:         8080,
				Binary:       "/bin/test",
				ContentType:  "text/plain",
				ResponseMode: "raw",
				Timeout:      60,
				Methods:      []string{"GET", "POST"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyDefaults(&tt.input)

			if tt.input.ContentType != tt.expected.ContentType {
				t.Errorf("ContentType = %v, want %v", tt.input.ContentType, tt.expected.ContentType)
			}
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
