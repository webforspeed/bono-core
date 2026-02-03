package core

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// ExecuteReadFile reads a file and returns its contents.
func ExecuteReadFile(path string) ToolResult {
	content, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   err,
			Status:  "fail: " + err.Error(),
		}
	}
	lines := len(strings.Split(string(content), "\n"))
	return ToolResult{
		Success: true,
		Output:  string(content),
		Status:  fmt.Sprintf("%d lines", lines),
	}
}

// ExecuteWriteFile writes content to a file.
func ExecuteWriteFile(path, content string) ToolResult {
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   err,
			Status:  "fail: " + err.Error(),
		}
	}
	return ToolResult{
		Success: true,
		Output:  "ok",
		Status:  "written",
	}
}

// ExecuteEditFile performs string replacement in a file.
func ExecuteEditFile(path, oldStr, newStr string, replaceAll bool) ToolResult {
	content, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   err,
			Status:  "fail: " + err.Error(),
		}
	}

	count := strings.Count(string(content), oldStr)
	if count == 0 {
		return ToolResult{
			Success: false,
			Error:   ErrStringNotFound,
			Status:  "fail: string not found",
		}
	}

	if count > 1 && !replaceAll {
		return ToolResult{
			Success: false,
			Error:   ErrMultipleMatches,
			Status:  fmt.Sprintf("fail: %d matches (use replace_all)", count),
		}
	}

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(string(content), oldStr, newStr)
	} else {
		newContent = strings.Replace(string(content), oldStr, newStr, 1)
	}

	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return ToolResult{
			Success: false,
			Error:   err,
			Status:  "fail: " + err.Error(),
		}
	}

	return ToolResult{
		Success: true,
		Output:  fmt.Sprintf("replaced %d occurrence(s)", count),
		Status:  "ok",
	}
}

// ExecuteShell runs a shell command using the configured executor (sandboxed or passthrough).
func ExecuteShell(command string) ToolResult {
	result, _ := GetExecutor().Run(command)
	return result
}

// ExecutePython runs Python code using the configured executor (sandboxed or passthrough).
func ExecutePython(code string) ToolResult {
	command := pythonCommand(code)
	return ExecuteShell(command)
}

// ExecuteShellUnsandboxed runs a shell command directly without sandboxing.
// Used for fallback when sandbox blocks a command and user approves unsandboxed execution.
func ExecuteShellUnsandboxed(command string) ToolResult {
	passthrough := &PassthroughExecutor{}
	result, _ := passthrough.Run(command)
	return result
}

// ExecuteShellWithSandbox executes a shell command with sandbox support.
// If sandbox blocks the command and onFallback returns true, executes unsandboxed.
// onFallback receives (command, reason) and returns true to approve unsandboxed execution.
func ExecuteShellWithSandbox(command string, onFallback func(cmd, reason string) bool) ToolResult {
	if !IsSandboxEnabled() {
		// No sandbox available - execute directly
		return ExecuteShellUnsandboxed(command)
	}

	// Try sandboxed execution
	result := ExecuteShell(command)

	// Check if sandbox blocked
	if result.ExecMeta != nil && result.ExecMeta.SandboxError {
		if onFallback != nil && onFallback(command, result.ExecMeta.SandboxReason) {
			return ExecuteShellUnsandboxed(command)
		}
		return result
	}

	return result
}

// ExecuteTool dispatches to the appropriate tool function.
func ExecuteTool(name string, args map[string]any) ToolResult {
	switch name {
	case "read_file":
		path, _ := args["path"].(string)
		return ExecuteReadFile(path)
	case "write_file":
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		return ExecuteWriteFile(path, content)
	case "edit_file":
		path, _ := args["path"].(string)
		oldStr, _ := args["old_string"].(string)
		newStr, _ := args["new_string"].(string)
		replaceAll, _ := args["replace_all"].(bool)
		return ExecuteEditFile(path, oldStr, newStr, replaceAll)
	case "run_shell":
		cmd, _ := args["command"].(string)
		return ExecuteShell(cmd)
	case "python_runtime":
		code, _ := args["code"].(string)
		return ExecutePython(code)
	default:
		return ToolResult{
			Success: false,
			Error:   fmt.Errorf("unknown tool: %s", name),
			Status:  "fail: unknown tool",
		}
	}
}

// RequiresConfirmation returns true if the tool should prompt user before executing.
// When sandboxed is true and sandbox is enabled, shell commands don't require confirmation.
func RequiresConfirmation(name string, sandboxed bool) bool {
	if name == "read_file" {
		return false
	}
	if (name == "run_shell" || name == "python_runtime") && sandboxed && IsSandboxEnabled() {
		return false // sandboxed shell commands don't need approval
	}
	return true
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
