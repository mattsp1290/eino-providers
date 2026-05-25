package gemini

import (
	"context"
	"fmt"

	geminimodel "github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/genai"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// Provider implements einoproviders.Provider using Gemini via Eino.
//
// The genai.Client is cached at construction time to avoid rebuilding the SDK
// HTTP client and sub-clients on every request.
type Provider struct {
	client *genai.Client
	model  string
}

func init() {
	einoproviders.RegisterProvider("gemini", func(ctx context.Context, model string, opts einoproviders.Options) (einoproviders.Provider, error) {
		return newGeminiProvider(ctx, opts.APIKey, model, opts.GenaiClient)
	})
}

// NewGeminiProvider constructs a Gemini provider with a cached genai.Client.
func NewGeminiProvider(ctx context.Context, apiKey, model string) (*Provider, error) {
	return newGeminiProvider(ctx, apiKey, model, nil)
}

// NewGeminiProviderWithClient constructs a Gemini provider with an existing genai.Client.
func NewGeminiProviderWithClient(client *genai.Client, model string) (*Provider, error) {
	return newGeminiProvider(context.Background(), "", model, client)
}

func newGeminiProvider(ctx context.Context, apiKey, model string, client *genai.Client) (*Provider, error) {
	if client == nil {
		var err error
		client, err = genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
		if err != nil {
			return nil, fmt.Errorf("gemini client init: %w", err)
		}
	}
	return &Provider{client: client, model: model}, nil
}

// Advise implements einoproviders.Provider using the Gemini model.
func (p *Provider) Advise(ctx context.Context, system, user string, maxTokens int) (string, einoproviders.Usage, error) {
	mt := maxTokens
	cm, err := geminimodel.NewChatModel(ctx, &geminimodel.Config{
		Client:    p.client,
		Model:     p.model,
		MaxTokens: &mt,
	})
	if err != nil {
		return "", einoproviders.Usage{}, einoproviders.WrapInitError(fmt.Errorf("gemini init: %w", err))
	}

	msg, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(user),
	})
	if err != nil {
		return "", einoproviders.Usage{}, fmt.Errorf("gemini generate: %w", err)
	}

	return msg.Content, einoproviders.ExtractUsage(msg), nil
}
