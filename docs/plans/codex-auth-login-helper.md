# Plan (Option A): `codex-auth-go` login helper to unblock on-the-wire verification

Goal: create a credential file at `codex-auth-go`'s path/format so
`openaicodex.NewChatModel` stops returning `ErrNotLoggedIn` and the
`examples/toolcall` 2-turn exchange can be run on the live Codex endpoint.

## Why this is needed (current state)

- `openaicodex.NewChatModel` builds its client via
  `codexauth.NewClient(Options{AppName}).HTTPClient(ctx)`, which loads
  `~/Library/Application Support/<AppName>/auth.json` in codex-auth-go's **own**
  format (`{"openai":{"access","refresh","expires","accountId"}}`).
- That file does **not** exist for any app name. The user's `~/.codex/auth.json`
  (codex CLI, `{"tokens":{...}}` format, mtime May 23) is a different path **and**
  format; codex-auth-go never reads it.
- So a `codex-auth-go`-specific login is required. Option A does that cleanly via
  the library's own OAuth flow (no hand-copying of credentials — that's Option B).

## What the login flow actually does (verified in codex-auth-go@v0.1.0)

`Client.Login(ctx, forceDevice)` (`public.go`):
- If `forceDevice` is false and a browser is available (`canOpenBrowserNative`),
  runs `LoginBrowser`; otherwise `LoginDevice`.

`LoginBrowser` (`login_browser.go`):
1. PKCE verifier/challenge + CSRF state.
2. `StartCallbackServer` binds **127.0.0.1:1455 and [::1]:1455** (the `OAuthPort`
   constant — `Client.CallbackPort` is **not** used by the browser flow; the
   `RedirectURI` `http://localhost:1455/auth/callback` is registered with the
   OAuth app, so the port is effectively fixed). EADDRINUSE → `*PortInUseError`
   (no browser opened).
3. Opens the browser to `auth.openai.com`; if none, prints the URL to stderr.
4. Waits up to **5 min** for the callback, exchanges the code, extracts the
   account id, and `Save`s to `~/Library/Application Support/<AppName>/auth.json`
   (0600 file, 0700 dir).

`LoginDevice` (`login_device.go`): prints `Open https://auth.openai.com/codex/device
… enter code: XXXX` and polls up to ~15 min. **No local port** — the fallback when
1455 is occupied or no browser is available.

`HTTPClient`'s transport auto-refreshes a stale access token using the refresh
token on the first request, so a freshly-saved credential set immediately works.

## Local source discovery (v0.2.0) and format compatibility

The user's `~/git/codex-auth-go` is **v0.2.0**; eino-providers' `go.mod` pins
**v0.1.0**. Verified facts:
- The on-disk `auth.json` format is **identical** across v0.1.0/v0.2.0
  (`storedCreds{access,refresh,expires,accountId}`, `diskAuthFile{openai}`), so a
  login performed by **either** version is readable by eino-providers' pinned
  v0.1.0 client. **A version bump is NOT required to unblock verification.**
- `Login(ctx, forceDevice)`, `Status(ctx)`, `HTTPClient(ctx)`, and
  `Options{AppName, CallbackPort}` are unchanged. v0.2.0 adds an optional
  `Options.DevicePrompt func(verificationURI, userCode string) error` (zero value
  is backward-compatible).
- **v0.2.0 already ships `cmd/smoke/main.go`** which does exactly Option A: reads
  `CODEX_AUTH_APPNAME` (default `codex`), `Status` → `Login(ctx,false)` if needed,
  then POSTs a minimal Responses body to `CodexEndpoint` and checks the status —
  i.e. it logs in AND smoke-tests the wire in one shot.

## Two sub-options

**A1 — reuse the existing `cmd/smoke` (fastest, recommended to unblock now).**
From `~/git/codex-auth-go`, run:
```bash
CODEX_AUTH_APPNAME=ag-ui-go-server-example go run ./cmd/smoke
```
It logs in (browser flow, user approves) and validates the endpoint. Because the
format matches, the written file is then read by eino-providers' v0.1.0 client.
No new code in eino-providers.

**Critical (env-var coupling — top failure risk).** `cmd/smoke` reads
`CODEX_AUTH_APPNAME` (default `codex`) while `examples/toolcall` reads
`CODEX_APP_NAME` (default `ag-ui-go-server-example`). They must resolve to the
**same** app-name string or the example still sees `ErrNotLoggedIn`. Drive both
from one shell variable set to `ag-ui-go-server-example` (see Execution).

**Port 1455 caveat (corrected).** `cmd/smoke` calls `Login(ctx, false)`, and the
device flow is reached only when no browser is available. On this Mac (browser
present), a busy port 1455 yields `*PortInUseError` and `cmd/smoke` aborts — there
is **no** automatic device fallback via A1. If 1455 is occupied, **free it**
(quit the Codex desktop app / any process holding the OAuth callback port). The
`CODEX_LOGIN_DEVICE=1` / forced device flow is an **A2-only** capability
(`Login(ctx, true)`).

