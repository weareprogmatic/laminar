package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ServiceConfig holds the configuration for a single Lambda service.
type ServiceConfig struct {
	Name         string            `json:"name"`
	Port         int               `json:"port"`
	Binary       string            `json:"binary"`
	Cors         []string          `json:"cors"`
	Methods      []string          `json:"methods"`
	ContentType  string            `json:"content_type,omitempty"`
	EnvFile      string            `json:"env_file,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	WorkingDir   string            `json:"working_dir,omitempty"`
	ResponseMode string            `json:"response_mode,omitempty"`
	Timeout      int               `json:"timeout,omitempty"`
	IgnorePaths  []string          `json:"ignore_paths,omitempty"`
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

		if svc.ResponseMode != "" && svc.ResponseMode != "lambda" && svc.ResponseMode != "raw" {
			return fmt.Errorf("service %s: response_mode must be 'lambda' or 'raw', got %s", svc.Name, svc.ResponseMode)
		}

		if svc.EnvFile != "" {
			if _, err := os.Stat(svc.EnvFile); err != nil {
				return fmt.Errorf("service %s: env_file %s not found: %w", svc.Name, svc.EnvFile, err)
			}
		}
	}

	return nil
}

func applyDefaults(svc *ServiceConfig) {
	if svc.ContentType == "" {
		svc.ContentType = "application/json"
	}

	if svc.ResponseMode == "" {
		svc.ResponseMode = "lambda"
	}

	if svc.Timeout <= 0 {
		svc.Timeout = 30
	}

	for i := range svc.Methods {
		svc.Methods[i] = strings.ToUpper(svc.Methods[i])
	}
}

func isValidHTTPMethod(method string) bool {
	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true,
		"PATCH": true, "HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
	}
	return validMethods[method]
}
