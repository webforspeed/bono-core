package core

import "testing"

func TestDefaultShellPolicyRoutesKnownCommandsOutsideSandbox(t *testing.T) {
	tests := []ShellRequest{
		{ToolName: "run_shell", Command: "npm install agent-browser", Safety: "modify"},
		{ToolName: "run_shell", Command: "git push origin main", Safety: "modify"},
		{ToolName: "run_shell", Command: "curl https://example.com", Safety: "modify"},
		{ToolName: "run_shell", Command: "echo hello", Safety: "network"},
	}

	for _, req := range tests {
		decision := DecideShellRequest(nil, req)
		if decision.Route != ShellRouteHostDirect {
			t.Fatalf("expected host-direct route for %q, got %#v", req.Command, decision)
		}
		if decision.Reason == "" {
			t.Fatalf("expected reason for %q", req.Command)
		}
	}
}

func TestDefaultShellPolicyLeavesSafeCommandsSandboxed(t *testing.T) {
	req := ShellRequest{ToolName: "run_shell", Command: "git status", Safety: "read-only"}

	decision := DecideShellRequest(nil, req)
	if decision.Route != ShellRouteSandboxFirst {
		t.Fatalf("expected sandbox-first route, got %#v", decision)
	}
}

func TestExecuteShellRequestUsesHostDirectRoute(t *testing.T) {
	req := ShellRequest{ToolName: "run_shell", Command: "false", Safety: "network"}

	result := ExecuteShellRequest(req, nil, func(cmd, reason string) bool {
		t.Fatal("host-direct route should not invoke sandbox fallback")
		return false
	})

	if result.Success {
		t.Fatal("expected false to fail")
	}
	if result.ExecMeta == nil || result.ExecMeta.Sandboxed {
		t.Fatal("expected unsandboxed execution for host-direct route")
	}
}
