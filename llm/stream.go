package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// StreamProvider sends messages to an LLM and streams the response back.
// Not all providers support streaming; check with type assertion.
type StreamProvider interface {
	Provider
	SendMessageStream(ctx context.Context, req *Request) (*Stream, error)
}

// StreamEvent represents a single incremental update from a streaming response.
type StreamEvent struct {
	ContentDelta   string    // text content fragment
	ReasoningDelta string    // reasoning text fragment
	Response       *Response // set on final event with fully assembled response
}

// Stream is an iterator over streaming events.
type Stream struct {
	ch  <-chan StreamEvent
	err error
}

// Next returns the next event. Returns false when the stream is exhausted.
func (s *Stream) Next() (StreamEvent, bool) {
	evt, ok := <-s.ch
	if !ok {
		return StreamEvent{}, false
	}
	if evt.Response == nil && evt.ContentDelta == "" && evt.ReasoningDelta == "" {
		// error-only event
		s.err = fmt.Errorf("llm: stream ended unexpectedly")
		return evt, false
	}
	return evt, true
}

// Err returns the first error encountered during streaming.
func (s *Stream) Err() error { return s.err }

// --- SSE streaming wire types for Chat Completions ---

type completionsStreamChunk struct {
	ID      string                    `json:"id"`
	Object  string                    `json:"object"`
	Created int64                     `json:"created"`
	Model   string                    `json:"model"`
	Choices []completionsStreamChoice `json:"choices"`
	Usage   json.RawMessage           `json:"usage,omitempty"`
}

type completionsStreamChoice struct {
	Index        int                    `json:"index"`
	Delta        completionsStreamDelta `json:"delta"`
	FinishReason *string                `json:"finish_reason"`
}

type completionsStreamDelta struct {
	Role      string                `json:"role,omitempty"`
	Content   *string               `json:"content,omitempty"`
	Reasoning *string               `json:"reasoning,omitempty"`
	ToolCalls []streamToolCallDelta `json:"tool_calls,omitempty"`
}

type streamToolCallDelta struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function streamFnCallDelta  `json:"function,omitempty"`
}

type streamFnCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// --- SendMessageStream on CompletionsClient ---

// SendMessageStream implements StreamProvider for the Chat Completions API.
func (c *CompletionsClient) SendMessageStream(ctx context.Context, req *Request) (*Stream, error) {
	wireReq := c.buildWireRequest(req)

	// Wrap in a struct that adds stream: true and requests usage in final chunk.
	type streamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	}
	type streamReq struct {
		completionsRequest
		Stream        bool          `json:"stream"`
		StreamOptions streamOptions `json:"stream_options"`
	}
	sr := streamReq{
		completionsRequest: wireReq,
		Stream:             true,
		StreamOptions:      streamOptions{IncludeUsage: true},
	}

	body, err := json.Marshal(sr)
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
		httpReq.Header.Set("X-OpenRouter-Title", c.config.AppTitle)
	}
	if c.config.Categories != "" {
		httpReq.Header.Set("X-OpenRouter-Categories", c.config.Categories)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: http request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, parseAPIError(httpResp.StatusCode, respBody)
	}

	ch := make(chan StreamEvent, 32)
	stream := &Stream{ch: ch}

	go readSSEStream(httpResp.Body, ch)

	return stream, nil
}

// readSSEStream reads SSE lines from the body, parses chunks, and sends StreamEvents.
// Closes the body and channel when done.
func readSSEStream(body io.ReadCloser, ch chan<- StreamEvent) {
	defer body.Close()
	defer close(ch)

	// Accumulators for the final Response.
	var (
		contentBuf   strings.Builder
		reasoningBuf strings.Builder
		toolCalls    []toolCallAccum // indexed by tool call index
		model        string
		id           string
		stopReason   StopReason
		usage        Usage
		rawUsage     json.RawMessage // preserve full usage JSON for cost extraction
		finished     bool            // set when finish_reason is received
	)

	scanner := bufio.NewScanner(body)
	// Increase buffer for potentially large SSE lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// SSE comment (keepalive) — skip.
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Empty line — SSE event boundary, skip.
		if line == "" {
			continue
		}

		// We only care about data lines.
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Stream termination.
		if data == "[DONE]" {
			break
		}

		var chunk completionsStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}

		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.ID != "" {
			id = chunk.ID
		}

		// Capture usage (often arrives in a chunk after finish_reason).
		if len(chunk.Usage) > 0 {
			rawUsage = chunk.Usage
			var u completionsUsage
			if err := json.Unmarshal(chunk.Usage, &u); err == nil {
				usage = Usage{
					InputTokens:  u.PromptTokens,
					OutputTokens: u.CompletionTokens,
				}
			}
			// If we already saw finish_reason, usage is the last thing we need.
			if finished {
				break
			}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// After finish_reason, don't process more deltas.
		if finished {
			continue
		}

		// Content delta.
		if delta.Content != nil && *delta.Content != "" {
			contentBuf.WriteString(*delta.Content)
			ch <- StreamEvent{ContentDelta: *delta.Content}
		}

		// Reasoning delta.
		if delta.Reasoning != nil && *delta.Reasoning != "" {
			reasoningBuf.WriteString(*delta.Reasoning)
			ch <- StreamEvent{ReasoningDelta: *delta.Reasoning}
		}

		// Tool call deltas — accumulate silently.
		for _, tcd := range delta.ToolCalls {
			for len(toolCalls) <= tcd.Index {
				toolCalls = append(toolCalls, toolCallAccum{})
			}
			tc := &toolCalls[tcd.Index]
			if tcd.ID != "" {
				tc.id = tcd.ID
			}
			if tcd.Function.Name != "" {
				tc.name += tcd.Function.Name
			}
			if tcd.Function.Arguments != "" {
				tc.args.WriteString(tcd.Function.Arguments)
			}
		}

		// Model is done generating. Keep scanning for usage chunk.
		if choice.FinishReason != nil {
			stopReason = finishReasonToStopReason(*choice.FinishReason)
			finished = true
		}
	}

	// Build final assembled Response.
	resp := &Response{
		ID:         id,
		Content:    contentBuf.String(),
		Reasoning:  reasoningBuf.String(),
		StopReason: stopReason,
		Model:      model,
		Usage:      usage,
		RawUsage:   rawUsage,
	}

	for _, tc := range toolCalls {
		parsed := ToolCall{
			ID:   tc.id,
			Name: tc.name,
		}
		if args := tc.args.String(); args != "" {
			_ = json.Unmarshal([]byte(args), &parsed.Input)
		}
		resp.ToolCalls = append(resp.ToolCalls, parsed)
	}

	ch <- StreamEvent{Response: resp}
}

// toolCallAccum accumulates streaming tool call fragments.
type toolCallAccum struct {
	id   string
	name string
	args strings.Builder
}
