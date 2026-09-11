package openaicodex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// streamAgenticResponses parses native Responses SSE directly into
// AgenticMessage chunks. It intentionally keeps content and metadata separate
// so schema.ConcatAgenticMessages is the only final aggregation point.
func streamAgenticResponses(ctx context.Context, body io.Reader, writer *schema.StreamWriter[*schema.AgenticMessage], requestedModel string, limits einoproviders.AgenticLimits) {
	scanner := bufio.NewScanner(io.LimitReader(body, limits.MaxResponseBytes+1))
	scanner.Buffer(make([]byte, 64<<10), int(limits.MaxEventBytes))
	var data strings.Builder
	completed := false
	dispatch := func(raw string) bool {
		raw = strings.TrimSpace(raw)
		if raw == "" || raw == "[DONE]" {
			return false
		}
		var event sseEvent
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			writer.Send(nil, fmt.Errorf("openai-codex: decode agentic stream: %w", err))
			return true
		}
		switch event.Type {
		case evOutputTextDelta:
			if event.Delta != "" {
				return writer.Send(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(&schema.AssistantGenText{Text: event.Delta}, &schema.StreamingMeta{Index: 0})}}, nil)
			}
		case evReasoningTextDelta, evReasoningSummaryDelta:
			if event.Delta != "" {
				return writer.Send(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(&schema.Reasoning{Text: event.Delta}, &schema.StreamingMeta{Index: 1})}}, nil)
			}
		case evOutputItemDone:
			var item sseItem
			if json.Unmarshal(event.Item, &item) == nil && item.Type == itemTypeFunctionCall {
				args := item.Arguments
				if args == "" {
					args = "{}"
				}
				return writer.Send(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(&schema.FunctionToolCall{CallID: item.CallID, Name: item.Name, Arguments: args}, &schema.StreamingMeta{Index: event.OutputIndex + 2})}}, nil)
			}
		case evCompleted:
			completed = true
			var response sseResponse
			_ = json.Unmarshal(event.Response, &response)
			meta := &schema.AgenticResponseMeta{Extension: einoproviders.AgenticResponseIdentity{Provider: "openai-codex", Protocol: "responses", RequestedModel: requestedModel, CorrelationID: response.ID}}
			if response.Usage != nil {
				meta.TokenUsage = usageToTokenUsage(response.Usage)
			}
			return writer.Send(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ResponseMeta: meta}, nil)
		case evFailed, evIncomplete:
			writer.Send(nil, classifyStreamError(event.Response, event.Type))
			return true
		}
		return false
	}
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			writer.Send(nil, ctx.Err())
			return
		default:
		}
		line := scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				if dispatch(data.String()) {
					return
				}
				data.Reset()
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimSpace(rest))
		}
	}
	if data.Len() > 0 && dispatch(data.String()) {
		return
	}
	if err := scanner.Err(); err != nil {
		writer.Send(nil, &einoproviders.ResourceLimitError{Resource: "response_bytes", Limit: limits.MaxResponseBytes})
		return
	}
	if !completed {
		writer.Send(nil, fmt.Errorf("openai-codex: stream ended before response.completed"))
	}
}
