# OpenCode Go provider

Status: Ready. Implementation has not occurred.

## Application context

```json
{
  "application_context": {
    "has_active_users": false,
    "backward_compatibility_required": false,
    "feature_flags": "not-applicable",
    "confirmation_digest": "2a36ed485b1e5954d44abb44f8eec0fc21bca0e5601cb83a4aaf8060dbf8bc50",
    "confirmed_at": "2026-09-08T21:23:41.465Z"
  }
}
```

The user confirmed “1 no users, 2 no backwards compat, 3 no feature falgs”. No compatibility migration or feature flags are required. Other backends remain regression targets because changing them is outside this request. No persisted data changes are planned. Rollout is importing and selecting the new provider; rollback is reverting its commits and dependency addition.

## Outcome and scope

Request: “$implementation-plan we need to support ~/git/opencode-auth-go to support OpenCode Go as a new provider ./”.

Change type: new backend integration across public options, provider registration, Eino chat models, HTTP transport composition, tests, and documentation.

Proposed result: an opt-in `opencodego` package registered as `opencode-go`, with `Provider.Advise` and a `model.ToolCallingChatModel` constructor. Explicit Chat Completions, Messages, and Responses selection covers the three native routes exposed by the supplied auth library. Each protocol must pass text generation, streaming, and a two-turn function-call fixture using the real auth transport. Usage absence stays distinguishable from zero usage. Cancellation, session isolation, and error classification must survive the complete adapter chain.

Non-goals: account login, key storage, session generation/persistence, subscription checks, model routing/allowlists, pricing, automatic fallback, built-in server tools, multimodal Responses input, a new factory architecture, changes in consuming applications, or implementing this plan during planning. Existing adapter-supported multimodal behavior is incidental, not a cross-protocol promise.

## Verified evidence

- `registry.go` and `factory.go` implement opt-in backend registration. `provider.go` defines single-shot messages and usage extraction. `options.go` has no user-agent, session, or protocol field.
- `openai/chatmodel.go` and `claude/chatmodel.go` wrap Eino tool-calling adapters. The pinned OpenAI adapter speaks Chat Completions. The Anthropic SDK appends `v1/messages`, whereas the auth library expects its normalized root plus `/messages`.
- `openaicodex/chatmodel.go` and `openaicodex/responses.go` contain Responses conversion patterns, but depend on Codex authentication, endpoint constants, mandatory streaming, and Codex-specific reasoning/error behavior. They are references, not a reusable public OpenCode adapter.
- External module `github.com/mattsp1290/opencode-auth-go`, revision `a3f44cca7a18028a4e6e90b021438923988539c6`, provides `NewClient`, `Options`, `Endpoint`, `HTTPClient`, `WithSessionID`, `ListModels`, and `DecodeHTTPError`. Its `README.md`, `client.go`, `transport.go`, `endpoint.go`, `errors.go`, and `integration_test.go` establish the contract.
- The remote main branch matched that revision. `go list -m -json github.com/mattsp1290/opencode-auth-go@a3f44cca7a18028a4e6e90b021438923988539c6` resolved `v0.0.0-20260908211055-a3f44cca7a18` with Go 1.25.5. Pin this exact version, not a filesystem replacement.
- The auth transport validates destinations, owns credential and session headers, disables HTTP redirects, and leaves response decoding to consumers. `UserAgent` is required; session comes from each operation's context or configured fallback. The catalog does not advertise protocols or tool capabilities.
- [OpenCode Go documentation](https://opencode.ai/docs/go/) inspected 2026-09-08 requires host identity and conversation continuity. Model availability is mutable; do not encode a current model list into dispatch logic.
- Local planning baseline is `3e0069d028bc946deaa96ea2dd6bff76b4118c38`. At inspection, `origin/main` was three commits ahead, ending at `f62b013`, with Codex stream panic containment. Preserve those changes when integrating.
- Existing unrelated work: modified `go.sum`, untracked `.agents/plans/incremental-toolcall-args-streaming/`, and `.agents/requests/`. These are not this plan's output.

For external source inspection set `OPENCODE_AUTH_GO_ROOT` to a checkout of the module at the pinned revision, or resolve the downloaded module directory with `go mod download -json`. The user-supplied sibling checkout was inspected. References above are relative to that module, not this repository.

## Decisions and target flow

Host configuration/context → proposed `opencodego` facade with operation-local observations → selected Eino protocol adapter → local response observer/error sanitizer → `opencode-auth-go` authenticated transport → native OpenCode Go route.

1. Keep reusable credential and session enforcement in its established owner, `opencode-auth-go`. Keep Eino schema conversion here. No new repository or cross-repository request is necessary: the required contract already exists at a resolvable pin.
2. Require explicit protocol selection. Do not infer it from a model name or catalog. Keep root options dependency-neutral with a string selector, and expose typed protocol constants only in the new backend package.
3. Reuse pinned Eino Chat Completions and Messages adapters. Implement a bounded native Responses adapter within the new package. Do not import `openaicodex`, change its parser, or extract a shared Responses framework in this work.
4. Require caller identity and caller-owned sessions. Do not substitute a generic provider user-agent or generate a session per HTTP request.
5. Bound and sanitize HTTP errors before SDK decoding, retaining `opencodeauth.HTTPError` in operation-local state while preserving HTTP status and retry headers. Observe successful bodies incrementally to recover usage presence erased by the pinned adapters; do not delay streaming delivery until the whole response arrives. Retain existing SDK retry behavior explicitly; do not add a provider retry or protocol fallback loop.

## Risks, assumptions, and gates

No unresolved user decisions remain. Non-blocking assumption: all three native protocols and text/function tools are the useful provider scope; this does not promise every model supports every protocol or tool.

The main engineering risks are Anthropic base-URL composition, typed-error preservation through SDK retries, Responses stream termination/tool assembly, and host cancellation on abandoned streams. The work files define fixtures and stop conditions for each. If an adapter cannot satisfy a required contract, stop that package and record the concrete missing seam in Beads; do not silently weaken acceptance or upgrade dependencies broadly.

Live service validation is optional and requires explicit credentials and model selection. Deterministic tests establish the implementation gate; live model compatibility must not be claimed without running the opt-in gate. No paid inference is authorized by this planning task.

## Document map

- [01-provider-and-transport.md](01-provider-and-transport.md): dependency pin, public configuration, registration, transport and error ownership.
- [02-protocol-adapters.md](02-protocol-adapters.md): adapter construction, Responses schema and stream contracts, tool and context lifecycle.
- [03-verification-and-documentation.md](03-verification-and-documentation.md): deterministic integration matrix, CI, dependency isolation, examples, optional live gate.
- [04-execution-handoff.md](04-execution-handoff.md): ordered work packages, integration gates, definition of done, and rollback.

Execution tracking uses Beads. Planning issue: `eino-providers-jug`. Implementation issue: `eino-providers-l8a`.
