package claude

import (
	"bufio"
	"bytes"
	"context"
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

// AgenticModelConfig configures the native Anthropic Messages protocol. The
// implementation owns HTTP dispatch so it performs exactly one request per
// model invocation; it does not inherit SDK retry behavior.
type AgenticModelConfig struct {
	APIKey     string
	Model      string
	MaxTokens  int
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
	Limits     einoproviders.AgenticLimits
}

type agenticModel struct {
	baseURL, model, apiKey string
	maxTokens              int
	client                 *http.Client
	limits                 einoproviders.AgenticLimits
}

// NewAgenticModel constructs a direct native Anthropic Messages AgenticModel.
func NewAgenticModel(_ context.Context, cfg AgenticModelConfig) (model.AgenticModel, error) {
	if cfg.Model == "" {
		return nil, einoproviders.WrapInitError(fmt.Errorf("claude: Model is required"))
	}
	if cfg.MaxTokens <= 0 {
		return nil, einoproviders.WrapInitError(fmt.Errorf("claude: MaxTokens must be > 0"))
	}
	if cfg.APIKey == "" && cfg.HTTPClient == nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("claude: APIKey is required when HTTPClient is nil"))
	}
	limits, err := cfg.Limits.Validate()
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("claude: invalid agentic limits: %w", err))
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	client, err = einoproviders.NewAgenticLimitedHTTPClient(client, limits)
	if err != nil {
		return nil, einoproviders.WrapInitError(fmt.Errorf("claude: build agentic limited client: %w", err))
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	return einoproviders.NewBoundedAgenticModel(&agenticModel{baseURL: base, model: cfg.Model, apiKey: cfg.APIKey, maxTokens: cfg.MaxTokens, client: client, limits: limits}, limits)
}

func (m *agenticModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	body, err := m.request(input, false, opts...)
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
	var wire claudeResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, m.limits.MaxResponseBytes+1)).Decode(&wire); err != nil {
		return nil, fmt.Errorf("claude: decode agentic response: %w", err)
	}
	return m.fromResponse(wire, false), nil
}

func (m *agenticModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	body, err := m.request(input, true, opts...)
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
		modelID, complete := "", false
		calls := map[int]*claudePendingToolCall{}
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var event claudeStreamEvent
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
				writer.Send(nil, fmt.Errorf("claude: decode agentic stream: %w", err))
				return
			}
			if event.Type == "message_start" {
				modelID = event.Message.Model
				continue
			}
			if event.Type == "content_block_start" && event.ContentBlock.Type == "tool_use" {
				calls[event.Index] = &claudePendingToolCall{id: event.ContentBlock.ID, name: event.ContentBlock.Name}
				continue
			}
			if event.Type == "content_block_delta" {
				if call := calls[event.Index]; call != nil && event.Delta.Type == "input_json_delta" {
					call.arguments.WriteString(event.Delta.PartialJSON)
					continue
				}
				if chunk := m.deltaMessage(event); chunk != nil && writer.Send(chunk, nil) {
					return
				}
				continue
			}
			if event.Type == "content_block_stop" {
				if call := calls[event.Index]; call != nil {
					args := call.arguments.String()
					if !json.Valid([]byte(args)) {
						writer.Send(nil, &einoproviders.UnsupportedCapabilityError{Provider: "claude", Protocol: "messages", Capability: "function_arguments"})
						return
					}
					block := schema.NewContentBlock(&schema.FunctionToolCall{CallID: call.id, Name: call.name, Arguments: args})
					block.StreamingMeta = &schema.StreamingMeta{Index: event.Index}
					if writer.Send(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{block}}, nil) {
						return
					}
					delete(calls, event.Index)
				}
				continue
			}
			if event.Type == "message_delta" {
				continue
			}
			if event.Type == "message_stop" {
				complete = true
				if writer.Send(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ResponseMeta: &schema.AgenticResponseMeta{Extension: einoproviders.AgenticResponseIdentity{Provider: "claude", Protocol: "messages", RequestedModel: m.model, ReturnedModel: modelID}}}, nil) {
					return
				}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			writer.Send(nil, &einoproviders.ResourceLimitError{Resource: "response_bytes", Limit: m.limits.MaxResponseBytes})
			return
		}
		if !complete {
			writer.Send(nil, fmt.Errorf("claude: truncated agentic stream"))
		}
	}()
	return reader, nil
}

func (m *agenticModel) do(ctx context.Context, body []byte) (*http.Response, error) {
	if int64(len(body)) > m.limits.MaxRequestBytes {
		return nil, &einoproviders.ResourceLimitError{Resource: "request_bytes", Limit: m.limits.MaxRequestBytes, Actual: int64(len(body))}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if m.apiKey != "" {
		req.Header.Set("x-api-key", m.apiKey)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("claude: send agentic request: %w", err)
	}
	return resp, nil
}

func (m *agenticModel) httpError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, m.limits.MaxErrorBodyBytes))
	return fmt.Errorf("claude: agentic API status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
}

