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

	einoproviders "github.com/mattsp1290/eino-providers"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type trackedBody struct {
	io.Reader
	mu     sync.Mutex
	closed int
}

func TestMessagesHTTPClientRoutesExactSDKURLBeforeAuthentication(t *testing.T) {
	type markerKey struct{}
	tests := []struct {
		name        string
		baseURL     string
		wantSDKBase string
		wantTarget  string
	}{
		{
			name:        "default root",
			wantSDKBase: "https://opencode.ai/zen/go/",
			wantTarget:  opencodeauth.DefaultBaseURL + "/messages",
		},
		{
			name:        "custom root without v1",
			baseURL:     "https://api.example.test/custom",
			wantSDKBase: "https://api.example.test" + messagesSDKRoutePrefix,
			wantTarget:  "https://api.example.test/custom/messages",
		},
		{
			name:        "custom trailing slash",
			baseURL:     "https://api.example.test/custom/",
			wantSDKBase: "https://api.example.test" + messagesSDKRoutePrefix,
			wantTarget:  "https://api.example.test/custom/messages",
		},
		{
			name:        "repeated path segments ending in v1",
			baseURL:     "https://api.example.test/v1/repeated/v1/",
			wantSDKBase: "https://api.example.test/v1/repeated/",
			wantTarget:  "https://api.example.test/v1/repeated/v1/messages",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recorded *http.Request
			var body string
			base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				recorded = req.Clone(req.Context())
				payload, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				body = string(payload)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
			})
			authClient := mustAuthClient(t, opencodeauth.Options{
				APIKey: "key", BaseURL: tt.baseURL, UserAgent: "route-test/1", SessionID: "session",
				HTTPClient: &http.Client{Transport: base},
			})
			client, sdkBase, err := newMessagesHTTPClient(authClient)
			if err != nil {
				t.Fatal(err)
			}
			if sdkBase != tt.wantSDKBase {
				t.Fatalf("SDK base = %q, want %q", sdkBase, tt.wantSDKBase)
			}
			ctx, _ := withOperationState(context.WithValue(context.Background(), markerKey{}, "kept"))
			req := mustRequest(t, ctx, http.MethodPost, sdkBase+"v1/messages", strings.NewReader("payload"))
			originalURL := req.URL.String()
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if recorded == nil || recorded.URL.String() != tt.wantTarget {
				t.Fatalf("target request = %#v, want %q", recorded, tt.wantTarget)
			}
			if req.URL.String() != originalURL {
				t.Fatalf("original request URL changed to %q", req.URL)
			}
			if recorded.Method != http.MethodPost || body != "payload" || recorded.Context().Value(markerKey{}) != "kept" || recorded.GetBody == nil {
				t.Fatalf("request properties changed: method=%q body=%q context=%v", recorded.Method, body, recorded.Context().Value(markerKey{}))
			}
			if recorded.Header.Get("X-Api-Key") != "key" || recorded.Header.Get("Authorization") != "" || recorded.Header.Get("User-Agent") != "route-test/1" {
				t.Fatalf("request was not authenticated after mapping: %#v", recorded.Header)
			}
		})
	}
}

func TestMessagesHTTPClientRejectsOtherSyntheticSDKRoutes(t *testing.T) {
	var calls int
	authClient := mustAuthClient(t, opencodeauth.Options{
		APIKey: "key", BaseURL: "https://api.example.test/custom", UserAgent: "route-test/1", SessionID: "session",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("unexpected base transport call")
		})},
	})
	client, sdkBase, err := newMessagesHTTPClient(authClient)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		method string
		url    string
		host   string
	}{
		{name: "path", method: http.MethodPost, url: sdkBase + "v1/messages/count_tokens"},
		{name: "query", method: http.MethodPost, url: sdkBase + "v1/messages?beta=true"},
		{name: "method", method: http.MethodGet, url: sdkBase + "v1/messages"},
		{name: "origin", method: http.MethodPost, url: "https://other.example" + messagesSDKRoutePrefix + "v1/messages"},
		{name: "host override", method: http.MethodPost, url: sdkBase + "v1/messages", host: "other.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &trackedBody{Reader: strings.NewReader("payload")}
			req := mustRequest(t, context.Background(), tt.method, tt.url, body)
			req.Host = tt.host
			resp, err := client.Do(req)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if !errors.Is(err, opencodeauth.ErrDisallowedRequest) {
				t.Fatalf("other SDK route error = %v, want ErrDisallowedRequest", err)
			}
			if body.closeCount() != 1 {
				t.Fatalf("request body close count = %d, want 1", body.closeCount())
			}
		})
	}
	if calls != 0 {
		t.Fatalf("base transport calls = %d, want 0", calls)
	}
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
	authClient := mustAuthClient(t, opencodeauth.Options{
		APIKey:     "real-key",
		BaseURL:    "https://api.example.test/custom/v1/",
		UserAgent:  "host-agent/1.0",
		SessionID:  "fallback-session",
		HTTPClient: source,
	})
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
	redirectReq := mustRequest(t, context.Background(), http.MethodGet, "https://other.example", nil)
	if got := client.CheckRedirect(redirectReq, nil); !errors.Is(got, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v, want ErrUseLastResponse", got)
	}

	endpoint := mustEndpoint(t, authClient, opencodeauth.ProtocolChatCompletions)
	ctx, _ := withOperationState(opencodeauth.WithSessionID(context.WithValue(context.Background(), contextMarker{}, "kept"), "operation-session"))
	req := mustRequest(t, ctx, http.MethodPost, endpoint, strings.NewReader("request"))
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
				"X-Secret-Trace":   {canary},
			},
			Body:    body,
			Request: req,
		}, nil
	})
	authClient := mustAuthClient(t, opencodeauth.Options{
		APIKey: "key", BaseURL: "https://api.example.test/v1", UserAgent: "agent/1", SessionID: "session", HTTPClient: &http.Client{Transport: base},
	})
	client, err := newObservedHTTPClient(authClient)
	if err != nil {
		t.Fatal(err)
	}
	ctx, state := withOperationState(context.Background())
	endpoint := mustEndpoint(t, authClient, opencodeauth.ProtocolResponses)
	req := mustRequest(t, ctx, http.MethodPost, endpoint, strings.NewReader("request"))
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
	if got := resp.Header.Get("X-Secret-Trace"); got != "" {
		t.Fatalf("sanitized response retained arbitrary header: %q", got)
	}
	if resp.Header.Get("Content-Type") != "application/json" || resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("Content-Length") != "" || resp.ContentLength != int64(len(gotBody)) {
		t.Fatalf("sanitized entity metadata is inconsistent: %#v length=%d", resp.Header, resp.ContentLength)
	}
	gotErr := state.httpError()
	if gotErr == nil || gotErr.StatusCode != http.StatusTooManyRequests || gotErr.Kind != opencodeauth.ErrorKindRateLimit || !gotErr.HasRetryAfter || gotErr.RetryAfter != 2*time.Second {
		t.Fatalf("observed HTTP error = %#v", gotErr)
	}
}

