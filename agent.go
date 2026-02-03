package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxChatTurns    = 10
	maxPreTaskTurns = 10
)

// Agent orchestrates conversations with an LLM and executes tools.
type Agent struct {
	config       Config
	client       *Client
	msgs         []Message
	preTasksDone bool // tracks if pre-tasks have executed

	// Optional hooks - nil means default behavior (auto-execute, no output)

	// OnToolCall is called before executing a tool.
	// Return false to skip tool execution (sends "cancelled by user" as result).
	OnToolCall func(name string, args map[string]any) bool

	// OnToolDone is called after a tool executes.
	// Receives the tool name, args (for display), and result.
	OnToolDone func(name string, args map[string]any, result ToolResult)

	// OnMessage is called when the assistant responds with text content.
	OnMessage func(content string)

	// OnPreTaskStart is called when a pre-task begins.
	OnPreTaskStart func(name string)

	// OnPreTaskEnd is called when a pre-task completes.
	OnPreTaskEnd func(name string)

	// OnSandboxFallback is called when sandbox blocks a command.
	// Return true to execute outside sandbox (requires approval), false to cancel.
	OnSandboxFallback func(command string, reason string) bool
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

	// Initialize sandbox with config or defaults
	sandboxCfg := config.Sandbox
	if sandboxCfg.ReadPaths == nil && sandboxCfg.WritePaths == nil && sandboxCfg.ExecPaths == nil {
		// No sandbox config provided, use defaults
		sandboxCfg = DefaultSandboxConfig()
	}
	InitSandbox(sandboxCfg)

	a := &Agent{config: config, client: client}
	if config.SystemPrompt != "" {
		a.msgs = append(a.msgs, Message{Role: "system", Content: config.SystemPrompt})
	}
	return a, nil
}

// Chat sends a user message and processes the complete turn.
// Blocks until the assistant provides a final text response.
// Tool calls are executed automatically unless OnToolCall returns false.
// On the first call, pre-tasks are automatically executed if configured.
func (a *Agent) Chat(ctx context.Context, input string) (string, error) {
	// Auto-run pre-tasks on first call
	if !a.preTasksDone && len(a.config.PreTasks) > 0 {
		if err := a.runPreTasks(ctx); err != nil {
			return "", err
		}
		a.preTasksDone = true
	}

	a.msgs = append(a.msgs, Message{Role: "user", Content: input})

	for turns := 0; ; turns++ {
		if turns >= maxChatTurns {
			return "", ErrMaxTurnsExceeded
		}
		msg, err := a.client.ChatCompletion(ctx, a.msgs)
		if err != nil {
			return "", err
		}

		a.msgs = append(a.msgs, *msg)
		content := messageContent(msg)
		if a.OnMessage != nil && strings.TrimSpace(content) != "" {
			a.OnMessage(content)
		}

		// No tool calls = done
		if len(msg.ToolCalls) == 0 {
			if content == "" {
				return "", ErrEmptyResponse
			}
			return content, nil
		}

		// Execute tools and collect results
		var toolResults []Message
		cancelled := false

		for _, tc := range msg.ToolCalls {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)

			// Check if tool should execute
			if a.OnToolCall != nil && !a.OnToolCall(tc.Function.Name, args) {
				cancelled = true
				break
			}

			// Execute tool with sandbox support for shell
			var result ToolResult
			if tc.Function.Name == "run_shell" {
				cmd, _ := args["command"].(string)
				result = ExecuteShellWithSandbox(cmd, a.OnSandboxFallback)
			} else {
				result = ExecuteTool(tc.Function.Name, args)
			}

			if a.OnToolDone != nil {
				a.OnToolDone(tc.Function.Name, args, result)
			}

			// Append tool result message
			toolResults = append(toolResults, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result.Output,
			})
		}

		// If cancelled, rollback to remove assistant message
		if cancelled {
			a.msgs = a.msgs[:len(a.msgs)-1]
			return "", nil
		}

		// Append all tool results
		a.msgs = append(a.msgs, toolResults...)
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

// runPreTasks executes all configured pre-tasks before the main agent loop.
// Each pre-task runs with its own isolated message history.
func (a *Agent) runPreTasks(ctx context.Context) error {
	for _, task := range a.config.PreTasks {
		if a.OnPreTaskStart != nil {
			a.OnPreTaskStart(task.Name)
		}

		// Create isolated message history for this pre-task
		taskMsgs := []Message{{Role: "system", Content: task.SystemPrompt}}
		input := task.Input
		if input == "" {
			input = "Begin"
		}
		taskMsgs = append(taskMsgs, Message{Role: "user", Content: input})

		// Run task loop until DoneMarker detected
		for turns := 0; ; turns++ {
			if turns >= maxPreTaskTurns {
				return fmt.Errorf("pretask %s: %w", task.Name, ErrMaxTurnsExceeded)
			}
			msg, err := a.client.ChatCompletion(ctx, taskMsgs)
			if err != nil {
				return err
			}
			taskMsgs = append(taskMsgs, *msg)
			content := messageContent(msg)
			if a.OnMessage != nil && strings.TrimSpace(content) != "" {
				a.OnMessage(content)
			}

			// Check for completion (no tool calls)
			if len(msg.ToolCalls) == 0 {
				if content == "" {
					return fmt.Errorf("pretask %s: %w", task.Name, ErrEmptyResponse)
				}
				break
			}

			// Execute tools and collect results
			var toolResults []Message
			cancelled := false

			for _, tc := range msg.ToolCalls {
				var args map[string]any
				json.Unmarshal([]byte(tc.Function.Arguments), &args)

				// Check if tool should execute
				if a.OnToolCall != nil && !a.OnToolCall(tc.Function.Name, args) {
					cancelled = true
					break
				}

				// Execute tool with sandbox support for shell
				var result ToolResult
				if tc.Function.Name == "run_shell" {
					cmd, _ := args["command"].(string)
					result = ExecuteShellWithSandbox(cmd, a.OnSandboxFallback)
				} else {
					result = ExecuteTool(tc.Function.Name, args)
				}

				if a.OnToolDone != nil {
					a.OnToolDone(tc.Function.Name, args, result)
				}

				// Append tool result message
				toolResults = append(toolResults, Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    result.Output,
				})
			}

			// If cancelled, rollback and skip remaining pre-tasks
			if cancelled {
				taskMsgs = taskMsgs[:len(taskMsgs)-1]
				if a.OnPreTaskEnd != nil {
					a.OnPreTaskEnd(task.Name)
				}
				return ErrToolCancelled
			}

			// Append all tool results
			taskMsgs = append(taskMsgs, toolResults...)
		}

		if a.OnPreTaskEnd != nil {
			a.OnPreTaskEnd(task.Name)
		}
	}
	return nil
}

func messageContent(msg *Message) string {
	if msg == nil {
		return ""
	}
	switch v := msg.Content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			if t != "text" && t != "output_text" {
				continue
			}
			text, _ := m["text"].(string)
			if text == "" {
				continue
			}
			b.WriteString(text)
		}
		return b.String()
	default:
		return ""
	}
}
