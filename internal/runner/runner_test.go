package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadEnvFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "simple env vars",
			content: `FOO=bar
BAZ=qux`,
			want: map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name: "with comments and empty lines",
			content: `# Comment
FOO=bar

# Another comment
BAZ=qux`,
			want: map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:    "with quoted values",
			content: `FOO="bar with spaces"`,
			want:    map[string]string{"FOO": "bar with spaces"},
		},
		{
			name:    "with single quotes",
			content: `FOO='bar'`,
			want:    map[string]string{"FOO": "bar"},
		},
		{
			name:    "invalid line",
			content: `INVALID_LINE_WITHOUT_EQUALS`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write temp file: %v", err)
			}

			got, err := loadEnvFile(tmpFile)

			if tt.wantErr {
				if err == nil {
					t.Errorf("loadEnvFile() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("loadEnvFile() unexpected error = %v", err)
			}

			if len(got) != len(tt.want) {
				t.Errorf("loadEnvFile() got %d vars, want %d", len(got), len(tt.want))
			}

			for key, wantVal := range tt.want {
				if gotVal, ok := got[key]; !ok {
					t.Errorf("loadEnvFile() missing key %s", key)
				} else if gotVal != wantVal {
					t.Errorf("loadEnvFile() %s = %v, want %v", key, gotVal, wantVal)
				}
			}
		})
	}
}

func TestBuildEnv(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envFile, []byte("CUSTOM_VAR=test\nAWS_REGION=eu-west-1"), 0644); err != nil {
		t.Fatalf("Failed to write env file: %v", err)
	}

	tests := []struct {
		name        string
		envFile     string
		envVars     map[string]string
		checkVar    string
		checkValue  string
		shouldExist bool
	}{
		{
			name:        "adds LAMINAR_LOCAL",
			envFile:     "",
			envVars:     nil,
			checkVar:    "LAMINAR_LOCAL",
			checkValue:  "true",
			shouldExist: true,
		},
		{
			name:        "adds AWS_REGION default",
			envFile:     "",
			envVars:     nil,
			checkVar:    "AWS_REGION",
			checkValue:  "us-east-1",
			shouldExist: true,
		},
		{
			name:        "loads custom vars from file",
			envFile:     envFile,
			envVars:     nil,
			checkVar:    "CUSTOM_VAR",
			checkValue:  "test",
			shouldExist: true,
		},
		{
			name:        "overrides AWS_REGION from file",
			envFile:     envFile,
			envVars:     nil,
			checkVar:    "AWS_REGION",
			checkValue:  "eu-west-1",
			shouldExist: true,
		},
		{
			name:        "env vars from map",
			envFile:     "",
			envVars:     map[string]string{"MY_VAR": "from_json"},
			checkVar:    "MY_VAR",
			checkValue:  "from_json",
			shouldExist: true,
		},
		{
			name:        "map overrides file",
			envFile:     envFile,
			envVars:     map[string]string{"CUSTOM_VAR": "overridden"},
			checkVar:    "CUSTOM_VAR",
			checkValue:  "overridden",
			shouldExist: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := buildEnv(tt.envFile, tt.envVars, 12345) // Use dummy port for testing
			if err != nil {
				t.Fatalf("buildEnv() error = %v", err)
			}

			found := false
			for _, e := range env {
				if strings.HasPrefix(e, tt.checkVar+"=") {
					found = true
					parts := strings.SplitN(e, "=", 2)
					if len(parts) == 2 && parts[1] != tt.checkValue {
						t.Errorf("buildEnv() %s = %v, want %v", tt.checkVar, parts[1], tt.checkValue)
					}
					break
				}
			}

			if tt.shouldExist && !found {
				t.Errorf("buildEnv() missing %s", tt.checkVar)
			}
		})
	}
}

func TestRun(t *testing.T) {
	setupTestScript := func(t *testing.T, script string) string {
		tmpFile := filepath.Join(t.TempDir(), "test-script")
		if err := os.WriteFile(tmpFile, []byte(script), 0755); err != nil {
			t.Fatalf("Failed to create test script: %v", err)
		}
		return tmpFile
	}

	t.Run("successful execution", func(t *testing.T) {
		// Create a fake Lambda that speaks the Runtime API
		script := `#!/bin/sh
# Fake Lambda that calls Runtime API
RUNTIME_API="${AWS_LAMBDA_RUNTIME_API}"

# Get invocation
curl -s "http://${RUNTIME_API}/2018-06-01/runtime/invocation/next" > /dev/null

# Send response
curl -s -X POST "http://${RUNTIME_API}/2018-06-01/runtime/invocation/mock-request-id/response" \
  -H "Content-Type: application/json" \
  -d 'Hello World'
`
		binary := setupTestScript(t, script)
		ctx := context.Background()

		output, err := Run(ctx, binary, "", nil, "", 5, []byte("test input"))
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}

		if !strings.Contains(string(output), "Hello World") {
			t.Errorf("Run() output = %s, want to contain 'Hello World'", output)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		script := setupTestScript(t, "#!/bin/sh\nsleep 10")
		ctx := context.Background()

		_, err := Run(ctx, script, "", nil, "", 1, []byte(""))
		if err == nil {
			t.Errorf("Run() expected timeout error, got nil")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Errorf("Run() error = %v, want timeout error", err)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		// Create a fake Lambda that takes some time but responds to Runtime API
		script := `#!/bin/sh
RUNTIME_API="${AWS_LAMBDA_RUNTIME_API}"
curl -s "http://${RUNTIME_API}/2018-06-01/runtime/invocation/next" > /dev/null
sleep 10
curl -s -X POST "http://${RUNTIME_API}/2018-06-01/runtime/invocation/mock-request-id/response" -d 'Done'
`
		binary := setupTestScript(t, script)
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		_, err := Run(ctx, binary, "", nil, "", 30, []byte(""))
		if err == nil {
			t.Errorf("Run() expected cancellation error, got nil")
		}
		// Context cancellation will result in timeout error since we kill the process
		if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "cancelled") {
			t.Errorf("Run() error = %v, want cancellation/timeout error", err)
		}
	})

	t.Run("binary not found", func(t *testing.T) {
		ctx := context.Background()
		_, err := Run(ctx, "/nonexistent/binary", "", nil, "", 5, []byte(""))
		if err == nil {
			t.Errorf("Run() expected error for nonexistent binary, got nil")
		}
	})
}

func TestMergeEnv(t *testing.T) {
	base := []string{"FOO=old", "BAR=keep", "BAZ=keep"}
	envVars := map[string]string{
		"FOO": "new",
		"QUX": "added",
	}

	result := mergeEnv(base, envVars)

	checks := map[string]string{
		"FOO": "new",
		"BAR": "keep",
		"BAZ": "keep",
		"QUX": "added",
	}

	for key, want := range checks {
		found := false
		for _, e := range result {
			if strings.HasPrefix(e, key+"=") {
				found = true
				parts := strings.SplitN(e, "=", 2)
				if len(parts) == 2 && parts[1] != want {
					t.Errorf("mergeEnv() %s = %v, want %v", key, parts[1], want)
				}
				break
			}
		}
		if !found {
			t.Errorf("mergeEnv() missing %s", key)
		}
	}
}
