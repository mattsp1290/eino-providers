package openai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	agenticopenai "github.com/cloudwego/eino-ext/components/model/agenticopenai"
	"github.com/cloudwego/eino/components/model"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// AgenticModelConfig configures a native OpenAI Responses agentic model.
// MaxRetries is deliberately restricted to zero so one runtime attempt maps
// to one observed HTTP request.
type AgenticModelConfig struct {
	APIKey      string
	Model       string
	BaseURL     string
	HTTPClient  *http.Client
	Timeout     time.Duration
	MaxRetries  int
	MaxTokens   *int
	Temperature *float32
	TopP        *float32
	Limits      einoproviders.AgenticLimits
}

// NewAgenticModel constructs a native OpenAI Responses AgenticModel. It does
// not route through the classic ChatModel adapter.
func NewAgenticModel(ctx context.Context, cfg AgenticModelConfig) (model.AgenticModel, error) {
	if cfg.Model == "" {
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai: Model is required"))
	}
	if cfg.APIKey == "" && cfg.BaseURL == "" {
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai: APIKey is required when BaseURL is empty"))
	}
	if cfg.MaxRetries != 0 {
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai: agentic MaxRetries must be zero"))
	}
	limits, err := cfg.Limits.Validate()
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai: invalid agentic limits: %w", err))
	}
	clientSource := cfg.HTTPClient
	if clientSource == nil && cfg.Timeout != 0 {
		clientSource = &http.Client{Timeout: cfg.Timeout}
	}
	httpClient, err := einoproviders.NewAgenticLimitedHTTPClient(clientSource, limits)
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai: build agentic limited client: %w", err))
	}
	zeroRetries := 0
	m, err := agenticopenai.NewResponsesModel(ctx, &agenticopenai.ResponsesConfig{
		APIKey: cfg.APIKey, Model: cfg.Model, BaseURL: cfg.BaseURL,
		HTTPClient: httpClient, MaxRetries: &zeroRetries,
		MaxTokens: cfg.MaxTokens, Temperature: cfg.Temperature, TopP: cfg.TopP,
	})
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai: build agentic responses model %q: %w", cfg.Model, err))
	}
	return einoproviders.NewBoundedAgenticModel(m, limits)
}
