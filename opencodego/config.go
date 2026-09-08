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

type constructionKind uint8

const (
	providerConstruction constructionKind = iota + 1
	chatModelConstruction
)

// validateConfig checks the fields owned by this package. Authentication,
// session, and endpoint syntax remain the responsibility of opencode-auth-go.
func validateConfig(cfg ChatModelConfig, construction constructionKind) error {
	if construction != providerConstruction && construction != chatModelConstruction {
		return fmt.Errorf("opencodego: invalid construction kind")
	}
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

	if cfg.MaxTokens != nil && *cfg.MaxTokens <= 0 {
		return fmt.Errorf("opencodego: MaxTokens must be > 0")
	}
	if construction == chatModelConstruction && cfg.Protocol == ProtocolMessages && cfg.MaxTokens == nil {
		return fmt.Errorf("opencodego: MaxTokens is required for Messages")
	}
	return nil
}

func snapshotMaxTokens(value *int) *int {
	if value == nil {
		return nil
	}
	snapshot := *value
	return &snapshot
}

type preparedConfig struct {
	model      string
	protocol   Protocol
	maxTokens  *int
	authClient *opencodeauth.Client
}

// prepareConfig validates and snapshots local configuration before constructing
// the immutable authentication owner used by every protocol adapter.
// opencode-auth-go validates credentials, base URLs, sessions, user-agent
// syntax, and the caller-supplied HTTP client without network or filesystem I/O.
func prepareConfig(cfg ChatModelConfig, construction constructionKind) (preparedConfig, error) {
	if err := validateConfig(cfg, construction); err != nil {
		return preparedConfig{}, mapConstructorError(err)
	}
	client, err := opencodeauth.NewClient(opencodeauth.Options{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		UserAgent:  cfg.UserAgent,
		SessionID:  cfg.SessionID,
		HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return preparedConfig{}, mapConstructorError(err)
	}
	return preparedConfig{
		model:      cfg.Model,
		protocol:   cfg.Protocol,
		maxTokens:  snapshotMaxTokens(cfg.MaxTokens),
		authClient: client,
	}, nil
}

type constructorError struct {
	cause error
}

func (*constructorError) Error() string {
	return "opencode-go: build client failed"
}

func (e *constructorError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// mapConstructorError classifies construction failures without allowing an
// upstream error string to become public. Missing API credentials remain both
// initialization- and authentication-classified.
func mapConstructorError(err error) error {
	if err == nil {
		return nil
	}
	safe := &constructorError{cause: err}
	if errors.Is(err, opencodeauth.ErrMissingAPIKey) {
		return einoproviders.WrapInitError(einoproviders.WrapAuthError(safe))
	}
	return einoproviders.WrapInitError(safe)
}
