package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	codexauth "github.com/mattsp1290/codex-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// sseBody frames each JSON string as an SSE `data:` event.
func sseBody(events ...string) io.ReadCloser {
	var b strings.Builder
	for _, e := range events {
		b.WriteString("data: ")
		b.WriteString(e)
		b.WriteString("\n\n")
	}
	return io.NopCloser(strings.NewReader(b.String()))
}

func sseOKResponse(events ...string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       sseBody(events...),
		Header:     make(http.Header),
	}
}

func newTestChatModel(t *testing.T, stub stubRoundTripper) model.ToolCallingChatModel {
	t.Helper()
	cm, err := NewChatModelWithHTTPClient(context.Background(), &http.Client{Transport: stub}, ChatModelConfig{
		Model: "gpt-5.5",
	})
	if err != nil {
		t.Fatalf("NewChatModelWithHTTPClient: %v", err)
	}
	return cm
}

func drain(t *testing.T, sr *schema.StreamReader[*schema.Message]) []*schema.Message {
	t.Helper()
	defer sr.Close()
	var out []*schema.Message
	for {
		msg, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		out = append(out, msg)
	}
}

func TestChatModel_Stream_Text(t *testing.T) {
	t.Parallel()
	stub := stubRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != codexauth.CodexEndpoint {
			t.Errorf("URL = %q, want codex endpoint", r.URL.String())
		}
		return sseOKResponse(
			`{"type":"response.created","response":{}}`,
			`{"type":"response.output_text.delta","delta":"Hello"}`,
			`{"type":"response.output_text.delta","delta":", world"}`,
			`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":11,"output_tokens":3,"total_tokens":14}}}`,
		), nil
	})
	cm := newTestChatModel(t, stub)

	sr, err := cm.Stream(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := drain(t, sr)
	merged, err := schema.ConcatMessages(chunks)
	if err != nil {
		t.Fatalf("ConcatMessages: %v", err)
	}
	if merged.Content != "Hello, world" {
		t.Errorf("content = %q, want %q", merged.Content, "Hello, world")
	}
	usage := einoproviders.ExtractUsage(merged)
	if !usage.Available || usage.InputTokens != 11 || usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want 11/3 available", usage)
	}
	if merged.ResponseMeta == nil || merged.ResponseMeta.FinishReason != "stop" {
		t.Errorf("finish reason = %+v, want stop", merged.ResponseMeta)
	}
}

func TestChatModel_Stream_ToolCall(t *testing.T) {
	t.Parallel()
	stub := stubRoundTripper(func(*http.Request) (*http.Response, error) {
		return sseOKResponse(
			`{"type":"response.output_item.added","item":{"type":"function_call","name":"get_weather","call_id":"call_abc","arguments":""}}`,
			`{"type":"response.output_item.done","item":{"type":"function_call","name":"get_weather","call_id":"call_abc","arguments":"{\"city\":\"SF\"}"}}`,
			`{"type":"response.completed","response":{"id":"resp_2","usage":{"input_tokens":20,"output_tokens":8,"total_tokens":28}}}`,
		), nil
	})
	cm := newTestChatModel(t, stub)

	sr, err := cm.Stream(context.Background(), []*schema.Message{schema.UserMessage("weather in SF?")})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	merged, err := schema.ConcatMessages(drain(t, sr))
	if err != nil {
		t.Fatalf("ConcatMessages: %v", err)
	}
	if len(merged.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(merged.ToolCalls))
	}
	tc := merged.ToolCalls[0]
	if tc.ID != "call_abc" || tc.Function.Name != "get_weather" {
		t.Errorf("tool call id/name = %q/%q, want call_abc/get_weather", tc.ID, tc.Function.Name)
	}
	if tc.Function.Arguments != `{"city":"SF"}` {
		t.Errorf("arguments = %q, want %q", tc.Function.Arguments, `{"city":"SF"}`)
	}
	if merged.ResponseMeta == nil || merged.ResponseMeta.FinishReason != "tool_calls" {
		t.Errorf("finish reason = %+v, want tool_calls", merged.ResponseMeta)
	}
}

func TestChatModel_Generate(t *testing.T) {
	t.Parallel()
	stub := stubRoundTripper(func(*http.Request) (*http.Response, error) {
		return sseOKResponse(
			`{"type":"response.output_text.delta","delta":"final "}`,
			`{"type":"response.output_text.delta","delta":"answer"}`,
			`{"type":"response.completed","response":{"id":"r","usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`,
		), nil
	})
	cm := newTestChatModel(t, stub)
	msg, err := cm.Generate(context.Background(), []*schema.Message{schema.UserMessage("q")})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if msg.Content != "final answer" {
		t.Errorf("content = %q, want %q", msg.Content, "final answer")
	}
}

