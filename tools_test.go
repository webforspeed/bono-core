package core

import (
	"strings"
	"testing"
	"time"
)

func TestExecuteShellWithSandboxFallbackOnSandboxError(t *testing.T) {
	if !sandboxAvailable() {
		t.Skip("sandbox-exec not available")
	}

	prev := defaultExecutor
	t.Cleanup(func() { defaultExecutor = prev })
	InitSandbox(DefaultSandboxConfig())

	fallbackCalls := 0
	result := ExecuteShellWithSandbox("touch /usr/local/bono_sandbox_fallback_test.txt", func(cmd, reason string) bool {
		fallbackCalls++
		if cmd == "" {
			t.Fatal("expected command in fallback callback")
		}
		if reason == "" {
			t.Fatal("expected reason in fallback callback")
		}
		return false
	})

	if fallbackCalls != 1 {
		t.Fatalf("expected fallback callback once, got %d", fallbackCalls)
	}
	if result.Success {
		t.Fatal("expected command to remain failed when fallback is rejected")
	}
	if result.ExecMeta == nil || !result.ExecMeta.Sandboxed {
		t.Fatal("expected original sandboxed result when fallback is rejected")
	}
}

func TestExecuteShellWithSandboxFallbackOnGenericSandboxFailure(t *testing.T) {
	if !sandboxAvailable() {
		t.Skip("sandbox-exec not available")
	}

	prev := defaultExecutor
	t.Cleanup(func() { defaultExecutor = prev })
	InitSandbox(DefaultSandboxConfig())

	fallbackCalls := 0
	result := ExecuteShellWithSandbox("false", func(cmd, reason string) bool {
		fallbackCalls++
		if cmd != "false" {
			t.Fatalf("unexpected fallback command: %q", cmd)
		}
		if reason == "" {
			t.Fatal("expected reason in fallback callback")
		}
		return true
	})

	if fallbackCalls != 1 {
		t.Fatalf("expected fallback callback once, got %d", fallbackCalls)
	}
	if result.Success {
		t.Fatal("expected false to still fail outside sandbox")
	}
	if result.ExecMeta == nil || result.ExecMeta.Sandboxed {
		t.Fatal("expected returned result from unsandboxed retry")
	}
}

func TestExecuteShellWithSandboxRespectsDisabledFallback(t *testing.T) {
	if !sandboxAvailable() {
		t.Skip("sandbox-exec not available")
	}

	prev := defaultExecutor
	t.Cleanup(func() { defaultExecutor = prev })
	cfg := DefaultSandboxConfig()
	cfg.FallbackOutsideSandbox = false
	InitSandbox(cfg)

	fallbackCalls := 0
	result := ExecuteShellWithSandbox("false", func(cmd, reason string) bool {
		fallbackCalls++
		return true
	})

	if fallbackCalls != 0 {
		t.Fatalf("expected fallback callback to be skipped, got %d call(s)", fallbackCalls)
	}
	if result.Success {
		t.Fatal("expected original sandboxed failure to be returned")
	}
	if result.ExecMeta == nil || !result.ExecMeta.Sandboxed {
		t.Fatal("expected sandboxed result when fallback is disabled")
	}
}

func TestExecuteShellWithSandboxFallbackOnTimeout(t *testing.T) {
	if !sandboxAvailable() {
		t.Skip("sandbox-exec not available")
	}

	prev := defaultExecutor
	t.Cleanup(func() { defaultExecutor = prev })
	cfg := DefaultSandboxConfig()
	cfg.CommandTimeout = 100 * time.Millisecond
	InitSandbox(cfg)

	fallbackCalls := 0
	result := ExecuteShellWithSandbox("sleep 1", func(cmd, reason string) bool {
		fallbackCalls++
		if cmd != "sleep 1" {
			t.Fatalf("unexpected fallback command: %q", cmd)
		}
		if !strings.Contains(reason, "timed out inside sandbox") {
			t.Fatalf("expected timeout reason, got %q", reason)
		}
		return false
	})

	if fallbackCalls != 1 {
		t.Fatalf("expected fallback callback once, got %d", fallbackCalls)
	}
	if result.Success {
		t.Fatal("expected timed out sandbox command to fail when fallback is rejected")
	}
	if result.Status == "" || !strings.Contains(result.Status, "timeout") {
		t.Fatalf("expected timeout status, got %q", result.Status)
	}
}
