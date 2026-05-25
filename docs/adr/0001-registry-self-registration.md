# ADR 0001: Registry Self-Registration

## Status

Accepted

## Context

`eino-providers` exposes a root factory, `NewProvider`, while each backend has
different SDK and authentication dependencies. Some dependencies are heavy or
security-sensitive. In particular, the OpenAI-Codex backend imports
`github.com/mattsp1290/codex-auth-go`, which brings OAuth and credential-store
code that Claude, OpenAI API-key, Gemini, and Ollama consumers should not load
unless they explicitly opt in.

If the root package imported every backend package directly, any consumer that
only wanted one provider would still receive the transitive dependency surface
for all providers. That would defeat the extraction goal of keeping backend
dependencies independently importable.

## Decision

Use a registry pattern like `database/sql` drivers.

Backend packages register themselves from `init`:

```go
func init() {
	einoproviders.RegisterProvider("claude", newProvider)
}
```

Consumers opt into the backends they want with side-effect imports:

```go
import (
	einoproviders "github.com/mattsp1290/eino-providers"
	_ "github.com/mattsp1290/eino-providers/claude"
)
```

The root package owns `RegisterProvider`, lookup, duplicate-name detection, and
`NewProvider`. It must not import backend subpackages.

Duplicate registration panics because it is a programming/configuration error,
matching `database/sql.Register`. The registry uses a mutex so lookup and
registration are race-safe, but registration after package initialization is not
part of the normal orchestration model.

## Consequences

Consumers must import backend packages for side effects before calling
`NewProvider`. Missing imports produce `ErrUnknownProvider`.

The root package stays small and does not pull `codex-auth-go` or backend SDKs
unless the consumer imports the corresponding backend package.

CI should include a transitive-dependency guard that imports non-Codex backends
and verifies `codex-auth-go` is absent from their dependency graphs.

Out-of-tree providers can register without forking this module once
`RegisterProvider` is documented as stable.

## Rejected Alternative

The root package could import every backend and switch directly on provider
name. That would make examples slightly shorter, but it would force every
consumer to download and compile every backend's SDK and the Codex OAuth stack.
That dependency leak is more costly than requiring explicit side-effect imports.
