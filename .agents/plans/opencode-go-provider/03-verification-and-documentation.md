# Verification and documentation

## WP5: integration gates and consumer instructions

Prerequisite: WP1–WP4. Proposed new files below live under new `opencodego/`, whose existing parent is the repository root. Modify existing `README.md`, `doc.go`, `options.go` comments, and `scripts/check-dep-leaks.sh`.

Create new `opencodego/integration_test.go` with deterministic HTTP fixtures. Use real auth client construction and the actual pinned Eino adapters rather than substituting model constructors. Table-drive all three protocols where the contract is common:

| Gate | Observable acceptance |
| --- | --- |
| Public entry points | Blank-import registration constructs `opencode-go`; direct NewChatModel produces Eino interface. |
| Generate/Advise | Correct native URL, model, system/user content, cap, text, and omitted/null/explicit-zero usage distinguished after actual SDK conversion. |
| Streaming | First text/tool fragment observable before server completion; final concatenation correct; native terminal marker required; completion does not wait for socket EOF. |
| Tools | Tool schema on request 1, assistant call retained, tool result correlated on request 2, final assistant text returned. |
| Sessions | Stable configured session across turns; operation context overrides; two concurrent conversations never exchange IDs. Empty override fails, even with fallback. |
| Identity/key | Host user-agent preserved; explicit key wins environment; owned headers cannot retain SDK or caller values. |
| Failure | Missing/invalid session produces zero base-transport requests; HTTPError survives SDK wrapping; receive errors propagate. |
| Lifecycle | Cancellation and deadlines preserved; cancel+Close ends silent reads and blocked sends; bodies close on all terminal paths. |
| Redirects/routes | A redirect target receives no request; Messages adapter rewrites only its exact known native route before auth. |

Use synthetic credentials and session markers only. Tests must not read developer credential stores or enable live tests based solely on the presence of a key. Verify Messages retry behavior: a permanent 400/401/403 makes one attempt by default; a replayable retryable failure makes at most three; explicit retry directives override the status policy; Retry-After is preserved; cancellation during a short server-supplied backoff prevents another network attempt. An immediate-failure transport does not bypass the SDK's sleep. Use short finite delays with generous timing bounds and document the existing cancellation-during-sleep limitation.

Extend existing `scripts/check-dep-leaks.sh` so root, Claude, OpenAI, Gemini, and Ollama imports exclude `opencode-auth-go`; the new backend excludes `codex-auth-go`; retain the existing non-Codex checks. Make temporary fixture cleanup reliable on failure. Import tests must build separately so global registration from another test does not hide a missing import. No root/backend import cycle is acceptable.

Documentation: add the backend row, package import, provider name, protocol strings, API key/environment precedence, base-root convention, and HTTPClient semantics to `README.md`. Describe SDK retry policy, safe typed-error inspection, Messages cap requirement, and operation-context session override. Update root package summary in `doc.go` to list the new backend. Document the supported text/function-tool scope and Responses limitations explicitly.

New `opencodego/doc.go` carries backend package documentation. New `opencodego/examples/toolcall/main.go` and `opencodego/examples/toolcall/README.md` live under proposed `opencodego/examples/toolcall/`. Demonstrate host-supplied model/protocol/user-agent/session, `WithTools`, one tool response and a final model turn, and `defer cancel()` plus stream Close. No automatic model selection or session printing. Keep the example bounded to two turns and synthetic coding input. Compile it in ordinary `go build ./...`; do not execute paid inference in default tests.

New `opencodego/chatmodel_live_test.go` is optional, skips unless `OPENCODE_GO_LIVE_TEST=1`, and then requires `OPENCODE_GO_API_KEY`, `OPENCODE_GO_CHAT_MODEL`, `OPENCODE_GO_MESSAGES_MODEL`, `OPENCODE_GO_RESPONSES_MODEL`, and `OPENCODE_GO_USER_AGENT`. The explicitly enabled test may create synthetic ephemeral conversation IDs for the test only, never for library inference. Use strict request/time bounds, no fallbacks, and no secret/response-body diagnostics. Exercise one Generate and one two-turn tool loop per protocol with streaming in that loop. Report the selected protocol and pass/fail only. Current model support is operator-selected, not inferred from the catalog.

Verification commands from repository root after implementation:

```sh
gofmt -l opencodego options.go doc.go
go build ./...
go test ./...
go test -race ./opencodego
go vet ./...
scripts/check-dep-leaks.sh
bash scripts/check-gemini-benchmark.sh
golangci-lint run
```

Use the CI Go 1.25.x and 1.26.x matrix in `.github/workflows/ci.yml`. Run `go mod tidy` only in the clean implementation checkout, then require no diff in `go.mod`/`go.sum` after committing intended dependency changes. Preserve the original checkout's unrelated `go.sum` edits. No CI workflow expansion is needed unless existing discovery fails to include the new package/example.

Optional live command, only after explicit runtime authorization and configuration:

```sh
OPENCODE_GO_LIVE_TEST=1 go test ./opencodego -run '^TestLiveOpenCodeGo$' -count=1 -timeout=6m
```

Acceptance: all deterministic gates pass on both CI toolchains; ordinary tests never require network credentials; docs/example match implemented exported names; report live validation separately as passed, failed, or not run. Do not claim live verification based on auth-library fixtures.
