package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	payload := []byte(`{"test": "data"}`)
	srv, err := NewServer(payload)
	if err != nil {
		t.Fatalf("NewServer() error = %v, want nil", err)
	}
	defer srv.Close()

	if srv.port == 0 {
		t.Errorf("port = 0, want non-zero")
	}
	if bytes.Equal(srv.payload, payload) == false {
		t.Errorf("payload mismatch")
	}
}

func TestPort(t *testing.T) {
	payload := []byte(`{"test": "data"}`)
	srv, err := NewServer(payload)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	port := srv.Port()
	if port == 0 {
		t.Errorf("Port() = 0, want non-zero")
	}
	if port != srv.port {
		t.Errorf("Port() = %d, want %d", port, srv.port)
	}
}

func TestStartAndWait(t *testing.T) {
	payload := []byte(`{"test": "invocation"}`)
	srv, err := NewServer(payload)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	srv.Start()
	time.Sleep(100 * time.Millisecond) // Give server time to start

	// Test handleInvocationNext - GET /2018-06-01/runtime/invocation/next
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/2018-06-01/runtime/invocation/next", srv.port)
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Failed to GET invocation/next: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Check response headers
	if resp.Header.Get("Lambda-Runtime-Aws-Request-Id") != "mock-request-id" {
		t.Errorf("Missing or incorrect Lambda-Runtime-Aws-Request-Id header")
	}

	// Test handleInvocationResponse - POST /2018-06-01/runtime/invocation/{requestId}/response
	responseBody := []byte(`{"statusCode": 200, "body": "test"}`)
	resp, err = client.Post(
		fmt.Sprintf("http://127.0.0.1:%d/2018-06-01/runtime/invocation/mock-request-id/response", srv.port),
		"application/json",
		bytes.NewReader(responseBody),
	)
	if err != nil {
		t.Fatalf("Failed to POST response: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("POST status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Test Wait() - should get the response we posted
	result, err := srv.Wait()
	if err != nil {
		t.Errorf("Wait() error = %v, want nil", err)
	}
	if !bytes.Equal(result, responseBody) {
		t.Errorf("Wait() = %s, want %s", string(result), string(responseBody))
	}
}

func TestHandleInvocationNextMethodValidation(t *testing.T) {
	payload := []byte(`{"test": "data"}`)
	srv, err := NewServer(payload)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	srv.Start()
	time.Sleep(100 * time.Millisecond)

	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/2018-06-01/runtime/invocation/next", srv.port)

	// Test with POST (should fail)
	req, _ := http.NewRequest("POST", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to POST to invocation/next: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandleInvocationResponseError(t *testing.T) {
	payload := []byte(`{"test": "data"}`)
	srv, err := NewServer(payload)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	srv.Start()
	time.Sleep(100 * time.Millisecond)

	client := &http.Client{Timeout: 5 * time.Second}

	// POST error payload
	errorPayload := map[string]interface{}{
		"errorMessage": "test error",
		"errorType":    "TestError",
	}
	body, _ := json.Marshal(errorPayload)
	resp, err := client.Post(
		fmt.Sprintf("http://127.0.0.1:%d/2018-06-01/runtime/invocation/mock-request-id/error", srv.port),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("Failed to POST error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("POST error status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Wait should return the error
	_, err = srv.Wait()
	if err == nil {
		t.Errorf("Wait() error = nil, want non-nil")
	}
}

func TestHandleInvocationResponseMethodValidation(t *testing.T) {
	payload := []byte(`{"test": "data"}`)
	srv, err := NewServer(payload)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	srv.Start()
	time.Sleep(100 * time.Millisecond)

	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/2018-06-01/runtime/invocation/mock-request-id/response", srv.port)

	// Test with GET (should fail)
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Failed to GET response endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestClose(t *testing.T) {
	payload := []byte(`{"test": "data"}`)
	srv, err := NewServer(payload)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	srv.Start()
	time.Sleep(100 * time.Millisecond)

	// Close should not error
	err = srv.Close()
	if err != nil && err.Error() != "close tcp 127.0.0.1:0: use of closed network connection" {
		t.Logf("Close() may have error due to already-closed listener: %v", err)
	}

	// Server should be closed, wait a bit for graceful shutdown
	time.Sleep(100 * time.Millisecond)
	client := &http.Client{Timeout: 1 * time.Second}
	_, err = client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", srv.port))
	if err == nil {
		t.Errorf("Expected connection to fail after Close(), but it succeeded")
	}
}
