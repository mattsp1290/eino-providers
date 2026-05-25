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
	// and Gemini. It is ignored by Ollama and OpenAI-Codex.
	APIKey string

	// BaseURL overrides the backend endpoint. Nil means use the backend
	// default. OpenAI-Codex rejects non-nil BaseURL because its transport owns
	// endpoint rewriting.
	BaseURL *string

	// MaxTokens requests an output-token cap where the backend supports one.
	// OpenAI-Codex ignores this because the Codex endpoint manages output
	// length server-side.
	MaxTokens *int

	// KeepAlive configures Ollama model residency. Other backends ignore it.
	KeepAlive *time.Duration

	// HTTPClient supplies transport customization. OpenAI-Codex requires an
	// authenticated client from codex-auth-go; other backends may ignore it.
	HTTPClient *http.Client

	// GenaiClient reuses an existing Gemini client and skips cold-start client
	// construction when non-nil.
	GenaiClient *genai.Client
}
