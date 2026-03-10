package core

import (
	"context"
	"fmt"
)

// EnterPlanModeTool returns the enter_plan_mode tool definition.
// runSubAgent is injected by the agent to invoke the planning subagent.
// This tool is auto-approved because planning is a read-only operation;
// if approval is needed, the subagent's hooks will handle it.
func EnterPlanModeTool(runSubAgent func(ctx context.Context, projectDesc string) (*SubAgentResult, error)) *ToolDef {
	return &ToolDef{
		Name: "enter_plan_mode",
		Description: "Starts the planning subagent to create a comprehensive planning document. " +
			"Use this tool when you need to plan a complex implementation before starting work. " +
			"The planning subagent will analyze the project description and produce a structured " +
			"planning document with context, requirements, design, and implementation tasks.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_description": map[string]any{
					"type":        "string",
					"description": "Description of the project or task to create a plan for",
				},
			},
			"required": []any{"project_description"},
		},
		Execute: func(args map[string]any) ToolResult {
			desc, ok := args["project_description"].(string)
			if !ok || desc == "" {
				return ToolResult{
					Success: false,
					Error:   fmt.Errorf("project_description is required"),
					Status:  "fail: missing project_description",
				}
			}

			// Use background context for subagent execution
			// (tool execution doesn't have access to request context)
			result, err := runSubAgent(context.Background(), desc)
			if err != nil {
				return ToolResult{
					Success: false,
					Error:   err,
					Status:  fmt.Sprintf("fail: %v", err),
				}
			}

			return ToolResult{
				Success: true,
				Output:  result.Content,
				Status:  "plan: created planning document",
			}
		},
		AutoApprove: func(sandboxed bool) bool {
			return true // Planning is a read-only operation
		},
	}
}
