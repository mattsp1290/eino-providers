package opencodego

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	claudeadapter "github.com/cloudwego/eino-ext/components/model/claude"
	openaiadapter "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// The SDK requires a nonempty key while constructing its client. Requests do
// not use this value: opencode-auth-go replaces authentication at the outer
// transport boundary.
const sdkValidationPlaceholder = "opencode-go-sdk-placeholder"

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
			owned, err := cloneJSON(parameters, encoded)
			if err != nil {
				return nil, fmt.Errorf("opencodego: copy tool %q parameters: %w", tool.Name, err)
			}
			clone.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(owned)
		}
		clones[i] = clone
	}
	return clones, nil
}

func cloneJSON[T any](_ *T, encoded []byte) (*T, error) {
	owned := new(T)
	if err := json.Unmarshal(encoded, owned); err != nil {
		return nil, err
	}
	return owned, nil
}

func newProtocolAdapter(ctx context.Context, cfg preparedConfig) (model.ToolCallingChatModel, error) {
	switch cfg.protocol {
	case ProtocolChatCompletions:
		return newChatCompletionsAdapter(ctx, cfg)
	case ProtocolMessages:
		return newMessagesAdapter(ctx, cfg)
	case ProtocolResponses:
		return newResponsesAdapter(ctx, cfg)
	default:
		return nil, fmt.Errorf("opencodego: unsupported Protocol %q", cfg.protocol)
	}
}

func newMessagesAdapter(ctx context.Context, cfg preparedConfig) (model.ToolCallingChatModel, error) {
	if cfg.maxTokens == nil {
		return nil, mapConstructorError(fmt.Errorf("opencodego: MaxTokens is required for Messages"))
	}
	httpClient, sdkBaseURL, err := newMessagesHTTPClient(cfg.authClient)
	if err != nil {
		return nil, err
	}
	delegate, err := claudeadapter.NewChatModel(ctx, &claudeadapter.Config{
		APIKey:     sdkValidationPlaceholder,
		BaseURL:    &sdkBaseURL,
		HTTPClient: httpClient,
		Model:      cfg.model,
		MaxTokens:  *cfg.maxTokens,
	})
	if err != nil {
		return nil, mapConstructorError(err)
	}
	return delegate, nil
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
