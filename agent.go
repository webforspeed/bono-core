package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
)

const (
	maxChatTurns       = 100 // allows for summary rounds that compress context
	maxPreTaskTurns    = 100
	maxSubAgentTurns   = 100
)

// Agent orchestrates conversations with an LLM and executes tools.
type Agent struct {
	config        Config
	client        *Client
	registry      *Registry
	apiTools      []Tool // resolved from registry, filtered by AllowedTools
	msgs          []Message
	preTasksDone  bool // tracks if pre-tasks have executed
	codeSearch    *CodeSearchService
	codeSearchErr error
	web           *WebService
	webErr        error
	subAgents              map[string]subAgentEntry
	subAgentLastOutputPath map[string]string // tracks output_path across revision cycles

	// Optional hooks - nil means default behavior (auto-execute, no output)

	// OnToolCall is called before executing a tool.
	// Return false to skip tool execution (sends "cancelled by user" as result).
	OnToolCall func(name string, args map[string]any) bool

	// OnToolDone is called after a tool executes.
	// Receives the tool name, args (for display), and result.
	OnToolDone func(name string, args map[string]any, result ToolResult)

	// OnMessage is called when the assistant responds with text content.
	OnMessage func(content string)

	// OnPreTaskStart is called when a pre-task begins.
	OnPreTaskStart func(name string)

	// OnPreTaskEnd is called when a pre-task completes.
	OnPreTaskEnd func(name string)

	// OnSubAgentStart is called when a subagent begins execution.
	OnSubAgentStart func(name string)

	// OnSubAgentEnd is called when a subagent completes execution.
	OnSubAgentEnd func(name string)

	// OnSandboxFallback is called when sandbox blocks a command.
	// Return true to execute outside sandbox (requires approval), false to cancel.
	OnSandboxFallback func(command string, reason string) bool

	// OnContextUsage is called after each LLM response with the prompt usage percentage and cumulative cost.
	OnContextUsage func(pct float64, totalCost float64)

	// OnResponseModel is called after each LLM response with the model identifier actually used.
	OnResponseModel func(model string)

	// OnContentDelta is called for each text content fragment during streaming.
	// If set, enables streaming mode. If nil, responses are buffered (non-streaming).
	OnContentDelta func(delta string)

	// OnReasoningDelta is called for each reasoning text fragment during streaming.
	OnReasoningDelta func(delta string)

	// OnSubAgentApproval is called after subagent hooks complete when an
	// ApprovalHook is registered. Blocks until the user responds.
	// If nil, ApprovalHook auto-approves.
	OnSubAgentApproval func(result SubAgentResult) SubAgentApprovalResponse
}

