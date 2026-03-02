package core

import (
	"fmt"
	"os"
	"strings"
)

const defaultMaxLines = 50

// ReadFileTool returns the read_file tool definition.
func ReadFileTool() *ToolDef {
	return &ToolDef{
		Name:        "read_file",
		Description: "Reads a single file's contents. For files over 100 lines, use line_start/line_end to read in chunks rather than loading the entire file. Start with a small range (e.g., first 50 lines) to understand structure, then read specific sections as needed. Full-file reads waste context and should be avoided.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "The path to the file to read, e.g. 'src/main.go' or './config.json'",
				},
				"line_start": map[string]any{
					"type":        "integer",
					"description": "1-indexed line number to start reading from. If omitted, reads from the beginning of the file.",
				},
				"line_end": map[string]any{
					"type":        "integer",
					"description": "1-indexed line number to stop reading at (inclusive). If omitted, reads to the end of the file. Use with line_start to read a specific range, e.g. line_start=10, line_end=20 returns lines 10-20.",
				},
				"show_line_numbers": map[string]any{
					"type":        "boolean",
					"description": "If true, prefix each line with its line number. Useful for referencing specific lines when planning edits. Defaults to false.",
					"default":     false,
				},
				"max_lines": map[string]any{
					"type":        "integer",
					"description": "Maximum number of lines to return. Useful for previewing large files without reading the entire content. Applied after line_start/line_end filtering.",
				},
			},
			"required": []any{"path"},
		},
		Execute: func(args map[string]any) ToolResult {
			path, _ := args["path"].(string)
			lineStart, _ := args["line_start"].(float64)
			lineEnd, _ := args["line_end"].(float64)
			maxLines, _ := args["max_lines"].(float64)
			showLineNumbers, _ := args["show_line_numbers"].(bool)
			return ExecuteReadFile(path, int(lineStart), int(lineEnd), int(maxLines), showLineNumbers)
		},
		AutoApprove: func(sandboxed bool) bool {
			return true
		},
	}
}

// ExecuteReadFile reads a file with optional line range and truncation.
func ExecuteReadFile(path string, lineStart, lineEnd, maxLines int, showLineNumbers bool) ToolResult {
	content, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   err,
			Status:  "fail: " + err.Error(),
		}
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	start := 0
	end := totalLines
	if lineStart > 0 {
		start = lineStart - 1
		if start > totalLines {
			start = totalLines
		}
	}
	if lineEnd > 0 {
		end = lineEnd
		if end > totalLines {
			end = totalLines
		}
	}
	if start > end {
		return ToolResult{
			Success: false,
			Error:   fmt.Errorf("line_start (%d) cannot be greater than line_end (%d)", lineStart, lineEnd),
			Status:  "fail: invalid line range",
		}
	}

	lines = lines[start:end]

	// If LLM didn't request a specific range, enforce a cap
	noRangeRequested := lineStart == 0 && lineEnd == 0 && maxLines == 0
	if noRangeRequested && len(lines) > defaultMaxLines {
		lines = lines[:defaultMaxLines]
		if showLineNumbers {
			numbered := make([]string, len(lines))
			for i, line := range lines {
				numbered[i] = fmt.Sprintf("%4d | %s", start+i+1, line)
			}
			lines = numbered
		}
		output := strings.Join(lines, "\n")
		output += fmt.Sprintf("\n\n[Truncated: showing first %d of %d lines. Use line_start/line_end to read specific sections.]", defaultMaxLines, totalLines)
		return ToolResult{
			Success: true,
			Output:  output,
			Status:  fmt.Sprintf("%d lines (of %d total, truncated)", defaultMaxLines, totalLines),
		}
	}

	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	if showLineNumbers {
		numbered := make([]string, len(lines))
		for i, line := range lines {
			numbered[i] = fmt.Sprintf("%4d | %s", start+i+1, line)
		}
		lines = numbered
	}

	output := strings.Join(lines, "\n")
	displayedLines := len(lines)

	status := fmt.Sprintf("%d lines", displayedLines)
	if start != 0 || end != totalLines || (maxLines > 0 && displayedLines < totalLines) {
		status = fmt.Sprintf("%d lines (of %d total)", displayedLines, totalLines)
	}

	return ToolResult{
		Success: true,
		Output:  output,
		Status:  status,
	}
}
