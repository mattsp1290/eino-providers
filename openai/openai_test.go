package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestConstructors(t *testing.T) {
	t.Parallel()

	p := NewOpenAIProvider("key", "model")
	if p == nil {
		t.Fatal("NewOpenAIProvider returned nil")
	}
	p = NewOpenAIProviderWithBaseURL("key", "model", "http://example.test/v1")
	if p == nil {
		t.Fatal("NewOpenAIProviderWithBaseURL returned nil")
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	baseURL := "http://example.test/v1"
	p, err := einoproviders.NewProvider(context.Background(), "openai", "gpt-5", einoproviders.Options{
		APIKey:  "key",
		BaseURL: &baseURL,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p == nil {
		t.Fatal("NewProvider returned nil provider")
	}
	if _, ok := p.(*Provider); !ok {
		t.Fatalf("NewProvider returned %T, want *Provider", p)
	}
}

func TestAdviseUsesMaxCompletionTokens(t *testing.T) {
	t.Parallel()

	var gotRequest struct {
		Model               string `json:"model"`
		MaxCompletionTokens int    `json:"max_completion_tokens"`
		Messages            []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Fatalf("Authorization = %q, want bearer key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"object":"chat.completion",
			"created":0,
			"model":"gpt-5",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}
		}`))
	}))
	t.Cleanup(srv.Close)

	p := NewOpenAIProviderWithBaseURL("key", "gpt-5", srv.URL+"/v1")
	text, usage, err := p.Advise(context.Background(), "system", "user", 123)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if text != "hello" {
		t.Fatalf("text = %q, want hello", text)
	}
	if !usage.Available || usage.InputTokens != 3 || usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v, want available 3/4", usage)
	}
	if gotRequest.Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", gotRequest.Model)
	}
	if gotRequest.MaxCompletionTokens != 123 {
		t.Fatalf("max_completion_tokens = %d, want 123", gotRequest.MaxCompletionTokens)
	}
	if len(gotRequest.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(gotRequest.Messages))
	}
	if gotRequest.Messages[0].Role != "system" || gotRequest.Messages[1].Role != "user" {
		t.Fatalf("message roles = %+v, want system/user", gotRequest.Messages)
	}
}
