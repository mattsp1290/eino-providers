package opencodego

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	openamodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

func TestObservedJSONBodyPreservesUsagePresence(t *testing.T) {
	tests := []struct {
		name     string
		protocol terminalProtocol
		body     string
		want     *schema.TokenUsage
	}{
		{name: "chat omitted", protocol: terminalChatCompletions, body: `{"choices":[]}`},
		{name: "chat null", protocol: terminalChatCompletions, body: `{"usage":null}`},
		{name: "chat empty object", protocol: terminalChatCompletions, body: `{"usage":{}}`},
		{name: "chat partial", protocol: terminalChatCompletions, body: `{"usage":{"prompt_tokens":4}}`},
		{
			name:     "chat explicit zero",
			protocol: terminalChatCompletions,
			body:     `{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
			want:     &schema.TokenUsage{},
		},
		{
			name:     "chat derives missing total and retains details",
			protocol: terminalChatCompletions,
			body:     `{"usage":{"prompt_tokens":4,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`,
			want: &schema.TokenUsage{
				PromptTokens:            4,
				CompletionTokens:        3,
				TotalTokens:             7,
				PromptTokenDetails:      schema.PromptTokenDetails{CachedTokens: 2},
				CompletionTokensDetails: schema.CompletionTokensDetails{ReasoningTokens: 1},
			},
		},
		{
			name:     "messages includes cache creation in prompt total",
			protocol: terminalMessages,
			body:     `{"usage":{"input_tokens":4,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":5}}`,
			want: &schema.TokenUsage{
				PromptTokens:       9,
				CompletionTokens:   5,
				TotalTokens:        14,
				PromptTokenDetails: schema.PromptTokenDetails{CachedTokens: 2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &operationState{}
			body := newObservedJSONBody(io.NopCloser(strings.NewReader(tt.body)), tt.protocol, state)
			gotBody, err := io.ReadAll(body)
			if err != nil {
				t.Fatal(err)
			}
			if err := body.Close(); err != nil {
				t.Fatal(err)
			}
			if string(gotBody) != tt.body {
				t.Fatalf("body = %q, want byte-identical input", gotBody)
			}
			message := &schema.Message{ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 99}}}
			if err := normalizeGeneratedUsage(message, state); err != nil {
				t.Fatal(err)
			}
			assertTokenUsage(t, message.ResponseMeta.Usage, tt.want)
		})
	}
}

func TestObservedJSONBodyRejectsInvalidAndOversizedUsage(t *testing.T) {
	invalid := []string{
		`{"usage":"wrong"}`,
		`{"usage":{"prompt_tokens":-1,"completion_tokens":2}}`,
		`{"usage":{"prompt_tokens":1.5,"completion_tokens":2}}`,
		`{"usage":{"prompt_tokens":1,"completion_tokens":"2"}}`,
		`{"usage":{"prompt_tokens_details":4,"prompt_tokens":1,"completion_tokens":2}}`,
		fmt.Sprintf(`{"usage":{"prompt_tokens":%d,"completion_tokens":1}}`, maxInt()),
	}
	for _, body := range invalid {
		state := &operationState{}
		observed := newObservedJSONBody(io.NopCloser(strings.NewReader(body)), terminalChatCompletions, state)
		_, _ = io.ReadAll(observed)
		_ = observed.Close()
		if err := normalizeGeneratedUsage(&schema.Message{}, state); !errors.Is(err, errMalformedUsage) {
			t.Fatalf("body %s: error = %v, want malformed usage", body, err)
		}
	}

	state := &operationState{}
	oversized := newObservedJSONBody(
		io.NopCloser(strings.NewReader(strings.Repeat("x", maxObservedJSONBodyBytes+1))),
		terminalChatCompletions,
		state,
	)
	_, err := io.ReadAll(oversized)
	if !errors.Is(err, errOversizedJSONObservation) {
		t.Fatalf("ReadAll() error = %v, want size limit", err)
	}
	if got := len(oversized.(*observedJSONBody).buffer); got != maxObservedJSONBodyBytes {
		t.Fatalf("captured bytes = %d, want %d", got, maxObservedJSONBodyBytes)
	}
}

func TestObserveResponseBodyWiresNativeRoutesOnly(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
		want *schema.TokenUsage
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			body: `{"usage":{"prompt_tokens":2,"completion_tokens":3}}`,
			want: &schema.TokenUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5},
		},
		{
			name: "messages",
			path: "/v1/messages",
			body: `{"usage":{"input_tokens":4,"output_tokens":5}}`,
			want: &schema.TokenUsage{PromptTokens: 4, CompletionTokens: 5, TotalTokens: 9},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "https://example.com"+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			state := &operationState{}
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(tt.body)), Header: make(http.Header)}
			resp.Header.Set("Content-Type", "application/json")
			observeResponseBody(req, resp, state)
			got, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if string(got) != tt.body {
				t.Fatalf("body = %q", got)
			}
			assertTokenUsage(t, observedUsageTokenUsage(state.bodySnapshot()), tt.want)
		})
	}

	state := &operationState{}
	source := &chunkedReadCloser{data: []byte(`{"usage":{"input_tokens":9,"output_tokens":9}}`), chunk: 64}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	resp := &http.Response{Body: source, Header: make(http.Header)}
	observeResponseBody(req, resp, state)
	if resp.Body != source {
		t.Fatal("Responses body was wrapped by the SDK observer")
	}
	if got := state.bodySnapshot(); got != (bodyObservation{}) {
		t.Fatalf("Responses route changed operation state: %#v", got)
	}
}

