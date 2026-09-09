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
	s.lastHTTPError = nil
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
