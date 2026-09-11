# Eino v0.9.19 agentic models

Status: Ready for implementation. Implementation has not occurred.

## Application context

```json
{
  "application_context": {
    "has_active_users": false,
    "backward_compatibility_required": false,
    "feature_flags": "not-applicable",
    "confirmation_digest": "0de9377e71911b13ae20f46d700be1aae6f019524145536ccb70327ca1b8d557",
    "confirmed_at": "2026-09-11T21:10:21Z"
  }
}
```

The user confirmed both booleans on 2026-09-11. The implementation needs no compatibility shim, migration, or feature flag. Keep the existing classic constructors where coexistence is simpler, but do not constrain the new surface to preserve them.

## Outcome and success

This is a breaking dependency/API addition across `go.mod`, the root error contract, all six provider packages, tests, examples, CI, ADRs, README, changelog, and the cross-repository response. Pin `github.com/cloudwego/eino v0.9.19` at upstream SHA `9d983b36a5112a1c233056b1a099825298fafb8f`. Expose a real `model.AgenticModel` constructor in each existing provider owner: `claude.NewAgenticModel`, `openai.NewAgenticModel`, `gemini.NewAgenticModel`, `ollama.NewAgenticModel`, `openaicodex.NewAgenticModel`, and `opencodego.NewAgenticModel`.

Success means:

- each constructor returns a native agentic implementation, not a classic `schema.Message` bridge advertised as lossless;
- `Generate` and incrementally streamed `Stream` agree after one `schema.ConcatAgenticMessages` operation;
- every supported input/output block, option, server/MCP tool, approval, search, metadata, and continuation row has a positive fake-native-endpoint fixture;
- every unsupported row returns a typed capability error before transport dispatch;
- function call/result continuation preserves provider-native IDs and opaque state across an encode/restore boundary without importing `eino-agent` in production;
- a clean `GOWORK=off` consumer resolves the published module without `replace` directives and exercises one agentic constructor; and
- the response at `$HOME/.agents/projects/eino-providers/responses/2026-09-10-eino-v0-9-19-agentic-models.md` records exact constructors, pins, matrix, fixture commands, live-test status, and remaining gaps.

## Scope and non-goals

In scope: dependency selection, native agentic constructors, capability validation, safe errors, wire codecs, stream lifecycle, deterministic fixtures, bounded opt-in live tests, documentation, and publication evidence. The root `Provider.Advise` registry is not the agentic factory; callers import a concrete provider package and select the model/protocol explicitly.

Out of scope: importing or embedding `eino-agent`; persisting provider-private state; automatic protocol/model/credential failover; retry orchestration; provider services; session creation; asset fetching/authorization; UI rendering; claiming that every provider implements every Eino block; and production credentials in ordinary CI.

## Repository findings

- `go.mod` pins Eino v0.8.13 and classic extension modules. ADR `docs/adr/0004-eino-version-pins.md` requires deliberate exact upgrades with backend tests and benchmarks.
- `claude/chatmodel.go`, `openai/chatmodel.go`, and `gemini/chatmodel.go` wrap classic Eino extensions. Released native packages now exist as `agenticclaude v0.1.5`, `agenticopenai v0.2.2`, and `agenticgemini v0.2.3`; their own module files require compatible Eino v0.9.x lines.
- The released agentic extensions expose real `AgenticMessage` codecs. OpenAI Responses and Claude implement deferred/tool-search options. Gemini implements agentic tool choice but not the Eino deferred/tool-search pair. All three omit wire-returned model identity from their `AgenticResponseMeta`; Claude also fixes its Anthropic SDK at the default two retries with no config seam.
- `openaicodex/chatmodel.go` and `opencodego/` already own native wire codecs and lifecycle tests, but only for `ToolCallingChatModel`. Their continuation data currently lives in classic message extras and must be represented through agentic blocks/extensions without a provider-to-runtime dependency.
- `ollama/ollama.go` has no released agentic Eino extension. A native `/api/chat` codec is required for honest agentic text/image, thinking, function-call, and function-result support.
- `.github/workflows/ci.yml` runs tidy, tests, dependency-leak checks, the Gemini construction benchmark, and lint on Go 1.25 and 1.26. `scripts/check-dep-leaks.sh` must learn the new agentic extension modules.
- The prospective consumer's W3 plan at `$EINO_AGENT_DIR/.agents/plans/eino-v0-9-19/03-content-and-models.md` requires a published native constructor and reopen-and-continue fixture. Core consumer work is independent, but its final W3/W8 integration remains blocked until this repository publishes a usable pin.
- The worktree was clean at planning start except for Beads state outside the Git worktree. The planning issue is `eino-providers-j7p`.

## Decisions

1. Keep native translation in concrete provider packages. Do not add a root agentic registry or make the root package import all backend SDKs.
2. Use `openai.NewAgenticModel` backed by OpenAI Responses as the first deliverable. It is the fastest real native path and proves the common contract before custom codecs.
3. Reuse the OpenAI and Gemini agentic extensions only behind bounded wire observers and local conformance tests. The observers recover actual response identity and enforce byte/attempt limits. Their unit tests are evidence, not proof of this module's contract.
4. Implement custom native agentic codecs for Claude, OpenAI-Codex, OpenCode Go, and Ollama. Claude cannot disable the released extension's hidden SDK retries. Shared local helpers may live in a new `internal/agenticfixture/` test-support package, but provider wire types remain with their owners.
5. Add a root typed unsupported-capability contract. Reject unsupported blocks/options before opening a request. Never silently flatten, drop, or reinterpret rich content.

