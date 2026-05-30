# Plan: `NewChatModel()` Constructors for Claude, OpenAI, and Gemini

**Request:** `.agents/requests/local-symphony-tool-calling-chat-models.md`
**Consumer:** `github.com/mattsp1290/local-symphony`
**Target release:** v0.2.0 (§2 "Dual-shape API" from `docs/prompts/extract-eino-providers.md`)
**Priority:** High — blocks multi-backend integration in local-symphony

---

## What this plan delivers

Three new `NewChatModel()` constructors — one per backend — so local-symphony's provider
selector can call them directly and receive a `model.ToolCallingChatModel` for every supported
backend:

| Package | New symbol | Returns |
|---|---|---|
| `claude/` | `NewChatModel(ctx, ChatModelConfig)` | `(model.ToolCallingChatModel, error)` |
| `openai/` | `NewChatModel(ctx, ChatModelConfig)` | `(model.ToolCallingChatModel, error)` |
| `gemini/` | `NewChatModel(ctx, ChatModelConfig)` | `(model.ToolCallingChatModel, error)` |

Each is purely additive. The existing `Provider`/`Advise()` path in each sub-package is
unchanged. No new top-level symbols are added at the root package level; the request explicitly
says a formal `ChatModelFactory` interface is a nice-to-have, not required.

---

## Why this is straightforward

The underlying eino-ext adapters already satisfy `model.ToolCallingChatModel`:

```
// In cloudwego/eino-ext/components/model/claude@v0.1.17/claude.go:
var _ model.ToolCallingChatModel = (*ChatModel)(nil)

// In cloudwego/eino-ext/components/model/openai@v0.1.13/chatmodel.go:
var _ model.ToolCallingChatModel = (*ChatModel)(nil)

// In cloudwego/eino-ext/components/model/gemini@v0.1.30/gemini.go:
var _ model.ToolCallingChatModel = (*ChatModel)(nil)
```

Each of these adapters already implements `WithTools`, `BindTools`, `Generate`, and `Stream`.
The work here is:

1. Define a `ChatModelConfig` for each backend (thin projection of the eino-ext config).
2. Write a `NewChatModel` that validates required fields and delegates to the eino-ext adapter.
3. Add smoke tests (construction without panicking, tool binding compiles cleanly).

---

## Files to create / change

| File | Change |
|---|---|
| `claude/chatmodel.go` | **new** — `ChatModelConfig` struct + `NewChatModel` |
| `claude/chatmodel_test.go` | **new** — smoke tests |
| `openai/chatmodel.go` | **new** — `ChatModelConfig` struct + `NewChatModel` |
| `openai/chatmodel_test.go` | **new** — smoke tests |
| `gemini/chatmodel.go` | **new** — `ChatModelConfig` struct + `NewChatModel` |
| `gemini/chatmodel_test.go` | **new** — smoke tests |

No existing files are modified.

---

## What local-symphony confirmed

All three of Claude, OpenAI, and Gemini's eino-ext adapters support `BindTools` / `WithTools`
(confirmed via source inspection of the module cache). There are no backends that need to be
flagged as tool-calling-unsupported.
