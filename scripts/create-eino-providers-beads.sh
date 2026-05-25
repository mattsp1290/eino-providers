#!/bin/bash
# Project: eino-providers
# Generated: 2026-05-25
# Warning: rerunning this script creates duplicate beads.

set -euo pipefail

if ! bd info >/dev/null 2>&1; then
    bd init --non-interactive --skip-agents --skip-hooks
fi

echo "Creating eino-providers bead graph..."

# ========================================
# External blockers and cross-repo prerequisites
# ========================================

EXT_CODEX_API=$(bd create "Prepare codex-auth-go API surface for HTTPClient and auth sentinels" -p 0 -l external-dep,release-gate,openaicodex -t task --silent)
EXT_CODEX_TAG=$(bd create "Publish codex-auth-go v0.1.0 before eino-providers release" -p 0 -l external-dep,release-gate,openaicodex,release -t task --silent)
bd dep add "$EXT_CODEX_TAG" "$EXT_CODEX_API"

SYM_PHASE0=$(bd create "Bump local-symphony to Go 1.25.5 and eino v0.8.x before provider adoption" -p 0 -l migration-symphony,external-dep,consumer-verification,pr5 -t task --silent)

# ========================================
# Phase 1: Repo skeleton PR1
# ========================================

SKEL_GOMOD=$(bd create "Add go.mod for github.com/mattsp1290/eino-providers with Go 1.25.5" -p 0 -l setup,pr1 -t task --silent)
SKEL_LINT=$(bd create "Add .golangci.yml mirroring advisor and local-symphony baseline" -p 1 -l setup,ci,pr1 -t task --silent)
SKEL_CI_BASE=$(bd create "Add GitHub Actions workflow for Go 1.25 and 1.26 test lint tidy" -p 1 -l setup,ci,pr1 -t task --silent)
SKEL_README=$(bd create "Add README with one sentence summary one under-30-line Claude example and consumer links" -p 1 -l docs,setup,pr1 -t task --silent)
SKEL_CHANGELOG=$(bd create "Add hand-maintained CHANGELOG with v0.1.0 placeholder" -p 1 -l docs,setup,pr1 -t task --silent)
SKEL_ADR_DIR=$(bd create "Add docs/adr directory and ADR naming convention" -p 1 -l docs,setup,pr1 -t task --silent)
ADR_REGISTRY=$(bd create "Write ADR 0001 for registry init self-registration and rejected root-imports-all design" -p 1 -l docs,api-decision,setup,pr1 -t decision --silent)
PR1_READY=$(bd create "Review PR1 skeleton for no Go source and clean CI baseline" -p 1 -l setup,ci,pr1 -t chore --silent)

bd dep add "$SKEL_LINT" "$SKEL_GOMOD"
bd dep add "$SKEL_CI_BASE" "$SKEL_LINT"
bd dep add "$SKEL_README" "$SKEL_GOMOD"
bd dep add "$SKEL_CHANGELOG" "$SKEL_GOMOD"
bd dep add "$SKEL_ADR_DIR" "$SKEL_GOMOD"
bd dep add "$ADR_REGISTRY" "$SKEL_ADR_DIR"
bd dep add "$PR1_READY" "$SKEL_CI_BASE"
bd dep add "$PR1_READY" "$SKEL_README"
bd dep add "$PR1_READY" "$SKEL_CHANGELOG"
bd dep add "$PR1_READY" "$ADR_REGISTRY"

# ========================================
# API decisions and ADRs blocking implementation
# ========================================

DEC_AUTH_MODE=$(bd create "Resolve AuthMode routing API: consumer Options versus lifted advisor env seam" -p 0 -l api-decision,core,pr2 -t decision --silent)
DEC_BASEURL=$(bd create "Resolve Options BaseURL pointer versus string shape before v0.1.0" -p 0 -l api-decision,core,release-gate,pr2 -t decision --silent)
DEC_CLASSIFY=$(bd create "Resolve Classify error API shape as enum helper or errors.Is only" -p 0 -l api-decision,core,release-gate,pr2 -t decision --silent)
DEC_VISIBILITY=$(bd create "Resolve repository visibility public versus private GOPRIVATE before v0.1.0" -p 1 -l api-decision,release-gate,docs -t decision --silent)
DEC_STUBS=$(bd create "Confirm no stub backends for Bedrock Together vLLM in v0.1.0" -p 1 -l api-decision,release-gate,docs -t decision --silent)
DEC_TESTHELPER=$(bd create "Decide whether eino-providers-test helper package is deferred or included" -p 1 -l api-decision,testing,release-gate -t decision --silent)
DEC_LICENSE=$(bd create "Resolve MIT versus Apache 2.0 license question before external consumers" -p 1 -l api-decision,docs,release-gate -t decision --silent)
DEC_CHATMODEL=$(bd create "Record raw HTTP Codex chatmodel placement deferred outside v0.1.0" -p 1 -l api-decision,openaicodex,docs,release-gate -t decision --silent)
DEC_EINO_PIN=$(bd create "Choose exact eino and eino-ext version pins for v0.1.0" -p 0 -l api-decision,setup,release-gate -t decision --silent)

