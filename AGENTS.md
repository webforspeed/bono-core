> Go library for building autonomous LLM agents with sandboxed shell execution

## Project Map

**Entry:** `NewAgent(config)` in `agent.go` - creates agent instance
**Core:** `agent.go` - autonomous agent loop with tool execution

### Structure (grouped)

- `agent.go` - Agent struct, Chat() loop, pre-tasks
- `client.go` - HTTP client for OpenRouter/OpenAI-compatible APIs
- `config.go` - Config, PreTaskConfig, SandboxConfig structs, validation
- `types.go` - Message, ToolCall, ChatRequest/Response, ToolResult, ExecMeta
- `tools.go` - ExecuteReadFile, ExecuteWriteFile, ExecuteEditFile, ExecuteShell, ExecuteShellWithSandbox, ExecuteTool
- `errors.go` - Sentinel errors (ErrMissingAPIKey, ErrToolCancelled, ErrSandboxBlocked, etc.)
- `pretasks.go` - DocSystemPrompt, DefaultExploringTask()
- `sandbox.go` - ShellExecutor interface, SandboxedExecutor, PassthroughExecutor
- `tools.json` - Tool definitions for API

### Commands

- Build: `go build ./...`
- Test: `go test ./...`
- Test verbose: `go test -v ./...`
- Lint: `go vet ./...`
- Format: `gofmt -w .`

### Conventions

- New tool → Add execute function in `tools.go`, add case to `ExecuteTool()`
- New error → Define sentinel error in `errors.go`
- New hook → Add field to `Agent` struct in `agent.go`
- New pre-task → Create `PreTaskConfig` with Name, SystemPrompt, Input, DoneMarker
- Package name is `core` (import as `github.com/webforspeed/bono-core`)

### Finding things

- Agent loop logic → `Chat()` in `agent.go`
- Tool execution → `ExecuteTool()` in `tools.go`
- Shell with sandbox → `ExecuteShellWithSandbox()` in `tools.go`
- API communication → `ChatCompletion()` in `client.go`
- Sandbox logic → `SandboxedExecutor.Run()` in `sandbox.go`
- Configuration → `Config`, `SandboxConfig` in `config.go`
- Pre-task system → `runPreTasks()` in `agent.go`, prompts in `pretasks.go`
- Type definitions → `types.go`

### Documentation

- Usage examples and API overview → `README.md`
- Tool JSON schema → `tools.json`

## Rules

### Always

- Zero external dependencies - stdlib only
- Use `fmt.Errorf("context: %w", err)` for error wrapping
- Return `ToolResult` struct from all tool functions
- Include `Success`, `Output`, `Status`, and optionally `Error`/`ExecMeta` in ToolResult
- Shell commands go through `GetExecutor()` for sandbox support

### Never

- Don't add external dependencies
- Don't modify `.env` file (contains API keys)
- Don't expose API keys in code or logs
- Don't skip config validation in NewClient/NewAgent
- Don't bypass sandbox by calling exec.Command directly in tools

### Gotchas

- `ExecuteShell()` always goes through sandbox; use `ExecuteShellUnsandboxed()` only for approved fallback
- `ExecuteShellWithSandbox()` tries sandbox first, falls back via `OnSandboxFallback` hook if blocked
- Sandbox is macOS-only (`sandbox-exec`); other platforms use `PassthroughExecutor`
- Tool results are appended as proper `tool` role messages with `ToolCallID` - full context preserved

### When unsure

- Ask before changing public API signatures (Agent, Config, hooks)
- Ask before adding new tool types
- Ask before modifying the agent loop in Chat()
- Ask before changing sandbox policies or defaults
- Check README.md for intended behavior before changing
