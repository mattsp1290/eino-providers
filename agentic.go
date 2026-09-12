package einoproviders

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const agenticContinuationVersion = 1

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

// NewIdentifiedAgenticModel preserves a delegate's native extension while
// attaching the stable identity contract required by this module.
func NewIdentifiedAgenticModel(delegate model.AgenticModel, identity AgenticResponseIdentity) model.AgenticModel {
	return &identifiedAgenticModel{delegate: delegate, identity: identity}
}

type identifiedAgenticModel struct {
	delegate model.AgenticModel
	identity AgenticResponseIdentity
}

func (m *identifiedAgenticModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	message, err := m.delegate.Generate(ctx, input, opts...)
	if err == nil {
		m.attach(message, m.effectiveIdentity(opts...))
	}
	return message, err
}

func (m *identifiedAgenticModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	stream, err := m.delegate.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	identity := m.effectiveIdentity(opts...)
	return schema.StreamReaderWithConvert(stream, func(message *schema.AgenticMessage) (*schema.AgenticMessage, error) {
		m.attach(message, identity)
		return message, nil
	}), nil
}

func (m *identifiedAgenticModel) effectiveIdentity(opts ...model.Option) AgenticResponseIdentity {
	identity := m.identity
	common := model.GetCommonOptions(&model.Options{}, opts...)
	if common.Model != nil {
		identity.RequestedModel = *common.Model
	}
	return identity
}

func (m *identifiedAgenticModel) attach(message *schema.AgenticMessage, identity AgenticResponseIdentity) {
	if message == nil {
		return
	}
	if message.ResponseMeta == nil {
		message.ResponseMeta = &schema.AgenticResponseMeta{}
	}
	native := message.ResponseMeta.Extension
	message.ResponseMeta.Extension = AgenticResponseMetadata{Identity: identity, Native: native}
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

type agenticContinuationPayload struct {
	MessageExtra map[string]json.RawMessage   `json:"message_extra,omitempty"`
	BlockExtra   []map[string]json.RawMessage `json:"block_extra,omitempty"`
	Signatures   map[int]string               `json:"reasoning_signatures,omitempty"`
}

// SplitAgenticContinuationForProvider creates an immutable display-safe
// projection and a provider-tagged opaque continuation envelope. It removes
// extras and reasoning signatures from the public clone only; callers must
// retain the returned state to resume provider-native execution.
func SplitAgenticContinuationForProvider(msg *schema.AgenticMessage, provider, protocol string) (*schema.AgenticMessage, AgenticContinuationState, error) {
	if msg == nil {
		return nil, AgenticContinuationState{}, fmt.Errorf("%s: nil agentic message", provider)
	}
	if provider == "" || protocol == "" {
		return nil, AgenticContinuationState{}, fmt.Errorf("agentic continuation requires provider and protocol")
	}
	public, err := cloneAgenticMessageForContinuation(msg)
	if err != nil {
		return nil, AgenticContinuationState{}, err
	}
	payload := agenticContinuationPayload{BlockExtra: make([]map[string]json.RawMessage, len(msg.ContentBlocks)), Signatures: map[int]string{}}
	if payload.MessageExtra, err = marshalContinuationExtra(msg.Extra); err != nil {
		return nil, AgenticContinuationState{}, err
	}
	public.Extra = nil
	for i, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if payload.BlockExtra[i], err = marshalContinuationExtra(block.Extra); err != nil {
			return nil, AgenticContinuationState{}, err
		}
		if public.ContentBlocks[i] == nil {
			continue
		}
		public.ContentBlocks[i].Extra = nil
		if reasoning := public.ContentBlocks[i].Reasoning; reasoning != nil {
			payload.Signatures[i] = reasoning.Signature
			reasoning.Signature = ""
		}
	}
	if len(payload.Signatures) == 0 {
		payload.Signatures = nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, AgenticContinuationState{}, fmt.Errorf("%s: encode continuation: %w", provider, err)
	}
	return public, AgenticContinuationState{Provider: provider, Protocol: protocol, Version: agenticContinuationVersion, CorrelationID: continuationCorrelationID(msg), Opaque: encoded}, nil
}

