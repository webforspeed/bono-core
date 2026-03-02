package core

// RunShellTool returns the run_shell tool definition.
// exec is the shell executor — the agent injects sandbox+fallback handling.
func RunShellTool(exec func(cmd string) ToolResult) *ToolDef {
	return &ToolDef{
		Name:        "run_shell",
		Description: "Executes a shell command and returns the output. Use for any CLI operation: build commands, git operations, package managers, file exploration.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The shell command to execute, e.g. 'git log --oneline -20' or 'find . -name \"*.go\"'",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Brief description of what this command does, for display",
				},
				"safety": map[string]any{
					"type":        "string",
					"enum":        []any{"read-only", "modify", "destructive", "network", "privileged"},
					"description": "Safety level: read-only, modify, destructive, network, privileged",
				},
			},
			"required": []any{"command", "description", "safety"},
		},
		Execute: func(args map[string]any) ToolResult {
			cmd, _ := args["command"].(string)
			return exec(cmd)
		},
		AutoApprove: func(sandboxed bool) bool {
			return sandboxed && IsSandboxEnabled()
		},
	}
}
