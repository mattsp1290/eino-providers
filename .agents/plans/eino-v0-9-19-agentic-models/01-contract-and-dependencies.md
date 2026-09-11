# W1 — Contract and dependency baseline

## Goal and prerequisites

Establish one compilable Eino v0.9.19 baseline and the contracts that every adapter must implement before provider work diverges. Prerequisite: none.

## Evidence

Existing pins live in `go.mod` and `docs/adr/0004-eino-version-pins.md`. Existing constructor/error patterns live in each `*/chatmodel.go`, `errors.go`, and `errors_test.go`. Eino v0.9.19 defines `model.AgenticModel`, request-time `model.WithTools`, `WithDeferredTools`, `WithToolSearchTool`, `WithAgenticToolChoice`, `schema.AgenticMessage`, 20 `ContentBlockType` values, stable `StreamingMeta.Index`, and `schema.ConcatAgenticMessages`.

Released adapter candidates are exact modules `agenticopenai v0.2.2`, `agenticclaude v0.1.5`, and `agenticgemini v0.2.3`. Their module requirements resolve to Eino v0.9.x and must be forced to this repository's v0.9.19 pin by minimal-version selection.

## Change surface

- `go.mod`, `go.sum`: existing; pin Eino v0.9.19 and add exact agentic extension modules only for provider packages that pass W2/W3 conformance.
- `docs/adr/0004-eino-version-pins.md`: existing; supersede its v0.8.13 decision or link a new ADR.
- `docs/adr/0008-agentic-model-boundary.md`: new, under existing `docs/adr/`; record constructor ownership, extension reuse gates, hidden-retry policy, and private-state boundary.
- `errors.go`: existing; add proposed `ErrUnsupportedCapability`, `Capability`, `UnsupportedCapabilityError`, `ErrorClassUnsupportedCapability`, `ErrResourceLimit`, `ResourceLimitError`, and `ErrorClassResourceLimit` symbols. `Classify` checks these principal pre-dispatch/overflow classes before generic provider API wrappers. Concrete errors expose safe identifiers and numeric limits without echoing content, options, bodies, or private state.
- `agentic.go`: new, at repository root; add proposed `AgenticResponseIdentity`, `AgenticContinuationState`, `AgenticLimits`, default-limit constants, and validation. Add proposed `AgenticProviderInfo` and `AgenticCapabilities` read-only metadata only if tests show callers need a machine-readable query. Do not add a root factory.
- `internal/`: new directory anchored at the existing repository root. `internal/agenticfixture/` is a new test-support package within it; provide fake JSON/SSE/NDJSON endpoint helpers, transport-call counters, bounded incremental stream collection, redacted bounded request capture, retry/identity observation, and private-sentinel assertions. It must not become a public module API.
- Each provider package: add proposed `AgenticModelConfig` and `NewAgenticModel`; `openaicodex` also adds proposed `NewAgenticModelWithHTTPClient`. Configs keep host-owned model/auth/client/base URL/session fields and default underlying retries to zero.
- Each provider package: add proposed public `SplitAgenticContinuation(*schema.AgenticMessage) (public *schema.AgenticMessage, state einoproviders.AgenticContinuationState, err error)` and `RestoreAgenticContinuation(public *schema.AgenticMessage, state einoproviders.AgenticContinuationState) (*schema.AgenticMessage, error)`. Split removes provider-private values from a cloned public projection atomically. Restore validates provider/protocol/version/correlation identity and reconstructs exact runtime types in another clone. Neither mutates inputs or persists state.

Exact public constructors:

```text
claude.NewAgenticModel(context.Context, claude.AgenticModelConfig) (model.AgenticModel, error)
openai.NewAgenticModel(context.Context, openai.AgenticModelConfig) (model.AgenticModel, error)
gemini.NewAgenticModel(context.Context, gemini.AgenticModelConfig) (model.AgenticModel, error)
ollama.NewAgenticModel(context.Context, ollama.AgenticModelConfig) (model.AgenticModel, error)
openaicodex.NewAgenticModel(context.Context, openaicodex.AgenticModelConfig) (model.AgenticModel, error)
openaicodex.NewAgenticModelWithHTTPClient(context.Context, *http.Client, openaicodex.AgenticModelConfig) (model.AgenticModel, error)
opencodego.NewAgenticModel(context.Context, opencodego.AgenticModelConfig) (model.AgenticModel, error)
```

