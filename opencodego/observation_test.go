package opencodego

import (
	"context"
	"io"
	"strings"
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
		wg.Add(3)
		go func(status int) {
			defer wg.Done()
			state.recordHTTPError(&opencodeauth.HTTPError{StatusCode: status})
		}(400 + i)
		go func(value int) {
			defer wg.Done()
			state.updateBody(func(observation *bodyObservation) {
				observation.usage.input = observedCount{value: value, present: true}
				observation.usage.output = observedCount{value: value, present: true}
			})
		}(i)
		go func() {
			defer wg.Done()
			_ = state.httpError()
			_ = state.bodySnapshot()
		}()
	}
	wg.Wait()
}

func TestOperationStateBeginAttemptClearsBodyObservation(t *testing.T) {
	state := &operationState{}
	state.updateBody(func(observation *bodyObservation) {
		observation.terminal = true
		observation.err = errMalformedUsage
		observation.usage.input = observedCount{value: 3, present: true}
		observation.usage.output = observedCount{value: 4, present: true}
	})
	state.beginAttempt()

	if got := state.bodySnapshot(); got != (bodyObservation{}) {
		t.Fatalf("body observation survived retry: %#v", got)
	}
}

func TestOperationStateRejectsLateUsageFromPriorAttempt(t *testing.T) {
	state := &operationState{}
	state.beginAttempt()
	prior := newObservedJSONBody(
		io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":90,"completion_tokens":90}}`)),
		terminalChatCompletions,
		state,
	)

	state.beginAttempt()
	current := newObservedJSONBody(
		io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":2,"completion_tokens":3}}`)),
		terminalChatCompletions,
		state,
	)
	_, _ = io.ReadAll(prior)
	_ = prior.Close()
	if got := state.bodySnapshot(); got != (bodyObservation{}) {
		t.Fatalf("prior attempt repopulated cleared observation: %#v", got)
	}
	_, _ = io.ReadAll(current)
	_ = current.Close()
	usage := observedUsageTokenUsage(state.bodySnapshot())
	if usage == nil || usage.PromptTokens != 2 || usage.CompletionTokens != 3 {
		t.Fatalf("current attempt usage = %#v", usage)
	}
}

func TestOperationUsageObservationsStayIsolated(t *testing.T) {
	const operations = 32
	states := make([]*operationState, operations)
	var wg sync.WaitGroup
	for i := range states {
		states[i] = &operationState{}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			states[index].updateBody(func(observation *bodyObservation) {
				observation.terminal = true
				observation.usage.input = observedCount{value: index, present: true}
				observation.usage.output = observedCount{value: index + 1, present: true}
			})
		}(i)
	}
	wg.Wait()
	for i, state := range states {
		got := state.bodySnapshot()
		if got.usage.input.value != i || got.usage.output.value != i+1 || !got.terminal {
			t.Fatalf("operation %d observation = %#v", i, got)
		}
	}
}

func TestOperationStateNilSafety(t *testing.T) {
	ctx, state := withOperationState(context.Background())
	if ctx == nil || state == nil {
		t.Fatal("operation state setup returned nil")
	}
	var nilState *operationState
	nilState.beginAttempt()
	nilState.recordHTTPError(nil)
	nilState.updateBody(nil)
	if nilState.httpError() != nil {
		t.Fatal("nil state returned an error")
	}
	if got := nilState.bodySnapshot(); got != (bodyObservation{}) {
		t.Fatalf("nil state body observation = %#v", got)
	}
}
