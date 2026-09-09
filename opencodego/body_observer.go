package opencodego

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
)

const maxObservedSSEEventBytes = 10 << 20

var (
	errMalformedSSE       = errors.New("opencode-go: malformed streaming response")
	errOversizedSSE       = errors.New("opencode-go: streaming event exceeds size limit")
	errMissingSSETerminal = errors.New("opencode-go: streaming response ended before terminal event")
)

type terminalProtocol uint8

const (
	terminalChatCompletions terminalProtocol = iota + 1
	terminalMessages
)

// terminalBodyObserver passes SSE bytes through unchanged until a complete
// native terminal event. It then closes the network body and supplies EOF to
// the SDK without waiting for the peer to close the connection.
type terminalBodyObserver struct {
	source   *observedBodySource
	protocol terminalProtocol
	state    *operationState
	attempt  uint64
	closed   atomic.Bool
	terminal atomic.Bool

	line       []byte
	eventData  []byte
	eventRaw   []byte
	eventType  string
	eventBytes int
	output     []byte
	pendingErr error
}

func newTerminalBodyObserver(source io.ReadCloser, protocol terminalProtocol, states ...*operationState) io.ReadCloser {
	var state *operationState
	if len(states) > 0 {
		state = states[0]
	}
	return &terminalBodyObserver{source: newObservedBodySource(source), protocol: protocol, state: state, attempt: state.attemptID()}
}

func (o *terminalBodyObserver) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(o.output) == 0 && o.pendingErr == nil && !o.terminal.Load() && !o.closed.Load() {
		buffer := make([]byte, 32<<10)
		n, readErr := o.source.Read(buffer)
		for i := 0; i < n; i++ {
			o.eventRaw = append(o.eventRaw, buffer[i])
			complete, terminal, parseErr := o.consumeByte(buffer[i])
			if parseErr != nil {
				o.pendingErr = parseErr
				o.recordError(parseErr)
				o.eventRaw = o.eventRaw[:0]
				_ = o.closeSource()
				break
			}
			if complete {
				o.output = append(o.output, o.eventRaw...)
				o.eventRaw = o.eventRaw[:0]
			}
			if terminal {
				o.terminal.Store(true)
				o.state.updateBodyForAttempt(o.attempt, func(observation *bodyObservation) { observation.terminal = true })
				_ = o.closeSource()
				readErr = nil
				break
			}
		}
		if readErr != nil && !o.terminal.Load() && o.pendingErr == nil {
			o.pendingErr = o.streamReadError(readErr)
			o.recordError(o.pendingErr)
			_ = o.closeSource()
		}
		if n == 0 && readErr == nil {
			return 0, nil
		}
	}
	if len(o.output) > 0 {
		n := copy(p, o.output)
		o.output = o.output[n:]
		return n, nil
	}
	if o.pendingErr != nil {
		err := o.pendingErr
		o.pendingErr = nil
		return 0, err
	}
	return 0, io.EOF
}

func (o *terminalBodyObserver) Close() error {
	return o.closeSource()
}

func (o *terminalBodyObserver) closeSource() error {
	o.closed.Store(true)
	return o.source.Close()
}

func (o *terminalBodyObserver) streamReadError(err error) error {
	if o.terminal.Load() {
		return io.EOF
	}
	if errors.Is(err, io.EOF) {
		return errMissingSSETerminal
	}
	return err
}

func (o *terminalBodyObserver) recordError(err error) {
	o.state.updateBodyForAttempt(o.attempt, func(observation *bodyObservation) {
		if observation.err == nil {
			observation.err = err
		}
	})
}

func (o *terminalBodyObserver) consumeByte(b byte) (bool, bool, error) {
	o.eventBytes++
	if o.eventBytes > maxObservedSSEEventBytes {
		return false, false, errOversizedSSE
	}
	o.line = append(o.line, b)
	if b != '\n' {
		return false, false, nil
	}

	line := o.line[:len(o.line)-1]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	o.line = o.line[:0]
	if len(line) == 0 {
		terminal, err := o.finishEvent()
		o.eventData = o.eventData[:0]
		o.eventType = ""
		o.eventBytes = 0
		return true, terminal, err
	}
	if line[0] == ':' {
		return false, false, nil
	}

	name, value, found := bytes.Cut(line, []byte{':'})
	if !found {
		name, value = line, nil
	} else if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	switch string(name) {
	case "event":
		o.eventType = string(value)
	case "data":
		if len(o.eventData) > 0 {
			o.eventData = append(o.eventData, '\n')
		}
		o.eventData = append(o.eventData, value...)
	}
	return false, false, nil
}

func (o *terminalBodyObserver) finishEvent() (bool, error) {
	if len(o.eventData) == 0 && o.eventType == "" {
		return false, nil
	}
	switch o.protocol {
	case terminalChatCompletions:
		if bytes.Equal(o.eventData, []byte("[DONE]")) {
			return true, nil
		}
		if len(o.eventData) > 0 && !json.Valid(o.eventData) {
			return false, errMalformedSSE
		}
		if err := observeSSEUsage(o.state, o.attempt, o.protocol, o.eventData); err != nil {
			return false, err
		}
	case terminalMessages:
		if len(o.eventData) == 0 {
			if o.eventType == "message_stop" {
				return false, errMalformedSSE
			}
			return false, nil
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(o.eventData, &envelope); err != nil {
			return false, errMalformedSSE
		}
		if err := observeSSEUsage(o.state, o.attempt, o.protocol, o.eventData); err != nil {
			return false, err
		}
		if o.eventType == "message_stop" && envelope.Type != "message_stop" {
			return false, errMalformedSSE
		}
		if envelope.Type == "message_stop" && o.eventType != "message_stop" {
			return false, errMalformedSSE
		}
		if o.eventType == "message_stop" && envelope.Type == "message_stop" {
			return true, nil
		}
	default:
		return false, errMalformedSSE
	}
	return false, nil
}

var _ io.ReadCloser = (*terminalBodyObserver)(nil)
