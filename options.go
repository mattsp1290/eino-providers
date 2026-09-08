package einoproviders

import (
	"net/http"
	"time"

	"google.golang.org/genai"
)

// Options configures provider construction.
//
// Use field names in struct literals. Backends ignore fields that do not apply
// to them and validate fields that are forbidden for their endpoint semantics.
type Options struct {
	// APIKey configures API-key authenticated backends such as Claude, OpenAI,
	// Gemini, and OpenCode-Go. It is ignored by Ollama and OpenAI-Codex.
	APIKey string

	// Protocol selects the OpenCode-Go native inference protocol. It is ignored
	// by every other backend. Use the typed constants from the opencodego
	// package when setting this string field.
	Protocol string

	// UserAgent identifies the host coding agent to OpenCode-Go. It is required
	// by that backend and ignored by every other backend.
	UserAgent string

	// SessionID supplies the fallback OpenCode-Go conversation identifier.
	// A session attached to an operation context takes precedence. It is ignored
	// by every other backend.
	SessionID string

	// BaseURL overrides the backend endpoint. Nil means use the backend
	// default. OpenCode-Go interprets it as the API root and appends the selected
	// native inference route. OpenAI-Codex rejects non-nil BaseURL because its
	// transport owns endpoint rewriting.
	BaseURL *string

	// MaxTokens requests an output-token cap where the backend supports one.
	// OpenCode-Go's direct Messages chat-model constructor requires a positive
	// cap. Its registered Provider may leave this nil because Advise supplies
	// the positive cap per invocation. Chat Completions and Responses allow nil.
	// OpenAI-Codex ignores this because the Codex endpoint manages output
	// length server-side.
	MaxTokens *int

	// KeepAlive configures Ollama model residency. Other backends ignore it.
	KeepAlive *time.Duration

	// HTTPClient supplies transport customization. OpenCode-Go treats it as an
	// unauthenticated base client that opencode-auth-go copies and decorates.
	// OpenAI-Codex requires an authenticated client from codex-auth-go; other
	// backends may ignore it.
	HTTPClient *http.Client

	// GenaiClient reuses an existing Gemini client and skips cold-start client
	// construction when non-nil.
	GenaiClient *genai.Client
}