func TestGenerateUsageNormalizationCorrectsPinnedSDKPresence(t *testing.T) {
	chatResponse := func(usage string) string {
		return `{"id":"chat-id","object":"chat.completion","model":"fixture","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]` + usage + `}`
	}
	messageResponse := func(usage string) string {
		return `{"id":"message-id","type":"message","role":"assistant","model":"fixture","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"` + usage + `}`
	}

	for _, tt := range []struct {
		name     string
		response string
		want     *schema.TokenUsage
	}{
		{name: "chat omitted", response: chatResponse("")},
		{name: "chat zero", response: chatResponse(`,"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`), want: &schema.TokenUsage{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			httpClient := newRetryTestObservedHTTPClient(t, responseTransport(tt.response))
			client, err := openamodel.NewChatModel(context.Background(), &openamodel.ChatModelConfig{
				APIKey: "sdk-placeholder", BaseURL: "https://api.example.test/v1", HTTPClient: httpClient, Model: "fixture",
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, state := withOperationState(context.Background())
			message, err := client.Generate(ctx, []*schema.Message{schema.UserMessage("hello")})
			if err != nil {
				t.Fatal(err)
			}
			if message.ResponseMeta == nil || message.ResponseMeta.Usage == nil {
				t.Fatal("pinned OpenAI converter no longer synthesizes usage; fixture assumption changed")
			}
			if err := normalizeGeneratedUsage(message, state); err != nil {
				t.Fatal(err)
			}
			assertTokenUsage(t, message.ResponseMeta.Usage, tt.want)
		})
	}

	for _, tt := range []struct {
		name     string
		response string
		want     *schema.TokenUsage
	}{
		{name: "messages omitted", response: messageResponse("")},
		{name: "messages zero", response: messageResponse(`,"usage":{"input_tokens":0,"output_tokens":0}`), want: &schema.TokenUsage{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newRetryTestMessagesClientWithHTTPClient(t, newRetryTestObservedHTTPClient(t, responseTransport(tt.response)))
			ctx, state := withOperationState(context.Background())
			message, err := client.Generate(ctx, []*schema.Message{schema.UserMessage("hello")})
			if err != nil {
				t.Fatal(err)
			}
			if message.ResponseMeta == nil || message.ResponseMeta.Usage == nil {
				t.Fatal("pinned Claude converter no longer synthesizes usage; fixture assumption changed")
			}
			if err := normalizeGeneratedUsage(message, state); err != nil {
				t.Fatal(err)
			}
			assertTokenUsage(t, message.ResponseMeta.Usage, tt.want)
		})
	}
}

func responseTransport(body string) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
}

func TestTerminalBodyObserverCapturesFinalChatUsage(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"a"}}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`,
		"",
		`data: {"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":5,"total_tokens":10}}`,
		"",
		"data: [DONE]",
		"",
		"ignored",
	}, "\n")
	state := &operationState{}
	source := &chunkedReadCloser{data: []byte(body), chunk: 7}
	got, err := io.ReadAll(newTerminalBodyObserver(source, terminalChatCompletions, state))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "ignored") {
		t.Fatalf("post-terminal bytes leaked: %q", got)
	}
	observation := state.bodySnapshot()
	if !observation.terminal {
		t.Fatal("terminal marker was not recorded")
	}
	assertTokenUsage(t, observedUsageTokenUsage(observation), &schema.TokenUsage{PromptTokens: 4, CompletionTokens: 5, TotalTokens: 10})
}

func TestTerminalBodyObserverCapturesMessagesCumulativeUsage(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"output_tokens":0}}}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","usage":{"output_tokens":5}}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","usage":{"output_tokens":7}}`,
		"",
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		"",
		"",
	}, "\r\n")
	state := &operationState{}
	got, err := io.ReadAll(newTerminalBodyObserver(&chunkedReadCloser{data: []byte(body), chunk: 3}, terminalMessages, state))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(body)) {
		t.Fatal("observer did not preserve CRLF bytes")
	}
	assertTokenUsage(t, observedUsageTokenUsage(state.bodySnapshot()), &schema.TokenUsage{
		PromptTokens:       9,
		CompletionTokens:   7,
		TotalTokens:        16,
		PromptTokenDetails: schema.PromptTokenDetails{CachedTokens: 3},
	})
}

