package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewChatModel(t *testing.T) {
	t.Parallel()
	srv, hits := fakeOllamaServer(t)

	chatModel, err := NewChatModel(context.Background(), Config{
		BaseURL:    srv.URL,
		Model:      "qwen3-coder:30b",
		Timeout:    time.Second,
		KeepAlive:  "-1",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewChatModel: %v", err)
	}
	if chatModel == nil {
		t.Fatal("NewChatModel returned nil model")
	}
	if hits.Load() != 1 {
		t.Fatalf("/api/tags hits = %d, want 1", hits.Load())
	}
}

func TestNewChatModelValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "missing base url", cfg: Config{Model: "x", Timeout: time.Second}, want: "BaseURL"},
		{name: "bad scheme", cfg: Config{BaseURL: "ftp://example.invalid", Model: "x", Timeout: time.Second}, want: "scheme"},
		{name: "missing host", cfg: Config{BaseURL: "http://", Model: "x", Timeout: time.Second}, want: "host"},
		{name: "missing model", cfg: Config{BaseURL: "http://localhost:11434", Timeout: time.Second}, want: "Model"},
		{name: "missing timeout", cfg: Config{BaseURL: "http://localhost:11434", Model: "x"}, want: "Timeout"},
		{name: "bad keep alive", cfg: Config{BaseURL: "http://localhost:11434", Model: "x", Timeout: time.Second, KeepAlive: "soon"}, want: "keep_alive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewChatModel(context.Background(), tt.cfg)
			if err == nil {
				t.Fatal("NewChatModel error = nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewChatModel error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func fakeOllamaServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			hits.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}
