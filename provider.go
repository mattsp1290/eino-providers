package einoproviders

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// Usage carries token accounting from a single provider call.
//
// Available is false when the upstream provider did not return usage data.
// Consumers must not interpret InputTokens or OutputTokens unless Available is
// true.
type Usage struct {
	InputTokens  int
	OutputTokens int
	Available    bool
}

// ExtractUsage converts Eino message usage data into a Usage struct.
func ExtractUsage(msg *schema.Message) Usage {
	if msg != nil && msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
		return Usage{
			InputTokens:  msg.ResponseMeta.Usage.PromptTokens,
			OutputTokens: msg.ResponseMeta.Usage.CompletionTokens,
			Available:    true,
		}
	}
	return Usage{Available: false}
}

// Provider is the contract every single-shot backend must satisfy.
//
// Advise sends a single-turn request consisting of a system message and a user
// message, then returns the model's text reply. Implementations must honour ctx
// cancellation and deadlines, clamp maxTokens at the backend's Eino config
// layer, report unavailable usage as Usage{Available: false}, and wrap upstream
// errors with a short backend prefix.
type Provider interface {
	Advise(ctx context.Context, system, user string, maxTokens int) (text string, usage Usage, err error)
}
