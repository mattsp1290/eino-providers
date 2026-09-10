package opencodego_test

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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	opencodeauth "github.com/mattsp1290/opencode-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
	"github.com/mattsp1290/eino-providers/opencodego"
)

type integrationProtocol struct {
	name     string
	protocol opencodego.Protocol
	path     string
	capField string
}

var integrationProtocols = []integrationProtocol{
	{name: "chat completions", protocol: opencodego.ProtocolChatCompletions, path: "/v1/chat/completions", capField: "max_tokens"},
	{name: "messages", protocol: opencodego.ProtocolMessages, path: "/v1/messages", capField: "max_tokens"},
	{name: "responses", protocol: opencodego.ProtocolResponses, path: "/v1/responses", capField: "max_output_tokens"},
}

func TestIntegrationPublicGenerateAndAdvise(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "environment-key")
	for _, protocol := range integrationProtocols {
		t.Run(protocol.name, func(t *testing.T) {
			for _, usage := range []string{"omitted", "null", "zero", "nonzero"} {
				t.Run("generate "+usage, func(t *testing.T) {
					var captured integrationRequest
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						captured = captureIntegrationRequest(t, protocol, r, "operation-session")
						writeIntegrationJSON(w, protocol.generateResponse("generated answer", usage))
					}))
					t.Cleanup(server.Close)

					chatModel := newIntegrationChatModel(t, protocol, server.URL+"/v1", server.Client(), "configured-session")
					message, err := chatModel.Generate(
						opencodeauth.WithSessionID(context.Background(), "operation-session"),
						[]*schema.Message{schema.SystemMessage("system text"), schema.UserMessage("user text")},
						model.WithMaxTokens(23),
					)
					if err != nil {
						t.Fatal(err)
					}
					if message.Content != "generated answer" {
						t.Fatalf("content = %q", message.Content)
					}
					assertIntegrationUsage(t, message.ResponseMeta, usage)
					assertIntegrationPrompt(t, protocol, captured, 23, "system text", "user text")
				})
			}

			for _, usageMode := range []string{"omitted", "null", "zero", "nonzero"} {
				t.Run("registered provider advise "+usageMode, func(t *testing.T) {
					var captured integrationRequest
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						captured = captureIntegrationRequest(t, protocol, r, "configured-session")
						writeIntegrationJSON(w, protocol.generateResponse("advised answer", usageMode))
					}))
					t.Cleanup(server.Close)
					baseURL := server.URL + "/v1"
					provider, err := einoproviders.NewProvider(context.Background(), "opencode-go", "fixture-model", einoproviders.Options{
						APIKey: "explicit-key", Protocol: string(protocol.protocol), UserAgent: "integration-host/1",
						SessionID: "configured-session", BaseURL: &baseURL, HTTPClient: server.Client(),
					})
					if err != nil {
						t.Fatal(err)
					}
					text, usage, err := provider.Advise(context.Background(), "system text", "user text", 29)
					if err != nil {
						t.Fatal(err)
					}
					if text != "advised answer" {
						t.Fatalf("Advise text = %q", text)
					}
					assertIntegrationProviderUsage(t, usage, usageMode)
					assertIntegrationPrompt(t, protocol, captured, 29, "system text", "user text")
				})
			}
		})
	}
}

func TestIntegrationStreamDeliversBeforeTerminalAndSocketEOF(t *testing.T) {
	for _, protocol := range integrationProtocols {
		t.Run(protocol.name, func(t *testing.T) {
			releasePrefix := make(chan struct{})
			releaseTerminal := make(chan struct{})
			requestDone := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = captureIntegrationRequest(t, protocol, r, "configured-session")
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				select {
				case <-releasePrefix:
				case <-r.Context().Done():
					close(requestDone)
					return
				}
				_, _ = io.WriteString(w, protocol.streamPrefix())
				w.(http.Flusher).Flush()
				select {
				case <-releaseTerminal:
				case <-r.Context().Done():
					close(requestDone)
					return
				}
				_, _ = io.WriteString(w, protocol.streamTerminal())
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(requestDone)
			}))
			t.Cleanup(server.Close)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := newIntegrationChatModel(t, protocol, server.URL+"/v1", server.Client(), "configured-session").Stream(
				ctx, []*schema.Message{schema.UserMessage("stream")}, model.WithMaxTokens(23),
			)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			close(releasePrefix)
			first := receiveIntegrationChunk(t, stream)
			if first.Content != "early" {
				t.Fatalf("first content = %q", first.Content)
			}
			close(releaseTerminal)
			chunks := []*schema.Message{first}
			for {
				chunk, recvErr := receiveIntegration(t, stream)
				if errors.Is(recvErr, io.EOF) {
					break
				}
				if recvErr != nil {
					t.Fatal(recvErr)
				}
				chunks = append(chunks, chunk)
			}
			message, err := schema.ConcatMessages(chunks)
			if err != nil {
				t.Fatal(err)
			}
			if message.Content != "early done" || message.ResponseMeta == nil || message.ResponseMeta.Usage == nil || message.ResponseMeta.Usage.TotalTokens != 3 {
				t.Fatalf("concatenated message = %#v", message)
			}
			select {
			case <-requestDone:
			case <-time.After(time.Second):
				t.Fatal("terminal completion waited for socket EOF")
			}
		})
	}
}

