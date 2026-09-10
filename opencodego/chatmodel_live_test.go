package opencodego_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-providers/opencodego"
)

func TestLiveOpenCodeGo(t *testing.T) {
	if os.Getenv("OPENCODE_GO_LIVE_TEST") != "1" {
		t.Skip("set OPENCODE_GO_LIVE_TEST=1 to enable paid live inference")
	}

	apiKey := requireLiveEnv(t, "OPENCODE_GO_API_KEY")
	userAgent := requireLiveEnv(t, "OPENCODE_GO_USER_AGENT")
	cases := []struct {
		protocol opencodego.Protocol
		model    string
	}{
		{opencodego.ProtocolChatCompletions, requireLiveEnv(t, "OPENCODE_GO_CHAT_MODEL")},
		{opencodego.ProtocolMessages, requireLiveEnv(t, "OPENCODE_GO_MESSAGES_MODEL")},
		{opencodego.ProtocolResponses, requireLiveEnv(t, "OPENCODE_GO_RESPONSES_MODEL")},
	}

	for _, tc := range cases {
		t.Run(string(tc.protocol), func(t *testing.T) {
			runLiveProtocol(t, tc.protocol, tc.model, apiKey, userAgent)
		})
	}
}

func runLiveProtocol(t *testing.T, protocol opencodego.Protocol, modelID, apiKey, userAgent string) {
	t.Helper()
	maxTokens := 256
	chatModel, err := opencodego.NewChatModel(context.Background(), opencodego.ChatModelConfig{
		Model: modelID, Protocol: protocol, APIKey: apiKey, UserAgent: userAgent,
		SessionID: liveSessionID(t), MaxTokens: &maxTokens,
		HTTPClient: &http.Client{Timeout: 50 * time.Second},
	})
	if err != nil {
		t.Fatal("live chat model construction failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	generated, err := chatModel.Generate(ctx, []*schema.Message{
		schema.UserMessage("Reply with exactly: synthetic pass"),
	})
	cancel()
	if err != nil || generated == nil || generated.Content == "" {
		t.Fatal("live Generate validation failed")
	}

	tool := &schema.ToolInfo{
		Name: "lookup_symbol",
		Desc: "Look up a symbol in a synthetic code index.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"symbol": {Type: schema.String, Desc: "Exact symbol name", Required: true},
		}),
	}
	bound, err := chatModel.WithTools([]*schema.ToolInfo{tool})
	if err != nil {
		t.Fatal("live tool binding failed")
	}
	history := []*schema.Message{
		schema.SystemMessage("Call lookup_symbol exactly once before answering."),
		schema.UserMessage("In a synthetic repository, explain ParseConfig. Look it up first."),
	}
	first := receiveLiveTurn(t, bound, history, model.WithToolChoice(schema.ToolChoiceForced))
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].ID == "" || first.ToolCalls[0].Function.Name != tool.Name {
		t.Fatal("live first turn returned an invalid tool call")
	}
	call := first.ToolCalls[0]
	var arguments struct {
		Symbol string `json:"symbol"`
	}
	if json.Unmarshal([]byte(call.Function.Arguments), &arguments) != nil || arguments.Symbol != "ParseConfig" {
		t.Fatal("live first turn returned invalid tool arguments")
	}
	history = append(history, first, schema.ToolMessage(
		`{"symbol":"ParseConfig","definition":"returns a Config value or an error"}`, call.ID,
	))
	final := receiveLiveTurn(t, bound, history)
	if final.Content == "" {
		t.Fatal("live final turn returned no text")
	}
}

func receiveLiveTurn(t *testing.T, chatModel model.ToolCallingChatModel, history []*schema.Message, opts ...model.Option) *schema.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	stream, err := chatModel.Stream(ctx, history, opts...)
	if err != nil {
		t.Fatal("live stream start failed")
	}
	defer stream.Close()

	var chunks []*schema.Message
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatal("live stream receive failed")
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		t.Fatal("live stream returned no chunks")
	}
	message, err := schema.ConcatMessages(chunks)
	if err != nil {
		t.Fatal("live stream assembly failed")
	}
	return message
}

func requireLiveEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required when OPENCODE_GO_LIVE_TEST=1", name)
	}
	return value
}

func liveSessionID(t *testing.T) string {
	t.Helper()
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal("ephemeral live session creation failed")
	}
	return "eino-live-" + hex.EncodeToString(random[:])
}
