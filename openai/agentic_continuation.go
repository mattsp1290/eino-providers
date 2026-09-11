package openai

import (
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

const agenticContinuationVersion = 1

type continuationPayload struct {
	MessageExtra map[string]json.RawMessage   `json:"message_extra,omitempty"`
	BlockExtra   []map[string]json.RawMessage `json:"block_extra,omitempty"`
	Signatures   map[int]string               `json:"reasoning_signatures,omitempty"`
}

// SplitAgenticContinuation clones msg into a display-safe public message and
// a versioned opaque payload. The public clone intentionally excludes provider
// extras and encrypted reasoning signatures; neither input is mutated.
func SplitAgenticContinuation(msg *schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) {
	if msg == nil {
		return nil, einoproviders.AgenticContinuationState{}, fmt.Errorf("openai: nil agentic message")
	}
	public, err := cloneAgenticMessage(msg)
	if err != nil {
		return nil, einoproviders.AgenticContinuationState{}, err
	}
	payload := continuationPayload{BlockExtra: make([]map[string]json.RawMessage, len(msg.ContentBlocks)), Signatures: map[int]string{}}
	if payload.MessageExtra, err = marshalExtra(msg.Extra); err != nil {
		return nil, einoproviders.AgenticContinuationState{}, err
	}
	public.Extra = nil
	for i, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if payload.BlockExtra[i], err = marshalExtra(block.Extra); err != nil {
			return nil, einoproviders.AgenticContinuationState{}, err
		}
		if public.ContentBlocks[i] != nil {
			public.ContentBlocks[i].Extra = nil
			if reasoning := public.ContentBlocks[i].Reasoning; reasoning != nil {
				payload.Signatures[i] = reasoning.Signature
				reasoning.Signature = ""
			}
		}
	}
	if len(payload.Signatures) == 0 {
		payload.Signatures = nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, einoproviders.AgenticContinuationState{}, fmt.Errorf("openai: encode continuation: %w", err)
	}
	return public, einoproviders.AgenticContinuationState{Provider: "openai", Protocol: "responses", Version: agenticContinuationVersion, Opaque: encoded}, nil
}

// RestoreAgenticContinuation combines a public clone and state into a new
// provider-ready message. It validates the provider envelope and does not
// mutate either input.
func RestoreAgenticContinuation(public *schema.AgenticMessage, state einoproviders.AgenticContinuationState) (*schema.AgenticMessage, error) {
	if public == nil {
		return nil, fmt.Errorf("openai: nil public agentic message")
	}
	if state.Provider != "openai" || state.Protocol != "responses" || state.Version != agenticContinuationVersion {
		return nil, fmt.Errorf("openai: incompatible agentic continuation state")
	}
	var payload continuationPayload
	if err := json.Unmarshal(state.Opaque, &payload); err != nil {
		return nil, fmt.Errorf("openai: decode continuation: %w", err)
	}
	if len(payload.BlockExtra) != 0 && len(payload.BlockExtra) != len(public.ContentBlocks) {
		return nil, fmt.Errorf("openai: continuation block count mismatch")
	}
	restored, err := cloneAgenticMessage(public)
	if err != nil {
		return nil, err
	}
	restored.Extra, err = unmarshalExtra(payload.MessageExtra)
	if err != nil {
		return nil, err
	}
	for i := range restored.ContentBlocks {
		if restored.ContentBlocks[i] == nil {
			continue
		}
		if len(payload.BlockExtra) > 0 {
			restored.ContentBlocks[i].Extra, err = unmarshalExtra(payload.BlockExtra[i])
			if err != nil {
				return nil, err
			}
		}
		if signature, ok := payload.Signatures[i]; ok && restored.ContentBlocks[i].Reasoning != nil {
			restored.ContentBlocks[i].Reasoning.Signature = signature
		}
	}
	return restored, nil
}

func cloneAgenticMessage(msg *schema.AgenticMessage) (*schema.AgenticMessage, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("openai: clone agentic message: %w", err)
	}
	var clone schema.AgenticMessage
	if err := json.Unmarshal(b, &clone); err != nil {
		return nil, fmt.Errorf("openai: clone agentic message: %w", err)
	}
	return &clone, nil
}

func marshalExtra(extra map[string]any) (map[string]json.RawMessage, error) {
	if len(extra) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(extra)
	if err != nil {
		return nil, fmt.Errorf("openai: encode continuation extra: %w", err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func unmarshalExtra(extra map[string]json.RawMessage) (map[string]any, error) {
	if len(extra) == 0 {
		return nil, nil
	}
	result := make(map[string]any, len(extra))
	for key, value := range extra {
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, fmt.Errorf("openai: decode continuation extra: %w", err)
		}
		result[key] = decoded
	}
	return result, nil
}
