# eino-providers

`eino-providers` is a shared Go module for constructing CloudWeGo Eino chat model providers across Claude, OpenAI, OpenAI-Codex, Gemini, and Ollama backends.

```go
package main

import (
	"context"
	"fmt"
	"log"

	einoproviders "github.com/mattsp1290/eino-providers"
	_ "github.com/mattsp1290/eino-providers/claude"
)

func main() {
	ctx := context.Background()

	provider, err := einoproviders.NewProvider(ctx, "claude", "claude-sonnet-4-5", einoproviders.Options{
		APIKey: "your-api-key",
	})
	if err != nil {
		log.Fatal(err)
	}

	text, usage, err := provider.Advise(ctx, "Be concise.", "Summarize Eino.", 512)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s\n%+v\n", text, usage)
}
```

The first consumers are [advisor](https://github.com/mattsp1290/advisor) and [local-symphony](https://github.com/mattsp1290/local-symphony).

## Backend Notes

Backends are opt-in packages. Import the package for each provider name before
calling `NewProvider`; the root package does not import SDKs or Codex OAuth.

| Provider | Import | Provider name | Notes |
| --- | --- | --- | --- |
| Claude | `github.com/mattsp1290/eino-providers/claude` | `claude` | Uses `Options.APIKey`; forwards `Options.BaseURL` when non-nil; maps `Advise` `maxTokens` to Claude `MaxTokens`. |
| OpenAI | `github.com/mattsp1290/eino-providers/openai` | `openai` | Uses `Options.APIKey`; forwards `Options.BaseURL` when non-nil; maps `Advise` `maxTokens` to OpenAI `MaxCompletionTokens`. |
| OpenAI-Codex | `github.com/mattsp1290/eino-providers/openaicodex` | `openai-codex` | Uses `codex-auth-go` for OAuth transport; ignores `Options.APIKey`; rejects non-nil `Options.BaseURL` and `Options.MaxTokens`; does not set Eino max-token fields. |
| Gemini | `github.com/mattsp1290/eino-providers/gemini` | `gemini` | Uses `Options.APIKey` unless `Options.GenaiClient` is supplied; caches the `genai.Client` at provider construction; maps `Advise` `maxTokens` to Gemini `MaxTokens`. |

The Ollama package exposes `ollama.NewChatModel` rather than a root
`Provider` registration. It validates and pings `BaseURL` with `GET /api/tags`
before constructing the Eino Ollama chat model. Use `KeepAlive` for Ollama
model residency, including `"-1"` for indefinite residency.

OpenAI-Codex's single-shot `Provider.Advise` path sets a non-secret placeholder
API key only to satisfy the Eino OpenAI adapter constructor. The Codex HTTP
transport owns endpoint rewriting, Bearer-token injection, and token refresh.
Plan and quota API responses are classified as `ErrProviderAuth` and preserve
the corresponding `codex-auth-go` sentinels for `errors.Is`.

The `openaicodex` package also exposes `openaicodex.NewChatModel` /
`NewChatModelWithHTTPClient` (returning `model.ToolCallingChatModel`): a native
**Responses-API** streaming, tool-calling chat model for agent loops. It speaks
the Codex Responses wire protocol directly (not Chat Completions), supports
`Generate`, `Stream`, and `WithTools` (immutable, concurrency-safe), threads
reasoning across turns for multi-turn tool calling, and preserves the same
auth/plan/quota error sentinels. See `openaicodex/examples/toolcall` for a
verified 2-turn tool-calling example.

## API Stability

The v0.1.x public API is intentionally small:

- `Provider`, `Usage`, `Options`, `NewProvider`, `RegisterProvider`, error
  sentinels, `Classify`, and wrapper helpers in the root package.
- Backend constructors documented by their package comments and exported
  functions.
- `ollama.NewChatModel` and `ollama.Config`.
- `openaicodex.NewChatModel`, `openaicodex.NewChatModelWithHTTPClient`, and
  `openaicodex.ChatModelConfig`.

Use keyed struct literals for `Options` and backend config types:

```go
einoproviders.Options{
	APIKey: "your-api-key",
}
```

Unkeyed `Options` literals are not supported. New optional fields may be added
in minor releases when they preserve zero-value behavior for existing keyed
literals.

`RegisterProvider` exists for backend package self-registration and advanced
tests. Treat direct third-party registration as experimental until the registry
contract is explicitly stabilized.

The planned v0.2.0 direction is a `ChatModelFactory` surface for consumers that
need reusable Eino chat models rather than the current single-shot `Provider`
contract. The current `Provider.Advise` API remains the v0.1.x compatibility
surface while that factory design is validated in advisor and local-symphony.
