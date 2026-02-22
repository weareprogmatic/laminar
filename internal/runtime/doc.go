// Package runtime implements a mock AWS Lambda Runtime API server for local Lambda testing.
//
// The runtime package implements the standard AWS Lambda Runtime API HTTP endpoints,
// allowing Lambda Go functions to run locally without modification. Lambdas communicate
// with Laminar through GET /2018-06-01/runtime/invocation/next and POST responses.
//
// The server is created and managed internally by the runner package on a random available
// port, exposed to the Lambda via the AWS_LAMBDA_RUNTIME_API environment variable.
//
// For more details see: https://docs.aws.amazon.com/lambda/latest/dg/runtimes-api.html
//
//nolint:revive
package runtime