// RestoreAgenticContinuationForProvider recombines a public clone and opaque
// provider state without mutating either input.
func RestoreAgenticContinuationForProvider(public *schema.AgenticMessage, state AgenticContinuationState, provider, protocol string) (*schema.AgenticMessage, error) {
	if public == nil {
		return nil, fmt.Errorf("%s: nil public agentic message", provider)
	}
	if state.Provider != provider || state.Protocol != protocol || state.Version != agenticContinuationVersion {
		return nil, fmt.Errorf("%s: incompatible agentic continuation state", provider)
	}
	identity, hasIdentity := continuationIdentity(public)
	if state.CorrelationID != "" && (!hasIdentity || identity.CorrelationID != state.CorrelationID) {
		return nil, fmt.Errorf("%s: continuation correlation mismatch", provider)
	}
	var payload agenticContinuationPayload
	if err := json.Unmarshal(state.Opaque, &payload); err != nil {
		return nil, fmt.Errorf("%s: decode continuation: %w", provider, err)
	}
	if len(payload.BlockExtra) != 0 && len(payload.BlockExtra) != len(public.ContentBlocks) {
		return nil, fmt.Errorf("%s: continuation block count mismatch", provider)
	}
	restored, err := cloneAgenticMessageForContinuation(public)
	if err != nil {
		return nil, err
	}
	if hasIdentity && restored.ResponseMeta != nil {
		restored.ResponseMeta.Extension = identity
	}
	restored.Extra, err = unmarshalContinuationExtra(payload.MessageExtra)
	if err != nil {
		return nil, err
	}
	for i := range restored.ContentBlocks {
		if restored.ContentBlocks[i] == nil {
			continue
		}
		if len(payload.BlockExtra) != 0 {
			restored.ContentBlocks[i].Extra, err = unmarshalContinuationExtra(payload.BlockExtra[i])
			if err != nil {
				return nil, err
			}
		}
		if signature, ok := payload.Signatures[i]; ok && restored.ContentBlocks[i].Reasoning != nil {
			restored.ContentBlocks[i].Reasoning.Signature = signature
		}
	}
	return restored, nil
}

func cloneAgenticMessageForContinuation(msg *schema.AgenticMessage) (*schema.AgenticMessage, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("clone agentic message: %w", err)
	}
	var clone schema.AgenticMessage
	if err := json.Unmarshal(b, &clone); err != nil {
		return nil, fmt.Errorf("clone agentic message: %w", err)
	}
	return &clone, nil
}

