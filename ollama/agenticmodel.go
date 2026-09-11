package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// AgenticModelConfig configures a native Ollama /api/chat model.
type AgenticModelConfig struct {
	BaseURL    string
	Model      string
	Timeout    time.Duration
	KeepAlive  string
	HTTPClient *http.Client
	Thinking   *bool
	Limits     einoproviders.AgenticLimits
}

type agenticModel struct {
	baseURL   string
	model     string
	client    *http.Client
	think     *bool
	keepAlive string
	limits    einoproviders.AgenticLimits
}

// NewAgenticModel constructs a native Ollama /api/chat AgenticModel. It uses
// the provider JSON/NDJSON protocol directly and never bridges through the
// classic schema.Message adapter.
func NewAgenticModel(ctx context.Context, cfg AgenticModelConfig) (model.AgenticModel, error) {
	if err := validateBaseURL(cfg.BaseURL); err != nil {
		return nil, einoproviders.WrapInitError(err)
	}
	if cfg.Model == "" {
		return nil, einoproviders.WrapInitError(fmt.Errorf("ollama: Model is required"))
	}
	if cfg.Timeout <= 0 && cfg.HTTPClient == nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("ollama: Timeout must be > 0 when HTTPClient is nil"))
	}
	if _, err := parseKeepAlive(cfg.KeepAlive); err != nil {
		return nil, einoproviders.WrapInitError(err)
	}
	limits, err := cfg.Limits.Validate()
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("ollama: invalid agentic limits: %w", err))
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	if err := pingWithCappedTimeout(ctx, cfg.BaseURL, client, cfg.Timeout); err != nil {
		return nil, err
	}
	return &agenticModel{baseURL: strings.TrimRight(cfg.BaseURL, "/"), model: cfg.Model, client: client, think: cfg.Thinking, keepAlive: strings.TrimSpace(cfg.KeepAlive), limits: limits}, nil
}

func (m *agenticModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	body, err := m.requestBody(input, false, opts...)
	if err != nil {
		return nil, err
	}
	resp, err := m.do(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, m.httpError(resp)
	}
	var wire ollamaChatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, m.limits.MaxResponseBytes+1)).Decode(&wire); err != nil {
		return nil, fmt.Errorf("ollama: decode agentic response: %w", err)
	}
	if !wire.Done {
		return nil, fmt.Errorf("ollama: incomplete agentic response")
	}
	return m.fromResponse(wire, false), nil
}

func (m *agenticModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	body, err := m.requestBody(input, true, opts...)
	if err != nil {
		return nil, err
	}
	resp, err := m.do(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, m.httpError(resp)
	}
	reader, writer := schema.Pipe[*schema.AgenticMessage](1)
	go func() {
		defer resp.Body.Close()
		defer writer.Close()
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, m.limits.MaxResponseBytes+1))
		scanner.Buffer(make([]byte, 64<<10), int(m.limits.MaxEventBytes))
		seenDone := false
		for scanner.Scan() {
			var wire ollamaChatResponse
			if err := json.Unmarshal(scanner.Bytes(), &wire); err != nil {
				writer.Send(nil, fmt.Errorf("ollama: decode agentic stream: %w", err))
				return
			}
			if writer.Send(m.fromResponse(wire, true), nil) {
				return
			}
			if wire.Done {
				seenDone = true
				return
			}
		}
		if err := scanner.Err(); err != nil {
			writer.Send(nil, &einoproviders.ResourceLimitError{Resource: "response_bytes", Limit: m.limits.MaxResponseBytes})
			return
		}
		if !seenDone {
			writer.Send(nil, fmt.Errorf("ollama: truncated agentic stream"))
		}
	}()
	return reader, nil
}

func (m *agenticModel) do(ctx context.Context, body []byte) (*http.Response, error) {
	if int64(len(body)) > m.limits.MaxRequestBytes {
		return nil, &einoproviders.ResourceLimitError{Resource: "request_bytes", Limit: m.limits.MaxRequestBytes, Actual: int64(len(body))}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: build agentic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: send agentic request: %w", err)
	}
	return resp, nil
}

