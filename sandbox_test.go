package core

import (
	"os"
	"strings"
	"testing"
)

func TestSandboxAvailable(t *testing.T) {
	available := sandboxAvailable()
	t.Logf("sandbox-exec available: %v", available)
	// On macOS, this should be true
	if available {
		t.Log("Running on macOS with sandbox-exec available")
	} else {
		t.Log("sandbox-exec not available (non-macOS or missing)")
	}
}

func TestDefaultSandboxConfig(t *testing.T) {
	cfg := DefaultSandboxConfig()

	if !cfg.FallbackOutsideSandbox {
		t.Error("FallbackOutsideSandbox should default to true")
	}

	if cfg.AllowNetwork {
		t.Error("AllowNetwork should default to false")
	}

	if len(cfg.ReadPaths) == 0 {
		t.Error("ReadPaths should have default values")
	}

	if len(cfg.WritePaths) == 0 {
		t.Error("WritePaths should have default values")
	}

	if len(cfg.ExecPaths) == 0 {
		t.Error("ExecPaths should have default values")
	}

	t.Logf("Default config: Enabled=%v, ReadPaths=%v, WritePaths=%v, ExecPaths=%v",
		cfg.Enabled, cfg.ReadPaths, cfg.WritePaths, cfg.ExecPaths)
}

func TestSandboxedExecutorSimpleCommand(t *testing.T) {
	if !sandboxAvailable() {
		t.Skip("sandbox-exec not available")
	}

	cfg := DefaultSandboxConfig()
	exec := NewShellExecutor(cfg)

	// Test simple read-only command
	result, meta := exec.Run("echo 'hello sandbox'")

	if !result.Success {
		t.Errorf("Expected success, got: %v, output: %s", result.Error, result.Output)
	}

	if !meta.Sandboxed {
		t.Error("Expected meta.Sandboxed to be true")
	}

	if !strings.Contains(result.Output, "hello sandbox") {
		t.Errorf("Expected output to contain 'hello sandbox', got: %s", result.Output)
	}

	t.Logf("Sandboxed command output: %s", result.Output)
}

func TestSandboxedExecutorWriteInCwd(t *testing.T) {
	if !sandboxAvailable() {
		t.Skip("sandbox-exec not available")
	}

	cfg := DefaultSandboxConfig()
	exec := NewShellExecutor(cfg)

	// Create a temp file in cwd (should be allowed)
	testFile := "sandbox_test_temp.txt"
	defer os.Remove(testFile)

	result, meta := exec.Run("echo 'test content' > " + testFile)

	if !result.Success {
		t.Logf("Write in cwd result: success=%v, error=%v, output=%s", result.Success, result.Error, result.Output)
		// Note: sandbox may still block this depending on exact config
	}

	t.Logf("Write in cwd: Sandboxed=%v, SandboxError=%v, Reason=%s",
		meta.Sandboxed, meta.SandboxError, meta.SandboxReason)
}

func TestSandboxedExecutorWriteOutsideCwd(t *testing.T) {
	if !sandboxAvailable() {
		t.Skip("sandbox-exec not available")
	}

	cfg := DefaultSandboxConfig()
	// Remove all write paths except temp to test blocking
	cfg.WritePaths = []string{"/tmp"}
	exec := &SandboxedExecutor{config: cfg}

	// Try to write outside allowed paths (should be blocked)
	result, meta := exec.Run("touch /usr/local/sandbox_test_blocked.txt 2>&1")

	t.Logf("Write outside cwd: Success=%v, Sandboxed=%v, SandboxError=%v, Reason=%s, Output=%s",
		result.Success, meta.Sandboxed, meta.SandboxError, meta.SandboxReason, result.Output)

	// We expect this to either fail or be blocked by sandbox
	if result.Success {
		t.Log("Command succeeded (might have write access to /usr/local)")
		os.Remove("/usr/local/sandbox_test_blocked.txt")
	}
}

func TestPassthroughExecutor(t *testing.T) {
	exec := &PassthroughExecutor{}

	result, meta := exec.Run("echo 'passthrough test'")

	if !result.Success {
		t.Errorf("Expected success, got: %v", result.Error)
	}

	if meta.Sandboxed {
		t.Error("Expected meta.Sandboxed to be false for passthrough")
	}

	if !strings.Contains(result.Output, "passthrough test") {
		t.Errorf("Expected output to contain 'passthrough test', got: %s", result.Output)
	}
}

func TestIsSandboxEnabled(t *testing.T) {
	// Initialize with defaults
	InitSandbox(DefaultSandboxConfig())

	enabled := IsSandboxEnabled()
	t.Logf("IsSandboxEnabled: %v", enabled)

	if sandboxAvailable() && !enabled {
		t.Error("Expected sandbox to be enabled when sandbox-exec is available")
	}
}

func TestExecMeta(t *testing.T) {
	meta := ExecMeta{
		Sandboxed:     true,
		SandboxError:  false,
		SandboxReason: "",
	}

	if !meta.Sandboxed {
		t.Error("Expected Sandboxed to be true")
	}

	if meta.SandboxError {
		t.Error("Expected SandboxError to be false")
	}
}
