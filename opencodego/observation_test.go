package opencodego

import (
	"context"
	"sync"
	"testing"
	"time"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"
)

type contextMarker struct{}

func TestWithOperationStatePreservesContext(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	parent, cancel := context.WithDeadline(context.WithValue(context.Background(), contextMarker{}, "marker"), deadline)
	derived, state := withOperationState(parent)

	if state == nil || operationStateFromContext(derived) != state {
		t.Fatal("derived context did not contain its operation state")
	}
	if got := derived.Value(contextMarker{}); got != "marker" {
		t.Fatalf("context value = %v, want marker", got)
	}
	gotDeadline, ok := derived.Deadline()
	if !ok || !gotDeadline.Equal(deadline) {
		t.Fatalf("deadline = %v, %v; want %v, true", gotDeadline, ok, deadline)
	}
	cancel()
	select {
	case <-derived.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not reach derived context")
	}
}

func TestOperationStateCopiesAndClearsHTTPError(t *testing.T) {
	state := &operationState{}
	source := &opencodeauth.HTTPError{StatusCode: 429, Kind: opencodeauth.ErrorKindRateLimit}
	state.recordHTTPError(source)
	source.StatusCode = 500

	first := state.httpError()
	if first == nil || first.StatusCode != 429 {
		t.Fatalf("recorded HTTP error = %#v, want independent 429 copy", first)
	}
	first.StatusCode = 418
	if got := state.httpError(); got == nil || got.StatusCode != 429 {
		t.Fatalf("returned HTTP error was not copied: %#v", got)
	}

	state.beginAttempt()
	if got := state.httpError(); got != nil {
		t.Fatalf("beginAttempt retained stale error: %#v", got)
	}
}

func TestOperationStateConcurrentAccess(t *testing.T) {
	state := &operationState{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(status int) {
			defer wg.Done()
			state.recordHTTPError(&opencodeauth.HTTPError{StatusCode: status})
		}(400 + i)
		go func() {
			defer wg.Done()
			_ = state.httpError()
		}()
	}
	wg.Wait()
}

func TestOperationStateNilSafety(t *testing.T) {
	ctx, state := withOperationState(context.Background())
	if ctx == nil || state == nil {
		t.Fatal("operation state setup returned nil")
	}
	var nilState *operationState
	nilState.beginAttempt()
	nilState.recordHTTPError(nil)
	if nilState.httpError() != nil {
		t.Fatal("nil state returned an error")
	}
}
