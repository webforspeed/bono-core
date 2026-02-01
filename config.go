package core

import "time"

// PreTaskConfig defines a sub-agent task that runs before the main agent.
type PreTaskConfig struct {
	Name         string // Display name (e.g., "exploring")
	SystemPrompt string // System prompt for this task
	Input        string // Initial input message (empty = "Begin")
	DoneMarker   string // Completion marker (e.g., "{{DONE}}")
}

// Config holds the configuration for the agent.
type Config struct {
	APIKey       string        // Required: API key for authentication
	BaseURL      string        // Base URL for the API (defaults to OpenRouter)
	Model        string        // Model to use (defaults to claude-opus-4.5)
	Tools        []Tool        // Tool definitions to send to the API
	SystemPrompt string        // Optional system prompt
	HTTPTimeout  time.Duration // HTTP client timeout
	PreTasks     []PreTaskConfig // Pre-tasks to run on first Chat() call
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
