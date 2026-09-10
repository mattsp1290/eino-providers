# eino-providers

`eino-providers` is a shared Go module for constructing CloudWeGo Eino chat model providers across Claude, OpenAI, OpenAI-Codex, OpenCode Go, Gemini, and Ollama backends.

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
| OpenCode Go | `github.com/mattsp1290/eino-providers/opencodego` | `opencode-go` | Requires an explicit model, native protocol, caller user agent, API key, and request session; also exposes a direct tool-calling chat model. |
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

## OpenCode Go

Import `github.com/mattsp1290/eino-providers/opencodego` to register the
`opencode-go` provider or to call `opencodego.NewChatModel` directly. Model and
protocol selection belong to the caller. The package does not discover models,
infer capabilities, or fall back between the native protocols
`chat-completions`, `messages`, and `responses`.

The registered provider uses the root options surface:

```go
import (
    "context"

    einoproviders "github.com/mattsp1290/eino-providers"
    "github.com/mattsp1290/eino-providers/opencodego"
)

provider, err := einoproviders.NewProvider(ctx, "opencode-go", modelID,
    einoproviders.Options{
        Protocol:  string(opencodego.ProtocolResponses),
        UserAgent: "my-coding-agent/1.0",
        SessionID: conversationID,
    })
```

A nonempty `Options.APIKey` wins; an empty value reads
`OPENCODE_GO_API_KEY`. The backend does not inspect login files or key stores.
`UserAgent` must identify the host coding agent. Every request also needs a
valid conversation ID. `SessionID` is the configured fallback, while a value
attached to the operation context takes precedence:

```go
import opencodeauth "github.com/mattsp1290/opencode-auth-go"

requestCtx := opencodeauth.WithSessionID(ctx, conversationID)
message, err := chatModel.Generate(requestCtx, input)
```

The library does not create, persist, or print session IDs. An explicitly empty
context override is invalid and does not fall back to the configured value.

`BaseURL` is the API root. The backend appends `/chat/completions`, `/messages`,
or `/responses`; do not pass a complete inference endpoint. Nil uses the
authentication library default. `HTTPClient` is a trusted, unauthenticated base
client: the authentication library copies it, preserves settings such as its
timeout and transport, and decorates the copy with credentials, caller identity,
session headers, and redirect protection. Its transport can therefore observe
injected credentials, and its timeout also bounds streaming.

For a reusable `model.ToolCallingChatModel`, construct the model directly:

```go
cap := 512
chatModel, err := opencodego.NewChatModel(ctx, opencodego.ChatModelConfig{
    Model:      modelID,
    Protocol:   opencodego.ProtocolMessages,
    UserAgent:  "my-coding-agent/1.0",
    SessionID:  conversationID,
    MaxTokens:  &cap,
})
```

A direct Messages model requires a positive configured `MaxTokens`. The
registered provider may omit it because each positive `Advise` cap overrides
the configuration. Chat Completions and Responses permit a nil cap.

Across the three protocols, the supported surface is text generation,
streaming, function calls and tool results, and usage metadata. Responses also
replays encrypted reasoning items across a stateless tool loop. Multimodal
inputs, server-hosted built-in tools, `previous_response_id` state, unsupported
roles, and unsupported invocation options are outside this package's contract.
See [`opencodego/examples/toolcall`](opencodego/examples/toolcall) for a bounded
two-turn streaming function-tool exchange.

Messages uses the pinned Anthropic SDK retry policy: two retries, for at most
three replayable attempts, on retryable statuses such as 408, 409, 429, and
5xx. `X-Should-Retry` directives and `Retry-After` are honored; permanent 4xx
responses normally receive one attempt. The authentication transport does not
add retries, and streams are never retried after they start. Cancellation stops
a subsequent attempt, although the pinned SDK can remain in its current
`time.Sleep` backoff before returning.

Errors retain stable classification and safe HTTP metadata without exposing
response bodies, credentials, sessions, or request URLs in their strings:

```go
if errors.Is(err, einoproviders.ErrProviderAuth) ||
    errors.Is(err, einoproviders.ErrProviderAPI) ||
    errors.Is(err, einoproviders.ErrProviderTimeout) {
    var httpErr *opencodeauth.HTTPError
    if errors.As(err, &httpErr) {
        // Inspect StatusCode, Kind, RetryAfter, and HasRetryAfter.
    }
}
```

The optional live gate has not been run as part of ordinary validation. It
requires explicit credentials and model choices for all three protocols:

```bash
OPENCODE_GO_LIVE_TEST=1 go test ./opencodego -run '^TestLiveOpenCodeGo$' -count=1 -timeout=6m
```

Roll out the backend by importing `opencodego` and selecting `opencode-go`
explicitly in the host. Roll back by reverting that host selection and the
provider dependency; no compatibility flag or migration is required because
there are no existing OpenCode Go consumers.

## API Stability

The v0.1.x public API is intentionally small:

- `Provider`, `Usage`, `Options`, `NewProvider`, `RegisterProvider`, error
  sentinels, `Classify`, and wrapper helpers in the root package.
- Backend constructors documented by their package comments and exported
  functions.
- `ollama.NewChatModel` and `ollama.Config`.
- `openaicodex.NewChatModel`, `openaicodex.NewChatModelWithHTTPClient`, and
  `openaicodex.ChatModelConfig`.
- `opencodego.NewChatModel`, `opencodego.ChatModelConfig`, and the
  `opencodego.Protocol*` constants.

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

The current `Provider.Advise` API remains the v0.1.x compatibility surface.
Backend-specific reusable chat-model constructors are available where listed
above; a uniform root-level factory remains a possible later API.
