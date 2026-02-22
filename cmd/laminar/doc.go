// Laminar is a high-performance Go CLI that orchestrates local AWS Lambda-like endpoints
// via a "Fork-per-Request" model.
//
// It reads a JSON configuration file and spawns local HTTP servers that mimic AWS Lambda
// Function URLs. For each incoming HTTP request, Laminar executes the configured binary
// with the request mapped to AWS Lambda Payload Version 2.0 format via stdin.
//
// Key Features:
//   - Fork-per-Request: Executes binaries for each HTTP request, enabling VS Code "Attach" debugging
//   - Streaming Support: Pipes binary stdout directly to HTTP responses for SSE and streaming
//   - Lambda Compatibility: Maps requests to Lambda Payload V2.0 and parses structured responses
//   - Environment Management: Supports .env files per service with automatic injection
//   - CORS & Method Filtering: Built-in middleware for cross-origin requests and HTTP method control
//   - Graceful Shutdown: Handles SIGINT/SIGTERM with proper cleanup
//   - Health Checks: /health endpoint on every service
//
// Usage:
//
//	laminar [-config path/to/config.json] [-verbose]
//	laminar -version
//
// Configuration file format (laminar.json):
//
//	[
//	  {
//	    "name": "my-service",
//	    "port": 8080,
//	    "binary": "./path/to/binary",
//	    "cors": ["*"],
//	    "methods": ["GET", "POST"],
//	    "content_type": "application/json",
//	    "response_mode": "lambda",
//	    "env_file": ".env",
//	    "timeout": 30
//	  }
//	]
//
// For more information, see https://github.com/weareprogmatic/laminar
package main
