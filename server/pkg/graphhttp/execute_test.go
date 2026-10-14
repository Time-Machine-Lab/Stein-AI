package graphhttp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/graph"
)

type resolver []netip.Addr

func (r resolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) { return r, nil }

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func config() graph.HTTPConfig {
	return graph.HTTPConfig{Method: "POST", URL: "https://example.com/action?mode=test", TimeoutSeconds: 3, MaxResponseBytes: 1024}
}

func TestGraphHTTPBlocksPrivateDestinationsBeforeDispatch(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "::ffff:192.168.1.1"} {
		t.Run(ip, func(t *testing.T) {
			_, err := execute(context.Background(), config(), nil, nil, "invocation", resolver{netip.MustParseAddr(ip)}, func(*url.URL) *http.Client { t.Fatal("private destination reached transport"); return nil })
			if err == nil {
				t.Fatal("private endpoint accepted")
			}
		})
	}
}

func TestGraphHTTPPreservesBoundedResponseAndStableInvocation(t *testing.T) {
	cfg := config()
	result, err := execute(context.Background(), cfg, map[string]string{"X-Test": "value"}, json.RawMessage(`{"request":1}`), "node-invocation", resolver{netip.MustParseAddr("93.184.216.34")}, func(endpoint *url.URL) *http.Client {
		if endpoint.RawQuery != "mode=test" {
			t.Fatal("query lost")
		}
		return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Idempotency-Key") != "node-invocation" || r.Header.Get("X-Test") != "value" {
				t.Fatal("bound headers lost")
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"request":1}` {
				t.Fatal("bound body lost")
			}
			if _, ok := r.Context().Deadline(); !ok {
				t.Fatal("request has no deadline")
			}
			return &http.Response{StatusCode: 422, Body: io.NopCloser(strings.NewReader(`{"error":"invalid"}`)), Header: http.Header{"Set-Cookie": []string{"secret"}}}, nil
		})}
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"status_code":422,"body":{"error":"invalid"}}` {
		t.Fatalf("unexpected result %s", result)
	}
}

func TestGraphHTTPRejectsSpoofedTransportHeadersAndOversizeBody(t *testing.T) {
	for _, headers := range []map[string]string{{"Host": "127.0.0.1"}, {"Idempotency-Key": "spoof"}, {"Connection": "upgrade"}, {"X-Test": "a\r\nb"}} {
		if _, err := Execute(context.Background(), config(), headers, nil, "invocation"); err == nil {
			t.Fatal("unsafe headers accepted")
		}
	}
	cfg := config()
	cfg.MaxResponseBytes = 4
	_, err := execute(context.Background(), cfg, nil, nil, "invocation", resolver{netip.MustParseAddr("93.184.216.34")}, func(*url.URL) *http.Client {
		return &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("12345")), Header: make(http.Header)}, nil
		})}
	})
	if err == nil {
		t.Fatal("oversize response silently truncated")
	}
}

func TestGraphHTTPRejectsUserinfoAndLocalhost(t *testing.T) {
	for _, raw := range []string{"http://localhost/path", "http://user:password@example.com", "file:///etc/passwd", "https://example.com/#fragment"} {
		cfg := config()
		cfg.URL = raw
		if _, err := Execute(context.Background(), cfg, nil, nil, "invocation"); err == nil {
			t.Fatalf("unsafe URL accepted: %s", raw)
		}
	}
}

func TestGraphHTTPBoundsEncodedResult(t *testing.T) {
	cfg := config()
	cfg.MaxResponseBytes = graph.MaxJSONBytes
	_, err := execute(context.Background(), cfg, nil, nil, "invocation", resolver{netip.MustParseAddr("93.184.216.34")}, func(*url.URL) *http.Client {
		return &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("\x01", graph.MaxJSONBytes/2))), Header: make(http.Header)}, nil
		})}
	})
	if err == nil || !strings.Contains(err.Error(), "encoded") {
		t.Fatalf("expected explicit encoded result limit: %v", err)
	}
}
