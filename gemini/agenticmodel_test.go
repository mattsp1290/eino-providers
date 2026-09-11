package gemini

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestNewAgenticModelValidation(t *testing.T) {
	_, err := NewAgenticModel(context.Background(), AgenticModelConfig{})
	if !errors.Is(err, einoproviders.ErrProviderInit) {
		t.Fatalf("missing model error = %v", err)
	}
	_, err = NewAgenticModel(context.Background(), AgenticModelConfig{Model: "gemini-test", Limits: einoproviders.AgenticLimits{MaxContentBlocks: einoproviders.HardAgenticMaxContentBlocks + 1}})
	if !errors.Is(err, einoproviders.ErrProviderInit) {
		t.Fatalf("limits error = %v", err)
	}
}

func TestValidateGeminiAgenticOptionsRejectsUnsupportedBeforeDispatch(t *testing.T) {
	err := validateGeminiAgenticOptions(model.WithDeferredTools([]*schema.ToolInfo{{Name: "deferred"}}))
	if !errors.Is(err, einoproviders.ErrUnsupportedCapability) {
		t.Fatalf("deferred error = %v", err)
	}
	err = validateGeminiAgenticOptions(model.WithToolSearchTool(&schema.ToolInfo{Name: "search"}))
	if !errors.Is(err, einoproviders.ErrUnsupportedCapability) {
		t.Fatalf("search error = %v", err)
	}
}
