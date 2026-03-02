package core

import (
	"fmt"
	"os"
	"strings"
)

// EditFileTool returns the edit_file tool definition.
func EditFileTool() *ToolDef {
	return &ToolDef{
		Name:        "edit_file",
		Description: "Performs a search-and-replace operation in a file. Use this for making targeted changes to existing files. The old_string must match exactly including all whitespace and newlines. Fails if old_string is not found. Fails if old_string matches multiple times unless replace_all is true. Always use read_file first to see the exact text you need to match.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "The path to the file to edit",
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "The exact text to find and replace. Must match character-for-character including whitespace and newlines.",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "The replacement text. Can be empty string to delete the matched text.",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"default":     false,
					"description": "If true, replaces all occurrences. If false (default), fails when multiple matches exist.",
				},
			},
			"required": []any{"path", "old_string", "new_string"},
		},
		Execute: func(args map[string]any) ToolResult {
			path, _ := args["path"].(string)
			oldStr, _ := args["old_string"].(string)
			newStr, _ := args["new_string"].(string)
			replaceAll, _ := args["replace_all"].(bool)
			return ExecuteEditFile(path, oldStr, newStr, replaceAll)
		},
		AutoApprove: func(sandboxed bool) bool {
			return false
		},
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
