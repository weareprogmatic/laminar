package invoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weareprogmatic/laminar/internal/config"
)

// makeService creates a minimal ServiceConfig backed by a shell script binary.
func makeService(t *testing.T, name, script string) config.ServiceConfig {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lambda")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to create test binary: %v", err)
	}
	return config.ServiceConfig{Name: name, Binary: bin, Timeout: 5}
}

// runtimeRespond is a shell snippet that polls invocation/next then posts a response.
const runtimeRespond = "#!/bin/sh\n" +
	"RUNTIME_API=\"${AWS_LAMBDA_RUNTIME_API}\"\n" +
	"curl -s \"http://${RUNTIME_API}/2018-06-01/runtime/invocation/next\" > /dev/null\n" +
	"curl -s -X POST \"http://${RUNTIME_API}/2018-06-01/runtime/invocation/mock-request-id/response\" \\\n" +
	"  -H \"Content-Type: application/json\" \\\n" +
	"  -d '%s'\n"

func TestParseFunctionName(t *testing.T) {
	tests := []struct {
		path   string
		want   string
		wantOK bool
	}{
		{"/2015-03-31/functions/my-function/invocations", "my-function", true},
		{"/2015-03-31/functions/my-function:v1/invocations", "my-function", true},
		{"/2015-03-31/functions/my-function:$LATEST/invocations", "my-function", true},
		{"/2015-03-31/functions/arn:aws:lambda:us-east-1:123:function:my-fn/invocations", "my-fn", true},
		{"/2015-03-31/functions//invocations", "", false},
		{"/wrong/path", "", false},
		{"/2015-03-31/functions/my-function", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := parseFunctionName(tt.path)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("name = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewServer(t *testing.T) {
	srv, err := NewServer([]config.ServiceConfig{})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	if srv.Port() == 0 {
		t.Errorf("Port() = 0, want non-zero")
	}
	want := fmt.Sprintf("127.0.0.1:%d", srv.Port())
	if srv.Addr() != want {
		t.Errorf("Addr() = %q, want %q", srv.Addr(), want)
	}
}

func TestInvokeRequestResponse(t *testing.T) {
	script := fmt.Sprintf(runtimeRespond, `{"statusCode":200,"body":"hello from lambda"}`)
	svc := makeService(t, "my-function", script)

	srv, err := NewServer([]config.ServiceConfig{svc})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()
	srv.Start()
	time.Sleep(50 * time.Millisecond)

	url := fmt.Sprintf("http://%s/2015-03-31/functions/my-function/invocations", srv.Addr())
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(`{"key":"value"}`))
	if err != nil {
		t.Fatalf("POST error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.Header.Get("X-Amz-Function-Error") != "" {
		t.Errorf("unexpected X-Amz-Function-Error header")
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("body decode error = %v", err)
	}
	if body["body"] != "hello from lambda" {
		t.Errorf("body[body] = %v, want hello from lambda", body["body"])
	}
}

func TestInvokeAsync(t *testing.T) {
	markerFile := filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\n" +
		"RUNTIME_API=\"${AWS_LAMBDA_RUNTIME_API}\"\n" +
		"curl -s \"http://${RUNTIME_API}/2018-06-01/runtime/invocation/next\" > /dev/null\n" +
		"touch " + markerFile + "\n" +
		"curl -s -X POST \"http://${RUNTIME_API}/2018-06-01/runtime/invocation/mock-request-id/response\" -d 'done'\n"

	svc := makeService(t, "async-fn", script)
	srv, err := NewServer([]config.ServiceConfig{svc})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()
	srv.Start()
	time.Sleep(50 * time.Millisecond)

	url := fmt.Sprintf("http://%s/2015-03-31/functions/async-fn/invocations", srv.Addr())
	req, _ := http.NewRequest("POST", url, bytes.NewBufferString(`{}`))
	req.Header.Set("X-Amz-Invocation-Type", "Event")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(markerFile); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("async Lambda did not run within 4 seconds")
}

func TestInvokeUnknownFunction(t *testing.T) {
	srv, err := NewServer([]config.ServiceConfig{})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()
	srv.Start()
	time.Sleep(50 * time.Millisecond)

	url := fmt.Sprintf("http://%s/2015-03-31/functions/unknown/invocations", srv.Addr())
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("POST error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestInvokeMethodNotAllowed(t *testing.T) {
	srv, err := NewServer([]config.ServiceConfig{})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()
	srv.Start()
	time.Sleep(50 * time.Millisecond)

	url := fmt.Sprintf("http://%s/2015-03-31/functions/my-fn/invocations", srv.Addr())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestInvokeCaseInsensitiveName(t *testing.T) {
	script := fmt.Sprintf(runtimeRespond, `"ok"`)
	svc := makeService(t, "MyService", script)

	srv, err := NewServer([]config.ServiceConfig{svc})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()
	srv.Start()
	time.Sleep(50 * time.Millisecond)

	url := fmt.Sprintf("http://%s/2015-03-31/functions/myservice/invocations", srv.Addr())
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("POST error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (case-insensitive name match failed)", resp.StatusCode)
	}
}

func TestInvokeWithQualifier(t *testing.T) {
	script := fmt.Sprintf(runtimeRespond, `"qualified"`)
	svc := makeService(t, "my-fn", script)

	srv, err := NewServer([]config.ServiceConfig{svc})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()
	srv.Start()
	time.Sleep(50 * time.Millisecond)

	url := fmt.Sprintf("http://%s/2015-03-31/functions/my-fn:$LATEST/invocations", srv.Addr())
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("POST error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("qualifier status = %d, want 200 (qualifier should be stripped)", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type = %q, want application/json", resp.Header.Get("Content-Type"))
	}
}
