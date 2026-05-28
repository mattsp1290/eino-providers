package openaicodex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/schema"
	codexauth "github.com/mattsp1290/codex-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// extraKeyReasoningItems stores the raw Responses-API `reasoning` output items
// (id + encrypted_content) on an assistant schema.Message.Extra so they can be
// re-threaded into `input` on subsequent turns. The Codex Responses endpoint is
// stateless per call (no previous_response_id) and reasoning models expect prior
// reasoning items echoed back between a function_call and its function_call_output.
const extraKeyReasoningItems = "openaicodex:reasoning_items"

// sseMaxLineBytes caps a single SSE data buffer. Encrypted reasoning content can
// be large, so this is well above bufio's 64KiB default.
const sseMaxLineBytes = 10 << 20 // 10 MiB

// --- Responses API request types ---
//
// Field presence mirrors the codex CLI's ResponsesApiRequest
// (codex-rs/codex-api/src/common.rs): `reasoning` is always serialized (null when
// absent); `tools`, `tool_choice`, `parallel_tool_calls`, `store`, `stream`,
// `include` are always present; `instructions` is omitted only when empty.

type responsesRequest struct {
	Model             string          `json:"model"`
	Instructions      string          `json:"instructions,omitempty"`
	Input             []any           `json:"input"`
	Tools             []responsesTool `json:"tools"`
	ToolChoice        string          `json:"tool_choice"`
	ParallelToolCalls bool            `json:"parallel_tool_calls"`
	Reasoning         *reasoningParam `json:"reasoning"` // pointer, no omitempty: null when nil
	Store             bool            `json:"store"`
	Stream            bool            `json:"stream"`
	Include           []string        `json:"include"`
	PromptCacheKey    string          `json:"prompt_cache_key,omitempty"`
}

type reasoningParam struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Strict      bool            `json:"strict"`
	Parameters  json.RawMessage `json:"parameters"`
}

// --- Responses API input items (serde tag="type", snake_case) ---

type inputMessage struct {
	Type    string        `json:"type"` // "message"
	Role    string        `json:"role"`
	Content []contentItem `json:"content"`
}

type contentItem struct {
	Type string `json:"type"` // "input_text" | "output_text"
	Text string `json:"text"`
}

type inputFunctionCall struct {
	Type      string `json:"type"` // "function_call"
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON encoded as a string
	CallID    string `json:"call_id"`
}

type inputFunctionCallOutput struct {
	Type   string `json:"type"` // "function_call_output"
	CallID string `json:"call_id"`
	Output string `json:"output"` // plain string form of FunctionCallOutputPayload
}

// buildToolsJSON converts Eino tool descriptions into Responses-API tool specs.
func buildToolsJSON(tools []*schema.ToolInfo) ([]responsesTool, error) {
	if len(tools) == 0 {
		return []responsesTool{}, nil
	}
	out := make([]responsesTool, 0, len(tools))
	for _, ti := range tools {
		if ti == nil {
			continue
		}
		params, err := toolParamsJSON(ti)
		if err != nil {
			return nil, fmt.Errorf("openai-codex: tool %q parameters: %w", ti.Name, err)
		}
		out = append(out, responsesTool{
			Type:        "function",
			Name:        ti.Name,
			Description: ti.Desc,
			Strict:      false,
			Parameters:  params,
		})
	}
	return out, nil
}

func toolParamsJSON(ti *schema.ToolInfo) (json.RawMessage, error) {
	if ti.ParamsOneOf == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}
	sc, err := ti.ToJSONSchema()
	if err != nil {
		return nil, err
	}
	if sc == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}
	return json.Marshal(sc)
}

