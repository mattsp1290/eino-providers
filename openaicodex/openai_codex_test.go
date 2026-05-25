package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	codexauth "github.com/mattsp1290/codex-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func overrideCodexHTTPClient(t *testing.T, fn func(context.Context) (*http.Client, error)) {
	t.Helper()
	orig := codexHTTPClient
	codexHTTPClient = fn
	t.Cleanup(func() { codexHTTPClient = orig })
}

type stubRoundTripper func(*http.Request) (*http.Response, error)

func (f stubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func chatCompletionResponse(content string) io.ReadCloser {
	body, _ := json.Marshal(map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "gpt-codex",
		"choices": []map[string]any{
			{
				"index":         0,
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": content},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	})
	return io.NopCloser(strings.NewReader(string(body)))
}

func apiErrorResponse(code, message string) io.ReadCloser {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
			"code":    code,
		},
	})
	return io.NopCloser(strings.NewReader(string(body)))
}

func TestNewOpenAICodexProvider_NotLoggedIn(t *testing.T) {
	overrideCodexHTTPClient(t, func(context.Context) (*http.Client, error) {
		return nil, codexauth.ErrNotLoggedIn
	})

	_, err := NewOpenAICodexProvider(context.Background(), "gpt-codex")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, einoproviders.ErrProviderInit) {
		t.Errorf("errors.Is(err, ErrProviderInit) = false, want true")
	}
	if !errors.Is(err, einoproviders.ErrProviderAuth) {
		t.Errorf("errors.Is(err, ErrProviderAuth) = false, want true")
	}
	if !errors.Is(err, codexauth.ErrNotLoggedIn) {
		t.Errorf("errors.Is(err, codexauth.ErrNotLoggedIn) = false, want true")
	}
}

func TestNewOpenAICodexProvider_GenericClientError(t *testing.T) {
	underlying := errors.New("disk read error")
	overrideCodexHTTPClient(t, func(context.Context) (*http.Client, error) {
		return nil, underlying
	})

	_, err := NewOpenAICodexProvider(context.Background(), "gpt-codex")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, einoproviders.ErrProviderInit) {
		t.Errorf("errors.Is(err, ErrProviderInit) = false, want true")
	}
	if !errors.Is(err, underlying) {
		t.Errorf("errors.Is(err, underlying) = false, want true")
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: stubRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("registry construction should not issue requests")
		return nil, nil
	})}
	p, err := einoproviders.NewProvider(context.Background(), "openai-codex", "gpt-codex", einoproviders.Options{
		HTTPClient: client,
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

func TestRegistryRejectsBaseURL(t *testing.T) {
	overrideCodexHTTPClient(t, func(context.Context) (*http.Client, error) {
		t.Fatal("forbidden BaseURL must be rejected before loading Codex credentials")
		return nil, nil
	})

	baseURL := "http://example.test/v1"
	_, err := einoproviders.NewProvider(context.Background(), "openai-codex", "gpt-codex", einoproviders.Options{
		BaseURL: &baseURL,
	})
	if err == nil {
		t.Fatal("expected BaseURL error, got nil")
	}
	if !errors.Is(err, einoproviders.ErrProviderInit) {
		t.Errorf("errors.Is(err, ErrProviderInit) = false, want true")
	}
}

func TestRegistryRejectsMaxTokens(t *testing.T) {
	overrideCodexHTTPClient(t, func(context.Context) (*http.Client, error) {
		t.Fatal("forbidden MaxTokens must be rejected before loading Codex credentials")
		return nil, nil
	})

	maxTokens := 100
	_, err := einoproviders.NewProvider(context.Background(), "openai-codex", "gpt-codex", einoproviders.Options{
		MaxTokens: &maxTokens,
	})
	if err == nil {
		t.Fatal("expected MaxTokens error, got nil")
	}
	if !errors.Is(err, einoproviders.ErrProviderInit) {
		t.Errorf("errors.Is(err, ErrProviderInit) = false, want true")
	}
}

func TestOpenAICodexProvider_Advise_DelegatesWithoutBaseURLOrMaxTokens(t *testing.T) {
	t.Parallel()

	const wantContent = "Hello from Codex"
	var gotRequest map[string]any
	callCount := 0

	stub := stubRoundTripper(func(r *http.Request) (*http.Response, error) {
		callCount++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+dummyAPIKey {
			t.Fatalf("Authorization = %q, want bearer dummy key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       chatCompletionResponse(wantContent),
			Header:     make(http.Header),
		}, nil
	})

	p := NewOpenAICodexProviderWithHTTPClient(&http.Client{Transport: stub}, "gpt-codex")
	got, usage, err := p.Advise(context.Background(), "system", "user", 100)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if got != wantContent {
		t.Errorf("Advise content = %q, want %q", got, wantContent)
	}
	if callCount != 1 {
		t.Errorf("http calls = %d, want 1", callCount)
	}
	if !usage.Available || usage.InputTokens != 10 || usage.OutputTokens != 5 {
		t.Errorf("usage = %+v, want available 10/5", usage)
	}
	if gotRequest["model"] != "gpt-codex" {
		t.Errorf("model = %v, want gpt-codex", gotRequest["model"])
	}
	if _, ok := gotRequest["max_completion_tokens"]; ok {
		t.Error("request unexpectedly included max_completion_tokens")
	}
	if _, ok := gotRequest["max_tokens"]; ok {
		t.Error("request unexpectedly included max_tokens")
	}
	if _, ok := gotRequest["base_url"]; ok {
		t.Error("request unexpectedly included base_url")
	}
}

func TestOpenAICodexProvider_Advise_PlanNotIncluded(t *testing.T) {
	t.Parallel()

	stub := stubRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body:       apiErrorResponse(codexErrCodeUsageNotIncluded, "plan does not include Codex"),
			Header:     make(http.Header),
		}, nil
	})

	p := NewOpenAICodexProviderWithHTTPClient(&http.Client{Transport: stub}, "gpt-codex")
	_, _, err := p.Advise(context.Background(), "system", "user", 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, einoproviders.ErrProviderAuth) {
		t.Errorf("errors.Is(err, ErrProviderAuth) = false, want true")
	}
	if !errors.Is(err, codexauth.ErrPlanNotIncluded) {
		t.Errorf("errors.Is(err, codexauth.ErrPlanNotIncluded) = false, want true")
	}
}

