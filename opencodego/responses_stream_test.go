package opencodego

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestResponsesStreamAssemblesTextReasoningAndParallelTools(t *testing.T) {
	body := responsesSSE(
		`{"type":"future.event","output_index":"opaque","delta":{"future":true},"arguments":[]}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_1","type":"reasoning"}}`,
		`{"type":"response.reasoning_text.delta","output_index":0,"delta":"think"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_1","type":"reasoning","status":"completed","encrypted_content":"opaque","summary":[]}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"hello "}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"world"}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello world"}]}}`,
		`{"type":"response.output_item.added","output_index":2,"item":{"id":"fc_1","type":"function_call","name":"weather","arguments":"","call_id":"call_1"}}`,
		`{"type":"response.output_item.added","output_index":3,"item":{"id":"fc_2","type":"function_call","name":"clock","arguments":"","call_id":"call_2"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"city\":"}`,
		`{"type":"response.function_call_arguments.delta","output_index":3,"delta":"{"}`,
		`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"\"NYC\"}"}`,
		`{"type":"response.function_call_arguments.delta","output_index":3,"delta":"}"}`,
		`{"type":"response.function_call_arguments.done","output_index":2,"arguments":"{\"city\":\"NYC\"}"}`,
		`{"type":"response.output_item.done","output_index":2,"item":{"id":"fc_1","type":"function_call","status":"completed","name":"weather","arguments":"{\"city\":\"NYC\"}","call_id":"call_1"}}`,
		`{"type":"response.output_item.done","output_index":3,"item":{"id":"fc_2","type":"function_call","status":"completed","name":"clock","arguments":"{}","call_id":"call_2"}}`,
		`{"type":"response.completed","response":{"status":"completed","output":[{"id":"rs_1","type":"reasoning","status":"completed","encrypted_content":"opaque","summary":[]},{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello world"}]},{"id":"fc_1","type":"function_call","status":"completed","name":"weather","arguments":"{\"city\":\"NYC\"}","call_id":"call_1"},{"id":"fc_2","type":"function_call","status":"completed","name":"clock","arguments":"{}","call_id":"call_2"}],"usage":{"input_tokens":8,"output_tokens":5,"total_tokens":13,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":1}},"error":null}}`,
	)
	message := parseAndConcatResponses(t, body)
	if message.Content != "hello world" || message.ReasoningContent != "think" || len(message.ToolCalls) != 2 {
		t.Fatalf("message = %#v", message)
	}
	if message.ToolCalls[0].Function.Arguments != `{"city":"NYC"}` || message.ToolCalls[1].Function.Arguments != `{}` ||
		message.ToolCalls[0].Index == nil || *message.ToolCalls[0].Index != 0 || message.ToolCalls[1].Index == nil || *message.ToolCalls[1].Index != 1 {
		t.Fatalf("tool calls = %#v", message.ToolCalls)
	}
	if message.ResponseMeta == nil || message.ResponseMeta.FinishReason != "tool_calls" || message.ResponseMeta.Usage == nil || message.ResponseMeta.Usage.TotalTokens != 13 {
		t.Fatalf("response metadata = %#v", message.ResponseMeta)
	}
	reasoning, ok := message.Extra[responsesReasoningItemsKey].([]json.RawMessage)
	if !ok || len(reasoning) != 1 || !bytes.Contains(reasoning[0], []byte("opaque")) {
		t.Fatalf("reasoning items = %#v", message.Extra)
	}
}

