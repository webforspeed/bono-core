package core

import (
	"context"
	"encoding/json"
)

// Agent orchestrates conversations with an LLM and executes tools.
type Agent struct {
	config Config
	client *Client
	msgs   []Message

	// Optional hooks - nil means default behavior (auto-execute, no output)

	// OnToolCall is called before executing a tool.
	// Return false to skip tool execution (sends "cancelled by user" as result).
	OnToolCall func(name string, args map[string]any) bool

	// OnToolDone is called after a tool executes.
	// Receives the tool name, args (for display), and result.
	OnToolDone func(name string, args map[string]any, result ToolResult)

	// OnMessage is called when the assistant responds with text content.
	OnMessage func(content string)
}

// NewAgent creates an agent. Set hooks after creation to customize behavior.
func NewAgent(config Config) (*Agent, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	client, err := NewClient(config)
	if err != nil {
		return nil, err
	}

	a := &Agent{config: config, client: client}
	if config.SystemPrompt != "" {
		a.msgs = append(a.msgs, Message{Role: "system", Content: config.SystemPrompt})
	}
	return a, nil
}

// Chat sends a user message and processes the complete turn.
// Blocks until the assistant provides a final text response.
// Tool calls are executed automatically unless OnToolCall returns false.
func (a *Agent) Chat(ctx context.Context, input string) (string, error) {
	a.msgs = append(a.msgs, Message{Role: "user", Content: input})

	for {
		msg, err := a.client.ChatCompletion(ctx, a.msgs)
		if err != nil {
			return "", err
		}

		a.msgs = append(a.msgs, *msg)

		// No tool calls = done
		if len(msg.ToolCalls) == 0 {
			content, _ := msg.Content.(string)
			if a.OnMessage != nil && content != "" {
				a.OnMessage(content)
			}
			return content, nil
		}

		// Track checkpoint for potential cancellation rollback
		checkpoint := len(a.msgs) - 1
		cancelled := false

		// Execute tools
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)

			// Check if tool should execute
			if a.OnToolCall != nil && !a.OnToolCall(tc.Function.Name, args) {
				cancelled = true
				break
			}

			result := ExecuteTool(tc.Function.Name, args)

			if a.OnToolDone != nil {
				a.OnToolDone(tc.Function.Name, args, result)
			}

			a.msgs = append(a.msgs, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result.Output,
			})
		}

		// If cancelled, rollback to checkpoint and return
		if cancelled {
			a.msgs = a.msgs[:checkpoint]
			return "", nil
		}
	}
}

// Messages returns the conversation history.
func (a *Agent) Messages() []Message {
	return a.msgs
}

// Reset clears conversation history (keeps system prompt if configured).
func (a *Agent) Reset() {
	a.msgs = nil
	if a.config.SystemPrompt != "" {
		a.msgs = append(a.msgs, Message{Role: "system", Content: a.config.SystemPrompt})
	}
}
