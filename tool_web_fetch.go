package core

import "context"

// WebFetchTool creates a ToolDef for the WebFetch tool.
// The fetchFn is injected by WebService and handles URL fetching + summarization.
func WebFetchTool(fetchFn func(ctx context.Context, url, question string) ToolResult) *ToolDef {
	return &ToolDef{
		Name: "WebFetch",
		Description: "Fetch and read the content of a specific URL. Use this when you already have a URL " +
			"and need to read what is on the page — documentation, source code, articles, or any web content. " +
			"Returns a plain text rendering of the page.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The full URL to fetch.",
				},
				"question": map[string]any{
					"type":        "string",
					"description": "Optional. A specific question to answer from the page content. If omitted, a general summary is returned.",
				},
			},
			"required": []any{"url"},
		},
		Execute: func(args map[string]any) ToolResult {
			url, _ := args["url"].(string)
			if url == "" {
				return ToolResult{
					Success: false,
					Output:  "url parameter is required",
					Status:  "fail: empty url",
				}
			}
			question, _ := args["question"].(string)
			return fetchFn(context.Background(), url, question)
		},
		AutoApprove: func(sandboxed bool) bool {
			return true
		},
	}
}
