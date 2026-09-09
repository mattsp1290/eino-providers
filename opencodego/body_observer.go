package opencodego

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"
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
	source    io.ReadCloser
	protocol  terminalProtocol
	closeOnce sync.Once
	closed    atomic.Bool
	terminal  atomic.Bool

	line       []byte
	eventData  []byte
	eventType  string
	eventBytes int
	pendingErr error
}

func newTerminalBodyObserver(source io.ReadCloser, protocol terminalProtocol) io.ReadCloser {
	if source == nil {
		source = io.NopCloser(bytes.NewReader(nil))
	}
	return &terminalBodyObserver{source: source, protocol: protocol}
}

func (o *terminalBodyObserver) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if o.pendingErr != nil {
		err := o.pendingErr
		o.pendingErr = nil
		return 0, err
	}
	if o.terminal.Load() || o.closed.Load() {
		return 0, io.EOF
	}

	n, readErr := o.source.Read(p)
	cutoff := n
	for i := 0; i < n; i++ {
		terminal, parseErr := o.consumeByte(p[i])
		if parseErr != nil {
			cutoff = i + 1
			o.pendingErr = parseErr
			o.closeSource()
			break
		}
		if terminal {
			cutoff = i + 1
			o.terminal.Store(true)
			o.closeSource()
			readErr = nil
			break
		}
	}

	if cutoff > 0 {
		if cutoff == n && readErr != nil && !o.terminal.Load() && o.pendingErr == nil {
			o.pendingErr = o.streamReadError(readErr)
			o.closeSource()
		}
		return cutoff, nil
	}
	if o.pendingErr != nil {
		err := o.pendingErr
		o.pendingErr = nil
		return 0, err
	}
	if readErr != nil {
		err := o.streamReadError(readErr)
		o.closeSource()
		return 0, err
	}
	return 0, nil
}

func (o *terminalBodyObserver) Close() error {
	return o.closeSource()
}

func (o *terminalBodyObserver) closeSource() error {
	var err error
	o.closeOnce.Do(func() {
		o.closed.Store(true)
		err = o.source.Close()
	})
	return err
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

func (o *terminalBodyObserver) consumeByte(b byte) (bool, error) {
	o.eventBytes++
	if o.eventBytes > maxObservedSSEEventBytes {
		return false, errOversizedSSE
	}
	o.line = append(o.line, b)
	if b != '\n' {
		return false, nil
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
		return terminal, err
	}
	if line[0] == ':' {
		return false, nil
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
	return false, nil
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
		if o.eventType == "message_stop" && envelope.Type != "message_stop" {
			return false, errMalformedSSE
		}
		if envelope.Type == "message_stop" && o.eventType != "" && o.eventType != "message_stop" {
			return false, errMalformedSSE
		}
		if o.eventType == "message_stop" || envelope.Type == "message_stop" {
			return true, nil
		}
	default:
		return false, errMalformedSSE
	}
	return false, nil
}

var _ io.ReadCloser = (*terminalBodyObserver)(nil)
