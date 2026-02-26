# Laminar

[![CI](https://github.com/weareprogmatic/laminar/actions/workflows/ci.yml/badge.svg)](https://github.com/weareprogmatic/laminar/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/weareprogmatic/laminar)](https://goreportcard.com/report/github.com/weareprogmatic/laminar)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE.MIT)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE.APACHE)

Laminar is a high-performance Go CLI that orchestrates local AWS Lambda endpoints using the **official AWS Lambda Runtime API**. It enables zero-modification local testing of Lambda functions built with the AWS Lambda Go SDK.

## Features

- **Zero Lambda Code Changes**: Works with unmodified Lambda functions using `github.com/aws/aws-lambda-go`
- **AWS Runtime API**: Implements the documented AWS Lambda Runtime API protocol
- **Warm Process Model**: Lambda processes start at Laminar startup and stay alive between requests, mirroring real AWS Lambda warm-container behaviour
- **Lambda Payload V2.0**: Automatically maps HTTP requests to AWS Lambda Payload Version 2.0 format
- **Lambda-to-Lambda Calls**: Built-in mock Lambda Service API so one Lambda can invoke another locally using the standard AWS SDK, with zero code changes
- **Secrets Manager**: Built-in mock Secrets Manager API so Lambda functions can call `GetSecretValue` locally using the standard AWS SDK, with zero code changes
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

> **Platform support:** Laminar is developed and tested on macOS. It should work on Linux and Windows, but has not been formally tested on those platforms.

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
{
  "services": [
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
}
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

### Configuration Format

Laminar supports two `laminar.json` formats:

**New object format** (recommended) — define a top-level `secrets` object shared across all services:

```json
{
  "services": [ ... ],
  "secrets": {
    "my-app/db-password": "local-dev-password"
  }
}
```

**Legacy array format** (still supported) — per-service `secrets` are merged into a single global namespace:

```json
[
  { "name": "my-service", "port": 8080, "binary": "./artifacts/my-lambda", "secrets": { ... } }
]
```

> When the same secret key appears in both a per-service `secrets` block and the top-level `secrets` object, the **top-level value wins**.

### Service Configuration Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | ✓ | | Service identifier — also used as the **function name** for Lambda-to-Lambda routing |
| `port` | integer | ✓ | | HTTP port (1-65535) |
| `binary` | string | ✓ | | Path to executable |
| `cors` | array | | `[]` | Allowed CORS origins (use `["*"]` for all) |
| `methods` | array | | `[]` | CORS-only: sets `Access-Control-Allow-Methods` header (no request filtering) |
| `response_mode` | string | | `"lambda"` | Response handling: `"lambda"` or `"raw"` |
| `env_file` | string | | | Path to `.env` file for environment variables |
| `env` | object | | | Inline key/value environment variables |
| `secrets` | object | | | Per-service secrets (legacy) — prefer the top-level `secrets` object instead |
| `timeout` | integer | | `30` | Execution timeout in seconds |
| `debug_port` | integer | | | Delve debugger port — when set, wraps Lambda with `dlv exec --headless` |

### Top-level Configuration Fields (object format only)

| Field | Type | Description |
|-------|------|-------------|
| `services` | array | List of service configurations (same fields as above) |
| `secrets` | object | Global Secrets Manager values shared across all services — keyed by secret name, returned by `GetSecretValue` |

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

Returns the Lambda's Runtime API response body as-is, without parsing it as a structured Lambda response. Useful when the Lambda returns plain text, HTML, or non-standard JSON.

### Lambda-to-Lambda Invocations

Laminar automatically starts a mock **Lambda Service API** alongside your services, so one Lambda can invoke another using the standard AWS SDK — no real AWS credentials or networking required.

When Laminar starts, it:

1. Starts a Lambda Service API server on a random port.
2. Injects `AWS_ENDPOINT_URL_LAMBDA` (and `AWS_LAMBDA_ENDPOINT` for older SDKs) into every Lambda process, pointing to that server.
3. Routes invocation calls by matching the function name against the `name` field in `laminar.json`.

Both invocation types are supported:

| `X-Amz-Invocation-Type` | Behaviour |
|-------------------------|-----------|
| `RequestResponse` (default) | Runs the target Lambda synchronously and returns its output |
| `Event` | Fires the target Lambda in a background goroutine; returns `202` immediately |

**Example `laminar.json` with two services:**

```json
{
  "services": [
    {
      "name": "api-service",
      "port": 8080,
      "binary": "./artifacts/api-service",
      "response_mode": "lambda"
    },
    {
      "name": "worker-service",
      "port": 8081,
      "binary": "./artifacts/worker-service",
      "response_mode": "lambda"
    }
  ]
}
```

**Example — Go Lambda calling another Lambda:**

```go
import (
    "context"

    awsconfig "github.com/aws/aws-sdk-go-v2/config"
    lambdasdk "github.com/aws/aws-sdk-go-v2/service/lambda"
    "github.com/aws/aws-sdk-go-v2/aws"
)

func handler(ctx context.Context, event MyEvent) (MyResponse, error) {
    cfg, err := awsconfig.LoadDefaultConfig(ctx)
    if err != nil {
        return MyResponse{}, err
    }
    client := lambdasdk.NewFromConfig(cfg)

    out, err := client.Invoke(ctx, &lambdasdk.InvokeInput{
        FunctionName: aws.String("worker-service"), // matches "name" in laminar.json
        Payload:      []byte(`{"task":"process"}`),
    })
    if err != nil {
        return MyResponse{}, err
    }

    // out.Payload contains the raw response from worker-service
    _ = out.Payload
    return MyResponse{Status: "ok"}, nil
}
```

Because `AWS_ENDPOINT_URL_LAMBDA` is already set by Laminar, the AWS SDK routes this call locally without any code changes between local and production environments.

**Function name formats supported:**

| Format | Example | Resolved as |
|--------|---------|-------------|
| Plain name | `"worker-service"` | `worker-service` |
| Name with qualifier | `"worker-service:$LATEST"` | `worker-service` (qualifier stripped) |
| Full ARN | `"arn:aws:lambda:us-east-1:123:function:worker-service"` | `worker-service` |

### Secrets Manager

Laminar automatically starts a mock **Secrets Manager API** alongside your services, so Lambda functions can call `GetSecretValue` (and `DescribeSecret`) using the standard AWS SDK — no real AWS credentials required.

When Laminar starts, it:

1. Starts a Secrets Manager API server on a random port.
2. Injects `AWS_ENDPOINT_URL_SECRETS_MANAGER` into every Lambda process, pointing to that server.
3. Returns values from the global `secrets` map in `laminar.json` keyed by secret name.

The value is returned as-is in `SecretString`. Use a JSON string for structured credentials.

Secrets are **global** — all services share the same namespace, matching real AWS Secrets Manager account-level scoping. Define them once in the top-level `secrets` object:

**Recommended `laminar.json` (object format):**

```json
{
  "services": [
    {
      "name": "api-service",
      "port": 8080,
      "binary": "./artifacts/api-service"
    },
    {
      "name": "worker-service",
      "port": 8081,
      "binary": "./artifacts/worker-service"
    }
  ],
  "secrets": {
    "my-app/db-password": "local-dev-password",
    "my-app/api-creds": "{\"key\":\"abc\",\"region\":\"us-east-1\"}"
  }
}
```

**Example — Go Lambda reading a secret:**

```go
import (
    "context"
    "encoding/json"

    awsconfig "github.com/aws/aws-sdk-go-v2/config"
    sm "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
    "github.com/aws/aws-sdk-go-v2/aws"
)

func handler(ctx context.Context, event MyEvent) (MyResponse, error) {
    cfg, err := awsconfig.LoadDefaultConfig(ctx)
    if err != nil {
        return MyResponse{}, err
    }
    client := sm.NewFromConfig(cfg)

    out, err := client.GetSecretValue(ctx, &sm.GetSecretValueInput{
        SecretId: aws.String("my-app/db-password"),
    })
    if err != nil {
        return MyResponse{}, err
    }

    password := aws.ToString(out.SecretString) // "local-dev-password"
    _ = password
    return MyResponse{Status: "ok"}, nil
}
```

For structured secrets (e.g. credentials objects), unmarshal `SecretString` as JSON:

```go
var creds struct {
    Key    string `json:"key"`
    Region string `json:"region"`
}
if err := json.Unmarshal([]byte(aws.ToString(out.SecretString)), &creds); err != nil {
    return MyResponse{}, err
}
```

Because `AWS_ENDPOINT_URL_SECRETS_MANAGER` is already set by Laminar, the AWS SDK routes this call locally without any code changes between local and production environments.

If a Lambda requests a secret not in the `secrets` map, it receives a `ResourceNotFoundException` with a message indicating which key to add to `laminar.json`.

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
- `AWS_ENDPOINT_URL_LAMBDA` / `AWS_LAMBDA_ENDPOINT` (points to Laminar's Lambda Service API for Lambda-to-Lambda calls)
- `AWS_ENDPOINT_URL_SECRETS_MANAGER` (points to Laminar's Secrets Manager API)

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
    "domainName": "localhost:8080",
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

`domainName` is set from the HTTP request's `Host` header (e.g. `localhost:8080`), so your Lambda can reconstruct its own URL:

```go
fullURL := fmt.Sprintf("http://%s%s", request.RequestContext.DomainName, request.RawPath)
```

## Debugging Lambda Functions

Laminar has built-in Delve integration for step-through debugging of Lambda functions. Add `debug_port` to a service in `laminar.json` and Laminar starts the Lambda **immediately at startup** wrapped with `dlv exec --headless --continue`. The debug port is open before the first request arrives — attach your IDE once and breakpoints fire on every subsequent request, just like a warm Lambda container.

### Prerequisites

- [Delve](https://github.com/go-delve/delve): `go install github.com/go-delve/delve/cmd/dlv@latest`
- [Go extension](https://marketplace.visualstudio.com/items?itemName=golang.Go) v0.40.0+

### Setup

1. **Add `debug_port` to your service config** (`laminar.json`):

```json
{
  "services": [
    {
      "name": "my-service",
      "port": 8080,
      "binary": "./artifacts/my-lambda",
      "debug_port": 2345
    }
  ]
}
```

When `debug_port` is set, the process timeout is automatically raised to at least **300 seconds**.

2. **Add a build task** (`.vscode/tasks.json`):

The task builds the Lambda with debug symbols and waits briefly for Laminar to pick up the new binary:

```json
{
  "label": "build-debug-my-service",
  "type": "shell",
  "command": "go build -gcflags=\"all=-N -l\" -o ${workspaceFolder}/artifacts/my-lambda ${workspaceFolder}/path/to/lambda && sleep 2",
  "group": "build",
  "problemMatcher": ["$go"]
}
```

3. **Add a launch configuration** (`.vscode/launch.json`):

Wire the build task as a `preLaunchTask` so pressing F5 rebuilds with debug symbols and then attaches automatically:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "DEBUG my-service",
      "type": "go",
      "request": "attach",
      "mode": "remote",
      "port": 2345,
      "host": "127.0.0.1",
      "preLaunchTask": "build-debug-my-service"
    }
  ]
}
```

4. **Start Laminar** (from the workspace root so source paths resolve correctly):

```bash
laminar
```

Laminar logs when the debugger is ready:

```
[Lambda] Debugger ready on 127.0.0.1:2345 – attach your IDE now
```

5. **Press F5 in VS Code** (Run → "DEBUG my-service"). The build task compiles the binary, then VS Code attaches to the debug port. The Lambda resumes, calls `GET /runtime/invocation/next`, and blocks — ready for requests.

6. **Send requests normally:**

```bash
curl http://localhost:8080/test
```

Your breakpoints are hit on every request. The debug session stays alive between requests — no need to re-attach.

> **Note:** When `debug_port` is set, the Lambda process is paused until you attach your IDE. HTTP requests will block until you connect and resume.

### Lambda-to-Lambda Debugging

If a target service has `debug_port` set and another Lambda invokes it via `client.Invoke()`, the invoked Lambda is also started warm with dlv on that port.

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

Each service starts a **persistent Lambda process** when Laminar starts, mirroring the warm-container model of real AWS Lambda. Requests are fed to the running process via the AWS Lambda Runtime API protocol — the process calls `GET /runtime/invocation/next` between requests, exactly as it would in production.

```
HTTP client
    │
    │  GET /api/users
    ▼
┌──────────────────────────────────────────┐
│  Laminar (per-service HTTP server)       │
│                                          │
│  1. Map request → Lambda Payload V2.0   │
│  2. Start mock Runtime API (random port) │
│  3. Fork Lambda binary with             │
│     AWS_LAMBDA_RUNTIME_API=127.0.0.1:X  │
│     AWS_ENDPOINT_URL_LAMBDA=127.0.0.1:Y │
└────────────────────┬─────────────────────┘
                     │
         ┌───────────┴────────────┐
         │                        │
         ▼                        ▼
┌─────────────────┐    ┌──────────────────────┐
│  Mock Runtime   │    │   Lambda binary      │
│  API server     │    │                      │
│                 │◄───│  lambda.Start()      │
│  GET /runtime/  │    │  polls next event    │
│  invocation/    │───►│                      │
│  next           │    │  handler() runs      │
│                 │◄───│                      │
│  POST /runtime/ │    │  POST response back  │
│  invocation/    │    │  to Runtime API      │
│  {id}/response  │    │         │            │
└────────┬────────┘    │         │ client.    │
         │             │         │ Invoke()   │
         ▼             └─────────┼────────────┘
┌──────────────────┐             │
│  Forward Lambda  │             ▼
│  response to     │  ┌──────────────────────┐
│  HTTP client     │  │  Mock Lambda Service │
└──────────────────┘  │  API (port Y)        │
                      │                      │
                      │  Routes by "name"    │
                      │  → forks target      │
                      │    Lambda binary     │
                      └──────────────────────┘
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

