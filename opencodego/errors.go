package opencodego

import (
	"context"
	"errors"
	"net"
	"net/http"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

type invocationOperation uint8

const (
	operationAdvise invocationOperation = iota + 1
	operationGenerate
	operationStream
	operationReceive
	operationRequest
)

func (o invocationOperation) label() string {
	switch o {
	case operationAdvise:
		return "advise"
	case operationGenerate:
		return "generate"
	case operationStream:
		return "stream"
	case operationReceive:
		return "receive"
	case operationRequest:
		return "request"
	default:
		return "unknown"
	}
}

type invocationClass uint8

const (
	invocationCanceled invocationClass = iota + 1
	invocationTimeout
	invocationAuth
	invocationAPI
)

type invocationError struct {
	operation invocationOperation
	causes    []error
}

func (e *invocationError) Error() string {
	if e == nil {
		return "opencode-go: request failed"
	}
	return "opencode-go: " + e.operation.label() + " failed"
}

func (e *invocationError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return e.causes
}

func safeFailure(operation invocationOperation, causes ...error) error {
	filtered := make([]error, 0, len(causes))
	for _, cause := range causes {
		if cause != nil {
			filtered = append(filtered, cause)
		}
	}
	return &invocationError{operation: operation, causes: filtered}
}

// mapInvocationError combines the SDK-facing error with the final HTTP
// attempt's typed observation. Classification uses only stable local values;
// Error never includes either wrapped cause's text.
func mapInvocationError(operation invocationOperation, err error, state *operationState) error {
	if err == nil {
		return nil
	}
	httpErr := state.httpError()
	causes := []error{err}
	if httpErr != nil {
		causes = append(causes, httpErr)
	} else {
		_ = errors.As(err, &httpErr)
	}

	switch classifyInvocationError(err, httpErr) {
	case invocationCanceled:
		return safeFailure(operation, causes...)
	case invocationTimeout:
		return safeFailure(operation, append([]error{einoproviders.ErrProviderTimeout}, causes...)...)
	case invocationAuth:
		return einoproviders.WrapAuthError(safeFailure(operation, causes...))
	case invocationAPI:
		return safeFailure(operation, append([]error{einoproviders.ErrProviderAPI}, causes...)...)
	}
	return safeFailure(operation, append([]error{einoproviders.ErrProviderAPI}, causes...)...)
}

func classifyInvocationError(err error, httpErr *opencodeauth.HTTPError) invocationClass {
	if errors.Is(err, context.Canceled) {
		return invocationCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return invocationTimeout
	}
	if httpErr != nil {
		switch httpErr.Kind {
		case opencodeauth.ErrorKindAuthentication, opencodeauth.ErrorKindQuota:
			return invocationAuth
		case opencodeauth.ErrorKindPolicy:
			return invocationAPI
		}
		if httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden {
			return invocationAuth
		}
		return invocationAPI
	}
	if errors.Is(err, opencodeauth.ErrMissingAPIKey) {
		return invocationAuth
	}
	return invocationAPI
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
