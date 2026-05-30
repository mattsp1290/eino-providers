package gemini

import (
	"context"
	"fmt"

	geminimodel "github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino/components/model"
	"google.golang.org/genai"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// ChatModelConfig configures a Gemini ToolCallingChatModel.
type ChatModelConfig struct {
	// APIKey is the Gemini API key used to build a genai.Client when Client is
	// nil. When Client is nil and APIKey is empty, genai.NewClient falls back to
	// environment-variable credentials (GEMINI_API_KEY, GOOGLE_API_KEY, or ADC).
	// Required when Client is nil and no env-var credentials are configured.
	APIKey string

	// Client is an existing genai.Client. When non-nil, APIKey is ignored.
	// Optional.
	Client *genai.Client

	// Model is the Gemini model slug, e.g. "gemini-2.0-flash".
	// Required.
	Model string

	// MaxTokens limits generation length.
	// Optional.
	MaxTokens *int

	// Temperature controls output randomness [0.0, 1.0].
	// Optional.
	Temperature *float32

	// TopP controls nucleus sampling [0.0, 1.0].
	// Optional.
	TopP *float32

	// TopK limits token selection.
	// Optional.
	TopK *int32
}

// NewChatModel constructs a Gemini Eino ToolCallingChatModel.
func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error) {
	if cfg.Model == "" {
		return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: Model is required"))
	}
	client := cfg.Client
	if client == nil {
		// APIKey may be empty when env-var credentials are configured
		// (GEMINI_API_KEY, GOOGLE_API_KEY, ADC). genai.NewClient resolves those
		// itself; we don't eagerly reject here.
		var err error
		client, err = genai.NewClient(ctx, &genai.ClientConfig{APIKey: cfg.APIKey})
		if err != nil {
			return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: build client: %w", err))
		}
	}
	cm, err := geminimodel.NewChatModel(ctx, &geminimodel.Config{
		Client:      client,
		Model:       cfg.Model,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
		TopK:        cfg.TopK,
	})
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: build chat model %q: %w", cfg.Model, err))
	}
	return cm, nil
}
