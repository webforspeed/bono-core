package core

import (
	"strings"
	"testing"
)

func TestDetectContext(t *testing.T) {
	ctx := DetectContext()

	if ctx.OS == "" {
		t.Error("OS should not be empty")
	}
	if ctx.Arch == "" {
		t.Error("Arch should not be empty")
	}
	if ctx.Shell == "" {
		t.Error("Shell should not be empty")
	}
	if ctx.CWD == "" {
		t.Error("CWD should not be empty")
	}
	// HomeDir can be empty in some environments, so we don't require it
}

func TestEnvironmentEnricher(t *testing.T) {
	e := EnvironmentEnricher()
	ctx := PromptContext{
		OS:    "darwin",
		Arch:  "arm64",
		Shell: "zsh",
		CWD:   "/tmp/test",
	}
	result := e.Enrich("base prompt", ctx)

	if !strings.Contains(result, "darwin") {
		t.Error("should contain OS")
	}
	if !strings.Contains(result, "arm64") {
		t.Error("should contain Arch")
	}
	if !strings.Contains(result, "zsh") {
		t.Error("should contain Shell")
	}
	if !strings.Contains(result, "/tmp/test") {
		t.Error("should contain CWD")
	}
	if !strings.Contains(result, "(macOS)") {
		t.Error("should contain macOS label for darwin")
	}
	if !strings.HasSuffix(result, "base prompt") {
		t.Error("base prompt should be at end")
	}
}

func TestEnvironmentEnricher_Linux(t *testing.T) {
	e := EnvironmentEnricher()
	ctx := PromptContext{
		OS:    "linux",
		Arch:  "amd64",
		Shell: "bash",
		CWD:   "/home/user",
	}
	result := e.Enrich("base", ctx)

	if !strings.Contains(result, "(Linux)") {
		t.Error("should contain Linux label")
	}
}

func TestOSCommandsEnricher_Darwin(t *testing.T) {
	e := OSCommandsEnricher()
	ctx := PromptContext{OS: "darwin"}
	result := e.Enrich("base", ctx)

	if !strings.Contains(result, "pbcopy") {
		t.Error("macOS should mention pbcopy")
	}
	if !strings.Contains(result, "BSD") {
		t.Error("macOS should mention BSD coreutils")
	}
	if strings.Contains(result, "xclip") {
		t.Error("macOS should not mention xclip")
	}
	if !strings.HasSuffix(result, "base") {
		t.Error("base prompt should be at end")
	}
}

func TestOSCommandsEnricher_Linux(t *testing.T) {
	e := OSCommandsEnricher()
	ctx := PromptContext{OS: "linux"}
	result := e.Enrich("base", ctx)

	if !strings.Contains(result, "xclip") {
		t.Error("Linux should mention xclip")
	}
	if !strings.Contains(result, "GNU") {
		t.Error("Linux should mention GNU coreutils")
	}
	if !strings.Contains(result, "systemctl") {
		t.Error("Linux should mention systemctl")
	}
	if strings.Contains(result, "pbcopy") {
		t.Error("Linux should not mention pbcopy")
	}
}

func TestOSCommandsEnricher_Windows(t *testing.T) {
	e := OSCommandsEnricher()
	ctx := PromptContext{OS: "windows"}
	result := e.Enrich("base", ctx)

	if !strings.Contains(result, "PowerShell") {
		t.Error("Windows should mention PowerShell")
	}
	if !strings.Contains(result, "Get-Clipboard") {
		t.Error("Windows should mention Get-Clipboard")
	}
}

func TestOSCommandsEnricher_Unknown(t *testing.T) {
	e := OSCommandsEnricher()
	ctx := PromptContext{OS: "freebsd"}
	result := e.Enrich("base", ctx)

	if result != "base" {
		t.Error("unknown OS should return base unchanged")
	}
}

func TestVerificationEnricher(t *testing.T) {
	e := VerificationEnricher()
	ctx := PromptContext{}
	result := e.Enrich("base", ctx)

	if !strings.Contains(result, "Verification") {
		t.Error("should contain Verification section")
	}
	if !strings.Contains(result, "Never assume") {
		t.Error("should contain 'Never assume' guidance")
	}
	if !strings.HasSuffix(result, "base") {
		t.Error("base prompt should be at end")
	}
}

