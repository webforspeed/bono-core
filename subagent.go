package core

import "context"

// SubAgent defines a self-contained agent mode with its own prompt and tool constraints.
// Implementations provide identity, a system prompt, and an optional tool allowlist.
// The Agent.RunSubAgent runner handles execution mechanics.
type SubAgent interface {
	// Name returns the subagent's identifier (e.g., "plan", "review").
	Name() string

	// AllowedTools returns the tool names this subagent may use.
	// Empty slice means all registered tools are available.
	AllowedTools() []string

	// SystemPrompt returns the rendered system prompt for this subagent.
	SystemPrompt() string
}

// UserPromptFormatter is an optional interface subagents can implement to
// wrap or transform the raw user input before it becomes the first user message.
type UserPromptFormatter interface {
	FormatUserPrompt(input string) string
}

// SubAgentHook runs after a subagent completes. Hooks are composable —
// multiple hooks can be attached to a single subagent via RegisterSubAgent.
// Hooks execute in registration order and can annotate the result via Meta.
type SubAgentHook interface {
	AfterComplete(ctx context.Context, result *SubAgentResult) error
}

// SubAgentResult carries the subagent's output and context for hooks.
// Hooks read Content and annotate via Meta; the runner uses Meta when
// building the handoff message.
type SubAgentResult struct {
	Name    string            // subagent identifier
	Input   string            // original user input
	Content string            // final LLM response text
	CWD     string            // working directory
	Meta    map[string]string // mutable annotations (e.g., "output_path", "approval")
}

// SubAgentApprovalAction represents the user's decision after reviewing subagent output.
type SubAgentApprovalAction int

const (
	SubAgentApprove SubAgentApprovalAction = iota
	SubAgentReject
	SubAgentRevise
)

// SubAgentApprovalResponse carries the user's decision and optional feedback.
type SubAgentApprovalResponse struct {
	Action   SubAgentApprovalAction
	Feedback string // non-empty when Action == SubAgentRevise
}

// subAgentEntry pairs a SubAgent with its registered hooks.
type subAgentEntry struct {
	agent SubAgent
	hooks []SubAgentHook
}
