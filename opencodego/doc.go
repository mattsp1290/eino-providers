// Package opencodego integrates the OpenCode Go inference service with Eino.
//
// Importing this package registers the provider name "opencode-go" with the
// root einoproviders package. NewChatModel constructs a reusable
// model.ToolCallingChatModel directly. Callers must select one native protocol
// explicitly: chat-completions, messages, or responses.
//
// The host supplies its model, user agent, and conversation session. A session
// attached with opencodeauth.WithSessionID overrides the configured fallback.
// BaseURL is an API root; the selected protocol route is appended internally.
// An optional HTTPClient is copied and decorated with authentication while its
// timeout and transport policy are preserved.
//
// Generate, Stream, and WithTools support text and function-tool exchanges.
// Direct Messages models require a positive MaxTokens value and use the pinned
// Anthropic SDK retry policy. Responses supports stateless encrypted reasoning
// replay, but not multimodal input, built-in server tools, or prior-response
// server state. Individual models may support a narrower protocol or tool set.
//
// Provider failures retain einoproviders error sentinels and safe typed
// opencodeauth HTTP details for errors.Is and errors.As inspection.
package opencodego
