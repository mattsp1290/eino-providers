package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestAgenticModelGenerateAndStreamUseNativeChatProtocol(t *testing.T) {
	var chatCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/chat":
			chatCalls.Add(1)
			var request struct {
				Stream   bool            `json:"stream"`
				Messages []ollamaMessage `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(request.Messages) != 1 || request.Messages[0].Content != "hello" {
				t.Errorf("native request = %#v", request)
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			if request.Stream {
				_, _ = w.Write([]byte("{\"model\":\"native-model\",\"message\":{\"role\":\"assistant\",\"content\":\"hi\"},\"done\":false}\n{\"model\":\"native-model\",\"message\":{\"role\":\"assistant\",\"content\":\" there\"},\"done\":true,\"done_reason\":\"stop\",\"prompt_eval_count\":2,\"eval_count\":3}\n"))
				return
			}
			_, _ = w.Write([]byte(`{"model":"native-model","message":{"role":"assistant","content":"hi there"},"done":true,"done_reason":"stop","prompt_eval_count":2,"eval_count":3}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	m, err := NewAgenticModel(context.Background(), AgenticModelConfig{BaseURL: srv.URL, Model: "requested", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	input := []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.UserInputText{Text: "hello"})}}}
	generated, err := m.Generate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if got := generated.ContentBlocks[0].AssistantGenText.Text; got != "hi there" {
		t.Fatalf("Generate text = %q", got)
	}
	identity, ok := generated.ResponseMeta.Extension.(einoproviders.AgenticResponseIdentity)
	if !ok || identity.ReturnedModel != "native-model" {
		t.Fatalf("identity = %#v", generated.ResponseMeta.Extension)
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
	if len(chunks) != 2 || chunks[0].ContentBlocks[0].AssistantGenText.Text != "hi" || chunks[1].ContentBlocks[0].StreamingMeta.Index != 0 {
		t.Fatalf("stream chunks = %#v", chunks)
	}
	concatenated, err := schema.ConcatAgenticMessages(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if got := concatenated.ContentBlocks[0].AssistantGenText.Text; got != generated.ContentBlocks[0].AssistantGenText.Text {
		t.Fatalf("stream concat text = %q, Generate text = %q", got, generated.ContentBlocks[0].AssistantGenText.Text)
	}
	if chatCalls.Load() != 2 {
		t.Fatalf("chat calls = %d, want 2", chatCalls.Load())
	}
}

func TestAgenticModelRejectsUnsupportedOptionsBeforeDispatch(t *testing.T) {
	var chatCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/chat" {
			chatCalls.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	m, err := NewAgenticModel(context.Background(), AgenticModelConfig{BaseURL: srv.URL, Model: "requested", HTTPClient: srv.Client(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Generate(context.Background(), []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeUser}}, model.WithDeferredTools([]*schema.ToolInfo{{Name: "deferred"}}))
	if !errors.Is(err, einoproviders.ErrUnsupportedCapability) {
		t.Fatalf("Generate error = %v", err)
	}
	if chatCalls.Load() != 0 {
		t.Fatalf("chat calls = %d, want 0", chatCalls.Load())
	}
}

func TestAgenticModelAssignsSyntheticToolCallIDs(t *testing.T) {
	m := &agenticModel{model: "m"}
	message := m.fromResponse(ollamaChatResponse{Message: ollamaMessage{ToolCalls: []ollamaToolCall{{Function: struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}{Name: "weather", Arguments: map[string]any{"city": "NYC"}}}}}}, false)
	if got := message.ContentBlocks[0].FunctionToolCall.CallID; got != "ollama-0" {
		t.Fatalf("CallID=%q", got)
	}
}
