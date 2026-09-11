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
			_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"native-claude\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
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
	if len(chunks) != 2 || chunks[0].ContentBlocks[0].AssistantGenText.Text != "hello" || chunks[1].ResponseMeta == nil {
		t.Fatalf("stream = %#v", chunks)
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
