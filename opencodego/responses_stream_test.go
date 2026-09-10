package opencodego

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestResponsesStreamAssemblesTextReasoningAndParallelTools(t *testing.T) {
	body := responsesSSE(
		`{"type":"future.event","value":1}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_1","type":"reasoning"}}`,
		`{"type":"response.reasoning_text.delta","output_index":0,"delta":"think"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_1","type":"reasoning","status":"completed","encrypted_content":"opaque","summary":[]}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"hello "}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"world"}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello world"}]}}`,
		`{"type":"response.output_item.added","output_index":2,"item":{"id":"fc_1","type":"function_call","name":"weather","arguments":"","call_id":"call_1"}}`,
		`{"type":"response.output_item.added","output_index":3,"item":{"id":"fc_2","type":"function_call","name":"clock","arguments":"","call_id":"call_2"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"city\":"}`,
		`{"type":"response.function_call_arguments.delta","output_index":3,"delta":"{"}`,
		`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"\"NYC\"}"}`,
		`{"type":"response.function_call_arguments.delta","output_index":3,"delta":"}"}`,
		`{"type":"response.function_call_arguments.done","output_index":2,"arguments":"{\"city\":\"NYC\"}"}`,
		`{"type":"response.output_item.done","output_index":2,"item":{"id":"fc_1","type":"function_call","status":"completed","name":"weather","arguments":"{\"city\":\"NYC\"}","call_id":"call_1"}}`,
		`{"type":"response.output_item.done","output_index":3,"item":{"id":"fc_2","type":"function_call","status":"completed","name":"clock","arguments":"{}","call_id":"call_2"}}`,
		`{"type":"response.completed","response":{"status":"completed","output":[{"id":"rs_1","type":"reasoning","status":"completed","encrypted_content":"opaque","summary":[]},{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello world"}]},{"id":"fc_1","type":"function_call","status":"completed","name":"weather","arguments":"{\"city\":\"NYC\"}","call_id":"call_1"},{"id":"fc_2","type":"function_call","status":"completed","name":"clock","arguments":"{}","call_id":"call_2"}],"usage":{"input_tokens":8,"output_tokens":5,"total_tokens":13,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":1}},"error":null}}`,
	)
	message := parseAndConcatResponses(t, body)
	if message.Content != "hello world" || message.ReasoningContent != "think" || len(message.ToolCalls) != 2 {
		t.Fatalf("message = %#v", message)
	}
	if message.ToolCalls[0].Function.Arguments != `{"city":"NYC"}` || message.ToolCalls[1].Function.Arguments != `{}` ||
		message.ToolCalls[0].Index == nil || *message.ToolCalls[0].Index != 0 || message.ToolCalls[1].Index == nil || *message.ToolCalls[1].Index != 1 {
		t.Fatalf("tool calls = %#v", message.ToolCalls)
	}
	if message.ResponseMeta == nil || message.ResponseMeta.FinishReason != "tool_calls" || message.ResponseMeta.Usage == nil || message.ResponseMeta.Usage.TotalTokens != 13 {
		t.Fatalf("response metadata = %#v", message.ResponseMeta)
	}
	reasoning, ok := message.Extra[responsesReasoningItemsKey].([]json.RawMessage)
	if !ok || len(reasoning) != 1 || !bytes.Contains(reasoning[0], []byte("opaque")) {
		t.Fatalf("reasoning items = %#v", message.Extra)
	}
}

