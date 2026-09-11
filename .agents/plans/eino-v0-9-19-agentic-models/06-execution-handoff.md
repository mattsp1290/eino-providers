# Execution handoff

## Work packages

| Order | Package | Prerequisites | Bounded result |
| --- | --- | --- | --- |
| 1 | W1 contract and dependency baseline | none | Eino v0.9.19 compiles; errors, constructor signatures, matrix schema, retry rule, and fixtures are fixed. |
| 2 | W2 OpenAI first deliverable | W1 | One real OpenAI Responses `AgenticModel` passes native continuation and stream tests. |
| 3a | W3 Claude | W2 harness | Local zero-retry Messages codec passes native conformance behind `claude.NewAgenticModel`. |
| 3b | W3 Gemini | W2 harness | Released Gemini agentic extension passes local conformance behind `gemini.NewAgenticModel`. |
| 4a | W4 OpenAI-Codex | W1, W2 lifecycle | Direct Codex Responses agentic path preserves auth sentinels and encrypted continuation. |
| 4b | W4 OpenCode Go | W1, W2 lifecycle | Three explicit protocol rows use native `AgenticMessage` codecs and no fallback. |
| 4c | W4 Ollama | W1, W2 lifecycle | Native `/api/chat` agentic subset has honest positive and rejection fixtures. |
| 5 | W5 verification/publication | W2–W4 | Matrix, docs, CI, clean remote pin, and canonical response are complete. |

W3a/W3b and W4a/W4b/W4c can run in parallel after their shared contracts stabilize. Do not edit shared fixture/error files concurrently without first isolating those changes. The first implementation action is to update the Eino pin and add failing W1 compile/error/dispatch tests; do not start by editing provider codecs.

## Package-level surfaces and commands

### W1

Change existing `go.mod`, `go.sum`, `errors.go`, `errors_test.go`, `scripts/check-dep-leaks.sh`, and ADR index. Add proposed `docs/adr/0008-agentic-model-boundary.md`, `docs/agentic-capabilities.md`, and `internal/agenticfixture/`. Add proposed configs/constructor declarations only when the package PR also supplies their tests.

Verify:

```bash
GOWORK=off go mod tidy
GOWORK=off go list -m all
GOWORK=off go test ./...
```

Acceptance: Eino resolves exactly v0.9.19; unsupported/resource-limit errors classify safely through `Classify`; raw-response probes recover returned model identity; public split/restore helpers produce sentinel-free clones and survive JSON type erasure; the fixed draft matrix has a disposition for every requested cell; negative fixtures observe zero requests; limit overflow closes/cancels; classic packages still compile without a compatibility promise.

### W2

Add the proposed files listed in [02-openai-first-deliverable.md](02-openai-first-deliverable.md). Verify `GOWORK=off go test -race ./openai` plus the full suite. Acceptance: native two-request continuation, all declared OpenAI positive rows, terminal error behavior, and immutable concurrency pass.

### W3

Add the proposed Claude/Gemini files in [03-claude-and-gemini.md](03-claude-and-gemini.md). Verify:

```bash
GOWORK=off go test -race ./claude ./gemini
bash scripts/check-gemini-benchmark.sh
bash scripts/check-dep-leaks.sh
```

Acceptance: Gemini adapter-backed claims and Claude local-codec claims are proved through fake native endpoints; unsupported common options reject before dispatch; SDK retries are zero; returned identity is raw-response-derived; private signatures round-trip through public helpers.

### W4

Add the proposed package-local codecs/tests/examples in [04-codex-opencode-ollama.md](04-codex-opencode-ollama.md). Verify:

```bash
GOWORK=off go test -race ./openaicodex ./opencodego ./ollama
bash scripts/check-dep-leaks.sh
```

Acceptance: each protocol has an honest subset, exact IDs where native, explicit Ollama synthetic-correlation semantics, opaque state where native, actual identity/usage/finish, one-attempt behavior, bounded streams, and zero-dispatch rejection.

### W5

Finalize existing docs/CI/scripts and proposed `testdata/agentic-consumer/` plus the external response. Verify all commands in [05-verification-and-publication.md](05-verification-and-publication.md), then run the fresh standard-proxy consumer against the published pin.

Acceptance: every matrix cell resolves to evidence; all repository references and commands are valid; the response states exact pins and separates acceptance from implementation and consumer completion.

## Integration and regression gates

- `schema.ConcatAgenticMessages` is the only final stream concatenation and runs once per collected stream. The collector enforces 32 MiB/1,024-block defaults incrementally and does not retain an unbounded chunk slice.
- Late errors win over EOF/success. Nil/empty successful outputs are errors.
- Closing/canceling a stream closes the native reader/body and terminates the producer.
- Hidden provider retries are disabled. Runtime retry accounting therefore remains one-to-one with dispatches.
- No provider production package imports `github.com/mattsp1290/eino-agent`.
- Root/non-Codex dependency isolation, Gemini construction bounds, classic APIs, and existing auth sentinels remain tested.
- No credentials, session IDs, raw request bodies containing private values, or opaque continuation appear in docs, snapshots, errors, or logs.
- Public split/restore helpers are the only supported provider-state boundary; clean-consumer tests cannot use package internals or unexported extension keys, and sanitized public clones contain no opaque continuation.

## Definition of done

- All seven proposed constructors compile at a remotely downloadable immutable pin.
- The finalized matrix includes separate cells for every requested block, metadata, search, approval, continuation, identity, retry, and lifecycle capability for every provider/protocol row.
- Every supported feature has a positive native-byte fixture; every unavailable feature rejects before dispatch.
- Generate and concatenated Stream agree; continuation crosses encode/restore with exact native identities.
- Full local/CI/clean-consumer gates pass and opt-in live status is explicit.
- README, ADR, changelog, examples, package docs, and canonical response agree on support and ownership.
- Implementation Beads are closed or deferred with owners, `bd dolt push` succeeds, Git commits are pushed, and `git status` reports the branch up to date with origin.

## Deferred work

No requested provider capability is silently deferred. Consumer-side persistence and W3/W8 adoption remain owned by `eino-agent`. Live verification may remain unrun without blocking deterministic provider completion, but the response must say so. New provider backends, a root agentic factory, automatic failover, provider-state persistence, and UI presentation remain out of scope.
