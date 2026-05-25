package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func BenchmarkNewChatModel(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	b.Cleanup(srv.Close)

	ctx := context.Background()
	cfg := Config{
		BaseURL:    srv.URL,
		Model:      "qwen3-coder:30b",
		Timeout:    time.Second,
		KeepAlive:  "-1",
		HTTPClient: srv.Client(),
	}

	b.ReportAllocs()
	for b.Loop() {
		chatModel, err := NewChatModel(ctx, cfg)
		if err != nil {
			b.Fatalf("NewChatModel: %v", err)
		}
		if chatModel == nil {
			b.Fatal("NewChatModel returned nil model")
		}
	}
}