func TestIntegrationTwoTurnParallelTools(t *testing.T) {
	for _, protocol := range integrationProtocols {
		t.Run(protocol.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []integrationRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured := captureIntegrationRequest(t, protocol, r, "configured-session")
				mu.Lock()
				requests = append(requests, captured)
				turn := len(requests)
				mu.Unlock()
				if turn == 1 {
					writeIntegrationJSON(w, protocol.toolResponse())
					return
				}
				writeIntegrationJSON(w, protocol.generateResponse("tools complete", "nonzero"))
			}))
			t.Cleanup(server.Close)

			chatModel := newIntegrationChatModel(t, protocol, server.URL+"/v1", server.Client(), "configured-session")
			bound, err := chatModel.WithTools([]*schema.ToolInfo{
				{Name: "weather", Desc: "Get weather", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"city": {Type: schema.String, Required: true},
				})},
				{Name: "clock", Desc: "Get time"},
			})
			if err != nil {
				t.Fatal(err)
			}
			first, err := bound.Generate(context.Background(), []*schema.Message{schema.UserMessage("weather and time")})
			if err != nil {
				t.Fatal(err)
			}
			if len(first.ToolCalls) != 2 || first.ToolCalls[0].ID != "call_weather" || first.ToolCalls[1].ID != "call_clock" {
				t.Fatalf("tool calls = %#v", first.ToolCalls)
			}
			encoded, err := json.Marshal(first)
			if err != nil {
				t.Fatal(err)
			}
			var replayed schema.Message
			if unmarshalErr := json.Unmarshal(encoded, &replayed); unmarshalErr != nil {
				t.Fatal(unmarshalErr)
			}
			second, err := bound.Generate(context.Background(), []*schema.Message{
				schema.UserMessage("weather and time"),
				&replayed,
				schema.ToolMessage(`{"temperature":72}`, "call_weather"),
				{Role: schema.Tool, ToolCallID: "call_clock", ToolName: "clock", Content: "12:00"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if second.Content != "tools complete" {
				t.Fatalf("second content = %q", second.Content)
			}

			mu.Lock()
			got := append([]integrationRequest(nil), requests...)
			mu.Unlock()
			if len(got) != 2 {
				t.Fatalf("requests = %d", len(got))
			}
			for i, request := range got {
				if request.session != "configured-session" {
					t.Fatalf("request %d session = %q", i, request.session)
				}
			}
			assertIntegrationToolReplay(t, protocol, got[0], got[1])
		})
	}
}