func TestChatModel_MultiTurn_InputEncoding(t *testing.T) {
	t.Parallel()
	var captured map[string]any
	stub := stubRoundTripper(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return sseOKResponse(
			`{"type":"response.output_text.delta","delta":"ok"}`,
			`{"type":"response.completed","response":{"id":"r"}}`,
		), nil
	})
	cm := newTestChatModel(t, stub)

	reasoningItem := json.RawMessage(`{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"ENC"}`)
	history := []*schema.Message{
		schema.SystemMessage("be concise"),
		schema.UserMessage("weather in SF?"),
		{
			Role:      schema.Assistant,
			ToolCalls: []schema.ToolCall{{ID: "call_abc", Type: "function", Function: schema.FunctionCall{Name: "get_weather", Arguments: `{"city":"SF"}`}}},
			Extra:     map[string]any{extraKeyReasoningItems: []json.RawMessage{reasoningItem}},
		},
		schema.ToolMessage(`{"temp":"68F"}`, "call_abc"),
	}

	if _, err := cm.Generate(context.Background(), history); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if captured["instructions"] != "be concise" {
		t.Errorf("instructions = %v, want 'be concise'", captured["instructions"])
	}
	if captured["stream"] != true {
		t.Errorf("stream = %v, want true", captured["stream"])
	}
	if captured["store"] != false {
		t.Errorf("store = %v, want false", captured["store"])
	}
	if _, ok := captured["reasoning"]; !ok {
		t.Error("request must always include the reasoning field (null when unset)")
	}
	if _, ok := captured["max_tokens"]; ok {
		t.Error("request must not include max_tokens")
	}
	if _, ok := captured["max_completion_tokens"]; ok {
		t.Error("request must not include max_completion_tokens")
	}

	input, ok := captured["input"].([]any)
	if !ok {
		t.Fatalf("input is %T, want array", captured["input"])
	}
	types := make([]string, 0, len(input))
	for _, it := range input {
		m, _ := it.(map[string]any)
		types = append(types, fmt.Sprintf("%v", m["type"]))
	}
	want := []string{"message", "reasoning", "function_call", "function_call_output"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("input item types = %v, want %v", types, want)
	}

	// reasoning item round-tripped verbatim (encrypted_content preserved).
	rsn, _ := input[1].(map[string]any)
	if rsn["encrypted_content"] != "ENC" {
		t.Errorf("reasoning encrypted_content = %v, want ENC", rsn["encrypted_content"])
	}
	fc, _ := input[2].(map[string]any)
	if fc["call_id"] != "call_abc" || fc["name"] != "get_weather" {
		t.Errorf("function_call = %v, want call_abc/get_weather", fc)
	}
	fco, _ := input[3].(map[string]any)
	if fco["call_id"] != "call_abc" || fco["output"] != `{"temp":"68F"}` {
		t.Errorf("function_call_output = %v", fco)
	}
}

