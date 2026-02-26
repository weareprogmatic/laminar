// Package secrets implements a mock AWS Secrets Manager API.
// It intercepts GetSecretValue calls made by Lambda functions and returns
// values configured in laminar.json under the "secrets" key.
//
// Set AWS_ENDPOINT_URL_SECRETS_MANAGER to point at this server so the AWS SDK
// routes calls here instead of real AWS Secrets Manager.
package secrets
