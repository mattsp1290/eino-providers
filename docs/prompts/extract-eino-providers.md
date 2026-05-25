# Plan: Extract `eino-providers` from advisor and local-symphony

Planning document for lifting the two existing `cloudwego/eino`-based ChatModel adapter stacks - advisor's single-shot `Provider` interface family and local-symphony's per-turn `ToolCallingChatModel` factory - into a shared Go module at `github.com/mattsp1290/eino-providers`. The repo already exists with an MIT `LICENSE`; nothing else is in tree yet. The plan describes what to write, in what order, and where each piece comes from. No Go is generated here.

## 1. Goals and non-goals

**Goals.**

- A single import path both advisor and local-symphony depend on for ChatModel construction across Claude, OpenAI (API key), OpenAI-Codex (OAuth), Gemini, and Ollama backends.
- Carry both shapes the two consumers use today: advisor's single-shot `Provider` interface and local-symphony's `ChatModelFactory` shape returning `eino/components/model.ToolCallingChatModel` for ReAct loops.
- Preserve advisor's behavioural quirks: the `readOpenAIAuthMode` injection seam (`factory.go:10`), sentinel-based error classification (`ErrProviderInit`, `ErrProviderTimeout`, `ErrProviderAPI`, `ErrAuthNotLoggedIn`, `ErrAuthPlanNotIncluded`, `ErrAuthQuotaExceeded`), the `WrapInitError` / `initErr` / `authErr` wrapper types, and the `classifyCodexError` API-error-code mapping (`openai_codex.go:96-114`).
- Preserve symphony's `PingOllama` health probe and the `ErrSparkUnreachable` sentinel (renamed - section 5).
- Keep the dependency surface opt-in: a consumer that only wants Claude must not transitively pull `codex-auth-go` and its OAuth + credstore code through the top-level factory.

**Non-goals.**

- OAuth flow, PKCE, credstore, RFC 7009 revocation, and the Codex `RoundTripper` itself - all in `codex-auth-go`. `eino-providers/openaicodex` only imports the `HTTPClient(ctx)` accessor and `ErrNotLoggedIn`.
- OpenTelemetry instrumentation - scoped to `agent-otel` (research doc 05 section 3).
- Tool definitions, compose-tool wrappers, fileops / search / shell - scoped to `eino-tools`. Tool binding via `WithTools` is the caller's job.
- CLI entry points, `login` / `logout` subcommands, env-var parsing beyond `ADVISOR_OPENAI_AUTH_MODE` routing - stay in each consumer.

## 2. Dual-shape API

