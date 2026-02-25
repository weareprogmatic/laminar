package config

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// ServiceConfig holds the configuration for a single Lambda service.
type ServiceConfig struct {
	Name             string            `json:"name"`
	Port             int               `json:"port"`
	Binary           string            `json:"binary"`
	Cors             []string          `json:"cors"`
	Methods          []string          `json:"methods"`
	ContentTypes     []string          `json:"content_types"`
	AllowHeaders     []string          `json:"allow_headers,omitempty"`
	ExposeHeaders    []string          `json:"expose_headers,omitempty"`
	MaxAge           int               `json:"max_age,omitempty"`
	AllowCredentials bool              `json:"allow_credentials,omitempty"`
	EnvFile          string            `json:"env_file,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	WorkingDir       string            `json:"working_dir,omitempty"`
	ResponseMode     string            `json:"response_mode,omitempty"`
	Timeout          int               `json:"timeout,omitempty"`
	DebugPort        int               `json:"debug_port,omitempty"`
	Watch            *bool             `json:"watch,omitempty"`
	IgnorePaths      []string          `json:"ignore_paths,omitempty"`
}

// Load reads and validates a Laminar configuration file.
func Load(path string) ([]ServiceConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var services []ServiceConfig
	if err := json.Unmarshal(data, &services); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("no services defined in %s", path)
	}

	for i := range services {
		normalizePlatformPaths(&services[i])
	}

	if err := validate(services); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	for i := range services {
		applyDefaults(&services[i])
	}

	return services, nil
}

func validate(services []ServiceConfig) error {
	ports := make(map[int]string)

	for _, svc := range services {
		if strings.TrimSpace(svc.Name) == "" {
			return fmt.Errorf("service name cannot be empty")
		}

		if svc.Port < 1 || svc.Port > 65535 {
			return fmt.Errorf("service %s: port %d is out of range (1-65535)", svc.Name, svc.Port)
		}

		if existing, exists := ports[svc.Port]; exists {
			return fmt.Errorf("duplicate port %d used by services %s and %s", svc.Port, existing, svc.Name)
		}
		ports[svc.Port] = svc.Name

		if strings.TrimSpace(svc.Binary) == "" {
			return fmt.Errorf("service %s: binary path cannot be empty", svc.Name)
		}

		if _, err := os.Stat(svc.Binary); err != nil {
			return fmt.Errorf("service %s: binary %s not found: %w", svc.Name, svc.Binary, err)
		}

		for _, method := range svc.Methods {
			method = strings.ToUpper(method)
			if !isValidHTTPMethod(method) {
				return fmt.Errorf("service %s: invalid HTTP method %s", svc.Name, method)
			}
		}

		if err := validateModes(svc); err != nil {
			return err
		}

		if svc.EnvFile != "" {
			if _, err := os.Stat(svc.EnvFile); err != nil {
				return fmt.Errorf("service %s: env_file %s not found: %w", svc.Name, svc.EnvFile, err)
			}
		}

		if svc.MaxAge < 0 || svc.MaxAge > 86400 {
			return fmt.Errorf("service %s: max_age must be between 0 and 86400 seconds, got %d", svc.Name, svc.MaxAge)
		}

		if err := validateDebugPort(svc); err != nil {
			return err
		}
	}

	return nil
}

// validateDebugPort checks that debug_port is valid and doesn't conflict with the service port.
func validateDebugPort(svc ServiceConfig) error {
	if svc.DebugPort == 0 {
		return nil
	}
	if svc.DebugPort < 1 || svc.DebugPort > 65535 {
		return fmt.Errorf("service %s: debug_port %d is out of range (1-65535)", svc.Name, svc.DebugPort)
	}
	if svc.DebugPort == svc.Port {
		return fmt.Errorf("service %s: debug_port %d cannot be the same as service port", svc.Name, svc.DebugPort)
	}
	return nil
}

// validateModes checks response_mode for a single service.
func validateModes(svc ServiceConfig) error {
	if svc.ResponseMode != "" && svc.ResponseMode != "lambda" && svc.ResponseMode != "raw" {
		return fmt.Errorf("service %s: response_mode must be 'lambda' or 'raw', got %s", svc.Name, svc.ResponseMode)
	}
	return nil
}

func applyDefaults(svc *ServiceConfig) {
	if len(svc.ContentTypes) == 0 {
		svc.ContentTypes = []string{"application/json"}
	}

	if svc.ResponseMode == "" {
		svc.ResponseMode = "lambda"
	}

	if svc.Timeout <= 0 {
		svc.Timeout = 30
	}

	// Apply CORS defaults when CORS is enabled
	if len(svc.Cors) > 0 && len(svc.AllowHeaders) == 0 {
		svc.AllowHeaders = []string{"Content-Type"}
	}

	for i := range svc.Methods {
		svc.Methods[i] = strings.ToUpper(svc.Methods[i])
	}

	// Watch defaults to true.
	if svc.Watch == nil {
		v := true
		svc.Watch = &v
	}
}

func isValidHTTPMethod(method string) bool {
	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true,
		"PATCH": true, "HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
	}
	return validMethods[method]
}

// normalizePlatformPaths adjusts file paths for the current OS.
// On Windows, binary paths that omit the .exe extension are resolved
// automatically so users don't need platform-specific laminar.json files.
func normalizePlatformPaths(svc *ServiceConfig) {
	if runtime.GOOS != "windows" {
		return
	}
	if svc.Binary != "" && !strings.HasSuffix(svc.Binary, ".exe") {
		// Prefer the path as-is if it already exists (e.g. cross-compiled binary
		// deliberately named without .exe). Fall back to .exe otherwise.
		if _, err := os.Stat(svc.Binary); os.IsNotExist(err) {
			svc.Binary += ".exe"
		}
	}
}
