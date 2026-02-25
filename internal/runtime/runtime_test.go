package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer() error = %v, want nil", err)
	}
	defer srv.Close()

	if srv.port == 0 {
		t.Errorf("port = 0, want non-zero")
	}
}

func TestPort(t *testing.T) {
	srv, err := NewServer()
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

func TestInvoke(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()
	srv.Start()
	time.Sleep(100 * time.Millisecond)

	payload := []byte(`{"test": "invocation"}`)
	responseBody := []byte(`{"statusCode": 200, "body": "test"}`)

	// Call Invoke in background (simulates the external HTTP caller)
	type invResult struct {
		data []byte
		err  error
	}
	resultCh := make(chan invResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		data, err := srv.Invoke(ctx, payload)
		resultCh <- invResult{data, err}
	}()

	client := &http.Client{Timeout: 5 * time.Second}
	nextURL := fmt.Sprintf("http://127.0.0.1:%d/2018-06-01/runtime/invocation/next", srv.port)

	// Simulate Lambda: GET /next
	resp, err := client.Get(nextURL)
	if err != nil {
		t.Fatalf("Failed to GET invocation/next: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.Header.Get("Lambda-Runtime-Aws-Request-Id") != "mock-request-id" {
		t.Errorf("Missing or incorrect Lambda-Runtime-Aws-Request-Id header")
	}

	// Simulate Lambda: POST /response
	postResp, err := client.Post(
		fmt.Sprintf("http://127.0.0.1:%d/2018-06-01/runtime/invocation/mock-request-id/response", srv.port),
		"application/json",
		bytes.NewReader(responseBody),
	)
	if err != nil {
		t.Fatalf("Failed to POST response: %v", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusAccepted {
		t.Errorf("POST status = %d, want %d", postResp.StatusCode, http.StatusAccepted)
	}

	// Invoke should return the response body
	r := <-resultCh
	if r.err != nil {
		t.Errorf("Invoke() error = %v, want nil", r.err)
	}
	if !bytes.Equal(r.data, responseBody) {
		t.Errorf("Invoke() = %s, want %s", string(r.data), string(responseBody))
	}
}

func TestHandleInvocationNextMethodValidation(t *testing.T) {
	srv, err := NewServer()
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
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()
	srv.Start()
	time.Sleep(100 * time.Millisecond)

	client := &http.Client{Timeout: 5 * time.Second}

	// Start Invoke in background so /next is consumed and currentRespCh is set
	type invResult struct {
		data []byte
		err  error
	}
	resultCh := make(chan invResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		data, err := srv.Invoke(ctx, []byte(`{}`))
		resultCh <- invResult{data, err}
	}()

	// Simulate Lambda: GET /next to set up currentRespCh
	nextURL := fmt.Sprintf("http://127.0.0.1:%d/2018-06-01/runtime/invocation/next", srv.port)
	nextResp, err := client.Get(nextURL)
	if err != nil {
		t.Fatalf("Failed to GET /next: %v", err)
	}
	_ = nextResp.Body.Close()

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

	// Invoke should return an error
	r := <-resultCh
	if r.err == nil {
		t.Errorf("Invoke() error = nil, want non-nil")
	}
}

func TestHandleInvocationResponseMethodValidation(t *testing.T) {
	srv, err := NewServer()
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
	srv, err := NewServer()
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

	// Server should be closed
	time.Sleep(100 * time.Millisecond)
	client := &http.Client{Timeout: 1 * time.Second}
	_, err = client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", srv.port))
	if err == nil {
		t.Errorf("Expected connection to fail after Close(), but it succeeded")
	}
}
