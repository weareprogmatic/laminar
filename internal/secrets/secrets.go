package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

const (
	targetGetSecretValue = "secretsmanager.GetSecretValue"
	targetDescribeSecret = "secretsmanager.DescribeSecret"
)

// Server is a mock AWS Secrets Manager API that returns values configured in laminar.json.
type Server struct {
	secrets  map[string]string // keyed by secret name/ARN
	listener net.Listener
	server   *http.Server
	port     int
}

// NewServer creates and binds a new secrets Server on a random port.
// secrets is the global secrets map from the Laminar config.
func NewServer(secrets map[string]string) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("failed to bind secrets manager API: %w", err)
	}

	if secrets == nil {
		secrets = make(map[string]string)
	}

	s := &Server{
		secrets:  secrets,
		listener: listener,
		port:     listener.Addr().(*net.TCPAddr).Port,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	s.server = &http.Server{
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	return s, nil
}

// Start begins serving requests in a background goroutine.
func (s *Server) Start() {
	go func() {
		if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[Secrets API] Server error: %v", err)
		}
	}()
}

// Port returns the TCP port the server is listening on.
func (s *Server) Port() int {
	return s.port
}

// Addr returns the full host:port address of the server.
func (s *Server) Addr() string {
	return fmt.Sprintf("127.0.0.1:%d", s.port)
}

// Close gracefully shuts down the server.
func (s *Server) Close() error {
	if s.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}
	return s.listener.Close()
}

// handleRequest routes incoming SDK requests based on the X-Amz-Target header.
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	target := r.Header.Get("X-Amz-Target")
	log.Printf("[Secrets API] %s", target)

	switch target {
	case targetGetSecretValue:
		s.handleGetSecretValue(w, r)
	case targetDescribeSecret:
		s.handleDescribeSecret(w, r)
	default:
		s.writeError(w, http.StatusBadRequest, "InvalidActionException",
			fmt.Sprintf("Action %q is not supported by the mock Secrets Manager API", target))
	}
}

// secretIDRequest is the shared request body shape for actions that take a SecretId.
type secretIDRequest struct {
	SecretId string `json:"SecretId"` //nolint:revive,stylecheck
}

// getSecretValueResponse mirrors the real GetSecretValue response shape.
type getSecretValueResponse struct {
	ARN           string   `json:"ARN"`
	CreatedDate   float64  `json:"CreatedDate"`
	Name          string   `json:"Name"`
	SecretString  string   `json:"SecretString"`
	VersionId     string   `json:"VersionId"` //nolint:revive,stylecheck
	VersionStages []string `json:"VersionStages"`
}

// describeSecretResponse mirrors the real DescribeSecret response shape (subset).
type describeSecretResponse struct {
	ARN                string              `json:"ARN"`
	Name               string              `json:"Name"`
	Description        string              `json:"Description"`
	LastChangedDate    float64             `json:"LastChangedDate"`
	LastAccessedDate   float64             `json:"LastAccessedDate"`
	VersionIdsToStages map[string][]string `json:"VersionIdsToStages"`
}

// apiError mirrors the AWS error response shape.
type apiError struct {
	Type    string `json:"__type"`
	Message string `json:"message"`
}

func (s *Server) handleGetSecretValue(w http.ResponseWriter, r *http.Request) {
	name, ok := s.parseSecretID(w, r)
	if !ok {
		return
	}

	value, found := s.secrets[name]
	if !found {
		s.writeError(w, http.StatusBadRequest, "ResourceNotFoundException",
			fmt.Sprintf("Secrets Manager can't find the specified secret: %q. "+
				"Add it to the \"secrets\" map in laminar.json.", name))
		return
	}

	resp := getSecretValueResponse{
		ARN:           mockARN(name),
		CreatedDate:   float64(time.Now().Unix()),
		Name:          name,
		SecretString:  value,
		VersionId:     "mock-version-id",
		VersionStages: []string{"AWSCURRENT"},
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDescribeSecret(w http.ResponseWriter, r *http.Request) {
	name, ok := s.parseSecretID(w, r)
	if !ok {
		return
	}

	if _, found := s.secrets[name]; !found {
		s.writeError(w, http.StatusBadRequest, "ResourceNotFoundException",
			fmt.Sprintf("Secrets Manager can't find the specified secret: %q.", name))
		return
	}

	now := float64(time.Now().Unix())
	resp := describeSecretResponse{
		ARN:              mockARN(name),
		Name:             name,
		Description:      "Managed by Laminar (local mock)",
		LastChangedDate:  now,
		LastAccessedDate: now,
		VersionIdsToStages: map[string][]string{
			"mock-version-id": {"AWSCURRENT"},
		},
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// parseSecretID reads and parses the request body, extracting SecretId.
// Writes an error response and returns false on failure.
func (s *Server) parseSecretID(w http.ResponseWriter, r *http.Request) (string, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "InvalidRequestException", "Failed to read request body")
		return "", false
	}

	var req secretIDRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "InvalidRequestException", "Invalid JSON request body")
		return "", false
	}

	if req.SecretId == "" {
		s.writeError(w, http.StatusBadRequest, "InvalidRequestException", "SecretId must not be empty")
		return "", false
	}

	return req.SecretId, true
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, status int, errType, message string) {
	log.Printf("[Secrets API] %s: %s", errType, message)
	s.writeJSON(w, status, apiError{Type: errType, Message: message})
}

// mockARN builds a plausible-looking Secrets Manager ARN for local use.
func mockARN(name string) string {
	return fmt.Sprintf("arn:aws:secretsmanager:us-east-1:000000000000:secret:%s-mock", name)
}
