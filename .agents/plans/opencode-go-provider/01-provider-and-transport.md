# Provider, configuration, and transport

## WP1: dependency and public boundary

Prerequisite: use a clean implementation branch based on current main, preserving unrelated user files. Confirm the pinned dependency resolves without local replacements. Change existing `go.mod` and `go.sum` to require `github.com/mattsp1290/opencode-auth-go v0.0.0-20260908211055-a3f44cca7a18`.

New package directory `opencodego/` is proposed under the existing repository root. All files and symbols named below within that directory are new/proposed unless explicitly described as external APIs.

Add to existing `options.go`, inside `Options`, proposed string fields `Protocol`, `UserAgent`, and `SessionID`, documented as OpenCode Go settings. Other backend constructors ignore them. No auth-module types or imports enter the root package. Existing `APIKey`, `BaseURL`, `HTTPClient`, and `MaxTokens` carry their ordinary values into this backend. `HTTPClient` means an unauthenticated base client to be copied and decorated by the auth library, not an already decorated client.

New `opencodego/config.go` defines proposed `Protocol` as a string type and constants `ProtocolChatCompletions`, `ProtocolMessages`, and `ProtocolResponses`, with values matching the external auth module. Define proposed `ChatModelConfig` with fields:

| Field | Contract |
| --- | --- |
| `Model string` | Required non-whitespace model ID, forwarded unchanged. |
| `Protocol Protocol` | Required explicit selection; empty and unknown values are init errors. |
| `APIKey string` | Empty delegates environment fallback to auth `NewClient`. |
| `UserAgent string` | Required host identifier; delegate validation to auth library. |
| `SessionID string` | Optional configured conversation fallback. Per-operation auth context overrides it. |
| `BaseURL string` | Optional auth API root, not a complete inference endpoint. |
| `HTTPClient *http.Client` | Optional base client copied by auth library; its transport is trusted. |
| `MaxTokens *int` | Optional positive cap; required for Messages `NewChatModel`. |

Keep the initial configuration bounded to these fields. Eino common per-call model, token cap, tools, and tool-choice options are specified in WP3. Do not expose a raw arbitrary-headers map or SDK-specific escape hatch.

New `opencodego/provider.go` defines proposed `Provider`, `init`, and unexported construction helpers. `init` registers exactly `opencode-go`. The registration maps root options into the backend config, interpreting nil `BaseURL` as empty. Validate model, protocol, any provided cap, and auth configuration at construction. Do not require a configured session: operation contexts can supply it later. Do not capture the constructor context for future inference.

Construct and retain one immutable auth client per provider. Snapshot pointer values to avoid retaining caller-mutable token caps. `Advise` builds the selected model using this client and sends exactly a system and user message. Its positive `maxTokens` argument overrides the configured cap for that invocation. Reject nonpositive `maxTokens` locally with `ErrProviderInit`; no request occurs. Messages construction therefore works via root `NewProvider` with nil `Options.MaxTokens`, because `Advise` supplies its required cap. Return message content and `ExtractUsage`; never dereference a nil successful message.

New `opencodego/chatmodel.go` exposes proposed `NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error)`. It shares validation/client construction with the registered provider. Unlike the single-shot provider, this constructor requires a positive configured cap for Messages. Chat Completions and Responses may omit a cap. No constructor performs model discovery, paid inference, login, or filesystem credential lookup.

Verification: add new `opencodego/provider_test.go` and `opencodego/config_test.go`; test registration through the public root factory from an external test package, each protocol, required-field errors, nil versus nonpositive caps, `Advise` override, missing usage, explicit key versus environment, and no-network construction. Run `go test ./opencodego ./...` once dependent adapters exist. WP1 alone may compile with backend dispatch deferred, but must not register a provider that returns success for unsupported behavior in a mergeable package.

## WP2: HTTP and error composition