// NewAgent creates an agent. Set hooks after creation to customize behavior.
func NewAgent(config Config) (*Agent, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	client, err := NewClient(config)
	if err != nil {
		return nil, err
	}

	// Initialize sandbox with config or defaults
	sandboxCfg := config.Sandbox
	if sandboxCfg.ReadPaths == nil && sandboxCfg.WritePaths == nil && sandboxCfg.ExecPaths == nil {
		// No sandbox config provided, use defaults
		sandboxCfg = DefaultSandboxConfig()
	}
	InitSandbox(sandboxCfg)

	a := &Agent{config: config, client: client, subAgents: make(map[string]subAgentEntry)}

	// Build tool registry — all tools get the same treatment.
	// Dependencies are injected as closures that capture the agent pointer.
	// Hooks (OnSandboxFallback etc.) are set by the caller after NewAgent returns;
	// closures evaluate them at call time, so they pick up the final values.
	shellPolicy := config.ShellPolicy
	shellExec := func(req ShellRequest) ToolResult {
		return ExecuteShellRequest(req, shellPolicy, a.OnSandboxFallback)
	}
	a.registry = NewRegistry()
	a.registry.Register(ReadFileTool())
	a.registry.Register(WriteFileTool())
	a.registry.Register(EditFileTool())
	a.registry.Register(RunShellTool(shellExec))
	a.registry.Register(PythonRuntimeTool(shellExec))
	a.registry.Register(CompactContextTool(a.compactMessages))

	// Register enter_plan_mode tool with injected subagent runner.
	// The closure captures the agent pointer and evaluates hooks at call time.
	a.registry.Register(EnterPlanModeTool(func(ctx context.Context, projectDesc string) (*SubAgentResult, error) {
		sa, ok := a.SubAgent("plan")
		if !ok {
			return nil, fmt.Errorf("plan subagent not found")
		}
		return a.RunSubAgent(ctx, sa, projectDesc)
	}))

	if config.CodeSearch != nil {
		serviceCfg := *config.CodeSearch
		serviceCfg.APIKey = config.APIKey
		if serviceCfg.BaseURL == "" {
			serviceCfg.BaseURL = config.BaseURL
		}
		service, err := NewCodeSearchService(serviceCfg)
		if err != nil {
			a.codeSearchErr = err
		} else {
			a.codeSearch = service
			a.registry.Register(service.Tool())
		}
	}
	if config.Web != nil {
		webCfg := *config.Web
		if webCfg.APIKey == "" {
			webCfg.APIKey = config.APIKey
		}
		if webCfg.BaseURL == "" {
			webCfg.BaseURL = config.BaseURL
		}
		if webCfg.SearchModel == "" {
			webCfg.SearchModel = config.Model
		}
		if webCfg.APILogPath == "" {
			webCfg.APILogPath = config.APILogPath
		}
		webSvc, err := NewWebService(webCfg)
		if err != nil {
			a.webErr = err
		} else {
			a.web = webSvc
			for _, t := range webSvc.Tools() {
				a.registry.Register(t)
			}
		}
	}

	a.apiTools = a.registry.Tools(config.AllowedTools...)

	if config.SystemPrompt != "" {
		a.msgs = append(a.msgs, Message{Role: "system", Content: config.SystemPrompt})
	}

	// Register default middleware.
	client.Use(ContextUsageMiddleware(client.LastUsage))

	// Register built-in subagents.
	a.registerBuiltinSubAgents()

	return a, nil
}

// Close releases resources owned by the agent.
func (a *Agent) Close() error {
	if a == nil || a.codeSearch == nil {
		return nil
	}
	return a.codeSearch.Close()
}

// CodeSearchService returns the initialized code-search service, if available.
func (a *Agent) CodeSearchService() *CodeSearchService {
	if a == nil {
		return nil
	}
	return a.codeSearch
}

// CodeSearchInitError returns initialization failure when code search was configured but unavailable.
func (a *Agent) CodeSearchInitError() error {
	if a == nil {
		return nil
	}
	return a.codeSearchErr
}

// WebService returns the initialized web service, if available.
func (a *Agent) WebService() *WebService {
	if a == nil {
		return nil
	}
	return a.web
}

// WebInitError returns initialization failure when web tools were configured but unavailable.
func (a *Agent) WebInitError() error {
	if a == nil {
		return nil
	}
	return a.webErr
}

// RegisterSubAgent registers a subagent with optional hooks for later lookup and execution.
// Hooks execute in order after the subagent completes.
func (a *Agent) RegisterSubAgent(sa SubAgent, hooks ...SubAgentHook) {
	a.subAgents[sa.Name()] = subAgentEntry{agent: sa, hooks: hooks}
}

// SubAgent returns a registered subagent by name.
func (a *Agent) SubAgent(name string) (SubAgent, bool) {
	entry, ok := a.subAgents[name]
	return entry.agent, ok
}

// subAgentHooks returns the hooks for a registered subagent.
func (a *Agent) subAgentHooks(name string) []SubAgentHook {
	return a.subAgents[name].hooks
}

// RegisterTool adds a tool to the agent's registry after creation.
// Use this for optional tools (like code_search) that aren't part of the default set.
func (a *Agent) RegisterTool(t *ToolDef) {
	a.registry.Register(t)
	a.apiTools = a.registry.Tools(a.config.AllowedTools...)
}

