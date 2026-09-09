package opencodego_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	einoproviders "github.com/mattsp1290/eino-providers"
	_ "github.com/mattsp1290/eino-providers/opencodego"
)

func TestBlankImportRegistersProviderAndAdvise(t *testing.T) {
	var calls atomic.Int32
	var request struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got, want := r.URL.Path, "/v1/chat/completions"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer real-key"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("User-Agent"), "provider-test/1"; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("X-OpenCode-Session"), "provider-session"; got != want {
			t.Errorf("session = %q, want %q", got, want)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-provider","object":"chat.completion","created":0,"model":"fixture-model",
			"choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}
		}`))
	}))
	t.Cleanup(server.Close)

	baseURL := server.URL + "/v1"
	configuredCap := 11
	provider, err := einoproviders.NewProvider(context.Background(), "opencode-go", "fixture-model", einoproviders.Options{
		APIKey:     "real-key",
		Protocol:   "chat-completions",
		UserAgent:  "provider-test/1",
		SessionID:  "provider-session",
		BaseURL:    &baseURL,
		MaxTokens:  &configuredCap,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	text, usage, err := provider.Advise(context.Background(), "system text", "user text", 37)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if text != "answer" || usage != (einoproviders.Usage{InputTokens: 7, OutputTokens: 3, Available: true}) {
		t.Fatalf("Advise = %q, %+v", text, usage)
	}
	if request.Model != "fixture-model" || request.MaxTokens != 37 {
		t.Fatalf("request model/cap = %q/%d", request.Model, request.MaxTokens)
	}
	if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[0].Content != "system text" || request.Messages[1].Role != "user" || request.Messages[1].Content != "user text" {
		t.Fatalf("messages = %#v, want exact system/user pair", request.Messages)
	}
	if calls.Load() != 1 {
		t.Fatalf("network calls = %d, want 1", calls.Load())
	}
}

func TestProviderRejectsUnavailableProtocolsAtConstruction(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "")
	for _, protocol := range []string{"responses"} {
		t.Run(protocol, func(t *testing.T) {
			_, err := einoproviders.NewProvider(context.Background(), "opencode-go", "fixture", einoproviders.Options{
				APIKey: "key", Protocol: protocol, UserAgent: "provider-test/1",
			})
			if !errors.Is(err, einoproviders.ErrProviderInit) {
				t.Fatalf("error = %v, want ErrProviderInit", err)
			}
		})
	}
}

func TestAdviseRejectsNonpositiveCapWithoutNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	baseURL := server.URL + "/v1"
	provider, err := einoproviders.NewProvider(context.Background(), "opencode-go", "fixture", einoproviders.Options{
		APIKey: "key", Protocol: "chat-completions", UserAgent: "provider-test/1", SessionID: "session", BaseURL: &baseURL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, cap := range []int{0, -1} {
		if _, _, err := provider.Advise(context.Background(), "system", "user", cap); !errors.Is(err, einoproviders.ErrProviderInit) {
			t.Fatalf("Advise cap %d error = %v, want ErrProviderInit", cap, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("network calls = %d, want 0", calls.Load())
	}
}

func TestAdvisePreservesMissingUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-no-usage","object":"chat.completion","created":0,"model":"fixture","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)
	baseURL := server.URL + "/v1"
	provider, err := einoproviders.NewProvider(context.Background(), "opencode-go", "fixture", einoproviders.Options{
		APIKey: "key", Protocol: "chat-completions", UserAgent: "provider-test/1", SessionID: "session", BaseURL: &baseURL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, usage, err := provider.Advise(context.Background(), "system", "user", 9)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Available {
		t.Fatalf("usage = %+v, want unavailable", usage)
	}
}