func TestTerminalBodyObserverMessagesUsagePresence(t *testing.T) {
	for _, tt := range []struct {
		name  string
		usage string
		want  *schema.TokenUsage
	}{
		{name: "omitted", usage: ""},
		{name: "null", usage: `"usage":null`},
		{name: "partial", usage: `"usage":{"input_tokens":0}`},
		{name: "explicit zero", usage: `"usage":{"input_tokens":0,"output_tokens":0}`, want: &schema.TokenUsage{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{" + tt.usage + "}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
			state := &operationState{}
			_, err := io.ReadAll(newTerminalBodyObserver(io.NopCloser(strings.NewReader(body)), terminalMessages, state))
			if err != nil {
				t.Fatal(err)
			}
			assertTokenUsage(t, observedUsageTokenUsage(state.bodySnapshot()), tt.want)
		})
	}
}

func TestTerminalBodyObserverUsagePresenceAndErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr error
		wantNil bool
	}{
		{name: "omitted", body: "data: {\"choices\":[]}\n\ndata: [DONE]\n\n", wantNil: true},
		{name: "null", body: "data: {\"choices\":[],\"usage\":null}\n\ndata: [DONE]\n\n", wantNil: true},
		{name: "partial", body: "data: {\"usage\":{\"prompt_tokens\":0}}\n\ndata: [DONE]\n\n", wantNil: true},
		{name: "malformed", body: "data: {\"usage\":\"bad\"}\n\ndata: [DONE]\n\n", wantErr: errMalformedUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &operationState{}
			_, err := io.ReadAll(newTerminalBodyObserver(io.NopCloser(strings.NewReader(tt.body)), terminalChatCompletions, state))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantNil && observedUsageTokenUsage(state.bodySnapshot()) != nil {
				t.Fatal("incomplete usage was reported as available")
			}
		})
	}
}