// Use registers message middleware that runs before each API call.
// Use this to inject metadata, transform messages, or add instrumentation.
func (a *Agent) Use(mw ...MessageMiddleware) {
	a.client.Use(mw...)
}

// Chat sends a user message and processes the complete turn.
// Blocks until the assistant provides a final text response.
// Tool calls are executed automatically unless OnToolCall returns false.
// On the first call, pre-tasks are automatically executed if configured.
func (a *Agent) Chat(ctx context.Context, input string) (string, error) {
	// Auto-run pre-tasks on first call
	if !a.preTasksDone && len(a.config.PreTasks) > 0 {
		if err := a.runPreTasks(ctx); err != nil {
			return "", err
		}
		a.preTasksDone = true
	}

	a.msgs = append(a.msgs, Message{Role: "user", Content: input})

	toolCallsSinceLastSummary := 0
	for turns := 0; ; turns++ {
		if turns >= maxChatTurns {
			return "", ErrMaxTurnsExceeded
		}
		msg, err := a.chatCompletion(ctx)
		if err != nil {
			return "", err
		}

		a.fireResponseModel()
		a.fireContextUsage()

		content := messageContent(msg)

		// No tool calls = done (unless empty, then nudge to continue)
		if len(msg.ToolCalls) == 0 {
			if content == "" {
				// Model returned empty (e.g. after summary round). Nudge it to continue the task.
				if last := lastAssistantContent(a.msgs); last != "" {
					// Avoid infinite loop: if we already nudged and got empty again, return last content
					const emptyNudge = "Your previous response was empty. Please continue: use tools to complete the task or provide your final response. Do not reply with empty content."
					if !lastMessageIs(a.msgs, "user", emptyNudge) {
						a.msgs = append(a.msgs, Message{Role: "user", Content: emptyNudge})
						continue // retry in next iteration
					}
					return last, nil
				}
				a.msgs = append(a.msgs, *msg)
				return "", ErrEmptyResponse
			}
			a.msgs = append(a.msgs, *msg)
			if a.OnMessage != nil && strings.TrimSpace(content) != "" {
				a.OnMessage(content)
			}
			return content, nil
		}

		// Cap: run at most remaining (cumulative across responses) until we hit MaxToolCallsPerTurn
		toRun := msg.ToolCalls
		if a.config.MaxToolCallsPerTurn > 0 {
			remaining := a.config.MaxToolCallsPerTurn - toolCallsSinceLastSummary
			if remaining <= 0 {
				remaining = a.config.MaxToolCallsPerTurn // fresh after summary
			}
			if len(toRun) > remaining {
				toRun = toRun[:remaining]
			}
			toolCallsSinceLastSummary += len(toRun)
		}
		hitCap := a.config.MaxToolCallsPerTurn > 0 && toolCallsSinceLastSummary >= a.config.MaxToolCallsPerTurn

		// Execute tools and collect results
		var toolResults []Message
		cancelled := false

		for i, tc := range toRun {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)

			// Check if tool should execute
			if a.OnToolCall != nil && !a.OnToolCall(tc.Function.Name, args) {
				cancelled = true
				break
			}

			var result ToolResult
			tool, ok := a.registry.Get(tc.Function.Name)
			if !ok {
				result = ToolResult{Success: false, Error: fmt.Errorf("unknown tool: %s", tc.Function.Name), Status: "fail: unknown tool"}
			} else {
				result = tool.Execute(args)
			}

			if a.OnToolDone != nil {
				a.OnToolDone(tc.Function.Name, args, result)
			}

			out := result.Output
			// Gentle nudge on last result when we hit the cap (like read_file truncation message)
			if hitCap && i == len(toRun)-1 {
				out = out + fmt.Sprintf("\n\n[You've used %d tool calls this round. Consider summarizing what you've learned before making more tool calls to save context.]", a.config.MaxToolCallsPerTurn)
			}
			toolResults = append(toolResults, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    out,
			})
		}

		// If cancelled, we never appended the assistant message; nothing to rollback
		if cancelled {
			return "", nil
		}

		// Append assistant message (trimmed to toRun so tool_calls and results match)
		assistantMsg := *msg
		if len(toRun) < len(msg.ToolCalls) {
			assistantMsg.ToolCalls = append([]ToolCall(nil), toRun...)
		}
		a.msgs = append(a.msgs, assistantMsg)
		a.msgs = append(a.msgs, toolResults...)

		// Summary round when we hit the cap: ask for summary, then compact and continue loop
		if hitCap {
			toolCallsSinceLastSummary = 0
			prompt := fmt.Sprintf("You've used your tool call limit for this round (%d). Please briefly summarize what you learned from the results above (1–2 paragraphs). After your summary we'll continue and you can use more tools if needed. Reply with your summary only (no tool calls).", a.config.MaxToolCallsPerTurn)
			a.msgs = append(a.msgs, Message{Role: "user", Content: prompt})
			summaryMsg, err := a.chatCompletion(ctx)
			if err != nil {
				return "", err
			}
			a.fireResponseModel()
			a.fireContextUsage()
			a.msgs = append(a.msgs, *summaryMsg)
			if summaryContent := messageContent(summaryMsg); a.OnMessage != nil && strings.TrimSpace(summaryContent) != "" {
				a.OnMessage(summaryContent)
			}
			// Compact: replace raw tool calls and results with the summary to save context
			// Keep [system, user(task)] + [assistant(summary)]; drop everything in between
			prefixLen := 2 // system + initial user
			if len(a.msgs) < prefixLen {
				prefixLen = len(a.msgs)
			}
			summary := a.msgs[len(a.msgs)-1]
			a.msgs = append(append([]Message{}, a.msgs[:prefixLen]...), summary)
			// Continue loop; next turn can do more tool calls or return final answer
		}
	}
}

