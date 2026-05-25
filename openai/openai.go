package openai

import (
	"context"
	"fmt"
	"net/http"

	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// Provider implements einoproviders.Provider using OpenAI via Eino.
type Provider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func init() {
	einoproviders.RegisterProvider("openai", func(_ context.Context, model string, opts einoproviders.Options) (einoproviders.Provider, error) {
		baseURL := ""
		if opts.BaseURL != nil {
			baseURL = *opts.BaseURL
		}
		return newOpenAIProvider(opts.APIKey, model, baseURL, opts.HTTPClient), nil
	})
}

// NewOpenAIProvider constructs an OpenAI provider.
func NewOpenAIProvider(apiKey, model string) *Provider {
	return &Provider{apiKey: apiKey, model: model}
}

// NewOpenAIProviderWithBaseURL constructs an OpenAI provider with a custom API endpoint.
func NewOpenAIProviderWithBaseURL(apiKey, model, baseURL string) *Provider {
	return newOpenAIProvider(apiKey, model, baseURL, nil)
}

func newOpenAIProvider(apiKey, model, baseURL string, client *http.Client) *Provider {
	return &Provider{apiKey: apiKey, model: model, baseURL: baseURL, client: client}
}

// Advise implements einoproviders.Provider using the OpenAI model.
func (p *Provider) Advise(ctx context.Context, system, user string, maxTokens int) (string, einoproviders.Usage, error) {
	mt := maxTokens
	cm, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		APIKey:              p.apiKey,
		Model:               p.model,
		MaxCompletionTokens: &mt,
		BaseURL:             p.baseURL,
		HTTPClient:          p.client,
	})
	if err != nil {
		return "", einoproviders.Usage{}, einoproviders.WrapInitError(fmt.Errorf("openai init: %w", err))
	}

	msg, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(user),
	})
	if err != nil {
		return "", einoproviders.Usage{}, fmt.Errorf("openai generate: %w", err)
	}

	return msg.Content, einoproviders.ExtractUsage(msg), nil
}
