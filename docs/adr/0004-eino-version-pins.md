# ADR 0004: Eino Version Pins

## Status

Accepted

## Context

The initial provider surface used the CloudWeGo Eino `v0.8.x` line. Native
agentic models require Eino's `model.AgenticModel` and `schema.AgenticMessage`
surfaces, which were stabilized on the v0.9 line. The shared provider module
needs one exact core version so classic and agentic model surfaces are tested
consistently.

Backend packages also depend on separate `eino-ext` modules.

## Decision

For the native agentic-model release, pin `github.com/cloudwego/eino` to
`v0.9.19`.

Use these backend extension pins unless a backend lift proves an incompatibility:

- `github.com/cloudwego/eino-ext/components/model/claude v0.1.17`
- `github.com/cloudwego/eino-ext/components/model/openai v0.1.13`
- `github.com/cloudwego/eino-ext/components/model/gemini v0.1.30`
- `github.com/cloudwego/eino-ext/components/model/ollama v0.1.9`

## Consequences

Consumers must align to Eino `v0.9.19` before adopting the agentic
constructors. This is a deliberate breaking dependency upgrade; classic
constructors remain available but are not a compatibility promise for older
Eino versions.

Future Eino bumps should be deliberate PRs with backend tests and construction
benchmarks, not incidental dependency churn.