func TestOpenAICodexProvider_Advise_QuotaExceeded(t *testing.T) {
	t.Parallel()

	stub := stubRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       apiErrorResponse(codexErrCodeInsufficientQuota, "quota exceeded"),
			Header:     make(http.Header),
		}, nil
	})

	p := NewOpenAICodexProviderWithHTTPClient(&http.Client{Transport: stub}, "gpt-codex")
	_, _, err := p.Advise(context.Background(), "system", "user", 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, einoproviders.ErrProviderAuth) {
		t.Errorf("errors.Is(err, ErrProviderAuth) = false, want true")
	}
	if !errors.Is(err, codexauth.ErrQuotaExceeded) {
		t.Errorf("errors.Is(err, codexauth.ErrQuotaExceeded) = false, want true")
	}
}

func TestOpenAICodexProvider_Advise_GenericAPIError(t *testing.T) {
	t.Parallel()

	stub := stubRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       apiErrorResponse("server_error", "internal server error"),
			Header:     make(http.Header),
		}, nil
	})

	p := NewOpenAICodexProviderWithHTTPClient(&http.Client{Transport: stub}, "gpt-codex")
	_, _, err := p.Advise(context.Background(), "system", "user", 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, einoproviders.ErrProviderAuth) {
		t.Error("generic API error must not satisfy ErrProviderAuth")
	}
	if errors.Is(err, codexauth.ErrPlanNotIncluded) {
		t.Error("generic API error must not satisfy ErrPlanNotIncluded")
	}
	if errors.Is(err, codexauth.ErrQuotaExceeded) {
		t.Error("generic API error must not satisfy ErrQuotaExceeded")
	}
}

func TestOpenAICodexProvider_Advise_TransportError(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("connection refused")
	stub := stubRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, transportErr
	})

	p := NewOpenAICodexProviderWithHTTPClient(&http.Client{Transport: stub}, "gpt-codex")
	_, _, err := p.Advise(context.Background(), "system", "user", 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, transportErr) {
		t.Errorf("errors.Is(err, transportErr) = false, want true")
	}
	if errors.Is(err, einoproviders.ErrProviderAuth) {
		t.Error("transport error must not satisfy ErrProviderAuth")
	}
}