// chatCompletion dispatches to streaming or non-streaming based on whether delta hooks are set.
func (a *Agent) chatCompletion(ctx context.Context) (*Message, error) {
	var opts []llmRequestOption
	if a.config.ReasoningEffort != "" {
		opts = append(opts, withReasoningEffort(a.config.ReasoningEffort))
	}

	if a.OnContentDelta != nil {
		return a.client.ChatCompletionWithToolsStream(
			ctx, a.msgs, a.apiTools,
			a.OnContentDelta, a.OnReasoningDelta,
			opts...,
		)
	}
	return a.client.ChatCompletionWithTools(ctx, a.msgs, a.apiTools, opts...)
}

// Messages returns the conversation history.
func (a *Agent) Messages() []Message {
	return a.msgs
}

// APIKey returns the configured API key (may be empty for local-only usage).
func (a *Agent) APIKey() string {
	return a.config.APIKey
}

// ModelName returns the current model identifier.
func (a *Agent) ModelName() string {
	return a.config.Model
}

// SetModel switches the model used for subsequent API calls.
func (a *Agent) SetModel(model string) {
	a.config.Model = model
	a.client.config.Model = model
}

// SetBaseURL updates the base URL for API calls.
// Empty resets to DefaultBaseURL.
func (a *Agent) SetBaseURL(baseURL string) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	a.config.BaseURL = baseURL
	a.client.SetBaseURL(baseURL)
}

// SetReasoningEffort sets the reasoning effort level for subsequent API calls.
// Valid values: "xhigh", "high", "medium", "low", "minimal", "none", "" (disabled).
func (a *Agent) SetReasoningEffort(effort string) {
	a.config.ReasoningEffort = effort
}

// ReasoningEffort returns the current reasoning effort level.
func (a *Agent) ReasoningEffort() string {
	return a.config.ReasoningEffort
}

