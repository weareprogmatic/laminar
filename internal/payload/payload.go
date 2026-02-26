package payload

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxBodySize is the maximum request body size in bytes (6MB).
const MaxBodySize = 6 * 1024 * 1024

// LambdaPayloadV2 represents an AWS Lambda Payload Version 2.0.
type LambdaPayloadV2 struct {
	Version               string            `json:"version"`
	RouteKey              string            `json:"routeKey"`
	RawPath               string            `json:"rawPath"`
	RawQueryString        string            `json:"rawQueryString"`
	Cookies               []string          `json:"cookies,omitempty"`
	Headers               map[string]string `json:"headers"`
	QueryStringParameters map[string]string `json:"queryStringParameters,omitempty"`
	RequestContext        RequestContext    `json:"requestContext"`
	Body                  string            `json:"body,omitempty"`
	IsBase64Encoded       bool              `json:"isBase64Encoded"`
}

// RequestContext contains AWS Lambda request context information.
type RequestContext struct {
	AccountID    string      `json:"accountId"`
	APIID        string      `json:"apiId"`
	DomainName   string      `json:"domainName"`
	DomainPrefix string      `json:"domainPrefix"`
	HTTP         RequestHTTP `json:"http"`
	RequestID    string      `json:"requestId"`
	RouteKey     string      `json:"routeKey"`
	Stage        string      `json:"stage"`
	Time         string      `json:"time"`
	TimeEpoch    int64       `json:"timeEpoch"`
}

// RequestHTTP contains HTTP-specific request information.
type RequestHTTP struct {
	Method    string `json:"method"`
	Path      string `json:"path"`
	Protocol  string `json:"protocol"`
	SourceIP  string `json:"sourceIp"`
	UserAgent string `json:"userAgent"`
}

// MapToLambda converts an HTTP request into Lambda Payload V2.0 format.
// nolint:funlen
func MapToLambda(r *http.Request) (LambdaPayloadV2, error) {
	headers := make(map[string]string)
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ",")
	}

	queryParams := make(map[string]string)
	for k, v := range r.URL.Query() {
		queryParams[k] = strings.Join(v, ",")
	}

	var cookies []string
	if cookieHeader := r.Header.Get("Cookie"); cookieHeader != "" {
		parts := strings.Split(cookieHeader, ";") //nolint:modernize
		for _, part := range parts {
			cookies = append(cookies, strings.TrimSpace(part))
		}
	}

	var bodyStr string
	if r.Body != nil {
		limitedReader := io.LimitReader(r.Body, MaxBodySize+1)
		bodyBytes, err := io.ReadAll(limitedReader)
		if err != nil {
			return LambdaPayloadV2{}, fmt.Errorf("failed to read request body: %w", err)
		}
		if len(bodyBytes) > MaxBodySize {
			return LambdaPayloadV2{}, fmt.Errorf("request body exceeds maximum size of %d bytes", MaxBodySize)
		}
		bodyStr = string(bodyBytes)
		_ = r.Body.Close()
	}

	now := time.Now()
	sourceIP := r.RemoteAddr
	if idx := strings.LastIndex(sourceIP, ":"); idx != -1 {
		sourceIP = sourceIP[:idx]
	}

	// Use the request's Host header so Lambdas can reconstruct their own URL
	// via request.RequestContext.DomainName (e.g. "localhost:8080").
	domainName := r.Host
	if domainName == "" {
		domainName = "localhost"
	}

	return LambdaPayloadV2{
		Version:               "2.0",
		RouteKey:              "$default",
		RawPath:               r.URL.Path,
		RawQueryString:        r.URL.RawQuery,
		Cookies:               cookies,
		Headers:               headers,
		QueryStringParameters: queryParams,
		RequestContext: RequestContext{
			AccountID:    "123456789012",
			APIID:        "laminar-local",
			DomainName:   domainName,
			DomainPrefix: "laminar",
			HTTP: RequestHTTP{
				Method:    r.Method,
				Path:      r.URL.Path,
				Protocol:  r.Proto,
				SourceIP:  sourceIP,
				UserAgent: r.UserAgent(),
			},
			RequestID: fmt.Sprintf("req-%d", now.UnixNano()),
			RouteKey:  "$default",
			Stage:     "$default",
			Time:      now.Format(time.RFC3339),
			TimeEpoch: now.UnixMilli(),
		},
		Body:            bodyStr,
		IsBase64Encoded: false,
	}, nil
}
