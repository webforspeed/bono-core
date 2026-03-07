package core

import "strings"

// ExecuteShell runs a shell command using the configured executor (sandboxed or passthrough).
func ExecuteShell(command string) ToolResult {
	result, _ := GetExecutor().Run(command)
	return result
}

// ExecuteShellUnsandboxed runs a shell command directly without sandboxing.
// Used for fallback when sandbox blocks a command and user approves unsandboxed execution.
func ExecuteShellUnsandboxed(command string) ToolResult {
	passthrough := &PassthroughExecutor{}
	result, _ := passthrough.Run(command)
	return result
}

// ExecuteShellRequest routes a shell-backed request according to policy.
func ExecuteShellRequest(req ShellRequest, policy ShellPolicy, onFallback func(cmd, reason string) bool) ToolResult {
	decision := DecideShellRequest(policy, req)
	if decision.Route == ShellRouteHostDirect {
		return ExecuteShellUnsandboxed(req.Command)
	}
	return ExecuteShellWithSandbox(req.Command, onFallback)
}

// ExecuteShellWithSandbox executes a shell command with sandbox support.
// If a sandboxed execution fails and fallback is enabled, onFallback can approve
// re-running the command outside the sandbox.
func ExecuteShellWithSandbox(command string, onFallback func(cmd, reason string) bool) ToolResult {
	if !IsSandboxEnabled() {
		// No sandbox available - execute directly
		return ExecuteShellUnsandboxed(command)
	}

	// Try sandboxed execution
	result := ExecuteShell(command)

	// Any sandboxed failure can offer host fallback. Some sandboxed network failures
	// surface as normal command errors (for example DNS resolution) rather than explicit
	// sandbox denials, so relying on SandboxError alone misses the approval path.
	if shouldOfferSandboxFallback(result) {
		if onFallback != nil && onFallback(command, sandboxFallbackReason(result)) {
			return ExecuteShellUnsandboxed(command)
		}
		return result
	}

	return result
}

func shouldOfferSandboxFallback(result ToolResult) bool {
	if result.Success || result.ExecMeta == nil || !result.ExecMeta.Sandboxed {
		return false
	}
	return SandboxFallbackEnabled()
}

func sandboxFallbackReason(result ToolResult) string {
	if result.ExecMeta != nil && result.ExecMeta.SandboxReason != "" {
		return result.ExecMeta.SandboxReason
	}

	output := strings.TrimSpace(result.Output)
	if output == "" && result.Error != nil {
		output = strings.TrimSpace(result.Error.Error())
	}
	if output == "" {
		return "command failed inside sandbox"
	}
	if len(output) > 100 {
		output = output[:100] + "..."
	}
	return "command failed inside sandbox: " + output
}