// WarmModelUsageLimits preloads endpoint token limits for a model into the client cache.
// Useful when switching models at runtime so response_usage calculations stay accurate.
func (a *Agent) WarmModelUsageLimits(ctx context.Context, model string) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("agent client not initialized")
	}
	return a.client.WarmModelUsageLimits(ctx, model)
}

// Reset clears conversation history (keeps system prompt if configured).
func (a *Agent) Reset() {
	a.msgs = nil
	if a.config.SystemPrompt != "" {
		a.msgs = append(a.msgs, Message{Role: "system", Content: a.config.SystemPrompt})
	}
}

// ResetCost zeroes cumulative session cost and clears usage tracking.
func (a *Agent) ResetCost() {
	a.client.ResetCost()
}

// compactMessages replaces conversation history with a summary.
// Keeps the system prompt and inserts the summary as a user message.
// The caller (tool loop) appends the current turn's assistant+tool messages after.
func (a *Agent) compactMessages(summary string) ToolResult {
	if strings.TrimSpace(summary) == "" {
		return ToolResult{
			Success: false,
			Output:  "compact_context requires a non-empty summary",
			Status:  "fail: empty summary",
			Error:   fmt.Errorf("compact_context: empty summary"),
		}
	}

	// Count messages being replaced (everything after system prompt)
	replaced := len(a.msgs)
	if replaced <= 1 {
		return ToolResult{
			Success: true,
			Output:  "Nothing to compact — conversation just started.",
			Status:  "skipped: nothing to compact",
		}
	}

	// Rebuild: keep system prompt, replace everything else with summary
	var newMsgs []Message
	if len(a.msgs) > 0 && a.msgs[0].Role == "system" {
		newMsgs = append(newMsgs, a.msgs[0])
		replaced-- // don't count system prompt
	}
	newMsgs = append(newMsgs, Message{
		Role:    "user",
		Content: "<conversation_summary>\n" + summary + "\n</conversation_summary>",
	})
	a.msgs = newMsgs

	return ToolResult{
		Success: true,
		Output:  fmt.Sprintf("Context compacted: %d messages replaced with summary.", replaced),
		Status:  fmt.Sprintf("compacted %d messages", replaced),
	}
}

// RunPreTask executes a single pre-task immediately without affecting pre-task auto-run state.
func (a *Agent) RunPreTask(ctx context.Context, task PreTaskConfig) error {
	return a.runPreTask(ctx, task)
}

// runPreTasks executes all configured pre-tasks before the main agent loop.
// Each pre-task runs with its own isolated message history.
func (a *Agent) runPreTasks(ctx context.Context) error {
	for _, task := range a.config.PreTasks {
		if err := a.runPreTask(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) runPreTask(ctx context.Context, task PreTaskConfig) error {
	if a.OnPreTaskStart != nil {
		a.OnPreTaskStart(task.Name)
	}

	// Create isolated message history for this pre-task
	taskMsgs := []Message{{Role: "system", Content: task.SystemPrompt}}
	input := task.Input
	if input == "" {
		input = "Begin"
	}
	taskMsgs = append(taskMsgs, Message{Role: "user", Content: input})

	// Run task loop until DoneMarker detected
	for turns := 0; ; turns++ {
		if turns >= maxPreTaskTurns {
			return fmt.Errorf("pretask %s: %w", task.Name, ErrMaxTurnsExceeded)
		}
		msg, err := a.client.ChatCompletionWithTools(ctx, taskMsgs, a.apiTools)
		if err != nil {
			return err
		}
		a.fireResponseModel()
		a.fireContextUsage()
		taskMsgs = append(taskMsgs, *msg)
		content := messageContent(msg)
		if a.OnMessage != nil && strings.TrimSpace(content) != "" {
			a.OnMessage(content)
		}

		// Check for completion (no tool calls)
		if len(msg.ToolCalls) == 0 {
			if content == "" {
				return fmt.Errorf("pretask %s: %w", task.Name, ErrEmptyResponse)
			}
			break
		}

		// Execute tools and collect results
		var toolResults []Message
		cancelled := false

		for _, tc := range msg.ToolCalls {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)

			// Check if tool should execute
			if a.OnToolCall != nil && !a.OnToolCall(tc.Function.Name, args) {
				cancelled = true
				break
			}

			var result ToolResult
			tool, ok := a.registry.Get(tc.Function.Name)
			if !ok {
				result = ToolResult{Success: false, Error: fmt.Errorf("unknown tool: %s", tc.Function.Name), Status: "fail: unknown tool"}
			} else {
				result = tool.Execute(args)
			}

			if a.OnToolDone != nil {
				a.OnToolDone(tc.Function.Name, args, result)
			}

			// Append tool result message
			toolResults = append(toolResults, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result.Output,
			})
		}

		// If cancelled, rollback and skip remaining pre-tasks
		if cancelled {
			taskMsgs = taskMsgs[:len(taskMsgs)-1]
			if a.OnPreTaskEnd != nil {
				a.OnPreTaskEnd(task.Name)
			}
			return ErrToolCancelled
		}

		// Append all tool results
		taskMsgs = append(taskMsgs, toolResults...)
	}

	if a.OnPreTaskEnd != nil {
		a.OnPreTaskEnd(task.Name)
	}
	return nil
}

