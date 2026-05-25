# ADR 0007: Defer Raw Codex ChatModel

## Status

Accepted

## Context

Advisor has a raw HTTP Codex ChatModel wrapper that parses the Codex Responses
API stream directly. It is Eino-coupled, so it does not belong in
`codex-auth-go`, but it is also broader than the v0.1.0 provider extraction.

## Decision

Do not lift the raw HTTP Codex ChatModel in `eino-providers v0.1.0`.

The v0.1.0 Codex provider work uses the Eino OpenAI adapter with a
`codex-auth-go` HTTP client. The raw wrapper remains a later placement decision.

## Consequences

Advisor can keep its private wrapper during the first migration. A future ADR
can choose whether the wrapper moves into `openaicodex` or remains
consumer-private.