func (m *agenticModel) httpError(resp *http.Response) error {
	limited := io.LimitReader(resp.Body, m.limits.MaxErrorBodyBytes)
	b, _ := io.ReadAll(limited)
	return fmt.Errorf("ollama: agentic API status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
}

func (m *agenticModel) requestBody(input []*schema.AgenticMessage, stream bool, opts ...model.Option) ([]byte, error) {
	common := model.GetCommonOptions(&model.Options{}, opts...)
	if len(common.DeferredTools) != 0 || common.ToolSearchTool != nil || common.AgenticToolChoice != nil {
		return nil, &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: "agentic_tools"}
	}
	messages, err := toOllamaAgenticMessages(input)
	if err != nil {
		return nil, err
	}
	tools, err := toOllamaAgenticTools(common.Tools)
	if err != nil {
		return nil, err
	}
	modelName := m.model
	if common.Model != nil {
		modelName = *common.Model
	}
	payload := ollamaChatRequest{Model: modelName, Messages: messages, Stream: stream, Tools: tools, Think: m.think}
	if m.keepAlive != "" {
		payload.KeepAlive = m.keepAlive
	}
	if common.Temperature != nil || common.TopP != nil || len(common.Stop) > 0 {
		payload.Options = map[string]any{}
		if common.Temperature != nil {
			payload.Options["temperature"] = *common.Temperature
		}
		if common.TopP != nil {
			payload.Options["top_p"] = *common.TopP
		}
		if len(common.Stop) > 0 {
			payload.Options["stop"] = common.Stop
		}
	}
	return json.Marshal(payload)
}