func TestResponsesStreamSupportsCRLFMultilineAndTerminalReconciliation(t *testing.T) {
	terminal := `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello"}]},{"type":"function_call","status":"completed","name":"tool","arguments":"{\"x\":1}","call_id":"call"}]}}`
	cut := strings.Index(terminal, `,"response"`)
	body := ": heartbeat\r\n\r\n" + responsesSSE(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"hel"}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","status":"completed","name":"tool","arguments":"{\"x\":1}","call_id":"call"}}`,
	)
	body = strings.ReplaceAll(body, "\n", "\r\n") + "data: " + terminal[:cut+1] + "\r\ndata: " + terminal[cut+1:] + "\r\n\r\n"
	message := parseAndConcatResponses(t, body)
	if message.Content != "hello" || len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Arguments != `{"x":1}` {
		t.Fatalf("message = %#v", message)
	}
}

func TestResponsesStreamRejectsMalformedAndIncompleteEvents(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "malformed json", body: "data: {\n\n", want: errInvalidResponsesEvent},
		{name: "missing type", body: responsesSSE(`{"delta":"x"}`), want: errInvalidResponsesEvent},
		{name: "missing delta", body: responsesSSE(`{"type":"response.output_text.delta","output_index":0}`), want: errInvalidResponsesEvent},
		{name: "negative output index", body: responsesSSE(`{"type":"response.output_item.added","output_index":-1,"item":{"type":"message"}}`), want: errInvalidResponsesEvent},
		{name: "unknown call delta", body: responsesSSE(`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{}"}`), want: errInvalidResponsesEvent},
		{name: "duplicate output", body: responsesSSE(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`, `{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`), want: errInvalidResponsesEvent},
		{name: "mismatched call", body: responsesSSE(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","name":"a","arguments":"","call_id":"call"}}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","name":"b","arguments":"{}","call_id":"call"}}`), want: errInvalidResponsesEvent},
		{name: "argument disagreement", body: responsesSSE(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","name":"a","arguments":"","call_id":"call"}}`, `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{}"}`, `{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"x\":1}"}`), want: errInvalidResponsesEvent},
		{name: "failed", body: responsesSSE(`{"type":"response.failed","response":{"error":{"message":"secret"}}}`), want: einoproviders.ErrProviderAPI},
		{name: "incomplete", body: responsesSSE(`{"type":"response.incomplete","response":{}}`), want: einoproviders.ErrProviderAPI},
		{name: "native error", body: responsesSSE(`{"type":"error","message":"secret"}`), want: einoproviders.ErrProviderAPI},
		{name: "done only", body: responsesSSE(`[DONE]`), want: errResponsesStreamEnded},
		{name: "premature eof", body: "", want: errResponsesStreamEnded},
		{name: "bad terminal status", body: responsesSSE(`{"type":"response.completed","response":{"status":"failed","output":[]}}`), want: einoproviders.ErrProviderAPI},
		{name: "missing terminal output", body: responsesSSE(`{"type":"response.completed","response":{"status":"completed"}}`), want: einoproviders.ErrProviderAPI},
		{name: "terminal text mismatch", body: responsesSSE(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","content":[]}}`, `{"type":"response.output_text.delta","output_index":0,"delta":"a"}`, `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"b"}]}]}}`), want: einoproviders.ErrProviderAPI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseResponsesForTest(tt.body)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaked event: %v", err)
			}
		})
	}
}

func TestResponsesStreamBoundsSingleAndMultilineEvents(t *testing.T) {
	prefix := `{"type":"future.event"}`
	exactPayload := prefix + strings.Repeat(" ", maxResponsesEventBytes-len(prefix))
	_, _, err := parseResponsesForTest("data: " + exactPayload + "\n\n")
	if !errors.Is(err, errResponsesStreamEnded) {
		t.Fatalf("exact limit error = %v", err)
	}
	_, _, err = parseResponsesForTest("data: " + exactPayload + " \n\n")
	if !errors.Is(err, errResponsesEventTooLarge) {
		t.Fatalf("over limit error = %v", err)
	}
	first := maxResponsesEventBytes / 2
	second := maxResponsesEventBytes - first
	multiline := "data: " + prefix + strings.Repeat(" ", first-len(prefix)) + "\ndata: " + strings.Repeat(" ", second) + "\n\n"
	_, _, err = parseResponsesForTest(multiline)
	if !errors.Is(err, errResponsesEventTooLarge) {
		t.Fatalf("multiline aggregate error = %v", err)
	}
}

func TestResponsesStreamHonorsContextAndConsumerClosure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := parseResponsesStream(ctx, strings.NewReader("\n"), func(*schema.Message) bool { return false })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v", err)
	}
	body := responsesSSE(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"x"}`,
	)
	_, err = parseResponsesStream(context.Background(), strings.NewReader(body), func(*schema.Message) bool { return true })
	if !errors.Is(err, errResponsesConsumerGone) {
		t.Fatalf("consumer error = %v", err)
	}
}

func parseAndConcatResponses(t *testing.T, body string) *schema.Message {
	t.Helper()
	terminal, chunks, err := parseResponsesForTest(body)
	if err != nil {
		t.Fatal(err)
	}
	chunks = append(chunks, terminal)
	message, err := schema.ConcatMessages(chunks)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func parseResponsesForTest(body string) (*schema.Message, []*schema.Message, error) {
	var chunks []*schema.Message
	terminal, err := parseResponsesStream(context.Background(), strings.NewReader(body), func(message *schema.Message) bool {
		chunks = append(chunks, message)
		return false
	})
	return terminal, chunks, err
}

func responsesSSE(events ...string) string {
	var body strings.Builder
	for _, event := range events {
		body.WriteString("data: ")
		body.WriteString(event)
		body.WriteString("\n\n")
	}
	return body.String()
}
