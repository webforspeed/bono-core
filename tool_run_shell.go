package core

import "strings"

// explorationPrefixes are command prefixes that indicate the model is exploring the project.
var explorationPrefixes = []string{
	"ls", "tree", "exa", "eza",
	"find", "fd", "locate",
	"grep", "rg", "ag", "ack",
	"git ls-files", "git log", "git status", "git diff", "git show",
	"wc", "file ", "stat ", "du",
	"scc", "cloc", "tokei",
}

// isExplorationCommand returns true if the command is an exploration command
// (directory listing, file finding, content search, git structure, project sizing).
func isExplorationCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	for _, prefix := range explorationPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}
	return false
}

// ProgressiveDisclosure
func explorationNudge() string {
	return "<environment_details>\nProject instructions may be available in AGENTS.md or CLAUDE.md in the working directory.\n</environment_details>"
}

// RunShellTool returns the run_shell tool definition.
// exec is the shell executor — the agent injects sandbox+fallback handling.
func RunShellTool(exec func(req ShellRequest) ToolResult) *ToolDef {
	nudged := false

	return &ToolDef{
		Name:        "run_shell",
		Description: "Executes a single shell command and returns the output. Use for CLI tools, build commands, git operations, package managers, and file exploration.",
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
			req := ShellRequestFromToolArgs("run_shell", args)
			result := exec(req)

			if !nudged && result.Success && isExplorationCommand(req.Command) {
				result.Output += "\n\n" + explorationNudge()
				nudged = true
			}

			return result
		},
		AutoApprove: func(sandboxed bool) bool {
			return sandboxed && IsSandboxEnabled()
		},
	}
}
