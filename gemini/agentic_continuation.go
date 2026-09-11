package gemini

import (
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func SplitAgenticContinuation(msg *schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) {
	return einoproviders.SplitAgenticContinuationForProvider(msg, "gemini", "generateContent")
}

func RestoreAgenticContinuation(public *schema.AgenticMessage, state einoproviders.AgenticContinuationState) (*schema.AgenticMessage, error) {
	return einoproviders.RestoreAgenticContinuationForProvider(public, state, "gemini", "generateContent")
}
