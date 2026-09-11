# W5 — Matrix verification and publication

## Goal and prerequisites

Prove the complete declared capability surface, publish honest consumer evidence, and answer the canonical request. Prerequisites: W1–W4.

## Change surface

- `docs/agentic-capabilities.md`: new under existing `docs/`; finalized provider/protocol matrix with fixture links and public/private field classification.
- `README.md`, `CHANGELOG.md`, root `doc.go`, and `opencodego/doc.go`: existing; document exact constructors, host responsibilities, unsupported-capability errors, retry behavior, lifecycle, rollback, and live-test commands.
- `claude/doc.go`, `openai/doc.go`, `gemini/doc.go`, `ollama/doc.go`, and `openaicodex/doc.go`: new under their existing package directories; document each native subset and continuation boundary.
- `.github/workflows/ci.yml`: existing; add agentic fixture and clean-consumer gates where ordinary `go test ./...` is insufficient.
- `scripts/check-dep-leaks.sh`: existing; cover all new package dependency boundaries.
- `testdata/agentic-consumer/`: new under existing repository root; clean external-module source and script with no local `replace` in publication mode.
- `$HOME/.agents/projects/eino-providers/responses/`: new directory anchored at existing `$HOME/.agents/projects/eino-providers/`; add new `2026-09-10-eino-v0-9-19-agentic-models.md` when execution reaches acceptance. Record contract acceptance separately from implementation/pin verification.

## Matrix verification

Generate or validate the documentation matrix from a test-owned manifest so documentation cannot claim a supported cell without a fixture identifier. For every provider/protocol:

- exercise every supported block and option through actual request/parser code;
- exercise every rejected block and option and assert zero dispatch;
- cover all five nested function-result media block types independently;
- compare Generate with one concatenation of the full Stream;
- run native tool call -> atomic sanitized/state split -> type-erased encode/reopen -> restore -> result -> second request;
- assert IDs, block order/index, actual model/provider/protocol, usage and finish/status;
- exercise simultaneous immutable clients/options;
- exercise malformed/truncated stream, late terminal error, cancellation, reader close, and body close; and
- scan serialized public/log/error surfaces for private sentinels.
- enforce each W1 quantitative limit at the boundary and one unit beyond it, including a never-terminating oversized stream; assert typed classification, body close, producer cancellation, and no successful EOF.

Do not make a fixture positive by manually constructing the output `AgenticMessage`. The fake server sends native bytes and the public constructor parses them.

## Consumer integration contract

Before publishing, resolve `EINO_AGENT_DIR` to a checkout of `github.com/mattsp1290/eino-agent`. A test-only integration may import its public candidate private-state codec after W3 exists. Production provider packages must not import it.

The integration fixture uses a real provider constructor with fake native HTTP/SSE, calls only the public provider split/restore helpers, persists only their sanitized public clone and opaque state in the proper stores, type-erases both through JSON, reopens the consumer state, and performs a second native request with exact IDs and opaque state. It scans the public JSON for absence of every private sentinel and proves the source message was not mutated. Provider-internal access is forbidden. If the consumer candidate is not yet published, provider publication may proceed with the external-package split/restore fixture, but the response must keep consumer W3/W8 integration explicitly blocked rather than claiming full cross-repository completion.

## Clean module and release proof

1. Run formatting and `GOWORK=off go mod tidy`; stage/commit the intended module changes, rerun tidy, then require `git diff --exit-code -- go.mod go.sum`. Run `GOWORK=off go vet ./...`, `GOWORK=off go test -race ./...`, dependency-leak checks, Gemini benchmark, and repository lint.
2. Commit and push the implementation branch; merge according to repository policy.
3. Publish an immutable tag or pseudo-version reachable from the standard Go proxy.
4. In a fresh temporary directory, use `GOWORK=off GOPROXY=https://proxy.golang.org` to initialize a consumer, `go get` the exact version, verify checksums/origin hash, build, and run one fake-native OpenAI agentic continuation without `replace`.
5. Write the response with exact Eino/eino-ext/provider pins, constructors, matrix link, commands/results, live-test status, consumer status, and remaining gaps. Request creation or plan completion alone is not acceptance evidence.

## Rollout and rollback

There are no active consumers, compatibility obligations, stored-data migrations, or feature flags. Rollout is opt-in by importing a concrete package and calling its new constructor. Roll back a host by reverting its dependency/constructor selection. Roll back a bad provider release by publishing a corrected immutable version and updating the response; never retag an existing version.

## Acceptance

- No supported matrix cell lacks a deterministic native fixture.
- No unsupported input or option reaches transport.
- All ordinary CI gates pass without credentials or live services.
- Live gates report passed, failed, or unrun; unrun is never represented as supported evidence.
- The standard-proxy clean consumer succeeds against the exact remote pin.
- The canonical response distinguishes contract acceptance, provider implementation completion, and consumer integration completion.