func marshalContinuationExtra(extra map[string]any) (map[string]json.RawMessage, error) {
	if len(extra) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(extra)
	if err != nil {
		return nil, fmt.Errorf("encode continuation extra: %w", err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func unmarshalContinuationExtra(extra map[string]json.RawMessage) (map[string]any, error) {
	if len(extra) == 0 {
		return nil, nil
	}
	result := make(map[string]any, len(extra))
	for key, value := range extra {
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, fmt.Errorf("decode continuation extra: %w", err)
		}
		result[key] = decoded
	}
	return result, nil
}

func continuationCorrelationID(msg *schema.AgenticMessage) string {
	identity, ok := continuationIdentity(msg)
	if !ok {
		return ""
	}
	return identity.CorrelationID
}

func continuationIdentity(msg *schema.AgenticMessage) (AgenticResponseIdentity, bool) {
	if msg == nil || msg.ResponseMeta == nil {
		return AgenticResponseIdentity{}, false
	}
	switch extension := msg.ResponseMeta.Extension.(type) {
	case AgenticResponseIdentity:
		return extension, true
	case AgenticResponseMetadata:
		return extension.Identity, true
	case map[string]any:
		if nested, ok := extension["identity"].(map[string]any); ok {
			return identityFromMap(nested)
		}
		return identityFromMap(extension)
	default:
		return AgenticResponseIdentity{}, false
	}
}

func identityFromMap(values map[string]any) (AgenticResponseIdentity, bool) {
	provider, providerOK := values["provider"].(string)
	protocol, protocolOK := values["protocol"].(string)
	if !providerOK || !protocolOK {
		return AgenticResponseIdentity{}, false
	}
	identity := AgenticResponseIdentity{Provider: provider, Protocol: protocol}
	identity.RequestedModel, _ = values["requested_model"].(string)
	identity.ReturnedModel, _ = values["returned_model"].(string)
	identity.CorrelationID, _ = values["correlation_id"].(string)
	return identity, true
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
	seen := make(map[int]struct{})
	count := 0
	return schema.StreamReaderWithConvert(stream, func(message *schema.AgenticMessage) (*schema.AgenticMessage, error) {
		if err := validateStreamAgenticContentBlocks(message, m.limits, seen, &count); err != nil {
			return nil, err
		}
		return message, nil
	}), nil
}

func validateStreamAgenticContentBlocks(message *schema.AgenticMessage, limits AgenticLimits, seen map[int]struct{}, count *int) error {
	if message == nil {
		return nil
	}
	for _, block := range message.ContentBlocks {
		if block == nil {
			continue
		}
		if block.StreamingMeta != nil {
			if _, exists := seen[block.StreamingMeta.Index]; exists {
				continue
			}
			seen[block.StreamingMeta.Index] = struct{}{}
		}
		if err := validateInlineMediaBlock(block, limits); err != nil {
			return err
		}
		*count = *count + 1
		if *count > limits.MaxContentBlocks {
			return &ResourceLimitError{Resource: "content_blocks", Limit: int64(limits.MaxContentBlocks), Actual: int64(*count)}
		}
	}
	return nil
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
		for _, block := range message.ContentBlocks {
			if block == nil {
				continue
			}
			count++
			if count > resolved.MaxContentBlocks {
				return &ResourceLimitError{Resource: "content_blocks", Limit: int64(resolved.MaxContentBlocks), Actual: int64(count)}
			}
			if err := validateInlineMediaBlock(block, resolved); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateInlineMediaBlock(block *schema.ContentBlock, limits AgenticLimits) error {
	if block == nil {
		return nil
	}
	media := make([]string, 0, 6)
	if value := block.UserInputImage; value != nil {
		media = append(media, value.Base64Data)
	}
	if value := block.UserInputAudio; value != nil {
		media = append(media, value.Base64Data)
	}
	if value := block.UserInputVideo; value != nil {
		media = append(media, value.Base64Data)
	}
	if value := block.UserInputFile; value != nil {
		media = append(media, value.Base64Data)
	}
	if value := block.AssistantGenImage; value != nil {
		media = append(media, value.Base64Data)
	}
	if value := block.AssistantGenAudio; value != nil {
		media = append(media, value.Base64Data)
	}
	if value := block.AssistantGenVideo; value != nil {
		media = append(media, value.Base64Data)
	}
	if result := block.FunctionToolResult; result != nil {
		for _, content := range result.Content {
			if content.Image != nil {
				media = append(media, content.Image.Base64Data)
			}
			if content.Audio != nil {
				media = append(media, content.Audio.Base64Data)
			}
			if content.Video != nil {
				media = append(media, content.Video.Base64Data)
			}
			if content.File != nil {
				media = append(media, content.File.Base64Data)
			}
		}
	}
	for _, encoded := range media {
		if decoded := decodedBase64Size(encoded); decoded > limits.MaxInlineMediaBytes {
			return &ResourceLimitError{Resource: "inline_media_bytes", Limit: limits.MaxInlineMediaBytes, Actual: decoded}
		}
	}
	return nil
}

func decodedBase64Size(encoded string) int64 {
	if encoded == "" {
		return 0
	}
	length := len(encoded)
	padding := 0
	if encoded[length-1] == '=' {
		padding++
	}
	if length > 1 && encoded[length-2] == '=' {
		padding++
	}
	if padding != 0 {
		return int64((length/4)*3 - padding)
	}
	decoded := (length / 4) * 3
	switch length % 4 {
	case 2:
		decoded++
	case 3:
		decoded += 2
	case 1:
		// Invalid encodings are rejected later by the provider codec. Count a
		// conservative upper bound here so they cannot bypass this boundary.
		decoded++
	}
	return int64(decoded)
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
