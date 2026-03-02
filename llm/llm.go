// Package llm provides a portable LLM client interface with implementations
// for the OpenRouter Anthropic Messages API, Chat Completions API, and Responses API.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Provider sends messages to an LLM and returns responses.
// Implementations: MessagesClient (Anthropic Messages API) and
// CompletionsClient (OpenAI-compatible Chat Completions API).
type Provider interface {
	// SendMessage sends a conversation to the LLM and returns the response.
	// The returned Response contains text content and/or tool calls.
	SendMessage(ctx context.Context, req *Request) (*Response, error)
}

// Role represents a message role in the conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// StopReason indicates why the model stopped generating.
type StopReason string

const (
	StopReasonEndTurn      StopReason = "end_turn"
	StopReasonMaxTokens    StopReason = "max_tokens"
	StopReasonStopSequence StopReason = "stop_sequence"
	StopReasonToolUse      StopReason = "tool_use"
)

// Request holds the parameters for a SendMessage call.
type Request struct {
	Model         string
	MaxTokens     int
	System        string
	Messages      []Message
	Tools         []Tool
	Temperature   *float64
	TopP          *float64
	StopSequences []string
}

// Message represents a single message in the conversation history.
// For user messages, set Role and Content.
// For assistant messages with tool calls, ToolCalls will be populated.
// For tool results, set Role=RoleUser and populate ToolResults.
type Message struct {
	Role        Role
	Content     string
	ToolCalls   []ToolCall
	ToolResults []ToolResult
}

// ToolCall represents a tool invocation requested by the assistant.
type ToolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolResult represents the result of executing a tool.
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// Tool describes a tool available to the model.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema object
}

// Response holds the model's response.
type Response struct {
	ID         string
	Content    string     // Aggregated text from all text content blocks.
	ToolCalls  []ToolCall // Tool calls requested by the model.
	StopReason StopReason
	Model      string
	Usage      Usage
}

// Usage holds token usage information.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Config holds configuration for creating a Provider.
type Config struct {
	APIKey      string        // Required: OpenRouter API key.
	BaseURL     string        // Base URL (defaults to https://openrouter.ai/api/v1).
	HTTPTimeout time.Duration // HTTP client timeout (defaults to 120s).
	HTTPReferer string        // Optional HTTP-Referer header.
	AppTitle    string        // Optional X-Title header.
}

func (c *Config) defaults() {
	if c.BaseURL == "" {
		c.BaseURL = "https://openrouter.ai/api/v1"
	}
	if c.HTTPTimeout == 0 {
		c.HTTPTimeout = 120 * time.Second
	}
}

// Errors returned by the package.
var (
	ErrMissingAPIKey = errors.New("llm: API key is required")
	ErrEmptyResponse = errors.New("llm: empty response content")
	ErrNoChoices     = errors.New("llm: no choices in response")
)

// APIError represents an error response from the API.
type APIError struct {
	StatusCode int
	Type       string // Error type from the API (e.g. "invalid_request_error").
	Message    string // Human-readable error message.
	RawBody    string // Raw response body if parsing failed.
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("llm: API error %d (%s): %s", e.StatusCode, e.Type, e.Message)
	}
	return fmt.Sprintf("llm: API error %d: %s", e.StatusCode, e.RawBody)
}

// --- Anthropic Messages API Implementation ---

// MessagesClient implements Provider using the OpenRouter Anthropic Messages API.
// Endpoint: POST {BaseURL}/messages
type MessagesClient struct {
	config     Config
	httpClient *http.Client
}

// NewMessagesClient creates a new MessagesClient.
func NewMessagesClient(cfg Config) (*MessagesClient, error) {
	if cfg.APIKey == "" {
		return nil, ErrMissingAPIKey
	}
	cfg.defaults()
	return &MessagesClient{
		config:     cfg,
		httpClient: &http.Client{Timeout: cfg.HTTPTimeout},
	}, nil
}

