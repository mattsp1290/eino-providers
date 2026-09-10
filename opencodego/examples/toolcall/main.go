// Command toolcall demonstrates a bounded, two-turn OpenCode Go function-tool
// exchange. The host explicitly supplies the model, protocol, identity, session,
// and credential through environment variables.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-providers/opencodego"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "opencode-go tool example failed:", err)
		os.Exit(1)
	}
}

func run() error {
	modelID, err := requiredEnv("OPENCODE_GO_MODEL")
	if err != nil {
		return err
	}
	protocol, err := parseProtocol(os.Getenv("OPENCODE_GO_PROTOCOL"))
	if err != nil {
		return err
	}
	userAgent, err := requiredEnv("OPENCODE_GO_USER_AGENT")
	if err != nil {
		return err
	}
	sessionID, err := requiredEnv("OPENCODE_GO_SESSION_ID")
	if err != nil {
		return err
	}
	apiKey, err := requiredEnv("OPENCODE_GO_API_KEY")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	maxTokens := 512
	chatModel, err := opencodego.NewChatModel(ctx, opencodego.ChatModelConfig{
		Model: modelID, Protocol: protocol, APIKey: apiKey, UserAgent: userAgent,
		SessionID: sessionID, MaxTokens: &maxTokens,
	})
	if err != nil {
		return fmt.Errorf("construct chat model: %w", err)
	}

	lookupTool := &schema.ToolInfo{
		Name: "lookup_symbol",
		Desc: "Look up the definition of a symbol in a synthetic code index.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"symbol": {Type: schema.String, Desc: "Exact symbol name", Required: true},
		}),
	}
	bound, err := chatModel.WithTools([]*schema.ToolInfo{lookupTool})
	if err != nil {
		return fmt.Errorf("bind tool: %w", err)
	}

	history := []*schema.Message{
		schema.SystemMessage("Use lookup_symbol exactly once before answering."),
		schema.UserMessage("In the synthetic repository, explain what ParseConfig returns. Look up ParseConfig first."),
	}
	first, err := streamTurn(ctx, bound, history, model.WithToolChoice(schema.ToolChoiceForced))
	if err != nil {
		return fmt.Errorf("first turn: %w", err)
	}
	if len(first.ToolCalls) != 1 {
		return fmt.Errorf("first turn returned %d tool calls; want 1", len(first.ToolCalls))
	}
	call := first.ToolCalls[0]
	if call.ID == "" || call.Function.Name != lookupTool.Name {
		return errors.New("first turn returned an invalid tool call")
	}
	var arguments struct {
		Symbol string `json:"symbol"`
	}
	if json.Unmarshal([]byte(call.Function.Arguments), &arguments) != nil || arguments.Symbol != "ParseConfig" {
		return errors.New("first turn returned invalid tool arguments")
	}

	result := `{"symbol":"ParseConfig","definition":"validates input and returns a Config value or an error"}`
	history = append(history, first, schema.ToolMessage(result, call.ID))
	final, err := streamTurn(ctx, bound, history)
	if err != nil {
		return fmt.Errorf("second turn: %w", err)
	}
	if final.Content == "" {
		return errors.New("second turn returned no text")
	}
	fmt.Println(final.Content)
	return nil
}

func streamTurn(ctx context.Context, chatModel model.ToolCallingChatModel, history []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	stream, err := chatModel.Stream(ctx, history, opts...)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	var chunks []*schema.Message
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return nil, recvErr
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, errors.New("empty stream")
	}
	return schema.ConcatMessages(chunks)
}

func requiredEnv(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func parseProtocol(value string) (opencodego.Protocol, error) {
	switch value {
	case string(opencodego.ProtocolChatCompletions):
		return opencodego.ProtocolChatCompletions, nil
	case string(opencodego.ProtocolMessages):
		return opencodego.ProtocolMessages, nil
	case string(opencodego.ProtocolResponses):
		return opencodego.ProtocolResponses, nil
	default:
		return "", errors.New("OPENCODE_GO_PROTOCOL must be chat-completions, messages, or responses")
	}
}
