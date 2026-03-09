package core

import (
	"embed"
	"fmt"
)

//go:embed subagent_prompts/plan/*.tmpl
var planPromptFS embed.FS

const planPromptVersion = "v1.1.0"

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
func (a *planAgent) AllowedTools() []string {
	return []string{"read_file", "run_shell", "code_search", "WebFetch", "WebSearch", "compact_context", "python_runtime"}
}
func (a *planAgent) SystemPrompt() string { return a.prompt }

func (a *planAgent) FormatUserPrompt(input string) string {
	return fmt.Sprintf(`Here is the project you need to create planning documentation for:

<project_description>
%s
</project_description>

Now write your complete planning document inside <planning_document> tags. The document should be in markdown format and follow all the requirements specified above. Ensure that Phase 1 tasks are actionable and detailed, while Future Phases provide context for the project's evolution without requiring immediate implementation.

<planning_document>
[Write your complete markdown planning document here]
</planning_document>`, input)
}

var _ SubAgent = (*planAgent)(nil)
var _ UserPromptFormatter = (*planAgent)(nil)

// registerBuiltinSubAgents registers all built-in subagents.
// Called from NewAgent so every consumer gets them automatically.
func (a *Agent) registerBuiltinSubAgents() {
	a.RegisterSubAgent(newPlanAgent(),
		PersistHook("~/.bono/{cwd}/plans"),
		ApprovalHook(func() func(SubAgentResult) SubAgentApprovalResponse {
			return a.OnSubAgentApproval
		}),
	)
}
