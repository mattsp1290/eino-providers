package opencodego

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"

	"github.com/cloudwego/eino/schema"
)

const maxObservedJSONBodyBytes = 16 << 20

var (
	errMalformedUsage           = errors.New("opencode-go: malformed usage observation")
	errOversizedJSONObservation = errors.New("opencode-go: response exceeds observation size limit")
)

type observedBodySource struct {
	io.ReadCloser
	closeOnce sync.Once
}

func newObservedBodySource(source io.ReadCloser) *observedBodySource {
	if source == nil {
		source = io.NopCloser(bytes.NewReader(nil))
	}
	return &observedBodySource{ReadCloser: source}
}

func (s *observedBodySource) Close() error {
	var err error
	s.closeOnce.Do(func() { err = s.ReadCloser.Close() })
	return err
}

func observedUsageTokenUsage(observation bodyObservation) *schema.TokenUsage {
	u := observation.usage
	if !u.input.present || !u.output.present {
		return nil
	}
	total := u.input.value + u.output.value
	if u.total.present {
		total = u.total.value
	}
	result := &schema.TokenUsage{
		PromptTokens:     u.input.value,
		CompletionTokens: u.output.value,
		TotalTokens:      total,
	}
	if u.cached.present {
		result.PromptTokenDetails.CachedTokens = u.cached.value
	}
	if u.reasoning.present {
		result.CompletionTokensDetails.ReasoningTokens = u.reasoning.value
	}
	return result
}

func observeJSONUsage(state *operationState, attempt uint64, protocol terminalProtocol, body []byte) error {
	usage, present, err := usageFromResponse(protocol, body)
	if err != nil {
		return err
	}
	state.updateBodyForAttempt(attempt, func(observation *bodyObservation) {
		observation.terminal = true
		if present {
			observation.usage = usage
		}
	})
	return nil
}

func observeSSEUsage(state *operationState, attempt uint64, protocol terminalProtocol, data []byte) error {
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return errMalformedSSE
	}

	switch protocol {
	case terminalChatCompletions:
		usage, present, err := usageFromRaw(envelope["usage"], terminalChatCompletions)
		if err != nil {
			return err
		}
		if present {
			state.updateBodyForAttempt(attempt, func(observation *bodyObservation) { observation.usage = usage })
		}
	case terminalMessages:
		var eventType string
		if raw := envelope["type"]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &eventType); err != nil {
				return errMalformedSSE
			}
		}
		switch eventType {
		case "message_start":
			var message map[string]json.RawMessage
			if err := json.Unmarshal(envelope["message"], &message); err != nil {
				return errMalformedUsage
			}
			usage, present, err := usageFromRaw(message["usage"], terminalMessages)
			if err != nil {
				return err
			}
			if present {
				state.updateBodyForAttempt(attempt, func(observation *bodyObservation) {
					observation.usage.input = usage.input
					observation.usage.cached = usage.cached
					observation.usage.total = usage.total
					if usage.output.present {
						observation.usage.output = usage.output
					}
				})
			}
		case "message_delta":
			usage, present, err := usageFromRaw(envelope["usage"], terminalMessages)
			if err != nil {
				return err
			}
			if present {
				state.updateBodyForAttempt(attempt, func(observation *bodyObservation) {
					if usage.output.present {
						if observation.usage.input.present && observation.usage.input.value > maxInt()-usage.output.value {
							observation.err = errMalformedUsage
							return
						}
						observation.usage.output = usage.output
					}
				})
			}
		}
	default:
		return errMalformedSSE
	}
	return nil
}

func usageFromResponse(protocol terminalProtocol, body []byte) (wireUsageObservation, bool, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return wireUsageObservation{}, false, errMalformedUsage
	}
	return usageFromRaw(response["usage"], protocol)
}

