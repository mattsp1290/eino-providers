package gemini

import (
	"context"
	"fmt"
	"net/http"

	agenticgemini "github.com/cloudwego/eino-ext/components/model/agenticgemini"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/genai"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// AgenticModelConfig configures a native Gemini generateContent agentic model.
type AgenticModelConfig struct {
	APIKey      string
	Client      *genai.Client
	Model       string
	MaxTokens   *int
	Temperature *float32
	TopP        *float32
	TopK        *int32
	HTTPClient  *http.Client
	Limits      einoproviders.AgenticLimits
}

// NewAgenticModel constructs the native Gemini agentic adapter with a cached
// caller-owned client when one is provided.
func NewAgenticModel(ctx context.Context, cfg AgenticModelConfig) (model.AgenticModel, error) {
	if cfg.Model == "" {
		return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: Model is required"))
	}
	limits, err := cfg.Limits.Validate()
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: invalid agentic limits: %w", err))
	}
	client := cfg.Client
	if client == nil {
		httpClient, err := einoproviders.NewAgenticLimitedHTTPClient(cfg.HTTPClient, limits)
		if err != nil {
			return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: build agentic limited client: %w", err))
		}
		client, err = genai.NewClient(ctx, &genai.ClientConfig{APIKey: cfg.APIKey, HTTPClient: httpClient})
		if err != nil {
			return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: build agentic client: %w", err))
		}
	}
	m, err := agenticgemini.New(ctx, &agenticgemini.Config{Client: client, Model: cfg.Model, MaxTokens: cfg.MaxTokens, Temperature: cfg.Temperature, TopP: cfg.TopP, TopK: cfg.TopK})
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: build agentic model %q: %w", cfg.Model, err))
	}
	bounded, err := einoproviders.NewBoundedAgenticModel(m, limits)
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("gemini: bound agentic model: %w", err))
	}
	return &geminiAgenticModel{delegate: bounded}, nil
}

type geminiAgenticModel struct{ delegate model.AgenticModel }

func (m *geminiAgenticModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	if err := validateGeminiAgenticOptions(opts...); err != nil {
		return nil, err
	}
	return m.delegate.Generate(ctx, input, opts...)
}

func (m *geminiAgenticModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	if err := validateGeminiAgenticOptions(opts...); err != nil {
		return nil, err
	}
	return m.delegate.Stream(ctx, input, opts...)
}

func validateGeminiAgenticOptions(opts ...model.Option) error {
	common := model.GetCommonOptions(&model.Options{}, opts...)
	if len(common.DeferredTools) != 0 {
		return &einoproviders.UnsupportedCapabilityError{Provider: "gemini", Protocol: "generateContent", Capability: "deferred_tools"}
	}
	if common.ToolSearchTool != nil {
		return &einoproviders.UnsupportedCapabilityError{Provider: "gemini", Protocol: "generateContent", Capability: "tool_search"}
	}
	return nil
}
