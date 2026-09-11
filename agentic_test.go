package einoproviders

import (
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestAgenticLimitsDefaultsAndValidation(t *testing.T) {
	got, err := (AgenticLimits{}).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got.MaxRequestBytes != DefaultAgenticMaxRequestBytes || got.MaxContentBlocks != DefaultAgenticMaxContentBlocks {
		t.Fatalf("defaults = %#v", got)
	}
	if _, err := (AgenticLimits{MaxEventBytes: HardAgenticMaxEventBytes + 1}).Validate(); err == nil {
		t.Fatal("Validate accepted event bytes over hard cap")
	}
	if _, err := (AgenticLimits{MaxContentBlocks: -1}).Validate(); err == nil {
		t.Fatal("Validate accepted negative content blocks")
	}
}

func TestValidateAgenticContentBlocksRejectsOverLimit(t *testing.T) {
	messages := []*schema.AgenticMessage{{ContentBlocks: make([]*schema.ContentBlock, 2)}}
	err := ValidateAgenticContentBlocks(messages, AgenticLimits{MaxContentBlocks: 1})
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("error = %v", err)
	}
}

func TestSplitRestoreAgenticContinuationDoesNotMutatePublicMessage(t *testing.T) {
	message := &schema.AgenticMessage{Extra: map[string]any{"private": "sentinel"}}
	public, state, err := SplitAgenticContinuationForProvider(message, "test", "native")
	if err != nil {
		t.Fatal(err)
	}
	if public.Extra != nil || message.Extra["private"] != "sentinel" {
		t.Fatalf("public/source extras = %#v / %#v", public.Extra, message.Extra)
	}
	restored, err := RestoreAgenticContinuationForProvider(public, state, "test", "native")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Extra["private"] != "sentinel" {
		t.Fatalf("restored extra = %#v", restored.Extra)
	}
}
