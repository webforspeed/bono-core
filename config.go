package core

import "time"

// PreTaskConfig defines a sub-agent task that runs before the main agent.
type PreTaskConfig struct {
	Name         string // Display name (e.g., "exploring")
	SystemPrompt string // System prompt for this task
	Input        string // Initial input message (empty = "Begin")
	DoneMarker   string // Completion marker (e.g., "{{DONE}}")
}

// SandboxConfig holds configuration for sandboxed shell execution.
type SandboxConfig struct {
	Enabled                bool     // Enable sandboxing (true on macOS by default)
	AllowNetwork           bool     // Allow outbound network from sandbox (default false)
	ReadPaths              []string // Read-only path allowlist
	WritePaths             []string // Read/write path allowlist (default: cwd + temp)
	ExecPaths              []string // Executable path allowlist
	FallbackOutsideSandbox bool     // Allow approval-based rerun outside sandbox if blocked (default true)
}

// Config holds the configuration for the agent.
type Config struct {
	APIKey       string          // Required: API key for authentication
	BaseURL      string          // Base URL for the API (defaults to OpenRouter)
	Model        string          // Model to use (defaults to claude-opus-4.5)
	Tools        []Tool          // Tool definitions to send to the API
	SystemPrompt string          // Optional system prompt
	HTTPTimeout  time.Duration   // HTTP client timeout
	PreTasks     []PreTaskConfig // Pre-tasks to run on first Chat() call
	Sandbox      SandboxConfig   // Sandbox configuration for shell execution
}

// Validate checks the configuration and sets defaults.
func (c *Config) Validate() error {
	if c.APIKey == "" {
		return ErrMissingAPIKey
	}
	if c.BaseURL == "" {
		c.BaseURL = "https://openrouter.ai/api/v1"
	}
	if c.Model == "" {
		c.Model = "anthropic/claude-opus-4.5"
	}
	if c.HTTPTimeout == 0 {
		c.HTTPTimeout = 30 * time.Second
	}
	return nil
}
