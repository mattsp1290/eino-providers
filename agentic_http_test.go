package einoproviders

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
)

type agenticRoundTripFunc func(*http.Request) (*http.Response, error)

func (f agenticRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestAgenticLimitedHTTPClientBoundsBodiesWithoutMutatingSource(t *testing.T) {
	source := &http.Client{Transport: agenticRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte("12345")))}, nil
	})}
	client, err := NewAgenticLimitedHTTPClient(source, AgenticLimits{MaxResponseBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if client == source || client.Transport == source.Transport {
		t.Fatal("limited client mutated or reused source client")
	}
	resp, err := client.Get("https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("read error = %v", err)
	}
}

func TestAgenticLimitedHTTPClientRejectsKnownOversizeRequest(t *testing.T) {
	called := false
	client, err := NewAgenticLimitedHTTPClient(&http.Client{Transport: agenticRoundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})}, AgenticLimits{MaxRequestBytes: 2})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.test", bytes.NewReader([]byte("123")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(req)
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("oversize request reached transport")
	}
}

func TestAgenticLimitedHTTPClientBoundsSSEEvent(t *testing.T) {
	client, err := NewAgenticLimitedHTTPClient(&http.Client{Transport: agenticRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(bytes.NewReader([]byte("data: 12345\n\n")))}, nil
	})}, AgenticLimits{MaxEventBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get("https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("read error = %v", err)
	}
}
