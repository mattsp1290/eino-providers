# W4 — Codex, OpenCode Go, and Ollama native codecs

## Goal and prerequisites

Convert the three locally owned native-protocol implementations to honest `AgenticMessage` models without routing through classic message translation. Prerequisites: W1 contracts and W2 stream/fixture conventions. The three providers may proceed in parallel if they do not change shared helpers concurrently.

## OpenAI-Codex Responses

Evidence: `openaicodex/chatmodel.go`, `responses.go`, and package tests already implement Responses request/SSE, function continuation, encrypted reasoning, cancellation, error sentinels, and immutable classic tools. Codex auth transport owns endpoint rewriting and credentials.

Change surface:

- `openaicodex/agenticmodel.go`: new; proposed `AgenticModelConfig`, `NewAgenticModel`, and `NewAgenticModelWithHTTPClient`.
- `openaicodex/agentic_responses.go` and `openaicodex/agentic_stream.go`: new beside existing codecs; accept/emit `AgenticMessage` directly. Refactor wire-only helpers from existing files only when both classic and agentic paths stay test-covered.
- `openaicodex/agentic_identity.go` and `openaicodex/agentic_continuation.go`: new; actual requested/returned identity and atomic public/encrypted-item split/restore.
- `openaicodex/agenticmodel_test.go` and `openaicodex/agentic_integration_test.go`: new native fixtures.
- `openaicodex/examples/agentic/`: new.

Support the actual Codex subset: system/user text, assistant text, reasoning summary/signature, function calls/results, ordinary tools, applicable agentic choice, usage/finish/model identity, and stateless encrypted-reasoning replay. Reject deferred/search, server/MCP/approval, response-ID state, unsupported media, temperature/top-p/stop, and any unaccepted option before dispatch. Preserve `ErrProviderAuth` and `codex-auth-go` sentinels. The auth client must not add retries.

First evaluate `agenticopenai v0.2.2` with the authenticated HTTP client and zero retries in a fake Codex fixture. Reuse it only if request shape, endpoint rewriting, private reasoning, error mapping, and one-attempt behavior all match. Otherwise use the local codec described above; this is the expected fallback and does not block the plan.

## OpenCode Go protocols

Evidence: `opencodego/config.go`, `adapters.go`, `responses*.go`, `stream.go`, and `integration_test.go` explicitly select Chat Completions, Messages, or Responses and already exercise fake native endpoints, cancellation, errors, session precedence, usage, retries, and concurrency for classic messages.

Change surface:

- `opencodego/agentic_config.go` and `opencodego/agenticmodel.go`: new; proposed config/constructor and immutable validation.
- `opencodego/agentic_chat_completions.go`, `opencodego/agentic_messages.go`, `opencodego/agentic_responses.go`, and matching stream files: new native `AgenticMessage` codecs beside existing protocol files.
- `opencodego/agentic_identity.go` and `opencodego/agentic_continuation.go`: new; protocol-tagged identity and atomic public/opaque-state split/restore.
- `opencodego/agentic_integration_test.go`: new; one table drives all three protocols plus protocol-specific capability cases.
- `opencodego/examples/agentic/`: new.

Keep explicit protocol selection, context session precedence, caller user agent, auth error safety, and no fallback. Responses supports the encrypted-reasoning/function subset proved for that service. Messages supports its native reasoning/function/media subset. Chat Completions supports only representable native fields. Each protocol gets separate matrix rows and rejects richer blocks/options before dispatch. Override the current Messages SDK retry default for the agentic constructor so one runtime attempt is one HTTP attempt; do not change classic retry behavior incidentally.

## Ollama `/api/chat`

Evidence: `ollama/ollama.go` owns URL validation, health probing, model, timeout, HTTP client, keep-alive, format, and thinking settings. No released Eino AgenticModel adapter exists.

Change surface:

- `ollama/agenticmodel.go`: new; proposed `AgenticModelConfig` and `NewAgenticModel` while reusing validated health/client construction.
- `ollama/agentic_request.go`, `ollama/agentic_response.go`, and `ollama/agentic_stream.go`: new native JSON/NDJSON codecs.
- `ollama/agentic_identity.go` and `ollama/agentic_continuation.go`: new; response model identity and atomic split/restore helpers. The state is empty because the selected protocol has no opaque replay but still validates provider/protocol correlation.
- `ollama/agenticmodel_test.go` and `ollama/agentic_integration_test.go`: new.
- `ollama/examples/agentic/`: new.

Support only native `/api/chat` features proven by fixtures: text and image input, generated text, thinking mapped to public reasoning text, function calls/results, ordinary tools, model identity, durations/token counts, done reason, keep-alive, cancellation, and streaming. The pinned Ollama wire schema has no tool-call/result ID field. Assign a clearly documented request-local synthetic `CallID` for host correlation, never claim or test that it was transmitted, and serialize results by native ordered tool name. Reject a continuation before dispatch when duplicate same-name calls make that mapping ambiguous. The matrix marks native ID preservation and opaque replay `N`. Reject agentic tool choice, audio/video/file, server/MCP/approval/deferred/search, opaque signed continuation, and unsupported generated media before dispatch.

## Lifecycle and acceptance

- Actual fake endpoints validate request bodies and feed fragmented, interleaved, malformed, truncated, terminal-error, cancellation, and early-close streams.
- Generate and concatenated Stream agree per protocol.
- HTTP 200 or NDJSON `done:false` without a valid terminal completion returns an error.
- Every request preserves actual model/protocol/session identity and exact native call IDs only for protocols that supply them. Ollama tests instead prove documented synthetic-ID stability in memory, absence from wire JSON, ordered unique-name continuation, and duplicate-name ambiguity rejection.
- Synthetic private sentinels survive documented encode/restore paths and do not appear in error strings, logs, public example output, or request snapshots outside expected encrypted fields.
- External-package tests invoke only public split/restore helpers, prove sanitized clones contain no private sentinels, preserve immutable sources, and cross a type-erasing JSON reopen.
- W1 request/event/aggregate/media/block/error/capture limits are enforced incrementally for SSE and NDJSON, including a never-terminating oversized stream.
- `GOWORK=off go test -race ./openaicodex ./opencodego ./ollama` and the full suite pass.
