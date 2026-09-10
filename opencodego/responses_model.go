package opencodego

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	opencodeauth "github.com/mattsp1290/opencode-auth-go"
)

var errResponsesStreamUnavailable = errors.New("opencode-go: Responses streaming adapter is not available")

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

func (m *responsesModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errResponsesStreamUnavailable
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
