package opencodego

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestNewAgenticModelValidatesSharedLimitsFirst(t *testing.T) {
	_, err := NewAgenticModel(context.Background(), AgenticModelConfig{Limits: einoproviders.AgenticLimits{MaxEventBytes: einoproviders.HardAgenticMaxEventBytes + 1}})
	if !errors.Is(err, einoproviders.ErrProviderInit) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateOpenCodeAgenticOptionsRejectsBeforeDispatch(t *testing.T) {
	err := validateOpenCodeAgenticOptions("responses", model.WithDeferredTools([]*schema.ToolInfo{{Name: "deferred"}}))
	if !errors.Is(err, einoproviders.ErrUnsupportedCapability) {
		t.Fatalf("deferred error = %v", err)
	}
	err = validateOpenCodeAgenticOptions("chat_completions", model.WithAgenticToolChoice(&schema.AgenticToolChoice{Type: schema.ToolChoiceAllowed}))
	if !errors.Is(err, einoproviders.ErrUnsupportedCapability) {
		t.Fatalf("choice error = %v", err)
	}
}
