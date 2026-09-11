# W2 — OpenAI Responses first deliverable

## Goal and prerequisite

Deliver one real native `model.AgenticModel` with function-call/result continuation, private-state fidelity, and a first complete capability slice. Prerequisite: W1 dependency and error contracts.

## Evidence and design

`openai/chatmodel.go` already owns OpenAI config validation and wrapper error conventions. Released `github.com/cloudwego/eino-ext/components/model/agenticopenai v0.2.2` exposes `NewResponsesModel`, consumes all four required Eino agentic tool options, maps server/MCP/approval blocks, citations/refusals, encrypted reasoning, usage, streaming indices, and response metadata.

Use the released Responses adapter only after exercising its actual HTTP/SSE path through an injected bounded observing `http.Client`. The observer must recover the wire-returned model because v0.2.2 drops `responses.Response.Model`. Do not wrap the existing classic `openai.NewChatModel`.

## Change surface

- `openai/agenticmodel.go`: new beside existing `openai/chatmodel.go`; define proposed `AgenticModelConfig`, `NewAgenticModel`, validation, zero-retry configuration, and wrapper error normalization.
- `openai/agentic_identity.go` and `openai/agentic_continuation.go`: new; attach proposed `AgenticResponseIdentity` and implement the public versioned export/restore contract without relying on unexported upstream keys in consumers.
- `openai/agenticmodel_test.go`: new; constructor, immutable option, rejection, and concurrent-call tests.
- `openai/agenticmodel_integration_test.go`: new; fake HTTP/SSE native fixtures using actual `agenticopenai` request/parser code.
- `openai/agenticmodel_live_test.go`: new; opt-in bounded live test using environment-selected model and capabilities. Never run by default.
- `openai/examples/agentic/`: new under existing `openai/`; two-turn function example with public content separated from opaque continuation.

`AgenticModelConfig` carries API key, model, base URL, HTTP client, timeout, max retries (validated as zero for runtime-accounted operation), max tokens, temperature, top-p, reasoning settings, and store/cache controls. Provider-specific server/MCP option helpers are proposed only where the host cannot express the capability through the four common Eino options; keep their names `openai.WithAgenticServerTools` and `openai.WithAgenticMCPTools` and return `model.Option`.

## Behavior

Exercise Generate and Stream through the same native protocol. Stream sends deltas immediately with stable `StreamingMeta.Index`. The adapter must send terminal usage/finish metadata once, surface a late terminal error before EOF, and never append a full done object after emitted deltas. Generate either invokes the adapter's native Generate or collects Stream once; the fixture compares the result after `schema.ConcatAgenticMessages`.

Function continuation fixture:

1. Send ordinary tools and force/allow a named tool with `WithAgenticToolChoice`.
2. Receive reasoning plus two interleaved function calls with exact native call IDs.
3. Split with `openai.SplitAgenticContinuation`, prove the public clone contains no private values, JSON encode both parts, discard all original Go values, decode, and restore with `openai.RestoreAgenticContinuation` from an external test package.
4. Send ordered function results on a second request.
5. Assert the outbound JSON replays exact call IDs and encrypted/private values and the final response preserves actual model, usage, finish state, citations/refusal metadata where present.

Cover hosted deferred-tool search, client tool search plus `ToolSearchFunctionToolResult`, server tools, MCP list/call/result, approval request/response, supported input media and generated output blocks. Unsupported media or role forms must fail before request.

## Tests and acceptance

- Native request snapshots prove core and provider-specific options without secrets.
- SSE fixtures cover interleaved indices, split UTF-8/JSON arguments, malformed event JSON, truncated stream, explicit failed/incomplete response, late terminal error, cancellation, early reader close, and an HTTP 200 that never completes.
- Concurrent calls share one base model while selecting different immutable options; request captures never cross-contaminate.
- Wire-returned model identity, not only the requested model, appears in the terminal response identity extension.
- The public split clone excludes encrypted reasoning, item/cache IDs, and provider-private extras while retaining visible reasoning, citations/refusal, call IDs, usage, and identity; the source remains unchanged.
- `schema.ConcatAgenticMessages` succeeds exactly once and matches Generate for equivalent fixture payloads.
- Default and custom event/request/aggregate/media/block/error/capture limits fail with `ErrResourceLimit`, close the body, and cancel the producer; Generate never retains an unbounded chunk slice.
- The package passes `GOWORK=off go test -race ./openai` and the repository test suite.
- The first deliverable publishes the complete W1 provider/protocol applicability matrix with `planned` versus `verified` status and versioned native evidence for every cell; only OpenAI cells may be marked verified at this stage.

Stop/go gate: if any required native value is inaccessible or lossy in `agenticopenai v0.2.2`, stop W2 and implement the missing OpenAI Responses codec locally in `openai/` using the same public constructor. Do not ship a misleading supported cell.
