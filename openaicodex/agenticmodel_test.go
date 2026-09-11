package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	codexauth "github.com/mattsp1290/codex-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestAgenticModelUsesResponsesSSE(t *testing.T) {
	stub := stubRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != codexauth.CodexEndpoint {
			t.Errorf("URL = %q", r.URL.String())
		}
		var request responsesRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(request.Input) != 1 {
			t.Errorf("input = %#v", request.Input)
		}
		return sseOKResponse(`{"type":"response.output_text.delta","delta":"hello"}`, `{"type":"response.completed","response":{"id":"resp_native","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`), nil
	})
	m, err := NewAgenticModelWithHTTPClient(context.Background(), &http.Client{Transport: stub}, AgenticModelConfig{Model: "gpt-codex"})
	if err != nil {
		t.Fatal(err)
	}
	input := []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.UserInputText{Text: "prompt"})}}}
	stream, err := m.Stream(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var chunks []*schema.AgenticMessage
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, chunk)
	}
	merged, err := schema.ConcatAgenticMessages(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.ContentBlocks[0].AssistantGenText.Text; got != "hello" {
		t.Fatalf("text=%q", got)
	}
	identity := merged.ResponseMeta.Extension.(einoproviders.AgenticResponseIdentity)
	if identity.CorrelationID != "resp_native" {
		t.Fatalf("identity=%#v", identity)
	}
}

func TestAgenticModelRejectsUnsupportedOptionBeforeDispatch(t *testing.T) {
	m := &agenticModel{model: "m", httpClient: &http.Client{}, limits: einoproviders.AgenticLimits{}.WithDefaults()}
	_, err := m.Stream(context.Background(), nil, model.WithDeferredTools([]*schema.ToolInfo{{Name: "x"}}))
	if !errors.Is(err, einoproviders.ErrUnsupportedCapability) {
		t.Fatalf("error=%v", err)
	}
}
