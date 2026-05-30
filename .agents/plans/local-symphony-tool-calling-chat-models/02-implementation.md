# Implementation: Per-Backend NewChatModel Details

## claude/chatmodel.go

```go
package claude

import (
    "context"
    "fmt"
    "net/http"

    claudemodel "github.com/cloudwego/eino-ext/components/model/claude"
    "github.com/cloudwego/eino/components/model"

    einoproviders "github.com/mattsp1290/eino-providers"
)

// ChatModelConfig configures a Claude ToolCallingChatModel.
type ChatModelConfig struct {
    APIKey        string
    Model         string
    MaxTokens     int
    BaseURL       *string
    HTTPClient    *http.Client
    Temperature   *float32
    TopP          *float32
    TopK          *int32
    StopSequences []string
}

// NewChatModel constructs a Claude Eino ToolCallingChatModel.
func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error) {
    if cfg.Model == "" {
        return nil, einoproviders.WrapInitError(fmt.Errorf("claude: Model is required"))
    }
    if cfg.MaxTokens <= 0 {
        return nil, einoproviders.WrapInitError(fmt.Errorf("claude: MaxTokens must be > 0"))
    }
    cm, err := claudemodel.NewChatModel(ctx, &claudemodel.Config{
        APIKey:        cfg.APIKey,
        Model:         cfg.Model,
        MaxTokens:     cfg.MaxTokens,
        BaseURL:       cfg.BaseURL,
        HTTPClient:    cfg.HTTPClient,
        Temperature:   cfg.Temperature,
        TopP:          cfg.TopP,
        TopK:          cfg.TopK,
        StopSequences: cfg.StopSequences,
    })
    if err != nil {
        return nil, einoproviders.WrapInitError(fmt.Errorf("claude: build chat model %q: %w", cfg.Model, err))
    }
    return cm, nil
}
```

**Notes:**
- `claudemodel.NewChatModel` returns `*claudemodel.ChatModel`, which satisfies
  `model.ToolCallingChatModel` (confirmed by `var _ model.ToolCallingChatModel = (*ChatModel)(nil)`
  in the eino-ext source). No type assertion or adapter struct needed.
- `MaxTokens` validation: the Anthropic API rejects 0; surfacing it at construction time
  gives a clearer error than a downstream API error.
- No `APIKey` validation at our layer — the eino-ext adapter will return its own error if the
  key is missing or invalid. We don't duplicate that check. Exception: if we add Bedrock/Vertex
  support later, we'd add a "at least one auth mode configured" check.

---

## openai/chatmodel.go

```go
package openai

import (
    "context"
    "fmt"
    "net/http"
    "time"

    openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
    "github.com/cloudwego/eino/components/model"

    einoproviders "github.com/mattsp1290/eino-providers"
)

// ChatModelConfig configures an OpenAI ToolCallingChatModel.
type ChatModelConfig struct {
    APIKey              string
    Model               string
    BaseURL             string
    HTTPClient          *http.Client
    Timeout             time.Duration
    MaxCompletionTokens *int
    // MaxTokens is the legacy field understood by most OpenAI-compatible local
    // servers. Prefer MaxCompletionTokens for the OpenAI API proper.
    MaxTokens           *int
    Temperature         *float32
    TopP                *float32
    Stop                []string
}

// NewChatModel constructs an OpenAI Eino ToolCallingChatModel.
func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error) {
    if cfg.Model == "" {
        return nil, einoproviders.WrapInitError(fmt.Errorf("openai: Model is required"))
    }
    // Require APIKey only for the hosted OpenAI API. When BaseURL is set
    // (local/compatible server), the key may be empty or a placeholder.
    if cfg.APIKey == "" && cfg.BaseURL == "" {
        return nil, einoproviders.WrapInitError(fmt.Errorf("openai: APIKey is required when BaseURL is empty"))
    }
    cm, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
        APIKey:              cfg.APIKey,
        Model:               cfg.Model,
        BaseURL:             cfg.BaseURL,
        HTTPClient:          cfg.HTTPClient,
        Timeout:             cfg.Timeout,
        MaxCompletionTokens: cfg.MaxCompletionTokens,
        MaxTokens:           cfg.MaxTokens,
        Temperature:         cfg.Temperature,
        TopP:                cfg.TopP,
        Stop:                cfg.Stop,
    })
    if err != nil {
        return nil, einoproviders.WrapInitError(fmt.Errorf("openai: build chat model %q: %w", cfg.Model, err))
    }
    return cm, nil
}
```