// SendMessage implements Provider by calling the Anthropic Messages endpoint.
func (c *MessagesClient) SendMessage(ctx context.Context, req *Request) (*Response, error) {
	wireReq := c.buildWireRequest(req)

	body, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if c.config.HTTPReferer != "" {
		httpReq.Header.Set("HTTP-Referer", c.config.HTTPReferer)
	}
	if c.config.AppTitle != "" {
		httpReq.Header.Set("X-Title", c.config.AppTitle)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: http request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, parseAPIError(httpResp.StatusCode, respBody)
	}

	var wireResp messagesResponse
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return nil, fmt.Errorf("llm: decode response: %w", err)
	}

	return wireResp.toResponse(), nil
}

// --- Wire format types (unexported) ---

type messagesRequest struct {
	Model         string              `json:"model"`
	MaxTokens     int                 `json:"max_tokens"`
	Messages      []messagesMessage   `json:"messages"`
	System        string              `json:"system,omitempty"`
	Tools         []messagesTool      `json:"tools,omitempty"`
	Temperature   *float64            `json:"temperature,omitempty"`
	TopP          *float64            `json:"top_p,omitempty"`
	StopSequences []string            `json:"stop_sequences,omitempty"`
}

type messagesMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []contentBlock
}

type contentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
	IsError   *bool          `json:"is_error,omitempty"`
}

type messagesTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type messagesResponse struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`
	Role         string            `json:"role"`
	Content      []json.RawMessage `json:"content"`
	Model        string            `json:"model"`
	StopReason   string            `json:"stop_reason"`
	StopSequence *string           `json:"stop_sequence"`
	Usage        messagesUsage     `json:"usage"`
}

type messagesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// buildWireRequest converts a Request to the wire format.
func (c *MessagesClient) buildWireRequest(req *Request) messagesRequest {
	wireReq := messagesRequest{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		System:        req.System,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.StopSequences,
	}

	for _, msg := range req.Messages {
		wireReq.Messages = append(wireReq.Messages, convertMessage(msg))
	}

	for _, t := range req.Tools {
		schema := t.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		wireReq.Tools = append(wireReq.Tools, messagesTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}

	return wireReq
}

// convertMessage converts a Message to the wire messagesMessage.
func convertMessage(msg Message) messagesMessage {
	// Assistant message with tool calls: emit text + tool_use blocks.
	if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
		var blocks []contentBlock
		if msg.Content != "" {
			blocks = append(blocks, contentBlock{Type: "text", Text: msg.Content})
		}
		for _, tc := range msg.ToolCalls {
			blocks = append(blocks, contentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			})
		}
		return messagesMessage{Role: "assistant", Content: blocks}
	}

	// Message carrying tool results: emit as user with tool_result blocks.
	if len(msg.ToolResults) > 0 {
		var blocks []contentBlock
		for _, tr := range msg.ToolResults {
			b := contentBlock{
				Type:      "tool_result",
				ToolUseID: tr.ToolUseID,
				Content:   tr.Content,
			}
			if tr.IsError {
				isErr := true
				b.IsError = &isErr
			}
			blocks = append(blocks, b)
		}
		return messagesMessage{Role: "user", Content: blocks}
	}

	// Simple text message.
	return messagesMessage{Role: string(msg.Role), Content: msg.Content}
}

// toResponse converts the wire response to a Response.
func (r *messagesResponse) toResponse() *Response {
	resp := &Response{
		ID:         r.ID,
		StopReason: StopReason(r.StopReason),
		Model:      r.Model,
		Usage: Usage{
			InputTokens:  r.Usage.InputTokens,
			OutputTokens: r.Usage.OutputTokens,
		},
	}

	var textParts []byte
	for _, raw := range r.Content {
		var block struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(raw, &block); err != nil {
			continue
		}

		switch block.Type {
		case "text":
			if len(textParts) > 0 {
				textParts = append(textParts, '\n')
			}
			textParts = append(textParts, block.Text...)
		case "tool_use":
			tc := ToolCall{
				ID:   block.ID,
				Name: block.Name,
			}
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &tc.Input)
			}
			resp.ToolCalls = append(resp.ToolCalls, tc)
		}
	}
	resp.Content = string(textParts)

	return resp
}

// parseAPIError parses an error response body into an APIError.
// Handles both Anthropic Messages format ({type:"error", error:{type,message}})
// and Chat Completions format ({error:{code,message}}).
func parseAPIError(statusCode int, body []byte) *APIError {
	// Try Anthropic Messages format first.
	var msgErr struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &msgErr); err == nil && msgErr.Error.Message != "" {
		return &APIError{
			StatusCode: statusCode,
			Type:       msgErr.Error.Type,
			Message:    msgErr.Error.Message,
		}
	}

	// Try Chat Completions format.
	var chatErr struct {
		Error struct {
			Code    any    `json:"code"` // int or string
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &chatErr); err == nil && chatErr.Error.Message != "" {
		errType := ""
		if chatErr.Error.Code != nil {
			errType = fmt.Sprintf("%v", chatErr.Error.Code)
		}
		return &APIError{
			StatusCode: statusCode,
			Type:       errType,
			Message:    chatErr.Error.Message,
		}
	}

	return &APIError{
		StatusCode: statusCode,
		RawBody:    string(body),
	}
}

// --- Chat Completions API Implementation ---

// CompletionsClient implements Provider using the OpenAI-compatible Chat Completions API.
// Endpoint: POST {BaseURL}/chat/completions
type CompletionsClient struct {
	config     Config
	httpClient *http.Client
}

// NewCompletionsClient creates a new CompletionsClient.
func NewCompletionsClient(cfg Config) (*CompletionsClient, error) {
	if cfg.APIKey == "" {
		return nil, ErrMissingAPIKey
	}
	cfg.defaults()
	return &CompletionsClient{
		config:     cfg,
		httpClient: &http.Client{Timeout: cfg.HTTPTimeout},
	}, nil
}

// SendMessage implements Provider by calling the Chat Completions endpoint.
func (c *CompletionsClient) SendMessage(ctx context.Context, req *Request) (*Response, error) {
	wireReq := c.buildWireRequest(req)

	body, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if c.config.HTTPReferer != "" {
		httpReq.Header.Set("HTTP-Referer", c.config.HTTPReferer)
	}
	if c.config.AppTitle != "" {
		httpReq.Header.Set("X-Title", c.config.AppTitle)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: http request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, parseAPIError(httpResp.StatusCode, respBody)
	}

	var wireResp completionsResponse
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return nil, fmt.Errorf("llm: decode response: %w", err)
	}

	return wireResp.toResponse()
}

// --- Chat Completions wire format types ---

type completionsRequest struct {
	Model       string                `json:"model"`
	Messages    []completionsMessage  `json:"messages"`
	MaxTokens   *int                  `json:"max_tokens,omitempty"`
	Temperature *float64              `json:"temperature,omitempty"`
	TopP        *float64              `json:"top_p,omitempty"`
	Stop        []string              `json:"stop,omitempty"`
	Tools       []completionsTool     `json:"tools,omitempty"`
}

type completionsMessage struct {
	Role       string                   `json:"role"`
	Content    string                   `json:"content,omitempty"`
	ToolCalls  []completionsToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
}

type completionsToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function completionsFnCall    `json:"function"`
}

type completionsFnCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type completionsTool struct {
	Type     string            `json:"type"`
	Function completionsFnDef  `json:"function"`
}

type completionsFnDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type completionsResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []completionsChoice `json:"choices"`
	Usage   *completionsUsage   `json:"usage,omitempty"`
}

type completionsChoice struct {
	Index        int                `json:"index"`
	Message      completionsRespMsg `json:"message"`
	FinishReason *string            `json:"finish_reason"`
}

type completionsRespMsg struct {
	Role      string                `json:"role"`
	Content   *string               `json:"content"`
	ToolCalls []completionsToolCall `json:"tool_calls,omitempty"`
}

type completionsUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (c *CompletionsClient) buildWireRequest(req *Request) completionsRequest {
	wireReq := completionsRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.StopSequences,
	}

	if req.MaxTokens > 0 {
		wireReq.MaxTokens = &req.MaxTokens
	}

	// System prompt becomes the first message.
	if req.System != "" {
		wireReq.Messages = append(wireReq.Messages, completionsMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, msg := range req.Messages {
		wireReq.Messages = append(wireReq.Messages, convertCompletionsMessages(msg)...)
	}

	for _, t := range req.Tools {
		wireReq.Tools = append(wireReq.Tools, completionsTool{
			Type: "function",
			Function: completionsFnDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	return wireReq
}

// convertCompletionsMessages converts a Message to one or more Chat Completions wire messages.
// A single Message can produce multiple wire messages (e.g., tool results produce one per result).
func convertCompletionsMessages(msg Message) []completionsMessage {
	// Assistant message with tool calls.
	if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
		var wireTCs []completionsToolCall
		for _, tc := range msg.ToolCalls {
			args, _ := json.Marshal(tc.Input)
			wireTCs = append(wireTCs, completionsToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: completionsFnCall{
					Name:      tc.Name,
					Arguments: string(args),
				},
			})
		}
		return []completionsMessage{{
			Role:      "assistant",
			Content:   msg.Content,
			ToolCalls: wireTCs,
		}}
	}

	// Tool results: each becomes a separate message with role "tool".
	if len(msg.ToolResults) > 0 {
		var msgs []completionsMessage
		for _, tr := range msg.ToolResults {
			msgs = append(msgs, completionsMessage{
				Role:       "tool",
				Content:    tr.Content,
				ToolCallID: tr.ToolUseID,
			})
		}
		return msgs
	}

	// Simple text message.
	return []completionsMessage{{
		Role:    string(msg.Role),
		Content: msg.Content,
	}}
}

// finishReasonToStopReason maps Chat Completions finish_reason to StopReason.
func finishReasonToStopReason(fr string) StopReason {
	switch fr {
	case "stop":
		return StopReasonEndTurn
	case "length":
		return StopReasonMaxTokens
	case "tool_calls":
		return StopReasonToolUse
	default:
		return StopReason(fr)
	}
}

func (r *completionsResponse) toResponse() (*Response, error) {
	if len(r.Choices) == 0 {
		return nil, ErrNoChoices
	}

	choice := r.Choices[0]
	resp := &Response{
		ID:    r.ID,
		Model: r.Model,
	}

	if choice.Message.Content != nil {
		resp.Content = *choice.Message.Content
	}

	if choice.FinishReason != nil {
		resp.StopReason = finishReasonToStopReason(*choice.FinishReason)
	}

	for _, tc := range choice.Message.ToolCalls {
		parsed := ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
		}
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &parsed.Input)
		}
		resp.ToolCalls = append(resp.ToolCalls, parsed)
	}

	if r.Usage != nil {
		resp.Usage = Usage{
			InputTokens:  r.Usage.PromptTokens,
			OutputTokens: r.Usage.CompletionTokens,
		}
	}

	return resp, nil
}

// --- Responses API Implementation ---

// ResponsesClient implements Provider using the OpenAI Responses API.
// Endpoint: POST {BaseURL}/responses
type ResponsesClient struct {
	config     Config
	httpClient *http.Client
}

// NewResponsesClient creates a new ResponsesClient.
func NewResponsesClient(cfg Config) (*ResponsesClient, error) {
	if cfg.APIKey == "" {
		return nil, ErrMissingAPIKey
	}
	cfg.defaults()
	return &ResponsesClient{
		config:     cfg,
		httpClient: &http.Client{Timeout: cfg.HTTPTimeout},
	}, nil
}

// SendMessage implements Provider by calling the Responses endpoint.
func (c *ResponsesClient) SendMessage(ctx context.Context, req *Request) (*Response, error) {
	wireReq := c.buildWireRequest(req)

	body, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if c.config.HTTPReferer != "" {
		httpReq.Header.Set("HTTP-Referer", c.config.HTTPReferer)
	}
	if c.config.AppTitle != "" {
		httpReq.Header.Set("X-Title", c.config.AppTitle)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: http request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, parseAPIError(httpResp.StatusCode, respBody)
	}

	var wireResp responsesResponse
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return nil, fmt.Errorf("llm: decode response: %w", err)
	}

	return wireResp.toResponse(), nil
}

// --- Responses wire format types ---

type responsesRequest struct {
	Model           string             `json:"model"`
	Input           []responsesInput   `json:"input"`
	Instructions    string             `json:"instructions,omitempty"`
	Tools           []responsesTool    `json:"tools,omitempty"`
	MaxOutputTokens *int               `json:"max_output_tokens,omitempty"`
	Temperature     *float64           `json:"temperature,omitempty"`
	TopP            *float64           `json:"top_p,omitempty"`
}

// responsesInput is a union type for input items.
// Only the relevant fields are populated based on Type.
type responsesInput struct {
	Type      string `json:"type"`
	Role      string `json:"role,omitempty"`       // for type:"message"
	Content   any    `json:"content,omitempty"`     // string for message
	CallID    string `json:"call_id,omitempty"`     // for function_call / function_call_output
	Name      string `json:"name,omitempty"`        // for function_call
	Arguments string `json:"arguments,omitempty"`   // for function_call
	ID        string `json:"id,omitempty"`          // for function_call
	Output    string `json:"output,omitempty"`      // for function_call_output
}

type responsesTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type responsesResponse struct {
	ID         string              `json:"id"`
	Object     string              `json:"object"`
	Model      string              `json:"model"`
	Status     string              `json:"status"`
	Output     []json.RawMessage   `json:"output"`
	OutputText string              `json:"output_text"`
	Usage      *responsesUsage     `json:"usage,omitempty"`
	Error      *responsesError     `json:"error,omitempty"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type responsesError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *ResponsesClient) buildWireRequest(req *Request) responsesRequest {
	wireReq := responsesRequest{
		Model:        req.Model,
		Instructions: req.System,
		Temperature:  req.Temperature,
		TopP:         req.TopP,
	}

	if req.MaxTokens > 0 {
		wireReq.MaxOutputTokens = &req.MaxTokens
	}

	for _, msg := range req.Messages {
		wireReq.Input = append(wireReq.Input, convertResponsesInput(msg)...)
	}

	for _, t := range req.Tools {
		wireReq.Tools = append(wireReq.Tools, responsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}

	return wireReq
}

// convertResponsesInput converts a Message to one or more Responses API input items.
func convertResponsesInput(msg Message) []responsesInput {
	// Assistant message with tool calls: emit function_call items.
	if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
		var items []responsesInput
		// If there's text content, emit it as a message.
		if msg.Content != "" {
			items = append(items, responsesInput{
				Type:    "message",
				Role:    "assistant",
				Content: msg.Content,
			})
		}
		for _, tc := range msg.ToolCalls {
			args, _ := json.Marshal(tc.Input)
			items = append(items, responsesInput{
				Type:      "function_call",
				ID:        tc.ID,
				CallID:    tc.ID,
				Name:      tc.Name,
				Arguments: string(args),
			})
		}
		return items
	}

	// Tool results: each becomes a function_call_output item.
	if len(msg.ToolResults) > 0 {
		var items []responsesInput
		for _, tr := range msg.ToolResults {
			items = append(items, responsesInput{
				Type:   "function_call_output",
				CallID: tr.ToolUseID,
				Output: tr.Content,
			})
		}
		return items
	}

	// Simple text message.
	return []responsesInput{{
		Type:    "message",
		Role:    string(msg.Role),
		Content: msg.Content,
	}}
}

