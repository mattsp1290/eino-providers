# OpenCode Go two-turn tool-call example

This compile-checked example binds one synthetic code-index tool, streams one
tool call, supplies its correlated result, and streams the final answer. It is
bounded to two model turns and never selects a model or protocol automatically.

The host must supply every value explicitly:

```bash
OPENCODE_GO_API_KEY=... \
OPENCODE_GO_MODEL=... \
OPENCODE_GO_PROTOCOL=responses \
OPENCODE_GO_USER_AGENT=my-coding-agent/1.0 \
OPENCODE_GO_SESSION_ID=host-conversation-id \
go run ./opencodego/examples/toolcall
```

`OPENCODE_GO_PROTOCOL` must be `chat-completions`, `messages`, or `responses`.
The program does not print the session ID. Running it performs paid inference;
ordinary `go build ./...` and `go test ./...` only compile it.
