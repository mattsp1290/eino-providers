# ADR 0005: License MIT

## Status

Accepted

## Context

The repository started with an MIT license. Before external consumers adopt the
module, the project needed an explicit decision between keeping MIT and
relicensing to Apache 2.0 for patent-grant or upstream-alignment reasons.

The first consumers are owned by the same operator, and the immediate v0.1.0
goal is a small shared provider module rather than a broad ecosystem SDK.

## Decision

Keep the repository licensed under MIT for v0.1.0.

Do not relicense to Apache 2.0 as part of the initial external-consumer release.

## Consequences

The existing `LICENSE` file remains authoritative.

Consumers do not need to account for an Apache 2.0 notice file or patent-grant
terms for v0.1.0 adoption.

If the project later becomes a broader public SDK or needs alignment with an
upstream licensing policy, revisit relicensing as a separate decision with
explicit downstream-consumer impact review.
