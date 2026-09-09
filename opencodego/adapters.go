package opencodego

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	openaiadapter "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// The SDK requires a nonempty key while constructing its client. Requests do
// not use this value: opencode-auth-go replaces authentication at the outer
// transport boundary.
const sdkValidationPlaceholder = "opencode-go-sdk-placeholder"

func protocolImplemented(protocol Protocol) bool {
	return protocol == ProtocolChatCompletions
}

// cloneTools converts parameter descriptions into an owned JSON Schema. The
// pinned OpenAI adapter sorts schema fields while binding, so passing caller
// objects directly would let one derived model mutate another's inputs.
func cloneTools(tools []*schema.ToolInfo) ([]*schema.ToolInfo, error) {
	clones := make([]*schema.ToolInfo, len(tools))
	for i, tool := range tools {
		if tool == nil {
			return nil, fmt.Errorf("opencodego: tool %d is nil", i)
		}
		clone := &schema.ToolInfo{Name: tool.Name, Desc: tool.Desc, Extra: maps.Clone(tool.Extra)}
		if tool.ParamsOneOf != nil {
			parameters, err := tool.ToJSONSchema()
			if err != nil {
				return nil, fmt.Errorf("opencodego: copy tool %q parameters: %w", tool.Name, err)
			}
			encoded, err := json.Marshal(parameters)
			if err != nil {
				return nil, fmt.Errorf("opencodego: copy tool %q parameters: %w", tool.Name, err)
			}
			owned := new(jsonschema.Schema)
			if err := json.Unmarshal(encoded, owned); err != nil {
				return nil, fmt.Errorf("opencodego: copy tool %q parameters: %w", tool.Name, err)
			}
			clone.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(owned)
		}
		clones[i] = clone
	}
	return clones, nil
}

func newProtocolAdapter(ctx context.Context, cfg preparedConfig) (model.ToolCallingChatModel, error) {
	switch cfg.protocol {
	case ProtocolChatCompletions:
		return newChatCompletionsAdapter(ctx, cfg)
	case ProtocolMessages:
		return nil, fmt.Errorf("opencodego: Messages adapter is not available")
	case ProtocolResponses:
		return nil, fmt.Errorf("opencodego: Responses adapter is not available")
	default:
		return nil, fmt.Errorf("opencodego: unsupported Protocol %q", cfg.protocol)
	}
}

func newChatCompletionsAdapter(ctx context.Context, cfg preparedConfig) (model.ToolCallingChatModel, error) {
	httpClient, err := newObservedHTTPClient(cfg.authClient)
	if err != nil {
		return nil, err
	}
	delegate, err := openaiadapter.NewChatModel(ctx, &openaiadapter.ChatModelConfig{
		APIKey:     sdkValidationPlaceholder,
		BaseURL:    cfg.authClient.BaseURL(),
		HTTPClient: httpClient,
		Model:      cfg.model,
		MaxTokens:  snapshotMaxTokens(cfg.maxTokens),
	})
	if err != nil {
		return nil, mapConstructorError(err)
	}
	return delegate, nil
}
