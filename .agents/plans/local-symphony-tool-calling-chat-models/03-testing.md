# Testing Strategy

## Goals

The acceptance criteria specify "at least a smoke test (can construct the model without
panicking; tool binding compiles cleanly)". We write more than that — the pattern from
`openaicodex/chatmodel_test.go` and `ollama/ollama_test.go` gives us a clear template.

For each backend we write:
1. Validation tests (required-field errors at construction time)
2. Construction success test (httptest server for claude/openai; mock genai.Client for gemini)
3. `WithTools` compiles and returns a valid model

We do NOT write live-wire tests as part of this plan (those are separate, `//go:build live` tagged).

---

## claude/chatmodel_test.go

`claudemodel.NewChatModel` constructs an `anthropic.Client` in memory — it makes no network
call. So the success test needs no httptest server: just pass a fake API key and assert
non-nil. An httptest server is only needed if the test calls `Generate`/`Stream`, which the
smoke tests do not.

**Test cases:**

```
TestNewChatModel_Validation
  - Model = ""            → error contains "Model", wraps ErrProviderInit
  - MaxTokens = 0         → error contains "MaxTokens", wraps ErrProviderInit

TestNewChatModel_Success
  - ChatModelConfig{APIKey: "fake", Model: "claude-sonnet-4-5", MaxTokens: 1024}
  - No network call needed; confirm returned value is non-nil

TestNewChatModel_WithTools
  - After construction, call WithTools([]*schema.ToolInfo{{Name:"noop", Desc:"noop"}})
  - No error, returned model is non-nil
  - Validates tool-binding compiles cleanly with no network call
```

If a future test wants to exercise `Generate` end-to-end, add an httptest server at that
point. Don't add dead scaffolding now.

---

## openai/chatmodel_test.go

`openaimodel.NewChatModel` constructs an `openai.Client` in memory — no network call at
construction time. Same reasoning as Claude.

**Test cases:**

```
TestNewChatModel_Validation
  - APIKey = ""   → error contains "APIKey", wraps ErrProviderInit
  - Model = ""    → error contains "Model", wraps ErrProviderInit

TestNewChatModel_Success
  - ChatModelConfig{APIKey: "fake", Model: "gpt-4o"}
  - No network call needed; confirm returned value is non-nil

TestNewChatModel_WithTools
  - WithTools([]*schema.ToolInfo{{Name:"noop", Desc:"noop"}}) succeeds, no network call
```

---

## gemini/chatmodel_test.go

Gemini is trickier: `genai.NewClient` makes no network call itself (it just initialises the
SDK client). `geminimodel.NewChatModel` also makes no network call. So the smoke test for
construction can pass a real (fake) `*genai.Client` by constructing it with a dummy API key
and then never actually generating — or by passing a pre-built `*genai.Client` via
`ChatModelConfig.Client`.

**Approach:** Use `cfg.Client` to bypass `genai.NewClient`. Construct the client with
`Backend: genai.BackendGeminiAPI` to pin it away from the Vertex AI branch — otherwise a
`GOOGLE_GENAI_USE_VERTEXAI=true` env var in CI can redirect `genai.NewClient` to the Vertex
path and trigger ADC credential detection (a network call):
```go
// In the test — pin Backend to avoid Vertex branch from env-var.
client, _ := genai.NewClient(ctx, &genai.ClientConfig{
    APIKey:  "fake",
    Backend: genai.BackendGeminiAPI,
})
cm, err := gemini.NewChatModel(ctx, gemini.ChatModelConfig{
    Client: client,
    Model:  "gemini-2.0-flash",
})
```

**Test cases:**

```
TestNewChatModel_Validation
  - Model = "", Client = pre-built → error contains "Model", wraps ErrProviderInit

TestNewChatModel_Success_WithClient
  - Constructs with a pre-built genai.Client (fake API key, pinned backend)
  - Result is non-nil, implements model.ToolCallingChatModel

TestNewChatModel_WithTools
  - WithTools([]*schema.ToolInfo{{Name:"noop", Desc:"noop"}}) on a constructed model
  - No error, returned model non-nil
```

We explicitly do NOT test the `APIKey`→`genai.NewClient` path in unit tests (that path may
make a network call depending on the credential environment). This is consistent with the
existing `gemini/gemini_test.go` approach.

---

## Test file placement

Tests live inside the same package as the implementation (white-box, `package claude` not
`package claude_test`). This matches all existing test files in the repo.

---

## go vet and build tags

All tests must pass `go vet ./...` and `go test ./...` with no build tags (no live server
required). Live-wire tests, if added later, go in separate `_live_test.go` files with
`//go:build live`.
