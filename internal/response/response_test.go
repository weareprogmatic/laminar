package response

import (
	"encoding/base64"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		output     []byte
		wantNil    bool
		wantErr    bool
		wantStatus int
		wantBody   string
	}{
		{
			name:       "valid lambda response",
			output:     []byte(`{"statusCode":200,"body":"Hello World"}`),
			wantStatus: 200,
			wantBody:   "Hello World",
		},
		{
			name:       "response with headers",
			output:     []byte(`{"statusCode":201,"headers":{"Content-Type":"application/json"},"body":"{}"}`),
			wantStatus: 201,
			wantBody:   "{}",
		},
		{
			name:       "response with cookies",
			output:     []byte(`{"statusCode":200,"cookies":["session=abc"],"body":"OK"}`),
			wantStatus: 200,
			wantBody:   "OK",
		},
		{
			name:       "base64 encoded body",
			output:     []byte(`{"statusCode":200,"body":"` + base64.StdEncoding.EncodeToString([]byte("Binary Data")) + `","isBase64Encoded":true}`),
			wantStatus: 200,
			wantBody:   "Binary Data",
		},
		{
			name:    "empty response",
			output:  []byte(""),
			wantErr: true,
		},
		{
			name:    "invalid json",
			output:  []byte(`{not valid json}`),
			wantNil: true,
		},
		{
			name:    "raw output without status",
			output:  []byte("This is raw output"),
			wantNil: true,
		},
		{
			name:    "invalid status code too low",
			output:  []byte(`{"statusCode":99,"body":"test"}`),
			wantErr: true,
		},
		{
			name:    "invalid status code too high",
			output:  []byte(`{"statusCode":600,"body":"test"}`),
			wantErr: true,
		},
		{
			name:       "status 500",
			output:     []byte(`{"statusCode":500,"body":"Internal Server Error"}`),
			wantStatus: 500,
			wantBody:   "Internal Server Error",
		},
		{
			name:       "status 404",
			output:     []byte(`{"statusCode":404,"body":"Not Found"}`),
			wantStatus: 404,
			wantBody:   "Not Found",
		},
		{
			name:       "empty body",
			output:     []byte(`{"statusCode":204,"body":""}`),
			wantStatus: 204,
			wantBody:   "",
		},
		{
			name:    "invalid base64",
			output:  []byte(`{"statusCode":200,"body":"not-valid-base64!!!","isBase64Encoded":true}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := Parse(tt.output)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Parse() unexpected error = %v", err)
			}

			if tt.wantNil {
				if resp != nil {
					t.Errorf("Parse() expected nil response, got %+v", resp)
				}
				return
			}

			if resp == nil {
				t.Fatal("Parse() returned nil response, expected valid response")
			}

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("StatusCode = %v, want %v", resp.StatusCode, tt.wantStatus)
			}

			if resp.Body != tt.wantBody {
				t.Errorf("Body = %v, want %v", resp.Body, tt.wantBody)
			}

			if resp.IsBase64Encoded {
				t.Errorf("IsBase64Encoded should be false after parsing, got true")
			}
		})
	}
}
