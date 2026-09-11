package ollama

import (
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func SplitAgenticContinuation(msg *schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) {
	return einoproviders.SplitAgenticContinuationForProvider(msg, "ollama", "api/chat")
}

func RestoreAgenticContinuation(public *schema.AgenticMessage, state einoproviders.AgenticContinuationState) (*schema.AgenticMessage, error) {
	return einoproviders.RestoreAgenticContinuationForProvider(public, state, "ollama", "api/chat")
}
