package core

import "testing"

func TestRegistryToolsReturnsInsertionOrder(t *testing.T) {
	r := NewRegistry()

	r.Register(&ToolDef{Name: "b", Description: "b"})
	r.Register(&ToolDef{Name: "a", Description: "a"})
	r.Register(&ToolDef{Name: "c", Description: "c"})

	tools := r.Tools()
	if len(tools) != 3 {
		t.Fatalf("Tools() length = %d, want 3", len(tools))
	}

	got := []string{tools[0].Function.Name, tools[1].Function.Name, tools[2].Function.Name}
	want := []string{"b", "a", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Tools()[%d] = %q, want %q (full order: %v)", i, got[i], want[i], got)
		}
	}
}

func TestRegistryRegisterReplaceKeepsOriginalOrder(t *testing.T) {
	r := NewRegistry()

	r.Register(&ToolDef{Name: "run_shell", Description: "first"})
	r.Register(&ToolDef{Name: "python_runtime", Description: "second"})
	r.Register(&ToolDef{Name: "run_shell", Description: "updated"})

	tools := r.Tools()
	if len(tools) != 2 {
		t.Fatalf("Tools() length = %d, want 2", len(tools))
	}

	if tools[0].Function.Name != "run_shell" || tools[1].Function.Name != "python_runtime" {
		t.Fatalf("order changed after replacement: got [%s, %s]", tools[0].Function.Name, tools[1].Function.Name)
	}

	if tools[0].Function.Description != "updated" {
		t.Fatalf("replaced tool description = %q, want %q", tools[0].Function.Description, "updated")
	}
}
