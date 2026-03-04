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

// WebConfig configures the web search and fetch tools.
// Nil in Config.Web disables web tools entirely.
type WebConfig struct {
	APIKey       string // OpenRouter API key (inherited from main Config.APIKey if empty)
	BaseURL      string // API base URL (inherited from main Config.BaseURL if empty)
	Model        string // Model for answer/classification/fetch (default: "perplexity/sonar")
	SearchModel  string // Model for search with web plugin (inherited from main Config.Model if empty)
	SearchEngine    string // Web plugin engine: "exa" or "native" (default: "exa")
	MaxResults      int    // Max search results from web plugin (default: 5)
	APILogPath      string // Path to JSONL log file (inherited from main Config.APILogPath if empty)
	ClassifierModel string // Model for query routing (default: "openai/gpt-oss-20b")
}

// Config holds the configuration for the agent.
type Config struct {
	APIKey              string            // Required: API key for authentication
	BaseURL             string            // Base URL for the API (defaults to OpenRouter)
	Model               string            // Model to use (defaults to claude-opus-4.5)
	AllowedTools        []string          // Tool names to enable. Empty = all registered tools.
	SystemPrompt        string            // Optional system prompt
	HTTPTimeout         time.Duration     // HTTP client timeout
	PreTasks            []PreTaskConfig   // Pre-tasks to run on first Chat() call
	Sandbox             SandboxConfig     // Sandbox configuration for shell execution
	CodeSearch          *CodeSearchConfig // Optional code search configuration. Nil disables code_search.
	Web                 *WebConfig        // Optional web search/fetch configuration. Nil disables web tools.
	APILogPath          string            // Path to JSONL log file (default: logs/api_calls.jsonl)
	MaxToolCallsPerTurn int               // Cap tool calls per round; 0 = unlimited. When hit, agent asks for a summary before continuing.
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
		c.HTTPTimeout = 120 * time.Second
	}
	if c.APILogPath == "" {
		c.APILogPath = "logs/api_calls.jsonl"
	}
	return nil
}
