package opencodego

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

func TestMapConstructorErrorClassifiesAndSanitizes(t *testing.T) {
	canary := "secret-api-key-canary"
	cause := errors.Join(opencodeauth.ErrMissingAPIKey, errors.New(canary))
	err := mapConstructorError(cause)

	for _, target := range []error{einoproviders.ErrProviderInit, einoproviders.ErrProviderAuth, opencodeauth.ErrMissingAPIKey, cause} {
		if !errors.Is(err, target) {
			t.Errorf("error does not preserve %v", target)
		}
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("public error leaked canary: %q", err)
	}
	if got := einoproviders.Classify(err); got != einoproviders.ErrorClassProviderInit {
		t.Fatalf("Classify(error) = %v, want init", got)
	}
}

func TestMapInvocationErrorHTTPClassification(t *testing.T) {
	tests := []struct {
		name      string
		httpError *opencodeauth.HTTPError
		wantClass einoproviders.ErrorClass
		wantAuth  bool
	}{
		{name: "authentication", httpError: &opencodeauth.HTTPError{StatusCode: 401, Kind: opencodeauth.ErrorKindAuthentication}, wantClass: einoproviders.ErrorClassProviderAuth, wantAuth: true},
		{name: "quota", httpError: &opencodeauth.HTTPError{StatusCode: 429, Kind: opencodeauth.ErrorKindQuota}, wantClass: einoproviders.ErrorClassProviderAuth, wantAuth: true},
		{name: "unknown unauthorized", httpError: &opencodeauth.HTTPError{StatusCode: 401, Kind: opencodeauth.ErrorKindUnknown}, wantClass: einoproviders.ErrorClassProviderAuth, wantAuth: true},
		{name: "unknown forbidden", httpError: &opencodeauth.HTTPError{StatusCode: 403, Kind: opencodeauth.ErrorKindUnknown}, wantClass: einoproviders.ErrorClassProviderAuth, wantAuth: true},
		{name: "policy forbidden", httpError: &opencodeauth.HTTPError{StatusCode: 403, Kind: opencodeauth.ErrorKindPolicy}, wantClass: einoproviders.ErrorClassProviderAPI},
		{name: "rate limit", httpError: &opencodeauth.HTTPError{StatusCode: 429, Kind: opencodeauth.ErrorKindRateLimit, RetryAfter: time.Second, HasRetryAfter: true}, wantClass: einoproviders.ErrorClassProviderAPI},
		{name: "model", httpError: &opencodeauth.HTTPError{StatusCode: 404, Kind: opencodeauth.ErrorKindModel}, wantClass: einoproviders.ErrorClassProviderAPI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &operationState{}
			state.recordHTTPError(tt.httpError)
			sdkErr := errors.New("sdk-body-secret")
			err := mapInvocationError(operationGenerate, sdkErr, state)
			if got := einoproviders.Classify(err); got != tt.wantClass {
				t.Fatalf("Classify(error) = %v, want %v", got, tt.wantClass)
			}
			if got := errors.Is(err, einoproviders.ErrProviderAuth); got != tt.wantAuth {
				t.Fatalf("errors.Is(auth) = %v, want %v", got, tt.wantAuth)
			}
			var preserved *opencodeauth.HTTPError
			if !errors.As(err, &preserved) || preserved.StatusCode != tt.httpError.StatusCode || preserved.Kind != tt.httpError.Kind {
				t.Fatalf("typed HTTP error = %#v, want %#v", preserved, tt.httpError)
			}
			if !errors.Is(err, sdkErr) {
				t.Fatal("SDK cause was not preserved")
			}
			if strings.Contains(err.Error(), "sdk-body-secret") {
				t.Fatalf("public error leaked SDK text: %q", err)
			}
		})
	}
}