func usageFromRaw(raw json.RawMessage, protocol terminalProtocol) (wireUsageObservation, bool, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return wireUsageObservation{}, false, nil
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return wireUsageObservation{}, false, errMalformedUsage
	}
	var result wireUsageObservation
	var err error
	switch protocol {
	case terminalChatCompletions:
		if result.input, err = observedJSONCount(values, "prompt_tokens"); err != nil {
			return wireUsageObservation{}, false, err
		}
		if result.output, err = observedJSONCount(values, "completion_tokens"); err != nil {
			return wireUsageObservation{}, false, err
		}
		if result.total, err = observedJSONCount(values, "total_tokens"); err != nil {
			return wireUsageObservation{}, false, err
		}
		if result.cached, err = observedNestedJSONCount(values, "prompt_tokens_details", "cached_tokens"); err != nil {
			return wireUsageObservation{}, false, err
		}
		if result.reasoning, err = observedNestedJSONCount(values, "completion_tokens_details", "reasoning_tokens"); err != nil {
			return wireUsageObservation{}, false, err
		}
	case terminalMessages:
		base, err := observedJSONCount(values, "input_tokens")
		if err != nil {
			return wireUsageObservation{}, false, err
		}
		cacheRead, err := observedJSONCount(values, "cache_read_input_tokens")
		if err != nil {
			return wireUsageObservation{}, false, err
		}
		cacheCreate, err := observedJSONCount(values, "cache_creation_input_tokens")
		if err != nil {
			return wireUsageObservation{}, false, err
		}
		result.input = base
		if base.present {
			if cacheRead.present {
				if result.input.value > maxInt()-cacheRead.value {
					return wireUsageObservation{}, false, errMalformedUsage
				}
				result.input.value += cacheRead.value
			}
			if cacheCreate.present {
				if result.input.value > maxInt()-cacheCreate.value {
					return wireUsageObservation{}, false, errMalformedUsage
				}
				result.input.value += cacheCreate.value
			}
		}
		result.cached = cacheRead
		if result.output, err = observedJSONCount(values, "output_tokens"); err != nil {
			return wireUsageObservation{}, false, err
		}
	default:
		return wireUsageObservation{}, false, errMalformedUsage
	}
	if result.input.present && result.output.present && !result.total.present {
		if result.input.value > maxInt()-result.output.value {
			return wireUsageObservation{}, false, errMalformedUsage
		}
	}
	return result, true, nil
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func observedNestedJSONCount(values map[string]json.RawMessage, objectKey, countKey string) (observedCount, error) {
	raw, ok := values[objectKey]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return observedCount{}, nil
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nested); err != nil || nested == nil {
		return observedCount{}, errMalformedUsage
	}
	return observedJSONCount(nested, countKey)
}

func observedJSONCount(values map[string]json.RawMessage, key string) (observedCount, error) {
	raw, ok := values[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return observedCount{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return observedCount{}, errMalformedUsage
	}
	number, ok := decoded.(json.Number)
	if !ok {
		return observedCount{}, errMalformedUsage
	}
	value, err := strconv.ParseInt(number.String(), 10, 0)
	if err != nil || value < 0 {
		return observedCount{}, errMalformedUsage
	}
	return observedCount{value: int(value), present: true}, nil
}

type observedJSONBody struct {
	source    *observedBodySource
	protocol  terminalProtocol
	state     *operationState
	attempt   uint64
	mu        sync.Mutex
	buffer    []byte
	pending   error
	finalized bool
}

func newObservedJSONBody(source io.ReadCloser, protocol terminalProtocol, state *operationState) io.ReadCloser {
	return &observedJSONBody{source: newObservedBodySource(source), protocol: protocol, state: state, attempt: state.attemptID()}
}

func (o *observedJSONBody) Read(p []byte) (int, error) {
	o.mu.Lock()
	if o.pending != nil {
		err := o.pending
		o.pending = nil
		o.mu.Unlock()
		return 0, err
	}
	o.mu.Unlock()
	n, readErr := o.source.Read(p)
	o.mu.Lock()
	if o.finalized {
		o.mu.Unlock()
		return n, readErr
	}
	if remaining := maxObservedJSONBodyBytes - len(o.buffer); n > remaining {
		if remaining > 0 {
			o.buffer = append(o.buffer, p[:remaining]...)
		}
		o.recordError(errOversizedJSONObservation)
		o.pending = errOversizedJSONObservation
		o.finalized = true
		o.mu.Unlock()
		_ = o.closeSource()
		if remaining > 0 {
			return remaining, nil
		}
		return 0, o.takePending()
	}
	if n > 0 {
		o.buffer = append(o.buffer, p[:n]...)
	}
	if readErr != nil {
		if errors.Is(readErr, io.EOF) {
			o.finalized = true
			body := bytes.Clone(o.buffer)
			o.mu.Unlock()
			o.finalizeBody(body)
			o.mu.Lock()
		}
		if n > 0 {
			o.pending = readErr
			o.mu.Unlock()
			return n, nil
		}
	}
	o.mu.Unlock()
	return n, readErr
}

func (o *observedJSONBody) takePending() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	err := o.pending
	o.pending = nil
	return err
}

func (o *observedJSONBody) Close() error {
	o.finalize()
	return o.closeSource()
}

func (o *observedJSONBody) closeSource() error {
	return o.source.Close()
}

func (o *observedJSONBody) finalize() {
	o.mu.Lock()
	if o.finalized {
		o.mu.Unlock()
		return
	}
	o.finalized = true
	body := bytes.Clone(o.buffer)
	o.mu.Unlock()
	o.finalizeBody(body)
}

func (o *observedJSONBody) finalizeBody(body []byte) {
	if err := observeJSONUsage(o.state, o.attempt, o.protocol, body); err != nil {
		o.recordError(err)
	}
}

func (o *observedJSONBody) recordError(err error) {
	o.state.updateBodyForAttempt(o.attempt, func(observation *bodyObservation) {
		if observation.err == nil {
			observation.err = err
		}
	})
}

func normalizeGeneratedUsage(message *schema.Message, state *operationState) error {
	observation := state.bodySnapshot()
	if observation.err != nil {
		return observation.err
	}
	if message == nil {
		return nil
	}
	if message.ResponseMeta == nil {
		message.ResponseMeta = &schema.ResponseMeta{}
	}
	message.ResponseMeta.Usage = observedUsageTokenUsage(observation)
	return nil
}