func TestIntegrationSessionsAndIdentityAreOperationLocal(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "environment-key")
	for _, protocol := range integrationProtocols {
		t.Run(protocol.name, func(t *testing.T) {
			var calls atomic.Int32
			var mu sync.Mutex
			sessions := make(map[string]int)
			pairs := make(map[string]string)
			transport := integrationRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if r.URL.Path != protocol.path || r.Header.Get("User-Agent") != "integration-host/1" {
					return nil, fmt.Errorf("request identity = %s %q", r.URL.Path, r.Header.Get("User-Agent"))
				}
				if protocol.protocol == opencodego.ProtocolMessages {
					if r.Header.Get("X-Api-Key") != "explicit-key" || r.Header.Get("Authorization") != "" {
						return nil, fmt.Errorf("messages auth headers were not owned")
					}
				} else if r.Header.Get("Authorization") != "Bearer explicit-key" || r.Header.Get("X-Api-Key") != "" {
					return nil, fmt.Errorf("bearer auth headers were not owned")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				_ = r.Body.Close()
				value, err := decodeIntegrationRequest(body)
				if err != nil {
					return nil, err
				}
				prompt := integrationOperationPrompt(value)
				session := r.Header.Get("X-OpenCode-Session")
				mu.Lock()
				sessions[session]++
				if prompt != "" {
					pairs[prompt] = session
				}
				mu.Unlock()
				return integrationHTTPResponse(r, protocol.generateResponse("session answer", "nonzero")), nil
			})
			client := &http.Client{Transport: transport}
			chatModel := newIntegrationChatModel(t, protocol, "https://api.example.test/v1", client, "configured-session")

			if _, err := chatModel.Generate(context.Background(), []*schema.Message{schema.UserMessage("fallback")}); err != nil {
				t.Fatal(err)
			}
			const operations = 6
			errs := make(chan error, operations)
			for i := range operations {
				go func() {
					ctx := opencodeauth.WithSessionID(context.Background(), fmt.Sprintf("operation-%d", i))
					message, err := chatModel.Generate(ctx, []*schema.Message{schema.UserMessage(fmt.Sprintf("prompt-%d", i))})
					if err == nil && (message == nil || message.Content != "session answer") {
						err = fmt.Errorf("message = %#v", message)
					}
					errs <- err
				}()
			}
			for range operations {
				if err := <-errs; err != nil {
					t.Fatal(err)
				}
			}
			beforeInvalid := calls.Load()
			_, err := chatModel.Generate(opencodeauth.WithSessionID(context.Background(), ""), []*schema.Message{schema.UserMessage("invalid")})
			if err == nil {
				t.Fatal("empty operation session succeeded")
			}
			if calls.Load() != beforeInvalid {
				t.Fatalf("empty session made a network request: %d -> %d", beforeInvalid, calls.Load())
			}

			mu.Lock()
			defer mu.Unlock()
			if sessions["configured-session"] != 1 || len(sessions) != operations+1 {
				t.Fatalf("sessions = %#v", sessions)
			}
			for i := range operations {
				prompt := fmt.Sprintf("prompt-%d", i)
				session := fmt.Sprintf("operation-%d", i)
				if sessions[session] != 1 || pairs[prompt] != session {
					t.Fatalf("sessions/pairs = %#v/%#v", sessions, pairs)
				}
			}
		})
	}
}

type integrationRequest struct {
	body    []byte
	value   map[string]any
	session string
}