func TestMapInvocationErrorClassifiesDirectHTTPError(t *testing.T) {
	tests := []struct {
		name      string
		httpError *opencodeauth.HTTPError
		wantClass einoproviders.ErrorClass
	}{
		{name: "authentication kind", httpError: &opencodeauth.HTTPError{StatusCode: 400, Kind: opencodeauth.ErrorKindAuthentication}, wantClass: einoproviders.ErrorClassProviderAuth},
		{name: "quota kind", httpError: &opencodeauth.HTTPError{StatusCode: 429, Kind: opencodeauth.ErrorKindQuota}, wantClass: einoproviders.ErrorClassProviderAuth},
		{name: "unknown unauthorized", httpError: &opencodeauth.HTTPError{StatusCode: 401, Kind: opencodeauth.ErrorKindUnknown}, wantClass: einoproviders.ErrorClassProviderAuth},
		{name: "unknown forbidden", httpError: &opencodeauth.HTTPError{StatusCode: 403, Kind: opencodeauth.ErrorKindUnknown}, wantClass: einoproviders.ErrorClassProviderAuth},
		{name: "policy forbidden", httpError: &opencodeauth.HTTPError{StatusCode: 403, Kind: opencodeauth.ErrorKindPolicy}, wantClass: einoproviders.ErrorClassProviderAPI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapInvocationError(operationRequest, tt.httpError, nil)
			if got := einoproviders.Classify(err); got != tt.wantClass {
				t.Fatalf("Classify(error) = %v, want %v", got, tt.wantClass)
			}
			var preserved *opencodeauth.HTTPError
			if !errors.As(err, &preserved) || preserved != tt.httpError {
				t.Fatalf("typed HTTP error = %#v, want original %#v", preserved, tt.httpError)
			}
		})
	}
}

func TestMapInvocationErrorLocalAndContextFailures(t *testing.T) {
	timeoutCause := &net.DNSError{IsTimeout: true}
	tests := []struct {
		name      string
		cause     error
		wantClass einoproviders.ErrorClass
		wantIs    error
	}{
		{name: "deadline", cause: context.DeadlineExceeded, wantClass: einoproviders.ErrorClassProviderTimeout, wantIs: context.DeadlineExceeded},
		{name: "network timeout", cause: timeoutCause, wantClass: einoproviders.ErrorClassProviderTimeout, wantIs: timeoutCause},
		{name: "cancellation", cause: context.Canceled, wantClass: einoproviders.ErrorClassUnknown, wantIs: context.Canceled},
		{name: "missing session", cause: opencodeauth.ErrMissingSessionID, wantClass: einoproviders.ErrorClassProviderAPI, wantIs: opencodeauth.ErrMissingSessionID},
		{name: "invalid session", cause: opencodeauth.ErrInvalidSessionID, wantClass: einoproviders.ErrorClassProviderAPI, wantIs: opencodeauth.ErrInvalidSessionID},
		{name: "disallowed request", cause: opencodeauth.ErrDisallowedRequest, wantClass: einoproviders.ErrorClassProviderAPI, wantIs: opencodeauth.ErrDisallowedRequest},
		{name: "missing key during invocation", cause: opencodeauth.ErrMissingAPIKey, wantClass: einoproviders.ErrorClassProviderAuth, wantIs: opencodeauth.ErrMissingAPIKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapInvocationError(operationRequest, tt.cause, nil)
			if got := einoproviders.Classify(err); got != tt.wantClass {
				t.Fatalf("Classify(error) = %v, want %v", got, tt.wantClass)
			}
			if !errors.Is(err, tt.wantIs) {
				t.Fatalf("error does not preserve %v", tt.wantIs)
			}
		})
	}
}

func TestSafeOperationLabelsDoNotEchoInput(t *testing.T) {
	cause := errors.New("body-secret")
	err := safeFailure(invocationOperation(255), cause)
	if got, want := err.Error(), "opencode-go: unknown failed"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("safe error did not preserve its cause")
	}
}

func TestMapInvocationErrorPreservesRetryMetadata(t *testing.T) {
	state := &operationState{}
	state.recordHTTPError(&opencodeauth.HTTPError{
		StatusCode:    http.StatusTooManyRequests,
		Kind:          opencodeauth.ErrorKindRateLimit,
		RetryAfter:    3 * time.Second,
		HasRetryAfter: true,
	})
	err := mapInvocationError(operationReceive, errors.New("sdk failure"), state)
	var got *opencodeauth.HTTPError
	if !errors.As(err, &got) || !got.HasRetryAfter || got.RetryAfter != 3*time.Second {
		t.Fatalf("retry metadata = %#v", got)
	}
}
