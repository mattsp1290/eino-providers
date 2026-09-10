package opencodego

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/cloudwego/eino/schema"
)

const (
	maxResponsesEventBytes = 10 << 20
	maxResponsesLineBytes  = maxResponsesEventBytes + 64
	maxResponsesOutputs    = 1 << 16
)

var (
	errInvalidResponsesEvent  = errors.New("opencode-go: invalid Responses stream event")
	errResponsesEventTooLarge = errors.New("opencode-go: Responses stream event exceeds size limit")
	errResponsesStreamEnded   = errors.New("opencode-go: Responses stream ended before completion")
	errResponsesConsumerGone  = errors.New("opencode-go: Responses stream consumer closed")
)

const (
	responsesEventOutputTextDelta       = "response.output_text.delta"
	responsesEventReasoningTextDelta    = "response.reasoning_text.delta"
	responsesEventReasoningSummaryDelta = "response.reasoning_summary_text.delta"
	responsesEventOutputItemAdded       = "response.output_item.added"
	responsesEventFunctionArgsDelta     = "response.function_call_arguments.delta"
	responsesEventFunctionArgsDone      = "response.function_call_arguments.done"
	responsesEventOutputItemDone        = "response.output_item.done"
	responsesEventCompleted             = "response.completed"
	responsesEventFailed                = "response.failed"
	responsesEventIncomplete            = "response.incomplete"
	responsesEventError                 = "error"
)

type responsesStreamEvent struct {
	Type        string          `json:"type"`
	OutputIndex *int            `json:"output_index"`
	Delta       *string         `json:"delta"`
	Arguments   *string         `json:"arguments"`
	Item        json.RawMessage `json:"item"`
	Response    json.RawMessage `json:"response"`
}

type responsesStreamOutputItem struct {
	ID        string                       `json:"id"`
	Type      string                       `json:"type"`
	Status    string                       `json:"status"`
	Role      string                       `json:"role"`
	Content   []responsesOutputContentItem `json:"content"`
	Name      string                       `json:"name"`
	Arguments string                       `json:"arguments"`
	CallID    string                       `json:"call_id"`
}

type responsesStreamCall struct {
	index     int
	itemID    string
	name      string
	callID    string
	arguments strings.Builder
	doneArgs  *string
	sawDelta  bool
	completed bool
}

type responsesStreamState struct {
	calls       map[int]*responsesStreamCall
	reasoning   map[int]json.RawMessage
	outputs     map[int]string
	finished    map[int]bool
	nextCall    int
	text        map[int]*strings.Builder
	sawToolCall bool
	retained    int
}

func newResponsesStreamState() *responsesStreamState {
	return &responsesStreamState{
		calls: make(map[int]*responsesStreamCall), reasoning: make(map[int]json.RawMessage),
		outputs: make(map[int]string), finished: make(map[int]bool), text: make(map[int]*strings.Builder),
	}
}

func parseResponsesStream(ctx context.Context, body io.Reader, send func(*schema.Message) bool) (*schema.Message, error) {
	reader := bufio.NewReaderSize(body, 64<<10)
	state := newResponsesStreamState()
	var data bytes.Buffer
	dataLines := 0
	for {
		line, err := readResponsesSSELine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				if dataLines > 0 {
					terminal, dispatchErr := dispatchResponsesEvent(data.Bytes(), state, send)
					if dispatchErr != nil || terminal != nil {
						return terminal, dispatchErr
					}
				}
				return nil, errResponsesStreamEnded
			}
			return nil, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if len(line) == 0 {
			if dataLines == 0 {
				continue
			}
			terminal, dispatchErr := dispatchResponsesEvent(data.Bytes(), state, send)
			data.Reset()
			dataLines = 0
			if dispatchErr != nil || terminal != nil {
				return terminal, dispatchErr
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, found := bytes.Cut(line, []byte(":"))
		if !found {
			field, value = line, nil
		}
		if !bytes.Equal(field, []byte("data")) {
			continue
		}
		value = bytes.TrimPrefix(value, []byte(" "))
		separator := 0
		if dataLines > 0 {
			separator = 1
		}
		if data.Len()+separator+len(value) > maxResponsesEventBytes {
			return nil, errResponsesEventTooLarge
		}
		if separator != 0 {
			data.WriteByte('\n')
		}
		_, _ = data.Write(value)
		dataLines++
	}
}

func readResponsesSSELine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		fragment, prefix, err := reader.ReadLine()
		if len(line)+len(fragment) > maxResponsesLineBytes {
			return nil, errResponsesEventTooLarge
		}
		line = append(line, fragment...)
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return line, nil
			}
			return nil, err
		}
		if !prefix {
			return line, nil
		}
	}
}