// TestChatModel_ReasoningRoundTrip exercises the full multi-turn reasoning chain
// end-to-end: a streamed reasoning output item is captured into Extra, survives
// ConcatMessages, and is re-emitted verbatim (ahead of the function_call) when
// the merged assistant message is sent back on the next turn.
func TestChatModel_ReasoningRoundTrip(t *testing.T) {
	t.Parallel()

	const turn1 = 1
	turn := 0
	var captured map[string]any
	stub := stubRoundTripper(func(r *http.Request) (*http.Response, error) {
		turn++
		if turn == turn1 {
			return sseOKResponse(
				`{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"ENC"}}`,
				`{"type":"response.output_item.done","item":{"type":"function_call","name":"get_weather","call_id":"call_abc","arguments":"{\"city\":\"SF\"}"}}`,
				`{"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}}`,
			), nil
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode turn 2 request: %v", err)
		}
		return sseOKResponse(
			`{"type":"response.output_text.delta","delta":"It is foggy."}`,
			`{"type":"response.completed","response":{"id":"r2"}}`,
		), nil
	})
	cm := newTestChatModel(t, stub)

	history := []*schema.Message{schema.UserMessage("weather in SF?")}

	// Turn 1: capture the assistant message (must carry reasoning in Extra).
	sr, err := cm.Stream(context.Background(), history)
	if err != nil {
		t.Fatalf("turn 1 Stream: %v", err)
	}
	merged, err := schema.ConcatMessages(drain(t, sr))
	if err != nil {
		t.Fatalf("ConcatMessages: %v", err)
	}
	if len(merged.ToolCalls) != 1 || merged.ToolCalls[0].ID != "call_abc" {
		t.Fatalf("turn 1 tool calls = %+v, want one call_abc", merged.ToolCalls)
	}
	raws, ok := merged.Extra[extraKeyReasoningItems].([]json.RawMessage)
	if !ok || len(raws) != 1 {
		t.Fatalf("merged Extra reasoning items = %#v, want one json.RawMessage", merged.Extra[extraKeyReasoningItems])
	}
	if !strings.Contains(string(raws[0]), `"encrypted_content":"ENC"`) {
		t.Fatalf("captured reasoning item lost encrypted_content: %s", raws[0])
	}

	// Turn 2: send back [user, assistant(merged), tool]. The reasoning item must
	// be re-emitted in input, ahead of the function_call.
	history = append(history, merged, schema.ToolMessage(`{"temp":"60F"}`, "call_abc"))
	if _, err := cm.Generate(context.Background(), history); err != nil {
		t.Fatalf("turn 2 Generate: %v", err)
	}

	input, ok := captured["input"].([]any)
	if !ok {
		t.Fatalf("turn 2 input is %T, want array", captured["input"])
	}
	types := make([]string, 0, len(input))
	for _, it := range input {
		m, _ := it.(map[string]any)
		types = append(types, fmt.Sprintf("%v", m["type"]))
	}
	want := []string{"message", "reasoning", "function_call", "function_call_output"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("turn 2 input types = %v, want %v", types, want)
	}
	rsn, _ := input[1].(map[string]any)
	if rsn["encrypted_content"] != "ENC" || rsn["id"] != "rs_1" {
		t.Errorf("re-emitted reasoning item = %v, want id rs_1 / encrypted ENC", rsn)
	}
}

func TestChatModel_CtxCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	stub := stubRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &ctxBlockingReader{ctx: r.Context(), initial: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"x\"}\n\n"},
			Header:     make(http.Header),
		}, nil
	})
	cm := newTestChatModel(t, stub)

	sr, err := cm.Stream(ctx, []*schema.Message{schema.UserMessage("hi")})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer sr.Close()

	// Read the first chunk, then cancel mid-stream.
	if _, err := sr.Recv(); err != nil {
		t.Fatalf("first Recv: %v", err)
	}
	cancel()
	for {
		_, err := sr.Recv()
		if err == nil {
			continue
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		t.Fatalf("Recv after cancel = %v, want context.Canceled", err)
	}
}

func TestChatModel_Error_PlanNotIncluded_HTTP(t *testing.T) {
	t.Parallel()
	stub := stubRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body:       apiErrorResponse(codexErrCodeUsageNotIncluded, "plan does not include Codex"),
			Header:     make(http.Header),
		}, nil
	})
	cm := newTestChatModel(t, stub)
	_, err := cm.Stream(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, einoproviders.ErrProviderAuth) {
		t.Error("want ErrProviderAuth")
	}
	if !errors.Is(err, codexauth.ErrPlanNotIncluded) {
		t.Error("want codexauth.ErrPlanNotIncluded")
	}
}

func TestChatModel_Error_QuotaExceeded_StreamFailed(t *testing.T) {
	t.Parallel()
	stub := stubRoundTripper(func(*http.Request) (*http.Response, error) {
		return sseOKResponse(
			`{"type":"response.failed","response":{"error":{"code":"insufficient_quota","message":"quota exceeded"}}}`,
		), nil
	})
	cm := newTestChatModel(t, stub)
	sr, err := cm.Stream(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer sr.Close()
	var lastErr error
	for {
		_, recvErr := sr.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			lastErr = recvErr
			break
		}
	}
	if !errors.Is(lastErr, einoproviders.ErrProviderAuth) || !errors.Is(lastErr, codexauth.ErrQuotaExceeded) {
		t.Errorf("err = %v, want ErrProviderAuth + ErrQuotaExceeded", lastErr)
	}
}

