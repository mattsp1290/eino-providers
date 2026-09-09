package opencodego

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type chunkedReadCloser struct {
	data   []byte
	chunk  int
	closed int
}

func (b *chunkedReadCloser) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), b.chunk, len(b.data))
	copy(p, b.data[:n])
	b.data = b.data[n:]
	return n, nil
}

func (b *chunkedReadCloser) Close() error { b.closed++; return nil }

func TestTerminalBodyObserverPreservesBytesThroughTerminal(t *testing.T) {
	tests := []struct {
		name     string
		protocol terminalProtocol
		body     string
		want     string
	}{
		{
			name:     "chat CRLF and multiline",
			protocol: terminalChatCompletions,
			body:     ": ping\r\ndata: {\"choices\":\r\ndata: []}\r\n\r\ndata: [DONE]\r\n\r\ndata: ignored\r\n\r\n",
			want:     ": ping\r\ndata: {\"choices\":\r\ndata: []}\r\n\r\ndata: [DONE]\r\n\r\n",
		},
		{
			name:     "messages JSON terminal",
			protocol: terminalMessages,
			body:     "data: {\"type\":\"content_block_delta\"}\n\ndata: {\"type\":\"message_stop\"}\n\npost-terminal",
			want:     "data: {\"type\":\"content_block_delta\"}\n\ndata: {\"type\":\"message_stop\"}\n\n",
		},
		{
			name:     "messages event terminal",
			protocol: terminalMessages,
			body:     "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\nignored",
			want:     "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, chunk := range []int{1, 2, 7, 4096} {
				source := &chunkedReadCloser{data: []byte(tt.body), chunk: chunk}
				got, err := io.ReadAll(newTerminalBodyObserver(source, tt.protocol))
				if err != nil {
					t.Fatalf("chunk %d: ReadAll() error = %v", chunk, err)
				}
				if string(got) != tt.want {
					t.Fatalf("chunk %d: bytes = %q, want %q", chunk, got, tt.want)
				}
				if source.closed != 1 {
					t.Fatalf("chunk %d: close count = %d, want 1", chunk, source.closed)
				}
			}
		})
	}
}

func TestTerminalBodyObserverFailsIncompleteAndMalformedStreams(t *testing.T) {
	tests := []struct {
		name     string
		protocol terminalProtocol
		body     string
		wantErr  error
	}{
		{name: "premature EOF", protocol: terminalChatCompletions, body: "data: {\"choices\":[]}\n\n", wantErr: errMissingSSETerminal},
		{name: "unterminated marker", protocol: terminalChatCompletions, body: "data: [DONE]\n", wantErr: errMissingSSETerminal},
		{name: "malformed chat", protocol: terminalChatCompletions, body: "data: {bad}\n\n", wantErr: errMalformedSSE},
		{name: "mismatched message stop", protocol: terminalMessages, body: "event: message_stop\ndata: {\"type\":\"message_delta\"}\n\n", wantErr: errMalformedSSE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &chunkedReadCloser{data: []byte(tt.body), chunk: 3}
			_, err := io.ReadAll(newTerminalBodyObserver(source, tt.protocol))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReadAll() error = %v, want %v", err, tt.wantErr)
			}
			if source.closed != 1 {
				t.Fatalf("close count = %d, want 1", source.closed)
			}
		})
	}
}

func TestTerminalBodyObserverRejectsOversizedEvent(t *testing.T) {
	body := "data: " + strings.Repeat("x", maxObservedSSEEventBytes) + "\n\n"
	source := &chunkedReadCloser{data: []byte(body), chunk: 32 << 10}
	_, err := io.ReadAll(newTerminalBodyObserver(source, terminalChatCompletions))
	if !errors.Is(err, errOversizedSSE) {
		t.Fatalf("ReadAll() error = %v, want oversized event", err)
	}
	if source.closed != 1 {
		t.Fatalf("close count = %d, want 1", source.closed)
	}
}

func TestTerminalBodyObserverCloseIsIdempotent(t *testing.T) {
	source := &chunkedReadCloser{data: []byte("data: [DONE]\n\n"), chunk: 64}
	observer := newTerminalBodyObserver(source, terminalChatCompletions)
	if err := observer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := observer.Close(); err != nil {
		t.Fatal(err)
	}
	if source.closed != 1 {
		t.Fatalf("close count = %d, want 1", source.closed)
	}
}

func TestTerminalBodyObserverTerminalWinsOverReadEOF(t *testing.T) {
	source := &eofWithDataBody{data: []byte("data: [DONE]\n\ntrailing")}
	got, err := io.ReadAll(newTerminalBodyObserver(source, terminalChatCompletions))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("data: [DONE]\n\n")) {
		t.Fatalf("bytes = %q", got)
	}
}

type eofWithDataBody struct {
	data   []byte
	closed int
}

func (b *eofWithDataBody) Read(p []byte) (int, error) {
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, io.EOF
}

func (b *eofWithDataBody) Close() error { b.closed++; return nil }
