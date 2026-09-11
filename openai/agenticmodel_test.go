package openai

import (
	"context"
	"errors"
	"testing"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestNewAgenticModelValidation(t *testing.T) {
	valid := AgenticModelConfig{APIKey: "test", Model: "gpt-test"}
	if model, err := NewAgenticModel(context.Background(), valid); err != nil || model == nil {
		t.Fatalf("NewAgenticModel(valid) = %v, %v", model, err)
	}
	for name, cfg := range map[string]AgenticModelConfig{
		"model":   {APIKey: "test"},
		"key":     {Model: "gpt-test"},
		"retries": {APIKey: "test", Model: "gpt-test", MaxRetries: 1},
		"limits":  {APIKey: "test", Model: "gpt-test", Limits: einoproviders.AgenticLimits{MaxEventBytes: einoproviders.HardAgenticMaxEventBytes + 1}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewAgenticModel(context.Background(), cfg)
			if !errors.Is(err, einoproviders.ErrProviderInit) {
				t.Fatalf("error = %v, want init error", err)
			}
		})
	}
}
