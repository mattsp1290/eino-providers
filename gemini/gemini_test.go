package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genai"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestConstructors(t *testing.T) {
	t.Parallel()

	client := &genai.Client{}
	p, err := NewGeminiProviderWithClient(client, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("NewGeminiProviderWithClient: %v", err)
	}
	if p == nil {
		t.Fatal("NewGeminiProviderWithClient returned nil")
	}
}

func TestRegistryUsesProvidedGenaiClient(t *testing.T) {
	t.Parallel()

	client := &genai.Client{}
	p, err := einoproviders.NewProvider(context.Background(), "gemini", "gemini-2.5-pro", einoproviders.Options{
		APIKey:      "unused-when-client-provided",
		GenaiClient: client,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p == nil {
		t.Fatal("NewProvider returned nil provider")
	}
	got, ok := p.(*Provider)
	if !ok {
		t.Fatalf("NewProvider returned %T, want *Provider", p)
	}
	if got.client != client {
		t.Fatal("registry did not reuse provided GenaiClient")
	}
}

func TestAdviseWithCachedClient(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotRequest struct {
		SystemInstruction struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		GenerationConfig struct {
			MaxOutputTokens int `json:"maxOutputTokens"`
		} `json:"generationConfig"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}
		}`))
	}))
	t.Cleanup(srv.Close)

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:     "key",
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: srv.Client(),
		HTTPOptions: genai.HTTPOptions{
			BaseURL:    srv.URL,
			APIVersion: "v1beta",
		},
	})
	if err != nil {
		t.Fatalf("genai.NewClient: %v", err)
	}

	p, err := NewGeminiProviderWithClient(client, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("NewGeminiProviderWithClient: %v", err)
	}
	text, usage, err := p.Advise(context.Background(), "system", "user", 123)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if text != "hello" {
		t.Fatalf("text = %q, want hello", text)
	}
	if !usage.Available || usage.InputTokens != 3 || usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v, want available 3/4", usage)
	}
	if !strings.Contains(gotPath, "/v1beta/models/gemini-2.5-pro:generateContent") {
		t.Fatalf("path = %q, want Gemini generateContent path", gotPath)
	}
	if gotRequest.GenerationConfig.MaxOutputTokens != 123 {
		t.Fatalf("maxOutputTokens = %d, want 123", gotRequest.GenerationConfig.MaxOutputTokens)
	}
	if len(gotRequest.SystemInstruction.Parts) != 1 || gotRequest.SystemInstruction.Parts[0].Text != "system" {
		t.Fatalf("systemInstruction = %+v, want system", gotRequest.SystemInstruction)
	}
	if len(gotRequest.Contents) != 1 || gotRequest.Contents[0].Role != "user" {
		t.Fatalf("contents = %+v, want one user content", gotRequest.Contents)
	}
	if len(gotRequest.Contents[0].Parts) != 1 || gotRequest.Contents[0].Parts[0].Text != "user" {
		t.Fatalf("content parts = %+v, want user text", gotRequest.Contents[0].Parts)
	}
}
