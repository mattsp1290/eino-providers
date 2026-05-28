# Request: a streaming, tool-calling `openai-codex` ChatModel (subscription-backed)

**Status:** request from a downstream consumer. This describes *what we need to be
successful*; it intentionally does **not** dictate *how* to implement it. Design,
file layout, and wire handling are yours.

**TL;DR / definition of done:** add an Eino `ToolCallingChatModel` for the `openai-codex`
backend (subscription-authenticated via `codex-auth-go`), with both `Generate` and
`Stream` working and multi-turn tool calling verified **on the live wire**, demonstrated
by a 2-turn tool-calling example committed to this repo.

## Who is asking and why

A new consumer — an **AG-UI protocol server** (Go + Fiber v3) at
`github.com/mattsp1290/ag-ui-go-server-example` — runs a multi-turn **agent loop**
(ReAct style: model → tool calls → tool results → model → … → final answer) and streams
the results to a UI over Server-Sent Events. The agent's only tool today is a read-only
`file_read` (from `eino-tools/fileops`); the loop streams assistant text token-by-token
and surfaces tool calls as they happen.

We want this loop driven by the user's **ChatGPT/OpenAI subscription** (the `openai-codex`
backend), not a metered API key.

## The gap

`eino-providers` today gives the `openai-codex` backend only the single-shot
`Provider.Advise(ctx, system, user, maxTokens)` surface. That is not enough for an agent
loop, which needs:

- **streaming** (incremental assistant output, not one blocking string), and
- **tool calling across multiple turns** (the model proposes tool calls; we execute them
  and feed `role=tool` results back; the model continues).

There is also a correctness concern we want closed, not papered over. The current
`openaicodex` path constructs the Eino **OpenAI (Chat Completions)** adapter behind the
`codex-auth-go` transport, and that transport rewrites every request to the **Responses
API** endpoint (`chatgpt.com/backend-api/codex/responses`). Chat Completions request/stream
shapes and Responses API shapes are different. Two things compound this: ADR-0007 ("Defer
Raw Codex ChatModel") notes that advisor already has a private, Eino-coupled raw
Responses-API ChatModel and defers lifting it; and the current `openaicodex` tests
exercise a **mocked** HTTP client (via the `codexHTTPClient` seam), so there is no
on-the-wire verification of the existing path. We therefore cannot assume the existing
path drives real streaming/tool-calling inference over the subscription. We need a path
that is verified end-to-end on the wire.

## What we need

A **reusable Eino chat model** for the `openai-codex` backend that satisfies Eino's
streaming + tool-calling contract — i.e. is usable as
`github.com/cloudwego/eino/components/model.ToolCallingChatModel`:

```go
type ToolCallingChatModel interface {
    Generate(ctx, []*schema.Message, ...Option) (*schema.Message, error)
    Stream(ctx, []*schema.Message, ...Option) (*schema.StreamReader[*schema.Message], error)
    WithTools(tools []*schema.ToolInfo) (ToolCallingChatModel, error)
}
```

This matches the `ChatModelFactory`-shaped `NewChatModel(ctx, Config) (model.ToolCallingChatModel, error)`
the `ollama` package already exposes and that the README names as the v0.2.0 direction.
We do not require a specific name or signature — `ollama.NewChatModel` is a fine precedent
to mirror, but that is your call.

**Concurrency.** The consumer constructs one ChatModel and shares it across concurrent
requests, calling `WithTools(...)` per request/loop. As the `ToolCallingChatModel`
contract requires, `WithTools` must return a new, independent instance without mutating
the receiver, so concurrent requests with different tool sets don't interfere.

### Authentication

- Authenticated by the ChatGPT subscription via `codex-auth-go` (OAuth + automatic token
  refresh), exactly as the existing `openaicodex` provider obtains its client.
- The consumer should be able to either let the package build the authenticated client
  (e.g. from an `AppName`) **or** supply an already-authenticated `*http.Client`
  (mirrors the existing `NewOpenAICodexProvider` / `...WithHTTPClient` split). Surfacing
  `codexauth.ErrNotLoggedIn` so the consumer can prompt for login is important.

### Behavior that must work on the wire (this is the crux)

Both `Generate` and `Stream` must function. The agent-loop consumer drives the model via
`Stream`, so streaming is the on-the-wire must-have; `Generate` correctness may be covered
by unit tests. Test against whatever model the codex subscription exposes (match what the
`codex` CLI uses by default), and record the tested model id in the example's README so we
can match it on our side.

With a logged-in subscription session, against the live endpoint:

1. **Streaming text.** `Stream(...)` yields incremental `*schema.Message` chunks whose
   `Content` carries assistant text deltas, terminating with `io.EOF`. An in-flight
   `Stream` must abort cleanly when the request `ctx` is canceled — the consumer streams
   to a UI over SSE and cancels on client disconnect.
2. **Tool calling, multi-turn.** When tools are bound via `WithTools(...)` and the model
   decides to call one, the streamed (and/or `schema.ConcatMessages`-merged) assistant
   message exposes `ToolCalls` with a stable id, function `Name`, and JSON `Arguments`.
   After the consumer appends a `schema.ToolMessage(result, toolCallID)` and calls
   `Stream`/`Generate` again, the conversation continues correctly (the model sees the
   tool result and proceeds). This must hold for at least two tool-calling turns.
3. **Reasoning (optional, non-blocking).** Reasoning/thinking content is not required. But
   if it is surfaced at all, it must land on `schema.Message.ReasoningContent` (not a
   parallel field), streamed incrementally if possible. Our consumer maps it to AG-UI
   `REASONING_*` events when present and omits them when absent, so partial or no support
   is fine.

### Preserve existing contracts

- Token usage on `schema.Message.ResponseMeta.Usage` when the endpoint provides it (we
  read it via `einoproviders.ExtractUsage`).
- Keep the auth/plan/quota error classification: `ErrProviderAuth` and the
  `codex-auth-go` sentinels (`ErrPlanNotIncluded`, `ErrQuotaExceeded`, `ErrNotLoggedIn`)
  must remain reachable via `errors.Is` (see `classifyCodexError`).
- The existing `Provider`/`Options` registry path rejects `Options.BaseURL` and
  `Options.MaxTokens` for codex (the transport owns the endpoint; the endpoint manages
  output length) — keep that. The new ChatModel surface should preserve the same semantics
  (no caller-supplied base URL, no caller output-token cap) whether it takes an `Options`
  or its own `Config`. ADR-0006's dummy-API-key trick (a placeholder key to satisfy the
  Eino OpenAI adapter constructor) is fine if you keep that adapter, but only if items 1–2
  above genuinely work on the wire.

