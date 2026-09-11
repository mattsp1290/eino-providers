package einoproviders

import (
	"context"
	"errors"
	"testing"
)

func TestWrapInitErrorPreservesCauseStringAndMatchesSentinel(t *testing.T) {
	cause := errors.New("openai init: dial failed")
	wrapped := WrapInitError(cause)

	if wrapped == nil {
		t.Fatal("WrapInitError returned nil")
	}
	if got := wrapped.Error(); got != cause.Error() {
		t.Fatalf("Error() = %q, want %q", got, cause.Error())
	}
	if !errors.Is(wrapped, ErrProviderInit) {
		t.Fatal("wrapped error does not match ErrProviderInit")
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("wrapped error does not match cause")
	}
	if got := Classify(wrapped); got != ErrorClassProviderInit {
		t.Fatalf("Classify(wrapped) = %v, want %v", got, ErrorClassProviderInit)
	}
}

func TestWrapAuthErrorPreservesCauseStringAndMatchesSentinel(t *testing.T) {
	cause := errors.New("codexauth: not logged in")
	wrapped := WrapAuthError(cause)

	if wrapped == nil {
		t.Fatal("WrapAuthError returned nil")
	}
	if got := wrapped.Error(); got != cause.Error() {
		t.Fatalf("Error() = %q, want %q", got, cause.Error())
	}
	if !errors.Is(wrapped, ErrProviderAuth) {
		t.Fatal("wrapped error does not match ErrProviderAuth")
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("wrapped error does not match cause")
	}
	if got := Classify(wrapped); got != ErrorClassProviderAuth {
		t.Fatalf("Classify(wrapped) = %v, want %v", got, ErrorClassProviderAuth)
	}
}

func TestWrapNilErrorsReturnNil(t *testing.T) {
	if got := WrapInitError(nil); got != nil {
		t.Fatalf("WrapInitError(nil) = %v, want nil", got)
	}
	if got := WrapAuthError(nil); got != nil {
		t.Fatalf("WrapAuthError(nil) = %v, want nil", got)
	}
}

func TestClassifyProviderSentinels(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{name: "nil", err: nil, want: ErrorClassUnknown},
		{name: "timeout", err: context.DeadlineExceeded, want: ErrorClassUnknown},
		{name: "provider timeout", err: ErrProviderTimeout, want: ErrorClassProviderTimeout},
		{name: "provider api", err: ErrProviderAPI, want: ErrorClassProviderAPI},
		{name: "unknown provider", err: ErrUnknownProvider, want: ErrorClassUnknownProvider},
		{name: "backend unreachable", err: ErrBackendUnreachable, want: ErrorClassBackendUnreachable},
		{name: "unknown", err: errors.New("other"), want: ErrorClassUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.err); got != tt.want {
				t.Fatalf("Classify(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestAgenticErrorsClassifyWithoutContentDisclosure(t *testing.T) {
	unsupported := &UnsupportedCapabilityError{Provider: "openai", Protocol: "responses", Capability: "user_input_audio"}
	if !errors.Is(unsupported, ErrUnsupportedCapability) {
		t.Fatal("unsupported error does not match sentinel")
	}
	if got := Classify(unsupported); got != ErrorClassUnsupportedCapability {
		t.Fatalf("Classify(unsupported) = %v", got)
	}
	limited := &ResourceLimitError{Resource: "event_bytes", Limit: 1, Actual: 2}
	if !errors.Is(limited, ErrResourceLimit) {
		t.Fatal("resource limit error does not match sentinel")
	}
	if got := Classify(limited); got != ErrorClassResourceLimit {
		t.Fatalf("Classify(limited) = %v", got)
	}
	if got := limited.Error(); got != "agentic resource limit exceeded: event_bytes" {
		t.Fatalf("unsafe resource error string: %q", got)
	}
}
