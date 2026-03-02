# llm

Portable LLM client package. Code against `Provider` interface, swap implementations without changing call sites.

## Three implementations, one interface

```go
var provider llm.Provider

// Anthropic Messages API — native Claude format, tool_use content blocks
provider, _ = llm.NewMessagesClient(cfg)

// Chat Completions API — OpenAI-compatible, works with any model on OpenRouter
provider, _ = llm.NewCompletionsClient(cfg)

// Responses API — OpenAI Responses format with input/output items
provider, _ = llm.NewResponsesClient(cfg)

// Same call site either way
resp, err := provider.SendMessage(ctx, &llm.Request{...})
```

## Anthropic Messages API (`NewMessagesClient`)

Endpoint: `POST {BaseURL}/messages`

```go
client, err := llm.NewMessagesClient(llm.Config{
    APIKey: os.Getenv("OPENROUTER_API_KEY"),
    // BaseURL defaults to https://openrouter.ai/api/v1
    // HTTPTimeout defaults to 120s
})

resp, err := client.SendMessage(ctx, &llm.Request{
    Model:     "anthropic/claude-sonnet-4-20250514",
    MaxTokens: 4096,   // required by Messages API
    System:    "You are helpful.",
    Messages:  []llm.Message{{Role: llm.RoleUser, Content: "Hello"}},
    Tools:     tools,  // optional
})
```

## Chat Completions API (`NewCompletionsClient`)

Endpoint: `POST {BaseURL}/chat/completions`

```go
client, err := llm.NewCompletionsClient(llm.Config{
    APIKey: os.Getenv("OPENROUTER_API_KEY"),
})

resp, err := client.SendMessage(ctx, &llm.Request{
    Model:     "openai/gpt-4",
    MaxTokens: 4096,   // optional for Completions API
    System:    "You are helpful.",
    Messages:  []llm.Message{{Role: llm.RoleUser, Content: "Hello"}},
})
```

## Responses API (`NewResponsesClient`)

Endpoint: `POST {BaseURL}/responses`

```go
client, err := llm.NewResponsesClient(llm.Config{
    APIKey: os.Getenv("OPENROUTER_API_KEY"),
})

resp, err := client.SendMessage(ctx, &llm.Request{
    Model:     "openai/gpt-4o",
    MaxTokens: 4096,   // maps to max_output_tokens
    System:    "You are helpful.",  // maps to instructions field
    Messages:  []llm.Message{{Role: llm.RoleUser, Content: "Hello"}},
})
```

## Tool loop pattern (works with all three)

```go
// After getting resp with ToolCalls, execute them and send results back:
msgs = append(msgs,
    llm.Message{Role: llm.RoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls},
    llm.Message{Role: llm.RoleUser, ToolResults: []llm.ToolResult{{
        ToolUseID: resp.ToolCalls[0].ID,
        Content:   resultJSON,
        IsError:   false, // set true to signal failure to the model
    }}},
)
resp, err = provider.SendMessage(ctx, &llm.Request{...Messages: msgs})
```

Each implementation converts this to the correct wire format:
- Messages: `tool_result` content blocks in a `user` message
- Completions: separate messages with `role: "tool"` and `tool_call_id`
- Responses: `function_call` + `function_call_output` input items

## Swapping at runtime

```go
var provider llm.Provider
switch os.Getenv("LLM_API") {
case "messages":
    provider, _ = llm.NewMessagesClient(cfg)
case "responses":
    provider, _ = llm.NewResponsesClient(cfg)
default:
    provider, _ = llm.NewCompletionsClient(cfg)
}
```

## API mapping notes

Key wire format differences the implementations abstract over:

