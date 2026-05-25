package einoproviders_test

import (
	"context"
	"net/http"
	"testing"

	einoproviders "github.com/mattsp1290/eino-providers"
	_ "github.com/mattsp1290/eino-providers/claude"
	_ "github.com/mattsp1290/eino-providers/gemini"
	_ "github.com/mattsp1290/eino-providers/openai"
	_ "github.com/mattsp1290/eino-providers/openaicodex"
)

const (
	benchAPIKey           = "test-key"
	benchModelClaude      = "claude-opus-4-7"
	benchModelGemini      = "gemini-2.5-pro"
	benchModelOpenAI      = "gpt-4o"
	benchModelOpenAICodex = "gpt-codex"
)

func BenchmarkProviderConstruction_Claude(b *testing.B) {
	benchmarkProviderConstruction(b, "claude", benchModelClaude, einoproviders.Options{APIKey: benchAPIKey})
}

func BenchmarkProviderConstruction_OpenAI(b *testing.B) {
	benchmarkProviderConstruction(b, "openai", benchModelOpenAI, einoproviders.Options{APIKey: benchAPIKey})
}

func BenchmarkProviderConstruction_OpenAICodex(b *testing.B) {
	benchmarkProviderConstruction(b, "openai-codex", benchModelOpenAICodex, einoproviders.Options{
		HTTPClient: &http.Client{Transport: stubRoundTripper(func(*http.Request) (*http.Response, error) {
			b.Fatal("provider construction must not issue requests")
			return nil, nil
		})},
	})
}

func BenchmarkProviderConstruction_Gemini(b *testing.B) {
	b.Setenv("GOOGLE_API_KEY", "")
	b.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "")
	b.Setenv("GOOGLE_CLOUD_PROJECT", "")
	b.Setenv("GOOGLE_CLOUD_LOCATION", "")
	b.Setenv("GOOGLE_CLOUD_REGION", "")

	benchmarkProviderConstruction(b, "gemini", benchModelGemini, einoproviders.Options{APIKey: benchAPIKey})
}

func benchmarkProviderConstruction(b *testing.B, name, model string, opts einoproviders.Options) {
	b.Helper()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		p, err := einoproviders.NewProvider(ctx, name, model, opts)
		if err != nil {
			b.Fatalf("NewProvider(%s): %v", name, err)
		}
		if p == nil {
			b.Fatalf("NewProvider(%s) returned nil provider", name)
		}
	}
}

type stubRoundTripper func(*http.Request) (*http.Response, error)

func (f stubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
