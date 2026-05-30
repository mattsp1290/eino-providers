# Conventions, Edge Cases, and Open Questions

## Conventions to follow

### From ollama/openaicodex (the reference implementations)

1. **Package prefix in errors:** Every error message starts with `"packagename: "` — e.g.
   `"claude: Model is required"`, `"openai: APIKey is required"`.
   
2. **WrapInitError on all construction-time failures:** Any error that returns before a
   working `model.ToolCallingChatModel` is produced should be wrapped with
   `einoproviders.WrapInitError(...)`. Downstream API errors (returned via `Generate`/`Stream`)
   are NOT wrapped — they come from the eino-ext adapter directly.

3. **Keyed struct, no positional helper:** Do not add `NewChatModelFromAPIKey(key, model string)`
   convenience helpers. The keyed struct is the API. Positional helpers rot.

4. **APIKey validation is conditional for OpenAI, absent for Claude/Gemini:**
   - Claude: no eager `APIKey` check — the eino-ext adapter surfaces auth failures on the first
     request. Claude's adapter does not require a key at construction time (e.g., for custom
     transports or proxy setups).
   - OpenAI: validate `APIKey != ""` only when `BaseURL == ""`. When `BaseURL` is set, the
     caller is using a local/compatible server that commonly accepts any or no key. Rejecting an
     empty key in that case blocks a documented and common use case.
   - Gemini: no eager `APIKey` check — `genai.NewClient` resolves credentials from env vars
     (`GEMINI_API_KEY`, `GOOGLE_API_KEY`, ADC) when `APIKey` is empty. Validating eagerly would
     defeat that fallback.

5. **No model slug validation:** We do not validate model slug strings (e.g. rejecting
   `"not-a-real-model"`). The API will return an appropriate error. This matches all existing
   backends.

6. **Return the eino-ext type as the interface:** Do not create a wrapper struct. The eino-ext
   `*ChatModel` is already the concrete type; returning it as `model.ToolCallingChatModel` is
   idiomatic Go.

---

## Runtime error classification: explicit non-goal

`WrapInitError` is used only for construction-time failures. Errors returned from
`Generate`/`Stream` (i.e., after a `model.ToolCallingChatModel` is in hand) pass through
from the eino-ext adapter as-is — they are NOT classified into `ErrProviderAPI`,
`ErrProviderAuth`, `ErrProviderTimeout`, or `ErrBackendUnreachable`.

This is a deliberate scope decision for this change. `openaicodex.chatModel` does classify
runtime errors via `classifyResponsesError` because it owns the HTTP layer. The three new
backends delegate entirely to the eino-ext adapters' own HTTP layers, which return their own
error types.

**Impact on local-symphony:** The ReAct loop cannot use `errors.Is(err, ErrProviderAuth)` on
`Generate`/`Stream` errors from these backends. This is a known gap. A v0.3.0 issue should be
filed to add runtime error wrapping for each backend once the transport layer is understood.

## System prompt handling: intentionally absent from config

System prompts are NOT a field in any `ChatModelConfig`. In Eino, system prompts ride in the
`[]*schema.Message` slice passed to `Generate`/`Stream` as `schema.SystemMessage(...)` entries.
The eino-ext adapters for all three backends handle system messages in their input processing.
This matches the openaicodex pattern where `messagesToInput` folds system messages into
`instructions`.

## Edge cases

### Claude: MaxTokens = 0
The Anthropic API will reject a request with `max_tokens: 0` at the API layer. We validate
eagerly to surface it at construction time with a readable error rather than a confusing API
error response.

### OpenAI: Timeout vs HTTPClient
The eino-ext adapter ignores `Timeout` when `HTTPClient` is non-nil (it documents this). Our
`ChatModelConfig` follows the same rule. We note it in the field comment but do not validate
(no error if both are set — the caller's HTTPClient takes precedence, same as the adapter).

### Gemini: genai.Client construction in tests
`genai.NewClient` with a fake API key does not validate the key and makes no network calls.
The test helper `func newFakeGeminiClient(t *testing.T) *genai.Client` can safely call
`genai.NewClient(ctx, &genai.ClientConfig{APIKey: "fake"})`. If Google ever changes this to
do a live check, tests will break — at that point, add a `//go:build !live` guard and a mock.

### Gemini: context passed to geminimodel.NewChatModel
The eino-ext adapter currently ignores its context argument (`_ context.Context`). We forward
`ctx` anyway for forward-compatibility. This is safe and costs nothing.

---

## Open questions (resolved)

**Q: Should we expose a `ChatModelFactory` interface at the root package?**
A: Not in this change. The request explicitly says "a plain exported `NewChatModel()` function
per backend unblocks us immediately without needing the interface." Formalise in v0.2.0 when
both consumers have confirmed the shape works.

**Q: Do any of Claude/OpenAI/Gemini fail to support tool calling for some models?**
A: The eino-ext adapters all implement `WithTools` and `BindTools`. Whether a specific model
slug (e.g. an older Gemini model) supports function calling is determined by the API at runtime.
We don't gate on model slug.

**Q: Should Gemini's validation reject `APIKey == "" && Client == nil`?**
A: No. `genai.NewClient` resolves credentials from environment variables (`GEMINI_API_KEY`,
`GOOGLE_API_KEY`, Application Default Credentials). An eager reject would block callers using
env-var auth. We forward the empty key and let `genai.NewClient` return its own credential
error if no credentials are found.

**Q: Should we add a `NewChatModelWithHTTPClient` helper for claude and openai (matching openaicodex)?**
A: No — the `HTTPClient` field in `ChatModelConfig` serves the same purpose. A separate
constructor is only justified when the primary path requires a complex auth flow (as it does
for openaicodex). Plain API-key backends don't need it.

**Q: Should Gemini's `ChatModelConfig` include `ThinkingConfig`?**
A: Not in the initial implementation. The field exists in the eino-ext `Config` but is niche.
Add when a consumer asks for it.

---

## Acceptance criteria checklist

- [ ] `claude.NewChatModel(ctx, ChatModelConfig)` returns `(model.ToolCallingChatModel, error)`
- [ ] `openai.NewChatModel(ctx, ChatModelConfig)` returns `(model.ToolCallingChatModel, error)`
- [ ] `gemini.NewChatModel(ctx, ChatModelConfig)` returns `(model.ToolCallingChatModel, error)`
- [ ] `go vet ./...` passes clean
- [ ] `go test ./...` passes clean (all smoke tests pass without a live server)
- [ ] Tool binding compiles: `WithTools([]*schema.ToolInfo{...})` on the returned model compiles
      and returns non-nil
- [ ] Keyed struct literals used for `ChatModelConfig`
- [ ] Existing `Provider`/`Advise()` path in each sub-package untouched
- [ ] Confirmed: Claude, OpenAI, and Gemini eino-ext adapters all implement `WithTools`/`BindTools`
      (no backends need to be flagged as tool-calling-unsupported)
- [ ] Timeout/Gemini note: callers can set timeout via `cfg.HTTPClient` for Claude/OpenAI;
      for Gemini the caller passes a pre-configured `*genai.Client`