**A2 — add a committed `openaicodex/examples/login` helper (nicer artifact).**
A consumer-facing login helper in eino-providers (compiles against pinned v0.1.0),
making the `examples/toolcall` README concrete. More code; arguably beyond the
minimal unblock. Could share a single `CODEX_APP_NAME` default with the example.

## Deliverable (A2): `openaicodex/examples/login`

A small `package main` helper (mirrors `examples/toolcall`) that runs the
codex-auth-go login for the **same app name the example uses**, then prints the
saved path + account id.

```go
appName := envOr("CODEX_APP_NAME", "ag-ui-go-server-example")
forceDevice := os.Getenv("CODEX_LOGIN_DEVICE") == "1" // or a -device flag
c := codexauth.NewClient(codexauth.Options{AppName: appName})
creds, err := c.Login(ctx, forceDevice)
// on *PortInUseError: tell the user to free :1455 or set CODEX_LOGIN_DEVICE=1
// on success: print c.Path() and creds.AccountID
```

Behavior:
- Default app name **`ag-ui-go-server-example`**, matching
  `examples/toolcall`'s default so the file the login writes is the file the
  example reads. (Both already read `CODEX_APP_NAME`; keep them identical.)
  Using a distinct name also avoids the macOS case-insensitive collision between
  app name `codex` and the existing Electron data dir `~/Library/Application
  Support/Codex/`.
- `-device` / `CODEX_LOGIN_DEVICE=1` forces the device flow for headless or
  port-1455-occupied cases.
- Clear messaging for `*PortInUseError` and generic errors.
- Add a short README documenting both flows and the app-name/example pairing.

## Execution sequence

**Recommended (A1).** The login blocks on a human browser approval (up to 5 min),
which the agent cannot perform — so **the user runs the login**, via the `!`
prefix so output lands in this session (not agent-launched in the background,
which would risk a hung multi-minute tool call). Use one shell variable to avoid
the env-var foot-gun:

```bash
export CODEX_APP_NAME=ag-ui-go-server-example
# 1) login + endpoint smoke, from ~/git/codex-auth-go (v0.2.0, isolated module):
CODEX_AUTH_APPNAME=$CODEX_APP_NAME go run ./cmd/smoke
# 2) acceptance, from ~/git/eino-providers:
go run ./openaicodex/examples/toolcall
```

1. User runs step 1 (approves in browser). If `:1455` is busy, free it first.
2. Verify `~/Library/Application Support/ag-ui-go-server-example/auth.json` exists
   (re-run the existence check).
3. User runs step 2. **This is the real acceptance proof** — `cmd/smoke` only
   validates auth + endpoint reachability with a trivial string `input` (no tools,
   no streaming, no SSE-item parsing); a green smoke must NOT be treated as
   success for the ChatModel. `examples/toolcall` exercises structured input,
   streaming, tool calling, and the multi-turn round-trip.

**Optional (A2), deferred.** A committed `openaicodex/examples/login` helper
(compiled against pinned v0.1.0) is out of scope for unblocking now and would
diverge from v0.2.0's `Options.DevicePrompt`/future API; file a follow-up bead if
a permanent helper is ever wanted. A2 is also the only path that can force the
device flow (`Login(ctx, true)`) for a headless or port-1455-blocked machine.

## Risks / decisions for review

- **Port 1455 contention.** The Codex desktop app or a `codex login` may hold
  1455. Browser flow then fails with `*PortInUseError`; device flow is the
  documented fallback. Plan surfaces both.
- **Interactive OAuth under an agent.** The browser approval is the user's step;
  the helper must be runnable standalone. Confirm the preferred run mechanism
  (user-run via `!` vs agent-launched background + monitor).
- **App-name coupling.** Login helper and example MUST agree on `AppName`/the
  `CODEX_APP_NAME` default, or the example still sees `ErrNotLoggedIn`.
- **Scope / commit vs throwaway.** Is a committed `examples/login` helper in
  scope (it's reusable by any consumer and makes the toolcall README concrete),
  or should it be a throwaway used only to log in? 
- **Token lifetime.** Saved creds refresh automatically via the transport, so a
  one-time login should keep working for the verification session.
- **Security.** Login writes the user's real OAuth tokens to a 0600 file via the
  library's own `Save`; no credential copying or logging of token values.

## Success criteria

- `~/Library/Application Support/ag-ui-go-server-example/auth.json` exists after
  login (codex-auth-go format).
- `examples/toolcall` completes a 2-turn tool-calling exchange on the live
  endpoint, demonstrating streaming + tool call + tool result + streamed final
  answer, confirming the ChatModel's wire shape end-to-end.
