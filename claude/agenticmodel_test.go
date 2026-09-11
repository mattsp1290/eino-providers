package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestAgenticModelUsesNativeMessagesBytes(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version = %q", got)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request["stream"] == true {
			_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"native-claude\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"reason\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
			return
		}
		_, _ = w.Write([]byte(`{"id":"msg_native","model":"native-claude","stop_reason":"end_turn","content":[{"type":"thinking","thinking":"reason","signature":"sig"},{"type":"text","text":"hello"}],"usage":{"input_tokens":2,"output_tokens":3}}`))
	}))
	defer srv.Close()
	m, err := NewAgenticModel(context.Background(), AgenticModelConfig{APIKey: "key", Model: "requested", MaxTokens: 32, BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	input := []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.UserInputText{Text: "prompt"})}}}
	generated, err := m.Generate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(generated.ContentBlocks) != 2 || generated.ContentBlocks[0].Reasoning.Signature != "sig" {
		t.Fatalf("generated = %#v", generated)
	}
	identity := generated.ResponseMeta.Extension.(einoproviders.AgenticResponseIdentity)
	if identity.ReturnedModel != "native-claude" || identity.CorrelationID != "msg_native" {
		t.Fatalf("identity = %#v", identity)
	}
	stream, err := m.Stream(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var chunks []*schema.AgenticMessage
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 4 || chunks[0].ContentBlocks[0].Reasoning.Text != "reason" || chunks[3].ResponseMeta == nil {
		t.Fatalf("stream = %#v", chunks)
	}
	concatenated, err := schema.ConcatAgenticMessages(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if len(concatenated.ContentBlocks) != len(generated.ContentBlocks) || concatenated.ContentBlocks[0].Reasoning.Signature != generated.ContentBlocks[0].Reasoning.Signature || concatenated.ContentBlocks[1].AssistantGenText.Text != generated.ContentBlocks[1].AssistantGenText.Text {
		t.Fatalf("stream concat = %#v, generate = %#v", concatenated, generated)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestAgenticModelRejectsToolSearchBeforeDispatch(t *testing.T) {
	m := &agenticModel{model: "m", maxTokens: 1, client: &http.Client{}, limits: einoproviders.AgenticLimits{}.WithDefaults()}
	_, err := m.Generate(context.Background(), nil, model.WithDeferredTools([]*schema.ToolInfo{{Name: "x"}}))
	if !errors.Is(err, einoproviders.ErrUnsupportedCapability) {
		t.Fatalf("error = %v", err)
	}
}

func TestAgenticModelRejectsMalformedBlockBeforeDispatch(t *testing.T) {
	m := &agenticModel{model: "m", maxTokens: 1, client: &http.Client{}, limits: einoproviders.AgenticLimits{}.WithDefaults()}
	_, err := m.Generate(context.Background(), []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{{Type: schema.ContentBlockTypeUserInputText}}}})
	if !errors.Is(err, einoproviders.ErrUnsupportedCapability) {
		t.Fatalf("error=%v", err)
	}
}

func TestAgenticModelRejectsNonTextSystemBlockBeforeDispatch(t *testing.T) {
	m := &agenticModel{model: "m", maxTokens: 1, client: &http.Client{}, limits: einoproviders.AgenticLimits{}.WithDefaults()}
	_, err := m.Generate(context.Background(), []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeSystem, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.Reasoning{Text: "private"})}}})
	if !errors.Is(err, einoproviders.ErrUnsupportedCapability) {
		t.Fatalf("error=%v", err)
	}
}

func TestAgenticModelStreamPreservesToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"native\"}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_native\",\"name\":\"weather\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"city\\\":\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"NYC\\\"}\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()
	m, err := NewAgenticModel(context.Background(), AgenticModelConfig{APIKey: "key", Model: "m", MaxTokens: 8, BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := m.Stream(context.Background(), []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeUser}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var call *schema.FunctionToolCall
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(chunk.ContentBlocks) > 0 {
			call = chunk.ContentBlocks[0].FunctionToolCall
		}
	}
	if call == nil || call.CallID != "call_native" || call.Name != "weather" || call.Arguments != "{\"city\":\"NYC\"}" {
		t.Fatalf("call=%#v", call)
	}
}
