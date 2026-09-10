package opencodego

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	opencodeauth "github.com/mattsp1290/opencode-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestResponsesRequestPreservesOrderReasoningAndTools(t *testing.T) {
	reasoning := json.RawMessage(`{"type":"reasoning","encrypted_content":"opaque","summary":[]}`)
	assistant := schema.AssistantMessage("checking", []schema.ToolCall{
		{ID: "call_weather", Type: "function", Function: schema.FunctionCall{Name: "weather", Arguments: `{"city":"NYC"}`}},
		{ID: "call_clock", Type: "function", Function: schema.FunctionCall{Name: "clock", Arguments: `{}`}},
	})
	assistant.Extra = map[string]any{responsesReasoningItemsKey: []json.RawMessage{reasoning}}
	tool := &schema.ToolInfo{
		Name: "weather", Desc: "Get weather",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"city": {Type: schema.String, Desc: "City", Required: true},
		}),
	}
	request, err := buildResponsesRequest("base", nil, nil, []*schema.Message{
		schema.SystemMessage("first"), schema.SystemMessage("second"), schema.UserMessage("weather?"), assistant,
		schema.ToolMessage(`{"temperature":72}`, "call_weather"),
		{Role: schema.Tool, ToolCallID: "call_clock", ToolName: "clock", Content: "12:00"},
	}, false, model.WithModel("override"), model.WithMaxTokens(23), model.WithTools([]*schema.ToolInfo{tool}), model.WithToolChoice(schema.ToolChoiceForced))
	if err != nil {
		t.Fatal(err)
	}
	reasoning[0] = 'X'
	if request.Model != "override" || request.MaxOutputTokens == nil || *request.MaxOutputTokens != 23 || request.Stream || request.Store {
		t.Fatalf("request options = %#v", request)
	}
	if request.Instructions != "first\n\nsecond" || fmt.Sprint(request.Include) != "[reasoning.encrypted_content]" || request.ToolChoice != "required" {
		t.Fatalf("request controls = %#v", request)
	}
	if len(request.Tools) != 1 || request.Tools[0].Name != "weather" || !bytes.Contains(request.Tools[0].Parameters, []byte(`"city"`)) {
		t.Fatalf("tools = %#v", request.Tools)
	}
	wantTypes := []string{"message", "reasoning", "message", "function_call", "function_call", "function_call_output", "function_call_output"}
	gotTypes := make([]string, len(request.Input))
	for i, raw := range request.Input {
		var item struct {
			Type string `json:"type"`
		}
		if decodeErr := json.Unmarshal(raw, &item); decodeErr != nil {
			t.Fatalf("input %d: %v", i, decodeErr)
		}
		gotTypes[i] = item.Type
	}
	if fmt.Sprint(gotTypes) != fmt.Sprint(wantTypes) {
		t.Fatalf("input types = %v, want %v", gotTypes, wantTypes)
	}
	if !bytes.Contains(request.Input[1], []byte(`"opaque"`)) {
		t.Fatalf("reasoning was not deep copied: %s", request.Input[1])
	}

	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, omitted := range []string{`"reasoning":`, `"previous_response_id"`, `"temperature"`, `"top_p"`} {
		if bytes.Contains(payload, []byte(omitted)) {
			t.Fatalf("request unexpectedly contains %s: %s", omitted, payload)
		}
	}
}

func TestResponsesRequestAcceptsJSONRoundTripReasoning(t *testing.T) {
	message := schema.AssistantMessage("answer", nil)
	message.Extra = map[string]any{responsesReasoningItemsKey: []json.RawMessage{json.RawMessage(`{"type":"reasoning","encrypted_content":"opaque"}`)}}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded schema.Message
	if decodeErr := json.Unmarshal(encoded, &decoded); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	request, err := buildResponsesRequest("model", nil, nil, []*schema.Message{&decoded}, false)
	if err != nil {
		t.Fatalf("round-tripped reasoning rejected: %v (%#v)", err, decoded.Extra)
	}
	if len(request.Input) != 2 || !bytes.Contains(request.Input[0], []byte(`"encrypted_content"`)) {
		t.Fatalf("input = %s", request.Input)
	}
}