func TestObservingTransportRetainsStatusForBodylessErrors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		nilBody   bool
		wantClass einoproviders.ErrorClass
	}{
		{name: "nil-body unauthorized", status: http.StatusUnauthorized, nilBody: true, wantClass: einoproviders.ErrorClassProviderAuth},
		{name: "empty-body forbidden", status: http.StatusForbidden, wantClass: einoproviders.ErrorClassProviderAuth},
		{name: "empty-body rate limit", status: http.StatusTooManyRequests, wantClass: einoproviders.ErrorClassProviderAPI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body *trackedBody
			base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				var responseBody io.ReadCloser
				if !tt.nilBody {
					body = &trackedBody{Reader: strings.NewReader("")}
					responseBody = body
				}
				return &http.Response{StatusCode: tt.status, Header: make(http.Header), Body: responseBody, Request: req}, nil
			})
			ctx, state := withOperationState(context.Background())
			req := mustRequest(t, ctx, http.MethodPost, "https://api.example.test/v1/responses", http.NoBody)
			resp, err := (&observingTransport{next: base}).RoundTrip(req)
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			if resp == nil || resp.Body == nil {
				t.Fatal("RoundTrip() did not provide the sanitized response body")
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatalf("sanitized response Close() error = %v", err)
			}
			if body != nil && body.closeCount() != 1 {
				t.Fatalf("source body close count = %d, want 1", body.closeCount())
			}
			httpErr := state.httpError()
			if httpErr == nil || httpErr.StatusCode != tt.status {
				t.Fatalf("HTTP observation = %#v, want status %d", httpErr, tt.status)
			}
			mapped := mapInvocationError(operationRequest, errors.New("sdk decode failed"), state)
			if got := einoproviders.Classify(mapped); got != tt.wantClass {
				t.Fatalf("Classify(error) = %v, want %v", got, tt.wantClass)
			}
		})
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
	req := mustRequest(t, ctx, http.MethodPost, "https://api.example.test/v1/responses", nil)
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
	req := mustRequest(t, ctx, http.MethodPost, "https://api.example.test/v1/responses", nil)
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

func TestObservingTransportWrapsOnlySDKEventStreams(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		wrapped bool
	}{
		{name: "chat stream", path: "/v1/chat/completions", content: "text/event-stream; charset=utf-8", wrapped: true},
		{name: "messages stream", path: "/v1/messages", content: "text/event-stream", wrapped: true},
		{name: "responses stream", path: "/v1/responses", content: "text/event-stream"},
		{name: "chat JSON", path: "/v1/chat/completions", content: "application/json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &trackedBody{Reader: strings.NewReader("data: [DONE]\n\n")}
			base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {tt.content}}, Body: source, Request: req}, nil
			})
			req := mustRequest(t, context.Background(), http.MethodPost, "https://api.example.test"+tt.path, http.NoBody)
			resp, err := (&observingTransport{next: base}).RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			_, got := resp.Body.(*terminalBodyObserver)
			if got != tt.wrapped {
				t.Fatalf("wrapped = %v, want %v", got, tt.wrapped)
			}
			_ = resp.Body.Close()
		})
	}
}

type idleClosingTransport struct {
	closed bool
}

func (*idleClosingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}

func (t *idleClosingTransport) CloseIdleConnections() { t.closed = true }

func mustAuthClient(t *testing.T, options opencodeauth.Options) *opencodeauth.Client {
	t.Helper()
	client, err := opencodeauth.NewClient(options)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func mustEndpoint(t *testing.T, client *opencodeauth.Client, protocol opencodeauth.Protocol) string {
	t.Helper()
	endpoint, err := client.Endpoint(protocol)
	if err != nil {
		t.Fatalf("Endpoint() error = %v", err)
	}
	return endpoint
}

func mustRequest(t *testing.T, ctx context.Context, method, url string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	return req
}