These symbols are proposed; their insertion points are the matching existing package directories beside `chatmodel.go`.

## Capability contract

Create `docs/agentic-capabilities.md` (new, under existing `docs/`) with rows for every provider/protocol and independent columns for: all 20 block kinds; five function-result media variants; Generate; Stream; ordinary tools; deferred tools; client tool search; hosted/deferred search; server tools; MCP list/call/result; MCP approval request/response; every agentic tool-choice variant; citations; grounding; refusal; usage; finish/status; requested and wire-returned model identity; provider/protocol identity; opaque replay; response-ID continuation; concurrency; retry; cancellation; resource limits; and live-test status.

Each cell is `supported`, `rejected-before-dispatch`, or `not-native`, with a separate implementation status of `planned` or `verified`. At the W2 minimal release, every cell links native protocol/SDK evidence and OpenAI verified cells also link fixtures. By W5, every supported cell links one fixture for that exact capability, not a combined metadata/search/continuation fixture, and every rejected cell links a typed negative fixture. `not-native` is reserved for protocol impossibility and links versioned native evidence. Do not use vague values such as partial or best effort.

Initial evidence-backed targets:

| Provider/protocol | Native implementation target | Distinguishing positive surface |
| --- | --- | --- |
| OpenAI Responses | `agenticopenai v0.2.2` behind local wrapper | deferred/client/hosted search, function/server/MCP tools, approvals, citations/refusal, encrypted reasoning, response identity |
| Claude Messages | local native codec, informed by `agenticclaude v0.1.5` mappings | deferred/client/hosted search, function/server tools, citations, signed thinking |
| Gemini generateContent | `agenticgemini v0.2.3` behind local wrapper | multimodal input/output, function/server tools, grounding, thought signatures |
| OpenAI-Codex Responses | local native codec or proven restricted OpenAI Responses wrapper | text, reasoning, function continuation, usage/finish, exact Codex auth errors |
| OpenCode Go Responses/Messages/Chat Completions | local protocol codecs selected explicitly | protocol-specific text, reasoning where native, function continuation, usage/finish |
| Ollama `/api/chat` | local native codec | text/image input, thinking, function continuation, usage/finish |

### Fixed draft matrix

Legend: `S` = required supported cell with a positive native-byte fixture; `N` = not native for the selected protocol/service version and rejected before dispatch. Provider rows are `OA` OpenAI Responses, `CL` Claude Messages, `GE` Gemini generateContent, `CX` OpenAI-Codex Responses, `OR` OpenCode Responses, `OM` OpenCode Messages, `OC` OpenCode Chat Completions, and `OL` Ollama `/api/chat`.

Block targets are grounded in the released agentic extension converters for OA/CL/GE, the existing local Responses/Message codecs for CX/OR/OM/OC, and the pinned Ollama wire types for OL. A cell can change only from cited native protocol/server-version evidence and maintainer review; adapter failure is not evidence for a downgrade.

| Block kind | OA | CL | GE | CX | OR | OM | OC | OL |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| reasoning | S | S | S | S | S | S | N | S |
| user input text | S | S | S | S | S | S | S | S |
| user input image | S | S | S | N | N | S | N | S |
| user input audio | N | N | S | N | N | N | N | N |
| user input video | N | N | S | N | N | N | N | N |
| user input file | S | S | S | N | N | S | N | N |
| tool-search result | S | S | N | N | N | N | N | N |
| assistant generated text | S | S | S | S | S | S | S | S |
| assistant generated image | N | N | S | N | N | N | N | N |
| assistant generated audio | N | N | S | N | N | N | N | N |
| assistant generated video | N | N | S | N | N | N | N | N |
| function-tool call | S | S | S | S | S | S | S | S |
| function-tool result | S | S | S | S | S | S | S | S |
| server-tool call | S | S | S | N | N | N | N | N |
| server-tool result | S | S | S | N | N | N | N | N |
| MCP tool call | S | N | N | N | N | N | N | N |
| MCP tool result | S | N | N | N | N | N | N | N |
| MCP list-tools result | S | N | N | N | N | N | N | N |
| MCP approval request | S | N | N | N | N | N | N | N |
| MCP approval response | S | N | N | N | N | N | N | N |

Nested function-result targets follow the converter evidence: OA and CL support text/image/file; GE, CX, OR, OM, OC, and OL support text only. Audio and video are `N` for every row. Each of the five variants still receives its own positive or zero-dispatch fixture.