func dispatchResponsesEvent(data []byte, state *responsesStreamState, send func(*schema.Message) bool) (*schema.Message, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
		return nil, nil
	}
	var event responsesStreamEvent
	if !json.Valid(data) || json.Unmarshal(data, &event) != nil || event.Type == "" {
		return nil, errInvalidResponsesEvent
	}
	switch event.Type {
	case responsesEventOutputTextDelta:
		if event.OutputIndex == nil || event.Delta == nil {
			return nil, errInvalidResponsesEvent
		}
		if state.outputs[*event.OutputIndex] != "message" || state.finished[*event.OutputIndex] {
			return nil, errInvalidResponsesEvent
		}
		if !state.retain(len(*event.Delta)) {
			return nil, errResponsesEventTooLarge
		}
		state.text[*event.OutputIndex].WriteString(*event.Delta)
		if *event.Delta != "" && send(&schema.Message{Role: schema.Assistant, Content: *event.Delta}) {
			return nil, errResponsesConsumerGone
		}
	case responsesEventReasoningTextDelta, responsesEventReasoningSummaryDelta:
		if event.OutputIndex == nil || event.Delta == nil {
			return nil, errInvalidResponsesEvent
		}
		if state.outputs[*event.OutputIndex] != "reasoning" || state.finished[*event.OutputIndex] {
			return nil, errInvalidResponsesEvent
		}
		if *event.Delta != "" && send(&schema.Message{Role: schema.Assistant, ReasoningContent: *event.Delta}) {
			return nil, errResponsesConsumerGone
		}
	case responsesEventOutputItemAdded:
		if event.OutputIndex == nil || len(event.Item) == 0 {
			return nil, errInvalidResponsesEvent
		}
		if err := state.addItem(*event.OutputIndex, event.Item, send); err != nil {
			return nil, err
		}
	case responsesEventFunctionArgsDelta:
		if event.OutputIndex == nil || event.Delta == nil {
			return nil, errInvalidResponsesEvent
		}
		if err := state.addArguments(*event.OutputIndex, *event.Delta, send); err != nil {
			return nil, err
		}
	case responsesEventFunctionArgsDone:
		if event.OutputIndex == nil || event.Arguments == nil {
			return nil, errInvalidResponsesEvent
		}
		if err := state.finishArguments(*event.OutputIndex, *event.Arguments); err != nil {
			return nil, err
		}
	case responsesEventOutputItemDone:
		if event.OutputIndex == nil || len(event.Item) == 0 {
			return nil, errInvalidResponsesEvent
		}
		if err := state.finishItem(*event.OutputIndex, event.Item, send); err != nil {
			return nil, err
		}
	case responsesEventCompleted:
		if len(event.Response) == 0 {
			return nil, errInvalidResponsesEvent
		}
		return state.complete(event.Response, send)
	case responsesEventFailed, responsesEventIncomplete, responsesEventError:
		return nil, responsesAPIFailure(errResponsesNotCompleted)
	default:
		return nil, nil
	}
	return nil, nil
}

