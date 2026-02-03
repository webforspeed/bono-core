package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"text/template"
	"time"
)

// ============================================================================
// TEMPLATES - Edit these to improve shell subagent behavior
// ============================================================================

const environmentTemplate = `## Environment
- OS: {{.OS}}{{if eq .OS "darwin"}} (macOS){{else if eq .OS "linux"}} (Linux){{end}}
- Architecture: {{.Arch}}
- Shell: {{.Shell}}
- Working Directory: {{.CWD}}

`

const osCommandsTemplateDarwin = `## macOS Shell Commands
- BSD coreutils (NOT GNU): ls, grep, sed, awk have BSD flags
- Clipboard: pbcopy / pbpaste
- Open files/URLs: open
- Homebrew paths: /opt/homebrew/bin (Apple Silicon), /usr/local/bin (Intel)
- Services: launchctl (not systemd)
- Prevent sleep: caffeinate -i <command>
- Find files: mdfind (Spotlight) or find
- Network: networksetup, scutil
- Disk: diskutil, hdiutil

`

const osCommandsTemplateLinux = `## Linux Shell Commands
- GNU coreutils: ls, grep, sed, awk with GNU flags
- Clipboard: xclip -selection clipboard or xsel --clipboard
- Open files/URLs: xdg-open
- Services: systemctl (systemd) or service
- Package managers: apt, dnf, pacman, zypper (varies by distro)
- Find files: locate or find
- Network: ip, nmcli, ss
- Disk: lsblk, fdisk, mount

`

const osCommandsTemplateWindows = `## Windows Shell Commands
- Use PowerShell cmdlets over cmd.exe
- Clipboard: Get-Clipboard / Set-Clipboard
- Open files: Invoke-Item or Start-Process
- Services: Get-Service, Start-Service, Stop-Service
- Paths: use backslash or escape forward slash
- Environment: $env:VARNAME
- Find files: Get-ChildItem -Recurse

`

const verificationTemplate = `## Verification Rules
CRITICAL: Never assume a command succeeded. Always verify.

After mutations, verify with read-only commands:
- File created → ls -la, stat, wc -l, head
- File modified → git diff, tail, cat
- Directory created → ls -la, tree
- Process started → ps aux | grep, pgrep
- Package installed → which, command -v, <pkg> --version
- Config changed → cat, grep for expected value
- Permission changed → ls -la, stat

If verification fails or is ambiguous, report the issue.

`

const safetyTemplate = `## Safety Awareness
Classify your commands mentally:
- read-only: ls, cat, grep, find, head, tail, which, echo, pwd
- modify: touch, mkdir, cp, mv, chmod, chown, git add/commit
- destructive: rm, rm -rf, truncate, dd, format
- network: curl, wget, ssh, scp, git push/pull
- privileged: sudo, su, chown root

For destructive commands: double-check paths, consider backup first.

`

const outputTemplate = `## Response Format
- Summarize findings in 1-2 sentences
- Include: file exists/missing, counts, sizes, errors
- When done, just respond with your summary

---

`

// ============================================================================
// PROMPT CONTEXT
// ============================================================================

// PromptContext holds runtime information for template rendering.
type PromptContext struct {
	OS         string // runtime.GOOS: darwin, linux, windows
	Arch       string // runtime.GOARCH: amd64, arm64
	Shell      string // $SHELL or default
	CWD        string // current working directory
	HomeDir    string // user home directory
	BasePrompt string // the original LLM-provided prompt (set by composer)
}

// DetectContext gathers runtime environment info.
func DetectContext() PromptContext {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	shell := filepath.Base(os.Getenv("SHELL"))
	if shell == "" || shell == "." {
		shell = "sh"
	}
	return PromptContext{
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Shell:   shell,
		CWD:     cwd,
		HomeDir: home,
	}
}

// ============================================================================
// ENRICHER INTERFACE
// ============================================================================

// PromptEnricher wraps or prepends context to a base prompt.
type PromptEnricher interface {
	Enrich(base string, ctx PromptContext) string
}

// PromptEnricherFunc adapts a function to PromptEnricher interface.
type PromptEnricherFunc func(base string, ctx PromptContext) string

// Enrich implements PromptEnricher.
func (f PromptEnricherFunc) Enrich(base string, ctx PromptContext) string {
	return f(base, ctx)
}

// ============================================================================
// PROMPT COMPOSER
// ============================================================================

// PromptComposer chains multiple enrichers to build a complete prompt.
type PromptComposer struct {
	enrichers []PromptEnricher
}

