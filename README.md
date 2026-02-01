# Bono Core

General-purpose autonomous agent core and API client for building CLIs, TUIs, HTTP services, IDE extensions, and other app integrations.

## Features

- [x] Autonomous agent loop with tool-call execution
- [x] Built‑in file tools (read/write/edit)
- [x] Shell execution with user approvals
- [x] Pre-tasks system (run sub-agents before main chat)
- [x] Default exploring task (auto-generates AGENT.md)
- [ ] Worktree‑isolated runs (disposable workspaces)
- [ ] Sandboxed execution (FS/command restrictions)
- [ ] Skills system (prompt/tool bundles)
- [ ] MCP tool integrations
- [ ] Slash commands (built-in + user-defined)
- [ ] Plan mode (preview and approve steps before execution)
- [ ] Sub-agents (delegate subtasks to child agents)
- [ ] Parallel tool execution (concurrent tool calls)

## Installation

```bash
go get github.com/webforspeed/bono-core
```

## Usage

```go
import (
    "context"
    "fmt"
    "os"

    core "github.com/webforspeed/bono-core"
)

func main() {
    config := core.Config{
        APIKey:       os.Getenv("API_KEY"),
        BaseURL:      "https://openrouter.ai/api/v1",
        Model:        "anthropic/claude-opus-4.5",
        SystemPrompt: "You are a helpful assistant.",
    }

    agent, err := core.NewAgent(config)
    if err != nil {
        panic(err)
    }

    // Optional: set hooks for customization
    agent.OnToolCall = func(name string, args map[string]any) bool {
        fmt.Printf("Executing: %s\n", name)
        return true // auto-approve all tools
    }

    agent.OnMessage = func(content string) {
        fmt.Println(content)
    }

    // Chat() auto-runs pre-tasks on first call if configured
    response, err := agent.Chat(context.Background(), "Hello!")
    if err != nil {
        panic(err)
    }
    fmt.Println(response)
}
```

## Hooks

All hooks are optional (nil = default behavior):

| Hook | Description |
|------|-------------|
| `OnToolCall(name, args) bool` | Return false to skip tool execution |
| `OnToolDone(name, args, result)` | Called after tool executes |
| `OnMessage(content)` | Called when assistant responds |
| `OnPreTaskStart(name)` | Called when a pre-task begins |
| `OnPreTaskEnd(name)` | Called when a pre-task completes |

## Pre-Tasks

Pre-tasks are sub-agents that run automatically on the first `Chat()` call. They execute with isolated message history before the main agent loop begins.

```go
config := core.Config{
    // ... other config
    PreTasks: []core.PreTaskConfig{
        core.DefaultExploringTask(), // auto-generates AGENT.md
    },
}
```

The default exploring task ensures an `AGENT.md` file exists with project documentation (structure, conventions, rules). You can also define custom pre-tasks:

```go
core.PreTaskConfig{
    Name:         "setup",
    SystemPrompt: "Your task-specific prompt here",
    Input:        "Begin",      // initial user message
    DoneMarker:   "{{DONE}}",   // completion signal
}
```

## Built-in Tools

The package includes execution functions for common tools:

| Function | Description |
|----------|-------------|
| `ExecuteReadFile(path)` | Read file contents |
| `ExecuteWriteFile(path, content)` | Write content to file |
| `ExecuteEditFile(path, old, new, replaceAll)` | String replacement in file |
| `ExecuteShell(command)` | Execute shell command |
| `ExecuteTool(name, args)` | Dispatcher for all tools |

## Access Patterns

This library is designed to support multiple access patterns:

- **Library**: Call `agent.Chat()` directly
- **stdio**: Wrap with JSON-RPC reader/writer
- **HTTP**: Wrap with HTTP handler

### Example: stdio Mode

```go
agent, _ := core.NewAgent(config)
agent.OnToolCall = func(name string, args map[string]any) bool { return true }

decoder := json.NewDecoder(os.Stdin)
encoder := json.NewEncoder(os.Stdout)

for {
    var req Request
    decoder.Decode(&req)
    response, _ := agent.Chat(context.Background(), req.Message)
    encoder.Encode(Response{Content: response})
}
```

### Example: HTTP Mode

```go
agent, _ := core.NewAgent(config)

http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
    var req ChatRequest
    json.NewDecoder(r.Body).Decode(&req)
    response, _ := agent.Chat(r.Context(), req.Message)
    json.NewEncoder(w).Encode(map[string]string{"response": response})
})
```

## Quirks

What makes this different from other open source agents:

- **Self-documenting**: The agent automatically creates and maintains an `AGENT.md` file with project context. This runs as a separate sub-agent with its own context window, so the main agent doesn't suffer from context rot as the conversation grows.

- **Hook-based architecture**: All agent behavior flows through hooks (`OnToolCall`, `OnToolDone`, `OnMessage`, etc.), making it trivial to build any frontend—CLI, TUI, HTTP API, IDE extension—on top of the same core.

- **Model agnostic**: Uses OpenRouter as the backend, so you can swap models freely. Claude, GPT-4, Gemini, Llama, Mistral—whatever works for your use case.

- **Transparent tool calls**: Every tool includes a plain-English description and a required `safety` classification (`read-only`, `modify`, `destructive`, `network`, `privileged`). You always know what the agent is about to do before approving.

- **Self-correcting**: The agent is eager to verify its work and validate after every mutation—commands, file changes, or tasks. This feedback loop lets it catch mistakes early and keep iterating until the goal is actually complete.

## Dependencies

This package has **zero external dependencies** - only Go standard library.