func captureIntegrationRequest(t *testing.T, protocol integrationProtocol, r *http.Request, session string) integrationRequest {
	t.Helper()
	if r.Method != http.MethodPost || r.URL.Path != protocol.path {
		t.Errorf("request = %s %s, want POST %s", r.Method, r.URL.Path, protocol.path)
	}
	if r.Header.Get("User-Agent") != "integration-host/1" || r.Header.Get("X-OpenCode-Session") != session {
		t.Errorf("agent/session = %q/%q", r.Header.Get("User-Agent"), r.Header.Get("X-OpenCode-Session"))
	}
	if protocol.protocol == opencodego.ProtocolMessages {
		if r.Header.Get("X-Api-Key") != "explicit-key" || r.Header.Get("Authorization") != "" {
			t.Errorf("messages auth = %q/%q", r.Header.Get("X-Api-Key"), r.Header.Get("Authorization"))
		}
	} else if r.Header.Get("Authorization") != "Bearer explicit-key" || r.Header.Get("X-Api-Key") != "" {
		t.Errorf("bearer auth = %q/%q", r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"))
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("read request: %v", err)
	}
	value, err := decodeIntegrationRequest(body)
	if err != nil {
		t.Errorf("decode request: %v", err)
	}
	return integrationRequest{body: body, value: value, session: r.Header.Get("X-OpenCode-Session")}
}

func assertIntegrationPrompt(t *testing.T, protocol integrationProtocol, request integrationRequest, cap int, system, user string) {
	t.Helper()
	if request.value["model"] != "fixture-model" {
		t.Fatalf("model = %#v", request.value["model"])
	}
	if request.value[protocol.capField] != json.Number(fmt.Sprint(cap)) {
		t.Fatalf("%s = %#v, want %d", protocol.capField, request.value[protocol.capField], cap)
	}
	switch protocol.protocol {
	case opencodego.ProtocolChatCompletions:
		messages := integrationObjects(t, request.value["messages"])
		if len(messages) != 2 || messages[0]["role"] != "system" || messages[0]["content"] != system ||
			messages[1]["role"] != "user" || messages[1]["content"] != user {
			t.Fatalf("chat prompt = %#v", messages)
		}
	case opencodego.ProtocolMessages:
		systemBlocks := integrationObjects(t, request.value["system"])
		messages := integrationObjects(t, request.value["messages"])
		if len(systemBlocks) != 1 || systemBlocks[0]["type"] != "text" || systemBlocks[0]["text"] != system || len(messages) != 1 || messages[0]["role"] != "user" {
			t.Fatalf("messages prompt = system:%#v messages:%#v", systemBlocks, messages)
		}
		userBlocks := integrationObjects(t, messages[0]["content"])
		if len(userBlocks) != 1 || userBlocks[0]["type"] != "text" || userBlocks[0]["text"] != user {
			t.Fatalf("messages user content = %#v", userBlocks)
		}
	case opencodego.ProtocolResponses:
		if request.value["instructions"] != system {
			t.Fatalf("instructions = %#v", request.value["instructions"])
		}
		input := integrationObjects(t, request.value["input"])
		if len(input) != 1 || input[0]["type"] != "message" || input[0]["role"] != "user" {
			t.Fatalf("responses input = %#v", input)
		}
		content := integrationObjects(t, input[0]["content"])
		if len(content) != 1 || content[0]["type"] != "input_text" || content[0]["text"] != user {
			t.Fatalf("responses user content = %#v", content)
		}
	}
}

func integrationJSONContains(value any, want string) bool {
	switch value := value.(type) {
	case string:
		return value == want
	case []any:
		for _, child := range value {
			if integrationJSONContains(child, want) {
				return true
			}
		}
	case map[string]any:
		for _, child := range value {
			if integrationJSONContains(child, want) {
				return true
			}
		}
	}
	return false
}

func assertIntegrationToolReplay(t *testing.T, protocol integrationProtocol, first, second integrationRequest) {
	t.Helper()
	tools := integrationObjects(t, first.value["tools"])
	if len(tools) != 2 {
		t.Fatalf("tool definitions = %#v", tools)
	}
	var weather, clock, parameters map[string]any
	switch protocol.protocol {
	case opencodego.ProtocolChatCompletions:
		if tools[0]["type"] != "function" || tools[1]["type"] != "function" {
			t.Fatalf("chat tool wrappers = %#v", tools)
		}
		weather = integrationObject(t, tools[0]["function"])
		clock = integrationObject(t, tools[1]["function"])
		parameters = integrationObject(t, weather["parameters"])
	case opencodego.ProtocolMessages:
		weather, clock = tools[0], tools[1]
		parameters = integrationObject(t, weather["input_schema"])
	case opencodego.ProtocolResponses:
		if tools[0]["type"] != "function" || tools[1]["type"] != "function" {
			t.Fatalf("responses tool wrappers = %#v", tools)
		}
		weather, clock = tools[0], tools[1]
		parameters = integrationObject(t, weather["parameters"])
	}
	if weather["name"] != "weather" || weather["description"] != "Get weather" || clock["name"] != "clock" || clock["description"] != "Get time" {
		t.Fatalf("tool identities = %#v", tools)
	}
	properties := integrationObject(t, parameters["properties"])
	city := integrationObject(t, properties["city"])
	required, ok := parameters["required"].([]any)
	if parameters["type"] != "object" || city["type"] != "string" || !ok || len(required) != 1 || required[0] != "city" {
		t.Fatalf("weather parameter schema = %#v", parameters)
	}
	switch protocol.protocol {
	case opencodego.ProtocolChatCompletions:
		messages := integrationObjects(t, second.value["messages"])
		if len(messages) != 4 || messages[1]["role"] != "assistant" || messages[2]["role"] != "tool" || messages[3]["role"] != "tool" {
			t.Fatalf("chat replay roles = %#v", messages)
		}
		calls := integrationObjects(t, messages[1]["tool_calls"])
		if len(calls) != 2 || calls[0]["id"] != "call_weather" || calls[1]["id"] != "call_clock" ||
			messages[2]["tool_call_id"] != "call_weather" || messages[3]["tool_call_id"] != "call_clock" {
			t.Fatalf("chat replay correlation = %#v", messages)
		}
	case opencodego.ProtocolMessages:
		messages := integrationObjects(t, second.value["messages"])
		if len(messages) != 4 || messages[1]["role"] != "assistant" || messages[2]["role"] != "user" || messages[3]["role"] != "user" {
			t.Fatalf("messages replay roles = %#v", messages)
		}
		calls := integrationObjects(t, messages[1]["content"])
		weatherResult := integrationObjects(t, messages[2]["content"])
		clockResult := integrationObjects(t, messages[3]["content"])
		if len(calls) != 2 || calls[0]["id"] != "call_weather" || calls[1]["id"] != "call_clock" ||
			len(weatherResult) != 1 || weatherResult[0]["tool_use_id"] != "call_weather" ||
			len(clockResult) != 1 || clockResult[0]["tool_use_id"] != "call_clock" {
			t.Fatalf("messages replay correlation = %#v", messages)
		}
	case opencodego.ProtocolResponses:
		input := integrationObjects(t, second.value["input"])
		wantTypes := []any{"message", "reasoning", "function_call", "function_call", "function_call_output", "function_call_output"}
		if len(input) != len(wantTypes) {
			t.Fatalf("responses replay = %#v", input)
		}
		for i, want := range wantTypes {
			if input[i]["type"] != want {
				t.Fatalf("responses input %d type = %#v, want %q", i, input[i]["type"], want)
			}
		}
		if input[2]["call_id"] != "call_weather" || input[3]["call_id"] != "call_clock" ||
			input[4]["call_id"] != "call_weather" || input[5]["call_id"] != "call_clock" ||
			input[1]["encrypted_content"] != "opaque" {
			t.Fatalf("responses replay correlation = %#v", input)
		}
	}
	for _, want := range []string{"temperature", "12:00"} {
		if !bytes.Contains(second.body, []byte(want)) {
			t.Fatalf("second request missing result %q: %s", want, second.body)
		}
	}
}

func integrationObjects(t *testing.T, value any) []map[string]any {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("value is not an array: %#v", value)
	}
	objects := make([]map[string]any, len(items))
	for i, item := range items {
		objects[i], ok = item.(map[string]any)
		if !ok {
			t.Fatalf("item %d is not an object: %#v", i, item)
		}
	}
	return objects
}

func integrationObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return object
}

func decodeIntegrationRequest(body []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode request: %w", err)
	}
	return value, nil
}

func integrationOperationPrompt(value map[string]any) string {
	for i := range 6 {
		prompt := fmt.Sprintf("prompt-%d", i)
		if integrationJSONContains(value, prompt) {
			return prompt
		}
	}
	return ""
}

func assertIntegrationUsage(t *testing.T, meta *schema.ResponseMeta, usage string) {
	t.Helper()
	var got *schema.TokenUsage
	if meta != nil {
		got = meta.Usage
	}
	switch usage {
	case "omitted", "null":
		if got != nil {
			t.Fatalf("usage = %#v, want absent", got)
		}
	case "zero":
		if got == nil || *got != (schema.TokenUsage{}) {
			t.Fatalf("usage = %#v, want explicit zero", got)
		}
	case "nonzero":
		if got == nil || got.PromptTokens != 2 || got.CompletionTokens != 1 || got.TotalTokens != 3 {
			t.Fatalf("usage = %#v", got)
		}
	default:
		t.Fatalf("unknown usage fixture %q", usage)
	}
}

func assertIntegrationProviderUsage(t *testing.T, got einoproviders.Usage, usage string) {
	t.Helper()
	switch usage {
	case "omitted", "null":
		if got.Available {
			t.Fatalf("usage = %+v, want unavailable", got)
		}
	case "zero":
		if !got.Available || got.InputTokens != 0 || got.OutputTokens != 0 {
			t.Fatalf("usage = %+v, want explicit zero", got)
		}
	case "nonzero":
		if got != (einoproviders.Usage{InputTokens: 2, OutputTokens: 1, Available: true}) {
			t.Fatalf("usage = %+v", got)
		}
	}
}

func newIntegrationChatModel(t *testing.T, protocol integrationProtocol, baseURL string, client *http.Client, session string) model.ToolCallingChatModel {
	t.Helper()
	cap := 17
	chatModel, err := opencodego.NewChatModel(context.Background(), opencodego.ChatModelConfig{
		Model: "fixture-model", Protocol: protocol.protocol, APIKey: "explicit-key", UserAgent: "integration-host/1",
		SessionID: session, BaseURL: baseURL, HTTPClient: client, MaxTokens: &cap,
	})
	if err != nil {
		t.Fatal(err)
	}
	if chatModel == nil {
		t.Fatal("NewChatModel returned nil")
	}
	return chatModel
}

func writeIntegrationJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func (p integrationProtocol) generateResponse(text, usage string) string {
	usageJSON := ""
	switch usage {
	case "null":
		usageJSON = `,"usage":null`
	case "zero":
		switch p.protocol {
		case opencodego.ProtocolMessages:
			usageJSON = `,"usage":{"input_tokens":0,"output_tokens":0}`
		case opencodego.ProtocolChatCompletions:
			usageJSON = `,"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`
		default:
			usageJSON = `,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}`
		}
	case "nonzero":
		switch p.protocol {
		case opencodego.ProtocolMessages:
			usageJSON = `,"usage":{"input_tokens":2,"output_tokens":1}`
		case opencodego.ProtocolChatCompletions:
			usageJSON = `,"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}`
		default:
			usageJSON = `,"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}`
		}
	}
	switch p.protocol {
	case opencodego.ProtocolChatCompletions:
		return `{"id":"chat-id","object":"chat.completion","created":0,"model":"fixture-model","choices":[{"index":0,"message":{"role":"assistant","content":` + quoted(text) + `},"finish_reason":"stop"}]` + usageJSON + `}`
	case opencodego.ProtocolMessages:
		return `{"id":"message-id","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"text","text":` + quoted(text) + `}],"stop_reason":"end_turn","stop_sequence":null` + usageJSON + `}`
	default:
		return `{"status":"completed","output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":` + quoted(text) + `}]}]` + usageJSON + `}`
	}
}

func (p integrationProtocol) toolResponse() string {
	switch p.protocol {
	case opencodego.ProtocolChatCompletions:
		return `{"id":"chat-tools","object":"chat.completion","created":0,"model":"fixture-model","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{\"city\":\"NYC\"}"}},{"id":"call_clock","type":"function","function":{"name":"clock","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`
	case opencodego.ProtocolMessages:
		return `{"id":"message-tools","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"tool_use","id":"call_weather","name":"weather","input":{"city":"NYC"}},{"type":"tool_use","id":"call_clock","name":"clock","input":{}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":1}}`
	default:
		return `{"status":"completed","output":[{"type":"reasoning","status":"completed","encrypted_content":"opaque","summary":[]},{"type":"function_call","status":"completed","name":"weather","arguments":"{\"city\":\"NYC\"}","call_id":"call_weather"},{"type":"function_call","status":"completed","name":"clock","arguments":"{}","call_id":"call_clock"}]}`
	}
}

func (p integrationProtocol) streamPrefix() string {
	switch p.protocol {
	case opencodego.ProtocolChatCompletions:
		return "data: {\"id\":\"stream\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"early\"}}]}\n\n"
	case opencodego.ProtocolMessages:
		return strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"message-stream","type":"message","role":"assistant","content":[],"model":"fixture-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":0}}}`,
			"",
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			"",
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"early"}}`,
			"", "",
		}, "\n")
	default:
		return "data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
			"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"delta\":\"early\"}\n\n"
	}
}

func (p integrationProtocol) streamTerminal() string {
	switch p.protocol {
	case opencodego.ProtocolChatCompletions:
		return "data: {\"id\":\"stream\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" done\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: {\"id\":\"stream\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"fixture-model\",\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\n" +
			"data: [DONE]\n\n"
	case opencodego.ProtocolMessages:
		return strings.Join([]string{
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" done"}}`,
			"",
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":0}`,
			"",
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`,
			"",
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			"", "",
		}, "\n")
	default:
		return "data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"delta\":\" done\"}\n\n" +
			"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"early done\"}]}}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"early done\"}]}],\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3}}}\n\n"
	}
}

func receiveIntegrationChunk(t *testing.T, stream *schema.StreamReader[*schema.Message]) *schema.Message {
	t.Helper()
	message, err := receiveIntegration(t, stream)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func receiveIntegration(t *testing.T, stream *schema.StreamReader[*schema.Message]) (*schema.Message, error) {
	t.Helper()
	result := make(chan struct {
		message *schema.Message
		err     error
	}, 1)
	go func() {
		message, err := stream.Recv()
		result <- struct {
			message *schema.Message
			err     error
		}{message: message, err: err}
	}()
	select {
	case got := <-result:
		return got.message, got.err
	case <-time.After(time.Second):
		t.Fatal("timed out receiving stream chunk")
		return nil, nil
	}
}

type integrationRoundTripper func(*http.Request) (*http.Response, error)

func (f integrationRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func integrationHTTPResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func quoted(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
