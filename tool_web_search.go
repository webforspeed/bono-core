package core

import "context"

// WebSearchTool creates a ToolDef for the WebSearch tool.
// The searchFn is injected by WebService and handles routing + backend calls.
func WebSearchTool(searchFn func(ctx context.Context, query, mode string) ToolResult) *ToolDef {
	return &ToolDef{
		Name: "WebSearch",
		Description: "Search the web for information. " +
			"Use mode=\"answer\" for factual questions, current events, and summaries — returns a synthesized answer with sources. " +
			"Use mode=\"search\" for specific pages, documentation, repos, or URLs — returns a list of results with links. " +
			"Omit mode to let the system decide. " +
			"Use WebFetch to read the full content of any URL returned.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "What you want to find or know.",
				},
				"mode": map[string]any{
					"type":        "string",
					"enum":        []any{"search", "answer"},
					"description": "\"search\" returns URL list; \"answer\" returns synthesized answer. Omit to auto-detect.",
				},
			},
			"required": []any{"query"},
		},
		Execute: func(args map[string]any) ToolResult {
			query, _ := args["query"].(string)
			if query == "" {
				return ToolResult{
					Success: false,
					Output:  "query parameter is required",
					Status:  "fail: empty query",
				}
			}
			mode, _ := args["mode"].(string)
			return searchFn(context.Background(), query, mode)
		},
		AutoApprove: func(sandboxed bool) bool {
			return true
		},
	}
}
