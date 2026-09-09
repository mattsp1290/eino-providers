package opencodego

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	opencodeauth "github.com/mattsp1290/opencode-auth-go"
)

const retryTestBaseURL = "https://api.example.test/v1"

func TestMessagesSDKUsesDefaultRetryLimitAndRetryAfter(t *testing.T) {
	var retryCounts []string
	var attempts []time.Time
	client := newRetryTestMessagesClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		retryCounts = append(retryCounts, req.Header.Get("X-Stainless-Retry-Count"))
		attempts = append(attempts, time.Now())
		return retryTestResponse(req, http.StatusTooManyRequests, http.Header{"Retry-After": {"0.02"}}), nil
	}))

	err := callRetryTestMessages(context.Background(), &client)
	if err == nil {
		t.Fatal("Messages.New() succeeded after repeated rate limits")
	}
	if want := []string{"0", "1", "2"}; !reflect.DeepEqual(retryCounts, want) {
		t.Fatalf("retry count headers = %v, want %v", retryCounts, want)
	}
	for i := 1; i < len(attempts); i++ {
		gap := attempts[i].Sub(attempts[i-1])
		if gap < 15*time.Millisecond {
			t.Fatalf("retry gap %d = %v, want at least the explicit Retry-After delay", i, gap)
		}
	}
}

func TestMessagesSDKHonorsPermanentAndExplicitRetryPolicy(t *testing.T) {
	tests := []struct {
		name         string
		headers      http.Header
		wantAttempts int
	}{
		{name: "permanent 400", headers: make(http.Header), wantAttempts: 1},
		{name: "explicit retry", headers: http.Header{"X-Should-Retry": {"true"}, "Retry-After": {"0"}}, wantAttempts: 3},
		{name: "explicit no retry overrides 500", headers: http.Header{"X-Should-Retry": {"false"}}, wantAttempts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			client := newRetryTestMessagesClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				status := http.StatusBadRequest
				if tt.headers.Get("X-Should-Retry") == "false" {
					status = http.StatusInternalServerError
				}
				return retryTestResponse(req, status, tt.headers), nil
			}))

			if err := callRetryTestMessages(context.Background(), &client); err == nil {
				t.Fatal("Messages.New() unexpectedly succeeded")
			}
			if attempts != tt.wantAttempts {
				t.Fatalf("attempts = %d, want %d", attempts, tt.wantAttempts)
			}
		})
	}
}

func TestMessagesSDKRetryRetainsOperationContextAndClearsFailureOnSuccess(t *testing.T) {
	type markerKey struct{}
	ctx, state := withOperationState(context.WithValue(context.Background(), markerKey{}, "kept"))
	ctx = opencodeauth.WithSessionID(ctx, "operation-session")
	attempts := 0
	client := newRetryTestMessagesClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if got := operationStateFromContext(req.Context()); got != state {
			t.Fatalf("attempt %d operation state = %p, want %p", attempts, got, state)
		}
		if got := req.Context().Value(markerKey{}); got != "kept" {
			t.Fatalf("attempt %d context marker = %v", attempts, got)
		}
		if got := req.Header.Get("X-OpenCode-Session"); got != "operation-session" {
			t.Fatalf("attempt %d session = %q", attempts, got)
		}
		if got := req.Header.Get("X-Api-Key"); got != "auth-key" {
			t.Fatalf("attempt %d API key = %q", attempts, got)
		}
		if attempts == 1 {
			return retryTestResponse(req, http.StatusTooManyRequests, http.Header{"Retry-After": {"0"}}), nil
		}
		return retryTestSuccess(req), nil
	}))

	if err := callRetryTestMessages(ctx, &client); err != nil {
		t.Fatalf("Messages.New() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got := state.httpError(); got != nil {
		t.Fatalf("successful retry retained HTTP failure: %#v", got)
	}
}

func TestMessagesSDKRetryReplaysRequestAndClosesResponseBodies(t *testing.T) {
	var requestBodies [][]byte
	var responseBodies []*trackedBody
	attempts := 0
	client := newRetryTestMessagesClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("attempt %d request body: %v", attempts, err)
		}
		requestBodies = append(requestBodies, body)
		if attempts == 1 {
			source := &trackedBody{Reader: strings.NewReader(`{"error":{"type":"RateLimitError","message":"fixture"}}`)}
			responseBodies = append(responseBodies, source)
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": {"0"}},
				Body:       source,
				Request:    req,
			}, nil
		}
		source := &trackedBody{Reader: strings.NewReader(retryTestSuccessJSON)}
		responseBodies = append(responseBodies, source)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       source,
			Request:    req,
		}, nil
	}))

	if err := callRetryTestMessages(context.Background(), &client); err != nil {
		t.Fatalf("Messages.New() error = %v", err)
	}
	if len(requestBodies) != 2 || len(requestBodies[0]) == 0 || !reflect.DeepEqual(requestBodies[0], requestBodies[1]) {
		t.Fatalf("replayed request bodies differ: %q", requestBodies)
	}
	for i, body := range responseBodies {
		if got := body.closeCount(); got != 1 {
			t.Errorf("response body %d close count = %d, want 1", i+1, got)
		}
	}
}

