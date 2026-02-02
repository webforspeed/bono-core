package core

import (
	"context"
	"encoding/json"
	"strings"
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

	// OnShellSubagentStart is called when a shell subagent begins execution.
	// Receives the system prompt that defines the subagent's behavior.
	OnShellSubagentStart func(systemPrompt string)

	// OnShellSubagentEnd is called when a shell subagent completes.
	// Receives the final result.
	OnShellSubagentEnd func(result ToolResult)

	// OnSubagentToolCall is called before a subagent executes a tool.
	// Return false to skip tool execution (sends "cancelled by user" as result).
	OnSubagentToolCall func(name string, args map[string]any) bool

	// OnSubagentToolDone is called after a subagent tool executes.
	// Receives the tool name, args (for display), and result.
	OnSubagentToolDone func(name string, args map[string]any, result ToolResult)
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

			// Handle run_shell via subagent, other tools directly
			var result ToolResult
			if tc.Function.Name == "run_shell" {
				cmd, _ := args["command"].(string)
				desc, _ := args["description"].(string)
				safety, _ := args["safety"].(string)
				sysPrompt, _ := args["system_prompt"].(string)
				result = a.ExecuteShellSubagent(ctx, cmd, desc, safety, sysPrompt)
			} else {
				result = ExecuteTool(tc.Function.Name, args)
			}

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

// SubagentShellTool is the simplified tool schema for shell subagents.
// Only exposes run_shell without system_prompt (no nested subagents).
var SubagentShellTool = []Tool{{
	Type: "function",
	Function: ToolFunction{
		Name:        "run_shell",
		Description: "Execute a shell command and return its output. Use for any follow-up commands needed to complete the task.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The shell command to execute",
				},
			},
			"required": []string{"command"},
		},
	},
}}

// ExecuteShellSubagent runs a command via a subagent with custom system prompt.
// The subagent can run follow-up commands and returns summarized output.
func (a *Agent) ExecuteShellSubagent(ctx context.Context, command, description, safety, systemPrompt string) ToolResult {
	// Notify start with system prompt
	if a.OnShellSubagentStart != nil {
		a.OnShellSubagentStart(systemPrompt)
	}

	// 1. Check if initial command should execute via hook
	// Include description and safety from parent call for display
	initialArgs := map[string]any{"command": command, "description": description, "safety": safety}
	if a.OnSubagentToolCall != nil && !a.OnSubagentToolCall("run_shell", initialArgs) {
		cancelledResult := ToolResult{Success: false, Output: "cancelled by user", Status: "cancelled"}
		if a.OnShellSubagentEnd != nil {
			a.OnShellSubagentEnd(cancelledResult)
		}
		return cancelledResult
	}

	// 2. Run initial command using existing ExecuteShell
	result := ExecuteShell(command)

	// Notify initial command done
	if a.OnSubagentToolDone != nil {
		a.OnSubagentToolDone("run_shell", initialArgs, result)
	}

	// 3. Create isolated subagent message history
	msgs := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "Command: " + command + "\n\nOutput:\n" + result.Output},
	}

	// 4. Subagent loop with shell-only tools
	for {
		msg, err := a.client.ChatCompletionWithTools(ctx, msgs, SubagentShellTool)
		if err != nil {
			errResult := ToolResult{Success: false, Error: err, Status: "subagent error"}
			if a.OnShellSubagentEnd != nil {
				a.OnShellSubagentEnd(errResult)
			}
			return errResult
		}
		msgs = append(msgs, *msg)

		// No tool calls - check for done marker
		if len(msg.ToolCalls) == 0 {
			if content, ok := msg.Content.(string); ok {
				if strings.Contains(content, "{{SHELL_DONE}}") {
					parts := strings.SplitN(content, "{{SHELL_DONE}}", 2)
					var finalResult ToolResult
					if len(parts) > 1 {
						finalResult = ToolResult{Success: true, Output: strings.TrimSpace(parts[1]), Status: "ok"}
					} else {
						finalResult = ToolResult{Success: true, Output: content, Status: "ok"}
					}
					if a.OnShellSubagentEnd != nil {
						a.OnShellSubagentEnd(finalResult)
					}
					return finalResult
				}
			}
			continue
		}

		// Execute follow-up shell commands
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			cmd, _ := args["command"].(string)

			// Check if follow-up command should execute via hook
			if a.OnSubagentToolCall != nil && !a.OnSubagentToolCall("run_shell", args) {
				// User cancelled - send cancelled result to LLM and continue
				msgs = append(msgs, Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    "cancelled by user",
				})
				continue
			}

			toolResult := ExecuteShell(cmd)

			// Notify follow-up command done
			if a.OnSubagentToolDone != nil {
				a.OnSubagentToolDone("run_shell", args, toolResult)
			}

			msgs = append(msgs, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    toolResult.Output,
			})
		}
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
		for {
			msg, err := a.client.ChatCompletion(ctx, taskMsgs)
			if err != nil {
				return err
			}
			taskMsgs = append(taskMsgs, *msg)

			// Check for completion (no tool calls and contains done marker)
			if len(msg.ToolCalls) == 0 {
				if content, ok := msg.Content.(string); ok {
					if a.OnMessage != nil && content != "" {
						a.OnMessage(content)
					}
					if strings.Contains(content, task.DoneMarker) {
						break
					}
				}
				continue
			}

			// Track checkpoint for potential cancellation rollback
			checkpoint := len(taskMsgs) - 1
			cancelled := false

			// Execute tools (using existing hooks)
			for _, tc := range msg.ToolCalls {
				var args map[string]any
				json.Unmarshal([]byte(tc.Function.Arguments), &args)

				// Check if tool should execute
				if a.OnToolCall != nil && !a.OnToolCall(tc.Function.Name, args) {
					cancelled = true
					break
				}

				// Handle run_shell via subagent, other tools directly
				var result ToolResult
				if tc.Function.Name == "run_shell" {
					cmd, _ := args["command"].(string)
					desc, _ := args["description"].(string)
					safety, _ := args["safety"].(string)
					sysPrompt, _ := args["system_prompt"].(string)
					result = a.ExecuteShellSubagent(ctx, cmd, desc, safety, sysPrompt)
				} else {
					result = ExecuteTool(tc.Function.Name, args)
				}

				if a.OnToolDone != nil {
					a.OnToolDone(tc.Function.Name, args, result)
				}

				taskMsgs = append(taskMsgs, Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    result.Output,
				})
			}

			// If cancelled, rollback and skip remaining pre-tasks
			if cancelled {
				taskMsgs = taskMsgs[:checkpoint]
				if a.OnPreTaskEnd != nil {
					a.OnPreTaskEnd(task.Name)
				}
				return ErrToolCancelled
			}
		}

		if a.OnPreTaskEnd != nil {
			a.OnPreTaskEnd(task.Name)
		}
	}
	return nil
}