// RunSubAgent executes a subagent in an isolated message history.
// Tool schemas sent to the LLM are filtered to the subagent's AllowedTools.
// Returns the SubAgentResult (with Meta annotations from hooks) and appends
// a handoff to the main conversation so follow-up prompts have context.
func (a *Agent) RunSubAgent(ctx context.Context, sa SubAgent, input string) (*SubAgentResult, error) {
	if a.OnSubAgentStart != nil {
		a.OnSubAgentStart(sa.Name())
	}
	defer func() {
		if a.OnSubAgentEnd != nil {
			a.OnSubAgentEnd(sa.Name())
		}
	}()

	// Build filtered tool list for the LLM API call.
	allowed := sa.AllowedTools()
	tools := a.registry.Tools(allowed...)

	// Build allowlist set for runtime rejection.
	allowSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowSet[name] = true
	}

	// Isolated message history.
	userContent := input
	if f, ok := sa.(UserPromptFormatter); ok {
		userContent = f.FormatUserPrompt(input)
	}
	msgs := []Message{
		{Role: "system", Content: sa.SystemPrompt()},
		{Role: "user", Content: userContent},
	}

	for turns := 0; ; turns++ {
		if turns >= maxSubAgentTurns {
			return nil, fmt.Errorf("subagent %s: %w", sa.Name(), ErrMaxTurnsExceeded)
		}

		msg, err := a.client.ChatCompletionWithTools(ctx, msgs, tools)
		if err != nil {
			return nil, err
		}
		a.fireResponseModel()
		a.fireContextUsage()
		msgs = append(msgs, *msg)

		content := messageContent(msg)
		if a.OnMessage != nil && strings.TrimSpace(content) != "" {
			a.OnMessage(content)
		}

		if len(msg.ToolCalls) == 0 {
			if content == "" {
				return nil, fmt.Errorf("subagent %s: %w", sa.Name(), ErrEmptyResponse)
			}

			// Run hooks and handle approval/revision loop.
			hooks := a.subAgentHooks(sa.Name())
			result, err := a.runSubAgentHooks(ctx, sa, hooks, content, input, &msgs, tools, allowSet)
			if err != nil {
				return nil, err
			}

			// Build handoff based on hook annotations.
			handoff := a.buildSubAgentHandoff(sa.Name(), result)
			a.msgs = append(a.msgs, Message{Role: "assistant", Content: handoff})
			return result, nil
		}

		var toolResults []Message
		cancelled := false

		for _, tc := range msg.ToolCalls {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)

			// Runtime rejection for disallowed tools.
			if len(allowSet) > 0 && !allowSet[tc.Function.Name] {
				toolResults = append(toolResults, Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("error: tool %q is not available in this mode", tc.Function.Name),
				})
				continue
			}

			if a.OnToolCall != nil && !a.OnToolCall(tc.Function.Name, args) {
				cancelled = true
				break
			}

			var result ToolResult
			tool, ok := a.registry.Get(tc.Function.Name)
			if !ok {
				result = ToolResult{Success: false, Error: fmt.Errorf("unknown tool: %s", tc.Function.Name), Status: "fail: unknown tool"}
			} else {
				result = tool.Execute(args)
			}

			if a.OnToolDone != nil {
				a.OnToolDone(tc.Function.Name, args, result)
			}

			toolResults = append(toolResults, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result.Output,
			})
		}

		if cancelled {
			return nil, ErrToolCancelled
		}

		msgs = append(msgs, toolResults...)
	}
}

