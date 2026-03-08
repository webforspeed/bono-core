package core

import "embed"

//go:embed subagent_prompts/plan/*.tmpl
var planPromptFS embed.FS

const planPromptVersion = "v1.0.0"

// planAgent implements SubAgent for planning mode.
type planAgent struct {
	prompt string
}

func newPlanAgent() *planAgent {
	content, err := planPromptFS.ReadFile("subagent_prompts/plan/" + planPromptVersion + ".tmpl")
	if err != nil {
		panic("plan: missing prompt " + planPromptVersion + ": " + err.Error())
	}
	return &planAgent{prompt: string(content)}
}

func (a *planAgent) Name() string          { return "plan" }
func (a *planAgent) AllowedTools() []string { return []string{"read_file", "run_shell", "code_search"} }
func (a *planAgent) SystemPrompt() string   { return a.prompt }

var _ SubAgent = (*planAgent)(nil)

// registerBuiltinSubAgents registers all built-in subagents.
// Called from NewAgent so every consumer gets them automatically.
func (a *Agent) registerBuiltinSubAgents() {
	a.RegisterSubAgent(newPlanAgent())
}
