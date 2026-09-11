package einoproviders

import "testing"

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