func TestResponsesInvalidRequestsFailBeforeNetwork(t *testing.T) {
	validCall := schema.AssistantMessage("", []schema.ToolCall{{ID: "call_1", Type: "function", Function: schema.FunctionCall{Name: "tool", Arguments: `{}`}}})
	multimodal := schema.UserMessage("text")
	multimodal.UserInputMultiContent = []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: "extra"}}
	badReasoning := schema.AssistantMessage("", nil)
	badReasoning.Extra = map[string]any{responsesReasoningItemsKey: []any{map[string]any{"type": "reasoning"}}}
	invalidParams := schema.NewParamsOneOfByJSONSchema(nil)
	tests := []struct {
		name     string
		messages []*schema.Message
		options  []model.Option
	}{
		{name: "nil message", messages: []*schema.Message{nil}},
		{name: "unknown role", messages: []*schema.Message{{Role: "developer", Content: "x"}}},
		{name: "multimodal", messages: []*schema.Message{multimodal}},
		{name: "message name", messages: []*schema.Message{{Role: schema.User, Name: "named", Content: "x"}}},
		{name: "reasoning content", messages: []*schema.Message{{Role: schema.Assistant, ReasoningContent: "unsupported"}}},
		{name: "missing call id", messages: []*schema.Message{schema.AssistantMessage("", []schema.ToolCall{{Function: schema.FunctionCall{Name: "tool", Arguments: `{}`}}})}},
		{name: "missing call name", messages: []*schema.Message{schema.AssistantMessage("", []schema.ToolCall{{ID: "call", Function: schema.FunctionCall{Arguments: `{}`}}})}},
		{name: "malformed arguments", messages: []*schema.Message{schema.AssistantMessage("", []schema.ToolCall{{ID: "call", Function: schema.FunctionCall{Name: "tool", Arguments: `{`}}})}},
		{name: "duplicate call id", messages: []*schema.Message{schema.AssistantMessage("", []schema.ToolCall{{ID: "call", Function: schema.FunctionCall{Name: "a", Arguments: `{}`}}, {ID: "call", Function: schema.FunctionCall{Name: "b", Arguments: `{}`}}})}},
		{name: "unknown tool result", messages: []*schema.Message{{Role: schema.Tool, ToolCallID: "missing", Content: "x"}}},
		{name: "mismatched tool name", messages: []*schema.Message{validCall, {Role: schema.Tool, ToolCallID: "call_1", ToolName: "other", Content: "x"}}},
		{name: "duplicate tool result", messages: []*schema.Message{validCall, schema.ToolMessage("x", "call_1"), schema.ToolMessage("x", "call_1")}},
		{name: "malformed reasoning", messages: []*schema.Message{badReasoning}},
		{name: "blank model", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithModel(" ")}},
		{name: "invalid max", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithMaxTokens(0)}},
		{name: "temperature", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithTemperature(0.2)}},
		{name: "top p", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithTopP(0.5)}},
		{name: "stop", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithStop([]string{"stop"})}},
		{name: "allowed names", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithToolChoice(schema.ToolChoiceAllowed, "tool")}},
		{name: "forced without tools", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithToolChoice(schema.ToolChoiceForced)}},
		{name: "nil tool", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithTools([]*schema.ToolInfo{nil})}},
		{name: "blank tool", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithTools([]*schema.ToolInfo{{Name: " "}})}},
		{name: "duplicate tool", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithTools([]*schema.ToolInfo{{Name: "same"}, {Name: "same"}})}},
		{name: "invalid tool schema", messages: []*schema.Message{schema.UserMessage("x")}, options: []model.Option{model.WithTools([]*schema.ToolInfo{{Name: "tool", ParamsOneOf: invalidParams}})}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			adapter := newDirectResponsesModel(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("network must not be reached")
			})})
			message, err := adapter.Generate(context.Background(), tt.messages, tt.options...)
			if !errors.Is(err, errInvalidResponsesRequest) || message != nil {
				t.Fatalf("Generate = %#v, %v, want invalid request", message, err)
			}
			if calls != 0 {
				t.Fatalf("network calls = %d, want 0", calls)
			}
		})
	}
}