func TestResponsesStreamSupportsCRLFMultilineAndTerminalReconciliation(t *testing.T) {
	terminal := `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello"}]},{"type":"function_call","status":"completed","name":"tool","arguments":"{\"x\":1}","call_id":"call"}]}}`
	cut := strings.Index(terminal, `,"response"`)
	body := ": heartbeat\r\n\r\n" + responsesSSE(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"hel"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","status":"completed","name":"tool","arguments":"{\"x\":1}","call_id":"call"}}`,
	)
	body = strings.ReplaceAll(body, "\n", "\r\n") + "data: " + terminal[:cut+1] + "\r\ndata: " + terminal[cut+1:] + "\r\n\r\n"
	message := parseAndConcatResponses(t, body)
	if message.Content != "hello" || len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Arguments != `{"x":1}` {
		t.Fatalf("message = %#v", message)
	}
}

func TestResponsesStreamRejectsMalformedAndIncompleteEvents(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "malformed json", body: "data: {\n\n", want: errInvalidResponsesEvent},
		{name: "missing type", body: responsesSSE(`{"delta":"x"}`), want: errInvalidResponsesEvent},
		{name: "missing delta", body: responsesSSE(`{"type":"response.output_text.delta","output_index":0}`), want: errInvalidResponsesEvent},
		{name: "negative output index", body: responsesSSE(`{"type":"response.output_item.added","output_index":-1,"item":{"type":"message"}}`), want: errInvalidResponsesEvent},
		{name: "unknown call delta", body: responsesSSE(`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{}"}`), want: errInvalidResponsesEvent},
		{name: "duplicate output", body: responsesSSE(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`, `{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`), want: errInvalidResponsesEvent},
		{name: "mismatched call", body: responsesSSE(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","name":"a","arguments":"","call_id":"call"}}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","name":"b","arguments":"{}","call_id":"call"}}`), want: errInvalidResponsesEvent},
		{name: "argument disagreement", body: responsesSSE(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","name":"a","arguments":"","call_id":"call"}}`, `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{}"}`, `{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"x\":1}"}`), want: errInvalidResponsesEvent},
		{name: "duplicate call id", body: responsesSSE(`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","name":"a","arguments":"{}","call_id":"call"}}`, `{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","name":"b","arguments":"{}","call_id":"call"}}`), want: errInvalidResponsesEvent},
		{name: "duplicate item id", body: responsesSSE(`{"type":"response.output_item.added","output_index":0,"item":{"id":"item","type":"message","role":"assistant","content":[]}}`, `{"type":"response.output_item.added","output_index":1,"item":{"id":"item","type":"message","role":"assistant","content":[]}}`), want: errInvalidResponsesEvent},
		{name: "failed", body: responsesSSE(`{"type":"response.failed","response":{"error":{"message":"secret"}}}`), want: einoproviders.ErrProviderAPI},
		{name: "incomplete", body: responsesSSE(`{"type":"response.incomplete","response":{}}`), want: einoproviders.ErrProviderAPI},
		{name: "native error", body: responsesSSE(`{"type":"error","message":"secret"}`), want: einoproviders.ErrProviderAPI},
		{name: "done only", body: responsesSSE(`[DONE]`), want: errResponsesStreamEnded},
		{name: "premature eof", body: "", want: errResponsesStreamEnded},
		{name: "unterminated terminal", body: `data: {"type":"response.completed","response":{"status":"completed","output":[]}}`, want: errResponsesStreamEnded},
		{name: "bad terminal status", body: responsesSSE(`{"type":"response.completed","response":{"status":"failed","output":[]}}`), want: einoproviders.ErrProviderAPI},
		{name: "missing terminal output", body: responsesSSE(`{"type":"response.completed","response":{"status":"completed"}}`), want: einoproviders.ErrProviderAPI},
		{name: "terminal text mismatch", body: responsesSSE(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","content":[]}}`, `{"type":"response.output_text.delta","output_index":0,"delta":"a"}`, `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"b"}]}]}}`), want: einoproviders.ErrProviderAPI},
		{name: "done and terminal text mismatch", body: responsesSSE(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"first"}]}}`, `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"second"}]}]}}`), want: einoproviders.ErrProviderAPI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseResponsesForTest(tt.body)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaked event: %v", err)
			}
		})
	}
}

func TestResponsesStreamBoundsSingleAndMultilineEvents(t *testing.T) {
	prefix := `{"type":"future.event"}`
	wireOverhead := len("data: ") + len("\n")
	exactPayload := prefix + strings.Repeat(" ", maxResponsesEventBytes-len(prefix)-wireOverhead)
	_, _, err := parseResponsesForTest("data: " + exactPayload + "\n\n")
	if !errors.Is(err, errResponsesStreamEnded) {
		t.Fatalf("exact limit error = %v", err)
	}
	_, _, err = parseResponsesForTest("data: " + exactPayload + " \n\n")
	if !errors.Is(err, errResponsesEventTooLarge) {
		t.Fatalf("over limit error = %v", err)
	}
	first := maxResponsesEventBytes / 2
	second := maxResponsesEventBytes - first
	multiline := "data: " + prefix + strings.Repeat(" ", first-len(prefix)) + "\ndata: " + strings.Repeat(" ", second) + "\n\n"
	_, _, err = parseResponsesForTest(multiline)
	if !errors.Is(err, errResponsesEventTooLarge) {
		t.Fatalf("multiline aggregate error = %v", err)
	}
	ignored := "event: " + strings.Repeat("x", maxResponsesEventBytes/2) + "\n"
	_, _, err = parseResponsesForTest(ignored + ignored + "\n")
	if !errors.Is(err, errResponsesEventTooLarge) {
		t.Fatalf("non-data aggregate error = %v", err)
	}
}

func TestResponsesStreamHonorsContextAndConsumerClosure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := parseResponsesStream(ctx, strings.NewReader("\n"), func(*schema.Message) bool { return false })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v", err)
	}
	body := responsesSSE(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"x"}`,
	)
	_, err = parseResponsesStream(context.Background(), strings.NewReader(body), func(*schema.Message) bool { return true })
	if !errors.Is(err, errResponsesConsumerGone) {
		t.Fatalf("consumer error = %v", err)
	}
}

