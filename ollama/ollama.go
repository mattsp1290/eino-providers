package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	einoollama "github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino/components/model"
)

// Config configures an Ollama ToolCallingChatModel.
type Config struct {
	BaseURL string
	Model   string
	Timeout time.Duration

	// KeepAlive uses Ollama duration syntax. Empty or whitespace leaves the
	// Eino adapter default unset. "-1" means keep the model loaded indefinitely.
	KeepAlive string

	HTTPClient *http.Client
	Format     json.RawMessage
	Options    *einoollama.Options
	Thinking   *einoollama.ThinkValue
}

// NewChatModel constructs an Ollama Eino ToolCallingChatModel.
func NewChatModel(ctx context.Context, cfg Config) (model.ToolCallingChatModel, error) {
	if err := validateBaseURL(cfg.BaseURL); err != nil {
		return nil, err
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("ollama: Model is required")
	}
	if cfg.Timeout <= 0 && cfg.HTTPClient == nil {
		return nil, fmt.Errorf("ollama: Timeout must be > 0 when HTTPClient is nil")
	}

	keepAlive, err := parseKeepAlive(cfg.KeepAlive)
	if err != nil {
		return nil, err
	}
	if pingErr := pingWithCappedTimeout(ctx, cfg.BaseURL, cfg.HTTPClient, cfg.Timeout); pingErr != nil {
		return nil, pingErr
	}

	chatModel, err := einoollama.NewChatModel(ctx, &einoollama.ChatModelConfig{
		BaseURL:    cfg.BaseURL,
		Timeout:    cfg.Timeout,
		HTTPClient: cfg.HTTPClient,
		Model:      cfg.Model,
		Format:     cfg.Format,
		KeepAlive:  keepAlive,
		Options:    cfg.Options,
		Thinking:   cfg.Thinking,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama: build chat model %q: %w", cfg.Model, err)
	}
	return chatModel, nil
}

func validateBaseURL(baseURL string) error {
	if baseURL == "" {
		return fmt.Errorf("ollama: BaseURL is required")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("ollama: BaseURL is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("ollama: BaseURL scheme must be http or https (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("ollama: BaseURL is missing a host")
	}
	return nil
}
