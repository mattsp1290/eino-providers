package opencodego

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"
)

const sanitizedNativeError = `{"error":{"message":"OpenCode Go request failed","type":"api_error","code":"opencode_go_error"}}`

// newObservedHTTPClient copies the auth client's fresh HTTP client and adds
// local response observation outside its authenticated transport. Client
// policy such as Timeout, Jar, and redirect rejection remains unchanged.
func newObservedHTTPClient(authClient *opencodeauth.Client) (*http.Client, error) {
	if authClient == nil {
		return nil, mapConstructorError(opencodeauth.ErrInvalidConfiguration)
	}
	authHTTPClient := authClient.HTTPClient()
	if authHTTPClient == nil || authHTTPClient.Transport == nil {
		return nil, mapConstructorError(opencodeauth.ErrInvalidConfiguration)
	}
	copy := *authHTTPClient
	copy.Transport = &observingTransport{next: authHTTPClient.Transport}
	return &copy, nil
}

type observingTransport struct {
	next http.RoundTripper
}

func (t *observingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, opencodeauth.ErrDisallowedRequest
	}
	state := operationStateFromContext(req.Context())
	state.beginAttempt()

	if t == nil || t.next == nil {
		return nil, opencodeauth.ErrInvalidConfiguration
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		closeResponse(resp)
		if contextErr := req.Context().Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("opencode-go transport returned no response")
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return resp, nil
	}

	decoded := opencodeauth.DecodeHTTPError(resp)
	var httpErr *opencodeauth.HTTPError
	if errors.As(decoded, &httpErr) {
		state.recordHTTPError(httpErr)
	}
	return sanitizedErrorResponse(resp), nil
}

func sanitizedErrorResponse(source *http.Response) *http.Response {
	body := []byte(sanitizedNativeError)
	copy := new(http.Response)
	*copy = *source
	copy.Header = source.Header.Clone()
	if copy.Header == nil {
		copy.Header = make(http.Header)
	}
	removeHeader(copy.Header, "Content-Type")
	removeHeader(copy.Header, "Content-Encoding")
	removeHeader(copy.Header, "Content-Length")
	copy.Header.Set("Content-Type", "application/json")
	copy.Body = io.NopCloser(bytes.NewReader(body))
	copy.ContentLength = int64(len(body))
	copy.TransferEncoding = nil
	copy.Trailer = nil
	copy.Uncompressed = false
	return copy
}

func removeHeader(header http.Header, name string) {
	for key := range header {
		if strings.EqualFold(key, name) {
			delete(header, key)
		}
	}
}

func closeResponse(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func (t *observingTransport) CloseIdleConnections() {
	if t == nil {
		return
	}
	if closer, ok := t.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

var _ http.RoundTripper = (*observingTransport)(nil)
