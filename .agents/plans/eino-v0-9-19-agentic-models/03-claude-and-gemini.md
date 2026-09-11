# W3 — Claude and Gemini native models

## Goal and prerequisite

Expose verified Claude and Gemini agentic constructors while preserving their different protocol capabilities. Prerequisites: W1 common contract and W2 conformance harness patterns. The two provider implementations may proceed in parallel after W2 fixes the harness interfaces.

## Claude

Evidence: `claude/chatmodel.go` wraps the classic extension. Released `agenticclaude v0.1.5` demonstrates request-time ordinary/deferred/search tools, agentic tool choice, native server tools, citations, signed reasoning, and stream mappings. It omits wire-returned model identity and its pinned Anthropic SDK defaults to two hidden retries; the extension exposes no `option.WithMaxRetries` seam. Therefore use it as mapping evidence, not as the production delegate.

Change surface:

- `claude/agenticmodel.go`, `claude/agentic_request.go`, `claude/agentic_response.go`, and `claude/agentic_stream.go`: new; proposed config/constructor plus direct Messages JSON/SSE translation using an Anthropic client configured with zero retries.
- `claude/agentic_identity.go` and `claude/agentic_continuation.go`: new; returned identity plus atomic public/signed-thinking split and restore.
- `claude/agenticmodel_test.go` and `claude/agenticmodel_integration_test.go`: new; validation plus actual fake Messages JSON/SSE fixtures.
- `claude/agenticmodel_live_test.go`: new opt-in test.
- `claude/examples/agentic/`: new example under existing `claude/`.

Map existing API key, model, max tokens, base URL, HTTP client, temperature, top-p, top-k, and stop sequences. Add only agentic-native config needed for thinking/cache. Expose proposed `claude.WithAgenticServerTools` for native built-ins. Common Eino options own ordinary, deferred, search, and agentic tool choice.

Positive fixtures cover supported text/image/file input, visible and signed thinking, generated text, function call/result, deferred/client/hosted search, server tool calls/results, citations, usage, stop reason, and exact IDs. Negative fixtures cover audio/video/generated media, MCP/approvals, invalid roles/unions, and any unsupported selector. Verify signed thinking survives encode/restore and the second request.

## Gemini

Evidence: `gemini/chatmodel.go` already builds/caches `genai.Client`. Released `agenticgemini v0.2.3` exposes `New`, multimodal content, function and server tools, grounding metadata, thought signatures, response media, and agentic tool choice. It does not consume the Eino deferred/tool-search options.

Change surface:

- `gemini/agenticmodel.go`: new; proposed `AgenticModelConfig`, `NewAgenticModel`, cached-client ownership, bounded response observer, local provider options, and safe errors.
- `gemini/agentic_identity.go` and `gemini/agentic_continuation.go`: new; recover returned model/version, remove unexported thought-signature extras from the public clone, and convert them into versioned state and exact `[]byte` runtime values on restore.
- `gemini/agenticmodel_test.go` and `gemini/agenticmodel_integration_test.go`: new; fake `genai` HTTP endpoint fixtures.
- `gemini/agenticmodel_live_test.go`: new opt-in test.
- `gemini/examples/agentic/`: new example under existing `gemini/`.
- `factory_bench_test.go` and `scripts/check-gemini-benchmark.sh`: existing; add/adjust an agentic constructor benchmark only from measured results.

Map API key/client, model, max tokens, temperature, top-p, top-k, thinking, image generation, response modalities, media resolution, safety, and cache controls. Proposed `gemini.WithAgenticServerTools` wraps native server tools. Reject `WithDeferredTools` and `WithToolSearchTool` before the `genai` client sends a request.

Positive fixtures cover the provider-supported input/output media matrix, visible reasoning plus opaque thought signature, function call/result including supported media results, Google search/code execution and grounding, requested/wire-returned model identity, usage/finish metadata, stable stream indices, and continuation. Negative fixtures cover unsupported server/MCP/approval/search forms and unsupported tool-choice variants.

## Shared lifecycle and acceptance

- Both providers reject unsupported inputs/options before dispatch with `ErrUnsupportedCapability`.
- Generate equals concatenated Stream for each supported block class.
- Stream cancellation and reader close close the native body and worker without a terminal success.
- Underlying SDK retries are disabled. Retryable fake responses cause one observed request. Claude's direct client must pass `option.WithMaxRetries(0)`; an extension wrapper is not an allowed substitute.
- Simultaneous calls with different tools/options do not mutate base configs or one another.
- Public split/restore helpers produce sentinel-free public clones, preserve immutable sources, survive a type-erasing JSON reopen, and restore exact signed/thought runtime types from an external package.
- Quantitative W1 limits apply before/while parsing native responses and on incremental Generate accumulation.
- Run `GOWORK=off go test -race ./claude ./gemini`, the Gemini benchmark gate, dependency-leak gate, and full suite.

Stop/go gate: if Gemini's released extension plus bounded observer remains lossy, replace it with a local native codec under the same package. Claude already selects the local-codec route. No downgrade is allowed for a feature the selected backend natively supports unless the released matrix marks it rejected and the response names the gap.
