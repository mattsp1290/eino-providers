# openai-codex ChatModel: 2-turn tool-calling example

Demonstrates the streaming, tool-calling `openaicodex` ChatModel against the
ChatGPT/OpenAI **Codex subscription** backend, using only the public
`eino-providers` surface plus `cloudwego/eino/schema`.

The program runs a 2-turn exchange:

1. Binds one `get_weather` tool, calls `Stream`, and observes the model emit a
   tool call (prints its id, name, and JSON arguments).
2. Feeds back a `role=tool` result via `schema.ToolMessage`, calls `Stream`
   again, and prints the final answer **token-by-token**, then token usage.

## Tested model

- **Model:** `gpt-5.5` (the `codex` CLI default in `~/.codex/config.toml`;
  `model_reasoning_effort = "medium"`). Override with `CODEX_MODEL`.
- Reasoning is enabled by default and `reasoning.encrypted_content` items are
  round-tripped between turns (required for multi-turn tool calling on reasoning
  models). The example preserves the full assistant `schema.Message` (including
  `Extra`) across turns so the round-trip works — consumers must do the same.

## Authentication / login

Credentials are loaded by `codex-auth-go` for the configured **app name**
(default `ag-ui-go-server-example`, override with `CODEX_APP_NAME`). They resolve
to the OS config dir, e.g. on macOS:

```
~/Library/Application Support/<app-name>/auth.json
```

> Note: this is **not** the `codex` CLI's `~/.codex/auth.json` (a different
> format and path). You must have a `codex-auth-go` session for the matching app
> name. If none exists, the program prints a "not logged in" message
> (`codexauth.ErrNotLoggedIn`).

Logging in is done through `codex-auth-go`'s OAuth flow (browser or device).
See the `codex-auth-go` `Client.Login` API / its `cmd/smoke` helper for the
login entry point, run with the same app name you pass here.

## Run

```bash
# default app name "ag-ui-go-server-example", model "gpt-5.5"
go run ./openaicodex/examples/toolcall

# or override
CODEX_APP_NAME=my-app CODEX_MODEL=gpt-5.5 go run ./openaicodex/examples/toolcall
```

## What success looks like

- Turn 1 prints a `-> tool call:` line with a stable `call_*` id, `get_weather`,
  and JSON arguments containing a city.
- Turn 2 streams the assistant's final natural-language answer incrementally and
  prints a `usage:` line when the endpoint provides token counts.
