package openai

import (
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// SplitAgenticContinuation creates an immutable public projection and a
// Responses-tagged opaque continuation envelope. Provider extras and reasoning
// signatures remain only in the returned state.
func SplitAgenticContinuation(msg *schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) {
	return einoproviders.SplitAgenticContinuationForProvider(msg, "openai", "responses")
}

// RestoreAgenticContinuation reconstructs a provider-ready clone without
// mutating either the public message or its opaque continuation state.
func RestoreAgenticContinuation(public *schema.AgenticMessage, state einoproviders.AgenticContinuationState) (*schema.AgenticMessage, error) {
	return einoproviders.RestoreAgenticContinuationForProvider(public, state, "openai", "responses")
}
