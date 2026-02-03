package core

import (
	"os/exec"
	"strings"
	"testing"
)

func TestExecutePython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found in PATH")
	}

	result := ExecutePython("print(1+1)")

	if !result.Success {
		t.Fatalf("expected success, got: %v, output: %s", result.Error, result.Output)
	}

	if result.ExecMeta == nil {
		t.Fatal("expected ExecMeta to be set")
	}

	if !strings.Contains(result.Output, "2") {
		t.Fatalf("expected output to contain 2, got: %s", result.Output)
	}
}