| Concept | Messages API | Chat Completions API | Responses API |
|---------|-------------|---------------------|---------------|
| Endpoint | `/messages` | `/chat/completions` | `/responses` |
| Tool calls from assistant | Content blocks `type: "tool_use"` | `tool_calls` array with `function` | Output items `type: "function_call"` |
| Tool results | User msg with `tool_result` blocks | `role: "tool"` msgs with `tool_call_id` | Input items `type: "function_call_output"` |
| System prompt | Top-level `system` field | `role: "system"` message | `instructions` field |
| Token limit | `max_tokens` (required) | `max_tokens` (optional) | `max_output_tokens` (optional) |
| Stop signal | `stop_reason` enum | `finish_reason` enum (mapped) | `status` field (mapped) |
| Tool schema field | `input_schema` | `parameters` under `function` | `parameters` at top level |
| Error format | `{type, error: {type, message}}` | `{error: {code, message}}` | `{error: {code, message}}` |
| Response structure | `content[]` blocks | `choices[].message` | `output[]` items + `output_text` |
| Tool call ID | `id` on tool_use block | `id` on tool_call object | `call_id` on function_call item |

## StopReason mapping

| Messages API | Completions API | Responses API | `llm.StopReason` |
|-------------|----------------|---------------|-----------------|
| `end_turn` | `stop` | `completed` (no tool calls) | `StopReasonEndTurn` |
| `max_tokens` | `length` | `incomplete` | `StopReasonMaxTokens` |
| `tool_use` | `tool_calls` | `completed` (with tool calls) | `StopReasonToolUse` |
| `stop_sequence` | — | — | `StopReasonStopSequence` |
| — | `content_filter` | — | passed through as-is |

## Not implemented (yet)

### Cross-cutting (all three APIs)

| Feature | Messages | Completions | Responses | Notes |
|---------|:--------:|:-----------:|:---------:|-------|
| Streaming | `stream` | `stream`, `stream_options` | `stream` | Would add `SendMessageStream` returning iterator/channel |
| Extended thinking / reasoning | `thinking` with `budget_tokens` | `reasoning` with `effort` (xhigh→none) + `summary` | `reasoning` with `effort`, `summary`, `max_tokens`, `enabled` | Thinking content blocks in response |
| Tool choice | `tool_choice` (auto/any/tool) | `tool_choice` (auto/none/required/function) | `tool_choice` (auto/none/required/function) | Control which tools the model can call |
| Provider routing | — | `provider` preferences | `provider` preferences | OpenRouter `order`, `only`, `ignore`, `sort` for provider selection |
| Request metadata | `metadata.user_id` | `user`, `session_id`, `trace` (trace_id, span_name, etc.) | `metadata` | Observability and tracking |
| Prompt caching | `cache_control` on system/messages/tools with TTL (ephemeral, 5m, 1h) | — | `prompt_cache_key` | Reduces cost on repeated prefixes |

### Messages API (`/messages`)

| Request field | Description |
|--------------|-------------|
| `thinking` | Extended thinking with `type: "enabled"` and `budget_tokens`. Returns `thinking` content blocks in response |
| `tool_choice` | `auto` (default), `any` (must use a tool), or `{type: "tool", name: "..."}` |
| `metadata` | `user_id` for abuse detection |
| `top_k` | Top-K sampling (only in Messages API) |
| `cache_control` | Attach `{type: "ephemeral"}` to system, messages, or tools for prompt caching with TTL |
| `output_config` | Control output behavior with `effort` levels (low/medium/high/max) |
| Image/PDF content | `type: "image"` and `type: "document"` content blocks in user messages (base64 or URL) |

| Response field | Description |
|---------------|-------------|
| `thinking` blocks | Content blocks with `type: "thinking"` containing model's chain-of-thought |
| `usage.cache_creation_input_tokens` | Tokens written to cache |
| `usage.cache_read_input_tokens` | Tokens read from cache |
| `usage.cache_creation` | Detailed cache breakdown: `ephemeral_5m_input_tokens`, `ephemeral_1h_input_tokens` |
| `usage.server_tool_use` | `web_search_requests` count |
| `service_tier` | Billing tier: standard, priority, batch |