Both shapes coexist as first-class citizens. Supporting both - rather than picking one and adapting the other consumer - is forced by the asymmetry of the call sites: advisor never holds a `ChatModel` between calls (it constructs per-request, except for Gemini's cached `genai.Client`), while local-symphony holds a `model.ToolCallingChatModel` for the lifetime of a turn and rebinds tools via `WithTools`.

**`Provider` interface (single-shot).** Lifted verbatim from `~/git/advisor/internal/advisor/provider.go:48-50`:

```go
type Provider interface {
    Advise(ctx context.Context, system, user string, maxTokens int) (text string, usage Usage, err error)
}
```

The contract clauses (honour `ctx` cancellation, clamp `maxTokens` at the eino layer using the provider-specific field, return `Usage{Available: false}` when token counts are absent, prefix wrapped errors with the backend name) come along verbatim in the package doc.

**`ChatModelFactory` (per-turn, for ReAct callers).** Each backend sub-package exports a `NewChatModel(ctx, Config) (model.ToolCallingChatModel, error)`. There is no top-level `ChatModelFactory` interface in `v0.1.0`; the interface gets formalised in `v0.2.0` once both consumers have lived with the shape.

Each backend can implement either shape or both. Claude, OpenAI, OpenAI-Codex, and Gemini implement `Provider` in `v0.1.0` (lift from advisor). Ollama implements only `NewChatModel` in `v0.1.0` (lift from symphony); a `Provider` adapter waits for a single-shot Ollama caller to appear.

## 3. Module layout

File-by-file. The root package is `einoproviders` (no underscore, no hyphen - Go convention). Module path is `github.com/mattsp1290/eino-providers`.

- `provider.go` - `Provider` interface, `Usage` struct, exported `ExtractUsage` (case-flipped from advisor's unexported `extractUsage`).
- `factory.go` - `NewProvider(ctx, name, model, opts Options) (Provider, error)` dispatches via the registry. No direct imports of any sub-package.
- `errors.go` - error sentinels (`ErrProviderInit`, `ErrProviderTimeout`, `ErrProviderAPI`, `ErrUnknownProvider`, `ErrBackendUnreachable`), the `initErr` and `authErr` wrapper types, `WrapInitError(err)`, and `Classify(err) ErrorClass` for callers that want a single switch over error kinds.
- `registry.go` - `RegisterProvider(name string, ctor ProviderCtor)`, package-level `providers map[string]ProviderCtor` guarded by `sync.RWMutex`, and `lookupProvider(name)` used by `factory.go`.
- `options.go` - the unified `Options` struct (see section 6).
- `claude/` - `claude.go` carries `Provider` (lifted from `~/git/advisor/internal/advisor/claude.go`), `NewClaudeProvider`, `NewClaudeProviderWithBaseURL`. The `init()` calls `einoproviders.RegisterProvider("claude", ...)`.
- `openai/` - `openai.go` carries `OpenAIProvider`, `NewOpenAIProvider`, `NewOpenAIProviderWithBaseURL` (lifted from `~/git/advisor/internal/advisor/openai.go`). The `init()` registers `"openai"`.
- `openaicodex/` - `openai_codex.go` carries `OpenAICodexProvider`, `classifyCodexError` (lifted from `~/git/advisor/internal/advisor/openai_codex.go:96-114`), the `codexHTTPClient` seam. Imports `github.com/mattsp1290/codex-auth-go` for `HTTPClient(ctx)` and `ErrNotLoggedIn`. The `init()` registers `"openai-codex"`.
- `gemini/` - `gemini.go` carries `GeminiProvider`, `NewGeminiProvider` (lifted from `~/git/advisor/internal/advisor/gemini.go`). The `init()` registers `"gemini"`. The cached `*genai.Client` lives on the provider as today; reconstruction-cost benchmark moves with it (section 12).
- `ollama/` - `ollama.go` carries `NewChatModel(ctx, Config) (model.ToolCallingChatModel, error)` (lifted from `~/git/local-symphony/internal/worker/agent/models.go:133-141`), the `parseKeepAlive` helper, the per-instance config struct. `health.go` carries `PingOllama` lifted from `~/git/local-symphony/internal/worker/agent/health.go:32-60` and `ErrBackendUnreachable` (renamed from `ErrSparkUnreachable`; rationale in section 5). The `init()` registers `"ollama"` only as a `ChatModelFactory`-shaped entry; no `Provider` adapter in v0.1.0.

A `doc.go` at the root spells out the registry pattern, why side-effect imports are required, and links to consumer migration notes.

## 4. Registry design (critical)

The root `factory.go` MUST NOT import any of the backend sub-packages directly. If it did, every consumer of `NewProvider` would transitively pull every backend's SDK and (through `openaicodex`) `codex-auth-go` plus its OAuth, browser, and credstore dependencies - even a consumer that only wants Claude. That defeats the headline benefit of the extraction (per research doc 05 section 2: "the OAuth dependency surface stays opt-in").

The pattern is the one `database/sql` uses for drivers:

1. Each sub-package has an `init()` that calls `einoproviders.RegisterProvider(name, ctor)`. The `ctor` closure captures the sub-package's `New<Backend>Provider` constructor.
2. Consumers do side-effect imports for the backends they want:
   ```go
   import (
       einoproviders "github.com/mattsp1290/eino-providers"
       _ "github.com/mattsp1290/eino-providers/claude"
       _ "github.com/mattsp1290/eino-providers/openai"
       // no openaicodex import => no codex-auth-go in deps
   )
   ```
3. `factory.NewProvider(ctx, name, model, opts)` looks up `name` in the registry. If no `init()` ran for that name (because the consumer did not import the sub-package), the call returns `ErrUnknownProvider`.

The doc comment on `RegisterProvider` says loudly: "Call from `init()` only. Concurrent registration after package init is supported but not recommended; the registry uses a `sync.RWMutex` for safety, not for orchestration." The error returned by duplicate registration is a `panic` to mirror `database/sql.Register` - duplicate driver names are a programming error.

## 5. Per-backend quirks

The four advisor adapters each use the eino-ext `ChatModelConfig` for their backend, and the field shapes do not agree. Documenting the asymmetry up front prevents the `Options` struct (section 6) from accidentally lossy-collapsing real semantic differences.

**Claude** (`~/go/pkg/mod/github.com/cloudwego/eino-ext/components/model/claude@v0.1.17`). `MaxTokens int` (literal value, not a pointer; `0` is sent as `0`). `BaseURL *string` (pointer; nil means default endpoint). Advisor stores `baseURL *string` directly in `ClaudeProvider` (`claude.go:15`). Construction is cheap: 146 ns/op, 48 B, 1 alloc (cited from `~/git/advisor/benchmarks/coldstart.txt`).

**OpenAI** (`~/go/pkg/mod/github.com/cloudwego/eino-ext/components/model/openai@v0.1.13`). The eino-ext `ChatModelConfig` actually has **both** `MaxTokens *int` (line 131 of `chatmodel.go`) and `MaxCompletionTokens *int` (line 134). Advisor's `openai.go:35` uses only `MaxCompletionTokens`. `MaxTokens` exists in the upstream config but is deprecated in the OpenAI API in favour of `max_completion_tokens` for newer models; the extracted adapter should continue to set `MaxCompletionTokens` only. `BaseURL string` (plain, not pointer; empty means default). Construction is cheap: 138 ns/op, 48 B, 1 alloc.

**OpenAI-Codex** (uses the same eino-ext openai `ChatModelConfig`). The codex adapter MUST NOT set `MaxCompletionTokens` (the Codex endpoint manages output length server-side - see the comment at `~/git/advisor/internal/advisor/openai_codex.go:57-63`). It MUST NOT set `BaseURL` (the `codexTransport` rewrites the URL on every request). It MUST inject `HTTPClient: <codex-authed client>` and set `APIKey: "codex-oauth-dummy"` because eino-ext rejects an empty API key string at construction time but the transport replaces the `Authorization` header anyway. These three constraints are not optional and need to be in the adapter's doc comment, not just in code.

**Gemini** (`~/go/pkg/mod/github.com/cloudwego/eino-ext/components/model/gemini@v0.1.30`). The eino-ext `Config` takes a pre-built `*genai.Client`. Construction is ~14x more expensive than the others at ~2 us/op, 912 B, 18 allocs because `genai.NewClient` eagerly builds nine sub-clients (Models, Live, Caches, Chats, Operations, Files, Batches, Tunings, AuthTokens). The number comes from `~/git/advisor/benchmarks/coldstart.txt` lines 25-34 and the longer commentary in `~/git/advisor/internal/advisor/factory_bench_test.go:15-29`. `MaxTokens *int` (pointer; nil means default). This cost is the reason `GeminiProvider` caches `*genai.Client` at construction time instead of rebuilding per request. The extracted adapter preserves this caching.

**Ollama** (`~/go/pkg/mod/github.com/cloudwego/eino-ext/components/model/ollama@v0.1.9`). `BaseURL string`, `Model string`, `Timeout time.Duration`, `KeepAlive *time.Duration`. No API key. The Ollama backend wants a `/api/tags` ping before first use (today via `PingOllama` in `~/git/local-symphony/internal/worker/agent/health.go:32-60`) capped at `healthProbeMaxTimeout = 5 * time.Second` so a long `Generate` timeout does not stall startup. The probe verifies a 2xx status but does not validate the JSON body; a non-Ollama HTTP server returning 200 to `/api/tags` would pass.

`ErrSparkUnreachable` in `models.go:24` is renamed to `ErrBackendUnreachable` in the extracted module. "Spark" is a local-symphony product term (the on-prem Ollama box) that has no business in a shared library. Symphony adopts the new sentinel on its end and may keep a local alias if it wants string-stable telemetry.

## 6. Unified `Options` struct

The `Options` struct reconciles the per-backend asymmetry by accepting only pointer-typed fields uniformly, so that "not set" is unambiguously `nil`. The per-backend constructor dereferences and applies the backend's native type, including any literal-vs-pointer flip (Claude's `MaxTokens int` is the only such case today).

```go
type Options struct {
    APIKey     string         // claude, openai (API-key path), gemini; ignored by ollama
    BaseURL    *string        // claude, openai, ollama; MUST be nil for openai-codex
    MaxTokens  *int           // claude, openai, gemini; ignored by openai-codex
    KeepAlive  *time.Duration // ollama only
    HTTPClient *http.Client   // openai-codex (required); optional for others
    // GenaiClient is the pre-built *genai.Client for the gemini backend.
    // When non-nil, NewProvider skips genai.NewClient and reuses this handle.
    GenaiClient *genai.Client
}
```

A consumer that wants the same per-call semantics as advisor today passes `BaseURL: nil`, `MaxTokens: &mt`, `KeepAlive: nil`, leaves `HTTPClient` and `GenaiClient` nil. The `openai-codex` constructor returns a validation error if `BaseURL != nil`, `MaxTokens != nil`, or `HTTPClient == nil`. The `gemini` constructor calls `genai.NewClient` when `GenaiClient == nil` (the cold-start path) or reuses the passed handle (the warm path - useful for tests and for callers who want to share one client across multiple providers).

This is a deliberate shift away from the per-backend "with X" constructor functions advisor uses today (`NewClaudeProviderWithBaseURL`, `NewOpenAIProviderWithBaseURL`). Those stay for backward-compatible advisor consumption but become thin shims that build an `Options` and call the unified constructor. Symphony's per-call config also collapses into `Options` once it adopts `v0.2.0`.

## 7. Error sentinels and promotion plan

Today the auth sentinels and the Codex error classifier are split across two packages in advisor:

- `ErrAuthNotLoggedIn`, `ErrAuthPlanNotIncluded`, `ErrAuthQuotaExceeded` are declared in `~/git/advisor/internal/advisor/factory_seam.go:61-73`.
- `classifyCodexError` and the two API-code string constants (`usage_not_included`, `insufficient_quota`) are in `~/git/advisor/internal/advisor/openai_codex.go:82-114`.
- The wrapper types `authErr` and `initErr` are in `factory_seam.go:78-130`.
- `codexauth.ErrNotLoggedIn` is in `~/git/advisor/internal/advisor/codexauth/`.

The promotion split, per research doc 05 section 1:

- **Move to `codex-auth-go`:** `ErrPlanNotIncluded`, `ErrQuotaExceeded`, `ErrRefreshFailed`. These are nearest to the source of the error codes (the Codex API response). `ErrNotLoggedIn` already lives in `codexauth` today and stays there. The doc 05 names them without the `Auth` prefix (`ErrPlanNotIncluded` not `ErrAuthPlanNotIncluded`); the `codex-auth-go` exports are the canonical names. Advisor's `ErrAuth*` names become deprecated aliases in advisor's tree, which keeps the byte-equal MCP replay gate stable for one cycle.
- **Stay in `eino-providers`:** `ErrProviderInit`, `ErrProviderTimeout`, `ErrProviderAPI`, the `initErr` and `authErr` wrapper types, `WrapInitError`, and a new `Classify(err) ErrorClass` helper that maps any wrapped error to one of the public classes. The `authErr` wrapper still needs the sentinel-first `Unwrap()` ordering (see `factory_seam.go:92-97`).
- **Lives in `eino-providers/openaicodex`:** the `classifyCodexError` helper, the two string constants, the seam variable `codexHTTPClient`. The helper wraps with the sentinel imported from `codex-auth-go`, so the public surface advisor consumes is the same `errors.Is(err, codexauth.ErrPlanNotIncluded)` it has today.

This split has one downside worth surfacing: a consumer doing `errors.Is(err, einoproviders.ErrAuthPlanNotIncluded)` won't compile against the new module, because that name no longer exists. The migration is "switch the import path of the sentinel from advisor's package to `codex-auth-go`." Advisor's core classification (`core.wrapProviderError`) does this switch in the same PR that adopts the new modules.

## 8. `Usage` extraction

`extractUsage` is unexported today at `~/git/advisor/internal/advisor/provider.go:21-31`. Promoting it to `ExtractUsage` is a one-character case flip plus updating callers. The call sites in advisor are:

- `~/git/advisor/internal/advisor/claude.go:49`
- `~/git/advisor/internal/advisor/openai.go:50`
- `~/git/advisor/internal/advisor/openai_codex.go:77`
- `~/git/advisor/internal/advisor/gemini.go:57`
- `~/git/advisor/internal/advisor/usage_test.go:19, 34, 45, 61, 71`

Four production call sites, five test call sites. The four production sites are themselves moving into `eino-providers` sub-packages, so they get the case flip "for free" as part of the lift. The five test sites in `usage_test.go` come over to the root of `eino-providers` (renamed `usage_test.go`) and get the case flip there.

No call site in `~/git/advisor/internal/` outside `internal/advisor/` references `extractUsage` - the grep is clean above the package boundary. The change is therefore self-contained to the lift PR and does not need a separate "deprecate unexported name" cycle.

## 9. Migration plan (advisor)

Step by step. Each step is one or more PRs against advisor's tree; the eino-providers side is described in section 14.

a. **Ship `codex-auth-go v0.1.0` first.** Release-ordering constraint, not a parallelism opportunity. `openaicodex` imports `codex-auth-go`, so `eino-providers v0.1.0` cannot tag earlier.

b. **Tag `eino-providers v0.1.0`** with the skeleton plus all five backend sub-packages. The `openaicodex` sub-package depends on `codex-auth-go v0.1.0`.

c. **Advisor PR: delete `internal/advisor/{provider,factory,claude,openai,openai_codex,gemini}.go` and update call sites.** Every public name becomes `einoproviders.Foo`. Tests under `internal/advisor/{factory_test,factory_seam_test,factory_bench_test,usage_test}.go` either move to `eino-providers` (see section 12) or stay as wired-up integration smoke tests.

d. **`codexauth/chatmodel.go` placement is a v0.1.0 punt.** Advisor has two Codex paths today: the eino-ext path (`openai_codex.go`, lifted here) and the raw-HTTP path (`codexauth/chatmodel.go`'s `NewWrapperChatModel`, a `BaseChatModel` that demuxes the Codex Responses-API SSE stream directly into `eino/schema.Message`). Per doc 05, `codex-auth-go` stays eino-free, so `chatmodel.go` cannot move there. Two defensible homes:
   - **Option A:** lift to `eino-providers/openaicodex` as an alternate constructor (`NewWrapperProvider`). Canonical place for "Codex-via-eino".
   - **Option B:** keep in advisor as a private fallback for cases the eino-ext path can't handle. No new module surface.

   v0.1.0 does NOT lift the raw-HTTP wrapper; filed as open question (section 15).

e. **`internal/advisor/codexauth/` becomes a deprecated shim** once `codex-auth-go v0.1.0` ships, calling into the new module. Then `eino-providers/openaicodex` imports `codex-auth-go` directly (not advisor's shim).

## 10. Migration plan (local-symphony)

Step by step. The first step is a hard blocker; the rest follow it linearly.

a. **Phase 0: bump eino `v0.7.13` to `v0.8.13` and Go `1.25.0` to `1.25.5`** to match advisor and the new module. This is the Phase 0 blocker from `~/docs/eino/04-integration-plan.html`. Verify `model.ToolCallingChatModel.WithTools` signature stability across the bump (stable across recent minors, but pin and verify). One PR, independent of any eino-providers work, lands before symphony imports the new module.

b. **Replace `~/git/local-symphony/internal/worker/agent/models.go` `NewModels`'s internal `ollama.NewChatModel` calls** with `eino-providers/ollama.NewChatModel(ctx, Config{...})`. The `Models` struct itself stays in symphony - it carries telemetry-only fields (`PrimaryKeepAlive`, `FallbackKeepAlive`, `TurnTimeout`) that have no business in a shared module. Only the construction logic delegates.

c. **`PingOllama` and `ErrBackendUnreachable` move to `eino-providers/ollama`.** Symphony's `health.go` becomes a shim or gets deleted; callers update their import path. `ErrSparkUnreachable` becomes a local alias if telemetry strings need to stay stable, otherwise the rename propagates.

d. **When the OpenAI-Codex backend lands in symphony** (Phase 2 in doc 04), it uses `eino-providers/openaicodex.NewChatModel(...)` directly - the same import path advisor uses.

## 11. Versioning and release plan

- `v0.1.0` - Provider interface, `NewProvider` factory with registry, the five backend sub-packages, error sentinels, `ExtractUsage`. Single-shot `Provider` only. Tag after both consumers have green CI against `eino-providers` as a `replace`-d local module for one full PR cycle each.
- `v0.2.0` - `ChatModelFactory` interface formalised. Symphony's ReAct callers adopt the formalised shape.
- `v0.3.0` - `RegisterProvider` documented as a stable public API for third-party backends.
- `v1.0.0` - tag after >= 1 month of both consumers stable on the module with no API churn.

**Release-ordering constraint.** `codex-auth-go v0.1.0` MUST ship before `eino-providers v0.1.0`. The `openaicodex` sub-package imports `codex-auth-go`'s `HTTPClient(ctx)` accessor and its `ErrNotLoggedIn` (plus the post-promotion `ErrPlanNotIncluded`, `ErrQuotaExceeded`) sentinels. Tagging against a not-yet-published `codex-auth-go` would force consumers to use `replace` directives - fine for dev, broken for first external adopters. Tag in order.

**License.** The repo carries MIT today. Research doc 05 noted a tension: Apache 2.0 matches upstream `cloudwego/eino` and gives a patent grant, which matters for a library others may depend on. MIT works for permissive use. A relicense before any external consumer arrives is an open question (section 15), not a v0.1.0 blocker.

**Repo conventions.** `main` branch. `.golangci.yml` baseline matches both consumers. `docs/adr/` for design decisions (registry pattern, `BaseURL` `string` vs `*string`, `chatmodel.go` placement). GitHub Actions matrix: `go test ./...` + `golangci-lint` + `go mod tidy` on Go 1.25 and 1.26. Hand-maintained `CHANGELOG.md` through v1.

## 12. Test parity

The tests that come over verbatim or near-verbatim from advisor:

- `~/git/advisor/internal/advisor/factory_test.go` - core dispatch-via-name tests.
- `~/git/advisor/internal/advisor/factory_seam_test.go` - the `readOpenAIAuthMode` seam coverage (oauth/apikey routing).
- `~/git/advisor/internal/advisor/factory_bench_test.go` - the per-provider construction benchmarks. Moves to a `factory_bench_test.go` at the root of `eino-providers` so the Gemini eager-construction cost stays visible in CI. The `b.Setenv` pin for `GOOGLE_API_KEY`, `GOOGLE_GENAI_USE_VERTEXAI`, and `GOOGLE_CLOUD_*` is preserved (the comment block at lines 15-29 explains why).
- `~/git/advisor/internal/advisor/usage_test.go` - the five `extractUsage` cases. Move to `usage_test.go` at the root and case-flip to `ExtractUsage`.
- `~/git/advisor/internal/advisor/openai_codex_test.go` - the `classifyCodexError` table-driven tests. Moves to `openaicodex/openai_codex_test.go`.

The advisor adapters today have very thin per-backend coverage (no `claude_test.go`, no `openai_test.go`, no `gemini_test.go`). The lift PRs add per-backend unit tests that exercise: (a) constructor with valid inputs, (b) constructor with each failure mode, (c) `Advise` against an `httptest.Server` that returns canned eino-shaped responses. The Codex tests already exist and come over.

From symphony:

- `~/git/local-symphony/internal/worker/agent/models_test.go` - the Ollama factory tests (validation of `BaseURL`, `ReadTimeoutMs`, `KeepAlive` parsing). Move to `ollama/models_test.go`.
- `~/git/local-symphony/internal/worker/agent/health_test.go` - the `PingOllama` tests. Move to `ollama/health_test.go`.

A new `factory_bench_test.go` benchmark for `ollama.NewChatModel` joins the cold-start matrix so all five backends have a comparable construction-cost number.

## 13. Risk register

- **Eino API drift across `v0.7 -> v0.8 -> future`.** Gating concern: `model.ToolCallingChatModel.WithTools` signature stability. Stable across recent minors but `eino` is pre-1.0. Mitigation: pin exact versions; deliberate bump PRs only.
- **Per-backend SDK drift** (`anthropic-sdk-go`, `google.golang.org/genai`, eino-ext Ollama). The Gemini benchmark doubles as a churn sentinel: any change to `genai.NewClient`'s allocation count shows up immediately.
- **Gemini construction cost regression.** 18 allocs / 912 B is the current floor. Mitigation: factory benchmark in CI; a 2x regression fails the build.
- **`RegisterProvider` cycle risk.** A sub-package `init()` calling the root package is fine in Go (root globals init before importer `init`). But cross-sub-package imports chaining registrations would deadlock on the registry mutex. Mitigation: doc-comment ban on cross-sub-package imports; a `staticcheck` rule catches it.
- **Public-API ossification after `v1.0.0`.** Every new `Options` field becomes a breaking change for positional struct-literal callers. Mitigation: doc-comment requires field-name keys.
- **Module-path collision with cloudwego.** If they ever publish an `eino-providers` submodule, only the human-readable name overlaps; the fully qualified import path is unambiguous. Note in README.
- **`extractUsage` -> `ExtractUsage` case flip.** Internal-only breakage; `internal/` precludes external consumers.

## 14. First-PR breakdown

Each PR is two to three sentences. PR3 is the heaviest; PR4 and PR5 are mechanical adoption sweeps.

**PR1 - Repo skeleton.** Add `go.mod` (`go 1.25.5`, `github.com/mattsp1290/eino-providers`), `.golangci.yml` mirroring advisor's, GitHub Actions matrix for Go 1.25 / 1.26, `README.md` with one example, `docs/adr/` with one ADR for the registry pattern, `CHANGELOG.md` with a `## v0.1.0` placeholder. LICENSE already in place. No Go source.

**PR2 - Core types and registry.** `provider.go`, `errors.go`, `registry.go`, `options.go`, `factory.go`, `doc.go`. Plus `usage_test.go` lifted with the case flip. No backend sub-packages yet; factory returns `ErrUnknownProvider` for every name.

**PR3 - Backend sub-packages.** Five sub-directories at once (claude, openai, openaicodex, gemini, ollama). Each lifts its adapter, registers via `init()`, brings relevant tests. `openaicodex` imports `codex-auth-go v0.1.0` (must already be published). The factory benchmark file lands at the root.

**PR4 - Tag `v0.1.0` and switch advisor.** Advisor PR deletes the six lifted files in `internal/advisor/`, replaces `core` call sites with `einoproviders.NewProvider(...)`, verifies the byte-equal MCP replay corpus is unchanged (`authErr.Error()` preserves the cause, so it should be), and keeps `codexauth/chatmodel.go` in place (section 9d).

**PR5 - Symphony Phase 0 plus adoption.** Symphony PR: bump `cloudwego/eino` `v0.7.13` -> `v0.8.13` and Go `1.25.0` -> `1.25.5` (one commit), replace `models.go`'s direct `ollama.NewChatModel` with `einoproviders.ollama.NewChatModel(...)` (next commit), delete `health.go` and update callers (final commit). `Models` struct stays.

## 15. Open questions and TODOs for human review

- **License: stick with MIT or relicense to Apache 2.0?** Doc 05 recommended Apache 2.0 for libraries (patent grant, upstream alignment). Repo carries MIT. Decide before external consumers arrive; relicensing later requires contributor consent.
- **Where does `codexauth/chatmodel.go` (raw-HTTP Codex wrapper) ultimately live?** Section 9d frames the options. v0.1.0 punts.
- **Repo visibility on day one** - public means publishing before API settles; private needs `GOPRIVATE`. Doc 05 implies public.
- **`Options.BaseURL *string` too clever?** Forces callers to `BaseURL: ptr.To("...")`. Alternative: `BaseURL string` with empty = default. Decide before v0.1.0.
- **Stub backends in v0.1.0 - Bedrock, Together, vLLM?** Doc 05 implies no (out-of-tree registrations once the registry is stable). Confirm.
- **`Classify(err) ErrorClass` API shape.** Switch-friendly enum versus "use `errors.Is` yourself" - does the helper carry its weight?
- **Publish a `eino-providers-test` helper package** with `recordingProvider` / `fakeProvider` doubles, or leave internal until a second consumer needs it?

---

Plan lives at `/Users/punk1290/git/eino-providers/docs/prompts/extract-eino-providers.md`. Next: ship `codex-auth-go v0.1.0` first (release-ordering constraint, section 11), then start with PR1 above.
