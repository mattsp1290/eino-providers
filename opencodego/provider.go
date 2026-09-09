package opencodego

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// Provider is the registered single-shot OpenCode Go backend.
type Provider struct {
	config preparedConfig
}

func init() {
	einoproviders.RegisterProvider("opencode-go", func(_ context.Context, model string, opts einoproviders.Options) (einoproviders.Provider, error) {
		baseURL := ""
		if opts.BaseURL != nil {
			baseURL = *opts.BaseURL
		}
		prepared, err := prepareConfig(ChatModelConfig{
			Model:      model,
			Protocol:   Protocol(opts.Protocol),
			APIKey:     opts.APIKey,
			UserAgent:  opts.UserAgent,
			SessionID:  opts.SessionID,
			BaseURL:    baseURL,
			HTTPClient: opts.HTTPClient,
			MaxTokens:  opts.MaxTokens,
		}, providerConstruction)
		if err != nil {
			return nil, err
		}
		if !protocolImplemented(prepared.protocol) {
			_, unsupported := newProtocolAdapter(context.Background(), prepared)
			return nil, einoproviders.WrapInitError(unsupported)
		}
		return &Provider{config: prepared}, nil
	})
}

// Advise sends exactly one system and one user message. maxTokens overrides
// any configured cap for this invocation.
func (p *Provider) Advise(ctx context.Context, system, user string, maxTokens int) (string, einoproviders.Usage, error) {
	if maxTokens <= 0 {
		return "", einoproviders.Usage{}, einoproviders.WrapInitError(errors.New("opencode-go: maxTokens must be positive"))
	}
	cfg := p.config
	cfg.maxTokens = snapshotMaxTokens(&maxTokens)
	cm, err := newChatModelFromPrepared(ctx, cfg)
	if err != nil {
		return "", einoproviders.Usage{}, err
	}
	message, err := cm.generate(ctx, []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(user),
	}, operationAdvise)
	if err != nil {
		return "", einoproviders.Usage{}, err
	}
	if message == nil {
		return "", einoproviders.Usage{}, mapInvocationError(operationAdvise, errNilSuccessfulMessage, nil)
	}
	return message.Content, einoproviders.ExtractUsage(message), nil
}
