# Plan: Streaming, Tool-Calling `openai-codex` ChatModel (Responses API)

Tracking bead: `eino-providers-717`
Request: `docs/prompts/openai-codex-streaming-toolcalling-chatmodel.md`
Consumer: `github.com/mattsp1290/ag-ui-go-server-example` (AG-UI server, ReAct agent loop over SSE)

## 1. Problem & decision

The existing `openaicodex.Provider.Advise` path constructs the Eino **OpenAI Chat
Completions** adapter behind the `codex-auth-go` transport. That transport rewrites
**every** request to the **Responses API** endpoint
(`https://chatgpt.com/backend-api/codex/responses`, see `codexauth.CodexEndpoint`).
Chat Completions request/stream shapes ≠ Responses API shapes, and the current tests
mock the round-tripper, so the existing path is **unverified on the wire** and almost
certainly wrong (Chat Completions body POSTed to a Responses endpoint).

**Decision: build a native Responses-API `model.ToolCallingChatModel`** in the
`openaicodex` package. Do **not** reuse the Eino OpenAI adapter for this surface.
This resolves ADR-0007 (which deferred lifting advisor's private raw Responses-API
wrapper) in favor of a public, on-the-wire-verified implementation.

`Provider.Advise` and its existing tests are **left untouched** (see §9) to satisfy
the acceptance criterion "existing `openaicodex` behavior and tests continue to pass."

## 2. Authoritative wire shape (from `openai/codex` Rust source)

Grounded in the codex CLI source (not guessed), fetched 2026-05-28:
- `codex-rs/core/src/client.rs` → `build_responses_request` (request fields)
- `codex-rs/codex-api/src/requests/responses.rs` → `attach_item_ids`
- `codex-rs/codex-api/src/sse/responses.rs` → `process_responses_event`, event struct
- `codex-rs/protocol/src/models.rs` → `ResponseItem`, `ContentItem`
- `codex-rs/tools/src/responses_api.rs` → tool JSON shape

### 2.1 Request body (POST, the transport overwrites URL+auth; we POST to `codexauth.CodexEndpoint`)

```jsonc
{
  "model": "gpt-5.5",
  "instructions": "<concatenated system text>",
  "input": [ /* ResponseItem array, see 2.2 */ ],
  "tools": [ /* see 2.3 */ ],
  "tool_choice": "auto",
  "parallel_tool_calls": false,
  "store": false,
  "stream": true,            // ALWAYS true — Generate must collapse to Stream (see §6)
  "include": []              // ["reasoning.encrypted_content"] only if reasoning requested
}
```

Codex hardcodes `tool_choice:"auto"`, `store:false` (non-Azure), `stream:true`.
`service_tier`, `prompt_cache_key`, `text`, `reasoning`, `client_metadata` are optional
and **omitted** in our minimal request.

### 2.2 Input items — `ResponseItem`, serde `tag="type"`, `snake_case`

| Eino `schema.Message`                         | Responses `input` item |
|-----------------------------------------------|------------------------|
| `System`                                      | folded into top-level `instructions` (concatenated, newline-joined) |
| `User` (text)                                 | `{"type":"message","role":"user","content":[{"type":"input_text","text":...}]}` |
| `Assistant` (text)                            | `{"type":"message","role":"assistant","content":[{"type":"output_text","text":...}]}` |
| `Assistant` with `ToolCalls[]`                | one `{"type":"function_call","name":...,"arguments":"<json string>","call_id":<id>}` per tool call |
| `Tool` (role, `ToolCallID`)                   | `{"type":"function_call_output","call_id":<id>,"output":"<result string>"}` |

`ContentItem` variants: `input_text`, `output_text`, `input_image{image_url,detail?}`.
Item `id` is `skip_serializing` on input (re-added only by `attach_item_ids` when present),
so **function-call item ids are not required** — only `call_id` matters for tool round-trip.

### 2.3 Tools JSON (flat, NOT Chat-Completions-nested)

```jsonc
{ "type": "function", "name": "<name>", "description": "<desc>", "strict": false,
  "parameters": { /* JSON Schema object */ } }
```

Build `parameters` from `schema.ToolInfo.ParamsOneOf.ToJSONSchema()` →
`*jsonschema.Schema` (`github.com/eino-contrib/jsonschema`), marshaled directly.

