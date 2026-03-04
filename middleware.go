package core

import "fmt"

// MessageMiddleware transforms messages before they are sent to the API.
// Implementations must not mutate the input slice; return a new or modified slice.
type MessageMiddleware func(messages []Message) []Message

// ContextUsageMiddleware returns a middleware that appends context window
// usage metadata to the last user or tool message. This keeps the model
// aware of how much context remains so it can adjust verbosity.
func ContextUsageMiddleware(usageFn func() *ResponseUsage) MessageMiddleware {
	return func(messages []Message) []Message {
		usage := usageFn()
		if usage == nil || usage.PromptUsagePct == nil {
			return messages
		}

		tag := formatContextUsage(usage)
		if tag == "" {
			return messages
		}

		// Shallow-copy the slice so callers' original is untouched.
		out := make([]Message, len(messages))
		copy(out, messages)

		// Inject into the last user or tool message only — that's the
		// most recent context the model will read before responding.
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].Role != "user" && out[i].Role != "tool" {
				continue
			}
			content, ok := out[i].Content.(string)
			if !ok {
				continue
			}
			out[i] = Message{
				Role:       out[i].Role,
				Content:    content + "\n" + tag,
				ToolCalls:  out[i].ToolCalls,
				ToolCallID: out[i].ToolCallID,
			}
			break
		}

		return out
	}
}
// ProgressiveDisclosure
func formatContextUsage(u *ResponseUsage) string {
	if u == nil || u.PromptUsagePct == nil {
		return ""
	}

	remaining := ""
	if u.PromptTokens != nil && u.ContextLimit != nil && *u.ContextLimit > 0 {
		rem := *u.ContextLimit - *u.PromptTokens
		if rem < 0 {
			rem = 0
		}
		remaining = fmt.Sprintf("\nremaining: %d tokens", rem)
	}

	return fmt.Sprintf("<context_usage>\nused: %.1f%%%s\n</context_usage>",
		*u.PromptUsagePct, remaining)
}
