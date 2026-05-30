# API Design: ChatModelConfig Structs and Constructors

## Design principles

1. **Minimal surface** — expose only what the eino-ext adapter requires or what callers
   (local-symphony, advisor) have confirmed they need. Exotic options (Bedrock, Vertex, Azure,
   modalities, audio, cache) are intentionally omitted; they can be added as optional fields in
   minor releases without breaking keyed-struct callers.

2. **Keyed-struct stability** — all `ChatModelConfig` structs use exported fields so callers
   write `ChatModelConfig{APIKey: "x", Model: "y"}`. Adding a field later is source-compatible.
   No positional constructors.

3. **Consistent error wrapping** — required-field violations use `einoproviders.WrapInitError`.
   This lets callers use `errors.Is(err, einoproviders.ErrProviderInit)` for all three backends.

4. **HTTPClient escape hatch** — Claude and OpenAI accept a `*http.Client` so tests can inject
   an httptest server. Gemini takes a `*genai.Client` (not `*http.Client`), so the test
   escape hatch is `ChatModelConfig.Client` with a pre-built `genai.Client` using a fake key.

---

## claude/chatmodel.go

```go
// ChatModelConfig configures a Claude ToolCallingChatModel.
type ChatModelConfig struct {
    // APIKey is the Anthropic API key.
    // Required unless HTTPClient is set with a pre-authenticated transport.
    APIKey string

    // Model is the Claude model slug, e.g. "claude-sonnet-4-5".
    // Required.
    Model string

    // MaxTokens is the maximum number of tokens the model may generate.
    // Required (Claude rejects requests with MaxTokens = 0).
    MaxTokens int

    // BaseURL overrides the Anthropic API endpoint. Useful for proxies.
    // Optional.
    BaseURL *string

    // HTTPClient, when non-nil, is used instead of the default transport.
    // Useful for tests and proxy injection.
    // Optional.
    HTTPClient *http.Client

    // Temperature controls output randomness [0.0, 1.0].
    // Optional.
    Temperature *float32

    // TopP controls nucleus sampling [0.0, 1.0].
    // Optional.
    TopP *float32

    // TopK limits token selection.
    // Optional.
    TopK *int32

    // StopSequences are custom stop sequences.
    // Optional.
    StopSequences []string
}

func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error)
```

**Validation required:** `Model != ""`, `MaxTokens > 0`.

**Delegation:** Maps 1:1 to `claudemodel.Config` fields; passes result through directly
(no wrapping type).

---

## openai/chatmodel.go

```go
// ChatModelConfig configures an OpenAI ToolCallingChatModel.
type ChatModelConfig struct {
    // APIKey is the OpenAI API key.
    // Required.
    APIKey string

    // Model is the model ID, e.g. "gpt-4o" or "o3".
    // Required.
    Model string

    // BaseURL overrides the OpenAI API endpoint. Use for OpenAI-compatible
    // servers (e.g. local inference endpoints).
    // Optional.
    BaseURL string

    // HTTPClient, when non-nil, is used instead of the default transport.
    // Optional.
    HTTPClient *http.Client

    // Timeout specifies the maximum duration to wait for API responses.
    // Ignored when HTTPClient is non-nil (the client's transport owns timeout).
    // Optional.
    Timeout time.Duration

    // MaxCompletionTokens caps generation including reasoning tokens.
    // Optional.
    MaxCompletionTokens *int

    // Temperature controls output randomness [0.0, 2.0].
    // Optional.
    Temperature *float32

    // TopP controls nucleus sampling [0.0, 1.0].
    // Optional.
    TopP *float32

    // Stop sequences where generation halts.
    // Optional.
    Stop []string

    // MaxTokens is the legacy max_tokens field, understood by most
    // OpenAI-compatible local inference servers (vLLM, llama.cpp, LM Studio).
    // Use MaxCompletionTokens for OpenAI API (o1/o3 series).
    // Optional. Deprecated for OpenAI API proper.
    MaxTokens *int
}

func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error)
```

**Validation required:** `Model != ""`. `APIKey` is required unless `BaseURL` is non-empty
(local/compatible servers commonly accept any or no key). When `BaseURL != ""` and
`APIKey == ""`, construction proceeds and the API-level auth error (if any) surfaces on the
first request.

**Delegation:** Maps to `openaimodel.ChatModelConfig`. The eino-ext adapter handles Azure;
we don't expose Azure fields. If a caller needs Azure, they construct via the eino-ext package
directly.

---

## gemini/chatmodel.go

Gemini is slightly different because the eino-ext adapter takes a `*genai.Client` (not an API
key). The existing `gemini.Provider` already caches the `genai.Client` — we follow the same
pattern.

```go
// ChatModelConfig configures a Gemini ToolCallingChatModel.
type ChatModelConfig struct {
    // APIKey is the Gemini API key used to build a genai.Client when Client
    // is nil. When Client is nil and APIKey is empty, genai.NewClient will
    // fall back to environment-variable credentials (GEMINI_API_KEY,
    // GOOGLE_API_KEY, or ADC). Returning a construction error is left to
    // genai.NewClient in that case.
    // Required when Client is nil and no env-var credentials are configured.
    APIKey string

    // Client is an existing genai.Client. When non-nil, APIKey is ignored.
    // Optional.
    Client *genai.Client

    // Model is the Gemini model slug, e.g. "gemini-2.0-flash".
    // Required.
    Model string

    // MaxTokens limits generation length.
    // Optional.
    MaxTokens *int

    // Temperature controls output randomness [0.0, 1.0].
    // Optional.
    Temperature *float32

    // TopP controls nucleus sampling [0.0, 1.0].
    // Optional.
    TopP *float32

    // TopK limits token selection.
    // Optional.
    TopK *int32
}

func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error)
```

**Validation required:** `Model != ""`. The `APIKey == "" && Client == nil` case is forwarded
to `genai.NewClient` (which resolves env-var credentials) rather than rejected eagerly.

**Delegation:**
1. If `cfg.Client == nil`, call `genai.NewClient(ctx, &genai.ClientConfig{APIKey: cfg.APIKey})`.
2. Call `geminimodel.NewChatModel(ctx, &geminimodel.Config{Client: client, Model: cfg.Model, ...})`.

---

## Naming note: `ChatModelConfig` vs `Config`

The existing `Provider` constructors in each sub-package use positional arguments (e.g.
`NewClaudeProvider(apiKey, model string)`). The new `ChatModelConfig` struct is keyed and follows
the pattern established by `openaicodex.ChatModelConfig` and `ollama.Config`. We use
`ChatModelConfig` (not `Config`) to avoid shadowing the eino-ext adapter's own `Config` type
inside the same package — particularly in `claude/` and `gemini/` where the eino-ext import
alias (`claudemodel`, `geminimodel`) makes the package-level `Config` name available. Using
`ChatModelConfig` is unambiguous regardless of import alias.