// messagesToInput converts an Eino message history into the top-level
// `instructions` string and the Responses-API `input` item list.
//
// System messages fold into instructions. For an assistant turn, any captured
// reasoning items (from Extra) are re-emitted first, then assistant text, then
// function_call items — keeping each function_call adjacent to the
// function_call_output produced by the following tool message.
func messagesToInput(msgs []*schema.Message) (string, []any, error) {
	var systemParts []string
	input := make([]any, 0, len(msgs))

	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		switch msg.Role {
		case schema.System:
			if msg.Content != "" {
				systemParts = append(systemParts, msg.Content)
			}
		case schema.User:
			input = append(input, inputMessage{
				Type:    "message",
				Role:    "user",
				Content: []contentItem{{Type: "input_text", Text: msg.Content}},
			})
		case schema.Assistant:
			for _, raw := range reasoningItemsFromExtra(msg) {
				input = append(input, raw)
			}
			if msg.Content != "" {
				input = append(input, inputMessage{
					Type:    "message",
					Role:    "assistant",
					Content: []contentItem{{Type: "output_text", Text: msg.Content}},
				})
			}
			for _, tc := range msg.ToolCalls {
				args := tc.Function.Arguments
				if args == "" {
					args = "{}"
				}
				input = append(input, inputFunctionCall{
					Type:      "function_call",
					Name:      tc.Function.Name,
					Arguments: args,
					CallID:    tc.ID,
				})
			}
		case schema.Tool:
			input = append(input, inputFunctionCallOutput{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: msg.Content,
			})
		default:
			return "", nil, fmt.Errorf("openai-codex: unsupported message role %q", msg.Role)
		}
	}

	return strings.Join(systemParts, "\n\n"), input, nil
}

// reasoningItemsFromExtra extracts raw reasoning items previously stored on an
// assistant message so they can be re-threaded into the next request.
func reasoningItemsFromExtra(msg *schema.Message) []json.RawMessage {
	if msg.Extra == nil {
		return nil
	}
	v, ok := msg.Extra[extraKeyReasoningItems]
	if !ok {
		return nil
	}
	if items, ok := v.([]json.RawMessage); ok {
		return items
	}
	return nil
}

// --- SSE event parsing ---

type sseEvent struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta"`
	Item     json.RawMessage `json:"item"`
	Response json.RawMessage `json:"response"`
}

type sseItem struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	CallID           string `json:"call_id"`
	EncryptedContent string `json:"encrypted_content"`
}

type sseResponse struct {
	ID    string    `json:"id"`
	Usage *sseUsage `json:"usage"`
	Error *sseError `json:"error"`
}

type sseUsage struct {
	InputTokens         int                  `json:"input_tokens"`
	OutputTokens        int                  `json:"output_tokens"`
	TotalTokens         int                  `json:"total_tokens"`
	InputTokensDetails  *sseInputTokenDetail `json:"input_tokens_details"`
	OutputTokensDetails *sseOutputTokenDetl  `json:"output_tokens_details"`
}

type sseInputTokenDetail struct {
	CachedTokens int `json:"cached_tokens"`
}

type sseOutputTokenDetl struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type sseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// streamResponses reads a Responses-API SSE stream and emits schema.Message
// chunks on sw. It returns when the stream terminates, errors, ctx is cancelled,
// or the consumer closes the reader (detected via sw.Send reporting closed).
//
// All emitted chunks carry Role=Assistant so schema.ConcatMessages can merge a
// coherent assistant message. The terminal chunk carries usage, finish reason,
// and any captured reasoning items (for multi-turn round-tripping) in Extra.
func streamResponses(ctx context.Context, body io.Reader, sw *schema.StreamWriter[*schema.Message]) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), sseMaxLineBytes)

	var (
		dataBuf       strings.Builder
		toolIndex     int
		sawToolCall   bool
		reasoningRaws []json.RawMessage
		completed     bool
	)

	// send returns true if the consumer closed the stream (caller should stop).
	send := func(msg *schema.Message) bool {
		return sw.Send(msg, nil)
	}

	dispatch := func(data string) (stop bool) {
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			return false
		}
		var ev sseEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			// Ignore unparseable frames rather than aborting the stream.
			return false
		}
		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" {
				return send(&schema.Message{Role: schema.Assistant, Content: ev.Delta})
			}
		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			if ev.Delta != "" {
				return send(&schema.Message{Role: schema.Assistant, ReasoningContent: ev.Delta})
			}
		case "response.output_item.done":
			return handleOutputItem(ev.Item, &toolIndex, &sawToolCall, &reasoningRaws, send)
		case "response.completed":
			completed = true
			msg := buildCompletedMessage(ev.Response, sawToolCall, reasoningRaws)
			return send(msg)
		case "response.failed", "response.incomplete":
			sw.Send(nil, classifyStreamError(ev.Response, ev.Type))
			return true
		}
		return false
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			sw.Send(nil, ctx.Err())
			return
		default:
		}
		line := scanner.Text()
		if line == "" {
			if dataBuf.Len() > 0 {
				stop := dispatch(dataBuf.String())
				dataBuf.Reset()
				if stop {
					return
				}
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // comment
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			dataBuf.WriteString(strings.TrimPrefix(rest, " "))
		}
		// event: / id: / retry: lines carry no JSON payload we need.
	}
	if dataBuf.Len() > 0 {
		if dispatch(dataBuf.String()) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			sw.Send(nil, ctx.Err())
			return
		}
		sw.Send(nil, fmt.Errorf("openai-codex: read stream: %w", err))
		return
	}
	if !completed {
		sw.Send(nil, fmt.Errorf("openai-codex: stream ended before response.completed"))
	}
}

