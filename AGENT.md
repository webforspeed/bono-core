## Project Map

**Entry:** `NewAgent(config)` in `agent.go` - creates agent instance  
**Core:** `agent.go` - autonomous agent loop with tool execution

### Structure (grouped)

- `agent.go` - Agent struct, Chat() loop, pre-tasks execution
- `client.go` - HTTP client for OpenRouter/OpenAI-compatible APIs
- `config.go` - Config struct, PreTaskConfig, validation
- `types.go` - Message, ToolCall, ChatRequest/Response types
- `tools.go` - ExecuteReadFile, ExecuteWriteFile, ExecuteEditFile, ExecuteShell
- `errors.go` - Sentinel errors (ErrMissingAPIKey, ErrToolCancelled, etc.)
- `pretasks.go` - DocSystemPrompt, DefaultExploringTask()
- `doc.go` - Package documentation
- `tools.json` - Tool definitions for API

### Conventions

- New tool → Add execute function in `tools.go`, add case to `ExecuteTool()`
- New error → Define sentinel error in `errors.go`
- New hook → Add field to `Agent` struct in `agent.go`
- New pre-task → Create `PreTaskConfig` with Name, SystemPrompt, Input, DoneMarker
- Package name is `core` (import as `github.com/webforspeed/bono-core`)

### Finding things

- Agent loop logic → `Chat()` in `agent.go`
- Tool execution → `ExecuteTool()` in `tools.go`
- API communication → `ChatCompletion()` in `client.go`
- Configuration → `Config` struct in `config.go`
- Pre-task system → `runPreTasks()` in `agent.go`, prompts in `pretasks.go`
- Type definitions → `types.go`

## Rules

### Always

- Zero external dependencies - stdlib only
- Use `fmt.Errorf("context: %w", err)` for error wrapping
- Return `ToolResult` struct from all tool functions
- Include `Success`, `Output`, `Status`, and optionally `Error` in ToolResult

### Never

- Don't add external dependencies
- Don't modify `.env` file (contains API keys)
- Don't expose API keys in code or logs
- Don't skip config validation in NewClient/NewAgent

### Style

- Prefer early returns over nested conditionals
- Use named return types sparingly
- Group imports: stdlib only (no external deps)
- Comments on exported functions follow godoc conventions
- Keep functions focused - one responsibility each

### When unsure

- Ask before changing public API signatures (Agent, Config, hooks)
- Ask before adding new tool types
- Ask before modifying the agent loop in Chat()
- Check README.md for intended behavior before changing
