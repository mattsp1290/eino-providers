package opencodego

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"testing"
	"time"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type trackedBody struct {
	io.Reader
	mu     sync.Mutex
	closed int
}

func (b *trackedBody) Close() error {
	b.mu.Lock()
	b.closed++
	b.mu.Unlock()
	return nil
}

func (b *trackedBody) closeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func TestObservedHTTPClientPreservesPolicyAndAuthenticates(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	var recorded *http.Request
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		recorded = req.Clone(req.Context())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    req,
		}, nil
	})
	source := &http.Client{
		Transport: base,
		Timeout:   17 * time.Second,
		Jar:       jar,
	}
	authClient, err := opencodeauth.NewClient(opencodeauth.Options{
		APIKey:     "real-key",
		BaseURL:    "https://api.example.test/custom/v1/",
		UserAgent:  "host-agent/1.0",
		SessionID:  "fallback-session",
		HTTPClient: source,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := newObservedHTTPClient(authClient)
	if err != nil {
		t.Fatal(err)
	}
	if client == source || client.Timeout != source.Timeout || client.Jar != jar {
		t.Fatal("observed client did not preserve copied client policy")
	}
	if _, ok := source.Transport.(roundTripFunc); !ok || source.CheckRedirect != nil {
		t.Fatal("auth construction modified caller client")
	}
	redirectReq, _ := http.NewRequest(http.MethodGet, "https://other.example", nil)
	if got := client.CheckRedirect(redirectReq, nil); !errors.Is(got, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v, want ErrUseLastResponse", got)
	}

	endpoint, err := authClient.Endpoint(opencodeauth.ProtocolChatCompletions)
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := withOperationState(opencodeauth.WithSessionID(context.WithValue(context.Background(), contextMarker{}, "kept"), "operation-session"))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("request"))
	req.Header.Set("Authorization", "Bearer caller-secret")
	req.Header.Set("X-Api-Key", "caller-secret")
	req.Header.Set("User-Agent", "caller-agent")
	req.Header.Set("X-OpenCode-Session", "caller-session")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if recorded == nil {
		t.Fatal("base transport did not receive request")
	}
	if got, want := recorded.URL.String(), "https://api.example.test/custom/v1/chat/completions"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	if got := recorded.Header.Get("Authorization"); got != "Bearer real-key" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := recorded.Header.Get("X-Api-Key"); got != "" {
		t.Fatalf("X-Api-Key leaked into chat request: %q", got)
	}
	if got := recorded.Header.Get("User-Agent"); got != "host-agent/1.0" {
		t.Fatalf("User-Agent = %q", got)
	}
	if got := recorded.Header.Get("X-OpenCode-Session"); got != "operation-session" {
		t.Fatalf("session = %q, want operation override", got)
	}
	if got := recorded.Context().Value(contextMarker{}); got != "kept" {
		t.Fatalf("context value = %v, want kept", got)
	}
}

func TestObservingTransportSanitizesHTTPErrorAndPreservesRetryPolicy(t *testing.T) {
	canary := "upstream-secret-canary"
	body := &trackedBody{Reader: strings.NewReader(`{"error":{"type":"RateLimitError","message":"` + canary + `"}}`)}
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"Content-Type":     {"text/plain"},
				"Content-Encoding": {"gzip"},
				"Content-Length":   {"999"},
				"Retry-After":      {"2"},
				"X-Should-Retry":   {"true"},
			},
			Body:    body,
			Request: req,
		}, nil
	})
	authClient, err := opencodeauth.NewClient(opencodeauth.Options{
		APIKey: "key", BaseURL: "https://api.example.test/v1", UserAgent: "agent/1", SessionID: "session", HTTPClient: &http.Client{Transport: base},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := newObservedHTTPClient(authClient)
	if err != nil {
		t.Fatal(err)
	}
	ctx, state := withOperationState(context.Background())
	endpoint, _ := authClient.Endpoint(opencodeauth.ProtocolResponses)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("request"))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if body.closeCount() != 1 {
		t.Fatalf("upstream body close count = %d, want 1", body.closeCount())
	}
	gotBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gotBody), canary) || string(gotBody) != sanitizedNativeError {
		t.Fatalf("sanitized body = %q", gotBody)
	}
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "2" || resp.Header.Get("X-Should-Retry") != "true" {
		t.Fatalf("status/retry policy was not preserved: %d %#v", resp.StatusCode, resp.Header)
	}
	if resp.Header.Get("Content-Type") != "application/json" || resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("Content-Length") != "" || resp.ContentLength != int64(len(gotBody)) {
		t.Fatalf("sanitized entity metadata is inconsistent: %#v length=%d", resp.Header, resp.ContentLength)
	}
	gotErr := state.httpError()
	if gotErr == nil || gotErr.StatusCode != http.StatusTooManyRequests || gotErr.Kind != opencodeauth.ErrorKindRateLimit || !gotErr.HasRetryAfter || gotErr.RetryAfter != 2*time.Second {
		t.Fatalf("observed HTTP error = %#v", gotErr)
	}
}

func TestObservingTransportClearsStaleAttemptAndClosesNetworkResponse(t *testing.T) {
	networkBody := &trackedBody{Reader: strings.NewReader("partial")}
	call := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		call++
		if call == 1 {
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"type":"AuthError"}}`)), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: networkBody, Request: req}, errors.New("network-secret")
	})
	transport := &observingTransport{next: base}
	ctx, state := withOperationState(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.example.test/v1/responses", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil || resp == nil || state.httpError() == nil {
		t.Fatalf("first RoundTrip = (%v, %v), state=%#v", resp, err, state.httpError())
	}
	_ = resp.Body.Close()
	resp, err = transport.RoundTrip(req)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil || resp != nil {
		t.Fatalf("second RoundTrip = (%v, %v), want network error", resp, err)
	}
	if state.httpError() != nil {
		t.Fatal("network failure inherited stale HTTP classification")
	}
	if networkBody.closeCount() != 1 {
		t.Fatalf("network response close count = %d, want 1", networkBody.closeCount())
	}
}

func TestObservingTransportUsesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx, _ = withOperationState(ctx)
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("transport observed cancellation")
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.example.test/v1/responses", nil)
	resp, err := (&observingTransport{next: base}).RoundTrip(req)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if resp != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip = (%v, %v), want context cancellation", resp, err)
	}
}

func TestObservingTransportForwardsCloseIdleConnections(t *testing.T) {
	next := &idleClosingTransport{}
	transport := &observingTransport{next: next}
	transport.CloseIdleConnections()
	if !next.closed {
		t.Fatal("CloseIdleConnections was not forwarded")
	}
}

type idleClosingTransport struct {
	closed bool
}

func (*idleClosingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}

func (t *idleClosingTransport) CloseIdleConnections() { t.closed = true }
