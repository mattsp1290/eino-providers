package claude

import (
	"context"
	"fmt"
	"net/http"

	claudemodel "github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// Provider implements einoproviders.Provider using Anthropic Claude via Eino.
type Provider struct {
	apiKey  string
	model   string
	baseURL *string
	client  *http.Client
}

func init() {
	einoproviders.RegisterProvider("claude", func(_ context.Context, model string, opts einoproviders.Options) (einoproviders.Provider, error) {
		return newClaudeProvider(opts.APIKey, model, opts.BaseURL, opts.HTTPClient), nil
	})
}

// NewClaudeProvider constructs a Claude provider.
func NewClaudeProvider(apiKey, model string) *Provider {
	return &Provider{apiKey: apiKey, model: model}
}

// NewClaudeProviderWithBaseURL constructs a Claude provider with a custom API endpoint.
func NewClaudeProviderWithBaseURL(apiKey, model string, baseURL *string) *Provider {
	return newClaudeProvider(apiKey, model, baseURL, nil)
}

func newClaudeProvider(apiKey, model string, baseURL *string, client *http.Client) *Provider {
	return &Provider{apiKey: apiKey, model: model, baseURL: baseURL, client: client}
}

// Advise implements einoproviders.Provider using the Claude model.
func (p *Provider) Advise(ctx context.Context, system, user string, maxTokens int) (string, einoproviders.Usage, error) {
	cm, err := claudemodel.NewChatModel(ctx, &claudemodel.Config{
		APIKey:     p.apiKey,
		Model:      p.model,
		MaxTokens:  maxTokens,
		BaseURL:    p.baseURL,
		HTTPClient: p.client,
	})
	if err != nil {
		return "", einoproviders.Usage{}, einoproviders.WrapInitError(fmt.Errorf("claude init: %w", err))
	}

	msg, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(user),
	})
	if err != nil {
		return "", einoproviders.Usage{}, fmt.Errorf("claude generate: %w", err)
	}

	return msg.Content, einoproviders.ExtractUsage(msg), nil
}
