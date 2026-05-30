package openai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// ChatModelConfig configures an OpenAI ToolCallingChatModel.
type ChatModelConfig struct {
	// APIKey is the OpenAI API key.
	// Required unless BaseURL is set (local/compatible servers accept any or no key).
	APIKey string

	// Model is the model ID, e.g. "gpt-4o" or "o3".
	// Required.
	Model string

	// BaseURL overrides the OpenAI API endpoint. Use for OpenAI-compatible
	// servers (e.g. local inference endpoints).
	// Optional.
	BaseURL string

	// HTTPClient, when non-nil, is used instead of the default transport.
	// Optional.
	HTTPClient *http.Client

	// Timeout specifies the maximum duration to wait for API responses.
	// Ignored when HTTPClient is non-nil (the client's transport owns timeout).
	// Optional.
	Timeout time.Duration

	// MaxCompletionTokens caps generation including reasoning tokens.
	// Optional.
	MaxCompletionTokens *int

	// MaxTokens is the legacy max_tokens field understood by most
	// OpenAI-compatible local inference servers (vLLM, llama.cpp, LM Studio).
	// Use MaxCompletionTokens for OpenAI API (o1/o3 series).
	// Optional. Deprecated for OpenAI API proper.
	MaxTokens *int

	// Temperature controls output randomness [0.0, 2.0].
	// Optional.
	Temperature *float32

	// TopP controls nucleus sampling [0.0, 1.0].
	// Optional.
	TopP *float32

	// Stop sequences where generation halts.
	// Optional.
	Stop []string
}

// NewChatModel constructs an OpenAI Eino ToolCallingChatModel.
func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error) {
	if cfg.Model == "" {
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai: Model is required"))
	}
	// Require APIKey only for the hosted OpenAI API. When BaseURL is set
	// (local/compatible server), the key may be empty or a placeholder.
	if cfg.APIKey == "" && cfg.BaseURL == "" {
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai: APIKey is required when BaseURL is empty"))
	}
	cm, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		APIKey:              cfg.APIKey,
		Model:               cfg.Model,
		BaseURL:             cfg.BaseURL,
		HTTPClient:          cfg.HTTPClient,
		Timeout:             cfg.Timeout,
		MaxCompletionTokens: cfg.MaxCompletionTokens,
		MaxTokens:           cfg.MaxTokens,
		Temperature:         cfg.Temperature,
		TopP:                cfg.TopP,
		Stop:                cfg.Stop,
	})
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai: build chat model %q: %w", cfg.Model, err))
	}
	return cm, nil
}