## Acceptance criteria

A reviewer (or our team) can confirm success by:

- **An example program in the repo** (e.g. under the codex package's `examples/`, like the
  existing OpenAI examples) that, given a logged-in session, runs a **2-turn tool-calling
  exchange** over the subscription: bind one tool, `Stream`, observe a tool call, feed back
  a `role=tool` result, `Stream` again, and print the final streamed answer. It should also
  demonstrate token-by-token streaming on the final turn.
- That example uses **only** the public surface of `eino-providers` (plus a `codex` login),
  with no consumer-private code.
- Existing `openaicodex` behavior and tests continue to pass; new behavior is covered by
  tests (mocked transport is acceptable for unit tests, but the example above is the
  on-the-wire proof).

An illustrative (non-binding) sketch of how our server would consume it:

```go
// shape is illustrative only — final API is yours
cm, err := openaicodex.NewChatModel(ctx, openaicodex.Config{AppName: "ag-ui-go-server-example", Model: "..."})
// or: cm, err := openaicodex.NewChatModelWithHTTPClient(ctx, authedClient, "...")
cm, _ = cm.WithTools([]*schema.ToolInfo{fileReadInfo})

stream, _ := cm.Stream(ctx, msgs)         // incremental assistant text
// ... on tool call: execute file_read, append schema.ToolMessage(result, id), Stream again ...
```

## Explicitly NOT dictating

- Whether you build a **native Responses-API ChatModel**, lift advisor's existing raw
  Responses-API wrapper into `openaicodex`, keep the Eino OpenAI adapter if it can be made
  to work, or take another route. Any approach that satisfies the acceptance criteria is
  welcome. (ADR-0007 left this open; this request is the consumer demand that may justify
  resolving it.)
- File/package layout, config struct shape, and exact exported names.
- Versioning/release mechanics. We can consume an unreleased commit via a local `replace`
  during integration, so you do not need a tagged release to unblock us.

## Out of scope for you

- The AG-UI event mapping, the Fiber server, the `file_read` tool wiring, and the agent
  loop itself — all of that lives in the consumer. We only need the ChatModel described above.

---
*Filed by the `ag-ui-go-server-example` author. Questions can come back to that repo; we
will integrate against whatever public surface you land and report whether the on-the-wire
streaming + tool-calling behavior meets the criteria above.*
