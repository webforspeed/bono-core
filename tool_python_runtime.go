package core

import (
	"encoding/base64"
	"strings"
)

// PythonRuntimeTool returns the python_runtime tool definition.
// exec is the shell executor — the agent injects sandbox+fallback handling.
func PythonRuntimeTool(exec func(cmd string) ToolResult) *ToolDef {
	return &ToolDef{
		Name:        "python_runtime",
		Description: "Executes Python code in the sandbox. Has full access to the filesystem (open/read/write), subprocess.run() for shell commands, and the standard library. When a task involves multiple related steps — reading several files, processing data, making edits, running commands — consider combining them into a single script. Process results in code and only print what's relevant — raw intermediate data never needs to leave the script. Returns stdout/stderr as output.",
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
			code, _ := args["code"].(string)
			return exec(pythonCommand(code))
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
