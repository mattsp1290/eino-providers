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

	statusCode := resp.StatusCode
	decoded := opencodeauth.DecodeHTTPError(resp)
	var httpErr *opencodeauth.HTTPError
	if !errors.As(decoded, &httpErr) {
		httpErr = &opencodeauth.HTTPError{
			StatusCode: statusCode,
			Kind:       opencodeauth.ErrorKindUnknown,
		}
	}
	state.recordHTTPError(httpErr)
	return sanitizedErrorResponse(resp), nil
}

func sanitizedErrorResponse(source *http.Response) *http.Response {
	body := []byte(sanitizedNativeError)
	copy := new(http.Response)
	*copy = *source
	copy.Header = make(http.Header)
	copyHeaderValues(source.Header, copy.Header, "Retry-After")
	copyHeaderValues(source.Header, copy.Header, "X-Should-Retry")
	copy.Header.Set("Content-Type", "application/json")
	copy.Body = io.NopCloser(bytes.NewReader(body))
	copy.ContentLength = int64(len(body))
	copy.TransferEncoding = nil
	copy.Trailer = nil
	copy.Uncompressed = false
	return copy
}

func copyHeaderValues(source, destination http.Header, name string) {
	for key, values := range source {
		if strings.EqualFold(key, name) {
			for _, value := range values {
				destination.Add(name, value)
			}
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
