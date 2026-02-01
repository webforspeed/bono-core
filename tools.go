package core

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
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

// ExecuteShell runs a shell command and returns output.
func ExecuteShell(command string) ToolResult {
	start := time.Now()
	out, err := exec.Command("sh", "-c", command).CombinedOutput()
	elapsed := time.Since(start).Seconds()

	if err != nil {
		return ToolResult{
			Success: false,
			Output:  string(out),
			Error:   err,
			Status:  fmt.Sprintf("fail (%.1fs)", elapsed),
		}
	}

	return ToolResult{
		Success: true,
		Output:  string(out),
		Status:  fmt.Sprintf("ok (%.1fs)", elapsed),
	}
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
	default:
		return ToolResult{
			Success: false,
			Error:   fmt.Errorf("unknown tool: %s", name),
			Status:  "fail: unknown tool",
		}
	}
}

// RequiresConfirmation returns true if the tool should prompt user before executing.
func RequiresConfirmation(name string) bool {
	return name != "read_file"
}
