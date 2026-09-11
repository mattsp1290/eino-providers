package einoproviders

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// NewAgenticLimitedHTTPClient returns a copy of source whose transport bounds
// native agentic request, successful response, and error response bodies. It
// never mutates the caller-owned client or transport. Protocol decoders remain
// responsible for enforcing framing-specific limits such as a single SSE event.
func NewAgenticLimitedHTTPClient(source *http.Client, limits AgenticLimits) (*http.Client, error) {
	resolved, err := limits.Validate()
	if err != nil {
		return nil, err
	}
	if source == nil {
		source = http.DefaultClient
	}
	copy := *source
	next := source.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	copy.Transport = &agenticLimitTransport{next: next, limits: resolved}
	return &copy, nil
}

type agenticLimitTransport struct {
	next   http.RoundTripper
	limits AgenticLimits
}

func (t *agenticLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("agentic limited transport: nil request")
	}
	if req.ContentLength > t.limits.MaxRequestBytes {
		closeRequest(req)
		return nil, &ResourceLimitError{Resource: "request_bytes", Limit: t.limits.MaxRequestBytes, Actual: req.ContentLength}
	}
	if req.Body != nil && req.Body != http.NoBody {
		req.Body = &agenticLimitedBody{ReadCloser: req.Body, resource: "request_bytes", limit: t.limits.MaxRequestBytes}
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	limit, resource := t.limits.MaxResponseBytes, "response_bytes"
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		limit, resource = t.limits.MaxErrorBodyBytes, "error_body_bytes"
	}
	if resp.ContentLength > limit {
		_ = resp.Body.Close()
		return nil, &ResourceLimitError{Resource: resource, Limit: limit, Actual: resp.ContentLength}
	}
	resp.Body = &agenticLimitedBody{ReadCloser: resp.Body, resource: resource, limit: limit}
	if resource == "response_bytes" {
		mediaType, _, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if parseErr == nil && (strings.EqualFold(mediaType, "text/event-stream") || strings.EqualFold(mediaType, "application/x-ndjson")) {
			resp.Body = &agenticEventLimitedBody{ReadCloser: resp.Body, limit: t.limits.MaxEventBytes, ndjson: strings.EqualFold(mediaType, "application/x-ndjson")}
		}
	}
	return resp, nil
}

func (t *agenticLimitTransport) CloseIdleConnections() {
	if closer, ok := t.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type agenticLimitedBody struct {
	io.ReadCloser
	resource string
	limit    int64
	read     int64
}

// agenticEventLimitedBody caps one complete SSE record (blank-line delimited)
// or one NDJSON record. The wrapper aborts before exposing the byte that would
// exceed the cap and relies on the enclosing response limiter for aggregate
// accounting.
type agenticEventLimitedBody struct {
	io.ReadCloser
	limit     int64
	read      int64
	ndjson    bool
	lineEmpty bool
}

func (b *agenticEventLimitedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	for i := 0; i < n; i++ {
		b.read++
		if b.read > b.limit {
			return i, &ResourceLimitError{Resource: "event_bytes", Limit: b.limit, Actual: b.read}
		}
		if b.ndjson {
			if p[i] == '\n' {
				b.read = 0
			}
			continue
		}
		if p[i] == '\n' {
			if b.lineEmpty {
				b.read = 0
				b.lineEmpty = false
			} else {
				b.lineEmpty = true
			}
			continue
		}
		if p[i] != '\r' {
			b.lineEmpty = false
		}
	}
	return n, err
}

func (b *agenticLimitedBody) Read(p []byte) (int, error) {
	if b.read >= b.limit {
		var probe [1]byte
		n, err := b.ReadCloser.Read(probe[:])
		if n > 0 {
			return 0, &ResourceLimitError{Resource: b.resource, Limit: b.limit, Actual: b.read + int64(n)}
		}
		return 0, err
	}
	remaining := b.limit - b.read
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	if err == io.EOF || err != nil {
		return n, err
	}
	return n, nil
}

func closeRequest(req *http.Request) {
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
}

var _ http.RoundTripper = (*agenticLimitTransport)(nil)
