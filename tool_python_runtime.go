package core

import (
	"encoding/base64"
	"strings"
)

// PythonRuntimeTool returns the python_runtime tool definition.
// exec is the shell executor — the agent injects sandbox+fallback handling.
func PythonRuntimeTool(exec func(req ShellRequest) ToolResult) *ToolDef {
	return &ToolDef{
		Name:        "python_runtime",
		Description: "Runs a Python script and returns stdout/stderr. Combines multiple operations into a single step — file reads, data processing, shell commands via subprocess, and output formatting all happen inside the script. Intermediate data stays in the script and never enters the conversation. Only the final print() output is returned. Particularly effective for: structured data (JSON, CSV, regex parsing), multi-file operations, aggregation/counting, and any task that would otherwise require multiple sequential tool calls.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"code": map[string]any{
					"type":        "string",
					"description": "The Python code to execute.",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Brief description of what this code does, for display",
				},
				"safety": map[string]any{
					"type":        "string",
					"enum":        []any{"read-only", "modify", "destructive", "network", "privileged"},
					"description": "Safety level: read-only, modify, destructive, network, privileged",
				},
			},
			"required": []any{"code", "description", "safety"},
		},
		Execute: func(args map[string]any) ToolResult {
			req := ShellRequestFromToolArgs("python_runtime", args)
			return exec(req)
		},
		AutoApprove: func(sandboxed bool) bool {
			return sandboxed && IsSandboxEnabled()
		},
	}
}

// ExecutePython runs Python code using the configured executor (sandboxed or passthrough).
func ExecutePython(code string) ToolResult {
	command := pythonCommand(code)
	return ExecuteShell(command)
}

func pythonCommand(code string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(code))
	var b strings.Builder
	b.WriteString("python3 - <<'PY'\n")
	b.WriteString("import base64\n")
	b.WriteString("code = base64.b64decode('")
	b.WriteString(encoded)
	b.WriteString("')\n")
	b.WriteString("exec(compile(code, '<bono>', 'exec'))\n")
	b.WriteString("PY\n")
	return b.String()
}