func (state *responsesStreamState) addItem(outputIndex int, raw json.RawMessage, send func(*schema.Message) bool) error {
	var item responsesStreamOutputItem
	if json.Unmarshal(raw, &item) != nil || outputIndex < 0 {
		return errInvalidResponsesEvent
	}
	if _, exists := state.outputs[outputIndex]; exists || len(state.outputs) >= maxResponsesOutputs ||
		(item.Type != "function_call" && item.Type != "reasoning" && item.Type != "message") {
		return errInvalidResponsesEvent
	}
	state.outputs[outputIndex] = item.Type
	if item.Type == "message" {
		if item.Role != "assistant" || item.Content == nil {
			return errInvalidResponsesEvent
		}
		state.text[outputIndex] = new(strings.Builder)
	}
	if item.Type != "function_call" {
		return nil
	}
	if _, exists := state.calls[outputIndex]; exists || strings.TrimSpace(item.Name) == "" || item.Arguments != "" {
		return errInvalidResponsesEvent
	}
	call := &responsesStreamCall{index: state.nextCall, itemID: item.ID, name: item.Name, callID: item.CallID}
	state.nextCall++
	state.sawToolCall = true
	state.calls[outputIndex] = call
	if send(responsesToolCallChunk(call, "")) {
		return errResponsesConsumerGone
	}
	return nil
}

func (state *responsesStreamState) addArguments(outputIndex int, delta string, send func(*schema.Message) bool) error {
	call, exists := state.calls[outputIndex]
	if !exists || call.completed || call.doneArgs != nil {
		return errInvalidResponsesEvent
	}
	if !state.retain(len(delta)) {
		return errResponsesEventTooLarge
	}
	if delta == "" {
		return nil
	}
	call.sawDelta = true
	call.arguments.WriteString(delta)
	if delta != "" && send(responsesToolCallChunk(call, delta)) {
		return errResponsesConsumerGone
	}
	return nil
}

func (state *responsesStreamState) finishArguments(outputIndex int, arguments string) error {
	call, exists := state.calls[outputIndex]
	if !exists || call.completed || call.doneArgs != nil || !json.Valid([]byte(arguments)) || (call.sawDelta && call.arguments.String() != arguments) {
		return errInvalidResponsesEvent
	}
	copy := arguments
	call.doneArgs = &copy
	return nil
}

func (state *responsesStreamState) finishItem(outputIndex int, raw json.RawMessage, send func(*schema.Message) bool) error {
	var item responsesStreamOutputItem
	if json.Unmarshal(raw, &item) != nil || outputIndex < 0 || state.finished[outputIndex] || (item.Status != "" && item.Status != "completed") {
		return errInvalidResponsesEvent
	}
	if expected, exists := state.outputs[outputIndex]; exists && expected != item.Type {
		return errInvalidResponsesEvent
	} else if !exists {
		if len(state.outputs) >= maxResponsesOutputs {
			return errResponsesEventTooLarge
		}
		state.outputs[outputIndex] = item.Type
	}
	switch item.Type {
	case "function_call":
		if item.CallID == "" || strings.TrimSpace(item.Name) == "" || !json.Valid([]byte(item.Arguments)) {
			return errInvalidResponsesEvent
		}
		call, exists := state.calls[outputIndex]
		if !exists {
			call = &responsesStreamCall{index: state.nextCall, itemID: item.ID, name: item.Name, callID: item.CallID, completed: true}
			state.nextCall++
			state.sawToolCall = true
			if !state.retain(len(item.Arguments)) {
				return errResponsesEventTooLarge
			}
			call.arguments.WriteString(item.Arguments)
			state.calls[outputIndex] = call
			state.finished[outputIndex] = true
			if send(responsesToolCallChunk(call, item.Arguments)) {
				return errResponsesConsumerGone
			}
			return nil
		}
		if call.completed || call.name != item.Name || (call.itemID != "" && item.ID != "" && call.itemID != item.ID) ||
			(call.callID != "" && call.callID != item.CallID) || (call.doneArgs != nil && *call.doneArgs != item.Arguments) {
			return errInvalidResponsesEvent
		}
		needsBackfill := call.callID == ""
		if needsBackfill {
			call.callID = item.CallID
		}
		if call.itemID == "" {
			call.itemID = item.ID
		}
		if call.sawDelta {
			if call.arguments.String() != item.Arguments {
				return errInvalidResponsesEvent
			}
			if needsBackfill && send(responsesToolCallChunk(call, "")) {
				return errResponsesConsumerGone
			}
		} else {
			if !state.retain(len(item.Arguments)) {
				return errResponsesEventTooLarge
			}
			call.arguments.WriteString(item.Arguments)
			if send(responsesToolCallChunk(call, item.Arguments)) {
				return errResponsesConsumerGone
			}
		}
		call.completed = true
	case "reasoning":
		if _, exists := state.reasoning[outputIndex]; exists || validateResponsesReasoningItem(raw) != nil {
			return errInvalidResponsesEvent
		}
		if !state.retain(len(raw)) {
			return errResponsesEventTooLarge
		}
		state.reasoning[outputIndex] = append(json.RawMessage(nil), raw...)
	case "message":
		if item.Role != "assistant" || item.Content == nil {
			return errInvalidResponsesEvent
		}
		for _, content := range item.Content {
			if content.Type != "output_text" || content.Text == nil {
				return errInvalidResponsesEvent
			}
		}
	default:
		return errInvalidResponsesEvent
	}
	state.finished[outputIndex] = true
	return nil
}

