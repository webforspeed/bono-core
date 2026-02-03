package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Client handles communication with the chat completions API.
type Client struct {
	config     Config
	httpClient *http.Client
	logPath    string
}

// APILogEntry represents a logged API call.
type APILogEntry struct {
	Timestamp  string        `json:"ts"`
	Request    ChatRequest   `json:"request"`
	Response   *ChatResponse `json:"response,omitempty"`
	Error      string        `json:"error,omitempty"`
	DurationMs int64         `json:"duration_ms"`
}

// NewClient creates a new API client with the given configuration.
func NewClient(config Config) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Client{
		config:     config,
		httpClient: &http.Client{Timeout: config.HTTPTimeout},
		logPath:    config.APILogPath,
	}, nil
}

// logAPICall appends an API call entry to the JSONL log file.
func (c *Client) logAPICall(entry APILogEntry) {
	os.MkdirAll(filepath.Dir(c.logPath), 0755)
	f, err := os.OpenFile(c.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if data, err := json.Marshal(entry); err == nil {
		f.Write(data)
		f.WriteString("\n")
	}
}

// ChatCompletion sends messages to the API and returns the assistant's response.
// Uses the tools configured in the client.
func (c *Client) ChatCompletion(ctx context.Context, messages []Message) (*Message, error) {
	return c.ChatCompletionWithTools(ctx, messages, c.config.Tools)
}

// ChatCompletionWithTools sends messages with a custom tool set.
// Use this when you need to override the default tools (e.g., for subagents).
func (c *Client) ChatCompletionWithTools(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	start := time.Now()
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
		c.logAPICall(APILogEntry{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Request:    req,
			Error:      err.Error(),
			DurationMs: time.Since(start).Milliseconds(),
		})
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		apiErr := &APIError{StatusCode: resp.StatusCode, Body: string(b)}
		c.logAPICall(APILogEntry{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Request:    req,
			Error:      apiErr.Error(),
			DurationMs: time.Since(start).Milliseconds(),
		})
		return nil, apiErr
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		c.logAPICall(APILogEntry{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Request:    req,
			Error:      err.Error(),
			DurationMs: time.Since(start).Milliseconds(),
		})
		return nil, fmt.Errorf("decode response: %w", err)
	}

	c.logAPICall(APILogEntry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Request:    req,
		Response:   &chatResp,
		DurationMs: time.Since(start).Milliseconds(),
	})

	if len(chatResp.Choices) == 0 {
		return nil, ErrNoChoices
	}

	return &chatResp.Choices[0].Message, nil
}
