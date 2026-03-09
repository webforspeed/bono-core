package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"normal text", "Add user authentication to the API", 50, "add-user-authentication-to-the-api"},
		{"special characters", "Fix bug #123 — can't login!!!", 50, "fix-bug-123-can-t-login"},
		{"long input", "this is a very long input string that should be truncated at fifty characters exactly here", 50, "this-is-a-very-long-input-string-that-should-be-tr"},
		{"empty input", "", 50, ""},
		{"only special chars", "!@#$%^&*()", 50, ""},
		{"leading trailing hyphens", "---hello---", 50, "hello"},
		{"consecutive hyphens", "hello   world", 50, "hello-world"},
		{"truncate trailing hyphen", "abcdefghij-", 10, "abcdefghij"},
		{"single word", "hello", 50, "hello"},
		{"numbers", "step 1 and step 2", 50, "step-1-and-step-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slugify(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("slugify(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestPersistHook(t *testing.T) {
	t.Run("creates file and sets meta", func(t *testing.T) {
		dir := t.TempDir()
		hook := PersistHook(dir)

		result := &SubAgentResult{
			Name:    "plan",
			Input:   "add auth",
			Content: "# Plan\nStep 1: do the thing",
			CWD:     "/some/project",
			Meta:    make(map[string]string),
		}

		err := hook.AfterComplete(context.Background(), result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		outputPath := result.Meta["output_path"]
		if outputPath == "" {
			t.Fatal("output_path not set in Meta")
		}

		content, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}
		if string(content) != result.Content {
			t.Errorf("file content = %q, want %q", string(content), result.Content)
		}
	})

	t.Run("creates nested directories", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "deep", "nested")
		hook := PersistHook(dir)

		result := &SubAgentResult{
			Input:   "test",
			Content: "content",
			Meta:    make(map[string]string),
		}

		err := hook.AfterComplete(context.Background(), result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(result.Meta["output_path"]); err != nil {
			t.Errorf("output file does not exist: %v", err)
		}
	})

	t.Run("cwd template expansion", func(t *testing.T) {
		base := t.TempDir()
		template := base + "/{cwd}/plans"
		hook := PersistHook(template)

		result := &SubAgentResult{
			Input:   "test",
			Content: "content",
			CWD:     "/Users/alice/myproject",
			Meta:    make(map[string]string),
		}

		err := hook.AfterComplete(context.Background(), result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		outputPath := result.Meta["output_path"]
		if !filepath.HasPrefix(outputPath, filepath.Join(base, "Users/alice/myproject/plans")) {
			t.Errorf("output_path %q does not contain expected CWD expansion", outputPath)
		}
	})

	t.Run("revision overwrites same file", func(t *testing.T) {
		dir := t.TempDir()
		hook := PersistHook(dir)

		result := &SubAgentResult{
			Input:   "test",
			Content: "version 1",
			Meta:    make(map[string]string),
		}

		hook.AfterComplete(context.Background(), result)
		path := result.Meta["output_path"]

		// Simulate revision: same Meta with output_path already set
		result.Content = "version 2"
		err := hook.AfterComplete(context.Background(), result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Meta["output_path"] != path {
			t.Errorf("output_path changed on revision: %q -> %q", path, result.Meta["output_path"])
		}

		content, _ := os.ReadFile(path)
		if string(content) != "version 2" {
			t.Errorf("file content = %q, want %q", string(content), "version 2")
		}
	})
}

func TestApprovalHook(t *testing.T) {
	t.Run("nil callback auto-approves", func(t *testing.T) {
		hook := ApprovalHook(func() func(SubAgentResult) SubAgentApprovalResponse { return nil })

		result := &SubAgentResult{Meta: make(map[string]string)}
		err := hook.AfterComplete(context.Background(), result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Meta["approval"] != "approved" {
			t.Errorf("approval = %q, want %q", result.Meta["approval"], "approved")
		}
	})

	t.Run("approve", func(t *testing.T) {
		hook := ApprovalHook(func() func(SubAgentResult) SubAgentApprovalResponse {
			return func(SubAgentResult) SubAgentApprovalResponse {
				return SubAgentApprovalResponse{Action: SubAgentApprove}
			}
		})

		result := &SubAgentResult{Meta: make(map[string]string)}
		hook.AfterComplete(context.Background(), result)
		if result.Meta["approval"] != "approved" {
			t.Errorf("approval = %q, want %q", result.Meta["approval"], "approved")
		}
	})

	t.Run("reject", func(t *testing.T) {
		hook := ApprovalHook(func() func(SubAgentResult) SubAgentApprovalResponse {
			return func(SubAgentResult) SubAgentApprovalResponse {
				return SubAgentApprovalResponse{Action: SubAgentReject}
			}
		})

		result := &SubAgentResult{Meta: make(map[string]string)}
		hook.AfterComplete(context.Background(), result)
		if result.Meta["approval"] != "rejected" {
			t.Errorf("approval = %q, want %q", result.Meta["approval"], "rejected")
		}
	})

	t.Run("revise with feedback", func(t *testing.T) {
		hook := ApprovalHook(func() func(SubAgentResult) SubAgentApprovalResponse {
			return func(SubAgentResult) SubAgentApprovalResponse {
				return SubAgentApprovalResponse{Action: SubAgentRevise, Feedback: "add more detail"}
			}
		})

		result := &SubAgentResult{Meta: make(map[string]string)}
		hook.AfterComplete(context.Background(), result)
		if result.Meta["approval"] != "revise" {
			t.Errorf("approval = %q, want %q", result.Meta["approval"], "revise")
		}
		if result.Meta["feedback"] != "add more detail" {
			t.Errorf("feedback = %q, want %q", result.Meta["feedback"], "add more detail")
		}
	})
}