func messageContent(msg *Message) string {
	if msg == nil {
		return ""
	}
	switch v := msg.Content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			if t != "text" && t != "output_text" {
				continue
			}
			text, _ := m["text"].(string)
			if text == "" {
				continue
			}
			b.WriteString(text)
		}
		return b.String()
	default:
		return ""
	}
}

// lastAssistantContent returns the content of the most recent assistant message
// that has non-empty content. Used when the model returns empty (e.g. after a summary round).
func lastAssistantContent(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			if c := messageContent(&msgs[i]); strings.TrimSpace(c) != "" {
				return c
			}
		}
	}
	return ""
}

// lastMessageIs returns true if the last message has the given role and content.
func lastMessageIs(msgs []Message, role, content string) bool {
	if len(msgs) == 0 {
		return false
	}
	m := &msgs[len(msgs)-1]
	if m.Role != role {
		return false
	}
	c, ok := m.Content.(string)
	return ok && c == content
}

// fireContextUsage calls OnContextUsage with the latest prompt usage percentage and cumulative cost.
func (a *Agent) fireContextUsage() {
	if a.OnContextUsage == nil {
		return
	}
	usage := a.client.LastUsage()
	if usage == nil {
		return
	}
	var pct, cost float64
	if usage.PromptUsagePct != nil {
		pct = *usage.PromptUsagePct
	}
	if usage.TotalSessionCost != nil {
		cost = *usage.TotalSessionCost
	}
	if pct == 0 && cost == 0 {
		return
	}
	a.OnContextUsage(pct, cost)
}

func (a *Agent) fireResponseModel() {
	if a.OnResponseModel == nil {
		return
	}
	model := strings.TrimSpace(a.client.LastModel())
	if model == "" {
		return
	}
	a.OnResponseModel(model)
}

// runSubAgentHooks executes hooks after a subagent completes and handles the
// revision loop: if a hook sets Meta["approval"]="revise", the feedback is
// appended to the isolated history and the LLM loop continues.
func (a *Agent) runSubAgentHooks(
	ctx context.Context,
	sa SubAgent,
	hooks []SubAgentHook,
	content, input string,
	msgs *[]Message,
	tools []Tool,
	allowSet map[string]bool,
) (*SubAgentResult, error) {
	if len(hooks) == 0 {
		return &SubAgentResult{
			Name:    sa.Name(),
			Input:   input,
			Content: content,
			Meta:    make(map[string]string),
		}, nil
	}

	cwd, _ := os.Getwd()

	for {
		result := &SubAgentResult{
			Name:    sa.Name(),
			Input:   input,
			Content: content,
			CWD:     cwd,
			Meta:    make(map[string]string),
		}

		// Preserve output_path across revisions so PersistHook overwrites the same file.
		if prev, ok := a.subAgentLastOutputPath[sa.Name()]; ok {
			result.Meta["output_path"] = prev
		}

		for _, hook := range hooks {
			if err := hook.AfterComplete(ctx, result); err != nil {
				log.Printf("subagent %s hook error: %v", sa.Name(), err)
			}
		}

		// Remember output_path for revision cycles.
		if p := result.Meta["output_path"]; p != "" {
			if a.subAgentLastOutputPath == nil {
				a.subAgentLastOutputPath = make(map[string]string)
			}
			a.subAgentLastOutputPath[sa.Name()] = p
		}

		if result.Meta["approval"] != "revise" {
			// Clean up revision state.
			delete(a.subAgentLastOutputPath, sa.Name())
			return result, nil
		}

		// Revision: append feedback and continue the LLM loop.
		feedback := result.Meta["feedback"]
		*msgs = append(*msgs, Message{Role: "user", Content: feedback})

		revised, err := a.continueSubAgentLoop(ctx, sa, msgs, tools, allowSet)
		if err != nil {
			return nil, err
		}
		content = revised
	}
}

