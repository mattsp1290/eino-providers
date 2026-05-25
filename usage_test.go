package einoproviders

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestExtractUsagePresent(t *testing.T) {
	msg := &schema.Message{
		ResponseMeta: &schema.ResponseMeta{
			Usage: &schema.TokenUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
			},
		},
	}

	u := ExtractUsage(msg)
	if !u.Available {
		t.Error("Available should be true when usage is present")
	}
	if u.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", u.InputTokens)
	}
	if u.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", u.OutputTokens)
	}
}

func TestExtractUsageNilResponseMeta(t *testing.T) {
	msg := &schema.Message{ResponseMeta: nil}

	u := ExtractUsage(msg)
	if u.Available {
		t.Error("Available should be false when ResponseMeta is nil")
	}
}

func TestExtractUsageNilUsage(t *testing.T) {
	msg := &schema.Message{
		ResponseMeta: &schema.ResponseMeta{Usage: nil},
	}

	u := ExtractUsage(msg)
	if u.Available {
		t.Error("Available should be false when Usage is nil")
	}
}

func TestExtractUsageZeroFields(t *testing.T) {
	msg := &schema.Message{
		ResponseMeta: &schema.ResponseMeta{
			Usage: &schema.TokenUsage{
				PromptTokens:     0,
				CompletionTokens: 0,
			},
		},
	}

	u := ExtractUsage(msg)
	if !u.Available {
		t.Error("Available should be true even when token counts are zero")
	}
	if u.InputTokens != 0 || u.OutputTokens != 0 {
		t.Error("zero token counts should be preserved")
	}
}

func TestExtractUsageNilMessage(t *testing.T) {
	u := ExtractUsage(nil)
	if u.Available {
		t.Error("Available should be false for nil message")
	}
}