### 2.4 SSE events (each `data:` line is a JSON object keyed by `type`)

Event struct fields: `type`, `response`, `item`, `item_id`, `call_id`, `delta`,
`summary_index`, `content_index`.

| Event `type`                              | Handling |
|-------------------------------------------|----------|
| `response.created`                        | ignore |
| `response.output_text.delta`              | `{delta}` → emit `Message{Role:Assistant, Content:delta}` (token stream) |
| `response.output_item.added`              | `{item}`; for `function_call` item, optionally emit the tool-call "header" chunk (Index, ID=call_id, Type="function", Name) |
| `response.output_item.done`               | `{item}`; **authoritative** complete item. For `function_call` → emit full tool-call chunk (call_id, name, full arguments). For `message` → ignore (text already streamed) |
| `response.reasoning_text.delta`           | `{delta}` → emit `Message{ReasoningContent:delta}` (optional) |
| `response.reasoning_summary_text.delta`   | `{delta}` → emit `Message{ReasoningContent:delta}` (optional) |
| `response.completed`                      | `{response:{id,usage:{input_tokens,output_tokens,total_tokens,...},end_turn}}` → emit final `Message` with `ResponseMeta.Usage` + `FinishReason` |
| `response.failed`                         | `{response:{error:{code,message}}}` → stream error; classify code (see §7) |
| `response.incomplete`                     | stream error |
| anything else                             | ignore |

**Tool-call capture strategy:** codex itself ignores `function_call_arguments.delta`
and reads the complete `FunctionCall` from `response.output_item.done`. We mirror that:
the authoritative tool call is assembled from `output_item.done`. Emitting it as a single
Eino tool-call chunk (with `Index`, `ID`, `Type`, `Name`, full `Arguments`) is correct and
`schema.ConcatMessages` merges it cleanly. (We may additionally stream `output_text.delta`
for text; arg-delta streaming is optional and not required for correctness.)

## 3. Files

All in `openaicodex/`:
- `chatmodel.go` — `ChatModelConfig`, `NewChatModel`, `NewChatModelWithHTTPClient`,
  `chatModel` struct implementing `Generate`/`Stream`/`WithTools`.
- `responses.go` — request/response types, message→input conversion, ToolInfo→tools,
  SSE parsing, native error classification (`classifyResponsesError`).