ADR_BASEURL=$(bd create "Write ADR 0002 for BaseURL pointer versus string decision" -p 1 -l docs,api-decision,pr2 -t task --silent)
ADR_AUTH=$(bd create "Write ADR 0003 for AuthMode supplied by consumer Options" -p 1 -l docs,api-decision,pr2 -t task --silent)
ADR_EINO=$(bd create "Write ADR 0004 for eino version pin choice" -p 1 -l docs,api-decision,pr2 -t task --silent)
ADR_LICENSE=$(bd create "Write ADR 0005 for MIT versus Apache 2.0 resolution" -p 2 -l docs,api-decision -t task --silent)
ADR_CODEX_DUMMY=$(bd create "Write ADR 0006 for codex oauth dummy API key workaround" -p 1 -l docs,openaicodex,api-decision,pr3 -t task --silent)
ADR_CHATMODEL=$(bd create "Write ADR 0007 for raw HTTP Codex chatmodel placement deferral" -p 2 -l docs,openaicodex,api-decision -t task --silent)

bd dep add "$DEC_AUTH_MODE" "$PR1_READY"
bd dep add "$DEC_BASEURL" "$PR1_READY"
bd dep add "$DEC_CLASSIFY" "$PR1_READY"
bd dep add "$DEC_VISIBILITY" "$PR1_READY"
bd dep add "$DEC_STUBS" "$PR1_READY"
bd dep add "$DEC_TESTHELPER" "$PR1_READY"
bd dep add "$DEC_LICENSE" "$PR1_READY"
bd dep add "$DEC_CHATMODEL" "$PR1_READY"
bd dep add "$DEC_EINO_PIN" "$PR1_READY"
bd dep add "$ADR_BASEURL" "$DEC_BASEURL"
bd dep add "$ADR_AUTH" "$DEC_AUTH_MODE"
bd dep add "$ADR_EINO" "$DEC_EINO_PIN"
bd dep add "$ADR_LICENSE" "$DEC_LICENSE"
bd dep add "$ADR_CODEX_DUMMY" "$DEC_CHATMODEL"
bd dep add "$ADR_CHATMODEL" "$DEC_CHATMODEL"

# ========================================
# Phase 2: Core package PR2
# ========================================

CORE_PROVIDER=$(bd create "Lift provider.go with Provider Usage and nil-safe ExtractUsage" -p 0 -l core,pr2 -t task --silent)
CORE_ERRORS=$(bd create "Implement errors.go sentinels wrappers WrapInitError and Classify" -p 0 -l core,pr2 -t task --silent)
CORE_OPTIONS=$(bd create "Implement options.go unified Options with pointer fields and doc comments" -p 0 -l core,pr2 -t task --silent)
CORE_REGISTRY=$(bd create "Implement registry.go RegisterProvider lookup mutex and duplicate panic" -p 0 -l core,pr2 -t task --silent)
CORE_FACTORY=$(bd create "Implement root NewProvider dispatch through registry without backend imports" -p 0 -l core,pr2 -t task --silent)
CORE_DOC=$(bd create "Add root doc.go documenting provider contracts side-effect imports and registry rules" -p 1 -l core,docs,pr2 -t task --silent)
TEST_USAGE=$(bd create "Lift advisor usage_test.go and case-flip extractUsage to ExtractUsage" -p 1 -l core,testing,pr2 -t task --silent)
TEST_FACTORY=$(bd create "Lift advisor factory_test.go for unknown provider and registry dispatch" -p 1 -l core,testing,pr2 -t task --silent)
TEST_FACTORY_SEAM=$(bd create "Lift factory_seam_test.go after AuthMode decision and preserve wrapper behavior" -p 1 -l core,testing,pr2 -t task --silent)
PR2_READY=$(bd create "Run PR2 go test and verify root package has no backend imports" -p 1 -l core,testing,ci,pr2 -t chore --silent)

