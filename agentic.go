package einoproviders

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

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

// AgenticResponseMetadata preserves a provider adapter's native response
// extension while adding the stable identity fields exposed by this module.
// It is used where a protocol adapter already returned extension data that
// callers may rely on; replacing that data would lose correlation details.
type AgenticResponseMetadata struct {
	Identity AgenticResponseIdentity `json:"identity"`
	Native   any                     `json:"native,omitempty"`
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

// NewBoundedAgenticModel adds pre-dispatch and returned-message content-block
// validation to a native model. It complements the HTTP limiter, which owns
// byte boundaries at the transport layer.
func NewBoundedAgenticModel(delegate model.AgenticModel, limits AgenticLimits) (model.AgenticModel, error) {
	if delegate == nil {
		return nil, fmt.Errorf("agentic delegate is required")
	}
	resolved, err := limits.Validate()
	if err != nil {
		return nil, err
	}
	return &boundedAgenticModel{delegate: delegate, limits: resolved}, nil
}

type boundedAgenticModel struct {
	delegate model.AgenticModel
	limits   AgenticLimits
}

func (m *boundedAgenticModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	if err := ValidateAgenticContentBlocks(input, m.limits); err != nil {
		return nil, err
	}
	result, err := m.delegate.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	if err := ValidateAgenticContentBlocks([]*schema.AgenticMessage{result}, m.limits); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *boundedAgenticModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	if err := ValidateAgenticContentBlocks(input, m.limits); err != nil {
		return nil, err
	}
	stream, err := m.delegate.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderWithConvert(stream, func(message *schema.AgenticMessage) (*schema.AgenticMessage, error) {
		if err := ValidateAgenticContentBlocks([]*schema.AgenticMessage{message}, m.limits); err != nil {
			return nil, err
		}
		return message, nil
	}), nil
}

// ValidateAgenticContentBlocks rejects a message collection whose total block
// count exceeds the configured boundary without inspecting private block data.
func ValidateAgenticContentBlocks(messages []*schema.AgenticMessage, limits AgenticLimits) error {
	resolved, err := limits.Validate()
	if err != nil {
		return err
	}
	count := 0
	for _, message := range messages {
		if message == nil {
			continue
		}
		count += len(message.ContentBlocks)
		if count > resolved.MaxContentBlocks {
			return &ResourceLimitError{Resource: "content_blocks", Limit: int64(resolved.MaxContentBlocks), Actual: int64(count)}
		}
	}
	return nil
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
