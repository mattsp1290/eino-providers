package opencodego

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"
)

const sanitizedNativeError = `{"error":{"message":"OpenCode Go request failed","type":"api_error","code":"opencode_go_error"}}`

const messagesSDKRoutePrefix = "/.opencodego/messages-sdk/"

// newMessagesHTTPClient gives the Anthropic SDK a base URL that composes to
// the auth client's exact Messages endpoint. Roots ending in /v1 compose
// directly. Other valid roots use one private synthetic SDK route which is
// mapped to the native endpoint before authentication.
func newMessagesHTTPClient(authClient *opencodeauth.Client) (*http.Client, string, error) {
	httpClient, err := newObservedHTTPClient(authClient)
	if err != nil {
		return nil, "", err
	}
	base, err := url.Parse(authClient.BaseURL())
	if err != nil {
		return nil, "", mapConstructorError(err)
	}
	targetText, err := authClient.Endpoint(opencodeauth.ProtocolMessages)
	if err != nil {
		return nil, "", mapConstructorError(err)
	}
	target, err := url.Parse(targetText)
	if err != nil {
		return nil, "", mapConstructorError(err)
	}

	if path.Base(base.Path) == "v1" {
		sdkBase := *base
		sdkBase.Path = strings.TrimSuffix(base.Path, "v1")
		sdkBase.RawPath = ""
		sdkBase.RawQuery = ""
		sdkBase.Fragment = ""
		return httpClient, sdkBase.String(), nil
	}

	sdkBase := &url.URL{Scheme: base.Scheme, Host: base.Host, Path: messagesSDKRoutePrefix}
	source := sdkBase.ResolveReference(&url.URL{Path: "v1/messages"})
	httpClient.Transport = &exactRouteTransport{
		next:   httpClient.Transport,
		source: source,
		target: target,
	}
	return httpClient, sdkBase.String(), nil
}

// exactRouteTransport is private to the Messages adapter. It rejects every
// SDK route except the one request path that the adapter supports.
type exactRouteTransport struct {
	next   http.RoundTripper
	source *url.URL
	target *url.URL
}

func (t *exactRouteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || t == nil || t.next == nil || t.source == nil || t.target == nil {
		closeRequestBody(req)
		return nil, opencodeauth.ErrDisallowedRequest
	}
	if req.Method != http.MethodPost || req.URL.String() != t.source.String() || (req.Host != "" && req.Host != t.source.Host) {
		closeRequestBody(req)
		return nil, opencodeauth.ErrDisallowedRequest
	}
	mapped := req.Clone(req.Context())
	target := *t.target
	mapped.URL = &target
	return t.next.RoundTrip(mapped)
}

func closeRequestBody(req *http.Request) {
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
}

func (t *exactRouteTransport) CloseIdleConnections() {
	if t == nil {
		return
	}
	if closer, ok := t.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

var _ http.RoundTripper = (*exactRouteTransport)(nil)

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
		observeResponseBody(req, resp, state)
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

func observeResponseBody(req *http.Request, resp *http.Response, state *operationState) {
	if req == nil || resp == nil || resp.Body == nil {
		return
	}
	path := strings.TrimSuffix(req.URL.Path, "/")
	var protocol terminalProtocol
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		protocol = terminalChatCompletions
	case strings.HasSuffix(path, "/messages"):
		protocol = terminalMessages
	default:
		return
	}

	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err == nil && strings.EqualFold(mediaType, "text/event-stream") {
		resp.Body = newTerminalBodyObserver(resp.Body, protocol, state)
		return
	}
	resp.Body = newObservedJSONBody(resp.Body, protocol, state)
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