bd dep add "$CORE_PROVIDER" "$PR1_READY"
bd dep add "$CORE_ERRORS" "$DEC_CLASSIFY"
bd dep add "$CORE_OPTIONS" "$DEC_AUTH_MODE"
bd dep add "$CORE_OPTIONS" "$DEC_BASEURL"
bd dep add "$CORE_REGISTRY" "$ADR_REGISTRY"
bd dep add "$CORE_FACTORY" "$CORE_PROVIDER"
bd dep add "$CORE_FACTORY" "$CORE_ERRORS"
bd dep add "$CORE_FACTORY" "$CORE_OPTIONS"
bd dep add "$CORE_FACTORY" "$CORE_REGISTRY"
bd dep add "$CORE_DOC" "$CORE_FACTORY"
bd dep add "$TEST_USAGE" "$CORE_PROVIDER"
bd dep add "$TEST_FACTORY" "$CORE_FACTORY"
bd dep add "$TEST_FACTORY_SEAM" "$CORE_FACTORY"
bd dep add "$TEST_FACTORY_SEAM" "$ADR_AUTH"
bd dep add "$PR2_READY" "$CORE_DOC"
bd dep add "$PR2_READY" "$TEST_USAGE"
bd dep add "$PR2_READY" "$TEST_FACTORY"
bd dep add "$PR2_READY" "$TEST_FACTORY_SEAM"

# ========================================
# Phase 3: Backend sub-packages PR3
# ========================================

CLAUDE_IMPL=$(bd create "Lift claude provider package with constructors BaseURL handling Advise and init registration" -p 0 -l claude,pr3 -t task --silent)
CLAUDE_TESTS=$(bd create "Add claude constructor validation and httptest Advise coverage" -p 1 -l claude,testing,pr3 -t task --silent)

OPENAI_IMPL=$(bd create "Lift openai provider package using MaxCompletionTokens BaseURL string and init registration" -p 0 -l openai,pr3 -t task --silent)
OPENAI_TESTS=$(bd create "Add openai constructor validation MaxCompletionTokens and httptest Advise coverage" -p 1 -l openai,testing,pr3 -t task --silent)

CODEX_IMPL=$(bd create "Lift openaicodex provider with codex-auth-go HTTPClient dummy API key and no BaseURL or max tokens" -p 0 -l openaicodex,pr3 -t task --silent)
CODEX_CLASSIFY=$(bd create "Lift openaicodex classifyCodexError constants and codex auth sentinel wrapping" -p 0 -l openaicodex,testing,pr3 -t task --silent)
CODEX_TESTS=$(bd create "Lift advisor openai_codex_test.go and add validation coverage for forbidden options" -p 1 -l openaicodex,testing,pr3 -t task --silent)

GEMINI_IMPL=$(bd create "Lift gemini provider package with cached genai client optional GenaiClient and init registration" -p 0 -l gemini,pr3 -t task --silent)
GEMINI_TESTS=$(bd create "Add gemini constructor cache validation failure modes and httptest Advise coverage" -p 1 -l gemini,testing,pr3 -t task --silent)

OLLAMA_IMPL=$(bd create "Lift ollama NewChatModel Config and parseKeepAlive from local-symphony" -p 0 -l ollama,pr3 -t task --silent)
OLLAMA_HEALTH=$(bd create "Lift ollama PingOllama health probe with capped timeout and ErrBackendUnreachable" -p 0 -l ollama,pr3 -t task --silent)
OLLAMA_TESTS=$(bd create "Lift ollama models_test health_test and keepalive_test from local-symphony" -p 1 -l ollama,testing,pr3 -t task --silent)

BENCH_ROOT=$(bd create "Lift factory_bench_test cold-start matrix and add ollama NewChatModel benchmark" -p 1 -l testing,ci,pr3 -t task --silent)
CI_DEP_LEAK=$(bd create "Add CI gate proving claude openai gemini ollama do not depend on codex-auth-go" -p 0 -l ci,testing,release-gate,pr3 -t task --silent)
CI_BENCH=$(bd create "Add CI benchmark gate for Gemini 18 allocs 912 bytes with 2x regression limit" -p 0 -l ci,testing,release-gate,pr3 -t task --silent)
PR3_READY=$(bd create "Run PR3 full go test lint tidy dep-leak and benchmark checks" -p 0 -l testing,ci,pr3 -t chore --silent)

