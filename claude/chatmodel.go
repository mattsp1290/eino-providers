package claude

import (
	"context"
	"fmt"
	"net/http"

	claudemodel "github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino/components/model"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// ChatModelConfig configures a Claude ToolCallingChatModel.
type ChatModelConfig struct {
	// APIKey is the Anthropic API key.
	// Required unless HTTPClient is set with a pre-authenticated transport.
	APIKey string

	// Model is the Claude model slug, e.g. "claude-sonnet-4-5".
	// Required.
	Model string

	// MaxTokens is the maximum number of tokens the model may generate.
	// Required (Claude rejects requests with MaxTokens = 0).
	MaxTokens int

	// BaseURL overrides the Anthropic API endpoint. Useful for proxies.
	// Optional.
	BaseURL *string

	// HTTPClient, when non-nil, is used instead of the default transport.
	// Optional.
	HTTPClient *http.Client

	// Temperature controls output randomness [0.0, 1.0].
	// Optional.
	Temperature *float32

	// TopP controls nucleus sampling [0.0, 1.0].
	// Optional.
	TopP *float32

	// TopK limits token selection.
	// Optional.
	TopK *int32

	// StopSequences are custom stop sequences.
	// Optional.
	StopSequences []string
}

// NewChatModel constructs a Claude Eino ToolCallingChatModel.
func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error) {
	if cfg.Model == "" {
		return nil, einoproviders.WrapInitError(fmt.Errorf("claude: Model is required"))
	}
	if cfg.MaxTokens <= 0 {
		return nil, einoproviders.WrapInitError(fmt.Errorf("claude: MaxTokens must be > 0"))
	}
	cm, err := claudemodel.NewChatModel(ctx, &claudemodel.Config{
		APIKey:        cfg.APIKey,
		Model:         cfg.Model,
		MaxTokens:     cfg.MaxTokens,
		BaseURL:       cfg.BaseURL,
		HTTPClient:    cfg.HTTPClient,
		Temperature:   cfg.Temperature,
		TopP:          cfg.TopP,
		TopK:          cfg.TopK,
		StopSequences: cfg.StopSequences,
	})
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("claude: build chat model %q: %w", cfg.Model, err))
	}
	return cm, nil
}