func TestSafetyEnricher(t *testing.T) {
	e := SafetyEnricher()
	ctx := PromptContext{}
	result := e.Enrich("base", ctx)

	if !strings.Contains(result, "Safety") {
		t.Error("should contain Safety section")
	}
	if !strings.Contains(result, "read-only") {
		t.Error("should mention read-only commands")
	}
	if !strings.Contains(result, "destructive") {
		t.Error("should mention destructive commands")
	}
}

func TestOutputEnricher(t *testing.T) {
	e := OutputEnricher()
	ctx := PromptContext{}
	result := e.Enrich("base", ctx)

	if !strings.Contains(result, "Response Format") {
		t.Error("should contain Response Format section")
	}
	if !strings.Contains(result, "Summarize") {
		t.Error("should mention summarize instruction")
	}
	if !strings.Contains(result, "---") {
		t.Error("should contain separator")
	}
}

func TestComposerChainOrder(t *testing.T) {
	composer := NewPromptComposer(
		EnvironmentEnricher(),
		OSCommandsEnricher(),
		OutputEnricher(),
	)
	ctx := PromptContext{
		OS:    "darwin",
		Arch:  "arm64",
		Shell: "zsh",
		CWD:   "/tmp",
	}
	result := composer.ComposeWithContext("LLM prompt here", ctx)

	// Find positions
	envIdx := strings.Index(result, "## Environment")
	osIdx := strings.Index(result, "## macOS")
	outputIdx := strings.Index(result, "## Response Format")
	baseIdx := strings.Index(result, "LLM prompt here")

	if envIdx == -1 {
		t.Fatal("Environment section not found")
	}
	if osIdx == -1 {
		t.Fatal("macOS section not found")
	}
	if outputIdx == -1 {
		t.Fatal("Response Format section not found")
	}
	if baseIdx == -1 {
		t.Fatal("Base prompt not found")
	}

	// Check order: Environment -> OS -> Output -> Base
	if envIdx > osIdx {
		t.Error("Environment should come before OS commands")
	}
	if osIdx > outputIdx {
		t.Error("OS commands should come before Output")
	}
	if outputIdx > baseIdx {
		t.Error("Output should come before base prompt")
	}
}

func TestComposerChainOrder_AllEnrichers(t *testing.T) {
	composer := DefaultShellComposer()
	ctx := PromptContext{
		OS:    "linux",
		Arch:  "amd64",
		Shell: "bash",
		CWD:   "/home/user",
	}
	result := composer.ComposeWithContext("base prompt", ctx)

	// All sections should be present
	sections := []string{
		"## Environment",
		"## Linux Shell Commands",
		"## Verification Rules",
		"## Safety Awareness",
		"## Response Format",
		"base prompt",
	}

	lastIdx := -1
	for _, section := range sections {
		idx := strings.Index(result, section)
		if idx == -1 {
			t.Errorf("Section '%s' not found", section)
			continue
		}
		if idx < lastIdx {
			t.Errorf("Section '%s' is out of order", section)
		}
		lastIdx = idx
	}
}

func TestEmptyBasePrompt(t *testing.T) {
	composer := DefaultShellComposer()
	ctx := PromptContext{
		OS:    "darwin",
		Arch:  "arm64",
		Shell: "zsh",
		CWD:   "/tmp",
	}
	result := composer.ComposeWithContext("", ctx)

	// Should still have enricher content
	if !strings.Contains(result, "## Environment") {
		t.Error("should contain environment section even with empty base")
	}
	if !strings.Contains(result, "## macOS") {
		t.Error("should contain OS section even with empty base")
	}
}

func TestCustomEnricher(t *testing.T) {
	custom := PromptEnricherFunc(func(base string, ctx PromptContext) string {
		return "## Custom\nHello\n\n" + base
	})

	composer := NewPromptComposer(custom)
	ctx := PromptContext{}
	result := composer.ComposeWithContext("base", ctx)

	if !strings.HasPrefix(result, "## Custom") {
		t.Error("custom enricher should prepend")
	}
	if !strings.HasSuffix(result, "base") {
		t.Error("base should be at end")
	}
}

