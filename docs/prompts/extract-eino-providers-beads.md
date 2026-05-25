# Project Planning with Beads

## Agent Instructions

You are an expert software architect creating a comprehensive task breakdown. This task graph will be executed by AI agents working in parallel, coordinated through MCP Agent Mail with file reservations to prevent conflicts.

<quality_expectations>
Create a thorough, production-ready task graph. Include all necessary setup, implementation, testing, and documentation tasks. Go beyond the basics - consider edge cases, error handling, security considerations, and integration points. Each task should be specific enough for an agent to execute independently without ambiguity.
</quality_expectations>

## Project Information

### Links to Relevant Documentation

- **`docs/prompts/extract-eino-providers.md` (this repo)** — the canonical 243-line planning document. Sections 2 (dual-shape API), 3 (module layout), 4 (registry design), 5 (per-backend quirks), 6 (`Options` struct), 7 (error promotion plan), 9 (advisor migration), 10 (symphony migration), 12 (test parity), 14 (first-PR breakdown), 15 (open questions) are the load-bearing references. Treat that document as the source of truth where it conflicts with anything below.
- `~/docs/eino/index.html` — overview of the four-repo extraction plan (`codex-auth-go`, `eino-providers`, `agent-otel`, `eino-tools`) with dependency graph and executive summary
- `~/docs/eino/02-shared-eino-patterns.html` — concrete public API for each shared module; sections 1 (Provider + ExtractUsage), 2 (per-backend adapter quirks), 3 (factory + auth-mode routing), 6 (error sentinels) are most relevant
- `~/docs/eino/05-shared-repos-proposal.html` — repo-by-repo scope, registry design (database/sql driver pattern), versioning plan, cross-repo conventions
- `~/docs/eino/01-current-state.html` — baseline for "what's being lifted" from advisor and symphony
- `~/docs/eino/04-integration-plan.html` — migration order and eino version-skew handling (advisor on `v0.8.13`, symphony on `v0.7.13`; both bump to match the shared module's pin)
- `~/git/advisor/internal/advisor/` — Go source being lifted: `provider.go`, `factory.go`, `factory_seam.go`, `claude.go`, `openai.go`, `openai_codex.go`, `gemini.go`. Test files: `factory_test.go`, `factory_seam_test.go`, `factory_bench_test.go`, `usage_test.go`, `openai_codex_test.go`. Plus `codexauth/chatmodel.go` (raw-HTTP wrapper — placement decision punted in v0.1.0 per plan §9d)
- `~/git/local-symphony/internal/worker/agent/` — Go source being lifted: `models.go` (Ollama factory + `ErrSparkUnreachable`), `health.go` (`PingOllama` + `pingWithCappedTimeout`), `keepalive.go` (`parseKeepAlive`). Test files: `models_test.go`, `health_test.go`, `keepalive_test.go`
- Upstream: `github.com/cloudwego/eino` (core), `github.com/cloudwego/eino-ext/components/model/{claude,openai,gemini,ollama}` (per-backend adapters)
- Sibling repo (mandatory dependency): `github.com/mattsp1290/codex-auth-go` — only the `openaicodex/` sub-package imports it. **Must publish `codex-auth-go v0.1.0` before tagging `eino-providers v0.1.0`** (release-ordering constraint, plan §11)

### Project Description

A reusable Go module (`github.com/mattsp1290/eino-providers`) that extracts the multi-provider model-construction surface currently embedded in `~/git/advisor/internal/advisor/` (and the Ollama-specific portion in `~/git/local-symphony/internal/worker/agent/`) into a standalone library consumable by `advisor`, `local-symphony`, and any future agentic CLI / MCP server / worker projects.

**Public surface (v0.1.0):**

- A single-shot `Provider` interface (`Advise(ctx, system, user string, maxTokens int) (text string, usage Usage, err error)`) — advisor's natural shape, lifted verbatim from `~/git/advisor/internal/advisor/provider.go:48-50`.
- A per-backend `NewChatModel(ctx, Config) (model.ToolCallingChatModel, error)` shape — local-symphony's natural shape for callers that bind tools and run their own ReAct loop. The interface itself is not formalised in v0.1.0; per-sub-package functions only. The formal `ChatModelFactory` interface lands in `v0.2.0`.
- A `Usage` struct (`InputTokens`, `OutputTokens`, `Available bool`) and an exported `ExtractUsage(*schema.Message) Usage` token-accounting helper, nil-safe. (Case-flip from advisor's unexported `extractUsage`.)
- A top-level `NewProvider(ctx, name, model string, opts Options) (Provider, error)` factory using a **registry / `init()`-self-registration** pattern (database/sql driver style). The root `factory.go` MUST NOT import sub-packages directly — that's the whole point.
- A `RegisterProvider(name string, ctor ProviderCtor)` hook so out-of-tree backends (Bedrock, vLLM, Together, …) can plug in without forking. Duplicate registration panics (mirrors `database/sql.Register`).
- Sub-packages per backend, each independently importable for side-effect registration (`import _ "github.com/mattsp1290/eino-providers/claude"`):
  - `claude/` — `NewClaudeProvider(apiKey, model)`, `NewClaudeProviderWithBaseURL(apiKey, model, baseURL)`; eino-ext claude adapter; `MaxTokens int` (literal), `BaseURL *string`.
  - `openai/` — `NewOpenAIProvider(apiKey, model)`, `NewOpenAIProviderWithBaseURL(apiKey, model, baseURL)`; eino-ext openai adapter; uses `MaxCompletionTokens *int` (not the deprecated `MaxTokens *int`), `BaseURL string` (plain).
  - `openaicodex/` — `NewOpenAICodexProvider(ctx, model)`; depends on `eino-ext/components/model/openai` AND `github.com/mattsp1290/codex-auth-go`. **Must not** set `MaxCompletionTokens` or `BaseURL` (the codex transport rewrites the URL; the Codex endpoint caps output server-side). Must inject `HTTPClient` from `codex-auth-go` and set `APIKey: "codex-oauth-dummy"` (eino-ext rejects empty `APIKey` at construction; the transport overwrites the `Authorization` header anyway). Also carries `classifyCodexError` lifted from `openai_codex.go:96-114` and the `usage_not_included` / `insufficient_quota` string constants.
  - `gemini/` — `NewGeminiProvider(ctx, apiKey, model)`; eino-ext gemini adapter. Caches `*genai.Client` at construction (cold-start cost is ~14× higher than Claude/OpenAI due to `genai.NewClient` eagerly constructing multiple sub-clients).
  - `ollama/` — `NewChatModel(ctx, Config) (model.ToolCallingChatModel, error)` lifted from `~/git/local-symphony/internal/worker/agent/models.go:133-141`, plus `PingOllama(ctx, baseURL, client) error` and `pingWithCappedTimeout` health probe logic lifted from `health.go` / `models.go` (cap at 5 s timeout). Include `parseKeepAlive` from `keepalive.go`. In v0.1.0 ollama exports only the `NewChatModel` shape (no `Provider` adapter yet).
- Shared typed error sentinels (`errors.go`):
  - `ErrProviderInit`, `ErrProviderTimeout`, `ErrProviderAPI`, `ErrUnknownProvider` — stay in `eino-providers`.
  - `ErrBackendUnreachable` — unifies advisor's "init" error and symphony's `ErrSparkUnreachable` (renamed; "Spark" is a symphony product term that has no place in a shared library).
  - `ErrNotLoggedIn`, `ErrPlanNotIncluded`, `ErrQuotaExceeded`, `ErrRefreshFailed` — **promoted to `codex-auth-go`** (already lives there for `ErrNotLoggedIn`; the other three are promoted from advisor's `factory_seam.go:61-73`). `eino-providers/openaicodex` re-uses these via `codex-auth-go` import; advisor's `errors.Is(err, einoproviders.ErrAuthPlanNotIncluded)` becomes `errors.Is(err, codexauth.ErrPlanNotIncluded)`.
  - `Classify(err) ErrorClass` helper that maps a wrapped error to one of the public classes (switch-friendly enum form).
  - Wrapper types `initErr` and `authErr` (and `WrapInitError`) lift from `factory_seam.go:78-130`. The sentinel-first `Unwrap()` ordering at lines 92-97 is preserved.
- A unified `Options` struct with pointer-typed fields so "not set" is unambiguously `nil`:
  ```go
  type Options struct {
      APIKey      string
      BaseURL     *string
      MaxTokens   *int
      KeepAlive   *time.Duration   // ollama only
      HTTPClient  *http.Client     // openai-codex (required); optional for others
      GenaiClient *genai.Client    // gemini cold-start avoidance
  }
  ```
  Per-backend constructors validate (e.g., `openaicodex` returns a validation error if `BaseURL != nil`, `MaxTokens != nil`, or `HTTPClient == nil`).
- **Auth-mode routing is an API decision bead.** The canonical plan currently preserves/lifts advisor's `readOpenAIAuthMode` seam and `factory_seam_test.go` coverage, while research docs point toward consumer-supplied auth mode. Model this as a P0 `api-decision` bead that blocks `options.go`, `factory.go`, and `factory_seam_test.go`; do not create contradictory implementation beads unless the decision bead explicitly resolves the final shape.

**Explicitly out of scope:**

- The Codex OAuth flow itself (PKCE, credstore, RFC 7009 revocation, the `codexTransport`) — delegated to `github.com/mattsp1290/codex-auth-go`. `eino-providers/openaicodex/` is a thin convenience wrapper that imports `HTTPClient(ctx)` and the codex sentinels.
- Telemetry / OTel — delegated to the future `agent-otel` module. Consumers wire instruments around their own `Advise` / `Generate` calls.
- Tool definitions (`fileops`, `search`, `shell`, `trackerwrite`) — delegated to the future `eino-tools` module. Tool binding via `WithTools` is the caller's job.
- Anthropic / Google OAuth — different beasts entirely.
- CLI commands (`login` / `logout`), MCP framing, agent loops, application config loading — stay in each consumer.
- The raw-HTTP Codex wrapper (`~/git/advisor/internal/advisor/codexauth/chatmodel.go`) — placement open question, NOT lifted in v0.1.0 (see plan §9d, §15).

**Target versioning (plan §11):**

- `v0.1.0` — extracted from advisor + symphony with full test parity. Single-shot `Provider` only. Tag only after both consumers have green CI against a local `replace` of `eino-providers` for one PR cycle.
- `v0.2.0` — `ChatModelFactory` interface formalised for symphony's ReAct path.
- `v0.3.0` — `RegisterProvider` documented as a stable public API for third-party backends.
- `v1.0.0` — tagged after ≥ 1 month of both consumers stable on the module with no API churn.

**Release-ordering constraint:** `codex-auth-go v0.1.0` MUST ship before `eino-providers v0.1.0`. Not a parallelism opportunity.

### Technical Stack

- **Language:** `go.mod` declares Go 1.25.5 per canonical plan §14. CI tests against Go 1.25 and 1.26.
- **Core framework:** `github.com/cloudwego/eino` — pin to a single minor version. Recommend the newer `v0.8.x` line (advisor is there; symphony bumps from `v0.7.13` to match as Phase 0 of its migration, per plan §10a).
- **Per-backend adapters:** `github.com/cloudwego/eino-ext/components/model/{claude,openai,gemini,ollama}` — each pulled in only by its respective sub-package so transitive deps stay opt-in.
- **Sibling dependency:** `github.com/mattsp1290/codex-auth-go` — only `openaicodex/` imports it.
- **Transitive (via eino-ext):** `anthropic-sdk-go`, `openai-go`, `google.golang.org/genai`, the Ollama Go client — pulled only when the corresponding sub-package is imported.
- **Tooling:** `golangci-lint` with the same baseline `.golangci.yml` as `~/git/advisor` and `~/git/local-symphony`. `go mod tidy` verification in CI.
- **CI:** GitHub Actions on Go 1.25 and 1.26 — `go test ./...`, `golangci-lint run`, `go mod tidy` cleanliness check, plus the transitive-dep-leak verification described below.
- **License:** MIT (already in tree). Relicense to Apache 2.0 is an open question (plan §15) but NOT a v0.1.0 blocker.
- **Default branch:** `main` (no `master`).
- **No runtime deps:** no databases, no servers, no telemetry, no CLI. Pure library.

### Specific Requirements

- **Test parity with advisor and symphony.** Every test currently exercising the provider / factory / per-backend code in `~/git/advisor/internal/advisor/` and the Ollama-relevant tests in `~/git/local-symphony/internal/worker/agent/` must lift with the corresponding code and pass against the extracted module. The specific test files to move (plan §12):
  - From advisor: `factory_test.go`, `factory_seam_test.go`, `factory_bench_test.go`, `usage_test.go`, `openai_codex_test.go`
  - From symphony: `models_test.go` (the Ollama factory tests), `health_test.go` (the `PingOllama` tests), `keepalive_test.go` (the `parseKeepAlive` tests)
  - The advisor adapters have very thin per-backend coverage today (no `claude_test.go`, no `openai_test.go`, no `gemini_test.go`); the lift PRs add per-backend unit tests covering (a) constructor with valid inputs, (b) constructor with each failure mode, (c) `Advise` against an `httptest.Server` that returns canned eino-shaped responses.
- **No transitive-dep-leak verification (CI gate).** Add a CI step that builds a throwaway module importing only `github.com/mattsp1290/eino-providers/claude` and asserts `github.com/mattsp1290/codex-auth-go` does NOT appear in `go list -deps` output. Repeat for `openai/`, `gemini/`, `ollama/` (none of those four may pull `codex-auth-go`). This guards the registry pattern's entire reason for existing.
- **Gemini cold-start benchmark in CI.** The 18 allocs / 912 B floor (from `~/git/advisor/benchmarks/coldstart.txt` and `factory_bench_test.go:15-29`) doubles as a churn sentinel for `google.golang.org/genai`. Any 2× regression fails the build. A new `ollama.NewChatModel` benchmark joins the cold-start matrix so all five backends have a comparable construction-cost number.
- **Public API stability between releases.** `v0.1.0` is the lifted code, no shape changes from the advisor original. `ChatModelFactory` lands in `v0.2.0`, not `v0.1.x`. Any breaking change after `v1.0.0` requires a `/v2/` module path component. `Options` struct literals must use field-name keys (positional literals are explicitly disallowed by doc comment so adding a field is non-breaking).
- **CHANGELOG.md maintained by hand to `v1.0.0`.** Switch to `release-please` (or similar) once `v1.0.0` is tagged.
- **ADR directory at `docs/adr/`.** Carry design decisions worth preserving (plan §11, §15):
  - ADR-0001: registry / `init()`-self-registration choice and the rejected root-imports-all alternative
  - ADR-0002: `BaseURL *string` vs `string` decision (open question in plan §15 — resolve before v0.1.0)
  - ADR-0003: `AuthMode`-from-consumer-Options not from env
  - ADR-0004: eino version pin choice
  - ADR-0005: MIT vs Apache 2.0 (capture the open question even if resolved as "stay MIT for now")
  - ADR-0006: Codex `APIKey="codex-oauth-dummy"` workaround
  - ADR-0007: `chatmodel.go` placement deferred (open question)
- **README opens with: one sentence on what it does, one example, link to consumer projects.** No marketing copy. The example: import the `claude/` sub-package for side effects, call `NewProvider`, call `Advise`, print the result and usage. Under 30 lines of Go.
- **No backwards-compat shim for lifted advisor provider/factory files.** When advisor migrates to `codex-auth-go`, it migrates to `eino-providers` in the same change set — provider/factory call sites update directly, with no deprecated wrappers around the lifted files. Auth sentinel aliases and the `internal/advisor/codexauth/` shim follow canonical plan §7/§9.
- **`ErrSparkUnreachable` rename propagates to symphony.** Symphony's `health.go` becomes a shim (or is deleted entirely) once `eino-providers/ollama` exports `ErrBackendUnreachable`. If symphony needs string-stable telemetry, it can keep a local type alias; the rename is non-negotiable in the shared module.
- **Test parity DOES NOT mean carrying advisor's MCP-replay gate.** That byte-equality gate (advisor's ADR-0001) is telemetry-specific and stays with `agent-otel` work. `eino-providers` extraction is byte-equality-free. Advisor's first migration PR verifies `authErr.Error()` still preserves the cause string (it should — the wrapper carries the cause verbatim) so the MCP replay corpus stays unchanged, but that verification is a one-time check, not an ongoing CI gate.
- **Resolve plan §15 open questions before tagging `v0.1.0`:** `BaseURL *string` vs `string`, `Classify(err)` API shape (switch-friendly enum vs "use errors.Is yourself"), repo visibility (public vs private with `GOPRIVATE`), stub backends decision (no, per doc 05 — confirm), test-helper package (`eino-providers-test` with `recordingProvider` / `fakeProvider` doubles) deferred or included. Each gets an ADR or a CHANGELOG note.

---

## Your Task

Analyze this project and create a comprehensive **Beads task graph** using the `bd` CLI. Beads provides dependency-aware, conflict-free task management for multi-agent execution.

The first-PR breakdown in plan §14 is a strong starting point for the task graph's macro phases, but the task graph should be more granular: each PR decomposes into multiple beads (skeleton files, per-backend lifts, per-test-file moves, ADR drafts, CI gates, etc.). Do not bundle release tagging into advisor migration. Model the tag as a final release bead blocked by: `codex-auth-go v0.1.0` publication, both consumers green against a local `replace` for one PR cycle, no-transitive-dep-leak CI, benchmark CI, and all §15 decisions resolved. Symphony Phase 0 (Go/eino bump) is independent and may run before `eino-providers v0.1.0` is tagged.

---

<critical_constraint>
Your ONLY output is a bash shell script. Do NOT use `bd add` — the correct command to create a bead is `bd create`. Use `bd dep add` for dependencies. Do not implement anything yourself. The script may create Beads tasks for documentation or context-prep work, but it must not create, copy, or edit project files itself.
</critical_constraint>

## Output Format

Generate a shell script that creates the full task graph. The script should:

1. **Initialize Beads** (if not already initialized)
2. **Create all beads** with appropriate priorities
3. **Establish dependencies** between beads
4. **Add labels** for phase grouping

### Example Output

```bash
#!/bin/bash
# Project: eino-providers
# Generated: 2026-05-25

set -e

# Initialize beads if needed
if ! bd info >/dev/null 2>&1; then
    bd init --non-interactive --skip-agents --skip-hooks
fi

echo "Creating project beads..."

# ========================================
# Phase 1: Repo skeleton (PR1)
# ========================================

SKEL_GOMOD=$(bd create "Add go.mod for github.com/mattsp1290/eino-providers with go 1.25.5" -p 0 -l setup,pr1 -t task --silent)

SKEL_LINT=$(bd create "Add .golangci.yml mirroring advisor baseline" -p 1 -l setup,pr1 -t task --silent)
bd dep add "$SKEL_LINT" "$SKEL_GOMOD"

SKEL_CI=$(bd create "Add GitHub Actions workflow for Go 1.25 and 1.26 test lint tidy" -p 1 -l setup,ci,pr1 -t task --silent)
bd dep add "$SKEL_CI" "$SKEL_LINT"

SKEL_ADR=$(bd create "Add ADR 0001 for registry pattern" -p 1 -l docs,pr1 -t decision --silent)

SKEL_README=$(bd create "Add README.md with one-sentence summary and 30-line example" -p 1 -l docs,pr1 -t task --silent)

# ========================================
# Phase 2: Core types and registry (PR2)
# ========================================

CORE_PROVIDER=$(bd create "Lift provider.go from advisor with Provider Usage ExtractUsage" -p 0 -l core,pr2 -t task --silent)
bd dep add "$CORE_PROVIDER" "$SKEL_GOMOD"

CORE_REGISTRY=$(bd create "Implement registry.go with RegisterProvider mutex and duplicate panic" -p 0 -l core,pr2 -t task --silent)
bd dep add "$CORE_REGISTRY" "$CORE_PROVIDER"

# ... continue: errors.go, options.go, factory.go, doc.go, usage_test.go lift ...

# ========================================
# Phase 3: Backend sub-packages (PR3)
# ========================================

# Each backend has lift + register + relevant tests. The root benchmark file carries the cold-start matrix.
# openaicodex blocks on codex-auth-go API readiness and codex-auth-go v0.1.0 publication.

# ... etc.

echo ""
echo "Bead graph created! View with:"
echo "  bd ready              # List unblocked tasks"
```

---

## Bead Creation Guidelines

### Priority Levels
- `-p 0` = Critical (blocking other work)
- `-p 1` = High (important but not blocking)
- `-p 2` = Medium (standard work)
- `-p 3` = Low (nice to have)

### Labels (Phase Grouping)
Use `-l` or `--labels` to group beads by phase. Use comma-separated labels for multi-label beads (for example `-l core,testing,pr2`):
- `setup` — Repo skeleton, go.mod, CI, lint config
- `core` — Provider, Usage, registry, factory, errors, Options
- `claude` / `openai` / `openaicodex` / `gemini` / `ollama` — Per-backend lifts
- `testing` — Test parity, dep-leak CI gate, benchmark CI gate
- `docs` — README, ADRs, CHANGELOG
- `migration-advisor` — Advisor's adoption PRs (deletes + import-path updates)
- `migration-symphony` — Symphony's Phase 0 bump + Ollama adoption
- `release` — Tagging v0.1.0, version constraints
- `external-dep` — External blockers in sibling repos
- `api-decision` — API/open-question decisions that block implementation
- `ci` — CI workflow and CI gate tasks
- `consumer-verification` — Advisor/symphony local-replace verification
- `release-gate` — Tasks that must complete before tagging
- `pr1` / `pr2` / `pr3` / `pr4` / `pr5` — Macro-phase grouping

### Dependency Rules
1. Never create cycles
2. Every repository-local bead should have a clear dependency chain back to setup tasks; external blocker beads may be root nodes
3. Use `bd dep add CHILD PARENT` (child depends on parent completing first)
4. Parallel work should share a common ancestor, not depend on each other
5. **External constraints:** model separate explicit blocker beads for "codex-auth-go API surface ready: HTTPClient(ctx), ErrNotLoggedIn, ErrPlanNotIncluded, ErrQuotaExceeded, ErrRefreshFailed" and "codex-auth-go v0.1.0 published" even though the work happens in a sibling repo. Every `openaicodex` implementation/test bead depends on both.
6. **Release gates:** the `eino-providers v0.1.0` tag bead depends on codex-auth-go publication, advisor local-replace verification, symphony local-replace verification, dep-leak CI, benchmark CI, and all §15 decision beads.

### Shell Script Rules
- Prefer a one-shot script and print a warning that rerunning it creates duplicate beads, unless you implement an exact-title get-or-create helper.
- Use `bd init --non-interactive --skip-agents --skip-hooks` when initialization is needed; detect initialization with `bd info >/dev/null 2>&1`, not just `.beads` existence.
- Quote bead variables in dependency commands: `bd dep add "$CHILD" "$PARENT"`.
- Do not put Markdown backticks inside double-quoted shell strings; remove the backticks from bead titles or use safe single-quoting.
- Use valid bead types only: `task`, `feature`, `bug`, `epic`, `chore`, or `decision`. Use `decision` for ADR/API decision beads and `task` or `chore` for docs work.

### Task Granularity
- Each bead should be completable in **under 750 lines of code**
- Tasks should be atomic enough for one agent to complete without coordination
- If a task requires multiple file areas, consider splitting by file area

---

## File Reservation Planning

For each major work area, note the file patterns that will need exclusive reservation:

```bash
# Core types: provider.go, registry.go, factory.go, errors.go, options.go, doc.go, usage_test.go, factory_test.go, factory_seam_test.go
# Per-backend: claude/**, openai/**, openaicodex/**, gemini/**, ollama/**
# CI: .github/workflows/**
# Docs: docs/adr/**, README.md, CHANGELOG.md
# Advisor migration: ~/git/advisor/internal/advisor/{provider,factory,claude,openai,openai_codex,gemini}.go and call sites in ~/git/advisor/internal/advisor/core/
# Symphony migration: ~/git/local-symphony/internal/worker/agent/{models,health,keepalive}.go and Models struct callers
```

This helps agents claim appropriate file surfaces when they start work.

---

## Context Documentation

Create beads for placing important context in `docs/prompts/` or `docs/adr/` for agents to reference. The generated script should not write those files directly. Context includes:
- The full plan at `docs/prompts/extract-eino-providers.md` (READ THIS FIRST — it is the source of truth)
- The research notes at `~/docs/eino/`
- ADR drafts as they're written

---

## Verification Steps

After generating the script:

1. **Run it**: `chmod +x setup-beads.sh && ./setup-beads.sh`
2. **Check ready work**: `bd ready` should show initial setup tasks (the PR1 skeleton beads with no dependencies)
3. **Check the openaicodex blockers**: run `bd dep tree "$OPENAICODEX_PROVIDER"` (or the equivalent variable used in the generated script) and verify both codex-auth-go blocker beads appear upstream.

---

## Completeness Checklist

Ensure your task graph includes:

- [ ] PR1: repo skeleton (go.mod, .golangci.yml, CI workflow, LICENSE already present, README, CHANGELOG, ADR-0001)
- [ ] PR2: core types (provider.go, registry.go, factory.go, errors.go, options.go, doc.go) + usage_test.go lift with case flip + factory_test.go lift
- [ ] PR2/PR3 bridge: lift or deliberately adapt `factory_seam_test.go` (oauth/apikey routing tests) after the AuthMode/API decision bead is resolved
- [ ] PR2 sub-task: core tests for duplicate registration panic, unknown provider when no side-effect import ran, root package does not import backend packages, WrapInitError/authErr/Classify, and Options doc-comment/validation behavior
- [ ] PR3: five backend sub-packages, each with lift + init()-register + relevant tests; root factory_bench_test.go carries the benchmark matrix and adds Ollama
- [ ] PR3 sub-task: openaicodex depends on codex-auth-go API readiness and v0.1.0 publication (model both as explicit external blockers)
- [ ] PR3 sub-task: lift the `classifyCodexError` table-driven tests to `openaicodex/openai_codex_test.go`
- [ ] PR3 sub-task: add openaicodex validation tests for rejecting BaseURL, rejecting MaxTokens, requiring HTTPClient, leaving BaseURL/MaxCompletionTokens unset, and using the dummy API key
- [ ] PR3 sub-task: lift `factory_bench_test.go` to module root + add ollama benchmark
- [ ] PR3 sub-task: lift symphony's `models_test.go`, `health_test.go`, and `keepalive_test.go` to `ollama/`
- [ ] PR3 sub-task: add Ollama negative-path tests for malformed URL, non-2xx ping, timeout cap, and credential redaction where applicable
- [ ] PR3 sub-task: add new per-backend unit tests for claude/openai/gemini (httptest.Server-based) — these are NEW, not lifts
- [ ] CI gate: no-transitive-dep-leak verification (Claude/OpenAI/Gemini/Ollama must NOT pull codex-auth-go)
- [ ] CI gate: Gemini cold-start benchmark with 2× regression threshold
- [ ] ADRs 0002-0007 drafted (BaseURL shape, AuthMode-from-Options, eino pin, license, codex-oauth-dummy, chatmodel.go placement)
- [ ] Resolve plan §15 open questions before v0.1.0 tag (BaseURL shape, Classify shape, repo visibility, stub backends, test-helper package)
- [ ] Advisor migration: add side-effect imports, update provider factory call sites to `einoproviders.NewProvider`, switch auth sentinels to `codex-auth-go`, delete lifted provider files, decide/keep `internal/advisor/codexauth` shim per canonical plan, verify MCP replay corpus unchanged once
- [ ] Symphony Phase 0: bump eino v0.7.13 → v0.8.13 and Go 1.25.0 → 1.25.5, verify `WithTools` signature, run `go mod tidy`
- [ ] Symphony adoption: replace `NewModels` Ollama construction with `eino-providers/ollama`, update/delete `health.go`, handle `ErrSparkUnreachable` rename/alias
- [ ] Consumer verification gate: advisor and local-symphony green against a local `replace` of `eino-providers` for one PR cycle before v0.1.0 tag
- [ ] Release: tag v0.1.0 only after all release-gate beads complete
- [ ] Clear dependency chains with no cycles
- [ ] All external blockers explicitly modelled (codex-auth-go API readiness and v0.1.0 publication)
