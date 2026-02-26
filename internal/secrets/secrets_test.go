package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewServer(t *testing.T) {
	srv, err := NewServer(map[string]string{"my-secret": "value"})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	if srv.Port() == 0 {
		t.Error("Port() = 0, want non-zero")
	}
	want := fmt.Sprintf("127.0.0.1:%d", srv.Port())
	if srv.Addr() != want {
		t.Errorf("Addr() = %q, want %q", srv.Addr(), want)
	}
}

func TestNewServer_EmptySecrets(t *testing.T) {
	srv, err := NewServer(nil)
	if err != nil {
		t.Fatalf("NewServer(nil) error = %v", err)
	}
	defer srv.Close()

	rr := doRequest(t, srv, targetGetSecretValue, `{"SecretId":"anything"}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

// doRequest calls the handler via httptest.
func doRequest(t *testing.T, srv *Server, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req.Header.Set("X-Amz-Target", target)
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	rr := httptest.NewRecorder()
	srv.handleRequest(rr, req)
	return rr
}

func TestGetSecretValue_Found(t *testing.T) {
	srv, err := NewServer(map[string]string{"my/secret": "s3cret-val"})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	rr := doRequest(t, srv, targetGetSecretValue, `{"SecretId":"my/secret"}`)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp getSecretValueResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Name != "my/secret" {
		t.Errorf("Name = %q, want %q", resp.Name, "my/secret")
	}
	if resp.SecretString != "s3cret-val" {
		t.Errorf("SecretString = %q, want %q", resp.SecretString, "s3cret-val")
	}
	if resp.VersionId != "mock-version-id" {
		t.Errorf("VersionId = %q, want %q", resp.VersionId, "mock-version-id")
	}
	if len(resp.VersionStages) == 0 || resp.VersionStages[0] != "AWSCURRENT" {
		t.Errorf("VersionStages = %v, want [AWSCURRENT]", resp.VersionStages)
	}
}

func TestGetSecretValue_NotFound(t *testing.T) {
	srv, err := NewServer(map[string]string{})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	rr := doRequest(t, srv, targetGetSecretValue, `{"SecretId":"missing-secret"}`)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	var errResp apiError
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Type != "ResourceNotFoundException" {
		t.Errorf("__type = %q, want ResourceNotFoundException", errResp.Type)
	}
}

func TestGetSecretValue_EmptySecretId(t *testing.T) {
	srv, err := NewServer(map[string]string{})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	rr := doRequest(t, srv, targetGetSecretValue, `{"SecretId":""}`)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	var errResp apiError
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Type != "InvalidRequestException" {
		t.Errorf("__type = %q, want InvalidRequestException", errResp.Type)
	}
}

func TestDescribeSecret_Found(t *testing.T) {
	srv, err := NewServer(map[string]string{"my/secret": "val"})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	rr := doRequest(t, srv, targetDescribeSecret, `{"SecretId":"my/secret"}`)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var resp describeSecretResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Name != "my/secret" {
		t.Errorf("Name = %q, want %q", resp.Name, "my/secret")
	}
	if _, ok := resp.VersionIdsToStages["mock-version-id"]; !ok {
		t.Error("VersionIdsToStages missing mock-version-id key")
	}
}

func TestDescribeSecret_NotFound(t *testing.T) {
	srv, err := NewServer(map[string]string{})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	rr := doRequest(t, srv, targetDescribeSecret, `{"SecretId":"missing"}`)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestUnknownTarget(t *testing.T) {
	srv, err := NewServer(map[string]string{})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	rr := doRequest(t, srv, "secretsmanager.CreateSecret", `{}`)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	var errResp apiError
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Type != "InvalidActionException" {
		t.Errorf("__type = %q, want InvalidActionException", errResp.Type)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv, err := NewServer(map[string]string{})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.handleRequest(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestGetSecretValue_JSONSecretValue(t *testing.T) {
	jsonValue := `{"username":"admin","password":"hunter2"}`
	srv, err := NewServer(map[string]string{"db/creds": jsonValue})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close()

	rr := doRequest(t, srv, targetGetSecretValue, `{"SecretId":"db/creds"}`)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp getSecretValueResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.SecretString != jsonValue {
		t.Errorf("SecretString = %q, want %q", resp.SecretString, jsonValue)
	}
}

func TestMockARN(t *testing.T) {
	arn := mockARN("my-app/db-password")
	want := "arn:aws:secretsmanager:us-east-1:000000000000:secret:my-app/db-password-mock"
	if arn != want {
		t.Errorf("mockARN() = %q, want %q", arn, want)
	}
}
