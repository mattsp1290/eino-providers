package opencodego

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	opencodeauth "github.com/mattsp1290/opencode-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

type responsesModel struct {
	model      string
	maxTokens  *int
	tools      []*schema.ToolInfo
	httpClient *http.Client
	endpoint   string
}

var _ model.ToolCallingChatModel = (*responsesModel)(nil)

func newResponsesAdapter(_ context.Context, cfg preparedConfig) (model.ToolCallingChatModel, error) {
	httpClient, err := newObservedHTTPClient(cfg.authClient)
	if err != nil {
		return nil, err
	}
	endpoint, err := cfg.authClient.Endpoint(opencodeauth.ProtocolResponses)
	if err != nil {
		return nil, mapConstructorError(err)
	}
	return &responsesModel{model: cfg.model, maxTokens: snapshotMaxTokens(cfg.maxTokens), httpClient: httpClient, endpoint: endpoint}, nil
}

func (m *responsesModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	request, err := buildResponsesRequest(m.model, m.maxTokens, m.tools, input, false, opts...)
	if err != nil {
		return nil, err
	}
	payload, err := jsonMarshalResponsesRequest(request)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errInvalidResponsesRequest
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	response, err := m.httpClient.Do(httpRequest) //nolint:bodyclose // closed on every response path below
	if err != nil {
		closeResponse(response)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	if response == nil || response.Body == nil {
		closeResponse(response)
		return nil, responsesAPIFailure(errInvalidResponsesResponse)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = response.Body.Close()
		return nil, safeFailure(operationGenerate, errResponsesNotCompleted)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponsesBodyBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, responsesAPIFailure(readErr)
	}
	if closeErr != nil {
		return nil, responsesAPIFailure(closeErr)
	}
	if len(body) > maxResponsesBodyBytes {
		return nil, responsesAPIFailure(errResponsesBodyTooLarge)
	}
	return decodeResponsesResponse(body)
}

func jsonMarshalResponsesRequest(request *responsesRequest) ([]byte, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, errInvalidResponsesRequest
	}
	return payload, nil
}

func (m *responsesModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	request, err := buildResponsesRequest(m.model, m.maxTokens, m.tools, input, true, opts...)
	if err != nil {
		return nil, err
	}
	payload, err := jsonMarshalResponsesRequest(request)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestContext, cancel := context.WithCancel(ctx)
	httpRequest, err := http.NewRequestWithContext(requestContext, http.MethodPost, m.endpoint, bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, errInvalidResponsesRequest
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	response, err := m.httpClient.Do(httpRequest) //nolint:bodyclose // producer owns every successful response body
	if err != nil {
		cancel()
		_ = closeResponsesResponse(response)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	if response == nil || response.Body == nil {
		cancel()
		_ = closeResponsesResponse(response)
		return nil, responsesAPIFailure(errInvalidResponsesResponse)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		cancel()
		if closeErr := closeResponsesResponse(response); closeErr != nil {
			return nil, responsesStreamFailure(closeErr)
		}
		return nil, safeFailure(operationStream, errResponsesNotCompleted)
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "text/event-stream" {
		cancel()
		if closeErr := closeResponsesResponse(response); closeErr != nil {
			return nil, responsesStreamFailure(closeErr)
		}
		return nil, responsesAPIFailure(errInvalidResponsesResponse)
	}

	reader, writer := schema.Pipe[*schema.Message](16)
	body := newObservedBodySource(response.Body)
	stopCancellation := context.AfterFunc(requestContext, func() { _ = closeResponsesStreamBody(body) })
	go runResponsesStream(requestContext, cancel, body, stopCancellation, writer)
	return reader, nil
}

func runResponsesStream(ctx context.Context, cancel context.CancelFunc, body io.ReadCloser, stopCancellation func() bool, writer *schema.StreamWriter[*schema.Message]) {
	defer writer.Close()
	defer cancel()
	terminal, parseErr := parseResponsesStreamSafely(ctx, body, func(message *schema.Message) bool {
		return writer.Send(message, nil)
	})
	_ = stopCancellation()
	closeErr := closeResponsesStreamBody(body)
	if errors.Is(parseErr, errResponsesConsumerGone) {
		return
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = writer.Send(nil, ctxErr)
		return
	}
	if parseErr != nil {
		_ = writer.Send(nil, responsesStreamFailure(parseErr))
		return
	}
	if closeErr != nil {
		_ = writer.Send(nil, responsesStreamFailure(closeErr))
		return
	}
	_ = writer.Send(terminal, nil)
}

func parseResponsesStreamSafely(ctx context.Context, body io.Reader, send func(*schema.Message) bool) (terminal *schema.Message, err error) {
	defer func() {
		if recover() != nil {
			terminal = nil
			err = errAdapterStreamPanic
		}
	}()
	return parseResponsesStream(ctx, body, send)
}

func closeResponsesStreamBody(body io.Closer) (err error) {
	defer func() {
		if recover() != nil {
			err = errAdapterStreamPanic
		}
	}()
	return body.Close()
}

func closeResponsesResponse(response *http.Response) error {
	if response == nil || response.Body == nil {
		return nil
	}
	return closeResponsesStreamBody(response.Body)
}

func responsesStreamFailure(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, einoproviders.ErrProviderAPI) {
		return err
	}
	return responsesAPIFailure(err)
}

func (m *responsesModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	owned, err := cloneTools(tools)
	if err != nil {
		return nil, err
	}
	if _, err := buildResponsesTools(owned); err != nil {
		return nil, err
	}
	copy := *m
	copy.tools = owned
	return &copy, nil
}