bd dep add "$CLAUDE_IMPL" "$PR2_READY"
bd dep add "$CLAUDE_IMPL" "$ADR_BASEURL"
bd dep add "$CLAUDE_TESTS" "$CLAUDE_IMPL"

bd dep add "$OPENAI_IMPL" "$PR2_READY"
bd dep add "$OPENAI_IMPL" "$ADR_BASEURL"
bd dep add "$OPENAI_TESTS" "$OPENAI_IMPL"

bd dep add "$CODEX_IMPL" "$PR2_READY"
bd dep add "$CODEX_IMPL" "$EXT_CODEX_API"
bd dep add "$CODEX_IMPL" "$EXT_CODEX_TAG"
bd dep add "$CODEX_IMPL" "$ADR_CODEX_DUMMY"
bd dep add "$CODEX_CLASSIFY" "$CODEX_IMPL"
bd dep add "$CODEX_TESTS" "$CODEX_CLASSIFY"

bd dep add "$GEMINI_IMPL" "$PR2_READY"
bd dep add "$GEMINI_TESTS" "$GEMINI_IMPL"

bd dep add "$OLLAMA_IMPL" "$PR2_READY"
bd dep add "$OLLAMA_HEALTH" "$OLLAMA_IMPL"
bd dep add "$OLLAMA_TESTS" "$OLLAMA_HEALTH"

bd dep add "$BENCH_ROOT" "$CLAUDE_IMPL"
bd dep add "$BENCH_ROOT" "$OPENAI_IMPL"
bd dep add "$BENCH_ROOT" "$CODEX_IMPL"
bd dep add "$BENCH_ROOT" "$GEMINI_IMPL"
bd dep add "$BENCH_ROOT" "$OLLAMA_IMPL"

bd dep add "$CI_DEP_LEAK" "$CLAUDE_TESTS"
bd dep add "$CI_DEP_LEAK" "$OPENAI_TESTS"
bd dep add "$CI_DEP_LEAK" "$GEMINI_TESTS"
bd dep add "$CI_DEP_LEAK" "$OLLAMA_TESTS"
bd dep add "$CI_BENCH" "$BENCH_ROOT"

bd dep add "$PR3_READY" "$CI_DEP_LEAK"
bd dep add "$PR3_READY" "$CI_BENCH"
bd dep add "$PR3_READY" "$CODEX_TESTS"
bd dep add "$PR3_READY" "$GEMINI_TESTS"
bd dep add "$PR3_READY" "$OLLAMA_TESTS"

# ========================================
# Documentation and release-readiness tasks
# ========================================

DOC_API=$(bd create "Document public API stability rules Options keyed literals and v0.2.0 ChatModelFactory plan" -p 1 -l docs,release-gate -t task --silent)
DOC_BACKENDS=$(bd create "Document per-backend quirks for max tokens base URLs codex transport and Gemini client caching" -p 1 -l docs,claude,openai,openaicodex,gemini,ollama -t task --silent)
DOC_CHANGELOG=$(bd create "Update CHANGELOG v0.1.0 with shipped surface and open deferrals" -p 1 -l docs,release-gate -t task --silent)
DOC_REGISTER=$(bd create "Document RegisterProvider as experimental until v0.3.0 stable API" -p 2 -l docs,core,release -t task --silent)

bd dep add "$DOC_API" "$PR3_READY"
bd dep add "$DOC_API" "$DEC_TESTHELPER"
bd dep add "$DOC_BACKENDS" "$PR3_READY"
bd dep add "$DOC_CHANGELOG" "$DOC_API"
bd dep add "$DOC_CHANGELOG" "$DEC_VISIBILITY"
bd dep add "$DOC_CHANGELOG" "$DEC_STUBS"
bd dep add "$DOC_CHANGELOG" "$ADR_LICENSE"
bd dep add "$DOC_CHANGELOG" "$ADR_CHATMODEL"
bd dep add "$DOC_REGISTER" "$DOC_API"

# ========================================
# Consumer verification and migrations
# ========================================

VERIFY_ADVISOR_REPLACE=$(bd create "Verify advisor green against local replace of eino-providers for one PR cycle" -p 0 -l consumer-verification,migration-advisor,release-gate,pr4 -t task --silent)
VERIFY_SYMPHONY_REPLACE=$(bd create "Verify local-symphony green against local replace of eino-providers for one PR cycle" -p 0 -l consumer-verification,migration-symphony,release-gate,pr5 -t task --silent)

