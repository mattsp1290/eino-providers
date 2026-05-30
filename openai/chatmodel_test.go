package openai

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

	t.Run("api key required when no base url", func(t *testing.T) {
		t.Parallel()
		_, err := NewChatModel(ctx, ChatModelConfig{Model: "gpt-4o"})
		if err == nil {
			t.Fatal("expected error for empty APIKey without BaseURL")
		}
		if !errors.Is(err, einoproviders.ErrProviderInit) {
			t.Errorf("err = %v, want ErrProviderInit", err)
		}
	})

	t.Run("model required", func(t *testing.T) {
		t.Parallel()
		_, err := NewChatModel(ctx, ChatModelConfig{APIKey: "fake"})
		if err == nil {
			t.Fatal("expected error for empty Model")
		}
		if !errors.Is(err, einoproviders.ErrProviderInit) {
			t.Errorf("err = %v, want ErrProviderInit", err)
		}
	})

	t.Run("no api key required when base url set", func(t *testing.T) {
		t.Parallel()
		cm, err := NewChatModel(ctx, ChatModelConfig{
			Model:   "llama3",
			BaseURL: "http://localhost:11434/v1",
		})
		if err != nil {
			t.Fatalf("unexpected error with BaseURL and no APIKey: %v", err)
		}
		if cm == nil {
			t.Fatal("NewChatModel returned nil")
		}
	})
}

func TestNewChatModel_Success(t *testing.T) {
	t.Parallel()
	cm, err := NewChatModel(context.Background(), ChatModelConfig{
		APIKey: "fake",
		Model:  "gpt-4o",
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
		APIKey: "fake",
		Model:  "gpt-4o",
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
