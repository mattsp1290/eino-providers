package opencodego

import (
	"errors"
	"net/http"
	"testing"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestProtocolValues(t *testing.T) {
	tests := map[string]struct {
		got  Protocol
		want Protocol
	}{
		"chat completions": {got: ProtocolChatCompletions, want: "chat-completions"},
		"messages":         {got: ProtocolMessages, want: "messages"},
		"responses":        {got: ProtocolResponses, want: "responses"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("protocol = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	maxTokens := 128
	base := ChatModelConfig{
		Model:     "model-id",
		Protocol:  ProtocolChatCompletions,
		UserAgent: "host-agent/1.0",
	}

	tests := []struct {
		name                     string
		cfg                      ChatModelConfig
		requireMessagesMaxTokens bool
		wantErr                  string
	}{
		{name: "valid without cap", cfg: base},
		{name: "valid with cap", cfg: func() ChatModelConfig {
			cfg := base
			cfg.MaxTokens = &maxTokens
			return cfg
		}()},
		{name: "model is required", cfg: func() ChatModelConfig {
			cfg := base
			cfg.Model = " \t"
			return cfg
		}(), wantErr: "Model is required"},
		{name: "protocol is required", cfg: func() ChatModelConfig {
			cfg := base
			cfg.Protocol = ""
			return cfg
		}(), wantErr: "Protocol is required"},
		{name: "protocol must be known", cfg: func() ChatModelConfig {
			cfg := base
			cfg.Protocol = "unknown"
			return cfg
		}(), wantErr: `unknown Protocol "unknown"`},
		{name: "user agent is required", cfg: func() ChatModelConfig {
			cfg := base
			cfg.UserAgent = "  "
			return cfg
		}(), wantErr: "UserAgent is required"},
		{name: "cap must be positive", cfg: func() ChatModelConfig {
			cfg := base
			zero := 0
			cfg.MaxTokens = &zero
			return cfg
		}(), wantErr: "MaxTokens must be > 0"},
		{name: "messages direct model requires cap", cfg: func() ChatModelConfig {
			cfg := base
			cfg.Protocol = ProtocolMessages
			return cfg
		}(), requireMessagesMaxTokens: true, wantErr: "MaxTokens is required for Messages"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg, tt.requireMessagesMaxTokens)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateConfig() error = %v", err)
				}
				return
			}
			if err == nil || err.Error() != "opencodego: "+tt.wantErr {
				t.Fatalf("validateConfig() error = %v, want %q", err, "opencodego: "+tt.wantErr)
			}
		})
	}
}

func TestValidateConfigMessagesCapDistinction(t *testing.T) {
	cfg := ChatModelConfig{
		Model:     "model-id",
		Protocol:  ProtocolMessages,
		UserAgent: "host-agent/1.0",
	}
	if err := validateConfig(cfg, false); err != nil {
		t.Fatalf("Advise-style validation error = %v", err)
	}
	if err := validateConfig(cfg, true); err == nil {
		t.Fatal("direct Messages validation unexpectedly accepted a nil cap")
	}
}

func TestSnapshotConfigCopiesMaxTokens(t *testing.T) {
	maxTokens := 128
	cfg := snapshotConfig(ChatModelConfig{MaxTokens: &maxTokens})
	maxTokens = 256
	if cfg.MaxTokens == nil {
		t.Fatal("snapshot lost MaxTokens")
	}
	if got, want := *cfg.MaxTokens, 128; got != want {
		t.Fatalf("snapshot MaxTokens = %d, want %d", got, want)
	}
	if cfg.MaxTokens == &maxTokens {
		t.Fatal("snapshot retained caller's MaxTokens pointer")
	}
}

func TestNewAuthClientValidatesWithoutNetwork(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "")

	client, err := newAuthClient(ChatModelConfig{UserAgent: "host-agent/1.0"})
	if client != nil {
		t.Fatal("newAuthClient returned a client without credentials")
	}
	if !errors.Is(err, einoproviders.ErrProviderInit) ||
		!errors.Is(err, einoproviders.ErrProviderAuth) ||
		!errors.Is(err, opencodeauth.ErrMissingAPIKey) {
		t.Fatalf("error = %v, want init, auth, and missing-key identities", err)
	}
	if got := einoproviders.Classify(err); got != einoproviders.ErrorClassProviderInit {
		t.Fatalf("Classify(error) = %v, want provider init", got)
	}

	baseTransport := &countingRoundTripper{}
	source := &http.Client{Transport: baseTransport}
	client, err = newAuthClient(ChatModelConfig{
		APIKey:     "explicit-key",
		UserAgent:  "host-agent/1.0",
		SessionID:  "conversation-1",
		HTTPClient: source,
	})
	if err != nil {
		t.Fatalf("newAuthClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("newAuthClient returned nil")
	}
	if baseTransport.calls != 0 {
		t.Fatalf("construction made %d network calls", baseTransport.calls)
	}
	if source.Transport != baseTransport {
		t.Fatal("construction modified the caller's HTTP client")
	}
}

func TestNewAuthClientDelegatesConfigurationValidation(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "environment-key")

	tests := []struct {
		name string
		cfg  ChatModelConfig
		want error
	}{
		{
			name: "invalid user agent syntax",
			cfg:  ChatModelConfig{UserAgent: "host\nagent"},
			want: opencodeauth.ErrInvalidConfiguration,
		},
		{
			name: "invalid session",
			cfg:  ChatModelConfig{UserAgent: "host-agent/1.0", SessionID: "bad session"},
			want: opencodeauth.ErrInvalidSessionID,
		},
		{
			name: "invalid base URL",
			cfg:  ChatModelConfig{UserAgent: "host-agent/1.0", BaseURL: "http://example.com"},
			want: opencodeauth.ErrInvalidConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := newAuthClient(tt.cfg)
			if client != nil || !errors.Is(err, tt.want) || !errors.Is(err, einoproviders.ErrProviderInit) {
				t.Fatalf("newAuthClient() = (%v, %v), want nil init error matching %v", client, err, tt.want)
			}
		})
	}
}

type countingRoundTripper struct {
	calls int
}

func (t *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("unexpected network request")
}
