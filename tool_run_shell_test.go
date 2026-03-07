package core

import "testing"

func TestIsExplorationCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		// Directory listing
		{"ls", true},
		{"ls -la", true},
		{"ls -la /some/path", true},
		{"tree", true},
		{"tree -L 2", true},
		{"exa --long", true},
		{"eza --icons", true},

		// File finding
		{"find . -name '*.go'", true},
		{"fd main.go", true},
		{"locate readme", true},

		// Content search
		{"grep -rn pattern .", true},
		{"rg TODO", true},
		{"ag pattern", true},
		{"ack pattern", true},

		// Git structure
		{"git log --oneline", true},
		{"git status", true},
		{"git diff HEAD~1", true},
		{"git show abc123", true},
		{"git ls-files", true},

		// Project sizing
		{"wc -l *.go", true},
		{"du -sh .", true},

		// Leading whitespace
		{"  ls", true},
		{"  find . -name '*.go'", true},

		// Non-exploration commands
		{"echo hello", false},
		{"go build ./...", false},
		{"cat main.go", false},
		{"head -20 file.go", false},
		{"rm -rf /tmp/foo", false},
		{"make build", false},
		{"npm install", false},
		{"cp a b", false},
		{"mv a b", false},
		{"mkdir newdir", false},
		{"git commit -m 'msg'", false},
		{"git push origin main", false},
	}
	for _, tt := range tests {
		if got := isExplorationCommand(tt.cmd); got != tt.want {
			t.Errorf("isExplorationCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestRunShellToolNudgeFiresOnce(t *testing.T) {
	callCount := 0
	exec := func(req ShellRequest) ToolResult {
		callCount++
		return ToolResult{Success: true, Output: "output-" + req.Command}
	}

	tool := RunShellTool(exec)

	// First exploration command — nudge should fire
	r1 := tool.Execute(map[string]any{"command": "ls -la"})
	if got := r1.Output; got == "output-ls -la" {
		t.Error("expected nudge appended to first exploration command output")
	}
	nudge := explorationNudge()
	if got := r1.Output; got != "output-ls -la\n\n"+nudge {
		t.Errorf("first exploration output = %q, want output + nudge", got)
	}

	// Second exploration command — nudge should NOT fire again
	r2 := tool.Execute(map[string]any{"command": "find . -name '*.go'"})
	if got := r2.Output; got != "output-find . -name '*.go'" {
		t.Errorf("second exploration output = %q, want raw output (no nudge)", got)
	}
}

func TestRunShellToolNudgeSkipsNonExploration(t *testing.T) {
	exec := func(req ShellRequest) ToolResult {
		return ToolResult{Success: true, Output: "output"}
	}

	tool := RunShellTool(exec)

	// Non-exploration command — no nudge
	r1 := tool.Execute(map[string]any{"command": "echo hello"})
	if r1.Output != "output" {
		t.Errorf("non-exploration output = %q, want %q", r1.Output, "output")
	}

	// Now an exploration command — nudge should fire (first time)
	r2 := tool.Execute(map[string]any{"command": "ls"})
	nudge := explorationNudge()
	if r2.Output != "output\n\n"+nudge {
		t.Errorf("first exploration after non-exploration = %q, want output + nudge", r2.Output)
	}
}

func TestRunShellToolNudgeSkipsFailedCommands(t *testing.T) {
	exec := func(req ShellRequest) ToolResult {
		return ToolResult{Success: false, Output: "error: not found"}
	}

	tool := RunShellTool(exec)

	// Failed exploration command — no nudge
	r := tool.Execute(map[string]any{"command": "ls /nonexistent"})
	if r.Output != "error: not found" {
		t.Errorf("failed command output = %q, want %q", r.Output, "error: not found")
	}
}
