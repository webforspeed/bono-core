package core

import (
	"context"
	"errors"
	"testing"
)

func TestEnterPlanModeTool_Success(t *testing.T) {
	wantContent := "<planning_document>Test plan content</planning_document>"
	runSubAgent := func(ctx context.Context, projectDesc string) (*SubAgentResult, error) {
		if projectDesc != "test project" {
			t.Errorf("unexpected project description: got %q, want %q", projectDesc, "test project")
		}
		return &SubAgentResult{Content: wantContent}, nil
	}

	tool := EnterPlanModeTool(runSubAgent)
	if tool.Name != "enter_plan_mode" {
		t.Errorf("unexpected tool name: got %q, want %q", tool.Name, "enter_plan_mode")
	}

	result := tool.Execute(map[string]any{"project_description": "test project"})
	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}
	if result.Output != wantContent {
		t.Errorf("unexpected output: got %q, want %q", result.Output, wantContent)
	}
	if result.Status != "plan: created planning document" {
		t.Errorf("unexpected status: got %q, want %q", result.Status, "plan: created planning document")
	}
}

func TestEnterPlanModeTool_MissingProjectDescription(t *testing.T) {
	runSubAgent := func(ctx context.Context, projectDesc string) (*SubAgentResult, error) {
		t.Error("runSubAgent should not be called with missing project_description")
		return nil, nil
	}

	tool := EnterPlanModeTool(runSubAgent)

	// Test missing parameter entirely
	result := tool.Execute(map[string]any{})
	if result.Success {
		t.Error("expected failure for missing project_description")
	}
	if result.Error == nil {
		t.Error("expected error for missing project_description")
	}
	if result.Status != "fail: missing project_description" {
		t.Errorf("unexpected status: got %q, want %q", result.Status, "fail: missing project_description")
	}
}

func TestEnterPlanModeTool_EmptyProjectDescription(t *testing.T) {
	runSubAgent := func(ctx context.Context, projectDesc string) (*SubAgentResult, error) {
		t.Error("runSubAgent should not be called with empty project_description")
		return nil, nil
	}

	tool := EnterPlanModeTool(runSubAgent)

	result := tool.Execute(map[string]any{"project_description": ""})
	if result.Success {
		t.Error("expected failure for empty project_description")
	}
	if result.Error == nil {
		t.Error("expected error for empty project_description")
	}
	if result.Status != "fail: missing project_description" {
		t.Errorf("unexpected status: got %q, want %q", result.Status, "fail: missing project_description")
	}
}

func TestEnterPlanModeTool_WrongTypeProjectDescription(t *testing.T) {
	runSubAgent := func(ctx context.Context, projectDesc string) (*SubAgentResult, error) {
		t.Error("runSubAgent should not be called with wrong type project_description")
		return nil, nil
	}

	tool := EnterPlanModeTool(runSubAgent)

	result := tool.Execute(map[string]any{"project_description": 123})
	if result.Success {
		t.Error("expected failure for wrong type project_description")
	}
	if result.Error == nil {
		t.Error("expected error for wrong type project_description")
	}
}

func TestEnterPlanModeTool_SubAgentError(t *testing.T) {
	wantErr := errors.New("subagent error")
	runSubAgent := func(ctx context.Context, projectDesc string) (*SubAgentResult, error) {
		return nil, wantErr
	}

	tool := EnterPlanModeTool(runSubAgent)

	result := tool.Execute(map[string]any{"project_description": "test project"})
	if result.Success {
		t.Error("expected failure when subagent returns error")
	}
	if result.Error != wantErr {
		t.Errorf("unexpected error: got %v, want %v", result.Error, wantErr)
	}
}

func TestEnterPlanModeTool_AutoApprove(t *testing.T) {
	runSubAgent := func(ctx context.Context, projectDesc string) (*SubAgentResult, error) {
		return nil, nil
	}

	tool := EnterPlanModeTool(runSubAgent)

	// AutoApprove should return true for both sandboxed and non-sandboxed
	if !tool.AutoApprove(true) {
		t.Error("AutoApprove(true) should return true")
	}
	if !tool.AutoApprove(false) {
		t.Error("AutoApprove(false) should return true")
	}
}

func TestEnterPlanModeTool_ParameterSchema(t *testing.T) {
	runSubAgent := func(ctx context.Context, projectDesc string) (*SubAgentResult, error) {
		return nil, nil
	}

	tool := EnterPlanModeTool(runSubAgent)

	// Verify parameter schema structure
	params := tool.Parameters
	if params == nil {
		t.Fatal("Parameters is nil")
	}

	paramsType, ok := params["type"].(string)
	if !ok || paramsType != "object" {
		t.Errorf("expected parameters type 'object', got %q", paramsType)
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties is not a map")
	}

	projectDesc, ok := props["project_description"].(map[string]any)
	if !ok {
		t.Fatal("project_description property is not a map")
	}

	propType, ok := projectDesc["type"].(string)
	if !ok || propType != "string" {
		t.Errorf("expected project_description type 'string', got %q", propType)
	}

	required, ok := params["required"].([]any)
	if !ok {
		t.Fatal("required is not a slice")
	}
	if len(required) != 1 {
		t.Fatalf("expected 1 required field, got %d", len(required))
	}
	if required[0] != "project_description" {
		t.Errorf("expected required field 'project_description', got %q", required[0])
	}
}

func TestEnterPlanModeTool_Description(t *testing.T) {
	runSubAgent := func(ctx context.Context, projectDesc string) (*SubAgentResult, error) {
		return nil, nil
	}

	tool := EnterPlanModeTool(runSubAgent)

	if tool.Description == "" {
		t.Error("tool description should not be empty")
	}
	// Verify key phrases in description
	if !containsAll(tool.Description, "planning subagent", "planning document") {
		t.Errorf("tool description should mention 'planning subagent' and 'planning document', got: %s", tool.Description)
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if !contains(s, substr) {
			return false
		}
	}
	return true
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