- `chatmodel_test.go` / `responses_test.go` — unit tests with a mocked `RoundTripper`
  replaying captured Responses-API SSE fixtures (fixtures derived from §2.4 shapes and the
  codex repo's own test fixtures).
- `examples/toolcall/main.go` + `examples/toolcall/README.md` — the 2-turn example.

Existing `openai_codex.go` / `openai_codex_test.go` (the `Advise` provider) stay as-is.

## 4. Public API

```go
type ChatModelConfig struct {
    // AppName builds an authenticated client via codex-auth-go when HTTPClient is nil.
    AppName string
    // Model, e.g. "gpt-5.5" (codex CLI default; recorded in example README).
    Model string
    // HTTPClient: optional pre-authenticated codex-auth-go client. MUST be a
    // codex-auth-go client (the transport owns endpoint rewrite + session_id +
    // ChatGPT-Account-Id + bearer); a generic authed client will NOT work.
    HTTPClient *http.Client
    // ReasoningEffort: optional ("low"/"medium"/"high"); empty → reasoning omitted.
    ReasoningEffort string
}

// NewChatModel builds the client from codex-auth-go (AppName) when cfg.HTTPClient is nil.
// Surfaces codexauth.ErrNotLoggedIn (wrapped ErrProviderAuth+ErrProviderInit) so callers
// can prompt for login.
func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error)

// NewChatModelWithHTTPClient builds from a caller-supplied codex-auth-go client.
func NewChatModelWithHTTPClient(ctx context.Context, client *http.Client, cfg ChatModelConfig) (model.ToolCallingChatModel, error)
```

Mirrors the `NewOpenAICodexProvider` / `...WithHTTPClient` split and `ollama.NewChatModel`.
`ChatModelConfig` exposes **no `BaseURL` and no max-output-tokens** field → the
"reject BaseURL / MaxTokens" semantics are preserved by construction.

## 5. `chatModel` struct & concurrency

```go
type chatModel struct {
    httpClient *http.Client
    model      string
    reasoning  string
    tools      []*schema.ToolInfo // immutable after WithTools
    toolsJSON  []responsesTool    // precomputed (optional)
}

func (m *chatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
    if len(tools) == 0 { /* per contract: return error or no-op clone — match Eino conventions */ }
    cp := *m                                   // shallow copy; shares *http.Client (safe)
    cp.tools = append([]*schema.ToolInfo(nil), tools...) // defensive copy
    // precompute toolsJSON; return error on bad schema
    return &cp, nil                            // receiver untouched
}
```

`WithTools` returns a **new** instance and never mutates the receiver → safe to share one
base model across goroutines and derive per-request tool sets (the consumer's pattern).
The `*http.Client` is concurrency-safe and shared.

## 6. `Generate` and `Stream`

- `Stream(ctx, msgs, opts)`:
  1. Build request body (§2.1), `stream:true`.
  2. `http.NewRequestWithContext(ctx, POST, codexauth.CodexEndpoint, body)`; set
     `req.GetBody` so the transport can rebuffer; `Content-Type: application/json`,
     `Accept: text/event-stream`.
  3. `m.httpClient.Do(req)`. On non-2xx, read body, `classifyResponsesError`.
  4. `schema.Pipe[*schema.Message](n)`; spawn a goroutine that scans the SSE body,
     parses events (§2.4), `sw.Send(chunk, nil)` per delta/tool-call, and on terminal
     `response.completed` sends the final usage/finish-reason chunk, then `sw.Close()`
     (reader sees `io.EOF`). Close the HTTP body on goroutine exit.
  5. **ctx cancellation:** the request ctx cancels the HTTP read; the goroutine detects
     `ctx.Err()` / read error, sends it via `sw.Send(_, err)`, closes body, returns.
     Consumer's `defer reader.Close()` cleans up.
- `Generate(ctx, msgs, opts)`: call the same streaming request internally, collect all
  chunks, `schema.ConcatMessages(chunks)` → single `*schema.Message`. This is required
  because the endpoint hardcodes `stream:true` (non-stream likely rejected) and guarantees
  Generate/Stream share one code path. Document this decision inline.

## 7. Error classification (native)

`classifyResponsesError(status int, body []byte)` and SSE `response.failed` handling map to
the existing sentinels (reachable via `errors.Is`):
- code `usage_not_included` (or HTTP 403 equivalent) → `WrapAuthError(fmt: %w ErrPlanNotIncluded)`
- code `insufficient_quota` (HTTP 429) → `WrapAuthError(fmt: %w ErrQuotaExceeded)`
- construction-time `codexauth.ErrNotLoggedIn` → `WrapInitError(WrapAuthError(ErrNotLoggedIn))`
- everything else → generic `fmt.Errorf("codex responses: %w", err)` (NOT auth)

Keeps `ErrProviderAuth`, `codexauth.ErrPlanNotIncluded`, `codexauth.ErrQuotaExceeded`,
`codexauth.ErrNotLoggedIn` reachable. The existing `classifyCodexError` (for the Eino
OpenAI `*APIError`) stays for `Advise`; native path uses the new classifier.

## 8. Reasoning & the multi-turn risk (most likely silent failure)

Reasoning is **optional** per the request (item 3). The biggest multi-turn risk is whether
the Codex endpoint requires prior **encrypted reasoning items** echoed back in `input`
between `function_call` and `function_call_output` for reasoning models (codex always
requests reasoning + `include:["reasoning.encrypted_content"]` for gpt-5.x).

**Default plan:** do **not** request reasoning (`reasoning` omitted, `include:[]`). This
minimizes multi-turn coupling and matches "reasoning optional". If the endpoint still emits
reasoning deltas, surface them to `ReasoningContent` (streamed) but do not echo them.

**Robustness hedge (if the wire test shows multi-turn breaks without reasoning echo):**
capture each `reasoning` output item (`id` + `encrypted_content`) into the assistant
`Message.Extra`, and in message→input conversion re-emit `{"type":"reasoning", ...}` items
ahead of the matching `function_call`. The consumer round-trips the full assistant Message,
so `Extra` survives. This is implemented only if §11 verification requires it.

**Open question for reviewers:** is the encrypted-reasoning round-trip needed for `gpt-5.5`
multi-turn tool calling, or does omitting reasoning suffice?

## 9. `Advise` left untouched (scope call)

Re-pointing `Advise` to the native Responses path would break its existing mocked
Chat-Completions tests, conflicting with "existing tests continue to pass." So `Advise`
stays on the Eino OpenAI adapter for v0.1.x `Provider` compatibility; the **new ChatModel**
is the corrected, wire-verified surface the consumer uses. File a follow-up bead to migrate
`Advise` onto the native client once it is on-the-wire-verified.

## 10. Example (`examples/toolcall`)

Given a logged-in session, the example:
1. `NewChatModel(ctx, {AppName, Model:"gpt-5.5"})`; on `ErrNotLoggedIn`, print login guidance.
2. Define one tool (`get_weather`-style `*schema.ToolInfo`), `cm.WithTools(...)`.
3. `Stream` turn 1 → observe a tool call (print id/name/args).
4. Append `schema.AssistantMessage`(with the tool call) + `schema.ToolMessage(result, callID)`.
5. `Stream` turn 2 → print token-by-token final answer; extract usage via `ExtractUsage`.

Uses **only** `eino-providers` public surface + `cloudwego/eino/schema`. README records the
tested model id (`gpt-5.5`, `model_reasoning_effort=medium` per `~/.codex/config.toml`).

## 11. Verification feasibility & gap (important)

`codex-auth-go` reads `~/Library/Application Support/<appname>/auth.json` in **its own
format**; the user's `~/.codex/auth.json` (codex CLI format) is **incompatible** and **no
codex-auth-go session exists on disk**. So:
- Unit tests (mocked transport, SSE fixtures) — done by us, gate CI.
- `go build ./...` + `go vet` + `golangci-lint` + `go test ./...` — done by us.
- **Live wire proof** requires a `codex-auth-go` login (browser OAuth) the user runs; the
  example surfaces `ErrNotLoggedIn` until then. We cannot complete the on-the-wire run
  autonomously. Best-effort: we may construct a throwaway client from `~/.codex` tokens to
  smoke-test the SSE parser, but the committed proof is the example + the user's run.

State this clearly in the example README and the final hand-off.

## 12. Step sequence

1. `responses.go`: types + message→input + ToolInfo→tools + SSE parser + error classifier.
2. `chatmodel.go`: config, constructors, struct, WithTools, Stream, Generate.
3. Unit tests: streaming text, tool-call assembly, multi-turn input encoding, ctx cancel,
   error classification, WithTools immutability/concurrency, usage extraction.
4. Example + README.
5. `go build/vet/test/lint`; update root README backend table + CHANGELOG.
6. Best-effort live smoke (if a session can be obtained); otherwise hand off the run step.
7. Session-close protocol (commit, `bd dolt push`, `git push`).

## 13. Acceptance mapping

- ToolCallingChatModel (Generate+Stream+WithTools) → §4–6
- Concurrency (immutable WithTools) → §5
- Auth (AppName or client; ErrNotLoggedIn) → §4, §7
- Streaming text + ctx cancel → §6, §2.4
- Multi-turn tool calling (≥2 turns) → §2.2, §2.4, §8
- Reasoning optional on ReasoningContent → §8
- Usage on ResponseMeta.Usage → §2.4, §6
- Error sentinels preserved → §7
- No caller BaseURL / output-token cap → §4
- Example (public surface only) → §10
- Existing behavior/tests pass → §9

## 14. Review reconciliation (two Opus reviews, 2026-05-28)

Both reviews verified against the codex Rust source. No conflicts; decisions:

**R1 (BLOCKING, wire). `reasoning` must serialize as `null`, not be omitted.**
`ResponsesApiRequest.reasoning: Option<Reasoning>` has **no** `skip_serializing_if`
(`codex-api/src/common.rs:178`). Always emit the field. Also always emit `tools`
(possibly `[]`), `tool_choice`, `parallel_tool_calls`, `store`, `stream`, `include` (`[]`).
`instructions` is omitted only when empty (`common.rs:172`). `service_tier`,
`prompt_cache_key`, `text`, `client_metadata` are omitted when unset.

**R2 (CRITICAL, wire — multi-turn). Thread encrypted reasoning by default.**
codex always sends `reasoning:{effort,summary}` + `include:["reasoning.encrypted_content"]`
for reasoning models, sends full history every turn (no `previous_response_id`), and
re-attaches reasoning item `id`s (`client.rs:746-751,768`; `api/requests/responses.rs:20-23`).
No codex path does a gpt-5.x 2-turn tool exchange *without* threaded reasoning, so omitting
it is unverified and risks a turn-2 400. **Decision:** default reasoning **ON** for the
wire-faithful path. Request `{"effort": <cfg or "medium">, "summary": "auto"}`,
`include:["reasoning.encrypted_content"]`. Capture each `reasoning` output item
(`id`+`encrypted_content`, `protocol/models.rs:768-778`) into the assistant
`Message.Extra` under key `openaicodex:reasoning_items`; in message→input conversion
re-emit those `{"type":"reasoning",...}` items immediately before the matching
`function_call`. Surface reasoning summary text to `ReasoningContent` (satisfies prompt
item 3). A `ChatModelConfig.DisableReasoning bool` sets `reasoning:null`,`include:[]` for
non-reasoning models. **The example must round-trip the full assistant `Message`
(including `Extra`)**; document that multi-turn reasoning threading depends on the consumer
preserving `Extra`.

**R3 (BLOCKING, contract). Handle `model.Option` varargs.** Use
`model.GetCommonOptions(&model.Options{Model:&m.model, Tools:m.tools}, opts...)`:
- `Tools` → option tools override bound tools for the call.
- `ToolChoice` → map `schema.ToolChoiceForbidden→"none"`, `ToolChoiceAllowed→"auto"`,
  `ToolChoiceForced→"required"`; nil → `"auto"`.
- `Model` → per-call override if set.
- `MaxTokens` → **ignored/not forwarded** (preserves "no caller output-token cap").
- `Temperature`/`TopP`/`Stop` → ignored (reasoning-model endpoints reject them); documented.

**R4 (BLOCKING, contract). Early `reader.Close()` must abort the HTTP read.**
`schema/stream.go:410-426`: consumer `Close()` only makes the *next* `sw.Send` return
`closed=true`; a goroutine blocked in `Read`/`Scan` never sees it. So: derive
`reqCtx, cancel := context.WithCancel(ctx)` for the request; in the SSE goroutine,
`defer resp.Body.Close()` and `defer cancel()`; when `sw.Send(chunk, err)` returns
`closed==true`, call `cancel()` and return. ctx cancellation (client disconnect) then
aborts the in-flight read on all paths.

**R5 (correctness). ConcatMessages role consistency.** Every emitted chunk — text delta,
tool-call chunk, terminal usage/finish chunk — carries `Role: schema.Assistant`. Mixed
roles make `ConcatMessages` error (`message.go:1818-1825`), which would break `Generate`.

**R6 (correctness). Always set tool-call `Index`.** Emit the authoritative tool call from
`output_item.done` as one chunk with `Index:&i`, `ID:call_id`, `Type:"function"`,
`Function:{Name,Arguments}`. `concatToolCalls` merges by `*Index` (`message.go:1439-1521`).

**R7. Usage mapping (explicit).** `input_tokens → ResponseMeta.Usage.PromptTokens`,
`output_tokens → CompletionTokens`, `total_tokens → TotalTokens`, plus cached/reasoning
details. `ExtractUsage` reads `PromptTokens`/`CompletionTokens` (`provider.go:23-27`).

**R8. `WithTools(nil)`/empty → no-op clone** (new instance, no tools), not an error —
preserves the shared-base pattern. Return type is `model.ToolCallingChatModel` only.

**R9. Error wrapping.** Init-time: `WrapInitError(WrapAuthError(ErrNotLoggedIn))`
(matches `openai_codex.go:57`). Call-time auth: `WrapAuthError(... %w sentinel)` only —
**not** additionally `WrapInitError`-wrapped (else misclassifies as init).

**R10. No `[DONE]` marker.** The SSE stream ends naturally after `response.completed`; a
close *before* `response.completed` is an error (`api_sse_responses.rs:422-427`). Treat
premature EOF as an error sent on the stream.

**R11. Example README** must name the exact `codex-auth-go` login command **and** the
`AppName`, since creds resolve to `~/Library/Application Support/<AppName>/auth.json`
(`client.go:104-110`) — not `~/.codex/auth.json`.

**Optional:** set a stable per-session `prompt_cache_key` (UUID) to match codex caching.
