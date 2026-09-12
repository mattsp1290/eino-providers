# Native agentic capabilities

This document records the protocol boundary for the native agentic
constructors. A capability not listed as native is rejected before request
dispatch rather than flattened into a classic `schema.Message`.

| Provider/protocol | Native input/output slice | Explicitly rejected before dispatch |
| --- | --- | --- |
| OpenAI Responses | text, reasoning, function calls/results, request-time tools, Responses server/MCP features supplied by the upstream adapter | malformed or unsupported upstream agentic blocks/options |
| Claude Messages | system/user/assistant text, thinking/signatures, function calls/results | request-time tools, deferred/client tool search, and unsupported block kinds |
| Gemini generateContent | upstream native multimodal/function/server-tool surface and agentic choice | deferred tools and client tool search |
| Ollama `/api/chat` | text/image input, thinking, text/function output, ordinary tools | agentic choice, deferred/search/server/MCP tools, unsupported media |
| OpenAI-Codex Responses | system/user/assistant text, encrypted reasoning, function calls/results, ordinary tools | deferred/search/server/MCP tools and unsupported media |
| OpenCode Responses | native Responses adapter surface | deferred/client tool search |
| OpenCode Messages | native Messages surface | deferred/client tool search |
| OpenCode Chat Completions | native Chat Completions surface | deferred/client tool search and agentic tool choice |

All constructors validate the shared `AgenticLimits` contract and apply shared
content-block and decoded inline-media bounds to inputs and streamed/returned
outputs (including nested function-result media).
Adapter-backed constructors use an immutable limiting HTTP-client wrapper for
request, response, error-body, and SSE/NDJSON event limits; an opaque
caller-provided Gemini client cannot be transport-wrapped by its public SDK.
Every provider exposes public
`SplitAgenticContinuation` and `RestoreAgenticContinuation` helpers; OpenCode
requires its response identity to preserve the selected protocol. Deterministic
fixture tests are the source of truth for each implemented request/response
codec; credentialed live verification remains opt-in and is never used as
ordinary CI evidence.

## Continuation persistence boundary

Continuation helpers preserve JSON-safe extras and reasoning signatures through
a sanitized public projection and opaque state. They do not promise lossless
round-tripping of adapter-private Go runtime values stored in `any` extensions
after a type-erasing JSON persistence cycle. Consumers requiring such private
replay values must keep the provider-native continuation in-process; this
library will not fabricate or reinterpret private runtime types.

## Stream lifecycle boundary

`schema.StreamReader.Close` in the pinned Eino v0.9.19 API closes only its
internal receiver and does not expose a close callback to a provider-owned
HTTP-body reader. Native constructors therefore honor context cancellation and
close response bodies when their producer exits, but cannot guarantee that an
early consumer `Close` interrupts a scanner currently blocked in a body read.
This is an accepted upstream API boundary, not a successful-completion signal;
callers that need immediate cancellation must cancel the stream context.

## Explicit degraded paths

Gemini accepts a caller-owned `*genai.Client` for cache/transport ownership.
That SDK does not expose its underlying HTTP transport, so byte-level request,
response, error-body, and event caps cannot be injected after construction;
the shared content-block and inline-media checks still apply. Callers requiring
full byte-limit enforcement should provide `HTTPClient` and let this package
construct the Gemini client. Claude request-time `WithTools` is deliberately
rejected before dispatch until its native Messages tool encoding has complete
fixture coverage.