// responsesStatusToStopReason maps Responses API status to StopReason.
func responsesStatusToStopReason(status string, hasToolCalls bool) StopReason {
	if hasToolCalls {
		return StopReasonToolUse
	}
	switch status {
	case "completed":
		return StopReasonEndTurn
	case "incomplete":
		return StopReasonMaxTokens
	default:
		return StopReason(status)
	}
}

func (r *responsesResponse) toResponse() *Response {
	resp := &Response{
		ID:    r.ID,
		Model: r.Model,
	}

	// Use the convenience output_text field for content.
	resp.Content = r.OutputText

	// Parse output items for tool calls.
	for _, raw := range r.Output {
		var item struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			CallID    string `json:"call_id"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}

		if item.Type == "function_call" {
			tc := ToolCall{
				ID:   item.CallID,
				Name: item.Name,
			}
			if item.Arguments != "" {
				_ = json.Unmarshal([]byte(item.Arguments), &tc.Input)
			}
			resp.ToolCalls = append(resp.ToolCalls, tc)
		}
	}

	resp.StopReason = responsesStatusToStopReason(r.Status, len(resp.ToolCalls) > 0)

	if r.Usage != nil {
		resp.Usage = Usage{
			InputTokens:  r.Usage.InputTokens,
			OutputTokens: r.Usage.OutputTokens,
		}
	}

	return resp
}
