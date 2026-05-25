# ADR 0003: Auth Mode Owned By Consumers

## Status

Accepted

## Context

Advisor historically used an environment-variable seam to choose between
OpenAI API-key mode and OpenAI-Codex OAuth mode. A shared library should not
hide environment reads in provider construction, and local-symphony has its own
configuration layer.

The registry already gives each backend a distinct provider name.

## Decision

`eino-providers` does not read auth-mode environment variables. Consumers decide
auth mode in their own configuration layer and call `NewProvider` with the
registered backend name they want, such as `openai` or `openai-codex`.

## Consequences

The shared module is deterministic from explicit inputs. Advisor can preserve
its env/config behavior by translating that setting before calling
`NewProvider`.

Factory seam tests from advisor should be adapted to verify explicit backend
selection rather than hidden environment routing.
