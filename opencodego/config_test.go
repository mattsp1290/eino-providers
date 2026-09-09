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
		name    string
		cfg     ChatModelConfig
		wantErr string
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
		{name: "cap must be positive", cfg: func() ChatModelConfig {
			cfg := base
			zero := 0
			cfg.MaxTokens = &zero
			return cfg
		}(), wantErr: "MaxTokens must be > 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg, providerConstruction)
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
	if err := validateConfig(cfg, providerConstruction); err != nil {
		t.Fatalf("Advise-style validation error = %v", err)
	}
	if err := validateConfig(cfg, chatModelConstruction); err == nil {
		t.Fatal("direct Messages validation unexpectedly accepted a nil cap")
	}
}

func TestSnapshotMaxTokensCopiesValue(t *testing.T) {
	maxTokens := 128
	snapshot := snapshotMaxTokens(&maxTokens)
	maxTokens = 256
	if snapshot == nil {
		t.Fatal("snapshot lost MaxTokens")
	}
	if got, want := *snapshot, 128; got != want {
		t.Fatalf("snapshot MaxTokens = %d, want %d", got, want)
	}
	if snapshot == &maxTokens {
		t.Fatal("snapshot retained caller's MaxTokens pointer")
	}
}

func TestPrepareConfigValidatesWithoutNetwork(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "")
	baseConfig := ChatModelConfig{
		Model:     "model-id",
		Protocol:  ProtocolChatCompletions,
		UserAgent: "host-agent/1.0",
	}

	prepared, err := prepareConfig(baseConfig, providerConstruction)
	if prepared.authClient != nil {
		t.Fatal("prepareConfig returned a client without credentials")
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
	baseConfig.APIKey = "explicit-key"
	baseConfig.SessionID = "conversation-1"
	baseConfig.HTTPClient = source
	prepared, err = prepareConfig(baseConfig, providerConstruction)
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}
	if prepared.authClient == nil {
		t.Fatal("prepareConfig returned nil auth client")
	}
	if baseTransport.calls != 0 {
		t.Fatalf("construction made %d network calls", baseTransport.calls)
	}
	if source.Transport != baseTransport {
		t.Fatal("construction modified the caller's HTTP client")
	}
}

func TestPrepareConfigRejectsLocalConfiguration(t *testing.T) {
	base := ChatModelConfig{
		Model:     "model-id",
		Protocol:  ProtocolChatCompletions,
		APIKey:    "explicit-key",
		UserAgent: "host-agent/1.0",
	}
	tests := []struct {
		name         string
		cfg          ChatModelConfig
		construction constructionKind
	}{
		{name: "missing model", cfg: func() ChatModelConfig {
			cfg := base
			cfg.Model = " "
			return cfg
		}(), construction: providerConstruction},
		{name: "unknown protocol", cfg: func() ChatModelConfig {
			cfg := base
			cfg.Protocol = "unknown"
			return cfg
		}(), construction: providerConstruction},
		{name: "nonpositive cap", cfg: func() ChatModelConfig {
			cfg := base
			zero := 0
			cfg.MaxTokens = &zero
			return cfg
		}(), construction: providerConstruction},
		{name: "messages cap required", cfg: func() ChatModelConfig {
			cfg := base
			cfg.Protocol = ProtocolMessages
			return cfg
		}(), construction: chatModelConstruction},
		{name: "unknown construction kind", cfg: base, construction: constructionKind(99)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepared, err := prepareConfig(tt.cfg, tt.construction)
			if prepared != (preparedConfig{}) {
				t.Fatalf("prepareConfig() returned usable state for invalid config: %#v", prepared)
			}
			if !errors.Is(err, einoproviders.ErrProviderInit) {
				t.Fatalf("prepareConfig() error = %v, want provider init identity", err)
			}
		})
	}
}

func TestPrepareConfigUsesEnvironmentAndExplicitAPIKeys(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "environment-key")

	tests := []struct {
		name    string
		apiKey  string
		wantKey string
	}{
		{name: "environment fallback", wantKey: "environment-key"},
		{name: "explicit precedence", apiKey: "explicit-key", wantKey: "explicit-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &countingRoundTripper{}
			prepared, err := prepareConfig(ChatModelConfig{
				Model:      "model-id",
				Protocol:   ProtocolChatCompletions,
				APIKey:     tt.apiKey,
				UserAgent:  "host-agent/1.0",
				SessionID:  "conversation-1",
				BaseURL:    "https://api.example.test/v1",
				HTTPClient: &http.Client{Transport: recorder},
			}, providerConstruction)
			if err != nil {
				t.Fatalf("prepareConfig() error = %v", err)
			}
			client := prepared.authClient.HTTPClient()
			endpoint, err := prepared.authClient.Endpoint(opencodeauth.ProtocolChatCompletions)
			if err != nil {
				t.Fatalf("Endpoint() error = %v", err)
			}
			req, err := http.NewRequest(http.MethodPost, endpoint, http.NoBody)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatalf("response Close() error = %v", err)
			}
			if got := recorder.authorization; got != "Bearer "+tt.wantKey {
				t.Fatalf("Authorization = %q, want Bearer credential", got)
			}
		})
	}
}

func TestPrepareConfigDelegatesAuthValidation(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "environment-key")
	base := ChatModelConfig{
		APIKey:    "explicit-key",
		Model:     "model-id",
		Protocol:  ProtocolChatCompletions,
		UserAgent: "host-agent/1.0",
	}

	tests := []struct {
		name string
		cfg  ChatModelConfig
		want error
	}{
		{
			name: "missing user agent",
			cfg: func() ChatModelConfig {
				cfg := base
				cfg.UserAgent = ""
				return cfg
			}(),
			want: opencodeauth.ErrInvalidConfiguration,
		},
		{
			name: "whitespace user agent",
			cfg: func() ChatModelConfig {
				cfg := base
				cfg.UserAgent = "  "
				return cfg
			}(),
			want: opencodeauth.ErrInvalidConfiguration,
		},
		{
			name: "invalid user agent syntax",
			cfg: func() ChatModelConfig {
				cfg := base
				cfg.UserAgent = "host\nagent"
				return cfg
			}(),
			want: opencodeauth.ErrInvalidConfiguration,
		},
		{
			name: "invalid session",
			cfg: func() ChatModelConfig {
				cfg := base
				cfg.SessionID = "bad session"
				return cfg
			}(),
			want: opencodeauth.ErrInvalidSessionID,
		},
		{
			name: "invalid base URL",
			cfg: func() ChatModelConfig {
				cfg := base
				cfg.BaseURL = "http://example.com"
				return cfg
			}(),
			want: opencodeauth.ErrInvalidConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepared, err := prepareConfig(tt.cfg, providerConstruction)
			if prepared != (preparedConfig{}) || !errors.Is(err, tt.want) || !errors.Is(err, einoproviders.ErrProviderInit) {
				t.Fatalf("prepareConfig() result/error = (%#v, %v), want zero init error matching %v", prepared, err, tt.want)
			}
		})
	}
}

type countingRoundTripper struct {
	calls         int
	authorization string
}

func (t *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	t.authorization = req.Header.Get("Authorization")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       http.NoBody,
		Request:    req,
	}, nil
}
