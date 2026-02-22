// Package main implements a sample Lambda function for testing.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// handler processes the Lambda Function URL request.
func handler(_ context.Context, request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	// Log the incoming request (this should appear in Laminar logs)
	log.Printf("Processing request: %s %s", request.RequestContext.HTTP.Method, request.RequestContext.HTTP.Path)

	// Extract information from the request
	method := request.RequestContext.HTTP.Method
	path := request.RequestContext.HTTP.Path

	// Build greeting message
	greeting := fmt.Sprintf("Hello from Laminar...!\n\nMethod: %s\nPath: %s\n", method, path)

	// Add body preview if present
	if request.Body != "" {
		bodyPreview := request.Body
		if len(bodyPreview) > 50 {
			bodyPreview = bodyPreview[:50] + "..."
		}
		greeting += fmt.Sprintf("Body preview: %s\n", bodyPreview)
	}

	// Add query parameters if present
	if len(request.QueryStringParameters) > 0 {
		greeting += "\nQuery Parameters:\n"
		for k, v := range request.QueryStringParameters {
			greeting += fmt.Sprintf("  %s = %s\n", k, v)
		}
	}

	// Return the response
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "text/plain",
		},
		Body: greeting,
	}, nil
}

func main() {
	lambda.Start(handler)
}
