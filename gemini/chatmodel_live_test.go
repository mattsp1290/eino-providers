//go:build live

package gemini

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestNewChatModel_Live_Generate(t *testing.T) {
	key := os.Getenv("GOOGLE_API_KEY")
	if key == "" {
		key = os.Getenv("GEMINI_API_KEY")
	}
	if key == "" {
		t.Skip("GOOGLE_API_KEY or GEMINI_API_KEY not set")
	}

	cm, err := NewChatModel(context.Background(), ChatModelConfig{
		APIKey: key,
		Model:  "gemini-2.0-flash",
	})
	if err != nil {
		t.Fatalf("NewChatModel: %v", err)
	}

	msg, err := cm.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("Reply with exactly the word: pong"),
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(strings.ToLower(msg.Content), "pong") {
		t.Errorf("content = %q, want it to contain 'pong'", msg.Content)
	}
}

func TestNewChatModel_Live_WithTools(t *testing.T) {
	key := os.Getenv("GOOGLE_API_KEY")
	if key == "" {
		key = os.Getenv("GEMINI_API_KEY")
	}
	if key == "" {
		t.Skip("GOOGLE_API_KEY or GEMINI_API_KEY not set")
	}

	cm, err := NewChatModel(context.Background(), ChatModelConfig{
		APIKey: key,
		Model:  "gemini-2.0-flash",
	})
	if err != nil {
		t.Fatalf("NewChatModel: %v", err)
	}

	tool := &schema.ToolInfo{
		Name: "get_greeting",
		Desc: "Returns a greeting message",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name": {Type: schema.String, Desc: "Name to greet", Required: true},
		}),
	}
	withTool, err := cm.WithTools([]*schema.ToolInfo{tool})
	if err != nil {
		t.Fatalf("WithTools: %v", err)
	}

	msg, err := withTool.Generate(context.Background(), []*schema.Message{
		schema.UserMessage("Call get_greeting with name='world'"),
	})
	if err != nil {
		t.Fatalf("Generate with tools: %v", err)
	}
	if msg == nil {
		t.Fatal("Generate returned nil message")
	}
}