ADVISOR_DELETE=$(bd create "Advisor migration delete lifted provider factory and backend files" -p 1 -l migration-advisor,pr4 -t task --silent)
ADVISOR_IMPORTS=$(bd create "Advisor migration update call sites to einoproviders NewProvider and promoted codex auth sentinels" -p 1 -l migration-advisor,pr4 -t task --silent)
ADVISOR_REPLAY=$(bd create "Advisor migration verify MCP replay corpus unchanged and authErr cause string preserved" -p 1 -l migration-advisor,testing,pr4 -t task --silent)

SYMPHONY_MODELS=$(bd create "Symphony adoption replace internal Ollama construction with eino-providers ollama NewChatModel" -p 1 -l migration-symphony,pr5 -t task --silent)
SYMPHONY_HEALTH=$(bd create "Symphony adoption replace PingOllama and ErrSparkUnreachable with shared ErrBackendUnreachable" -p 1 -l migration-symphony,pr5 -t task --silent)
SYMPHONY_ALIAS=$(bd create "Symphony decide whether to keep local ErrSparkUnreachable alias for telemetry string stability" -p 2 -l migration-symphony,api-decision,pr5 -t decision --silent)

bd dep add "$VERIFY_ADVISOR_REPLACE" "$PR3_READY"
bd dep add "$VERIFY_SYMPHONY_REPLACE" "$PR3_READY"
bd dep add "$VERIFY_SYMPHONY_REPLACE" "$SYM_PHASE0"

bd dep add "$ADVISOR_DELETE" "$VERIFY_ADVISOR_REPLACE"
bd dep add "$ADVISOR_IMPORTS" "$ADVISOR_DELETE"
bd dep add "$ADVISOR_REPLAY" "$ADVISOR_IMPORTS"

bd dep add "$SYMPHONY_MODELS" "$VERIFY_SYMPHONY_REPLACE"
bd dep add "$SYMPHONY_HEALTH" "$SYMPHONY_MODELS"
bd dep add "$SYMPHONY_ALIAS" "$SYMPHONY_HEALTH"

# ========================================
# Release v0.1.0 and future follow-up beads
# ========================================

RELEASE_AUDIT=$(bd create "Audit v0.1.0 release gates: decisions ADRs CI docs consumers and codex-auth-go publication" -p 0 -l release,release-gate -t chore --silent)
RELEASE_TAG=$(bd create "Tag eino-providers v0.1.0 after all release gates pass" -p 0 -l release,release-gate -t task --silent)
POST_V02=$(bd create "Plan v0.2.0 ChatModelFactory interface after consumers stabilize" -p 2 -l release,core -t task --silent)
POST_V03=$(bd create "Plan v0.3.0 RegisterProvider stable public API documentation" -p 3 -l release,docs -t task --silent)
POST_V10=$(bd create "Plan v1.0.0 after one month stable in advisor and symphony with no API churn" -p 3 -l release -t task --silent)

bd dep add "$RELEASE_AUDIT" "$EXT_CODEX_TAG"
bd dep add "$RELEASE_AUDIT" "$PR3_READY"
bd dep add "$RELEASE_AUDIT" "$CI_DEP_LEAK"
bd dep add "$RELEASE_AUDIT" "$CI_BENCH"
bd dep add "$RELEASE_AUDIT" "$VERIFY_ADVISOR_REPLACE"
bd dep add "$RELEASE_AUDIT" "$VERIFY_SYMPHONY_REPLACE"
bd dep add "$RELEASE_AUDIT" "$DOC_CHANGELOG"
bd dep add "$RELEASE_AUDIT" "$DEC_BASEURL"
bd dep add "$RELEASE_AUDIT" "$DEC_CLASSIFY"
bd dep add "$RELEASE_AUDIT" "$DEC_VISIBILITY"
bd dep add "$RELEASE_AUDIT" "$DEC_STUBS"
bd dep add "$RELEASE_AUDIT" "$DEC_TESTHELPER"
bd dep add "$RELEASE_AUDIT" "$DEC_LICENSE"
bd dep add "$RELEASE_AUDIT" "$DEC_CHATMODEL"
bd dep add "$RELEASE_AUDIT" "$DEC_EINO_PIN"

bd dep add "$RELEASE_TAG" "$RELEASE_AUDIT"
bd dep add "$POST_V02" "$RELEASE_TAG"
bd dep add "$POST_V03" "$POST_V02"
bd dep add "$POST_V10" "$POST_V03"

echo ""
echo "Bead graph created. Next commands:"
echo "  bd ready"
echo "  bd list --label release-gate"
