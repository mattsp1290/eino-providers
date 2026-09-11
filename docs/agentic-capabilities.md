# Native agentic capabilities

This document records the protocol boundary for the native agentic
constructors. A capability not listed as native is rejected before request
dispatch rather than flattened into a classic `schema.Message`.

| Provider/protocol | Native input/output slice | Explicitly rejected before dispatch |
| --- | --- | --- |
| OpenAI Responses | text, reasoning, function calls/results, request-time tools, Responses server/MCP features supplied by the upstream adapter | malformed or unsupported upstream agentic blocks/options |
| Claude Messages | system/user/assistant text, thinking/signatures, function calls/results | deferred/client tool search and unsupported block kinds |
| Gemini generateContent | upstream native multimodal/function/server-tool surface and agentic choice | deferred tools and client tool search |
| Ollama `/api/chat` | text/image input, thinking, text/function output, ordinary tools | agentic choice, deferred/search/server/MCP tools, unsupported media |
| OpenAI-Codex Responses | system/user/assistant text, encrypted reasoning, function calls/results, ordinary tools | deferred/search/server/MCP tools and unsupported media |
| OpenCode Responses | native Responses adapter surface | deferred/client tool search |
| OpenCode Messages | native Messages surface | deferred/client tool search |
| OpenCode Chat Completions | native Chat Completions surface | deferred/client tool search and agentic tool choice |

All constructors validate the shared `AgenticLimits` contract. Adapter-backed
constructors use an immutable limiting HTTP-client wrapper for request,
response, error-body, SSE/NDJSON event, and content-block limits; an opaque
caller-provided Gemini client cannot be transport-wrapped by its public SDK,
but still receives content-block validation. Every provider exposes public
`SplitAgenticContinuation` and `RestoreAgenticContinuation` helpers; OpenCode
requires its response identity to preserve the selected protocol. Deterministic
fixture tests are the source of truth for each implemented request/response
codec; credentialed live verification remains opt-in and is never used as
ordinary CI evidence.
