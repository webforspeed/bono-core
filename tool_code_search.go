package core

// SearchFunc is the function signature for code search.
// Called by the tool with the query string and raw args map from the LLM.
type SearchFunc func(query string, opts map[string]any) ToolResult

// CodeSearchTool returns the code_search tool definition.
// searchFn is injected by the caller with the actual search implementation.
func CodeSearchTool(searchFn SearchFunc) *ToolDef {
	return &ToolDef{
		Name: "code_search",
		Description: `Search the indexed codebase by meaning, intent, and structure—not just exact text.
Best for locating implementations, tracing patterns across files, understanding architecture, and finding code similar to what is already in view.
Use natural language queries like "where is authentication handled?" or "error handling in HTTP handlers".
Requires an index (run /index). If semantic indexing is unavailable, search degrades toward exact text matching.
Returns ranked matches with file paths, line numbers, and code snippets.`,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Natural language search query describing what you're looking for, e.g. 'user authentication login flow' or 'HTTP middleware error handling'",
				},
				"file_patterns": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "File glob patterns to include, e.g. ['*.go', 'src/**/*.ts']. Empty means all indexed files.",
				},
				"exclude_patterns": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "File/directory patterns to exclude, e.g. ['vendor/', '*_test.go', 'node_modules/']",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return. Default: 10.",
					"default":     10,
				},
				"search_type": map[string]any{
					"type":        "string",
					"enum":        []any{"semantic", "hybrid", "exact"},
					"description": "Search strategy: 'semantic' (vector similarity, default), 'hybrid' (semantic + keyword), 'exact' (keyword/text only).",
					"default":     "semantic",
				},
				"scope": map[string]any{
					"type":        "string",
					"enum":        []any{"functions", "classes", "comments", "all"},
					"description": "Limit search to specific code element types. Default: all.",
				},
				"context_window": map[string]any{
					"type":        "integer",
					"description": "Lines of context to include around each match. Default: 5.",
					"default":     5,
				},
				"snippet_max_lines": map[string]any{
					"type":        "integer",
					"description": "Maximum lines per code snippet. Default: 20.",
					"default":     20,
				},
				"group_by_file": map[string]any{
					"type":        "boolean",
					"description": "Group results by file path. Default: false.",
					"default":     false,
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
			return searchFn(query, args)
		},
		AutoApprove: func(sandboxed bool) bool {
			return true // read-only operation
		},
	}
}
