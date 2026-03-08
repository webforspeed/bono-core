package core

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
