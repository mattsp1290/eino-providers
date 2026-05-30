package claude

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestNewChatModel_Validation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("model required", func(t *testing.T) {
		t.Parallel()
		_, err := NewChatModel(ctx, ChatModelConfig{APIKey: "fake", MaxTokens: 1024})
		if err == nil {
			t.Fatal("expected error for empty Model")
		}
		if !errors.Is(err, einoproviders.ErrProviderInit) {
			t.Errorf("err = %v, want ErrProviderInit", err)
		}
	})

	t.Run("max tokens required", func(t *testing.T) {
		t.Parallel()
		_, err := NewChatModel(ctx, ChatModelConfig{APIKey: "fake", Model: "claude-sonnet-4-5"})
		if err == nil {
			t.Fatal("expected error for MaxTokens = 0")
		}
		if !errors.Is(err, einoproviders.ErrProviderInit) {
			t.Errorf("err = %v, want ErrProviderInit", err)
		}
	})
}

func TestNewChatModel_Success(t *testing.T) {
	t.Parallel()
	cm, err := NewChatModel(context.Background(), ChatModelConfig{
		APIKey:    "fake",
		Model:     "claude-sonnet-4-5",
		MaxTokens: 1024,
	})
	if err != nil {
		t.Fatalf("NewChatModel: %v", err)
	}
	if cm == nil {
		t.Fatal("NewChatModel returned nil")
	}
}

func TestNewChatModel_WithTools(t *testing.T) {
	t.Parallel()
	cm, err := NewChatModel(context.Background(), ChatModelConfig{
		APIKey:    "fake",
		Model:     "claude-sonnet-4-5",
		MaxTokens: 1024,
	})
	if err != nil {
		t.Fatalf("NewChatModel: %v", err)
	}
	derived, err := cm.WithTools([]*schema.ToolInfo{{Name: "noop", Desc: "noop"}})
	if err != nil {
		t.Fatalf("WithTools: %v", err)
	}
	if derived == nil {
		t.Fatal("WithTools returned nil")
	}
}