### Chat Completions API (`/chat/completions`)

| Request field | Description |
|--------------|-------------|
| `response_format` | `{type: "json_object"}` for JSON mode, or `{type: "json_schema", json_schema: {...}}` for structured output |
| `reasoning` | `{effort: "high", summary: "auto"}` for extended thinking (model-dependent) |
| `seed` | Integer for deterministic sampling (best-effort) |
| `frequency_penalty` | -2.0 to 2.0, penalize tokens by frequency in text so far |
| `presence_penalty` | -2.0 to 2.0, penalize tokens that appear at all in text so far |
| `logit_bias` | `{token_id: bias}` map to adjust specific token probabilities |
| `logprobs` + `top_logprobs` | Return log probabilities for output tokens (0–20 top alternatives) |
| `n` | Number of completions to generate (only first choice is used currently) |
| `max_completion_tokens` | Alternative to `max_tokens` (some models use this instead) |
| `parallel_tool_calls` | Allow model to issue multiple tool calls in one response |
| `debug` | `{transform_only: true}` returns the transformed request without calling the model |
| `image_config` | Provider-specific image options (e.g. `{aspect_ratio: "16:9"}`) |

| Response field | Description |
|---------------|-------------|
| `choices[].logprobs` | Per-token log probabilities and top alternatives |
| `choices[].message.reasoning` | Extended reasoning content (when `reasoning` enabled) |
| `usage.prompt_tokens_details` | `cached_tokens`, `cache_write_tokens`, `audio_tokens`, `video_tokens` |
| `usage.completion_tokens_details` | `reasoning_tokens`, `audio_tokens`, `accepted_prediction_tokens`, `rejected_prediction_tokens` |
| `system_fingerprint` | Backend configuration identifier |

### Responses API (`/responses`)

| Request field | Description |
|--------------|-------------|
| `previous_response_id` | Chain responses server-side without re-sending full history |
| `reasoning` | `{effort: "high", summary: "auto", max_tokens: 1024, enabled: true}` |
| `text` | Response text config: `{format: {type: "json_schema", ...}, verbosity: "medium"}` |
| `tool_choice` | `auto`, `none`, `required`, or `{type: "function", name: "..."}` |
| `parallel_tool_calls` | Allow parallel function calls |
| `modalities` | Output types: `["text"]`, `["text", "image"]` |
| `include` | Extra fields in response: `file_search_call.results`, `reasoning.encrypted_content`, etc. |
| `truncation` | `auto` (truncate oldest input when context exceeded) or `disabled` (error instead) |
| `store` | Persist response for `previous_response_id` chains |
| `background` | Run async, poll for completion |
| `prompt` | Template with `{variables}` and file-based inputs |
| `top_k` | Top-K sampling |
| `frequency_penalty` / `presence_penalty` | Repetition penalties (-2.0 to 2.0) |
| `max_tool_calls` | Cap on total tool invocations |
| `top_logprobs` | Return top-N log probabilities (0–20) |
| `image_config` | Provider-specific image generation options |

| Response field | Description |
|---------------|-------------|
| `output[]` reasoning items | `{type: "reasoning", content: [...]}` with thinking text and signature |
| `output[]` web_search_call | `{type: "web_search_call", id, status}` |
| `output[]` file_search_call | `{type: "file_search_call", id, queries, results}` |
| `output[]` image_generation_call | `{type: "image_generation_call", id, result}` |
| `incomplete_details` | `{reason: "max_output_tokens" \| "content_filter"}` when status is `incomplete` |
| `usage.cost` | Estimated cost in USD |
| `usage.input_tokens_details` | `cached_tokens` breakdown |
| `usage.output_tokens_details` | `reasoning_tokens` breakdown |
| `error` | `{code: "server_error" \| "rate_limit_error" \| ..., message: "..."}` when status is `failed` |
