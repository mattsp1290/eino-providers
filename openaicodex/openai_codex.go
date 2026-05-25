package openaicodex

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
	codexauth "github.com/mattsp1290/codex-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

const dummyAPIKey = "codex-oauth-dummy" //nolint:gosec // Non-secret placeholder required by Eino; Codex transport replaces auth.

const (
	codexErrCodeUsageNotIncluded  = "usage_not_included"
	codexErrCodeInsufficientQuota = "insufficient_quota"
)

// codexHTTPClient is the seam for obtaining a Codex-authenticated client.
var codexHTTPClient = func(ctx context.Context) (*http.Client, error) {
	return codexauth.NewClient(codexauth.Options{AppName: "advisor"}).HTTPClient(ctx)
}

// Provider implements einoproviders.Provider using OpenAI-Codex via Eino.
//
// The Codex OAuth transport owns endpoint rewriting, bearer-token injection,
// and refresh. This provider must not set BaseURL or MaxCompletionTokens.
type Provider struct {
	httpClient *http.Client
	model      string
}

func init() {
	einoproviders.RegisterProvider("openai-codex", func(ctx context.Context, model string, opts einoproviders.Options) (einoproviders.Provider, error) {
		if opts.BaseURL != nil {
			return nil, einoproviders.WrapInitError(errors.New("openai-codex: BaseURL must be nil"))
		}
		if opts.MaxTokens != nil {
			return nil, einoproviders.WrapInitError(errors.New("openai-codex: MaxTokens must be nil"))
		}
		if opts.HTTPClient != nil {
			return NewOpenAICodexProviderWithHTTPClient(opts.HTTPClient, model), nil
		}
		return NewOpenAICodexProvider(ctx, model)
	})
}

// NewOpenAICodexProvider constructs a provider backed by Codex OAuth.
func NewOpenAICodexProvider(ctx context.Context, model string) (*Provider, error) {
	client, err := codexHTTPClient(ctx)
	if err != nil {
		if errors.Is(err, codexauth.ErrNotLoggedIn) {
			return nil, einoproviders.WrapInitError(einoproviders.WrapAuthError(codexauth.ErrNotLoggedIn))
		}
		return nil, einoproviders.WrapInitError(fmt.Errorf("codex init: %w", err))
	}
	return NewOpenAICodexProviderWithHTTPClient(client, model), nil
}

// NewOpenAICodexProviderWithHTTPClient constructs a provider from an existing client.
func NewOpenAICodexProviderWithHTTPClient(client *http.Client, model string) *Provider {
	return &Provider{httpClient: client, model: model}
}

// Advise implements einoproviders.Provider using the Codex-authenticated OpenAI model.
func (p *Provider) Advise(ctx context.Context, system, user string, _ int) (string, einoproviders.Usage, error) {
	cm, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		APIKey:     dummyAPIKey,
		Model:      p.model,
		HTTPClient: p.httpClient,
	})
	if err != nil {
		return "", einoproviders.Usage{}, einoproviders.WrapInitError(fmt.Errorf("codex model init: %w", err))
	}

	msg, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(user),
	})
	if err != nil {
		return "", einoproviders.Usage{}, classifyCodexError(err)
	}

	return msg.Content, einoproviders.ExtractUsage(msg), nil
}

func classifyCodexError(err error) error {
	var apiErr *openaimodel.APIError
	if errors.As(err, &apiErr) {
		switch fmt.Sprint(apiErr.Code) {
		case codexErrCodeUsageNotIncluded:
			return einoproviders.WrapAuthError(fmt.Errorf("%w: %w", codexauth.ErrPlanNotIncluded, err))
		case codexErrCodeInsufficientQuota:
			return einoproviders.WrapAuthError(fmt.Errorf("%w: %w", codexauth.ErrQuotaExceeded, err))
		}
	}
	return fmt.Errorf("codex generate: %w", err)
}