New `opencodego/transport.go` wraps the `Transport` of the fresh client returned by auth `HTTPClient()`. Preserve its redirect policy, timeout, cookie jar, and other client fields; do not modify the caller's original client. The local outer transport invokes the authenticated inner transport. New `opencodego/observation.go` defines a proposed private operation-state context value containing the last attempt's typed HTTP failure and wire usage observations. The facade creates a fresh state for each Generate/Stream/Advise call and passes a derived context to the SDK, preserving all parent context values and cancellation. State is never stored on the shared model. Synchronize reads/writes because SDK producers and facade consumers may run concurrently. Clear attempt state at the beginning of each HTTP attempt so a later network error cannot inherit a stale HTTP classification.

On non-2xx, external `DecodeHTTPError` consumes its bounded body and closes it. Record its typed error in this operation's state. Return a copied response with its original status and retry-control headers, and a new minimal native JSON error body containing only a fixed safe message. Set JSON Content-Type, recompute ContentLength, clear obsolete content encoding/length headers, and leave Retry-After and x-should-retry intact. Do not return a nil response for an ordinary HTTP error. The facade combines the final SDK failure with the final attempt's recorded HTTPError, using a safe Error string and Unwrap preserving both causes. A final successful attempt clears prior HTTP errors. For the direct Responses path, the same state supplies HTTPError before decoding any synthetic error body.

On SDK-path 2xx responses, install the incremental observer described in WP3 without changing bytes through the native terminal frame; after that frame, synthesize EOF and close the underlying body. Direct Responses uses its own parser. On network failure, close any accompanying response and preserve context cancellation/deadlines. Forward `CloseIdleConnections` to the inner transport when supported.

Messages uses the pinned Anthropic SDK's default two retries (at most three attempts for replayable requests), with ordinary permanent 4xx errors not retried unless the service explicitly requests retries. OpenCode's auth transport itself never retries. Retain status/header retry policy and do not add retries or mid-stream retries. All attempts retain the same operation context and session. The pinned SDK uses time.Sleep for backoff; cancellation stops further network I/O but may only return after its current sleep. Document this limit rather than promising immediate backoff interruption. Retry-After delays accepted by that SDK can approach one minute. Fixtures use short explicit delays and prove no subsequent base-transport call occurs after cancellation.

New `opencodego/errors.go` defines proposed unexported error mapping helpers. Preserve known auth sentinels with `errors.Is`, typed `*opencodeauth.HTTPError` with `errors.As`, and cancellation/deadline identity. Wrap constructor errors with root `WrapInitError`. Missing API key is also auth-classified. At invocation, recognized authentication/quota kinds map through `WrapAuthError`; HTTP 401/403 with unknown kind also map to auth. Rate-limit, model, policy, and other unsuccessful responses map to `ErrProviderAPI` while retaining HTTPError kind/status/RetryAfter. A recognized policy kind takes precedence over the 403 fallback. Deadline/timeout maps to `ErrProviderTimeout`; cancellation remains discoverable without being called an authentication failure. Missing/invalid session and disallowed-request sentinels map to `ErrProviderAPI`, preserving the cause.

Error text must use local operation labels, not raw native error bodies, headers, credentials, session IDs, request URLs containing secrets, or panic values. For arbitrary network/SDK errors, a proposed private wrapper provides a fixed safe `Error()` string while `Unwrap()` preserves the cause. Never print raw wrapped causes in diagnostics. New HTTPError classification is backend-local; do not change root `Classify` ordering or import auth types into root `errors.go`.

Verification: new `opencodego/transport_test.go`, `opencodego/observation_test.go`, and `opencodego/errors_test.go` exercise real auth transport composition with a recording base transport. Cover non-2xx closure, byte-identical 2xx delivery through the native terminal frame, bounded errors, typed causes after SDK wrapping, retry then success/network failure without stale error state, timeout/cancellation, session precedence, redirects, invalid paths, and header replacement. Test sentinel secret markers never appear in public error strings. WP2 depends on WP1's config and is required by every protocol adapter.