func TestChatModel_WithTools_Immutable(t *testing.T) {
	t.Parallel()
	stub := stubRoundTripper(func(*http.Request) (*http.Response, error) {
		return sseOKResponse(`{"type":"response.completed","response":{"id":"r"}}`), nil
	})
	base := newTestChatModel(t, stub).(*chatModel)
	if base.tools != nil {
		t.Fatal("base model must start tool-free")
	}

	toolA := &schema.ToolInfo{Name: "a", Desc: "tool a"}
	derived, err := base.WithTools([]*schema.ToolInfo{toolA})
	if err != nil {
		t.Fatalf("WithTools: %v", err)
	}
	if base.tools != nil {
		t.Error("WithTools must not mutate the receiver")
	}
	if dc := derived.(*chatModel); len(dc.tools) != 1 || dc.tools[0].Name != "a" {
		t.Errorf("derived tools = %+v, want [a]", dc.tools)
	}

	// empty/nil yields a tool-free clone, not an error.
	cleared, err := derived.WithTools(nil)
	if err != nil {
		t.Fatalf("WithTools(nil): %v", err)
	}
	if cc := cleared.(*chatModel); cc.tools != nil {
		t.Errorf("WithTools(nil) tools = %+v, want nil", cc.tools)
	}
}

func TestChatModel_WithTools_SerializesToolJSON(t *testing.T) {
	t.Parallel()
	var captured map[string]any
	stub := stubRoundTripper(func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		return sseOKResponse(`{"type":"response.completed","response":{"id":"r"}}`), nil
	})
	base := newTestChatModel(t, stub)
	tool := &schema.ToolInfo{
		Name: "get_weather",
		Desc: "Get weather",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"city": {Type: schema.String, Desc: "city name", Required: true},
		}),
	}
	withTool, err := base.WithTools([]*schema.ToolInfo{tool})
	if err != nil {
		t.Fatalf("WithTools: %v", err)
	}
	if _, err := withTool.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	tools, ok := captured["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want 1", captured["tools"])
	}
	tm, _ := tools[0].(map[string]any)
	if tm["type"] != "function" || tm["name"] != "get_weather" {
		t.Errorf("tool = %v, want flat function/get_weather", tm)
	}
	if _, ok := tm["parameters"].(map[string]any); !ok {
		t.Errorf("tool parameters missing/!object: %v", tm["parameters"])
	}
}

func TestNewChatModel_NotLoggedIn(t *testing.T) {
	orig := newCodexChatClient
	newCodexChatClient = func(context.Context, string) (*http.Client, error) {
		return nil, codexauth.ErrNotLoggedIn
	}
	t.Cleanup(func() { newCodexChatClient = orig })

	_, err := NewChatModel(context.Background(), ChatModelConfig{AppName: "x", Model: "gpt-5.5"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, einoproviders.ErrProviderInit) {
		t.Error("want ErrProviderInit")
	}
	if !errors.Is(err, einoproviders.ErrProviderAuth) {
		t.Error("want ErrProviderAuth")
	}
	if !errors.Is(err, codexauth.ErrNotLoggedIn) {
		t.Error("want codexauth.ErrNotLoggedIn")
	}
}

func TestNewChatModelWithHTTPClient_Validation(t *testing.T) {
	t.Parallel()
	if _, err := NewChatModelWithHTTPClient(context.Background(), nil, ChatModelConfig{Model: "m"}); err == nil {
		t.Error("nil client must error")
	}
	if _, err := NewChatModelWithHTTPClient(context.Background(), &http.Client{}, ChatModelConfig{}); err == nil {
		t.Error("empty model must error")
	}
}

func TestChatModel_DisableReasoning(t *testing.T) {
	t.Parallel()
	var captured map[string]any
	stub := stubRoundTripper(func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		return sseOKResponse(`{"type":"response.completed","response":{"id":"r"}}`), nil
	})
	cm, err := NewChatModelWithHTTPClient(context.Background(), &http.Client{Transport: stub}, ChatModelConfig{
		Model: "gpt-5.5", DisableReasoning: true,
	})
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if _, err := cm.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if captured["reasoning"] != nil {
		t.Errorf("reasoning = %v, want null when disabled", captured["reasoning"])
	}
	if inc, ok := captured["include"].([]any); !ok || len(inc) != 0 {
		t.Errorf("include = %v, want []", captured["include"])
	}
}

// ctxBlockingReader serves an initial payload, then blocks until ctx is done,
// emulating an SSE connection aborted by request-context cancellation.
type ctxBlockingReader struct {
	ctx     context.Context
	initial string
	served  bool
}

func (r *ctxBlockingReader) Read(p []byte) (int, error) {
	if !r.served {
		r.served = true
		n := copy(p, r.initial)
		return n, nil
	}
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func (r *ctxBlockingReader) Close() error { return nil }