// continueSubAgentLoop runs the subagent LLM loop from the current message
// state until the next final response (no tool calls).
func (a *Agent) continueSubAgentLoop(
	ctx context.Context,
	sa SubAgent,
	msgs *[]Message,
	tools []Tool,
	allowSet map[string]bool,
) (string, error) {
	for turns := 0; ; turns++ {
		if turns >= maxSubAgentTurns {
			return "", fmt.Errorf("subagent %s: %w", sa.Name(), ErrMaxTurnsExceeded)
		}

		msg, err := a.client.ChatCompletionWithTools(ctx, *msgs, tools)
		if err != nil {
			return "", err
		}
		a.fireResponseModel()
		a.fireContextUsage()
		*msgs = append(*msgs, *msg)

		content := messageContent(msg)
		if a.OnMessage != nil && strings.TrimSpace(content) != "" {
			a.OnMessage(content)
		}

		if len(msg.ToolCalls) == 0 {
			if content == "" {
				return "", fmt.Errorf("subagent %s: %w", sa.Name(), ErrEmptyResponse)
			}
			return content, nil
		}

		var toolResults []Message
		cancelled := false

		for _, tc := range msg.ToolCalls {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)

			if len(allowSet) > 0 && !allowSet[tc.Function.Name] {
				toolResults = append(toolResults, Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("error: tool %q is not available in this mode", tc.Function.Name),
				})
				continue
			}

			if a.OnToolCall != nil && !a.OnToolCall(tc.Function.Name, args) {
				cancelled = true
				break
			}

			var result ToolResult
			tool, ok := a.registry.Get(tc.Function.Name)
			if !ok {
				result = ToolResult{Success: false, Error: fmt.Errorf("unknown tool: %s", tc.Function.Name), Status: "fail: unknown tool"}
			} else {
				result = tool.Execute(args)
			}

			if a.OnToolDone != nil {
				a.OnToolDone(tc.Function.Name, args, result)
			}

			toolResults = append(toolResults, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result.Output,
			})
		}

		if cancelled {
			return "", ErrToolCancelled
		}

		*msgs = append(*msgs, toolResults...)
	}
}

// buildSubAgentHandoff constructs the handoff message injected into the main
// conversation based on hook annotations.
// ProgressiveDisclosure
func (a *Agent) buildSubAgentHandoff(name string, result *SubAgentResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s agent summary]\n%s", name, result.Content)

	if path := result.Meta["output_path"]; path != "" {
		fmt.Fprintf(&b, "\n\nPlan saved to %s.", path)
	}

	switch result.Meta["approval"] {
	case "approved":
		b.WriteString(`
Implement the plan above. Follow each step in order.

After completing all tasks, provide a final implementation status with exactly these sections:

### Completed Tasks
A markdown table with columns: Task, Status (Done/Failed/Skipped).

### Verification Results
Bullet points for each verification performed (e.g., tests passing, build succeeding, code formatted).

### Files Changed
A list of files that were created or modified, with (NEW) or (MODIFIED) markers.`)
	case "rejected":
		b.WriteString("\nPlan was not approved for implementation.")
	}

	return b.String()
}