**Notes:**
- `openaimodel.NewChatModel` returns `*openaimodel.ChatModel`, satisfying
  `model.ToolCallingChatModel`.
- `APIKey` is validated here because the eino-ext adapter may not surface a readable error
  for an empty key until the first request.
- `BaseURL` being an empty string (not `*string`) is intentional — it matches the eino-ext
  field type and the existing `openai.Provider` pattern in this repo.

---

## gemini/chatmodel.go

```go
package gemini

import (
    "context"
    "fmt"

    geminimodel "github.com/cloudwego/eino-ext/components/model/gemini"
    "github.com/cloudwego/eino/components/model"
    "google.golang.org/genai"

    einoproviders "github.com/mattsp1290/eino-providers"
)

// ChatModelConfig configures a Gemini ToolCallingChatModel.
type ChatModelConfig struct {
    APIKey      string
    Client      *genai.Client
    Model       string
    MaxTokens   *int
    Temperature *float32
    TopP        *float32
    TopK        *int32
}

// NewChatModel constructs a Gemini Eino ToolCallingChatModel.
func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error) {
    if cfg.Model == "" {
        return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: Model is required"))
    }
    client := cfg.Client
    if client == nil {
        // APIKey may be empty when env-var credentials are configured
        // (GEMINI_API_KEY, GOOGLE_API_KEY, ADC). genai.NewClient resolves
        // those itself; we don't eagerly reject here.
        var err error
        client, err = genai.NewClient(ctx, &genai.ClientConfig{APIKey: cfg.APIKey})
        if err != nil {
            return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: build client: %w", err))
        }
    }
    cm, err := geminimodel.NewChatModel(ctx, &geminimodel.Config{
        Client:      client,
        Model:       cfg.Model,
        MaxTokens:   cfg.MaxTokens,
        Temperature: cfg.Temperature,
        TopP:        cfg.TopP,
        TopK:        cfg.TopK,
    })
    if err != nil {
        return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: build chat model %q: %w", cfg.Model, err))
    }
    return cm, nil
}
```

**Notes:**
- `geminimodel.NewChatModel` signature is `(_ context.Context, cfg *Config) (*ChatModel, error)`
  (the context is unused in the eino-ext adapter, but we accept it for consistency with the
  other backends and forward it for the `genai.NewClient` call).
- `genai.Client` construction mirrors what the existing `gemini.Provider` does in `gemini.go`.
  We do not cache the client on a struct (unlike the Provider) because `NewChatModel` is a
  constructor — the returned `model.ToolCallingChatModel` is the thing that gets cached.
- `geminimodel.NewChatModel` ignores its context argument currently, so we document
  `ctx` as used only for `genai.NewClient`. This is safe — if the eino-ext adapter starts
  using ctx in a future version, it will still receive a valid one.
- The `APIKey == ""` case is forwarded to `genai.NewClient` rather than rejected. This allows
  callers relying on `GEMINI_API_KEY`, `GOOGLE_API_KEY`, or Application Default Credentials
  (ADC) to construct the model without explicitly setting `APIKey`.
- The existing `gemini.Provider` uses two constructors (`NewGeminiProvider`, `NewGeminiProviderWithClient`).
  The new `ChatModelConfig` consolidates both paths into one struct with `Client` taking precedence
  when non-nil. The precedence is documented in the field comment. This diverges from the
  Provider pattern intentionally — keyed structs compose better than positional helpers.

---

## Error wrapping consistency

All three implementations wrap construction errors with `einoproviders.WrapInitError`. This
means local-symphony can write:

```go
m, err := claude.NewChatModel(ctx, cfg)
if errors.Is(err, einoproviders.ErrProviderInit) {
    // log and fall back
}
```

The same works for openai and gemini. This is consistent with how `openaicodex.NewChatModel`
behaves.

---

## What does NOT need to change

- `claude/claude.go`, `openai/openai.go`, `gemini/gemini.go` — the existing `Provider` structs
  and `Advise()` methods are untouched.
- `go.mod` — all three eino-ext adapters are already direct dependencies.
- Registry — `NewChatModel` is a plain exported function, not a registry-based factory. No
  `init()` changes needed.
- Root `factory.go` — not involved.
