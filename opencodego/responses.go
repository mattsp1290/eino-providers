package opencodego

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

const (
	responsesReasoningItemsKey = "opencodego:reasoning_items"
	maxResponsesBodyBytes      = 16 << 20
)

var (
	errInvalidResponsesRequest  = errors.New("opencode-go: invalid Responses request")
	errInvalidResponsesResponse = errors.New("opencode-go: invalid Responses response")
	errResponsesBodyTooLarge    = errors.New("opencode-go: Responses body exceeds size limit")
	errResponsesNotCompleted    = errors.New("opencode-go: Responses request did not complete")
)

type responsesRequest struct {
	Model           string            `json:"model"`
	Instructions    string            `json:"instructions,omitempty"`
	Input           []json.RawMessage `json:"input"`
	Tools           []responsesTool   `json:"tools"`
	ToolChoice      string            `json:"tool_choice"`
	Store           bool              `json:"store"`
	Stream          bool              `json:"stream"`
	Include         []string          `json:"include"`
	MaxOutputTokens *int              `json:"max_output_tokens,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Strict      bool            `json:"strict"`
	Parameters  json.RawMessage `json:"parameters"`
}

type responsesInputMessage struct {
	Type    string                 `json:"type"`
	Role    string                 `json:"role"`
	Content []responsesContentItem `json:"content"`
}

type responsesContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesFunctionCall struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	CallID    string `json:"call_id"`
}

type responsesFunctionCallOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

func buildResponsesRequest(baseModel string, baseMaxTokens *int, baseTools []*schema.ToolInfo, input []*schema.Message, stream bool, opts ...model.Option) (*responsesRequest, error) {
	common := model.GetCommonOptions(&model.Options{
		Model:     stringPointer(baseModel),
		MaxTokens: snapshotMaxTokens(baseMaxTokens),
		Tools:     baseTools,
	}, opts...)
	if common.Model == nil || strings.TrimSpace(*common.Model) == "" {
		return nil, errInvalidResponsesRequest
	}
	if common.MaxTokens != nil && *common.MaxTokens <= 0 {
		return nil, errInvalidResponsesRequest
	}
	if common.Temperature != nil || common.TopP != nil || len(common.Stop) != 0 {
		return nil, errInvalidResponsesRequest
	}

	if len(common.AllowedToolNames) != 0 {
		return nil, errInvalidResponsesRequest
	}
	tools, err := buildResponsesTools(common.Tools)
	if err != nil {
		return nil, err
	}
	toolChoice, err := responsesToolChoice(common.ToolChoice, len(tools))
	if err != nil {
		return nil, err
	}
	instructions, items, err := responsesMessagesToInput(input)
	if err != nil {
		return nil, err
	}
	return &responsesRequest{
		Model:           *common.Model,
		Instructions:    instructions,
		Input:           items,
		Tools:           tools,
		ToolChoice:      toolChoice,
		Store:           false,
		Stream:          stream,
		Include:         []string{"reasoning.encrypted_content"},
		MaxOutputTokens: snapshotMaxTokens(common.MaxTokens),
	}, nil
}

func stringPointer(value string) *string {
	copy := value
	return &copy
}

func buildResponsesTools(tools []*schema.ToolInfo) ([]responsesTool, error) {
	seen := make(map[string]struct{}, len(tools))
	out := make([]responsesTool, 0, len(tools))
	for _, tool := range tools {
		if tool == nil || strings.TrimSpace(tool.Name) == "" {
			return nil, errInvalidResponsesRequest
		}
		if _, duplicate := seen[tool.Name]; duplicate {
			return nil, errInvalidResponsesRequest
		}
		seen[tool.Name] = struct{}{}
		parameters, err := responsesToolParameters(tool)
		if err != nil {
			return nil, errInvalidResponsesRequest
		}
		out = append(out, responsesTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Desc,
			Strict:      false,
			Parameters:  parameters,
		})
	}
	return out, nil
}

func responsesToolParameters(tool *schema.ToolInfo) (json.RawMessage, error) {
	if tool.ParamsOneOf == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}
	parameters, err := tool.ToJSONSchema()
	if err != nil || parameters == nil {
		return nil, errInvalidResponsesRequest
	}
	encoded, err := json.Marshal(parameters)
	if err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), encoded...), nil
}

func responsesToolChoice(choice *schema.ToolChoice, toolCount int) (string, error) {
	if choice == nil || *choice == schema.ToolChoiceAllowed {
		return "auto", nil
	}
	switch *choice {
	case schema.ToolChoiceForbidden:
		return "none", nil
	case schema.ToolChoiceForced:
		if toolCount == 0 {
			return "", errInvalidResponsesRequest
		}
		return "required", nil
	default:
		return "", errInvalidResponsesRequest
	}
}

func responsesMessagesToInput(messages []*schema.Message) (string, []json.RawMessage, error) {
	var instructions []string
	items := make([]json.RawMessage, 0, len(messages))
	pendingCalls := make(map[string]string)
	seenCalls := make(map[string]struct{})
	appendItem := func(item any) error {
		encoded, err := json.Marshal(item)
		if err != nil {
			return errInvalidResponsesRequest
		}
		items = append(items, encoded)
		return nil
	}

	for _, message := range messages {
		if message == nil || hasUnsupportedResponsesContent(message) {
			return "", nil, errInvalidResponsesRequest
		}
		switch message.Role {
		case schema.System:
			if len(message.ToolCalls) != 0 || message.ToolCallID != "" {
				return "", nil, errInvalidResponsesRequest
			}
			if message.Content != "" {
				instructions = append(instructions, message.Content)
			}
		case schema.User:
			if len(message.ToolCalls) != 0 || message.ToolCallID != "" {
				return "", nil, errInvalidResponsesRequest
			}
			if err := appendItem(responsesInputMessage{Type: "message", Role: "user", Content: []responsesContentItem{{Type: "input_text", Text: message.Content}}}); err != nil {
				return "", nil, err
			}
		case schema.Assistant:
			if message.ToolCallID != "" {
				return "", nil, errInvalidResponsesRequest
			}
			reasoning, err := responsesReasoningFromExtra(message.Extra)
			if err != nil {
				return "", nil, err
			}
			for _, raw := range reasoning {
				items = append(items, append(json.RawMessage(nil), raw...))
			}
			if message.Content != "" {
				if err := appendItem(responsesInputMessage{Type: "message", Role: "assistant", Content: []responsesContentItem{{Type: "output_text", Text: message.Content}}}); err != nil {
					return "", nil, err
				}
			}
			for _, call := range message.ToolCalls {
				if call.ID == "" || strings.TrimSpace(call.Function.Name) == "" || (call.Type != "" && call.Type != "function") {
					return "", nil, errInvalidResponsesRequest
				}
				if _, duplicate := seenCalls[call.ID]; duplicate {
					return "", nil, errInvalidResponsesRequest
				}
				arguments := call.Function.Arguments
				if arguments == "" {
					arguments = "{}"
				}
				if !json.Valid([]byte(arguments)) {
					return "", nil, errInvalidResponsesRequest
				}
				seenCalls[call.ID] = struct{}{}
				pendingCalls[call.ID] = call.Function.Name
				if err := appendItem(responsesFunctionCall{Type: "function_call", Name: call.Function.Name, Arguments: arguments, CallID: call.ID}); err != nil {
					return "", nil, err
				}
			}
		case schema.Tool:
			name, exists := pendingCalls[message.ToolCallID]
			if message.ToolCallID == "" || !exists || len(message.ToolCalls) != 0 || (message.ToolName != "" && message.ToolName != name) {
				return "", nil, errInvalidResponsesRequest
			}
			delete(pendingCalls, message.ToolCallID)
			if err := appendItem(responsesFunctionCallOutput{Type: "function_call_output", CallID: message.ToolCallID, Output: message.Content}); err != nil {
				return "", nil, err
			}
		default:
			return "", nil, errInvalidResponsesRequest
		}
	}
	return strings.Join(instructions, "\n\n"), items, nil
}

func hasUnsupportedResponsesContent(message *schema.Message) bool {
	return len(message.MultiContent) != 0 || len(message.UserInputMultiContent) != 0 || len(message.AssistantGenMultiContent) != 0 || message.Name != "" || message.ReasoningContent != ""
}

func responsesReasoningFromExtra(extra map[string]any) ([]json.RawMessage, error) {
	if extra == nil {
		return nil, nil
	}
	value, exists := extra[responsesReasoningItemsKey]
	if !exists {
		return nil, nil
	}
	var encodedItems []json.RawMessage
	switch items := value.(type) {
	case []json.RawMessage:
		encodedItems = items
	case []any:
		encodedItems = make([]json.RawMessage, len(items))
		for i, item := range items {
			encoded, err := json.Marshal(item)
			if err != nil {
				return nil, errInvalidResponsesRequest
			}
			encodedItems[i] = encoded
		}
	default:
		return nil, errInvalidResponsesRequest
	}
	result := make([]json.RawMessage, len(encodedItems))
	for i, item := range encodedItems {
		if err := validateResponsesReasoningItem(item); err != nil {
			return nil, err
		}
		result[i] = append(json.RawMessage(nil), item...)
	}
	return result, nil
}

func validateResponsesReasoningItem(raw json.RawMessage) error {
	var item struct {
		Type             string `json:"type"`
		EncryptedContent string `json:"encrypted_content"`
	}
	if !json.Valid(raw) || json.Unmarshal(raw, &item) != nil || item.Type != "reasoning" || item.EncryptedContent == "" {
		return errInvalidResponsesRequest
	}
	return nil
}

type responsesResponse struct {
	Status string            `json:"status"`
	Output []json.RawMessage `json:"output"`
	Usage  *responsesUsage   `json:"usage"`
	Error  json.RawMessage   `json:"error"`
}

type responsesUsage struct {
	InputTokens         int                         `json:"input_tokens"`
	OutputTokens        int                         `json:"output_tokens"`
	TotalTokens         int                         `json:"total_tokens"`
	InputTokensDetails  *responsesInputTokenDetails `json:"input_tokens_details"`
	OutputTokensDetails *responsesOutputTokenDetail `json:"output_tokens_details"`
}

type responsesInputTokenDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type responsesOutputTokenDetail struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type responsesOutputItem struct {
	Type      string                       `json:"type"`
	Status    string                       `json:"status"`
	Role      string                       `json:"role"`
	Content   []responsesOutputContentItem `json:"content"`
	Name      string                       `json:"name"`
	Arguments string                       `json:"arguments"`
	CallID    string                       `json:"call_id"`
}

type responsesOutputContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func decodeResponsesResponse(body []byte) (*schema.Message, error) {
	var response responsesResponse
	if !json.Valid(body) || json.Unmarshal(body, &response) != nil {
		return nil, responsesAPIFailure(errInvalidResponsesResponse)
	}
	if response.Status != "completed" {
		return nil, responsesAPIFailure(errResponsesNotCompleted)
	}
	if response.Output == nil || (len(response.Error) != 0 && string(bytes.TrimSpace(response.Error)) != "null") {
		return nil, responsesAPIFailure(errInvalidResponsesResponse)
	}
	if err := validateResponsesUsage(response.Usage); err != nil {
		return nil, responsesAPIFailure(err)
	}

	var text strings.Builder
	toolCalls := make([]schema.ToolCall, 0)
	reasoning := make([]json.RawMessage, 0)
	seenCallIDs := make(map[string]struct{})
	for _, raw := range response.Output {
		var item responsesOutputItem
		if err := json.Unmarshal(raw, &item); err != nil || (item.Status != "" && item.Status != "completed") {
			return nil, responsesAPIFailure(errInvalidResponsesResponse)
		}
		switch item.Type {
		case "message":
			if item.Role != "assistant" || item.Content == nil {
				return nil, responsesAPIFailure(errInvalidResponsesResponse)
			}
			for _, content := range item.Content {
				if content.Type != "output_text" {
					return nil, responsesAPIFailure(errInvalidResponsesResponse)
				}
				text.WriteString(content.Text)
			}
		case "function_call":
			if item.CallID == "" || strings.TrimSpace(item.Name) == "" || !json.Valid([]byte(item.Arguments)) {
				return nil, responsesAPIFailure(errInvalidResponsesResponse)
			}
			if _, duplicate := seenCallIDs[item.CallID]; duplicate {
				return nil, responsesAPIFailure(errInvalidResponsesResponse)
			}
			seenCallIDs[item.CallID] = struct{}{}
			index := len(toolCalls)
			toolCalls = append(toolCalls, schema.ToolCall{Index: &index, ID: item.CallID, Type: "function", Function: schema.FunctionCall{Name: item.Name, Arguments: item.Arguments}})
		case "reasoning":
			if err := validateResponsesReasoningItem(raw); err != nil {
				return nil, responsesAPIFailure(errInvalidResponsesResponse)
			}
			reasoning = append(reasoning, append(json.RawMessage(nil), raw...))
		default:
			return nil, responsesAPIFailure(errInvalidResponsesResponse)
		}
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	message := &schema.Message{
		Role:         schema.Assistant,
		Content:      text.String(),
		ToolCalls:    toolCalls,
		ResponseMeta: &schema.ResponseMeta{FinishReason: finishReason, Usage: responsesTokenUsage(response.Usage)},
	}
	if len(reasoning) > 0 {
		message.Extra = map[string]any{responsesReasoningItemsKey: reasoning}
	}
	return message, nil
}

func validateResponsesUsage(usage *responsesUsage) error {
	if usage == nil {
		return nil
	}
	values := []int{usage.InputTokens, usage.OutputTokens, usage.TotalTokens}
	if usage.InputTokensDetails != nil {
		values = append(values, usage.InputTokensDetails.CachedTokens)
	}
	if usage.OutputTokensDetails != nil {
		values = append(values, usage.OutputTokensDetails.ReasoningTokens)
	}
	for _, value := range values {
		if value < 0 {
			return errInvalidResponsesResponse
		}
	}
	return nil
}

func responsesTokenUsage(usage *responsesUsage) *schema.TokenUsage {
	if usage == nil {
		return nil
	}
	result := &schema.TokenUsage{PromptTokens: usage.InputTokens, CompletionTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens}
	if usage.InputTokensDetails != nil {
		result.PromptTokenDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
	}
	if usage.OutputTokensDetails != nil {
		result.CompletionTokensDetails.ReasoningTokens = usage.OutputTokensDetails.ReasoningTokens
	}
	return result
}

func responsesAPIFailure(causes ...error) error {
	return safeFailure(operationGenerate, append([]error{einoproviders.ErrProviderAPI}, causes...)...)
}
