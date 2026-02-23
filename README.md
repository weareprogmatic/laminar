# Laminar

[![CI](https://github.com/weareprogmatic/laminar/actions/workflows/ci.yml/badge.svg)](https://github.com/weareprogmatic/laminar/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/weareprogmatic/laminar)](https://goreportcard.com/report/github.com/weareprogmatic/laminar)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE.MIT)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE.APACHE)

Laminar is a high-performance Go CLI that orchestrates local AWS Lambda endpoints using the **official AWS Lambda Runtime API**. It enables zero-modification local testing of Lambda functions built with the AWS Lambda Go SDK.

## Features

- **Zero Lambda Code Changes**: Works with unmodified Lambda functions using `github.com/aws/aws-lambda-go`
- **AWS Runtime API**: Implements the documented AWS Lambda Runtime API protocol
- **Fork-per-Request**: Each HTTP request spawns a new Lambda execution for isolation
- **Lambda Payload V2.0**: Automatically maps HTTP requests to AWS Lambda Payload Version 2.0 format
- **Streaming & SSE**: Full support for Server-Sent Events and streaming responses
- **Response Modes**: Parse Lambda structured responses or stream raw output
- **Environment Management**: Load environment variables from `.env` files per service
- **CORS Support**: Built-in middleware for cross-origin requests matching AWS Lambda Function URL behavior
- **Health Checks**: Automatic `/health` endpoint on every service
- **Graceful Shutdown**: Handles `SIGINT`/`SIGTERM` with proper cleanup
- **Zero Dependencies**: Core Laminar built entirely with Go standard library

## Installation

```bash
go install github.com/weareprogmatic/laminar/cmd/laminar@latest
```

## Quick Start

1. **Create a Lambda binary** (see `examples/hello/main.go`):

```go
package main

import (
    "context"
    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-lambda-go/lambda"
)

func handler(_ context.Context, request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
    return events.LambdaFunctionURLResponse{
        StatusCode: 200,
        Body:       "Hello from Lambda!",
    }, nil
}

func main() {
    lambda.Start(handler)
}
```

2. **Build your Lambda binary**:

```bash
go build -o artifacts/my-lambda ./path/to/your/lambda
```

3. **Create `laminar.json`**:

```json
[
  {
    "name": "my-service",
    "port": 8080,
    "binary": "./artifacts/my-lambda",
    "cors": ["*"],
    "methods": ["GET", "POST"],
    "response_mode": "lambda",
    "timeout": 30
  }
]
```

4. **Run Laminar**:

```bash
laminar
# or with custom config:
laminar -config path/to/config.json
```

5. **Test your endpoint**:

```bash
curl http://localhost:8080
```

## Configuration Reference

### Service Configuration Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | ✓ | | Human-readable service identifier |
| `port` | integer | ✓ | | HTTP port (1-65535) |
| `binary` | string | ✓ | | Path to executable |
| `cors` | array | | `[]` | Allowed CORS origins (use `["*"]` for all) |
| `methods` | array | | `[]` | CORS-only: sets `Access-Control-Allow-Methods` header (no request filtering) |
| `content_type` | string | | `"application/json"` | Default Content-Type header |
| `response_mode` | string | | `"lambda"` | Response handling: `"lambda"` or `"raw"` |
| `env_file` | string | | | Path to `.env` file for environment variables |
| `timeout` | integer | | `30` | Execution timeout in seconds |

### Response Modes

#### `lambda` (default)

Parses binary output as Lambda structured response:

```json
{
  "statusCode": 200,
  "headers": {
    "Content-Type": "text/html"
  },
  "body": "Response body",
  "cookies": ["session=abc; Path=/"]
}
```

Falls back to raw mode if output is not valid JSON.

#### `raw`

Streams binary stdout directly as HTTP response body with configured `content_type`.

### Environment Variables

Create a `.env` file and reference it in your service config:

```env
# .env
AWS_REGION=us-west-2
API_KEY=secret-key-123
DATABASE_URL=postgresql://localhost/mydb
```

**Automatic Variables:**
- `LAMINAR_LOCAL=true` (always injected)
- `AWS_REGION=us-east-1` (default if not set)

Variables from `.env` files override system environment variables.

## AWS Lambda Payload V2.0

Laminar maps HTTP requests to the AWS Lambda Payload Version 2.0 format:

```json
{
  "version": "2.0",
  "routeKey": "$default",
  "rawPath": "/api/users",
  "rawQueryString": "id=123",
  "cookies": ["session=abc"],
  "headers": {
    "host": "localhost:8080",
    "user-agent": "curl/7.79.1"
  },
  "queryStringParameters": {
    "id": "123"
  },
  "requestContext": {
    "accountId": "123456789012",
    "apiId": "laminar-local",
    "domainName": "localhost",
    "domainPrefix": "laminar",
    "http": {
      "method": "GET",
      "path": "/api/users",
      "protocol": "HTTP/1.1",
      "sourceIp": "127.0.0.1",
      "userAgent": "curl/7.79.1"
    },
    "requestId": "req-1234567890",
    "routeKey": "$default",
    "stage": "$default",
    "time": "2026-02-22T10:30:00Z",
    "timeEpoch": 1708599000000
  },
  "body": "{\"key\":\"value\"}",
  "isBase64Encoded": false
}
```

## VS Code Debugging

Laminar's fork-per-request model enables attaching a debugger to Lambda processes.

### Setup

1. Add to `.vscode/launch.json`:

```json
{
  "name": "Attach to Lambda Process",
  "type": "go",
  "request": "attach",
  "mode": "local",
  "processId": "${command:pickProcess}"
}
```

2. Start Laminar:

```bash
laminar
```

3. In VS Code:
   - Set a breakpoint in your Lambda source code
   - Run **"Attach to Lambda Process"** configuration
   - Select your binary from the process list (or wait for next request)

4. Trigger the endpoint:

```bash
curl http://localhost:8080
```

The debugger will attach when the process spawns, hitting your breakpoint.

## Health Checks

Every service automatically exposes a `/health` endpoint:

```bash
curl http://localhost:8080/health
# {"status":"ok","service":"my-service"}
```

## CLI Usage

```bash
laminar [options]

Options:
  -config, -c string    Path to configuration file (default "laminar.json")
  -verbose             Enable verbose logging
  -version, -v         Show version information
```

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                     Laminar                         │
│                                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌───────────┐  │
│  │  Service 1  │  │  Service 2  │  │ Service N │  │
│  │  Port 8080  │  │  Port 8081  │  │ Port 8082 │  │
│  └──────┬──────┘  └──────┬──────┘  └─────┬─────┘  │
│         │                │                │        │
│         └────────────────┴────────────────┘        │
│                          │                         │
└──────────────────────────┼─────────────────────────┘
                           │
                           ▼
                  ┌─────────────────┐
                  │  HTTP Request   │
                  └─────────┬───────┘
                            │
                  ┌─────────▼──────────────┐
   ┌──────────────┤ Mock Runtime API      │
   │              │ (AWS Lambda Protocol) │
   │              └─────────┬──────────────┘
   │                        │
   ▼                        ▼
┌─────────────┐   ┌─────────────────┐
│ fork binary │   │ Lambda.Start()  │
│    setup    │   │  polls GET      │
│AWS_LAMBDA_  │────────────┤        │
│ RUNTIME_API │   │  receives V2.0  │
│             │   │  JSON payload   │
│             │   └─────────┬───────┘
│             │             │
├─────────────┤  handler() processes request
│ binary      │             │
│ /runtime/   │   ┌─────────▼───────┐
│  invocation │   │ POST response   │
│  response   │───────────┤  to      │
│ (stream     │   │ Runtime API     │
│  stdout)    │   └─────────┬───────┘
│             │             │
└─────────────┘   ┌─────────▼───────┐
                  │  stream stdout  │
                  │  to HTTP client │
                  └─────────────────┘
```

## Development

### Prerequisites

- Go 1.22 or later
- `golangci-lint` (for linting)
- `goimports` (for formatting)

### Build

```bash
make build
```

### Test

```bash
make test
make coverage  # With coverage report
```

### Lint & Format

```bash
make fmt
make lint
```

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Dual-licensed under:
- [Apache License 2.0](LICENSE.APACHE)
- [MIT License](LICENSE.MIT)

You may choose either license to use this software.

## Credits

Developed by [We Are Progmatic](https://weareprogmatic.com)

---

**Questions or issues?** Open an issue on [GitHub](https://github.com/weareprogmatic/laminar/issues).