func TestTerminalBodyObserverCompletesWithoutSocketEOF(t *testing.T) {
	for _, tt := range []struct {
		protocol terminalProtocol
		body     string
	}{
		{terminalChatCompletions, "data: [DONE]\n\n"},
		{terminalMessages, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"},
	} {
		source := newTerminalThenBlockBody(tt.body)
		done := make(chan error, 1)
		go func() {
			_, err := io.ReadAll(newTerminalBodyObserver(source, tt.protocol, &operationState{}))
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("observer waited for socket EOF after terminal frame")
		}
		if source.closeCount() != 1 {
			t.Fatalf("close count = %d, want 1", source.closeCount())
		}
	}
}

func TestTerminalBodyObserverAcceptsExactEventLimit(t *testing.T) {
	prefix, suffix := "data: {\"x\":\"", "\"}\n\n"
	event := prefix + strings.Repeat("x", maxObservedSSEEventBytes-len(prefix)-len(suffix)) + suffix
	body := event + "data: [DONE]\n\n"
	got, err := io.ReadAll(newTerminalBodyObserver(io.NopCloser(strings.NewReader(body)), terminalChatCompletions))
	if err != nil || len(got) != len(body) {
		t.Fatalf("exact-limit event: bytes = %d, error = %v", len(got), err)
	}
}

func TestObservingTransportResetsUsageAcrossAttempts(t *testing.T) {
	responses := []string{
		`{"usage":{"prompt_tokens":90,"completion_tokens":90}}`,
		`{"usage":{"prompt_tokens":2,"completion_tokens":3}}`,
	}
	attempt := 0
	transport := &observingTransport{next: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if attempt == len(responses) {
			return nil, errors.New("network failure")
		}
		body := responses[attempt]
		attempt++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	ctx, state := withOperationState(context.Background())
	request := func() *http.Request {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test/v1/chat/completions", nil)
		return req
	}
	for range responses {
		resp, err := transport.RoundTrip(request())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
	assertTokenUsage(t, observedUsageTokenUsage(state.bodySnapshot()), &schema.TokenUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5})
	resp, err := transport.RoundTrip(request())
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("network attempt succeeded")
	}
	if got := state.bodySnapshot(); got != (bodyObservation{}) {
		t.Fatalf("network attempt retained stale usage: %#v", got)
	}
}

func TestObservedJSONBodyConcurrentCloseUnblocksRead(t *testing.T) {
	source := newTerminalThenBlockBody(`{"usage":`)
	body := newObservedJSONBody(source, terminalChatCompletions, &operationState{})
	buffer := make([]byte, 32)
	if n, err := body.Read(buffer); n == 0 || err != nil {
		t.Fatalf("initial Read() = %d, %v", n, err)
	}
	done := make(chan error, 1)
	go func() { _, err := body.Read(buffer); done <- err }()
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock concurrent Read")
	}
	if source.closeCount() != 1 {
		t.Fatalf("close count = %d, want 1", source.closeCount())
	}
}

func assertTokenUsage(t *testing.T, got, want *schema.TokenUsage) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("usage = %#v, want %#v", got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("usage = %#v, want %#v", got, want)
	}
}

type terminalThenBlockBody struct {
	data      []byte
	closed    chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	closes    int
}

func newTerminalThenBlockBody(data string) *terminalThenBlockBody {
	return &terminalThenBlockBody{data: []byte(data), closed: make(chan struct{})}
}

func (b *terminalThenBlockBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	if len(b.data) > 0 {
		n := copy(p, b.data)
		b.data = b.data[n:]
		b.mu.Unlock()
		return n, nil
	}
	b.mu.Unlock()
	<-b.closed
	return 0, io.EOF
}

func (b *terminalThenBlockBody) Close() error {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closes++
		b.mu.Unlock()
		close(b.closed)
	})
	return nil
}

func (b *terminalThenBlockBody) closeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closes
}
