package einoproviders

import "errors"

// Provider-error sentinels used for errors.Is classification.
var (
	ErrProviderInit       = errors.New("provider init error")
	ErrProviderTimeout    = errors.New("provider timeout")
	ErrProviderAPI        = errors.New("provider API error")
	ErrProviderAuth       = errors.New("provider auth error")
	ErrUnknownProvider    = errors.New("unknown provider")
	ErrBackendUnreachable = errors.New("backend unreachable")
)

// ErrorClass is the switch-friendly classification returned by Classify.
type ErrorClass int

const (
	ErrorClassUnknown ErrorClass = iota
	ErrorClassProviderInit
	ErrorClassProviderTimeout
	ErrorClassProviderAPI
	ErrorClassProviderAuth
	ErrorClassUnknownProvider
	ErrorClassBackendUnreachable
)

// Classify returns the first matching provider error class for err.
func Classify(err error) ErrorClass {
	switch {
	case err == nil:
		return ErrorClassUnknown
	case errors.Is(err, ErrProviderTimeout):
		return ErrorClassProviderTimeout
	case errors.Is(err, ErrProviderInit):
		return ErrorClassProviderInit
	case errors.Is(err, ErrProviderAuth):
		return ErrorClassProviderAuth
	case errors.Is(err, ErrUnknownProvider):
		return ErrorClassUnknownProvider
	case errors.Is(err, ErrBackendUnreachable):
		return ErrorClassBackendUnreachable
	case errors.Is(err, ErrProviderAPI):
		return ErrorClassProviderAPI
	default:
		return ErrorClassUnknown
	}
}

type initErr struct{ cause error }

func (e *initErr) Error() string {
	if e == nil || e.cause == nil {
		return ErrProviderInit.Error()
	}
	return e.cause.Error()
}

func (e *initErr) Unwrap() []error {
	if e == nil || e.cause == nil {
		return []error{ErrProviderInit}
	}
	return []error{ErrProviderInit, e.cause}
}

// WrapInitError tags err as a provider initialization failure.
//
// The returned error matches ErrProviderInit with errors.Is and preserves
// err.Error() as its own Error string. Nil input returns nil.
func WrapInitError(err error) error {
	if err == nil {
		return nil
	}
	return &initErr{cause: err}
}

type authErr struct{ cause error }

func (e *authErr) Error() string {
	if e == nil || e.cause == nil {
		return ErrProviderAuth.Error()
	}
	return e.cause.Error()
}

func (e *authErr) Unwrap() []error {
	if e == nil || e.cause == nil {
		return []error{ErrProviderAuth}
	}
	return []error{ErrProviderAuth, e.cause}
}

// WrapAuthError tags err as a provider authentication failure.
//
// The returned error matches ErrProviderAuth with errors.Is and preserves
// err.Error() as its own Error string. Nil input returns nil.
func WrapAuthError(err error) error {
	if err == nil {
		return nil
	}
	return &authErr{cause: err}
}