func TestResponsesGenerateUsesNativeAuthAndDecodesOutput(t *testing.T) {
	var request responsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom/root/responses" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "" {
			t.Errorf("X-Api-Key = %q, want empty", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer real-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-OpenCode-Session"); got != "operation-session" {
			t.Errorf("session = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"status":"completed",
			"output":[
				{"type":"reasoning","status":"completed","encrypted_content":"opaque","summary":[]},
				{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello "},{"type":"output_text","text":"world"}]},
				{"type":"function_call","status":"completed","name":"weather","arguments":"{\"city\":\"NYC\"}","call_id":"call_1"},
				{"type":"function_call","status":"completed","name":"clock","arguments":"{}","call_id":"call_2"}
			],
			"usage":{"input_tokens":9,"output_tokens":5,"total_tokens":14,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}},
			"error": null
		}`)
	}))
	t.Cleanup(server.Close)
	prepared, err := prepareConfig(ChatModelConfig{
		Model: "fixture-model", Protocol: ProtocolResponses, APIKey: "real-key", UserAgent: "responses-test/1",
		SessionID: "responses-session", BaseURL: server.URL + "/custom/root", HTTPClient: server.Client(),
	}, chatModelConstruction)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := newResponsesAdapter(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	ctx := opencodeauth.WithSessionID(context.Background(), "operation-session")
	message, err := adapter.Generate(ctx, []*schema.Message{schema.UserMessage("hello")})
	if err != nil {
		t.Fatal(err)
	}
	if request.Model != "fixture-model" || request.Stream || request.Store || request.MaxOutputTokens != nil {
		t.Fatalf("request = %#v", request)
	}
	if message.Content != "hello world" || len(message.ToolCalls) != 2 || message.ResponseMeta.FinishReason != "tool_calls" {
		t.Fatalf("message = %#v", message)
	}
	if message.ToolCalls[0].Index == nil || *message.ToolCalls[0].Index != 0 || message.ToolCalls[1].Index == nil || *message.ToolCalls[1].Index != 1 {
		t.Fatalf("tool call indexes = %#v", message.ToolCalls)
	}
	usage := message.ResponseMeta.Usage
	if usage == nil || usage.PromptTokens != 9 || usage.CompletionTokens != 5 || usage.TotalTokens != 14 || usage.PromptTokenDetails.CachedTokens != 3 || usage.CompletionTokensDetails.ReasoningTokens != 2 {
		t.Fatalf("usage = %#v", usage)
	}
	items, ok := message.Extra[responsesReasoningItemsKey].([]json.RawMessage)
	if !ok || len(items) != 1 || !bytes.Contains(items[0], []byte(`"opaque"`)) {
		t.Fatalf("reasoning = %#v", message.Extra)
	}
}

func TestResponsesGeneratePreservesUsagePresence(t *testing.T) {
	for _, tt := range []struct {
		name      string
		usageJSON string
		wantNil   bool
	}{
		{name: "absent", wantNil: true},
		{name: "null", usageJSON: `,"usage":null`, wantNil: true},
		{name: "explicit zero", usageJSON: `,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			adapter := responseBodyModel(t, `{"status":"completed","output":[]`+tt.usageJSON+`}`)
			message, err := adapter.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")})
			if err != nil {
				t.Fatal(err)
			}
			if (message.ResponseMeta.Usage == nil) != tt.wantNil {
				t.Fatalf("usage = %#v, want nil %v", message.ResponseMeta.Usage, tt.wantNil)
			}
		})
	}
}

func TestResponsesGenerateAllowsToolOnlyCompletion(t *testing.T) {
	adapter := responseBodyModel(t, `{"status":"completed","output":[{"type":"function_call","status":"completed","name":"tool","arguments":"{}","call_id":"call_1"}]}`)
	message, err := adapter.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "" || len(message.ToolCalls) != 1 || message.ResponseMeta.FinishReason != "tool_calls" {
		t.Fatalf("message = %#v", message)
	}
}