| Option/tool capability | OA | CL | GE | CX | OR | OM | OC | OL |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `WithTools` | S | S | S | S | S | S | S | S |
| `WithDeferredTools` | S | S | N | N | N | N | N | N |
| `WithToolSearchTool` | S | S | N | N | N | N | N | N |
| allowed agentic choice | S | S | S | S | S | S | S | N |
| forced agentic choice | S | S | S | S | S | S | S | N |
| forbidden agentic choice | S | S | S | S | S | S | S | N |
| hosted/deferred search | S | S | N | N | N | N | N | N |
| server tools | S | S | S | N | N | N | N | N |
| MCP tools/approvals | S | N | N | N | N | N | N | N |

| Metadata/continuation | OA | CL | GE | CX | OR | OM | OC | OL |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| citations | S | S | N | N | N | N | N | N |
| grounding | N | N | S | N | N | N | N | N |
| refusal | S | N | N | N | N | N | N | N |
| usage and finish/status | S | S | S | S | S | S | S | S |
| requested + returned model identity | S | S | S | S | S | S | S | S |
| opaque replay | S | S | S | S | S | S | N | N |
| response-ID continuation | S | N | N | N | N | N | N | N |
| native tool-call ID | S | S | S | S | S | S | S | N |

All rows require Generate, Stream, stable streaming indices, immutable concurrency, one observed HTTP attempt, cancellation/body close, late-error precedence, quantitative limits, public/private split, and explicit live-test status. Ollama lacks a wire call-ID field in the pinned `github.com/eino-contrib/ollama v0.1.0` types; its local correlation rules are specified in W4 and must never be labeled native ID preservation.

## Invariants and failures

- Validate discriminated unions: exactly one payload matches `ContentBlock.Type`; reject nil messages/blocks, unsupported roles, malformed JSON arguments, duplicate/missing call IDs, unmatched results, invalid media, and incompatible private state.
- Apply common options immutably per call. Nil/empty ordinary, deferred, and search tools have explicit clearing semantics.
- Validate unsupported options before transport. A transport counter must remain zero on rejection.
- Preserve actual configured/overridden model identity and protocol. Never synthesize provider identity from a default after dispatch.
- Run a W1 raw-response probe for every extension candidate. A bounded per-operation response observer must capture provider, protocol, requested model, and wire-returned model and attach `AgenticResponseIdentity`. Current released OpenAI, Claude, and Gemini extensions omit returned model metadata, so an unchanged wrapper cannot pass.
- Disable SDK retries for new agentic models. If an SDK cannot do so, expose attempt observation and document the exact bound before its adapter can pass.
- Enforce default limits from `AgenticLimits`: 16 MiB request, 2 MiB event, 32 MiB aggregate response, 8 MiB decoded inline media per block, 1,024 blocks, 1 MiB error body, and 32 MiB fixture capture. Enforce hard caps of 64 MiB, 8 MiB, 128 MiB, 32 MiB, 4,096, 4 MiB, and 64 MiB respectively at config validation. Cap before JSON/base64 expansion when possible. Incrementally accumulate Generate output and abort/close on overflow.

## Verification and acceptance

- `GOWORK=off go mod tidy` leaves only intended changes and `GOWORK=off go list -m all` shows Eino v0.9.19.
- `GOWORK=off go test ./...` compiles the old and new APIs together at the end of W1 scaffolding.
- Root error tests prove `errors.Is`, direct and multiply wrapped `Classify` precedence, safe strings, typed fields, and no request/body/private values for unsupported and resource-limit errors.
- Table tests pass every unsupported content/option class through validation and assert zero fake-server requests.
- External-package tests use only public split/restore helpers. They JSON-scan the sanitized clone for absence of private sentinels, verify state contains the private sentinel, discard original runtime values, JSON encode/decode both parts, restore, and produce the exact second native request without mutating the source.
- Boundary/over-limit tests cover every configured limit, a never-terminating oversized stream, body close, producer cancellation, and absence of successful EOF.
- `scripts/check-dep-leaks.sh` still proves root and non-Codex packages do not import Codex auth modules.

Risk: extension upgrades can shift transitive SDKs and invalidate the Gemini construction benchmark. Rebaseline only from measured constructor behavior in the same PR; do not raise the threshold to hide accidental client reconstruction.
