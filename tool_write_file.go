package core

import (
	"os"
	"path/filepath"
)

// WriteFileTool returns the write_file tool definition.
func WriteFileTool() *ToolDef {
	return &ToolDef{
		Name:        "write_file",
		Description: "Creates a new file or completely overwrites an existing file with the provided content. Use this only when creating new files or replacing entire file contents. For partial modifications, use edit_file instead. Creates parent directories automatically if they don't exist. File is written with 0644 permissions.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "The path where the file should be written. Parent directories are created automatically.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The complete text content to write. This replaces any existing content entirely.",
				},
			},
			"required": []any{"path", "content"},
		},
		Execute: func(args map[string]any) ToolResult {
			path, _ := args["path"].(string)
			content, _ := args["content"].(string)
			return ExecuteWriteFile(path, content)
		},
		AutoApprove: func(sandboxed bool) bool {
			return false
		},
	}
}

// ExecuteWriteFile writes content to a file, creating parent directories if needed.
func ExecuteWriteFile(path, content string) ToolResult {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return ToolResult{
			Success: false,
			Error:   err,
			Status:  "fail: " + err.Error(),
		}
	}
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
