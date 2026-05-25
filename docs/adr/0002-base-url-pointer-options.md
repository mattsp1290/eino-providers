# ADR 0002: Pointer-Typed BaseURL

## Status

Accepted

## Context

Backends interpret endpoint overrides differently. Claude uses a pointer
configuration field, OpenAI and Ollama use strings where empty means default,
and OpenAI-Codex must reject endpoint overrides because its authenticated
transport rewrites request URLs itself.

Using a plain string in the shared Options type would collapse "not set" and
"explicitly empty" into the same value and make backend validation less clear.

## Decision

`Options.BaseURL` is `*string`.

Nil means the caller did not request an endpoint override. Non-nil means the
caller intentionally supplied one, even if the string is empty.

## Consequences

Backend constructors can apply their native field shape without guessing caller
intent. OpenAI-Codex can reject any non-nil BaseURL before constructing its
client.

Callers must take the address of string values when setting BaseURL. This is a
small ergonomics cost in exchange for unambiguous API semantics.
