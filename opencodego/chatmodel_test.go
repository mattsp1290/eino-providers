package opencodego

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	opencodeauth "github.com/mattsp1290/opencode-auth-go"
	orderedmap "github.com/wk8/go-ordered-map/v2"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestChatModelGenerateOptionsToolsAndSession(t *testing.T) {
	type capturedRequest struct {
		model      string
		maxTokens  int
		tools      []map[string]any
		toolChoice any
		messages   []map[string]any
		session    string
	}
	var mu sync.Mutex
	var captured []capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model      string           `json:"model"`
			MaxTokens  int              `json:"max_tokens"`
			Tools      []map[string]any `json:"tools"`
			ToolChoice any              `json:"tool_choice"`
			Messages   []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		captured = append(captured, capturedRequest{body.Model, body.MaxTokens, body.Tools, body.ToolChoice, body.Messages, r.Header.Get("X-OpenCode-Session")})
		call := len(captured)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-tools","object":"chat.completion","created":0,"model":"override-model",
				"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{\"city\":\"NYC\"}"}}]},"finish_reason":"tool_calls"}],
				"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-result","object":"chat.completion","created":0,"model":"fixture-model",
			"choices":[{"index":0,"message":{"role":"assistant","content":"sunny"},"finish_reason":"stop"}]
		}`))
	}))
	t.Cleanup(server.Close)

	cm := newTestChatModel(t, server, "fallback-session", nil)
	tool := &schema.ToolInfo{
		Name: "weather",
		Desc: "Get weather",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"city": {Type: schema.String, Desc: "City", Required: true},
		}),
	}
	bound, err := cm.WithTools([]*schema.ToolInfo{tool})
	if err != nil {
		t.Fatalf("WithTools: %v", err)
	}
	tool.Name = "mutated-after-bind"
	ctx := opencodeauth.WithSessionID(context.WithValue(context.Background(), chatContextKey{}, "preserved"), "operation-session")
	first, err := bound.Generate(ctx, []*schema.Message{schema.UserMessage("weather?")},
		model.WithModel("override-model"), model.WithMaxTokens(23), model.WithToolChoice(schema.ToolChoiceForced))
	if err != nil {
		t.Fatalf("Generate tool call: %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != "weather" || first.ToolCalls[0].ID != "call_weather" {
		t.Fatalf("tool calls = %#v", first.ToolCalls)
	}
	if first.ResponseMeta == nil || first.ResponseMeta.Usage == nil || first.ResponseMeta.Usage.TotalTokens != 0 {
		t.Fatalf("explicit-zero usage = %#v", first.ResponseMeta)
	}

	history := []*schema.Message{schema.UserMessage("weather?"), first, schema.ToolMessage(`{"temperature":"72F"}`, "call_weather")}
	second, err := cm.Generate(context.Background(), history)
	if err != nil {
		t.Fatalf("Generate tool result: %v", err)
	}
	if second.Content != "sunny" || second.ResponseMeta == nil || second.ResponseMeta.Usage != nil {
		t.Fatalf("second response = %#v, want content with unavailable usage", second)
	}

	mu.Lock()
	requests := append([]capturedRequest(nil), captured...)
	mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if requests[0].model != "override-model" || requests[0].maxTokens != 23 || requests[0].session != "operation-session" {
		t.Fatalf("first request = %#v", requests[0])
	}
	if len(requests[0].tools) != 1 || nestedToolName(requests[0].tools[0]) != "weather" {
		t.Fatalf("bound tools = %#v", requests[0].tools)
	}
	choice, _ := requests[0].toolChoice.(map[string]any)
	function, _ := choice["function"].(map[string]any)
	if choice["type"] != "function" || function["name"] != "weather" {
		t.Fatalf("tool_choice = %#v, want forced weather function", requests[0].toolChoice)
	}
	if len(requests[1].tools) != 0 || requests[1].session != "fallback-session" {
		t.Fatalf("base request tools/session = %#v/%q", requests[1].tools, requests[1].session)
	}
	if got := requests[1].messages[len(requests[1].messages)-1]["tool_call_id"]; got != "call_weather" {
		t.Fatalf("tool result call ID = %#v", got)
	}
}

func TestChatModelWithToolsCanClearDerivedModel(t *testing.T) {
	var toolCounts []int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []json.RawMessage `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		toolCounts = append(toolCounts, len(body.Tools))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"id","object":"chat.completion","created":0,"model":"fixture","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)
	base := newTestChatModel(t, server, "session", nil)
	bound, err := base.WithTools([]*schema.ToolInfo{{Name: "one", Desc: "one"}})
	if err != nil {
		t.Fatal(err)
	}
	cleared, err := bound.WithTools(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cm := range []model.ToolCallingChatModel{bound, base, cleared} {
		if _, err := cm.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")}); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(toolCounts) != "[1 0 0]" {
		t.Fatalf("tool counts = %v, want [1 0 0]", toolCounts)
	}
}

func TestChatModelPerCallToolsOwnCallerSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"id\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"id","object":"chat.completion","created":0,"model":"fixture","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	cm := newTestChatModel(t, server, "session", nil)

	properties := orderedmap.New[string, *jsonschema.Schema]()
	properties.Set("z", &jsonschema.Schema{Type: "string"})
	properties.Set("a", &jsonschema.Schema{Type: "string"})
	parameters := &jsonschema.Schema{Type: "object", Properties: properties, Required: []string{"z", "a"}}
	tools := []*schema.ToolInfo{{Name: "owned", ParamsOneOf: schema.NewParamsOneOfByJSONSchema(parameters)}}
	option := model.WithTools(tools)

	const operations = 8
	errs := make(chan error, operations)
	for i := 0; i < operations; i++ {
		go func(streaming bool) {
			if !streaming {
				_, err := cm.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")}, option)
				errs <- err
				return
			}
			stream, err := cm.Stream(context.Background(), []*schema.Message{schema.UserMessage("hi")}, option)
			if err == nil {
				for {
					_, receiveErr := stream.Recv()
					if errors.Is(receiveErr, io.EOF) {
						break
					}
					if receiveErr != nil {
						err = receiveErr
						break
					}
				}
				stream.Close()
			}
			errs <- err
		}(i%2 == 0)
	}
	for i := 0; i < operations; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("operation error: %v", err)
		}
	}
	if got := fmt.Sprint(parameters.Required); got != "[z a]" {
		t.Fatalf("caller schema Required mutated to %s", got)
	}
}

func TestChatCompletionsStreamNormalizesUsageAfterDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"id\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"id\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"fixture\",\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	cm := newTestChatModel(t, server, "session", nil)
	stream, err := cm.Stream(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var text string
	var usages []*schema.TokenUsage
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		text += chunk.Content
		if chunk.ResponseMeta != nil && chunk.ResponseMeta.Usage != nil {
			usages = append(usages, chunk.ResponseMeta.Usage)
		}
	}
	if text != "hello" || len(usages) != 1 || usages[0].PromptTokens != 4 || usages[0].CompletionTokens != 2 {
		t.Fatalf("stream text/usages = %q/%#v", text, usages)
	}
}

func TestChatModelCancellationAndPrematureEOF(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		started := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
		}))
		defer server.Close()
		cm := newTestChatModel(t, server, "session", nil)
		ctx, cancel := context.WithCancel(context.Background())
		stream, err := cm.Stream(ctx, []*schema.Message{schema.UserMessage("hi")})
		if err != nil {
			t.Fatal(err)
		}
		<-started
		cancel()
		defer stream.Close()
		_, err = stream.Recv()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Recv error = %v, want context.Canceled", err)
		}
	})

	t.Run("premature eof", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"id\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
		}))
		defer server.Close()
		cm := newTestChatModel(t, server, "session", nil)
		stream, err := cm.Stream(context.Background(), []*schema.Message{schema.UserMessage("hi")})
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		if _, firstErr := stream.Recv(); firstErr != nil {
			t.Fatalf("first Recv: %v", firstErr)
		}
		_, err = stream.Recv()
		if !errors.Is(err, einoproviders.ErrProviderAPI) {
			t.Fatalf("terminal error = %v, want ErrProviderAPI", err)
		}
	})
}

func TestChatModelValidatesOptionsAndMapsHTTPError(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"AuthenticationError","message":"secret upstream detail"}}`))
	}))
	t.Cleanup(server.Close)
	cm := newTestChatModel(t, server, "session", nil)
	for _, option := range []model.Option{model.WithModel("  "), model.WithMaxTokens(0)} {
		_, err := cm.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")}, option)
		if !errors.Is(err, einoproviders.ErrProviderAPI) {
			t.Fatalf("local option error = %v, want ErrProviderAPI", err)
		}
	}
	if calls != 0 {
		t.Fatalf("local validation made %d requests", calls)
	}
	_, err := cm.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if !errors.Is(err, einoproviders.ErrProviderAuth) {
		t.Fatalf("HTTP error = %v, want ErrProviderAuth", err)
	}
	if strings.Contains(err.Error(), "secret upstream detail") {
		t.Fatalf("public error leaked response: %v", err)
	}
}

func TestNewChatModelRejectsUnavailableProtocols(t *testing.T) {
	cap := 10
	for _, cfg := range []ChatModelConfig{
		{Model: "fixture", Protocol: ProtocolMessages, APIKey: "key", UserAgent: "test/1", MaxTokens: &cap},
		{Model: "fixture", Protocol: ProtocolResponses, APIKey: "key", UserAgent: "test/1"},
	} {
		if _, err := NewChatModel(context.Background(), cfg); !errors.Is(err, einoproviders.ErrProviderInit) {
			t.Fatalf("protocol %q error = %v, want ErrProviderInit", cfg.Protocol, err)
		}
	}
}

type chatContextKey struct{}

func newTestChatModel(t *testing.T, server *httptest.Server, session string, maxTokens *int) model.ToolCallingChatModel {
	t.Helper()
	cm, err := NewChatModel(context.Background(), ChatModelConfig{
		Model: "fixture-model", Protocol: ProtocolChatCompletions, APIKey: "real-key", UserAgent: "chatmodel-test/1",
		SessionID: session, BaseURL: server.URL + "/v1", HTTPClient: server.Client(), MaxTokens: maxTokens,
	})
	if err != nil {
		t.Fatalf("NewChatModel: %v", err)
	}
	return cm
}

func nestedToolName(tool map[string]any) string {
	function, _ := tool["function"].(map[string]any)
	name, _ := function["name"].(string)
	return name
}
