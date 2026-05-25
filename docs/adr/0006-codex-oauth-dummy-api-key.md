# ADR 0006: Codex OAuth Dummy API Key

## Status

Accepted

## Context

The OpenAI-Codex provider uses the Eino OpenAI adapter, but authentication is
handled by a `codex-auth-go` HTTP client. That client injects subscription OAuth
bearer tokens and rewrites requests for the Codex endpoint.

The Eino OpenAI adapter rejects an empty API key during construction even when a
custom HTTP client will replace the authorization header later.

## Decision

The `openaicodex` backend sets a non-empty placeholder API key:

```text
codex-oauth-dummy
```

The value is not a secret and must never be sent as the effective
Authorization header. The Codex HTTP client owns request authentication.

## Consequences

The backend can construct the Eino OpenAI adapter without leaking an actual API
key into configuration. Tests for `openaicodex` must verify that the custom HTTP
client is required and that the backend rejects BaseURL and MaxTokens overrides.
