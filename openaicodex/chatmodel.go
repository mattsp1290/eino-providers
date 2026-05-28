package openaicodex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	codexauth "github.com/mattsp1290/codex-auth-go"

	einoproviders "github.com/mattsp1290/eino-providers"
)

const defaultReasoningEffort = "medium"

// newCodexChatClient is the seam for building a Codex-authenticated client for
// the ChatModel surface. It honours the caller-supplied AppName (unlike the
// legacy codexHTTPClient seam, which is pinned to "advisor").
var newCodexChatClient = func(ctx context.Context, appName string) (*http.Client, error) {
	return codexauth.NewClient(codexauth.Options{AppName: appName}).HTTPClient(ctx)
}

// ChatModelConfig configures a Codex Responses-API ToolCallingChatModel.
//
// The Codex OAuth transport owns endpoint rewriting, bearer-token injection, and
// refresh, so there is intentionally no BaseURL field and no output-token cap:
// the endpoint manages output length server-side.
type ChatModelConfig struct {
	// AppName selects the codex-auth-go on-disk credential directory and is used
	// to build an authenticated client when HTTPClient is nil. Credentials
	// resolve to the OS config dir (e.g. ~/Library/Application Support/<AppName>/
	// auth.json on macOS), not ~/.codex/auth.json.
	AppName string

	// Model is the Responses-API model slug, e.g. "gpt-5.5".
	Model string

	// HTTPClient, when non-nil, must be a codex-auth-go authenticated client. Its
	// transport rewrites every request to the Codex Responses endpoint and injects
	// the OAuth bearer plus the required session_id / originator / account headers;
	// a generic *http.Client will not work.
	HTTPClient *http.Client

	// ReasoningEffort is "low", "medium", or "high". Empty defaults to "medium"
	// unless DisableReasoning is set.
	ReasoningEffort string

	// DisableReasoning omits the reasoning request (reasoning: null, include: []).
	// Use for non-reasoning models. Reasoning models like gpt-5.x expect reasoning
	// items threaded across turns, so reasoning is enabled by default.
	DisableReasoning bool
}

type chatModel struct {
	httpClient *http.Client
	model      string
	reasoning  *reasoningParam
	include    []string
	tools      []*schema.ToolInfo
}

// NewChatModel constructs a Codex Responses-API ToolCallingChatModel. When
// cfg.HTTPClient is nil, it builds an authenticated client from codex-auth-go
// using cfg.AppName. codexauth.ErrNotLoggedIn is surfaced (wrapped as an init +
// auth error) so callers can prompt the user to log in.
func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error) {
	if cfg.HTTPClient != nil {
		return NewChatModelWithHTTPClient(ctx, cfg.HTTPClient, cfg)
	}
	client, err := newCodexChatClient(ctx, cfg.AppName)
	if err != nil {
		if errors.Is(err, codexauth.ErrNotLoggedIn) {
			return nil, einoproviders.WrapInitError(einoproviders.WrapAuthError(codexauth.ErrNotLoggedIn))
		}
		return nil, einoproviders.WrapInitError(fmt.Errorf("openai-codex: build client: %w", err))
	}
	return NewChatModelWithHTTPClient(ctx, client, cfg)
}

// NewChatModelWithHTTPClient constructs the ChatModel from a caller-supplied
// codex-auth-go authenticated client. See ChatModelConfig.HTTPClient.
func NewChatModelWithHTTPClient(_ context.Context, client *http.Client, cfg ChatModelConfig) (model.ToolCallingChatModel, error) {
	if client == nil {
		return nil, einoproviders.WrapInitError(errors.New("openai-codex: HTTPClient must not be nil"))
	}
	if cfg.Model == "" {
		return nil, einoproviders.WrapInitError(errors.New("openai-codex: Model is required"))
	}

	cm := &chatModel{
		httpClient: client,
		model:      cfg.Model,
		include:    []string{},
	}
	if !cfg.DisableReasoning {
		effort := cfg.ReasoningEffort
		if effort == "" {
			effort = defaultReasoningEffort
		}
		cm.reasoning = &reasoningParam{Effort: effort, Summary: "auto"}
		cm.include = []string{"reasoning.encrypted_content"}
	}
	return cm, nil
}

// WithTools returns a new ToolCallingChatModel with the given tools bound. It does
// not mutate the receiver, so a single base model can be shared across goroutines
// and derived per request. An empty tool set yields a tool-free clone.
func (m *chatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	// Validate tool schemas eagerly so misconfiguration surfaces at bind time.
	if _, err := buildToolsJSON(tools); err != nil {
		return nil, err
	}
	cp := *m
	if len(tools) == 0 {
		cp.tools = nil
	} else {
		cp.tools = append([]*schema.ToolInfo(nil), tools...)
	}
	return &cp, nil
}

// Generate runs a single turn and returns the complete assistant message. It
// drives the same streaming request as Stream (the Codex endpoint requires
// stream:true) and merges the chunks with schema.ConcatMessages.
func (m *chatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	sr, err := m.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	defer sr.Close()

	var chunks []*schema.Message
	for {
		chunk, recvErr := sr.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return nil, recvErr
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("openai-codex: empty response")
	}
	return schema.ConcatMessages(chunks)
}

// Stream issues a streaming Responses-API request and returns a reader of
// incremental assistant message chunks. An in-flight stream aborts when ctx is
// cancelled or when the consumer closes the reader (e.g. on client disconnect).
func (m *chatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	co := model.GetCommonOptions(&model.Options{Model: &m.model, Tools: m.tools}, opts...)

	body, err := m.buildRequest(co, input)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai-codex: marshal request: %w", err)
	}

	reqCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, codexauth.CodexEndpoint, bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("openai-codex: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	//nolint:gosec,bodyclose // URL is the constant codex endpoint; body is closed on the error path below and via defer in the stream goroutine.
	resp, err := m.httpClient.Do(req)
	if err != nil {
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("openai-codex: responses request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		cancel()
		return nil, classifyResponsesError(resp.StatusCode, errBody)
	}

	sr, sw := schema.Pipe[*schema.Message](16)
	go func() {
		defer cancel()
		defer func() { _ = resp.Body.Close() }()
		defer sw.Close()
		streamResponses(reqCtx, resp.Body, sw)
	}()
	return sr, nil
}

// buildRequest assembles the full Responses-API request: model, instructions
// (folded system messages), input items, tools, tool choice, and reasoning.
func (m *chatModel) buildRequest(co *model.Options, input []*schema.Message) (*responsesRequest, error) {
	modelName := m.model
	if co.Model != nil && *co.Model != "" {
		modelName = *co.Model
	}
	tools, err := buildToolsJSON(co.Tools)
	if err != nil {
		return nil, err
	}
	instructions, items, err := messagesToInput(input)
	if err != nil {
		return nil, err
	}
	return &responsesRequest{
		Model:             modelName,
		Instructions:      instructions,
		Input:             items,
		Tools:             tools,
		ToolChoice:        toolChoiceParam(co.ToolChoice),
		ParallelToolCalls: false,
		Reasoning:         m.reasoning,
		Store:             false,
		Stream:            true,
		Include:           m.include,
	}, nil
}

// toolChoiceParam maps Eino's tool choice to the Responses-API string. Codex
// defaults to "auto".
func toolChoiceParam(tc *schema.ToolChoice) string {
	if tc == nil {
		return "auto"
	}
	switch *tc {
	case schema.ToolChoiceForbidden:
		return "none"
	case schema.ToolChoiceForced:
		return "required"
	case schema.ToolChoiceAllowed:
		return "auto"
	default:
		return "auto"
	}
}
