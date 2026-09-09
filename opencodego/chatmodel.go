package opencodego

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

var errNilSuccessfulMessage = errors.New("opencode-go: adapter returned no message")

type chatModel struct {
	delegate model.ToolCallingChatModel
	config   preparedConfig
}

var _ model.ToolCallingChatModel = (*chatModel)(nil)

// NewChatModel constructs an OpenCode Go model without performing network or
// filesystem I/O. The selected protocol is explicit; adapters that have not
// landed return an initialization error.
func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error) {
	prepared, err := prepareConfig(cfg, chatModelConstruction)
	if err != nil {
		return nil, err
	}
	return newChatModelFromPrepared(ctx, prepared)
}

func newChatModelFromPrepared(ctx context.Context, cfg preparedConfig) (*chatModel, error) {
	if !protocolImplemented(cfg.protocol) {
		_, err := newProtocolAdapter(ctx, cfg)
		return nil, einoproviders.WrapInitError(err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	delegate, err := newProtocolAdapter(ctx, cfg)
	if err != nil {
		if errors.Is(err, einoproviders.ErrProviderInit) {
			return nil, err
		}
		return nil, einoproviders.WrapInitError(err)
	}
	return &chatModel{delegate: delegate, config: cfg}, nil
}

func (m *chatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return m.generate(ctx, input, operationGenerate, opts...)
}

func (m *chatModel) generate(ctx context.Context, input []*schema.Message, operation invocationOperation, opts ...model.Option) (*schema.Message, error) {
	preparedOpts, err := prepareInvocationOptions(opts)
	if err != nil {
		return nil, mapInvocationError(operation, err, nil)
	}
	opCtx, state := withOperationState(ctx)
	message, err := m.delegate.Generate(opCtx, input, preparedOpts...)
	if err != nil {
		return nil, mapInvocationError(operation, err, state)
	}
	if message == nil {
		return nil, mapInvocationError(operation, errNilSuccessfulMessage, state)
	}
	if err := normalizeGeneratedUsage(message, state); err != nil {
		return nil, mapInvocationError(operation, err, state)
	}
	return message, nil
}

func (m *chatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	preparedOpts, err := prepareInvocationOptions(opts)
	if err != nil {
		return nil, mapInvocationError(operationStream, err, nil)
	}
	opCtx, state := withOperationState(ctx)
	stream, err := m.delegate.Stream(opCtx, input, preparedOpts...)
	if err != nil {
		return nil, mapInvocationError(operationStream, err, state)
	}
	return normalizeObservedStream(opCtx, stream, state), nil
}

func (m *chatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if len(tools) == 0 {
		delegate, err := newProtocolAdapter(context.Background(), m.config)
		if err != nil {
			return nil, mapInvocationError(operationRequest, err, nil)
		}
		return &chatModel{delegate: delegate, config: m.config}, nil
	}
	owned, err := cloneTools(tools)
	if err != nil {
		return nil, mapInvocationError(operationRequest, err, nil)
	}
	derived, err := m.delegate.WithTools(owned)
	if err != nil {
		return nil, mapInvocationError(operationRequest, err, nil)
	}
	return &chatModel{delegate: derived, config: m.config}, nil
}

func prepareInvocationOptions(opts []model.Option) ([]model.Option, error) {
	common := model.GetCommonOptions(nil, opts...)
	if common.Model != nil && strings.TrimSpace(*common.Model) == "" {
		return nil, errors.New("opencode-go: model override is required")
	}
	if common.MaxTokens != nil && *common.MaxTokens <= 0 {
		return nil, errors.New("opencode-go: max token override must be positive")
	}
	prepared := append([]model.Option(nil), opts...)
	if common.Tools != nil {
		owned, err := cloneTools(common.Tools)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, model.WithTools(owned))
	}
	return prepared, nil
}