func (state *responsesStreamState) complete(raw json.RawMessage, send func(*schema.Message) bool) (*schema.Message, error) {
	var response responsesResponse
	if !json.Valid(raw) || json.Unmarshal(raw, &response) != nil || response.Status != "completed" || response.Output == nil ||
		(len(response.Error) != 0 && string(bytes.TrimSpace(response.Error)) != "null") || validateResponsesUsage(response.Usage) != nil {
		return nil, responsesAPIFailure(errInvalidResponsesResponse)
	}
	seenCalls := make(map[int]struct{})
	seenReasoning := make(map[int]struct{})
	for outputIndex, rawItem := range response.Output {
		var item responsesStreamOutputItem
		if json.Unmarshal(rawItem, &item) != nil || (item.Status != "" && item.Status != "completed") {
			return nil, responsesAPIFailure(errInvalidResponsesResponse)
		}
		if expected, exists := state.outputs[outputIndex]; exists && expected != item.Type {
			return nil, responsesAPIFailure(errInvalidResponsesResponse)
		}
		switch item.Type {
		case "message":
			if item.Role != "assistant" || item.Content == nil {
				return nil, responsesAPIFailure(errInvalidResponsesResponse)
			}
			var terminalText strings.Builder
			for _, content := range item.Content {
				if content.Type != "output_text" || content.Text == nil {
					return nil, responsesAPIFailure(errInvalidResponsesResponse)
				}
				terminalText.WriteString(*content.Text)
			}
			emitted := ""
			if builder := state.text[outputIndex]; builder != nil {
				emitted = builder.String()
			}
			if !strings.HasPrefix(terminalText.String(), emitted) {
				return nil, responsesAPIFailure(errInvalidResponsesResponse)
			}
			if suffix := strings.TrimPrefix(terminalText.String(), emitted); suffix != "" && send(&schema.Message{Role: schema.Assistant, Content: suffix}) {
				return nil, errResponsesConsumerGone
			}
		case "function_call":
			if err := state.reconcileCall(outputIndex, rawItem, send); err != nil {
				return nil, err
			}
			seenCalls[outputIndex] = struct{}{}
		case "reasoning":
			if validateResponsesReasoningItem(rawItem) != nil {
				return nil, responsesAPIFailure(errInvalidResponsesResponse)
			}
			if prior, exists := state.reasoning[outputIndex]; exists {
				if !responsesJSONEqual(prior, rawItem) {
					return nil, responsesAPIFailure(errInvalidResponsesResponse)
				}
			} else {
				if !state.retain(len(rawItem)) {
					return nil, responsesAPIFailure(errResponsesEventTooLarge)
				}
				state.reasoning[outputIndex] = append(json.RawMessage(nil), rawItem...)
			}
			seenReasoning[outputIndex] = struct{}{}
		default:
			return nil, responsesAPIFailure(errInvalidResponsesResponse)
		}
	}
	for outputIndex, call := range state.calls {
		if _, exists := seenCalls[outputIndex]; !exists || !call.completed {
			return nil, responsesAPIFailure(errInvalidResponsesResponse)
		}
	}
	for outputIndex := range state.reasoning {
		if _, exists := seenReasoning[outputIndex]; !exists {
			return nil, responsesAPIFailure(errInvalidResponsesResponse)
		}
	}
	for outputIndex := range state.outputs {
		if outputIndex >= len(response.Output) {
			return nil, responsesAPIFailure(errInvalidResponsesResponse)
		}
	}
	finish := "stop"
	if state.sawToolCall {
		finish = "tool_calls"
	}
	terminal := &schema.Message{Role: schema.Assistant, ResponseMeta: &schema.ResponseMeta{FinishReason: finish, Usage: responsesTokenUsage(response.Usage)}}
	if len(state.reasoning) > 0 {
		items := make([]json.RawMessage, 0, len(state.reasoning))
		for outputIndex := range response.Output {
			if reasoning, exists := state.reasoning[outputIndex]; exists {
				items = append(items, append(json.RawMessage(nil), reasoning...))
			}
		}
		terminal.Extra = map[string]any{responsesReasoningItemsKey: items}
	}
	return terminal, nil
}

