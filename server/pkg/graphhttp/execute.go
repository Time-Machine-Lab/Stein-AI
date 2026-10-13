// Package graphhttp executes one already-persisted Graph HTTP invocation.
// The coordinator owns invocation leases and retries; this package never retries.
package graphhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
	"golang.org/x/net/http/httpguts"
)

// Execute accepts public HTTP/HTTPS endpoints. The existing Remote MCP transport
// checks DNS again when dialing, pins the connection to that address, disables
// environment proxies and rejects redirects. No workspace credentials are added.
func Execute(ctx context.Context, cfg graph.HTTPConfig, headers map[string]string, body json.RawMessage, invocation string) (json.RawMessage, error) {
	return execute(ctx, cfg, headers, body, invocation, nil, remotemcp.NewSecureHTTPClient)
}

func execute(ctx context.Context, cfg graph.HTTPConfig, headers map[string]string, body json.RawMessage, invocation string, resolver remotemcp.Resolver, newClient func(*url.URL) *http.Client) (json.RawMessage, error) {
	if cfg.TimeoutSeconds < 1 || cfg.TimeoutSeconds > 300 || cfg.MaxResponseBytes < 1 || cfg.MaxResponseBytes > 262144 || len(body) > 262144 || len(headers) > 32 || invocation == "" || len(invocation) > 128 || !httpguts.ValidHeaderFieldValue(invocation) {
		return nil, errors.New("invalid HTTP execution limits")
	}
	switch cfg.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
	default:
		return nil, errors.New("unsupported HTTP method")
	}
	if len(body) > 0 && !json.Valid(body) {
		return nil, errors.New("HTTP request body must be JSON")
	}
	requestHeaders := make(http.Header)
	headerBytes := 0
	for key, value := range headers {
		name := http.CanonicalHeaderKey(key)
		switch name {
		case "Host", "Connection", "Content-Length", "Transfer-Encoding", "Trailer", "Upgrade", "Proxy-Authorization", "Proxy-Connection", "Te", "Idempotency-Key":
			return nil, errors.New("HTTP transport and invocation headers cannot be overridden")
		}
		_, duplicate := requestHeaders[name]
		if !httpguts.ValidHeaderFieldName(key) || !httpguts.ValidHeaderFieldValue(value) || duplicate {
			return nil, errors.New("invalid or duplicate HTTP header")
		}
		headerBytes += len(key) + len(value)
		requestHeaders.Set(name, value)
	}
	if headerBytes > 16384 {
		return nil, errors.New("HTTP headers exceed 16384 bytes")
	}
	endpoint, err := url.Parse(cfg.URL)
	if err != nil || len(cfg.URL) > 8192 || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, errors.New("HTTP endpoint must be an absolute public HTTP or HTTPS URL without credentials or fragment")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	// Validate the same host with the shared public-address policy. Query strings
	// are valid Graph inputs; MCP's URL policy is intentionally more restrictive.
	check := *endpoint
	check.Scheme = "https"
	check.RawQuery = ""
	check.ForceQuery = false
	if _, err = remotemcp.ValidatePublicHTTPSEndpoint(ctx, check.String(), nil, resolver); err != nil {
		return nil, errors.New("HTTP endpoint is unavailable or not public")
	}
	request, err := http.NewRequestWithContext(ctx, cfg.Method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid HTTP request")
	}
	request.Header = requestHeaders
	request.Header.Set("Idempotency-Key", invocation)
	if len(body) > 0 && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	client := newClient(endpoint)
	client.Timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.MaxResponseHeaderBytes = 32768
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("HTTP request failed or timed out; the remote operation may have occurred")
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, int64(cfg.MaxResponseBytes)+1))
	if err != nil {
		return nil, errors.New("HTTP response could not be read; the remote operation may have occurred")
	}
	if len(responseBody) > cfg.MaxResponseBytes {
		return nil, errors.New("HTTP response exceeds configured byte limit; the remote operation may have occurred")
	}
	var value any
	if json.Valid(responseBody) {
		value = json.RawMessage(responseBody)
	} else if utf8.Valid(responseBody) {
		value = string(responseBody)
	} else {
		return nil, errors.New("HTTP response is not JSON or UTF-8 text")
	}
	encoded, err := json.Marshal(struct {
		StatusCode int `json:"status_code"`
		Body       any `json:"body"`
	}{response.StatusCode, value})
	if err != nil {
		return nil, errors.New("HTTP response could not be encoded")
	}
	if len(encoded) > graph.MaxJSONBytes {
		return nil, errors.New("HTTP encoded result exceeds Graph output byte limit; the remote operation may have occurred")
	}
	return encoded, nil
}
