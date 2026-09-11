package claude

import (
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func SplitAgenticContinuation(msg *schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) {
	return einoproviders.SplitAgenticContinuationForProvider(msg, "claude", "messages")
}

func RestoreAgenticContinuation(public *schema.AgenticMessage, state einoproviders.AgenticContinuationState) (*schema.AgenticMessage, error) {
	return einoproviders.RestoreAgenticContinuationForProvider(public, state, "claude", "messages")
}
