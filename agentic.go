package einoproviders

import "fmt"

// AgenticResponseIdentity records the provider identity observed for a native
// agentic response. RequestedModel and ReturnedModel are deliberately kept
// distinct because compatible endpoints may route to a different model.
type AgenticResponseIdentity struct {
	Provider       string `json:"provider"`
	Protocol       string `json:"protocol"`
	RequestedModel string `json:"requested_model,omitempty"`
	ReturnedModel  string `json:"returned_model,omitempty"`
	CorrelationID  string `json:"correlation_id,omitempty"`
}

// AgenticContinuationState is an opaque, versioned provider payload returned
// by a provider's SplitAgenticContinuation helper. Applications may persist it
// alongside the sanitized public message, but must not inspect or display it.
// The type intentionally has no String method to avoid accidental disclosure.
type AgenticContinuationState struct {
	Provider      string `json:"provider"`
	Protocol      string `json:"protocol"`
	Version       uint32 `json:"version"`
	CorrelationID string `json:"correlation_id,omitempty"`
	Opaque        []byte `json:"opaque,omitempty"`
}

const (
	DefaultAgenticMaxRequestBytes        int64 = 16 << 20
	DefaultAgenticMaxEventBytes          int64 = 2 << 20
	DefaultAgenticMaxResponseBytes       int64 = 32 << 20
	DefaultAgenticMaxInlineMediaBytes    int64 = 8 << 20
	DefaultAgenticMaxContentBlocks             = 1024
	DefaultAgenticMaxErrorBodyBytes      int64 = 1 << 20
	DefaultAgenticMaxFixtureCaptureBytes int64 = 32 << 20

	HardAgenticMaxRequestBytes        int64 = 64 << 20
	HardAgenticMaxEventBytes          int64 = 8 << 20
	HardAgenticMaxResponseBytes       int64 = 128 << 20
	HardAgenticMaxInlineMediaBytes    int64 = 32 << 20
	HardAgenticMaxContentBlocks             = 4096
	HardAgenticMaxErrorBodyBytes      int64 = 4 << 20
	HardAgenticMaxFixtureCaptureBytes int64 = 64 << 20
)

// AgenticLimits bounds request parsing and fixture capture. A zero field uses
// the documented default; negative values and values above hard caps are
// rejected by Validate.
type AgenticLimits struct {
	MaxRequestBytes        int64
	MaxEventBytes          int64
	MaxResponseBytes       int64
	MaxInlineMediaBytes    int64
	MaxContentBlocks       int
	MaxErrorBodyBytes      int64
	MaxFixtureCaptureBytes int64
}

// WithDefaults returns limits with every zero field replaced by its default.
func (l AgenticLimits) WithDefaults() AgenticLimits {
	if l.MaxRequestBytes == 0 {
		l.MaxRequestBytes = DefaultAgenticMaxRequestBytes
	}
	if l.MaxEventBytes == 0 {
		l.MaxEventBytes = DefaultAgenticMaxEventBytes
	}
	if l.MaxResponseBytes == 0 {
		l.MaxResponseBytes = DefaultAgenticMaxResponseBytes
	}
	if l.MaxInlineMediaBytes == 0 {
		l.MaxInlineMediaBytes = DefaultAgenticMaxInlineMediaBytes
	}
	if l.MaxContentBlocks == 0 {
		l.MaxContentBlocks = DefaultAgenticMaxContentBlocks
	}
	if l.MaxErrorBodyBytes == 0 {
		l.MaxErrorBodyBytes = DefaultAgenticMaxErrorBodyBytes
	}
	if l.MaxFixtureCaptureBytes == 0 {
		l.MaxFixtureCaptureBytes = DefaultAgenticMaxFixtureCaptureBytes
	}
	return l
}

// Validate resolves defaults and rejects invalid or unsafe hard-limit values.
func (l AgenticLimits) Validate() (AgenticLimits, error) {
	l = l.WithDefaults()
	checks := []struct {
		name        string
		value, hard int64
	}{
		{"request_bytes", l.MaxRequestBytes, HardAgenticMaxRequestBytes},
		{"event_bytes", l.MaxEventBytes, HardAgenticMaxEventBytes},
		{"response_bytes", l.MaxResponseBytes, HardAgenticMaxResponseBytes},
		{"inline_media_bytes", l.MaxInlineMediaBytes, HardAgenticMaxInlineMediaBytes},
		{"error_body_bytes", l.MaxErrorBodyBytes, HardAgenticMaxErrorBodyBytes},
		{"fixture_capture_bytes", l.MaxFixtureCaptureBytes, HardAgenticMaxFixtureCaptureBytes},
	}
	for _, check := range checks {
		if check.value < 0 || check.value > check.hard {
			return AgenticLimits{}, fmt.Errorf("agentic %s must be between 1 and %d", check.name, check.hard)
		}
	}
	if l.MaxContentBlocks < 0 || l.MaxContentBlocks > HardAgenticMaxContentBlocks {
		return AgenticLimits{}, fmt.Errorf("agentic content_blocks must be between 1 and %d", HardAgenticMaxContentBlocks)
	}
	return l, nil
}