Rejected alternatives: a generic classic-message adapter loses native blocks and continuation; importing `eino-agent` inverts ownership; relying only on extension tests misses wrapper behavior; automatic fallback obscures actual provider identity and attempt accounting; and server-side response IDs are not a substitute for replayable opaque state.

## Change model

```text
Before: host -> concrete NewChatModel -> schema.Message -> classic adapter/native partial codec

After:  host -> concrete NewAgenticModel -> schema.AgenticMessage
             -> pre-dispatch capability validation
             -> native request codec / verified native eino-ext adapter
             -> native response or SSE parser
             -> ordered AgenticMessage blocks + public metadata + opaque continuation
```

Host-owned auth, model selection, HTTP client, session ID, and protocol stay constructor inputs. Eino ADK owns retries. Provider models either configure underlying SDK retries to zero or expose every attempt through an injected observation seam; the default plan is zero hidden retries for new agentic constructors.

## Public and private data boundary

Public display data includes user/assistant text and media, visible reasoning text, function/server/MCP calls and results, approval prompts/decisions, citations, grounding, refusals, requested and wire-returned model identity, provider/protocol identity, token usage, response status, finish reason, and stable correlation IDs. Each provider attaches the proposed root `AgenticResponseIdentity` to response metadata without replacing Eino's typed OpenAI, Claude, or Gemini extensions.

Opaque continuation includes `Reasoning.Signature`, OpenAI encrypted reasoning payloads, Gemini thought signatures in block extras, Claude signed thinking, cached/previous-response identifiers when needed to resume, and provider-specific raw replay items. Each provider exposes proposed `SplitAgenticContinuation` and `RestoreAgenticContinuation` helpers. Split atomically returns a sanitized public clone plus a versioned root `AgenticContinuationState`; it removes every provider-private signature, extra, cached ID, and replay item while leaving public text, correlation IDs, and display metadata. Restore combines that public clone and state into a new provider-ready message with exact runtime types. External-package and clean-consumer tests use only these public helpers through a type-erasing JSON encode/reopen/decode cycle. Providers do not persist the state. The source remains immutable. The state type has no content-revealing `String` method and must not enter display, logs, errors, or telemetry.

## Resource bounds

Every proposed `AgenticModelConfig` includes root `AgenticLimits`. Zero values resolve to defaults: 16 MiB encoded request, 2 MiB one native event, 32 MiB aggregate response, 8 MiB decoded inline media per block, 1,024 content blocks, 1 MiB captured error body, and 32 MiB test request capture. Hard caps are 64 MiB request, 8 MiB event, 128 MiB response, 32 MiB media, 4,096 blocks, 4 MiB error, and 64 MiB capture. Reject negative or over-hard-cap values at construction. For extension-backed paths, wrap `resp.Body` with a framing reader that limits each SSE record and aggregate bytes before the extension decoder reads them. Local codecs enforce the same checks in their parsers. Incremental collectors enforce aggregate bytes/blocks without retaining an unbounded chunk slice. Overflow returns proposed `ErrResourceLimit` / `ResourceLimitError`, closes the body, cancels the producer, and never emits successful EOF.

## Risks, assumptions, and gates

- Stop the extension-backed package if a fake-endpoint test proves that the released extension plus bounded identity observer drops or rewrites a required native value. Replace only that provider with a local codec; do not weaken the matrix.
- Stop the Codex wrapper path if `agenticopenai` emits fields rejected by the Codex endpoint or performs hidden retries. Use the existing local Responses engine as the implementation base.
- Treat extension values stored in `any` or `Extra` as private unless explicitly allowlisted in `docs/agentic-capabilities.md` (new, under existing `docs/`).
- Assume no live credentials. Live gates remain opt-in and unrun status is reported honestly.
- The full draft matrix in [01-contract-and-dependencies.md](01-contract-and-dependencies.md) is the fixed implementation target. A supported cell may change only when cited native protocol/server-version evidence proves it unavailable; that change requires maintainer review and synchronized plan/response edits. Adapter difficulty or a failing fixture is not downgrade evidence.

No blocking user decision remains. All gates above are implementation evidence gates with deterministic fallback paths.

## Cross-repository dependency

The canonical request is `$HOME/.agents/projects/eino-providers/requests/2026-09-10-eino-v0-9-19-agentic-models.md`. Owner: Matt / eino-providers maintainers. Demonstrated active consumers: none. Blocked prospective consumer: `github.com/mattsp1290/eino-agent`, W3 native-provider integration and W8 release verification. This local plan is ready independently. The consumer remains blocked until the response names an accepted contract and a remotely downloadable pin passes its clean-consumer fixture.

## Document map

- [01-contract-and-dependencies.md](01-contract-and-dependencies.md): pin selection, public errors/configs, capability matrix, and proof gates.
- [02-openai-first-deliverable.md](02-openai-first-deliverable.md): first native adapter and common conformance harness.
- [03-claude-and-gemini.md](03-claude-and-gemini.md): local Claude codec and verified Gemini extension wrapper.
- [04-codex-opencode-ollama.md](04-codex-opencode-ollama.md): custom native codecs, continuation, and lifecycle behavior.
- [05-verification-and-publication.md](05-verification-and-publication.md): full matrix, consumer fixture, docs, response, and release evidence.
- [06-execution-handoff.md](06-execution-handoff.md): dependency-ordered implementation packages and definition of done.