func handleOutputItem(
	raw json.RawMessage,
	toolIndex *int,
	sawToolCall *bool,
	reasoningRaws *[]json.RawMessage,
	send func(*schema.Message) bool,
) (stop bool) {
	if len(raw) == 0 {
		return false
	}
	var item sseItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return false
	}
	switch item.Type {
	case "function_call":
		idx := *toolIndex
		*toolIndex++
		*sawToolCall = true
		args := item.Arguments
		if args == "" {
			args = "{}"
		}
		return send(&schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				Index: &idx,
				ID:    item.CallID,
				Type:  "function",
				Function: schema.FunctionCall{
					Name:      item.Name,
					Arguments: args,
				},
			}},
		})
	case "reasoning":
		// Preserve the raw item verbatim (id + encrypted_content + summary) so it
		// can be re-threaded into a later turn's input.
		clone := make(json.RawMessage, len(raw))
		copy(clone, raw)
		*reasoningRaws = append(*reasoningRaws, clone)
	}
	return false
}

func buildCompletedMessage(raw json.RawMessage, sawToolCall bool, reasoningRaws []json.RawMessage) *schema.Message {
	msg := &schema.Message{Role: schema.Assistant}
	finish := "stop"
	if sawToolCall {
		finish = "tool_calls"
	}
	meta := &schema.ResponseMeta{FinishReason: finish}
	if len(raw) > 0 {
		var resp sseResponse
		if err := json.Unmarshal(raw, &resp); err == nil && resp.Usage != nil {
			meta.Usage = usageToTokenUsage(resp.Usage)
		}
	}
	msg.ResponseMeta = meta
	if len(reasoningRaws) > 0 {
		msg.Extra = map[string]any{extraKeyReasoningItems: reasoningRaws}
	}
	return msg
}

func usageToTokenUsage(u *sseUsage) *schema.TokenUsage {
	tu := &schema.TokenUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
	}
	if u.InputTokensDetails != nil {
		tu.PromptTokenDetails.CachedTokens = u.InputTokensDetails.CachedTokens
	}
	if u.OutputTokensDetails != nil {
		tu.CompletionTokensDetails.ReasoningTokens = u.OutputTokensDetails.ReasoningTokens
	}
	return tu
}

// classifyStreamError maps a Responses-API failure event to a provider error,
// preserving the codex-auth-go auth sentinels via errors.Is.
func classifyStreamError(raw json.RawMessage, eventType string) error {
	if len(raw) > 0 {
		var resp sseResponse
		if err := json.Unmarshal(raw, &resp); err == nil && resp.Error != nil {
			return classifyByCode(resp.Error.Code, resp.Error.Message)
		}
	}
	return fmt.Errorf("openai-codex: %s event received", eventType)
}

// classifyResponsesError maps a non-2xx HTTP response to a provider error.
func classifyResponsesError(status int, body []byte) error {
	var parsed struct {
		Error *sseError `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error != nil {
		return classifyByCode(parsed.Error.Code, parsed.Error.Message)
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = "no response body"
	}
	return fmt.Errorf("openai-codex: responses request failed (status %d): %s", status, msg)
}

func classifyByCode(code, message string) error {
	if message == "" {
		message = code
	}
	switch code {
	case codexErrCodeUsageNotIncluded:
		return einoproviders.WrapAuthError(fmt.Errorf("%w: %s", codexauth.ErrPlanNotIncluded, message))
	case codexErrCodeInsufficientQuota:
		return einoproviders.WrapAuthError(fmt.Errorf("%w: %s", codexauth.ErrQuotaExceeded, message))
	}
	return fmt.Errorf("openai-codex: responses error (%s): %s", code, message)
}