func TestResponsesInvalidResponsesReturnNoPartialSuccess(t *testing.T) {
	tests := []struct{ name, body string }{
		{name: "malformed json", body: `{`},
		{name: "failed", body: `{"status":"failed","output":[]}`},
		{name: "incomplete", body: `{"status":"incomplete","output":[]}`},
		{name: "missing output", body: `{"status":"completed"}`},
		{name: "response error", body: `{"status":"completed","output":[],"error":{"message":"secret"}}`},
		{name: "unknown output", body: `{"status":"completed","output":[{"type":"computer_call","status":"completed"}]}`},
		{name: "bad message role", body: `{"status":"completed","output":[{"type":"message","role":"user","content":[]}]}`},
		{name: "missing message content", body: `{"status":"completed","output":[{"type":"message","role":"assistant"}]}`},
		{name: "bad message content", body: `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"refusal","text":"no"}]}]}`},
		{name: "missing output text", body: `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text"}]}]}`},
		{name: "null output text", body: `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":null}]}]}`},
		{name: "bad call", body: `{"status":"completed","output":[{"type":"function_call","name":"tool","arguments":"{","call_id":"call"}]}`},
		{name: "duplicate call", body: `{"status":"completed","output":[{"type":"function_call","name":"tool","arguments":"{}","call_id":"call"},{"type":"function_call","name":"tool","arguments":"{}","call_id":"call"}]}`},
		{name: "bad reasoning", body: `{"status":"completed","output":[{"type":"reasoning","encrypted_content":""}]}`},
		{name: "negative usage", body: `{"status":"completed","output":[],"usage":{"input_tokens":-1}}`},
		{name: "missing usage counter", body: `{"status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0}}`},
		{name: "null usage counter", body: `{"status":"completed","output":[],"usage":{"input_tokens":null,"output_tokens":0,"total_tokens":0}}`},
		{name: "missing cached counter", body: `{"status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0,"input_tokens_details":{}}}`},
		{name: "null reasoning counter", body: `{"status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0,"output_tokens_details":{"reasoning_tokens":null}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := responseBodyModel(t, tt.body)
			message, err := adapter.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")})
			if message != nil || !errors.Is(err, einoproviders.ErrProviderAPI) {
				t.Fatalf("Generate = %#v, %v, want nil ErrProviderAPI", message, err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaked response: %v", err)
			}
		})
	}
}

func TestResponsesGenerateLetsFacadeClassifyHTTPFailures(t *testing.T) {
	for _, tt := range []struct {
		name     string
		status   int
		wantAuth bool
		wantAPI  bool
	}{
		{name: "authentication", status: http.StatusUnauthorized, wantAuth: true},
		{name: "server", status: http.StatusInternalServerError, wantAPI: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				errorType := "server_error"
				if tt.wantAuth {
					errorType = "authentication_error"
				}
				_, _ = io.WriteString(w, `{"error":{"type":"`+errorType+`","message":"secret-upstream-detail"}}`)
			}))
			t.Cleanup(server.Close)
			prepared, err := prepareConfig(ChatModelConfig{
				Model: "fixture", Protocol: ProtocolResponses, APIKey: "key", UserAgent: "responses-test/1",
				SessionID: "session", BaseURL: server.URL + "/v1", HTTPClient: server.Client(),
			}, chatModelConstruction)
			if err != nil {
				t.Fatal(err)
			}
			delegate, err := newResponsesAdapter(context.Background(), prepared)
			if err != nil {
				t.Fatal(err)
			}
			facade := &chatModel{delegate: delegate, config: prepared}
			message, err := facade.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")})
			if message != nil || err == nil {
				t.Fatalf("Generate = %#v, %v", message, err)
			}
			if errors.Is(err, einoproviders.ErrProviderAuth) != tt.wantAuth || errors.Is(err, einoproviders.ErrProviderAPI) != tt.wantAPI {
				t.Fatalf("classification auth=%v api=%v: %v", errors.Is(err, einoproviders.ErrProviderAuth), errors.Is(err, einoproviders.ErrProviderAPI), err)
			}
			var httpErr *opencodeauth.HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != tt.status {
				t.Fatalf("typed HTTP error = %#v", httpErr)
			}
			if strings.Contains(err.Error(), "secret-upstream-detail") {
				t.Fatalf("error leaked upstream detail: %v", err)
			}
		})
	}
}

func TestResponsesGenerateBoundsAndClosesBodies(t *testing.T) {
	valid := []byte(`{"status":"completed","output":[]}`)
	for _, tt := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "exact limit", size: maxResponsesBodyBytes},
		{name: "over limit", size: maxResponsesBodyBytes + 1, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes := append(append([]byte(nil), valid...), bytes.Repeat([]byte(" "), tt.size-len(valid))...)
			body := &trackedBody{Reader: bytes.NewReader(bodyBytes)}
			adapter := newDirectResponsesModel(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
			})})
			message, err := adapter.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")})
			if tt.wantErr {
				if message != nil || !errors.Is(err, errResponsesBodyTooLarge) || !errors.Is(err, einoproviders.ErrProviderAPI) {
					t.Fatalf("Generate = %#v, %v", message, err)
				}
			} else if err != nil || message == nil {
				t.Fatalf("Generate = %#v, %v", message, err)
			}
			if got := body.closeCount(); got != 1 {
				t.Fatalf("close count = %d, want 1", got)
			}
		})
	}
}

func responseBodyModel(t *testing.T, responseBody string) *responsesModel {
	t.Helper()
	body := &trackedBody{Reader: strings.NewReader(responseBody)}
	model := newDirectResponsesModel(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
	})})
	t.Cleanup(func() {
		if got := body.closeCount(); got != 1 {
			t.Errorf("response body close count = %d, want 1", got)
		}
	})
	return model
}

func newDirectResponsesModel(t *testing.T, client *http.Client) *responsesModel {
	t.Helper()
	prepared, err := prepareConfig(ChatModelConfig{
		Model: "fixture-model", Protocol: ProtocolResponses, APIKey: "key", UserAgent: "responses-test/1",
		SessionID: "session", BaseURL: "https://api.example.test/v1", HTTPClient: client,
	}, chatModelConstruction)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := newResponsesAdapter(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	responses, ok := adapter.(*responsesModel)
	if !ok {
		t.Fatalf("adapter type = %T", adapter)
	}
	return responses
}
