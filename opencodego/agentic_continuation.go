package opencodego

import (
	"fmt"

	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// SplitAgenticContinuation preserves protocol selection in the opaque state.
// A message must carry the identity returned by this package so a Responses,
// Messages, or Chat Completions continuation cannot be restored into another
// wire protocol by accident.
func SplitAgenticContinuation(msg *schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) {
	protocol, err := openCodeContinuationProtocol(msg)
	if err != nil {
		return nil, einoproviders.AgenticContinuationState{}, err
	}
	return einoproviders.SplitAgenticContinuationForProvider(msg, "opencodego", protocol)
}

func RestoreAgenticContinuation(public *schema.AgenticMessage, state einoproviders.AgenticContinuationState) (*schema.AgenticMessage, error) {
	protocol, err := openCodeContinuationProtocol(public)
	if err != nil {
		return nil, err
	}
	return einoproviders.RestoreAgenticContinuationForProvider(public, state, "opencodego", protocol)
}

func openCodeContinuationProtocol(msg *schema.AgenticMessage) (string, error) {
	if msg == nil || msg.ResponseMeta == nil {
		return "", fmt.Errorf("opencodego: continuation requires response identity")
	}
	switch extension := msg.ResponseMeta.Extension.(type) {
	case einoproviders.AgenticResponseIdentity:
		if extension.Provider == "opencodego" && extension.Protocol != "" {
			return extension.Protocol, nil
		}
	case einoproviders.AgenticResponseMetadata:
		if extension.Identity.Provider == "opencodego" && extension.Identity.Protocol != "" {
			return extension.Identity.Protocol, nil
		}
	}
	return "", fmt.Errorf("opencodego: continuation requires opencodego response identity")
}
