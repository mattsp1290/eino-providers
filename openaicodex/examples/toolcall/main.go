// Command toolcall demonstrates a 2-turn tool-calling exchange against the
// OpenAI-Codex subscription backend using only the public eino-providers surface.
//
// Turn 1: bind a tool, Stream, and observe the model emit a tool call.
// Turn 2: feed back a role=tool result and Stream again, printing the final
// answer token-by-token.
//
// It requires a logged-in codex-auth-go session for the configured app name.
// See README.md in this directory for login instructions and the tested model.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	codexauth "github.com/mattsp1290/codex-auth-go"

	"github.com/mattsp1290/eino-providers/openaicodex"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	appName := envOr("CODEX_APP_NAME", "ag-ui-go-server-example")
	modelID := envOr("CODEX_MODEL", "gpt-5.5")

	cm, err := openaicodex.NewChatModel(ctx, openaicodex.ChatModelConfig{
		AppName: appName,
		Model:   modelID,
	})
	if err != nil {
		if errors.Is(err, codexauth.ErrNotLoggedIn) {
			return fmt.Errorf("not logged in for app %q: run a codex-auth-go login first (see README.md): %w", appName, err)
		}
		return err
	}

	weatherTool := &schema.ToolInfo{
		Name: "get_weather",
		Desc: "Get the current weather for a city.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"city": {Type: schema.String, Desc: "City name, e.g. 'San Francisco'", Required: true},
		}),
	}

	bound, err := cm.WithTools([]*schema.ToolInfo{weatherTool})
	if err != nil {
		return fmt.Errorf("bind tools: %w", err)
	}

	history := []*schema.Message{
		schema.SystemMessage("You are a helpful assistant. Use tools when they help answer."),
		schema.UserMessage("What's the weather in San Francisco? Use the get_weather tool."),
	}

	// --- Turn 1: expect a tool call ---
	fmt.Println("=== Turn 1: streaming (expecting a tool call) ===")
	turn1, err := streamTurn(ctx, bound, history)
	if err != nil {
		return fmt.Errorf("turn 1: %w", err)
	}
	if len(turn1.ToolCalls) == 0 {
		return fmt.Errorf("turn 1 produced no tool call; got content %q", turn1.Content)
	}
	call := turn1.ToolCalls[0]
	fmt.Printf("\n-> tool call: id=%s name=%s args=%s\n", call.ID, call.Function.Name, call.Function.Arguments)

	// Execute the tool (stubbed result for the example).
	result := `{"city":"San Francisco","temp_f":63,"conditions":"foggy"}`
	fmt.Printf("-> tool result: %s\n", result)

	// Append the full assistant message (preserving Extra so any reasoning items
	// are threaded back) and the tool result.
	history = append(history, turn1, schema.ToolMessage(result, call.ID))

	// --- Turn 2: final answer, streamed token-by-token ---
	fmt.Println("\n=== Turn 2: streaming final answer ===")
	turn2, err := streamTurn(ctx, bound, history)
	if err != nil {
		return fmt.Errorf("turn 2: %w", err)
	}
	fmt.Printf("\n\n=== done ===\nfinal answer length: %d chars\n", len(turn2.Content))
	if turn2.ResponseMeta != nil && turn2.ResponseMeta.Usage != nil {
		u := turn2.ResponseMeta.Usage
		fmt.Printf("usage: prompt=%d completion=%d total=%d\n", u.PromptTokens, u.CompletionTokens, u.TotalTokens)
	}
	return nil
}

// streamTurn streams one turn, printing assistant text deltas as they arrive,
// and returns the merged assistant message.
func streamTurn(ctx context.Context, cm model.ToolCallingChatModel, msgs []*schema.Message) (*schema.Message, error) {
	sr, err := cm.Stream(ctx, msgs)
	if err != nil {
		return nil, err
	}
	defer sr.Close()

	var chunks []*schema.Message
	for {
		chunk, recvErr := sr.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return nil, recvErr
		}
		if chunk.Content != "" {
			fmt.Print(chunk.Content) // token-by-token
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("empty stream")
	}
	return schema.ConcatMessages(chunks)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
