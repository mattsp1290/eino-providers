package opencodego

import (
	"context"
	"sync"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"
)

// operationState contains observations that belong to one model operation.
// It must never be retained on a shared model: SDK calls and stream producers
// can overlap, so every access is synchronized.
type operationState struct {
	mu            sync.RWMutex
	lastHTTPError *opencodeauth.HTTPError
	body          bodyObservation
	attempt       uint64
}

type observedCount struct {
	value   int
	present bool
}

type wireUsageObservation struct {
	input     observedCount
	output    observedCount
	total     observedCount
	cached    observedCount
	reasoning observedCount
}

type bodyObservation struct {
	usage    wireUsageObservation
	terminal bool
	err      error
}

type operationStateContextKey struct{}

// withOperationState derives an operation context without losing caller
// values, cancellation, or deadlines.
func withOperationState(ctx context.Context) (context.Context, *operationState) {
	if ctx == nil {
		ctx = context.Background()
	}
	state := &operationState{}
	return context.WithValue(ctx, operationStateContextKey{}, state), state
}

func operationStateFromContext(ctx context.Context) *operationState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(operationStateContextKey{}).(*operationState)
	return state
}

// beginAttempt clears observations before control reaches the authenticated
// transport. A later network failure therefore cannot inherit a typed failure
// from an earlier SDK retry.
func (s *operationState) beginAttempt() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.attempt++
	s.lastHTTPError = nil
	s.body = bodyObservation{}
	s.mu.Unlock()
}

func (s *operationState) recordHTTPError(err *opencodeauth.HTTPError) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if err == nil {
		s.lastHTTPError = nil
	} else {
		copy := *err
		s.lastHTTPError = &copy
	}
	s.mu.Unlock()
}

func (s *operationState) httpError() *opencodeauth.HTTPError {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastHTTPError == nil {
		return nil
	}
	copy := *s.lastHTTPError
	return &copy
}

func (s *operationState) updateBody(update func(*bodyObservation)) {
	if s == nil || update == nil {
		return
	}
	s.mu.Lock()
	update(&s.body)
	s.mu.Unlock()
}

func (s *operationState) attemptID() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.attempt
}

func (s *operationState) updateBodyForAttempt(attempt uint64, update func(*bodyObservation)) {
	if s == nil || update == nil {
		return
	}
	s.mu.Lock()
	if s.attempt == attempt {
		update(&s.body)
	}
	s.mu.Unlock()
}

func (s *operationState) bodySnapshot() bodyObservation {
	if s == nil {
		return bodyObservation{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.body
}
