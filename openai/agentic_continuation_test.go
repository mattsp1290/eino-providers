package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestAgenticContinuationSeparatesPrivateState(t *testing.T) {
	source := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, Extra: map[string]any{"private": "cache-secret"}, ContentBlocks: []*schema.ContentBlock{
		{Type: schema.ContentBlockTypeReasoning, Reasoning: &schema.Reasoning{Text: "visible", Signature: "encrypted-secret"}, Extra: map[string]any{"item": "item-secret"}},
	}}
	public, state, err := SplitAgenticContinuation(source)
	if err != nil {
		t.Fatal(err)
	}
	encodedPublic, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if string(encodedPublic) == "" || strings.Contains(string(encodedPublic), "secret") {
		t.Fatalf("public continuation leaked private value: %s", encodedPublic)
	}
	if source.ContentBlocks[0].Reasoning.Signature != "encrypted-secret" || source.Extra["private"] != "cache-secret" {
		t.Fatal("split mutated source")
	}
	reopenedPublic := new(schema.AgenticMessage)
	if err := json.Unmarshal(encodedPublic, reopenedPublic); err != nil {
		t.Fatal(err)
	}
	reopenedState := state
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stateJSON, &reopenedState); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreAgenticContinuation(reopenedPublic, reopenedState)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ContentBlocks[0].Reasoning.Signature != "encrypted-secret" || restored.Extra["private"] != "cache-secret" {
		t.Fatalf("restored state = %#v", restored)
	}
}
