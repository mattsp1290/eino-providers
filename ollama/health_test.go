package ollama

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestPingOllamaSuccess(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	var sawPath atomic.Value
	var sawMethod atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		sawPath.Store(r.URL.Path)
		sawMethod.Store(r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	t.Cleanup(srv.Close)

	if err := PingOllama(context.Background(), srv.URL, srv.Client()); err != nil {
		t.Fatalf("PingOllama: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", hits.Load())
	}
	if got, _ := sawPath.Load().(string); got != "/api/tags" {
		t.Errorf("path = %q, want /api/tags", got)
	}
	if got, _ := sawMethod.Load().(string); got != http.MethodGet {
		t.Errorf("method = %q, want GET", got)
	}
}

func TestPingOllamaProxyPathAndQueryDropped(t *testing.T) {
	t.Parallel()

	var sawPath atomic.Value
	var sawQuery atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/ollama/api/tags", func(w http.ResponseWriter, r *http.Request) {
		sawPath.Store(r.URL.Path)
		sawQuery.Store(r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	if err := PingOllama(context.Background(), srv.URL+"/ollama?token=foo", srv.Client()); err != nil {
		t.Fatalf("PingOllama: %v", err)
	}
	if got, _ := sawPath.Load().(string); got != "/ollama/api/tags" {
		t.Errorf("path = %q, want /ollama/api/tags", got)
	}
	if got, _ := sawQuery.Load().(string); got != "" {
		t.Errorf("raw query = %q, want empty", got)
	}
}

func TestPingOllamaCredentialsScrubbed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	withCreds := strings.Replace(srv.URL, "http://", "http://hunter2:supersecret@", 1)
	err := PingOllama(context.Background(), withCreds, srv.Client())
	if err == nil {
		t.Fatal("PingOllama: nil err, want non-2xx error")
	}
	if strings.Contains(err.Error(), "supersecret") || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error message leaked credentials: %q", err.Error())
	}
}

func TestPingOllamaNon2xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	err := PingOllama(context.Background(), srv.URL, srv.Client())
	if err == nil {
		t.Fatal("PingOllama: nil err, want non-nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Errorf("err = %q, want substring status 502", err.Error())
	}
}

func TestPingWithCappedTimeoutWrapsBackendUnreachable(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-block
	}))
	t.Cleanup(func() {
		close(block)
		srv.Close()
	})

	start := time.Now()
	err := pingWithCappedTimeout(context.Background(), srv.URL, srv.Client(), 50*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("pingWithCappedTimeout: nil err, want timeout error")
	}
	if !errors.Is(err, ErrBackendUnreachable) {
		t.Errorf("errors.Is(err, ErrBackendUnreachable) = false, want true")
	}
	if !errors.Is(err, einoproviders.ErrBackendUnreachable) {
		t.Errorf("errors.Is(err, einoproviders.ErrBackendUnreachable) = false, want true")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, want true (err=%v)", err)
	}
	if elapsed > time.Second {
		t.Errorf("ping took %v; expected fast-fail near 50ms", elapsed)
	}
}

func TestPingOllamaRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "empty", url: "", want: "empty"},
		{name: "bad scheme", url: "ftp://example.invalid", want: "scheme"},
		{name: "missing host", url: "http://", want: "host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := PingOllama(context.Background(), tt.url, &http.Client{})
			if err == nil {
				t.Fatal("PingOllama error = nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("PingOllama error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}
