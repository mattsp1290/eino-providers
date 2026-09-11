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
	// ErrUnsupportedCapability reports an agentic input or option that the
	// selected native provider protocol cannot represent. It is always raised
	// before a request is dispatched.
	ErrUnsupportedCapability = errors.New("unsupported agentic capability")
	// ErrResourceLimit reports an agentic request or response that exceeded a
	// configured boundary.
	ErrResourceLimit = errors.New("agentic resource limit exceeded")
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
	ErrorClassUnsupportedCapability
	ErrorClassResourceLimit
)

// Classify returns the first matching provider error class for err.
func Classify(err error) ErrorClass {
	switch {
	case err == nil:
		return ErrorClassUnknown
	case errors.Is(err, ErrProviderTimeout):
		return ErrorClassProviderTimeout
	case errors.Is(err, ErrUnsupportedCapability):
		return ErrorClassUnsupportedCapability
	case errors.Is(err, ErrResourceLimit):
		return ErrorClassResourceLimit
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

// Capability identifies a native protocol feature for capability errors. It
// intentionally contains no caller content, credentials, or opaque state.
type Capability string

// UnsupportedCapabilityError is returned before transport dispatch when a
// concrete agentic provider cannot faithfully encode a requested feature.
type UnsupportedCapabilityError struct {
	Provider   string
	Protocol   string
	Capability Capability
}

func (e *UnsupportedCapabilityError) Error() string {
	if e == nil {
		return ErrUnsupportedCapability.Error()
	}
	if e.Provider == "" && e.Protocol == "" && e.Capability == "" {
		return ErrUnsupportedCapability.Error()
	}
	return "unsupported agentic capability: " + e.Provider + "/" + e.Protocol + "/" + string(e.Capability)
}

func (e *UnsupportedCapabilityError) Is(target error) bool {
	return target == ErrUnsupportedCapability
}

// ResourceLimitError identifies a bounded agentic resource without retaining
// the content that exceeded it.
type ResourceLimitError struct {
	Resource string
	Limit    int64
	Actual   int64
}

func (e *ResourceLimitError) Error() string {
	if e == nil {
		return ErrResourceLimit.Error()
	}
	return "agentic resource limit exceeded: " + e.Resource
}

func (e *ResourceLimitError) Is(target error) bool {
	return target == ErrResourceLimit
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