func (m *agenticModel) request(input []*schema.AgenticMessage, stream bool, opts ...model.Option) ([]byte, error) {
	common := model.GetCommonOptions(&model.Options{}, opts...)
	if common.ToolSearchTool != nil || len(common.DeferredTools) != 0 {
		return nil, &einoproviders.UnsupportedCapabilityError{Provider: "claude", Protocol: "messages", Capability: "tool_search"}
	}
	messages := make([]map[string]any, 0, len(input))
	system := ""
	for _, msg := range input {
		if msg == nil {
			return nil, &einoproviders.UnsupportedCapabilityError{Provider: "claude", Protocol: "messages", Capability: "nil_message"}
		}
		var content []map[string]any
		for _, b := range msg.ContentBlocks {
			if b == nil {
				return nil, &einoproviders.UnsupportedCapabilityError{Provider: "claude", Protocol: "messages", Capability: "nil_block"}
			}
			switch b.Type {
			case schema.ContentBlockTypeUserInputText:
				if b.UserInputText == nil {
					return nil, invalidClaudeBlock()
				}
				content = append(content, map[string]any{"type": "text", "text": b.UserInputText.Text})
			case schema.ContentBlockTypeAssistantGenText:
				if b.AssistantGenText == nil {
					return nil, invalidClaudeBlock()
				}
				content = append(content, map[string]any{"type": "text", "text": b.AssistantGenText.Text})
			case schema.ContentBlockTypeReasoning:
				if b.Reasoning == nil {
					return nil, invalidClaudeBlock()
				}
				content = append(content, map[string]any{"type": "thinking", "thinking": b.Reasoning.Text, "signature": b.Reasoning.Signature})
			case schema.ContentBlockTypeFunctionToolCall:
				if b.FunctionToolCall == nil {
					return nil, invalidClaudeBlock()
				}
				var args any
				if json.Unmarshal([]byte(b.FunctionToolCall.Arguments), &args) != nil {
					return nil, &einoproviders.UnsupportedCapabilityError{Provider: "claude", Protocol: "messages", Capability: "function_arguments"}
				}
				content = append(content, map[string]any{"type": "tool_use", "id": b.FunctionToolCall.CallID, "name": b.FunctionToolCall.Name, "input": args})
			case schema.ContentBlockTypeFunctionToolResult:
				if b.FunctionToolResult == nil || len(b.FunctionToolResult.Content) != 1 || b.FunctionToolResult.Content[0].Text == nil {
					return nil, &einoproviders.UnsupportedCapabilityError{Provider: "claude", Protocol: "messages", Capability: "function_result"}
				}
				content = append(content, map[string]any{"type": "tool_result", "tool_use_id": b.FunctionToolResult.CallID, "content": b.FunctionToolResult.Content[0].Text.Text})
			default:
				return nil, &einoproviders.UnsupportedCapabilityError{Provider: "claude", Protocol: "messages", Capability: einoproviders.Capability(b.Type)}
			}
		}
		if msg.Role == schema.AgenticRoleTypeSystem {
			for _, c := range content {
				if text, ok := c["text"].(string); ok {
					system += text
				}
			}
			continue
		}
		if msg.Role != schema.AgenticRoleTypeUser && msg.Role != schema.AgenticRoleTypeAssistant {
			return nil, &einoproviders.UnsupportedCapabilityError{Provider: "claude", Protocol: "messages", Capability: "role"}
		}
		messages = append(messages, map[string]any{"role": msg.Role, "content": content})
	}
	modelID := m.model
	if common.Model != nil {
		modelID = *common.Model
	}
	max := m.maxTokens
	if common.MaxTokens != nil {
		max = *common.MaxTokens
	}
	payload := map[string]any{"model": modelID, "max_tokens": max, "messages": messages, "stream": stream}
	if system != "" {
		payload["system"] = system
	}
	return json.Marshal(payload)
}

func invalidClaudeBlock() error {
	return &einoproviders.UnsupportedCapabilityError{Provider: "claude", Protocol: "messages", Capability: "malformed_block"}
}

type claudeResponse struct {
	ID         string          `json:"id"`
	Model      string          `json:"model"`
	StopReason string          `json:"stop_reason"`
	Content    []claudeContent `json:"content"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
type claudeContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Signature string          `json:"signature"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}
type claudeStreamEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Message struct {
		Model string `json:"model"`
	} `json:"message"`
}

type claudePendingToolCall struct {
	id, name  string
	arguments strings.Builder
}

func (m *agenticModel) fromResponse(wire claudeResponse, streaming bool) *schema.AgenticMessage {
	blocks := make([]*schema.ContentBlock, 0, len(wire.Content))
	for i, item := range wire.Content {
		var block *schema.ContentBlock
		switch item.Type {
		case "text":
			block = schema.NewContentBlock(&schema.AssistantGenText{Text: item.Text})
		case "thinking":
			block = schema.NewContentBlock(&schema.Reasoning{Text: item.Thinking, Signature: item.Signature})
		case "tool_use":
			block = schema.NewContentBlock(&schema.FunctionToolCall{CallID: item.ID, Name: item.Name, Arguments: string(item.Input)})
		default:
			continue
		}
		if streaming {
			block.StreamingMeta = &schema.StreamingMeta{Index: i}
		}
		blocks = append(blocks, block)
	}
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: blocks, ResponseMeta: &schema.AgenticResponseMeta{TokenUsage: &schema.TokenUsage{PromptTokens: wire.Usage.InputTokens, CompletionTokens: wire.Usage.OutputTokens, TotalTokens: wire.Usage.InputTokens + wire.Usage.OutputTokens}, Extension: einoproviders.AgenticResponseIdentity{Provider: "claude", Protocol: "messages", RequestedModel: m.model, ReturnedModel: wire.Model, CorrelationID: wire.ID}}}
}
func (m *agenticModel) deltaMessage(event claudeStreamEvent) *schema.AgenticMessage {
	var block *schema.ContentBlock
	switch event.Delta.Type {
	case "text_delta":
		block = schema.NewContentBlock(&schema.AssistantGenText{Text: event.Delta.Text})
	case "thinking_delta":
		block = schema.NewContentBlock(&schema.Reasoning{Text: event.Delta.Thinking})
	case "signature_delta":
		block = schema.NewContentBlock(&schema.Reasoning{Signature: event.Delta.Signature})
	default:
		return nil
	}
	block.StreamingMeta = &schema.StreamingMeta{Index: event.Index}
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{block}}
}
