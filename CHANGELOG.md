# Changelog

This changelog is maintained by hand until `v1.0.0`.

## Unreleased

- No unreleased changes yet.

## v0.1.0

- Initial extracted provider module for CloudWeGo Eino consumers.
- Added the root `Provider` contract, `Usage` accounting, `Options`,
  registry-backed `NewProvider`, error sentinels, and error classification.
- Added opt-in backend packages for Claude, OpenAI, OpenAI-Codex, and Gemini.
- Added the Ollama `NewChatModel` helper with BaseURL validation, health
  probing, keep-alive parsing, and Eino `ChatModelConfig` construction.
- Added OpenAI-Codex support through `codex-auth-go` OAuth transport, including
  plan/quota auth error classification.
- Added documentation for backend quirks, public API stability, keyed
  `Options` literals, and registry experimental status.
- Added ADRs for registry self-registration, BaseURL pointer semantics,
  consumer-owned auth mode, Eino version pins, MIT licensing, Codex dummy API
  key behavior, and raw Codex chat model deferral.
- Added CI checks for tests, lint, go mod tidy, non-Codex dependency leaks, and
  Gemini construction benchmark regressions.

Open deferrals:

- No Bedrock, Together, or vLLM stub backends in v0.1.0.
- Raw HTTP Codex chat model factory is deferred; v0.1.0 uses the Eino OpenAI
  adapter plus Codex OAuth transport.
- `ChatModelFactory` remains a v0.2.0 design direction; v0.1.x keeps the
  single-shot `Provider.Advise` compatibility surface.
- Advisor still needs call-site migration before deleting its lifted internal
  provider/factory files.
- local-symphony verification against a local replace is blocked by unrelated
  consumer test failures in worker/shell tests.
