package core

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ShellExecutor abstracts shell command execution.
type ShellExecutor interface {
	// Run executes a shell command and returns the result with execution metadata.
	Run(command string) (ToolResult, ExecMeta)
}

// sandboxAvailable checks if macOS sandbox-exec is available.
func sandboxAvailable() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := exec.LookPath("sandbox-exec")
	return err == nil
}

// NewShellExecutor creates the appropriate executor based on config and platform.
// Returns a SandboxedExecutor on macOS with sandbox enabled, otherwise PassthroughExecutor.
func NewShellExecutor(cfg SandboxConfig) ShellExecutor {
	if cfg.Enabled && sandboxAvailable() {
		return &SandboxedExecutor{config: cfg}
	}
	return &PassthroughExecutor{}
}

// DefaultSandboxConfig returns sensible defaults for sandbox configuration.
func DefaultSandboxConfig() SandboxConfig {
	cwd, _ := os.Getwd()
	return SandboxConfig{
		Enabled:      sandboxAvailable(),
		AllowNetwork: false,
		ReadPaths: []string{
			"/",
		},
		WritePaths: []string{
			cwd,
			os.TempDir(),
			"/private/tmp",
			"/private/var/folders", // macOS temp folders
		},
		ExecPaths: []string{
			"/bin",
			"/usr/bin",
			"/usr/local/bin",
			"/opt/homebrew/bin",
			"/usr/sbin",
			"/sbin",
		},
		FallbackOutsideSandbox: true,
	}
}

// SandboxedExecutor executes commands inside macOS sandbox-exec.
type SandboxedExecutor struct {
	config SandboxConfig
}

// Run executes a command inside the sandbox.
func (s *SandboxedExecutor) Run(command string) (ToolResult, ExecMeta) {
	profile := s.generateProfile()

	start := time.Now()
	cmd := exec.Command("sandbox-exec", "-p", profile, "sh", "-c", command)
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start).Seconds()

	meta := ExecMeta{Sandboxed: true}

	if err != nil {
		// Check if this is a sandbox denial vs regular command failure
		output := string(out)
		if isSandboxDenial(output, err) {
			meta.SandboxError = true
			meta.SandboxReason = extractSandboxReason(output)
			return ToolResult{
				Success:  false,
				Output:   output,
				Error:    err,
				Status:   fmt.Sprintf("sandbox blocked (%.1fs)", elapsed),
				ExecMeta: &meta,
			}, meta
		}

		// Regular command failure (not sandbox related)
		return ToolResult{
			Success:  false,
			Output:   output,
			Error:    err,
			Status:   fmt.Sprintf("fail (%.1fs)", elapsed),
			ExecMeta: &meta,
		}, meta
	}

	return ToolResult{
		Success:  true,
		Output:   string(out),
		Status:   fmt.Sprintf("ok (%.1fs)", elapsed),
		ExecMeta: &meta,
	}, meta
}

// generateProfile creates a sandbox-exec SBPL profile string.
func (s *SandboxedExecutor) generateProfile() string {
	var sb strings.Builder

	sb.WriteString("(version 1)\n")
	sb.WriteString("(deny default)\n")

	// Allow process execution
	sb.WriteString("(allow process-fork)\n")
	sb.WriteString("(allow process-exec)\n")

	// Allow reading from specified paths (default: everywhere)
	for _, path := range s.config.ReadPaths {
		sb.WriteString(fmt.Sprintf("(allow file-read* (subpath \"%s\"))\n", path))
	}

	// Allow writing to specified paths
	for _, path := range s.config.WritePaths {
		sb.WriteString(fmt.Sprintf("(allow file-write* (subpath \"%s\"))\n", path))
	}

	// Allow execution from specified paths
	for _, path := range s.config.ExecPaths {
		sb.WriteString(fmt.Sprintf("(allow process-exec (subpath \"%s\"))\n", path))
	}

	// System essentials for shell execution
	sb.WriteString("(allow file-read-metadata)\n")
	sb.WriteString("(allow sysctl-read)\n")
	sb.WriteString("(allow mach-lookup)\n")
	sb.WriteString("(allow signal)\n")
	sb.WriteString("(allow iokit-open)\n")

	// Allow pseudo-terminals for shell
	sb.WriteString("(allow file-read* file-write* (regex #\"^/dev/\"))\n")
	sb.WriteString("(allow file-ioctl (regex #\"^/dev/\"))\n")

	// Network access
	if s.config.AllowNetwork {
		sb.WriteString("(allow network*)\n")
	} else {
		sb.WriteString("(deny network*)\n")
	}

	return sb.String()
}

// isSandboxDenial checks if an error is due to sandbox policy denial.
func isSandboxDenial(output string, err error) bool {
	// sandbox-exec typically outputs "deny" messages or specific error codes
	lower := strings.ToLower(output)
	if strings.Contains(lower, "sandbox") && strings.Contains(lower, "deny") {
		return true
	}
	if strings.Contains(lower, "operation not permitted") {
		return true
	}
	// Check for sandbox-specific exit code patterns
	if exitErr, ok := err.(*exec.ExitError); ok {
		// Exit code 1 with specific patterns indicates sandbox denial
		if exitErr.ExitCode() == 1 && (strings.Contains(lower, "permission denied") ||
			strings.Contains(lower, "not permitted")) {
			return true
		}
	}
	return false
}

// extractSandboxReason attempts to extract a human-readable reason from sandbox denial output.
func extractSandboxReason(output string) string {
	lower := strings.ToLower(output)

	if strings.Contains(lower, "network") {
		return "network access denied"
	}
	if strings.Contains(lower, "write") || strings.Contains(lower, "permission denied") {
		return "write access denied"
	}
	if strings.Contains(lower, "exec") {
		return "execution denied"
	}
	if strings.Contains(lower, "read") {
		return "read access denied"
	}

	// Generic fallback
	if len(output) > 100 {
		return output[:100] + "..."
	}
	if output != "" {
		return output
	}
	return "sandbox policy violation"
}

// PassthroughExecutor executes commands directly without sandboxing.
type PassthroughExecutor struct{}

// Run executes a command directly (no sandbox).
func (p *PassthroughExecutor) Run(command string) (ToolResult, ExecMeta) {
	meta := ExecMeta{Sandboxed: false}

	start := time.Now()
	out, err := exec.Command("sh", "-c", command).CombinedOutput()
	elapsed := time.Since(start).Seconds()

	if err != nil {
		return ToolResult{
			Success:  false,
			Output:   string(out),
			Error:    err,
			Status:   fmt.Sprintf("fail (%.1fs)", elapsed),
			ExecMeta: &meta,
		}, meta
	}

	return ToolResult{
		Success:  true,
		Output:   string(out),
		Status:   fmt.Sprintf("ok (%.1fs)", elapsed),
		ExecMeta: &meta,
	}, meta
}

// Global executor instance, initialized by InitSandbox or on first use.
var defaultExecutor ShellExecutor

// InitSandbox initializes the sandbox executor with the given configuration.
// Should be called at startup before any shell commands are executed.
func InitSandbox(cfg SandboxConfig) {
	defaultExecutor = NewShellExecutor(cfg)
}

// GetExecutor returns the current shell executor, initializing with defaults if needed.
func GetExecutor() ShellExecutor {
	if defaultExecutor == nil {
		defaultExecutor = NewShellExecutor(DefaultSandboxConfig())
	}
	return defaultExecutor
}

// IsSandboxEnabled returns true if the current executor is sandboxed.
func IsSandboxEnabled() bool {
	exec := GetExecutor()
	_, isSandboxed := exec.(*SandboxedExecutor)
	return isSandboxed
}
