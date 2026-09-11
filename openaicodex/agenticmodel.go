package openaicodex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	codexauth "github.com/mattsp1290/codex-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// AgenticModelConfig configures the direct Codex Responses agentic model.
type AgenticModelConfig struct {
	AppName          string
	Model            string
	HTTPClient       *http.Client
	ReasoningEffort  string
	DisableReasoning bool
	Limits           einoproviders.AgenticLimits
}

type agenticModel struct {
	httpClient *http.Client
	model      string
	reasoning  *reasoningParam
	include    []string
	limits     einoproviders.AgenticLimits
}

// NewAgenticModel constructs an AgenticModel using the Codex-auth-go HTTP
// transport. It keeps auth error sentinels intact and does not use classic
// ChatModel conversion.
func NewAgenticModel(ctx context.Context, cfg AgenticModelConfig) (model.AgenticModel, error) {
	if cfg.HTTPClient != nil {
		return NewAgenticModelWithHTTPClient(ctx, cfg.HTTPClient, cfg)
	}
	client, err := newCodexChatClient(ctx, cfg.AppName)
	if err != nil {
		if errors.Is(err, codexauth.ErrNotLoggedIn) {
			return nil, einoproviders.WrapInitError(einoproviders.WrapAuthError(codexauth.ErrNotLoggedIn))
		}
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai-codex: build client: %w", err))
	}
	return NewAgenticModelWithHTTPClient(ctx, client, cfg)
}

// NewAgenticModelWithHTTPClient constructs an AgenticModel with an already
// authenticated Codex client, which is useful for deterministic native tests.
func NewAgenticModelWithHTTPClient(_ context.Context, client *http.Client, cfg AgenticModelConfig) (model.AgenticModel, error) {
	if client == nil {
		return nil, einoproviders.WrapInitError(errors.New("openai-codex: HTTPClient must not be nil"))
	}
	if cfg.Model == "" {
		return nil, einoproviders.WrapInitError(errors.New("openai-codex: Model is required"))
	}
	limits, err := cfg.Limits.Validate()
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai-codex: invalid agentic limits: %w", err))
	}
	m := &agenticModel{httpClient: client, model: cfg.Model, include: []string{}, limits: limits}
	if !cfg.DisableReasoning {
		effort := cfg.ReasoningEffort
		if effort == "" {
			effort = defaultReasoningEffort
		}
		m.reasoning = &reasoningParam{Effort: effort, Summary: "auto"}
		m.include = []string{"reasoning.encrypted_content"}
	}
	return m, nil
}

func (m *agenticModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	stream, err := m.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var chunks []*schema.AgenticMessage
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("openai-codex: empty agentic response")
	}
	return schema.ConcatAgenticMessages(chunks)
}

func (m *agenticModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	common := model.GetCommonOptions(&model.Options{Model: &m.model}, opts...)
	if len(common.DeferredTools) != 0 || common.ToolSearchTool != nil || common.AgenticToolChoice != nil {
		return nil, &einoproviders.UnsupportedCapabilityError{Provider: "openai-codex", Protocol: "responses", Capability: "agentic_options"}
	}
	body, err := m.buildRequest(common, input)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > m.limits.MaxRequestBytes {
		return nil, &einoproviders.ResourceLimitError{Resource: "request_bytes", Limit: m.limits.MaxRequestBytes, Actual: int64(len(payload))}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexauth.CodexEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("openai-codex: responses request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, m.limits.MaxErrorBodyBytes))
		return nil, classifyResponsesError(resp.StatusCode, b)
	}
	reader, writer := schema.Pipe[*schema.AgenticMessage](8)
	go func() {
		defer resp.Body.Close()
		defer writer.Close()
		streamAgenticResponses(ctx, resp.Body, writer, m.model, m.limits)
	}()
	return reader, nil
}

func (m *agenticModel) buildRequest(common *model.Options, input []*schema.AgenticMessage) (*responsesRequest, error) {
	modelName := m.model
	if common.Model != nil && *common.Model != "" {
		modelName = *common.Model
	}
	tools, err := buildToolsJSON(common.Tools)
	if err != nil {
		return nil, err
	}
	instructions, items, err := agenticMessagesToInput(input)
	if err != nil {
		return nil, err
	}
	return &responsesRequest{Model: modelName, Instructions: instructions, Input: items, Tools: tools, ToolChoice: "auto", ParallelToolCalls: false, Reasoning: m.reasoning, Store: false, Stream: true, Include: m.include}, nil
}

func agenticMessagesToInput(messages []*schema.AgenticMessage) (string, []any, error) {
	var system []string
	items := make([]any, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			return "", nil, &einoproviders.UnsupportedCapabilityError{Provider: "openai-codex", Protocol: "responses", Capability: "nil_message"}
		}
		var text string
		for _, b := range msg.ContentBlocks {
			if b == nil {
				return "", nil, &einoproviders.UnsupportedCapabilityError{Provider: "openai-codex", Protocol: "responses", Capability: "nil_block"}
			}
			switch b.Type {
			case schema.ContentBlockTypeUserInputText:
				text += b.UserInputText.Text
			case schema.ContentBlockTypeAssistantGenText:
				text += b.AssistantGenText.Text
			case schema.ContentBlockTypeReasoning:
				items = append(items, map[string]any{"type": "reasoning", "encrypted_content": b.Reasoning.Signature, "summary": []map[string]any{{"type": "summary_text", "text": b.Reasoning.Text}}})
			case schema.ContentBlockTypeFunctionToolCall:
				items = append(items, inputFunctionCall{Type: "function_call", Name: b.FunctionToolCall.Name, Arguments: b.FunctionToolCall.Arguments, CallID: b.FunctionToolCall.CallID})
			case schema.ContentBlockTypeFunctionToolResult:
				if b.FunctionToolResult == nil || len(b.FunctionToolResult.Content) != 1 || b.FunctionToolResult.Content[0].Text == nil {
					return "", nil, &einoproviders.UnsupportedCapabilityError{Provider: "openai-codex", Protocol: "responses", Capability: "function_result"}
				}
				items = append(items, inputFunctionCallOutput{Type: "function_call_output", CallID: b.FunctionToolResult.CallID, Output: b.FunctionToolResult.Content[0].Text.Text})
			default:
				return "", nil, &einoproviders.UnsupportedCapabilityError{Provider: "openai-codex", Protocol: "responses", Capability: einoproviders.Capability(b.Type)}
			}
		}
		if msg.Role == schema.AgenticRoleTypeSystem {
			system = append(system, text)
			continue
		}
		if msg.Role != schema.AgenticRoleTypeUser && msg.Role != schema.AgenticRoleTypeAssistant {
			return "", nil, &einoproviders.UnsupportedCapabilityError{Provider: "openai-codex", Protocol: "responses", Capability: "role"}
		}
		if text != "" {
			typ := "input_text"
			if msg.Role == schema.AgenticRoleTypeAssistant {
				typ = "output_text"
			}
			items = append(items, inputMessage{Type: "message", Role: string(msg.Role), Content: []contentItem{{Type: typ, Text: text}}})
		}
	}
	return strings.Join(system, "\n\n"), items, nil
}
