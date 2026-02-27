// Laminar is a local AWS Lambda orchestrator that mimics AWS Lambda Function URLs.
//
// It reads a JSON configuration file and spawns local HTTP servers, one per service.
// For each incoming HTTP request, Laminar starts the configured Lambda binary and
// communicates with it using the AWS Lambda Runtime API protocol — the same protocol
// used in production AWS environments. This means Lambda functions require zero
// modification to run locally under Laminar.
//
// How it works:
//
//  1. An HTTP request arrives at the configured port.
//  2. Laminar starts a mock AWS Lambda Runtime API server on a random local port.
//  3. Laminar spawns the Lambda binary with AWS_LAMBDA_RUNTIME_API set to the mock server.
//  4. The Lambda binary (via the AWS SDK) polls GET /runtime/invocation/next.
//  5. Laminar responds with the request mapped to Lambda Payload Version 2.0 format.
//  6. The Lambda processes the request and POSTs the response back to the Runtime API.
//  7. Laminar forwards the response to the original HTTP client.
//
// Key Features:
//   - Zero Code Changes: Works with unmodified Lambda functions using github.com/aws/aws-lambda-go
//   - AWS Runtime API: Implements the official AWS Lambda Runtime API protocol
//   - Fork-per-Request: Each HTTP request spawns a new process, enabling VS Code debugging
//   - Lambda Payload V2.0: Maps HTTP requests to official AWS Lambda format
//   - CORS Support: Built-in CORS middleware matching AWS Lambda Function URL behavior
//   - Graceful Shutdown: Handles SIGINT/SIGTERM with proper cleanup
//
// Usage:
//
//	laminar [-config path/to/laminar.json] [-verbose]
//	laminar -version
//
// For more information, see https://github.com/weareprogmatic/laminar
package main
