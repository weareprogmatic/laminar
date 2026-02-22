package response

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// LambdaResponse represents an AWS Lambda structured response.
type LambdaResponse struct {
	StatusCode      int               `json:"statusCode"`
	Headers         map[string]string `json:"headers,omitempty"`
	Body            string            `json:"body"`
	IsBase64Encoded bool              `json:"isBase64Encoded,omitempty"`
	Cookies         []string          `json:"cookies,omitempty"`
}

// Parse attempts to parse Lambda structured response JSON from binary output.
// Returns nil with no error if the output is not structured Lambda format.
func Parse(output []byte) (*LambdaResponse, error) {
	if len(output) == 0 {
		return nil, fmt.Errorf("empty response from binary")
	}

	var resp LambdaResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		// Not valid JSON for Lambda response, return nil to trigger raw fallback
		return nil, nil //nolint:nilerr
	}

	if resp.StatusCode == 0 {
		return nil, nil
	}

	if resp.StatusCode < 100 || resp.StatusCode > 599 {
		return nil, fmt.Errorf("invalid status code %d (must be 100-599)", resp.StatusCode)
	}

	if resp.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 body: %w", err)
		}
		resp.Body = string(decoded)
		resp.IsBase64Encoded = false
	}

	return &resp, nil
}