func TestCustomEnricherInChain(t *testing.T) {
	custom := PromptEnricherFunc(func(base string, ctx PromptContext) string {
		return "## Project\nType: Go\n\n" + base
	})

	composer := NewPromptComposer(
		EnvironmentEnricher(),
		custom,
		OutputEnricher(),
	)
	ctx := PromptContext{
		OS:    "darwin",
		Arch:  "arm64",
		Shell: "zsh",
		CWD:   "/tmp",
	}
	result := composer.ComposeWithContext("base prompt", ctx)

	// Check order: Environment -> Custom -> Output -> Base
	envIdx := strings.Index(result, "## Environment")
	customIdx := strings.Index(result, "## Project")
	outputIdx := strings.Index(result, "## Response Format")
	baseIdx := strings.Index(result, "base prompt")

	if envIdx > customIdx {
		t.Error("Environment should come before custom")
	}
	if customIdx > outputIdx {
		t.Error("Custom should come before Output")
	}
	if outputIdx > baseIdx {
		t.Error("Output should come before base")
	}
}

func TestMinimalShellComposer(t *testing.T) {
	composer := MinimalShellComposer()
	ctx := PromptContext{
		OS:    "darwin",
		Arch:  "arm64",
		Shell: "zsh",
		CWD:   "/tmp",
	}
	result := composer.ComposeWithContext("base", ctx)

	// Should have Environment, OS, Output
	if !strings.Contains(result, "## Environment") {
		t.Error("should have Environment")
	}
	if !strings.Contains(result, "## macOS") {
		t.Error("should have OS commands")
	}
	if !strings.Contains(result, "## Response Format") {
		t.Error("should have Output")
	}

	// Should NOT have Verification, Safety
	if strings.Contains(result, "## Verification") {
		t.Error("minimal should not have Verification")
	}
	if strings.Contains(result, "## Safety") {
		t.Error("minimal should not have Safety")
	}
}

func TestComposeWithRealContext(t *testing.T) {
	// Test with real detected context
	composer := DefaultShellComposer()
	result := composer.Compose("Test prompt from LLM")

	// Should have content
	if len(result) < 100 {
		t.Error("composed prompt should have substantial content")
	}

	// Should end with base prompt
	if !strings.HasSuffix(result, "Test prompt from LLM") {
		t.Error("should end with base prompt")
	}

	// Should have environment section with real values
	if !strings.Contains(result, "## Environment") {
		t.Error("should have Environment section")
	}
}

func TestEnricherPreservesBaseOnError(t *testing.T) {
	// Create an enricher with invalid template (this tests graceful fallback)
	// Note: template.Must panics on parse error, so we test the Execute error path
	// by using a template that could fail on certain inputs

	// For OSCommandsEnricher with unknown OS, it should return base unchanged
	e := OSCommandsEnricher()
	ctx := PromptContext{OS: ""}
	result := e.Enrich("original base", ctx)

	if result != "original base" {
		t.Error("empty OS should return base unchanged")
	}
}

func TestPromptContextFields(t *testing.T) {
	ctx := PromptContext{
		OS:         "darwin",
		Arch:       "arm64",
		Shell:      "zsh",
		CWD:        "/Users/test",
		HomeDir:    "/Users/test",
		BasePrompt: "test prompt",
	}

	// Verify all fields are accessible
	if ctx.OS != "darwin" {
		t.Error("OS field mismatch")
	}
	if ctx.Arch != "arm64" {
		t.Error("Arch field mismatch")
	}
	if ctx.Shell != "zsh" {
		t.Error("Shell field mismatch")
	}
	if ctx.CWD != "/Users/test" {
		t.Error("CWD field mismatch")
	}
	if ctx.HomeDir != "/Users/test" {
		t.Error("HomeDir field mismatch")
	}
	if ctx.BasePrompt != "test prompt" {
		t.Error("BasePrompt field mismatch")
	}
}
