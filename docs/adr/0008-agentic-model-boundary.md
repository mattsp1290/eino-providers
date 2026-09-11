# ADR 0008: Native agentic model boundary

## Status

Accepted

## Context

Eino v0.9.19 introduces `model.AgenticModel` and `schema.AgenticMessage`.
Provider protocols carry data that cannot be faithfully represented through
the classic `schema.Message` API, including signed reasoning and response
continuation state.

## Decision

Concrete provider packages own native agentic constructors and codecs. The
root package defines only shared errors, limits, response identity, and an
opaque continuation envelope; it deliberately has no backend registry or
factory. Provider helpers split a response into a display-safe public message
and opaque versioned state, then restore the exact provider runtime values.

New agentic constructors account for every HTTP attempt and configure SDK
retries to zero where the SDK permits it. An extension-backed implementation
is acceptable only when local fake-endpoint tests prove it preserves native
blocks, identity, lifecycle, and continuation values. Unsupported native
features fail before transport with `ErrUnsupportedCapability`; they are never
silently flattened or discarded.

## Consequences

Applications import a concrete provider package and select its native
protocol explicitly. They may persist opaque continuation state but must not
render, log, or inspect it. Resource limits are shared and validated at
construction, while providers enforce them during encoding and stream parsing.
