# ADR 0004: Eino Version Pins

## Status

Accepted

## Context

Advisor already uses the CloudWeGo Eino `v0.8.x` line, while local-symphony
started on `v0.7.13`. The shared provider module needs one exact core version
so the `model.ToolCallingChatModel` and schema surfaces are tested consistently.

Backend packages also depend on separate `eino-ext` modules.

## Decision

For `v0.1.0`, pin `github.com/cloudwego/eino` to `v0.8.13`.

Use these backend extension pins unless a backend lift proves an incompatibility:

- `github.com/cloudwego/eino-ext/components/model/claude v0.1.17`
- `github.com/cloudwego/eino-ext/components/model/openai v0.1.13`
- `github.com/cloudwego/eino-ext/components/model/gemini v0.1.30`
- `github.com/cloudwego/eino-ext/components/model/ollama v0.1.9`

## Consequences

Consumers must align to Eino `v0.8.13` before adopting this module.
local-symphony's phase-zero bump handles that prerequisite.

Future Eino bumps should be deliberate PRs with backend tests and construction
benchmarks, not incidental dependency churn.
