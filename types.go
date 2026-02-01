// Package core provides the agent loop and API client for building CLI/TUI agents.
package core

// Message represents a message in the conversation history.
type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool invocation requested by the assistant.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall contains the name and arguments for a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool represents a tool definition sent to the API.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction contains the metadata for a tool.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatRequest is the request body for the chat completions API.
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}

// ChatResponse is the response from the chat completions API.
type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

// Choice represents a single choice in the API response.
type Choice struct {
	Message Message `json:"message"`
}

// ToolResult contains the outcome of a tool execution.
type ToolResult struct {
	Success bool   // Whether the tool executed successfully
	Output  string // Result content to send back to API
	Status  string // Human-readable status for display
	Error   error  // Error if execution failed
}