func TestMessagesSDKFinalNetworkFailureClearsStaleHTTPClassification(t *testing.T) {
	ctx, state := withOperationState(context.Background())
	attempts := 0
	networkErr := errors.New("fixture network failure")
	client := newRetryTestMessagesClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts < 3 {
			return retryTestResponse(req, http.StatusTooManyRequests, http.Header{"Retry-After": {"0"}}), nil
		}
		return nil, networkErr
	}))

	err := callRetryTestMessages(ctx, &client)
	if !errors.Is(err, networkErr) {
		t.Fatalf("Messages.New() error = %v, want network cause", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if got := state.httpError(); got != nil {
		t.Fatalf("network failure retained stale HTTP classification: %#v", got)
	}
}

func TestMessagesSDKCancellationDuringBackoffPreventsNextTransportCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ctx, _ = withOperationState(ctx)
	backoffStarted := make(chan struct{})
	var mu sync.Mutex
	attempts := 0
	httpClient := newRetryTestObservedHTTPClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		attempts++
		mu.Unlock()
		return retryTestResponse(req, http.StatusBadRequest, http.Header{
			"X-Should-Retry": {"true"},
			"Retry-After":    {"0.05"},
		}), nil
	}))
	doer := &retryCloseSignalDoer{next: httpClient, closed: backoffStarted}
	client := newRetryTestMessagesClientWithHTTPDoer(doer)

	done := make(chan error, 1)
	go func() { done <- callRetryTestMessages(ctx, &client) }()
	select {
	case <-backoffStarted:
	case <-time.After(time.Second):
		t.Fatal("SDK did not close the retry response before backoff")
	}
	canceledAt := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Messages.New() error = %v, want cancellation", err)
		}
		if elapsed := time.Since(canceledAt); elapsed < 40*time.Millisecond {
			t.Fatalf("cancellation returned after %v, want current SDK backoff to complete", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("Messages.New() did not return after retry backoff")
	}
	mu.Lock()
	gotAttempts := attempts
	mu.Unlock()
	if gotAttempts != 1 {
		t.Fatalf("base transport calls = %d, want 1 after cancellation", gotAttempts)
	}
}

func newRetryTestMessagesClient(t *testing.T, base http.RoundTripper) anthropic.Client {
	t.Helper()
	return newRetryTestMessagesClientWithHTTPDoer(newRetryTestObservedHTTPClient(t, base))
}

func newRetryTestObservedHTTPClient(t *testing.T, base http.RoundTripper) *http.Client {
	t.Helper()
	authClient := mustAuthClient(t, opencodeauth.Options{
		APIKey:     "auth-key",
		BaseURL:    retryTestBaseURL,
		UserAgent:  "retry-test/1",
		SessionID:  "fallback-session",
		HTTPClient: &http.Client{Transport: base},
	})
	httpClient, err := newObservedHTTPClient(authClient)
	if err != nil {
		t.Fatalf("newObservedHTTPClient() error = %v", err)
	}
	return httpClient
}

func newRetryTestMessagesClientWithHTTPDoer(httpClient option.HTTPClient) anthropic.Client {
	return anthropic.NewClient(
		option.WithAPIKey("sdk-placeholder"),
		option.WithBaseURL("https://api.example.test/"),
		option.WithHTTPClient(httpClient),
	)
}

type retryCloseSignalDoer struct {
	next   option.HTTPClient
	closed chan struct{}
	once   sync.Once
}

func (d *retryCloseSignalDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.next.Do(req)
	if resp != nil && resp.Body != nil {
		resp.Body = &retryCloseSignalBody{ReadCloser: resp.Body, signal: func() {
			d.once.Do(func() { close(d.closed) })
		}}
	}
	return resp, err
}

type retryCloseSignalBody struct {
	io.ReadCloser
	signal func()
}

func (b *retryCloseSignalBody) Close() error {
	b.signal()
	return b.ReadCloser.Close()
}

func callRetryTestMessages(ctx context.Context, client *anthropic.Client) error {
	_, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		MaxTokens: 16,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hello"))},
		Model:     anthropic.Model("retry-model"),
	})
	return err
}

func retryTestResponse(req *http.Request, status int, headers http.Header) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     headers.Clone(),
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"api_error","message":"fixture"}}`)),
		Request:    req,
	}
}

func retryTestSuccess(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(retryTestSuccessJSON)),
		Request:    req,
	}
}

const retryTestSuccessJSON = `{
	"id":"msg_fixture","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],
	"model":"retry-model","stop_reason":"end_turn","stop_sequence":null,
	"usage":{"input_tokens":1,"output_tokens":1}
}`
