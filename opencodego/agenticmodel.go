package opencodego

import (
	"context"
	"fmt"
	"net/http"

	agenticopenai "github.com/cloudwego/eino-ext/components/model/agenticopenai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	providerclaude "github.com/mattsp1290/eino-providers/claude"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// AgenticModelConfig configures a native OpenCode Go agentic model. Protocol
// remains explicit because Responses, Messages, and Chat Completions have
// different native wire capabilities.
type AgenticModelConfig struct {
	Model      string
	Protocol   Protocol
	APIKey     string
	UserAgent  string
	SessionID  string
	BaseURL    string
	HTTPClient *http.Client
	MaxTokens  *int
	Limits     einoproviders.AgenticLimits
}

type openCodeAgenticModel struct {
	delegate model.AgenticModel
	provider string
	protocol string
	model    string
	limits   einoproviders.AgenticLimits
}

func (m *openCodeAgenticModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	if err := validateOpenCodeAgenticOptions(m.protocol, opts...); err != nil {
		return nil, err
	}
	result, err := m.delegate.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	m.attachIdentity(result)
	return result, nil
}

func (m *openCodeAgenticModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	if err := validateOpenCodeAgenticOptions(m.protocol, opts...); err != nil {
		return nil, err
	}
	stream, err := m.delegate.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderWithConvert(stream, func(message *schema.AgenticMessage) (*schema.AgenticMessage, error) {
		m.attachIdentity(message)
		return message, nil
	}), nil
}

func (m *openCodeAgenticModel) attachIdentity(message *schema.AgenticMessage) {
	if message == nil || message.ResponseMeta == nil {
		return
	}
	identity := einoproviders.AgenticResponseIdentity{Provider: m.provider, Protocol: m.protocol, RequestedModel: m.model}
	if native, ok := message.ResponseMeta.Extension.(einoproviders.AgenticResponseIdentity); ok {
		identity.ReturnedModel = native.ReturnedModel
		identity.CorrelationID = native.CorrelationID
		message.ResponseMeta.Extension = identity
		return
	}
	message.ResponseMeta.Extension = einoproviders.AgenticResponseMetadata{Identity: identity, Native: message.ResponseMeta.Extension}
}

func validateOpenCodeAgenticOptions(protocol string, opts ...model.Option) error {
	common := model.GetCommonOptions(&model.Options{}, opts...)
	if len(common.DeferredTools) != 0 || common.ToolSearchTool != nil {
		return &einoproviders.UnsupportedCapabilityError{Provider: "opencodego", Protocol: protocol, Capability: "tool_search"}
	}
	if common.AgenticToolChoice != nil && protocol == "chat_completions" {
		return &einoproviders.UnsupportedCapabilityError{Provider: "opencodego", Protocol: protocol, Capability: "agentic_tool_choice"}
	}
	return nil
}

// NewAgenticModel constructs a native AgenticModel for the selected OpenCode
// protocol. It never routes inputs through ToolCallingChatModel.
func NewAgenticModel(ctx context.Context, cfg AgenticModelConfig) (model.AgenticModel, error) {
	limits, err := cfg.Limits.Validate()
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("opencodego: invalid agentic limits: %w", err))
	}
	prepared, err := prepareConfig(ChatModelConfig{Model: cfg.Model, Protocol: cfg.Protocol, APIKey: cfg.APIKey, UserAgent: cfg.UserAgent, SessionID: cfg.SessionID, BaseURL: cfg.BaseURL, HTTPClient: cfg.HTTPClient, MaxTokens: cfg.MaxTokens}, providerConstruction)
	if err != nil {
		return nil, err
	}
	switch prepared.protocol {
	case ProtocolResponses:
		httpClient, err := newObservedHTTPClient(prepared.authClient)
		if err != nil {
			return nil, err
		}
		httpClient, err = einoproviders.NewAgenticLimitedHTTPClient(httpClient, limits)
		if err != nil {
			return nil, mapConstructorError(err)
		}
		zero := 0
		m, err := agenticopenai.NewResponsesModel(ctx, &agenticopenai.ResponsesConfig{APIKey: sdkValidationPlaceholder, BaseURL: prepared.authClient.BaseURL(), HTTPClient: httpClient, Model: prepared.model, MaxTokens: prepared.maxTokens, MaxRetries: &zero})
		if err != nil {
			return nil, mapConstructorError(err)
		}
		return newBoundedOpenCodeAgenticModel(m, "responses", prepared.model, limits)
	case ProtocolChatCompletions:
		httpClient, err := newObservedHTTPClient(prepared.authClient)
		if err != nil {
			return nil, err
		}
		httpClient, err = einoproviders.NewAgenticLimitedHTTPClient(httpClient, limits)
		if err != nil {
			return nil, mapConstructorError(err)
		}
		m, err := agenticopenai.NewChatModel(ctx, &agenticopenai.ChatConfig{APIKey: sdkValidationPlaceholder, BaseURL: prepared.authClient.BaseURL(), HTTPClient: httpClient, Model: prepared.model, MaxCompletionTokens: prepared.maxTokens})
		if err != nil {
			return nil, mapConstructorError(err)
		}
		return newBoundedOpenCodeAgenticModel(m, "chat_completions", prepared.model, limits)
	case ProtocolMessages:
		if prepared.maxTokens == nil {
			return nil, mapConstructorError(fmt.Errorf("opencodego: MaxTokens is required for Messages"))
		}
		httpClient, baseURL, err := newMessagesHTTPClient(prepared.authClient)
		if err != nil {
			return nil, err
		}
		httpClient, err = einoproviders.NewAgenticLimitedHTTPClient(httpClient, limits)
		if err != nil {
			return nil, mapConstructorError(err)
		}
		m, err := providerclaude.NewAgenticModel(ctx, providerclaude.AgenticModelConfig{Model: prepared.model, MaxTokens: *prepared.maxTokens, BaseURL: baseURL, HTTPClient: httpClient, Limits: limits})
		if err != nil {
			return nil, err
		}
		return newBoundedOpenCodeAgenticModel(m, "messages", prepared.model, limits)
	default:
		return nil, mapConstructorError(fmt.Errorf("opencodego: unsupported Protocol %q", prepared.protocol))
	}
}

func newBoundedOpenCodeAgenticModel(delegate model.AgenticModel, protocol, modelName string, limits einoproviders.AgenticLimits) (model.AgenticModel, error) {
	bounded, err := einoproviders.NewBoundedAgenticModel(delegate, limits)
	if err != nil {
		return nil, mapConstructorError(err)
	}
	return &openCodeAgenticModel{delegate: bounded, provider: "opencodego", protocol: protocol, model: modelName, limits: limits}, nil
}