type ollamaChatRequest struct {
	Model     string          `json:"model"`
	Messages  []ollamaMessage `json:"messages"`
	Stream    bool            `json:"stream"`
	Tools     []ollamaTool    `json:"tools,omitempty"`
	Options   map[string]any  `json:"options,omitempty"`
	Think     *bool           `json:"think,omitempty"`
	KeepAlive string          `json:"keep_alive,omitempty"`
}
type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Thinking  string           `json:"thinking,omitempty"`
	Images    []string         `json:"images,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
}
type ollamaToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}
type ollamaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}
type ollamaChatResponse struct {
	Model           string        `json:"model"`
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	DoneReason      string        `json:"done_reason,omitempty"`
	PromptEvalCount int           `json:"prompt_eval_count,omitempty"`
	EvalCount       int           `json:"eval_count,omitempty"`
}

func toOllamaAgenticMessages(input []*schema.AgenticMessage) ([]ollamaMessage, error) {
	result := make([]ollamaMessage, 0, len(input))
	for _, msg := range input {
		if msg == nil {
			return nil, &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: "nil_message"}
		}
		out := ollamaMessage{Role: string(msg.Role)}
		if out.Role != "system" && out.Role != "user" && out.Role != "assistant" {
			return nil, &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: "role"}
		}
		for _, block := range msg.ContentBlocks {
			if block == nil {
				return nil, &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: "nil_block"}
			}
			switch block.Type {
			case schema.ContentBlockTypeUserInputText:
				if block.UserInputText == nil {
					return nil, invalidOllamaBlock()
				}
				out.Content += block.UserInputText.Text
			case schema.ContentBlockTypeAssistantGenText:
				if block.AssistantGenText == nil {
					return nil, invalidOllamaBlock()
				}
				out.Content += block.AssistantGenText.Text
			case schema.ContentBlockTypeReasoning:
				if block.Reasoning == nil {
					return nil, invalidOllamaBlock()
				}
				out.Thinking += block.Reasoning.Text
			case schema.ContentBlockTypeUserInputImage:
				if block.UserInputImage == nil || block.UserInputImage.Base64Data == "" {
					return nil, &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: "image_url"}
				}
				if _, err := base64.StdEncoding.DecodeString(block.UserInputImage.Base64Data); err != nil {
					return nil, &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: "image_base64"}
				}
				out.Images = append(out.Images, block.UserInputImage.Base64Data)
			case schema.ContentBlockTypeFunctionToolCall:
				if block.FunctionToolCall == nil {
					return nil, invalidOllamaBlock()
				}
				var args map[string]any
				if err := json.Unmarshal([]byte(block.FunctionToolCall.Arguments), &args); err != nil {
					return nil, &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: "function_arguments"}
				}
				call := ollamaToolCall{}
				call.Function.Name, call.Function.Arguments = block.FunctionToolCall.Name, args
				out.ToolCalls = append(out.ToolCalls, call)
			case schema.ContentBlockTypeFunctionToolResult:
				if block.FunctionToolResult == nil || len(block.FunctionToolResult.Content) != 1 || block.FunctionToolResult.Content[0].Type != schema.FunctionToolResultContentBlockTypeText || block.FunctionToolResult.Content[0].Text == nil {
					return nil, &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: "function_result"}
				}
				out.Role, out.ToolName, out.Content = "tool", block.FunctionToolResult.Name, block.FunctionToolResult.Content[0].Text.Text
			default:
				return nil, &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: einoproviders.Capability(block.Type)}
			}
		}
		result = append(result, out)
	}
	return result, nil
}

func invalidOllamaBlock() error {
	return &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: "malformed_block"}
}

func toOllamaAgenticTools(tools []*schema.ToolInfo) ([]ollamaTool, error) {
	result := make([]ollamaTool, 0, len(tools))
	for _, tool := range tools {
		if tool == nil || tool.Name == "" {
			return nil, &einoproviders.UnsupportedCapabilityError{Provider: "ollama", Protocol: "api/chat", Capability: "tool"}
		}
		params := any(map[string]any{"type": "object"})
		if tool.ParamsOneOf != nil {
			var err error
			params, err = tool.ParamsOneOf.ToJSONSchema()
			if err != nil {
				return nil, fmt.Errorf("ollama: encode tool %q: %w", tool.Name, err)
			}
		}
		out := ollamaTool{Type: "function"}
		out.Function.Name, out.Function.Description, out.Function.Parameters = tool.Name, tool.Desc, params
		result = append(result, out)
	}
	return result, nil
}

func (m *agenticModel) fromResponse(wire ollamaChatResponse, streaming bool) *schema.AgenticMessage {
	blocks := make([]*schema.ContentBlock, 0, 2+len(wire.Message.ToolCalls))
	index := 0
	add := func(block *schema.ContentBlock) {
		if streaming {
			block.StreamingMeta = &schema.StreamingMeta{Index: index}
		}
		index++
		blocks = append(blocks, block)
	}
	if wire.Message.Thinking != "" {
		add(schema.NewContentBlock(&schema.Reasoning{Text: wire.Message.Thinking}))
	}
	if wire.Message.Content != "" {
		add(schema.NewContentBlock(&schema.AssistantGenText{Text: wire.Message.Content}))
	}
	for callIndex, call := range wire.Message.ToolCalls {
		args, _ := json.Marshal(call.Function.Arguments)
		// Ollama does not expose a wire call ID. This is deliberately a
		// request-local synthetic correlation ID and is never serialized back.
		add(schema.NewContentBlock(&schema.FunctionToolCall{CallID: fmt.Sprintf("ollama-%d", callIndex), Name: call.Function.Name, Arguments: string(args)}))
	}
	message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: blocks}
	if !streaming || wire.Done {
		message.ResponseMeta = &schema.AgenticResponseMeta{TokenUsage: &schema.TokenUsage{PromptTokens: wire.PromptEvalCount, CompletionTokens: wire.EvalCount, TotalTokens: wire.PromptEvalCount + wire.EvalCount}, Extension: einoproviders.AgenticResponseIdentity{Provider: "ollama", Protocol: "api/chat", RequestedModel: m.model, ReturnedModel: wire.Model}}
	}
	return message
}