func TestChatModelResponsesStreamAuthenticatesAndStopsAtTerminal(t *testing.T) {
	serverReleased := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverReleased)
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer key" ||
			r.Header.Get("X-OpenCode-Session") != "session" || r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("request path/auth/session/accept = %q/%q/%q/%q", r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-OpenCode-Session"), r.Header.Get("Accept"))
		}
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		for _, field := range [][]byte{[]byte(`"stream":true`), []byte(`"store":false`), []byte(`"reasoning.encrypted_content"`)} {
			if !bytes.Contains(requestBody, field) {
				t.Errorf("request body missing %s: %s", field, requestBody)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, responsesSSE(
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","content":[]}}`,
			`{"type":"response.output_text.delta","output_index":0,"delta":"hello"}`,
			`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
		))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	stream, err := newResponsesModelAtServer(t, server).Stream(context.Background(), []*schema.Message{schema.UserMessage("hello")})
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := receiveResponsesChunks(stream)
	if err != nil {
		t.Fatal(err)
	}
	message, err := schema.ConcatMessages(chunks)
	if err != nil || message.Content != "hello" || message.ResponseMeta == nil || message.ResponseMeta.Usage.TotalTokens != 3 {
		t.Fatalf("message/error = %#v/%v", message, err)
	}
	select {
	case <-serverReleased:
	case <-time.After(time.Second):
		t.Fatal("terminal completion did not close the held-open response")
	}
}

func TestChatModelResponsesStreamDeliversTextBeforeCompletion(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, responsesSSE(
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","content":[]}}`,
			`{"type":"response.output_text.delta","output_index":0,"delta":"visible"}`,
		))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, responsesSSE(
			`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"visible"}]}]}}`,
		))
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(server.Close)
	stream, err := newResponsesModelAtServer(t, server).Stream(context.Background(), []*schema.Message{schema.UserMessage("x")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	first := make(chan struct {
		message *schema.Message
		err     error
	}, 1)
	go func() {
		message, recvErr := stream.Recv()
		first <- struct {
			message *schema.Message
			err     error
		}{message, recvErr}
	}()
	select {
	case result := <-first:
		if result.err != nil || result.message == nil || result.message.Content != "visible" {
			t.Fatalf("first chunk/error = %#v/%v", result.message, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("text delta was buffered until completion")
	}
	releaseOnce.Do(func() { close(release) })
	for {
		_, err = stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestResponsesModelStreamCancellationClosesBlockedBody(t *testing.T) {
	body := newBlockingResponsesBody()
	model := newPublicResponsesModel(t, responsesHTTPClient(http.StatusOK, "text/event-stream", body))
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := model.Stream(ctx, []*schema.Message{schema.UserMessage("x")})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_, err = stream.Recv()
	stream.Close()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Recv error = %v", err)
	}
	select {
	case <-body.closed:
	case <-time.After(time.Second):
		t.Fatal("cancel did not close body")
	}
	if body.closeCount() != 1 {
		t.Fatalf("close count = %d", body.closeCount())
	}
}

func TestResponsesModelStreamReaderCloseReleasesBlockedProducer(t *testing.T) {
	events := []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","content":[]}}`}
	for range 40 {
		events = append(events, `{"type":"response.output_text.delta","output_index":0,"delta":"x"}`)
	}
	body := &trackedBody{Reader: strings.NewReader(responsesSSE(events...))}
	stream, err := newPublicResponsesModel(t, responsesHTTPClient(http.StatusOK, "text/event-stream", body)).Stream(context.Background(), []*schema.Message{schema.UserMessage("x")})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	stream.Close()
	deadline := time.Now().Add(time.Second)
	for body.closeCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if body.closeCount() != 1 {
		t.Fatal("reader Close did not release producer and close body")
	}
}

func TestResponsesModelStreamContainsBodyPanics(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		body := &panicResponsesBody{canary: "secret-read-panic"}
		stream, err := newPublicResponsesModel(t, responsesHTTPClient(http.StatusOK, "text/event-stream", body)).Stream(context.Background(), []*schema.Message{schema.UserMessage("x")})
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		_, err = stream.Recv()
		if !errors.Is(err, einoproviders.ErrProviderAPI) || strings.Contains(err.Error(), body.canary) {
			t.Fatalf("panic error = %v", err)
		}
		if body.closeCount() != 1 {
			t.Fatalf("close count = %d", body.closeCount())
		}
	})
	t.Run("close", func(t *testing.T) {
		body := &panicCloseResponsesBody{Reader: strings.NewReader(responsesSSE(`{"type":"response.completed","response":{"status":"completed","output":[]}}`)), canary: "secret-close-panic"}
		stream, err := newPublicResponsesModel(t, responsesHTTPClient(http.StatusOK, "text/event-stream", body)).Stream(context.Background(), []*schema.Message{schema.UserMessage("x")})
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		_, err = stream.Recv()
		if !errors.Is(err, einoproviders.ErrProviderAPI) || strings.Contains(err.Error(), body.canary) {
			t.Fatalf("panic error = %v", err)
		}
		if body.closeCount() != 1 {
			t.Fatalf("close count = %d", body.closeCount())
		}
	})
}

func TestResponsesModelStreamRejectsInvalidHTTPResponseAndClosesBody(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        io.ReadCloser
		wantAPI     bool
	}{
		{name: "status", status: http.StatusInternalServerError, contentType: "text/event-stream", wantAPI: true},
		{name: "content type", status: http.StatusOK, contentType: "application/json", wantAPI: true},
		{name: "close panic", status: http.StatusOK, contentType: "application/json", body: &panicCloseResponsesBody{Reader: strings.NewReader("secret response"), canary: "secret-close-panic"}, wantAPI: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := tt.body
			if body == nil {
				body = &trackedBody{Reader: strings.NewReader("secret response")}
			}
			_, err := newPublicResponsesModel(t, responsesHTTPClient(tt.status, tt.contentType, body)).Stream(context.Background(), []*schema.Message{schema.UserMessage("x")})
			if err == nil || (tt.wantAPI && !errors.Is(err, einoproviders.ErrProviderAPI)) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("Stream error = %v", err)
			}
			counter, ok := body.(interface{ closeCount() int })
			if !ok {
				t.Fatalf("body %T does not expose close count", body)
			}
			if counter.closeCount() != 1 {
				t.Fatalf("close count = %d", counter.closeCount())
			}
		})
	}
}

