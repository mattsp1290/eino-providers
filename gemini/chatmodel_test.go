package gemini

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
	"google.golang.org/genai"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func newFakeGeminiClient(t *testing.T) *genai.Client {
	t.Helper()
	// Use BackendGeminiAPI to avoid the Vertex AI branch (triggered by
	// GOOGLE_GENAI_USE_VERTEXAI=true) which requires live ADC credentials.
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  "fake",
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		t.Fatalf("genai.NewClient: %v", err)
	}
	return client
}

func TestNewChatModel_Validation(t *testing.T) {
	t.Parallel()

	t.Run("model required", func(t *testing.T) {
		t.Parallel()
		_, err := NewChatModel(context.Background(), ChatModelConfig{
			Client: newFakeGeminiClient(t),
		})
		if err == nil {
			t.Fatal("expected error for empty Model")
		}
		if !errors.Is(err, einoproviders.ErrProviderInit) {
			t.Errorf("err = %v, want ErrProviderInit", err)
		}
	})
}

func TestNewChatModel_Success_WithClient(t *testing.T) {
	t.Parallel()
	cm, err := NewChatModel(context.Background(), ChatModelConfig{
		Client: newFakeGeminiClient(t),
		Model:  "gemini-2.0-flash",
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
		Client: newFakeGeminiClient(t),
		Model:  "gemini-2.0-flash",
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