// NewPromptComposer creates a composer with the given enrichers.
// Enrichers are applied in reverse order so that the first enricher's
// output appears first in the final prompt.
func NewPromptComposer(enrichers ...PromptEnricher) *PromptComposer {
	return &PromptComposer{enrichers: enrichers}
}

// Compose applies all enrichers to the base prompt.
// The base prompt (LLM's system_prompt) ends up at the end.
func (c *PromptComposer) Compose(base string) string {
	ctx := DetectContext()
	result := base
	// Iterate in reverse so first-defined enricher appears first in output
	for i := len(c.enrichers) - 1; i >= 0; i-- {
		result = c.enrichers[i].Enrich(result, ctx)
	}
	return result
}

// ComposeWithContext applies all enrichers using a provided context.
// Useful for testing with mocked context.
func (c *PromptComposer) ComposeWithContext(base string, ctx PromptContext) string {
	result := base
	// Iterate in reverse so first-defined enricher appears first in output
	for i := len(c.enrichers) - 1; i >= 0; i-- {
		result = c.enrichers[i].Enrich(result, ctx)
	}
	return result
}

// ============================================================================
// BUILT-IN ENRICHERS
// ============================================================================

// templateEnricher creates an enricher that prepends rendered template.
func templateEnricher(tmplStr string) PromptEnricher {
	tmpl := template.Must(template.New("").Parse(tmplStr))
	return PromptEnricherFunc(func(base string, ctx PromptContext) string {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, ctx); err != nil {
			return base // fallback to original on error
		}
		return buf.String() + base
	})
}

// EnvironmentEnricher adds OS/Arch/Shell/CWD context.
func EnvironmentEnricher() PromptEnricher {
	return templateEnricher(environmentTemplate)
}

// OSCommandsEnricher adds OS-specific command guidance.
func OSCommandsEnricher() PromptEnricher {
	return PromptEnricherFunc(func(base string, ctx PromptContext) string {
		var tmplStr string
		switch ctx.OS {
		case "darwin":
			tmplStr = osCommandsTemplateDarwin
		case "linux":
			tmplStr = osCommandsTemplateLinux
		case "windows":
			tmplStr = osCommandsTemplateWindows
		default:
			return base // unknown OS, return unchanged
		}
		tmpl, err := template.New("").Parse(tmplStr)
		if err != nil {
			return base
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, ctx); err != nil {
			return base
		}
		return buf.String() + base
	})
}

// VerificationEnricher adds "never assume success" patterns.
func VerificationEnricher() PromptEnricher {
	return templateEnricher(verificationTemplate)
}

// SafetyEnricher adds safety classification awareness.
func SafetyEnricher() PromptEnricher {
	return templateEnricher(safetyTemplate)
}

// OutputEnricher adds response format rules.
func OutputEnricher() PromptEnricher {
	return templateEnricher(outputTemplate)
}

// LoggingEnricher appends the final prompt to a JSONL file (passthrough).
func LoggingEnricher(logPath string) PromptEnricher {
	return PromptEnricherFunc(func(base string, ctx PromptContext) string {
		// Create logs dir if needed
		dir := filepath.Dir(logPath)
		os.MkdirAll(dir, 0755)

		// Append JSONL record
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			defer f.Close()
			record := map[string]any{
				"ts":     time.Now().UTC().Format(time.RFC3339),
				"prompt": base,
			}
			if data, err := json.Marshal(record); err == nil {
				f.Write(data)
				f.WriteString("\n")
			}
		}
		return base // passthrough
	})
}

// ============================================================================
// DEFAULT COMPOSERS
// ============================================================================

// DefaultShellComposer returns the standard shell prompt composer.
// Used automatically by ExecuteShellSubagent.
func DefaultShellComposer() *PromptComposer {
	return NewPromptComposer(
		LoggingEnricher("logs/shell_prompts.jsonl"), // First = runs last, logs final prompt
		EnvironmentEnricher(),                       // OS: darwin, Arch: arm64...
		OSCommandsEnricher(),                        // macOS: use pbcopy, BSD sed...
		VerificationEnricher(),                      // Verify after every mutation
		SafetyEnricher(),                            // Classify command safety
		OutputEnricher(),                            // Response format rules
	)
}

// MinimalShellComposer returns a lighter composer (just OS context).
// Use when verification/safety sections are not needed.
func MinimalShellComposer() *PromptComposer {
	return NewPromptComposer(
		EnvironmentEnricher(),
		OSCommandsEnricher(),
		OutputEnricher(),
	)
}