func (state *responsesStreamState) reconcileCall(outputIndex int, raw json.RawMessage, send func(*schema.Message) bool) error {
	var item responsesStreamOutputItem
	if json.Unmarshal(raw, &item) != nil || item.CallID == "" || strings.TrimSpace(item.Name) == "" || !json.Valid([]byte(item.Arguments)) {
		return responsesAPIFailure(errInvalidResponsesResponse)
	}
	call, exists := state.calls[outputIndex]
	if !exists {
		call = &responsesStreamCall{index: state.nextCall, itemID: item.ID, name: item.Name, callID: item.CallID, completed: true}
		state.nextCall++
		state.sawToolCall = true
		if !state.retain(len(item.Arguments)) {
			return responsesAPIFailure(errResponsesEventTooLarge)
		}
		call.arguments.WriteString(item.Arguments)
		state.calls[outputIndex] = call
		if send(responsesToolCallChunk(call, item.Arguments)) {
			return errResponsesConsumerGone
		}
		return nil
	}
	if call.name != item.Name || (call.itemID != "" && item.ID != "" && call.itemID != item.ID) ||
		(call.callID != "" && call.callID != item.CallID) || (call.doneArgs != nil && *call.doneArgs != item.Arguments) {
		return responsesAPIFailure(errInvalidResponsesResponse)
	}
	needsBackfill := call.callID == ""
	if needsBackfill {
		call.callID = item.CallID
	}
	if call.sawDelta {
		if call.arguments.String() != item.Arguments {
			return responsesAPIFailure(errInvalidResponsesResponse)
		}
		if !call.completed && needsBackfill && send(responsesToolCallChunk(call, "")) {
			return errResponsesConsumerGone
		}
	} else if !call.completed {
		if !state.retain(len(item.Arguments)) {
			return responsesAPIFailure(errResponsesEventTooLarge)
		}
		call.arguments.WriteString(item.Arguments)
		if send(responsesToolCallChunk(call, item.Arguments)) {
			return errResponsesConsumerGone
		}
	} else if call.arguments.String() != item.Arguments {
		return responsesAPIFailure(errInvalidResponsesResponse)
	}
	call.completed = true
	return nil
}

func (state *responsesStreamState) retain(size int) bool {
	if size < 0 || state.retained > maxResponsesEventBytes-size {
		return false
	}
	state.retained += size
	return true
}

func responsesToolCallChunk(call *responsesStreamCall, arguments string) *schema.Message {
	index := call.index
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
		Index: &index, ID: call.callID, Type: "function",
		Function: schema.FunctionCall{Name: call.name, Arguments: arguments},
	}}}
}

func responsesJSONEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil &&
		jsonValuesEqual(leftValue, rightValue)
}

func jsonValuesEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
