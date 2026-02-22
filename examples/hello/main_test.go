package main

import (
	"context"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func TestHandler(t *testing.T) {
	tests := []struct {
		name           string
		request        events.LambdaFunctionURLRequest
		wantStatusCode int
		wantContains   string
	}{
		{
			name: "GET request",
			request: events.LambdaFunctionURLRequest{
				RequestContext: events.LambdaFunctionURLRequestContext{
					HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{
						Method: "GET",
						Path:   "/test",
					},
				},
			},
			wantStatusCode: 200,
			wantContains:   "Method: GET",
		},
		{
			name: "POST with query params",
			request: events.LambdaFunctionURLRequest{
				RequestContext: events.LambdaFunctionURLRequestContext{
					HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{
						Method: "POST",
						Path:   "/api/users",
					},
				},
				QueryStringParameters: map[string]string{
					"name": "world",
					"foo":  "bar",
				},
			},
			wantStatusCode: 200,
			wantContains:   "name = world",
		},
		{
			name: "with body",
			request: events.LambdaFunctionURLRequest{
				RequestContext: events.LambdaFunctionURLRequestContext{
					HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{
						Method: "POST",
						Path:   "/",
					},
				},
				Body: `{"test": "data"}`,
			},
			wantStatusCode: 200,
			wantContains:   "Body preview:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set breakpoint on the next line to debug the handler
			response, err := handler(context.Background(), tt.request)

			if err != nil {
				t.Fatalf("handler() error = %v", err)
			}

			if response.StatusCode != tt.wantStatusCode {
				t.Errorf("handler() StatusCode = %v, want %v", response.StatusCode, tt.wantStatusCode)
			}

			if tt.wantContains != "" && response.Body == "" {
				t.Errorf("handler() Body is empty, want to contain %q", tt.wantContains)
			}

			// Add more assertions as needed
			t.Logf("Response body: %s", response.Body)
		})
	}
}
