package opencodego

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// Protocol identifies an OpenCode Go native inference request format.
type Protocol string

// Protocol values are kept equal to the values accepted by opencode-auth-go.
const (
	ProtocolChatCompletions Protocol = Protocol(opencodeauth.ProtocolChatCompletions)
	ProtocolMessages        Protocol = Protocol(opencodeauth.ProtocolMessages)
	ProtocolResponses       Protocol = Protocol(opencodeauth.ProtocolResponses)
)

// ChatModelConfig configures an OpenCode Go ToolCallingChatModel.
//
// Authentication and endpoint validation are delegated to opencode-auth-go
// when the provider or chat model is constructed. Protocol and model
// validation is performed locally so invalid requests fail before network I/O.
type ChatModelConfig struct {
	// Model is the OpenCode Go model identifier. It must contain non-whitespace
	// text and is forwarded unchanged.
	Model string

	// Protocol selects the native OpenCode Go request format. It is required.
	Protocol Protocol

	// APIKey is the OpenCode Go API key. An empty value delegates environment
	// fallback to opencode-auth-go.
	APIKey string

	// UserAgent identifies the host coding agent. It is required by OpenCode Go.
	UserAgent string

	// SessionID is an optional configured conversation fallback. A session ID
	// attached to an operation context takes precedence.
	SessionID string

	// BaseURL is the optional OpenCode Go API root, rather than an inference
	// endpoint. An empty value uses the auth library's default.
	BaseURL string

	// HTTPClient is an optional unauthenticated base client. The auth library
	// copies and decorates it with OpenCode Go authentication.
	HTTPClient *http.Client

	// MaxTokens is an optional output cap. Messages chat models require a
	// positive configured value; Advise may supply its cap per invocation.
	MaxTokens *int
}

// validateConfig checks the fields owned by this package. Authentication,
// session, and endpoint syntax remain the responsibility of opencode-auth-go.
// requireMessagesMaxTokens distinguishes direct Messages model construction
// from Provider.Advise, which supplies its cap when it sends a request.
func validateConfig(cfg ChatModelConfig, requireMessagesMaxTokens bool) error {
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("opencodego: Model is required")
	}

	switch cfg.Protocol {
	case ProtocolChatCompletions, ProtocolMessages, ProtocolResponses:
	default:
		if cfg.Protocol == "" {
			return fmt.Errorf("opencodego: Protocol is required")
		}
		return fmt.Errorf("opencodego: unknown Protocol %q", cfg.Protocol)
	}

	if strings.TrimSpace(cfg.UserAgent) == "" {
		return fmt.Errorf("opencodego: UserAgent is required")
	}
	if cfg.MaxTokens != nil && *cfg.MaxTokens <= 0 {
		return fmt.Errorf("opencodego: MaxTokens must be > 0")
	}
	if requireMessagesMaxTokens && cfg.Protocol == ProtocolMessages && cfg.MaxTokens == nil {
		return fmt.Errorf("opencodego: MaxTokens is required for Messages")
	}
	return nil
}

// snapshotConfig copies pointer-valued configuration owned by the caller so a
// provider can retain an immutable cap for concurrent operations.
func snapshotConfig(cfg ChatModelConfig) ChatModelConfig {
	if cfg.MaxTokens != nil {
		maxTokens := *cfg.MaxTokens
		cfg.MaxTokens = &maxTokens
	}
	return cfg
}

// newAuthClient constructs the immutable authentication owner used by every
// protocol adapter. opencode-auth-go validates credentials, base URLs,
// sessions, user-agent syntax, and the caller-supplied HTTP client without
// performing network or filesystem I/O.
func newAuthClient(cfg ChatModelConfig) (*opencodeauth.Client, error) {
	client, err := opencodeauth.NewClient(opencodeauth.Options{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		UserAgent:  cfg.UserAgent,
		SessionID:  cfg.SessionID,
		HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return nil, mapConstructorError(err)
	}
	return client, nil
}

type safeError struct {
	message string
	causes  []error
}

func (e *safeError) Error() string {
	if e == nil || e.message == "" {
		return "opencode-go: request failed"
	}
	return e.message
}

func (e *safeError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return e.causes
}

func safeFailure(operation string, causes ...error) error {
	filtered := make([]error, 0, len(causes))
	for _, cause := range causes {
		if cause != nil {
			filtered = append(filtered, cause)
		}
	}
	return &safeError{
		message: "opencode-go: " + safeOperationLabel(operation) + " failed",
		causes:  filtered,
	}
}

func safeOperationLabel(operation string) string {
	switch operation {
	case "build client", "build chat model", "advise", "generate", "stream", "receive", "request":
		return operation
	default:
		return "request"
	}
}

// mapConstructorError classifies construction failures without allowing an
// upstream error string to become public. Missing API credentials remain both
// initialization- and authentication-classified.
func mapConstructorError(err error) error {
	if err == nil {
		return nil
	}
	safe := safeFailure("build client", err)
	if errors.Is(err, opencodeauth.ErrMissingAPIKey) {
		return einoproviders.WrapInitError(einoproviders.WrapAuthError(safe))
	}
	return einoproviders.WrapInitError(safe)
}
