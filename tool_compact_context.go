package core

// CompactContextTool returns the compact_context tool definition.
// compact is the function that replaces conversation history with a summary.
func CompactContextTool(compact func(summary string) ToolResult) *ToolDef {
	return &ToolDef{
		Name:        "compact_context",
		Description: "Replaces earlier conversation history with your summary to free context. The <context_usage> tag in messages shows current usage — as it climbs, older messages get pushed into lower-attention regions where details are effectively lost. Use this at natural checkpoints (after exploration, before starting implementation, after completing a subtask) to preserve important context in a high-attention position. Write a working-state summary covering: the task, key findings, changes made, decisions and rationale, and what's pending. Focus on what's needed to continue — skip files read but not relevant, commands that didn't produce useful results, etc.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]any{
					"type":        "string",
					"description": "Your working-state summary of the conversation so far. This replaces the older messages, so include everything needed to continue the task without re-reading or re-doing prior work.",
				},
			},
			"required": []any{"summary"},
		},
		Execute: func(args map[string]any) ToolResult {
			summary, _ := args["summary"].(string)
			return compact(summary)
		},
		AutoApprove: func(sandboxed bool) bool {
			return true
		},
	}
}
