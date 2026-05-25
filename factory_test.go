package einoproviders

import (
	"context"
	"errors"
	"testing"
)

type testProvider struct{}

func (testProvider) Advise(context.Context, string, string, int) (string, Usage, error) {
	return "ok", Usage{Available: false}, nil
}

func TestNewProviderUnknownProvider(t *testing.T) {
	_, err := NewProvider(context.Background(), t.Name(), "model", Options{})
	if err == nil {
		t.Fatal("NewProvider returned nil error")
	}
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("NewProvider error = %v, want ErrUnknownProvider", err)
	}
	if got := Classify(err); got != ErrorClassUnknownProvider {
		t.Fatalf("Classify(err) = %v, want %v", got, ErrorClassUnknownProvider)
	}
}

func TestNewProviderDispatchesRegisteredConstructor(t *testing.T) {
	name := t.Name()
	wantModel := "model-a"
	wantBaseURL := "http://example.test"
	wantOpts := Options{
		APIKey:  "key-a",
		BaseURL: &wantBaseURL,
	}

	var called bool
	RegisterProvider(name, func(ctx context.Context, model string, opts Options) (Provider, error) {
		called = true
		if ctx == nil {
			t.Fatal("ctx is nil")
		}
		if model != wantModel {
			t.Fatalf("model = %q, want %q", model, wantModel)
		}
		if opts.APIKey != wantOpts.APIKey {
			t.Fatalf("APIKey = %q, want %q", opts.APIKey, wantOpts.APIKey)
		}
		if opts.BaseURL == nil || *opts.BaseURL != wantBaseURL {
			t.Fatalf("BaseURL = %v, want %q", opts.BaseURL, wantBaseURL)
		}
		return testProvider{}, nil
	})

	provider, err := NewProvider(context.Background(), name, wantModel, wantOpts)
	if err != nil {
		t.Fatalf("NewProvider returned error: %v", err)
	}
	if !called {
		t.Fatal("registered constructor was not called")
	}

	text, usage, err := provider.Advise(context.Background(), "", "", 1)
	if err != nil {
		t.Fatalf("Advise returned error: %v", err)
	}
	if text != "ok" || usage.Available {
		t.Fatalf("Advise returned (%q, %+v), want ok unavailable usage", text, usage)
	}
}
