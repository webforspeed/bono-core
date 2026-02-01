# Bono Core

General-purpose autonomous agent core and API client for building CLIs, TUIs, HTTP services, IDE extensions, and other app integrations.

## Features

- [x] Autonomous agent loop with tool-call execution
- [x] Built‑in file tools (read/write/edit)
- [x] Shell execution with user approvals
- [ ] Worktree‑isolated runs (disposable workspaces)
- [ ] Sandboxed execution (FS/command restrictions)
- [ ] Agent config files (AGENT.md, CLAUDE.md)
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

    // You control the loop
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

## Dependencies

This package has **zero external dependencies** - only Go standard library.
