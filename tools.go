package core

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

// ExecuteShellWithSandbox executes a shell command with sandbox support.
// If sandbox blocks the command and onFallback returns true, executes unsandboxed.
// onFallback receives (command, reason) and returns true to approve unsandboxed execution.
func ExecuteShellWithSandbox(command string, onFallback func(cmd, reason string) bool) ToolResult {
	if !IsSandboxEnabled() {
		// No sandbox available - execute directly
		return ExecuteShellUnsandboxed(command)
	}

	// Try sandboxed execution
	result := ExecuteShell(command)

	// Check if sandbox blocked
	if result.ExecMeta != nil && result.ExecMeta.SandboxError {
		if onFallback != nil && onFallback(command, result.ExecMeta.SandboxReason) {
			return ExecuteShellUnsandboxed(command)
		}
		return result
	}

	return result
}
