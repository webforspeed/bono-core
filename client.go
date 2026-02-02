package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client handles communication with the chat completions API.
type Client struct {
	config     Config
	httpClient *http.Client
}

// NewClient creates a new API client with the given configuration.
func NewClient(config Config) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Client{
		config:     config,
		httpClient: &http.Client{Timeout: config.HTTPTimeout},
	}, nil
}

// ChatCompletion sends messages to the API and returns the assistant's response.
// Uses the tools configured in the client.
func (c *Client) ChatCompletion(ctx context.Context, messages []Message) (*Message, error) {
	return c.ChatCompletionWithTools(ctx, messages, c.config.Tools)
}

// ChatCompletionWithTools sends messages with a custom tool set.
// Use this when you need to override the default tools (e.g., for subagents).
func (c *Client) ChatCompletionWithTools(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	req := ChatRequest{
		Model:    c.config.Model,
		Messages: messages,
		Tools:    tools,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.config.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "http://localhost")
	httpReq.Header.Set("X-Title", "Agent")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, ErrNoChoices
	}

	return &chatResp.Choices[0].Message, nil
}