func responsesHTTPClient(status int, contentType string, body io.ReadCloser) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: body, Request: req}, nil
	})}
}

func receiveResponsesChunks(stream *schema.StreamReader[*schema.Message]) ([]*schema.Message, error) {
	defer stream.Close()
	var chunks []*schema.Message
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return chunks, nil
		}
		if err != nil {
			return chunks, err
		}
		chunks = append(chunks, chunk)
	}
}

func newResponsesModelAtServer(t *testing.T, server *httptest.Server) model.ToolCallingChatModel {
	t.Helper()
	adapter, err := NewChatModel(context.Background(), ChatModelConfig{
		Model: "fixture", Protocol: ProtocolResponses, APIKey: "key", UserAgent: "responses-stream-test/1",
		SessionID: "session", BaseURL: server.URL + "/v1", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func newPublicResponsesModel(t *testing.T, client *http.Client) model.ToolCallingChatModel {
	t.Helper()
	adapter, err := NewChatModel(context.Background(), ChatModelConfig{
		Model: "fixture", Protocol: ProtocolResponses, APIKey: "key", UserAgent: "responses-stream-test/1",
		SessionID: "session", BaseURL: "https://api.example.test/v1", HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

type blockingResponsesBody struct {
	closed chan struct{}
	once   sync.Once
	mu     sync.Mutex
	closes int
}

func newBlockingResponsesBody() *blockingResponsesBody {
	return &blockingResponsesBody{closed: make(chan struct{})}
}

func (body *blockingResponsesBody) Read([]byte) (int, error) {
	<-body.closed
	return 0, errors.New("body closed")
}

func (body *blockingResponsesBody) Close() error {
	body.mu.Lock()
	body.closes++
	body.mu.Unlock()
	body.once.Do(func() { close(body.closed) })
	return nil
}

func (body *blockingResponsesBody) closeCount() int {
	body.mu.Lock()
	defer body.mu.Unlock()
	return body.closes
}

type panicResponsesBody struct {
	canary string
	mu     sync.Mutex
	closes int
}

func (body *panicResponsesBody) Read([]byte) (int, error) { panic(body.canary) }

func (body *panicResponsesBody) Close() error {
	body.mu.Lock()
	defer body.mu.Unlock()
	body.closes++
	return nil
}

func (body *panicResponsesBody) closeCount() int {
	body.mu.Lock()
	defer body.mu.Unlock()
	return body.closes
}

type panicCloseResponsesBody struct {
	io.Reader
	canary string
	mu     sync.Mutex
	closes int
}

func (body *panicCloseResponsesBody) Close() error {
	body.mu.Lock()
	body.closes++
	body.mu.Unlock()
	panic(body.canary)
}

func (body *panicCloseResponsesBody) closeCount() int {
	body.mu.Lock()
	defer body.mu.Unlock()
	return body.closes
}

func parseAndConcatResponses(t *testing.T, body string) *schema.Message {
	t.Helper()
	terminal, chunks, err := parseResponsesForTest(body)
	if err != nil {
		t.Fatal(err)
	}
	chunks = append(chunks, terminal)
	message, err := schema.ConcatMessages(chunks)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func parseResponsesForTest(body string) (*schema.Message, []*schema.Message, error) {
	var chunks []*schema.Message
	terminal, err := parseResponsesStream(context.Background(), strings.NewReader(body), func(message *schema.Message) bool {
		chunks = append(chunks, message)
		return false
	})
	return terminal, chunks, err
}

func responsesSSE(events ...string) string {
	var body strings.Builder
	for _, event := range events {
		body.WriteString("data: ")
		body.WriteString(event)
		body.WriteString("\n\n")
	}
	return body.String()
}
