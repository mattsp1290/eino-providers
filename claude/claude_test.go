package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestConstructors(t *testing.T) {
	t.Parallel()

	p := NewClaudeProvider("key", "model")
	if p == nil {
		t.Fatal("NewClaudeProvider returned nil")
	}
	baseURL := "http://example.test"
	p = NewClaudeProviderWithBaseURL("key", "model", &baseURL)
	if p == nil {
		t.Fatal("NewClaudeProviderWithBaseURL returned nil")
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	baseURL := "http://example.test"
	p, err := einoproviders.NewProvider(context.Background(), "claude", "claude-sonnet", einoproviders.Options{
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

func TestAdviseUsesMaxTokens(t *testing.T) {
	t.Parallel()

	var gotRequest struct {
		Model     string          `json:"model"`
		MaxTokens int             `json:"max_tokens"`
		System    json.RawMessage `json:"system"`
		Messages  []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("X-Api-Key"); got != "key" {
			t.Errorf("X-Api-Key = %q, want key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_test",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet",
			"content":[{"type":"text","text":"hello"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":3,"output_tokens":4}
		}`))
	}))
	t.Cleanup(srv.Close)

	baseURL := srv.URL
	p := NewClaudeProviderWithBaseURL("key", "claude-sonnet", &baseURL)
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
	if gotRequest.Model != "claude-sonnet" {
		t.Fatalf("model = %q, want claude-sonnet", gotRequest.Model)
	}
	if gotRequest.MaxTokens != 123 {
		t.Fatalf("max_tokens = %d, want 123", gotRequest.MaxTokens)
	}
	if !strings.Contains(string(gotRequest.System), "system") {
		t.Fatalf("system = %s, want content containing system", gotRequest.System)
	}
	if len(gotRequest.Messages) != 1 || gotRequest.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v, want one user message", gotRequest.Messages)
	}
}
