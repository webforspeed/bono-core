package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewMessagesClient(t *testing.T) {
	t.Run("missing API key", func(t *testing.T) {
		_, err := NewMessagesClient(Config{})
		if err != ErrMissingAPIKey {
			t.Fatalf("got %v, want ErrMissingAPIKey", err)
		}
	})

	t.Run("defaults applied", func(t *testing.T) {
		c, err := NewMessagesClient(Config{APIKey: "test-key"})
		if err != nil {
			t.Fatal(err)
		}
		if c.config.BaseURL != "https://openrouter.ai/api/v1" {
			t.Errorf("BaseURL = %q, want default", c.config.BaseURL)
		}
		if c.config.HTTPTimeout != 120_000_000_000 { // 120s in ns
			t.Errorf("HTTPTimeout = %v, want 120s", c.config.HTTPTimeout)
		}
	})

	t.Run("custom config preserved", func(t *testing.T) {
		c, err := NewMessagesClient(Config{
			APIKey:  "test-key",
			BaseURL: "https://custom.api/v1",
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.config.BaseURL != "https://custom.api/v1" {
			t.Errorf("BaseURL = %q, want custom", c.config.BaseURL)
		}
	})
}

func TestMessagesClient_ImplementsProvider(t *testing.T) {
	var _ Provider = (*MessagesClient)(nil)
}

func TestSendMessage_SimpleText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path.
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/messages" {
			t.Errorf("path = %s, want /messages", r.URL.Path)
		}

		// Verify headers.
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("X-Title"); got != "TestApp" {
			t.Errorf("X-Title = %q, want TestApp", got)
		}
		if got := r.Header.Get("HTTP-Referer"); got != "http://test" {
			t.Errorf("HTTP-Referer = %q, want http://test", got)
		}

		// Verify request body.
		body, _ := io.ReadAll(r.Body)
		var req messagesRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.Model != "anthropic/claude-sonnet-4-20250514" {
			t.Errorf("model = %q", req.Model)
		}
		if req.MaxTokens != 1024 {
			t.Errorf("max_tokens = %d", req.MaxTokens)
		}
		if req.System != "You are helpful." {
			t.Errorf("system = %q", req.System)
		}
		if len(req.Messages) != 1 {
			t.Fatalf("messages len = %d", len(req.Messages))
		}

		// Return a text response.
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "Hello! How can I help?"},
			},
			"model":         "anthropic/claude-sonnet-4-20250514",
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 8,
			},
		})
	}))
	defer srv.Close()

	client, err := NewMessagesClient(Config{
		APIKey:      "test-key",
		BaseURL:     srv.URL,
		HTTPReferer: "http://test",
		AppTitle:    "TestApp",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "anthropic/claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    "You are helpful.",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.ID != "msg_123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Content != "Hello! How can I help?" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if resp.Model != "anthropic/claude-sonnet-4-20250514" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 8 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("ToolCalls = %v, want empty", resp.ToolCalls)
	}
}

func TestSendMessage_ToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req messagesRequest
		json.Unmarshal(body, &req)

		// Verify tools are sent correctly.
		if len(req.Tools) != 1 {
			t.Fatalf("tools len = %d, want 1", len(req.Tools))
		}
		if req.Tools[0].Name != "get_weather" {
			t.Errorf("tool name = %q", req.Tools[0].Name)
		}
		if req.Tools[0].InputSchema["type"] != "object" {
			t.Errorf("tool input_schema type = %v", req.Tools[0].InputSchema["type"])
		}

		// Return tool_use response.
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_456",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "Let me check the weather."},
				{
					"type":  "tool_use",
					"id":    "toolu_abc",
					"name":  "get_weather",
					"input": map[string]any{"city": "London"},
				},
			},
			"model":       "anthropic/claude-sonnet-4-20250514",
			"stop_reason": "tool_use",
			"usage":       map[string]any{"input_tokens": 20, "output_tokens": 15},
		})
	}))
	defer srv.Close()

	client, _ := NewMessagesClient(Config{APIKey: "key", BaseURL: srv.URL})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "anthropic/claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages:  []Message{{Role: RoleUser, Content: "What's the weather in London?"}},
		Tools: []Tool{{
			Name:        "get_weather",
			Description: "Get the current weather",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
				"required": []string{"city"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Content != "Let me check the weather." {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.StopReason != StopReasonToolUse {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "toolu_abc" || tc.Name != "get_weather" {
		t.Errorf("ToolCall = %+v", tc)
	}
	if tc.Input["city"] != "London" {
		t.Errorf("ToolCall input = %v", tc.Input)
	}
}

func TestSendMessage_ToolResultConversion(t *testing.T) {
	var capturedReq messagesRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg_789",
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "text", "text": "It's sunny in London."}},
			"model":       "anthropic/claude-sonnet-4-20250514",
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 30, "output_tokens": 10},
		})
	}))
	defer srv.Close()

	client, _ := NewMessagesClient(Config{APIKey: "key", BaseURL: srv.URL})

	// Simulate a full tool-use conversation:
	// 1. user asks question
	// 2. assistant calls tool
	// 3. user provides tool result
	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "anthropic/claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: RoleUser, Content: "What's the weather in London?"},
			{
				Role:    RoleAssistant,
				Content: "Let me check.",
				ToolCalls: []ToolCall{{
					ID:    "toolu_abc",
					Name:  "get_weather",
					Input: map[string]any{"city": "London"},
				}},
			},
			{
				Role: RoleUser,
				ToolResults: []ToolResult{{
					ToolUseID: "toolu_abc",
					Content:   `{"temp": 22, "condition": "sunny"}`,
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify the wire format of messages.
	if len(capturedReq.Messages) != 3 {
		t.Fatalf("wire messages len = %d, want 3", len(capturedReq.Messages))
	}

	// First message: simple text.
	msg0 := capturedReq.Messages[0]
	if msg0.Role != "user" {
		t.Errorf("msg[0] role = %q", msg0.Role)
	}
	if s, ok := msg0.Content.(string); !ok || s != "What's the weather in London?" {
		t.Errorf("msg[0] content = %v", msg0.Content)
	}

	// Second message: assistant with text + tool_use blocks.
	msg1 := capturedReq.Messages[1]
	if msg1.Role != "assistant" {
		t.Errorf("msg[1] role = %q", msg1.Role)
	}
	blocks1, ok := msg1.Content.([]any)
	if !ok {
		t.Fatalf("msg[1] content type = %T, want []any", msg1.Content)
	}
	if len(blocks1) != 2 {
		t.Fatalf("msg[1] blocks len = %d, want 2", len(blocks1))
	}

	// Third message: user with tool_result block.
	msg2 := capturedReq.Messages[2]
	if msg2.Role != "user" {
		t.Errorf("msg[2] role = %q, want user", msg2.Role)
	}
	blocks2, ok := msg2.Content.([]any)
	if !ok {
		t.Fatalf("msg[2] content type = %T, want []any", msg2.Content)
	}
	if len(blocks2) != 1 {
		t.Fatalf("msg[2] blocks len = %d, want 1", len(blocks2))
	}
	// Verify the tool_result block has the right structure.
	trBlock, ok := blocks2[0].(map[string]any)
	if !ok {
		t.Fatalf("tool_result block type = %T", blocks2[0])
	}
	if trBlock["type"] != "tool_result" {
		t.Errorf("tool_result type = %v", trBlock["type"])
	}
	if trBlock["tool_use_id"] != "toolu_abc" {
		t.Errorf("tool_use_id = %v", trBlock["tool_use_id"])
	}
}

func TestSendMessage_ToolResultWithError(t *testing.T) {
	var capturedReq messagesRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg_err",
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "text", "text": "Tool failed."}},
			"model":       "anthropic/claude-sonnet-4-20250514",
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 5, "output_tokens": 5},
		})
	}))
	defer srv.Close()

	client, _ := NewMessagesClient(Config{APIKey: "key", BaseURL: srv.URL})

	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "anthropic/claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []Message{
			{
				Role: RoleUser,
				ToolResults: []ToolResult{{
					ToolUseID: "toolu_xyz",
					Content:   "connection timeout",
					IsError:   true,
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify is_error is set in the wire format.
	msg := capturedReq.Messages[0]
	blocks, ok := msg.Content.([]any)
	if !ok {
		t.Fatalf("content type = %T", msg.Content)
	}
	block := blocks[0].(map[string]any)
	if block["is_error"] != true {
		t.Errorf("is_error = %v, want true", block["is_error"])
	}
}

func TestSendMessage_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "max_tokens must be positive",
			},
		})
	}))
	defer srv.Close()

	client, _ := NewMessagesClient(Config{APIKey: "key", BaseURL: srv.URL})

	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "anthropic/claude-sonnet-4-20250514",
		MaxTokens: -1,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.Type != "invalid_request_error" {
		t.Errorf("Type = %q", apiErr.Type)
	}
	if apiErr.Message != "max_tokens must be positive" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

func TestSendMessage_APIErrorUnparseable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	client, _ := NewMessagesClient(Config{APIKey: "key", BaseURL: srv.URL})

	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.RawBody != "internal server error" {
		t.Errorf("RawBody = %q", apiErr.RawBody)
	}
}

func TestSendMessage_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never respond - the cancelled context should abort.
		select {}
	}))
	defer srv.Close()

	client, _ := NewMessagesClient(Config{APIKey: "key", BaseURL: srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := client.SendMessage(ctx, &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestSendMessage_OptionalParams(t *testing.T) {
	var capturedReq messagesRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg_opt",
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"model":       "test",
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	client, _ := NewMessagesClient(Config{APIKey: "key", BaseURL: srv.URL})

	temp := 0.7
	topP := 0.9
	_, err := client.SendMessage(context.Background(), &Request{
		Model:         "test",
		MaxTokens:     100,
		Messages:      []Message{{Role: RoleUser, Content: "Hi"}},
		Temperature:   &temp,
		TopP:          &topP,
		StopSequences: []string{"STOP", "END"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if capturedReq.Temperature == nil || *capturedReq.Temperature != 0.7 {
		t.Errorf("temperature = %v", capturedReq.Temperature)
	}
	if capturedReq.TopP == nil || *capturedReq.TopP != 0.9 {
		t.Errorf("top_p = %v", capturedReq.TopP)
	}
	if len(capturedReq.StopSequences) != 2 || capturedReq.StopSequences[0] != "STOP" {
		t.Errorf("stop_sequences = %v", capturedReq.StopSequences)
	}
}

func TestSendMessage_NoOptionalHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Title"); got != "" {
			t.Errorf("X-Title = %q, want empty", got)
		}
		if got := r.Header.Get("HTTP-Referer"); got != "" {
			t.Errorf("HTTP-Referer = %q, want empty", got)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg_nh",
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"model":       "test",
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	client, _ := NewMessagesClient(Config{APIKey: "key", BaseURL: srv.URL})
	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSendMessage_ToolNilParameters(t *testing.T) {
	var capturedReq messagesRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg_tn",
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"model":       "test",
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	client, _ := NewMessagesClient(Config{APIKey: "key", BaseURL: srv.URL})

	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
		Tools: []Tool{{
			Name:        "no_params_tool",
			Description: "A tool with no parameters",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// When Parameters is nil, it should default to {"type": "object"}.
	if capturedReq.Tools[0].InputSchema["type"] != "object" {
		t.Errorf("input_schema = %v, want {type: object}", capturedReq.Tools[0].InputSchema)
	}
}

func TestSendMessage_MultipleTextBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_multi",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "First part."},
				{"type": "text", "text": "Second part."},
			},
			"model":       "test",
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	client, _ := NewMessagesClient(Config{APIKey: "key", BaseURL: srv.URL})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Content != "First part.\nSecond part." {
		t.Errorf("Content = %q, want joined with newline", resp.Content)
	}
}

func TestAPIError_ErrorString(t *testing.T) {
	t.Run("structured error", func(t *testing.T) {
		e := &APIError{StatusCode: 400, Type: "invalid_request_error", Message: "bad request"}
		want := "llm: API error 400 (invalid_request_error): bad request"
		if got := e.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("raw body error", func(t *testing.T) {
		e := &APIError{StatusCode: 500, RawBody: "oops"}
		want := "llm: API error 500: oops"
		if got := e.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
}

func TestConvertMessage_SimpleText(t *testing.T) {
	msg := Message{Role: RoleUser, Content: "hello"}
	wire := convertMessage(msg)

	if wire.Role != "user" {
		t.Errorf("role = %q", wire.Role)
	}
	if s, ok := wire.Content.(string); !ok || s != "hello" {
		t.Errorf("content = %v (%T)", wire.Content, wire.Content)
	}
}

func TestConvertMessage_AssistantWithToolCalls(t *testing.T) {
	msg := Message{
		Role:    RoleAssistant,
		Content: "Thinking...",
		ToolCalls: []ToolCall{{
			ID:    "tc_1",
			Name:  "search",
			Input: map[string]any{"q": "test"},
		}},
	}
	wire := convertMessage(msg)

	blocks, ok := wire.Content.([]contentBlock)
	if !ok {
		t.Fatalf("content type = %T, want []contentBlock", wire.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks len = %d, want 2", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "Thinking..." {
		t.Errorf("block[0] = %+v", blocks[0])
	}
	if blocks[1].Type != "tool_use" || blocks[1].Name != "search" {
		t.Errorf("block[1] = %+v", blocks[1])
	}
}

func TestConvertMessage_AssistantToolCallsNoText(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		ToolCalls: []ToolCall{{
			ID:    "tc_1",
			Name:  "search",
			Input: map[string]any{"q": "test"},
		}},
	}
	wire := convertMessage(msg)

	blocks, ok := wire.Content.([]contentBlock)
	if !ok {
		t.Fatalf("content type = %T, want []contentBlock", wire.Content)
	}
	// No text block since Content is empty.
	if len(blocks) != 1 {
		t.Fatalf("blocks len = %d, want 1", len(blocks))
	}
	if blocks[0].Type != "tool_use" {
		t.Errorf("block[0].Type = %q", blocks[0].Type)
	}
}

// --- CompletionsClient Tests ---

func TestNewCompletionsClient(t *testing.T) {
	t.Run("missing API key", func(t *testing.T) {
		_, err := NewCompletionsClient(Config{})
		if err != ErrMissingAPIKey {
			t.Fatalf("got %v, want ErrMissingAPIKey", err)
		}
	})

	t.Run("defaults applied", func(t *testing.T) {
		c, err := NewCompletionsClient(Config{APIKey: "test-key"})
		if err != nil {
			t.Fatal(err)
		}
		if c.config.BaseURL != "https://openrouter.ai/api/v1" {
			t.Errorf("BaseURL = %q, want default", c.config.BaseURL)
		}
	})
}

func TestCompletionsClient_ImplementsProvider(t *testing.T) {
	var _ Provider = (*CompletionsClient)(nil)
}

func TestCompletions_SimpleText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Title"); got != "TestApp" {
			t.Errorf("X-Title = %q", got)
		}

		body, _ := io.ReadAll(r.Body)
		var req completionsRequest
		json.Unmarshal(body, &req)

		if req.Model != "openai/gpt-4" {
			t.Errorf("model = %q", req.Model)
		}
		// System prompt should be the first message.
		if len(req.Messages) != 2 {
			t.Fatalf("messages len = %d, want 2 (system + user)", len(req.Messages))
		}
		if req.Messages[0].Role != "system" || req.Messages[0].Content != "You are helpful." {
			t.Errorf("system msg = %+v", req.Messages[0])
		}
		if req.Messages[1].Role != "user" || req.Messages[1].Content != "Hello" {
			t.Errorf("user msg = %+v", req.Messages[1])
		}

		fr := "stop"
		content := "Hello! How can I help?"
		json.NewEncoder(w).Encode(completionsResponse{
			ID:    "chatcmpl-123",
			Model: "openai/gpt-4",
			Choices: []completionsChoice{{
				Index:        0,
				FinishReason: &fr,
				Message:      completionsRespMsg{Role: "assistant", Content: &content},
			}},
			Usage: &completionsUsage{PromptTokens: 15, CompletionTokens: 10, TotalTokens: 25},
		})
	}))
	defer srv.Close()

	client, _ := NewCompletionsClient(Config{
		APIKey:   "test-key",
		BaseURL:  srv.URL,
		AppTitle: "TestApp",
	})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "openai/gpt-4",
		MaxTokens: 1024,
		System:    "You are helpful.",
		Messages:  []Message{{Role: RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.ID != "chatcmpl-123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Content != "Hello! How can I help?" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q, want end_turn", resp.StopReason)
	}
	if resp.Usage.InputTokens != 15 || resp.Usage.OutputTokens != 10 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

func TestCompletions_ToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req completionsRequest
		json.Unmarshal(body, &req)

		// Verify tools sent as function type.
		if len(req.Tools) != 1 {
			t.Fatalf("tools len = %d", len(req.Tools))
		}
		if req.Tools[0].Type != "function" {
			t.Errorf("tool type = %q", req.Tools[0].Type)
		}
		if req.Tools[0].Function.Name != "get_weather" {
			t.Errorf("tool name = %q", req.Tools[0].Function.Name)
		}

		fr := "tool_calls"
		content := "Let me check the weather."
		json.NewEncoder(w).Encode(completionsResponse{
			ID:    "chatcmpl-456",
			Model: "openai/gpt-4",
			Choices: []completionsChoice{{
				Index:        0,
				FinishReason: &fr,
				Message: completionsRespMsg{
					Role:    "assistant",
					Content: &content,
					ToolCalls: []completionsToolCall{{
						ID:   "call_abc",
						Type: "function",
						Function: completionsFnCall{
							Name:      "get_weather",
							Arguments: `{"city":"London"}`,
						},
					}},
				},
			}},
			Usage: &completionsUsage{PromptTokens: 20, CompletionTokens: 15, TotalTokens: 35},
		})
	}))
	defer srv.Close()

	client, _ := NewCompletionsClient(Config{APIKey: "key", BaseURL: srv.URL})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "openai/gpt-4",
		MaxTokens: 1024,
		Messages:  []Message{{Role: RoleUser, Content: "What's the weather?"}},
		Tools: []Tool{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.StopReason != StopReasonToolUse {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" || tc.Name != "get_weather" {
		t.Errorf("ToolCall = %+v", tc)
	}
	if tc.Input["city"] != "London" {
		t.Errorf("ToolCall input = %v", tc.Input)
	}
}

func TestCompletions_ToolResultConversion(t *testing.T) {
	var capturedReq completionsRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		fr := "stop"
		content := "It's sunny."
		json.NewEncoder(w).Encode(completionsResponse{
			ID:    "chatcmpl-789",
			Model: "openai/gpt-4",
			Choices: []completionsChoice{{
				Index:        0,
				FinishReason: &fr,
				Message:      completionsRespMsg{Role: "assistant", Content: &content},
			}},
			Usage: &completionsUsage{PromptTokens: 30, CompletionTokens: 5, TotalTokens: 35},
		})
	}))
	defer srv.Close()

	client, _ := NewCompletionsClient(Config{APIKey: "key", BaseURL: srv.URL})

	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "openai/gpt-4",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: RoleUser, Content: "What's the weather?"},
			{
				Role:    RoleAssistant,
				Content: "Let me check.",
				ToolCalls: []ToolCall{{
					ID:    "call_abc",
					Name:  "get_weather",
					Input: map[string]any{"city": "London"},
				}},
			},
			{
				Role: RoleUser,
				ToolResults: []ToolResult{{
					ToolUseID: "call_abc",
					Content:   `{"temp": 22}`,
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wire format: user, assistant (with tool_calls), tool (with tool_call_id).
	if len(capturedReq.Messages) != 3 {
		t.Fatalf("wire messages len = %d, want 3", len(capturedReq.Messages))
	}

	// First: user text.
	if capturedReq.Messages[0].Role != "user" {
		t.Errorf("msg[0] role = %q", capturedReq.Messages[0].Role)
	}

	// Second: assistant with tool_calls.
	msg1 := capturedReq.Messages[1]
	if msg1.Role != "assistant" {
		t.Errorf("msg[1] role = %q", msg1.Role)
	}
	if len(msg1.ToolCalls) != 1 {
		t.Fatalf("msg[1] tool_calls len = %d", len(msg1.ToolCalls))
	}
	if msg1.ToolCalls[0].ID != "call_abc" {
		t.Errorf("tool_call id = %q", msg1.ToolCalls[0].ID)
	}
	if msg1.ToolCalls[0].Type != "function" {
		t.Errorf("tool_call type = %q", msg1.ToolCalls[0].Type)
	}
	if msg1.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("tool_call fn name = %q", msg1.ToolCalls[0].Function.Name)
	}

	// Third: tool result.
	msg2 := capturedReq.Messages[2]
	if msg2.Role != "tool" {
		t.Errorf("msg[2] role = %q, want tool", msg2.Role)
	}
	if msg2.ToolCallID != "call_abc" {
		t.Errorf("msg[2] tool_call_id = %q", msg2.ToolCallID)
	}
	if msg2.Content != `{"temp": 22}` {
		t.Errorf("msg[2] content = %q", msg2.Content)
	}
}

func TestCompletions_MultipleToolResults(t *testing.T) {
	var capturedReq completionsRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		fr := "stop"
		content := "ok"
		json.NewEncoder(w).Encode(completionsResponse{
			ID:    "chatcmpl-multi",
			Model: "test",
			Choices: []completionsChoice{{
				Index: 0, FinishReason: &fr,
				Message: completionsRespMsg{Role: "assistant", Content: &content},
			}},
		})
	}))
	defer srv.Close()

	client, _ := NewCompletionsClient(Config{APIKey: "key", BaseURL: srv.URL})

	// Two tool results in one Message should become two separate wire messages.
	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages: []Message{{
			Role: RoleUser,
			ToolResults: []ToolResult{
				{ToolUseID: "call_1", Content: "result1"},
				{ToolUseID: "call_2", Content: "result2"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(capturedReq.Messages) != 2 {
		t.Fatalf("wire messages = %d, want 2", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != "tool" || capturedReq.Messages[0].ToolCallID != "call_1" {
		t.Errorf("msg[0] = %+v", capturedReq.Messages[0])
	}
	if capturedReq.Messages[1].Role != "tool" || capturedReq.Messages[1].ToolCallID != "call_2" {
		t.Errorf("msg[1] = %+v", capturedReq.Messages[1])
	}
}

func TestCompletions_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    429,
				"message": "Rate limit exceeded",
			},
		})
	}))
	defer srv.Close()

	client, _ := NewCompletionsClient(Config{APIKey: "key", BaseURL: srv.URL})

	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 429 {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.Message != "Rate limit exceeded" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

func TestCompletions_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(completionsResponse{
			ID:      "chatcmpl-empty",
			Model:   "test",
			Choices: []completionsChoice{},
		})
	}))
	defer srv.Close()

	client, _ := NewCompletionsClient(Config{APIKey: "key", BaseURL: srv.URL})

	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != ErrNoChoices {
		t.Fatalf("got %v, want ErrNoChoices", err)
	}
}

func TestCompletions_NullContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fr := "tool_calls"
		json.NewEncoder(w).Encode(completionsResponse{
			ID:    "chatcmpl-null",
			Model: "test",
			Choices: []completionsChoice{{
				Index:        0,
				FinishReason: &fr,
				Message: completionsRespMsg{
					Role:    "assistant",
					Content: nil,
					ToolCalls: []completionsToolCall{{
						ID:       "call_1",
						Type:     "function",
						Function: completionsFnCall{Name: "do_thing", Arguments: "{}"},
					}},
				},
			}},
		})
	}))
	defer srv.Close()

	client, _ := NewCompletionsClient(Config{APIKey: "key", BaseURL: srv.URL})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Content should be empty string when null.
	if resp.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d", len(resp.ToolCalls))
	}
}

func TestCompletions_FinishReasonMapping(t *testing.T) {
	tests := []struct {
		finish string
		want   StopReason
	}{
		{"stop", StopReasonEndTurn},
		{"length", StopReasonMaxTokens},
		{"tool_calls", StopReasonToolUse},
		{"content_filter", StopReason("content_filter")},
	}

	for _, tt := range tests {
		t.Run(tt.finish, func(t *testing.T) {
			got := finishReasonToStopReason(tt.finish)
			if got != tt.want {
				t.Errorf("finishReasonToStopReason(%q) = %q, want %q", tt.finish, got, tt.want)
			}
		})
	}
}

func TestCompletions_OptionalParams(t *testing.T) {
	var capturedReq completionsRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		fr := "stop"
		content := "ok"
		json.NewEncoder(w).Encode(completionsResponse{
			ID:    "chatcmpl-opt",
			Model: "test",
			Choices: []completionsChoice{{
				Index: 0, FinishReason: &fr,
				Message: completionsRespMsg{Role: "assistant", Content: &content},
			}},
		})
	}))
	defer srv.Close()

	client, _ := NewCompletionsClient(Config{APIKey: "key", BaseURL: srv.URL})

	temp := 0.5
	topP := 0.8
	_, err := client.SendMessage(context.Background(), &Request{
		Model:         "test",
		MaxTokens:     200,
		Messages:      []Message{{Role: RoleUser, Content: "Hi"}},
		Temperature:   &temp,
		TopP:          &topP,
		StopSequences: []string{"END"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if capturedReq.Temperature == nil || *capturedReq.Temperature != 0.5 {
		t.Errorf("temperature = %v", capturedReq.Temperature)
	}
	if capturedReq.TopP == nil || *capturedReq.TopP != 0.8 {
		t.Errorf("top_p = %v", capturedReq.TopP)
	}
	if capturedReq.MaxTokens == nil || *capturedReq.MaxTokens != 200 {
		t.Errorf("max_tokens = %v", capturedReq.MaxTokens)
	}
	// StopSequences maps to "stop" field.
	if len(capturedReq.Stop) != 1 || capturedReq.Stop[0] != "END" {
		t.Errorf("stop = %v", capturedReq.Stop)
	}
}

func TestCompletions_MaxTokensZero(t *testing.T) {
	var capturedReq completionsRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		fr := "stop"
		content := "ok"
		json.NewEncoder(w).Encode(completionsResponse{
			ID:    "chatcmpl-zero",
			Model: "test",
			Choices: []completionsChoice{{
				Index: 0, FinishReason: &fr,
				Message: completionsRespMsg{Role: "assistant", Content: &content},
			}},
		})
	}))
	defer srv.Close()

	client, _ := NewCompletionsClient(Config{APIKey: "key", BaseURL: srv.URL})

	_, err := client.SendMessage(context.Background(), &Request{
		Model:    "test",
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// MaxTokens 0 should be omitted.
	if capturedReq.MaxTokens != nil {
		t.Errorf("max_tokens = %v, want nil", capturedReq.MaxTokens)
	}
}

func TestCompletions_NoSystemPrompt(t *testing.T) {
	var capturedReq completionsRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		fr := "stop"
		content := "ok"
		json.NewEncoder(w).Encode(completionsResponse{
			ID:    "chatcmpl-ns",
			Model: "test",
			Choices: []completionsChoice{{
				Index: 0, FinishReason: &fr,
				Message: completionsRespMsg{Role: "assistant", Content: &content},
			}},
		})
	}))
	defer srv.Close()

	client, _ := NewCompletionsClient(Config{APIKey: "key", BaseURL: srv.URL})

	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// No system message prepended.
	if len(capturedReq.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != "user" {
		t.Errorf("msg[0] role = %q", capturedReq.Messages[0].Role)
	}
}

// --- ResponsesClient Tests ---

func TestNewResponsesClient(t *testing.T) {
	t.Run("missing API key", func(t *testing.T) {
		_, err := NewResponsesClient(Config{})
		if err != ErrMissingAPIKey {
			t.Fatalf("got %v, want ErrMissingAPIKey", err)
		}
	})

	t.Run("defaults applied", func(t *testing.T) {
		c, err := NewResponsesClient(Config{APIKey: "test-key"})
		if err != nil {
			t.Fatal(err)
		}
		if c.config.BaseURL != "https://openrouter.ai/api/v1" {
			t.Errorf("BaseURL = %q, want default", c.config.BaseURL)
		}
	})
}

func TestResponsesClient_ImplementsProvider(t *testing.T) {
	var _ Provider = (*ResponsesClient)(nil)
}

func TestResponses_SimpleText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path = %s, want /responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Title"); got != "TestApp" {
			t.Errorf("X-Title = %q", got)
		}
		if got := r.Header.Get("HTTP-Referer"); got != "http://test" {
			t.Errorf("HTTP-Referer = %q", got)
		}

		body, _ := io.ReadAll(r.Body)
		var req responsesRequest
		json.Unmarshal(body, &req)

		if req.Model != "openai/gpt-4o" {
			t.Errorf("model = %q", req.Model)
		}
		if req.Instructions != "You are helpful." {
			t.Errorf("instructions = %q", req.Instructions)
		}
		if len(req.Input) != 1 {
			t.Fatalf("input len = %d", len(req.Input))
		}
		if req.Input[0].Type != "message" || req.Input[0].Role != "user" {
			t.Errorf("input[0] = %+v", req.Input[0])
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":          "resp_123",
			"object":      "response",
			"model":       "openai/gpt-4o",
			"status":      "completed",
			"output_text": "Hello! How can I help?",
			"output": []map[string]any{
				{
					"type": "message",
					"id":   "msg_1",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": "Hello! How can I help?"},
					},
				},
			},
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 8,
				"total_tokens":  18,
			},
		})
	}))
	defer srv.Close()

	client, _ := NewResponsesClient(Config{
		APIKey:      "test-key",
		BaseURL:     srv.URL,
		HTTPReferer: "http://test",
		AppTitle:    "TestApp",
	})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "openai/gpt-4o",
		MaxTokens: 1024,
		System:    "You are helpful.",
		Messages:  []Message{{Role: RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.ID != "resp_123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Content != "Hello! How can I help?" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if resp.Model != "openai/gpt-4o" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 8 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("ToolCalls = %v, want empty", resp.ToolCalls)
	}
}

func TestResponses_ToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req responsesRequest
		json.Unmarshal(body, &req)

		// Verify tools.
		if len(req.Tools) != 1 {
			t.Fatalf("tools len = %d", len(req.Tools))
		}
		if req.Tools[0].Type != "function" {
			t.Errorf("tool type = %q", req.Tools[0].Type)
		}
		if req.Tools[0].Name != "get_weather" {
			t.Errorf("tool name = %q", req.Tools[0].Name)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":          "resp_456",
			"object":      "response",
			"model":       "openai/gpt-4o",
			"status":      "completed",
			"output_text": "",
			"output": []map[string]any{
				{
					"type":      "function_call",
					"id":        "fc_1",
					"name":      "get_weather",
					"arguments": `{"city":"London"}`,
					"call_id":   "call_abc",
					"status":    "completed",
				},
			},
			"usage": map[string]any{
				"input_tokens":  20,
				"output_tokens": 15,
				"total_tokens":  35,
			},
		})
	}))
	defer srv.Close()

	client, _ := NewResponsesClient(Config{APIKey: "key", BaseURL: srv.URL})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "openai/gpt-4o",
		MaxTokens: 1024,
		Messages:  []Message{{Role: RoleUser, Content: "What's the weather in London?"}},
		Tools: []Tool{{
			Name:        "get_weather",
			Description: "Get the current weather",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
				"required":   []string{"city"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.StopReason != StopReasonToolUse {
		t.Errorf("StopReason = %q, want tool_use", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q", tc.Name)
	}
	if tc.Input["city"] != "London" {
		t.Errorf("ToolCall.Input = %v", tc.Input)
	}
}

func TestResponses_ToolResultConversion(t *testing.T) {
	var capturedReq responsesRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		json.NewEncoder(w).Encode(map[string]any{
			"id":          "resp_789",
			"object":      "response",
			"model":       "openai/gpt-4o",
			"status":      "completed",
			"output_text": "It's sunny in London.",
			"output": []map[string]any{
				{
					"type": "message",
					"id":   "msg_1",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": "It's sunny in London."},
					},
				},
			},
			"usage": map[string]any{
				"input_tokens": 30, "output_tokens": 10, "total_tokens": 40,
			},
		})
	}))
	defer srv.Close()

	client, _ := NewResponsesClient(Config{APIKey: "key", BaseURL: srv.URL})

	// Full tool-use conversation:
	// 1. user asks
	// 2. assistant calls tool
	// 3. tool result
	_, err := client.SendMessage(context.Background(), &Request{
		Model:     "openai/gpt-4o",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: RoleUser, Content: "What's the weather in London?"},
			{
				Role:    RoleAssistant,
				Content: "Let me check.",
				ToolCalls: []ToolCall{{
					ID:    "call_abc",
					Name:  "get_weather",
					Input: map[string]any{"city": "London"},
				}},
			},
			{
				Role: RoleUser,
				ToolResults: []ToolResult{{
					ToolUseID: "call_abc",
					Content:   `{"temp": 22, "condition": "sunny"}`,
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wire format: message (user), message (assistant text), function_call, function_call_output.
	if len(capturedReq.Input) != 4 {
		t.Fatalf("input len = %d, want 4", len(capturedReq.Input))
	}

	// First: user message.
	if capturedReq.Input[0].Type != "message" || capturedReq.Input[0].Role != "user" {
		t.Errorf("input[0] = %+v", capturedReq.Input[0])
	}

	// Second: assistant text message.
	if capturedReq.Input[1].Type != "message" || capturedReq.Input[1].Role != "assistant" {
		t.Errorf("input[1] = %+v", capturedReq.Input[1])
	}

	// Third: function_call.
	if capturedReq.Input[2].Type != "function_call" {
		t.Errorf("input[2] type = %q", capturedReq.Input[2].Type)
	}
	if capturedReq.Input[2].CallID != "call_abc" {
		t.Errorf("input[2] call_id = %q", capturedReq.Input[2].CallID)
	}
	if capturedReq.Input[2].Name != "get_weather" {
		t.Errorf("input[2] name = %q", capturedReq.Input[2].Name)
	}

	// Fourth: function_call_output.
	if capturedReq.Input[3].Type != "function_call_output" {
		t.Errorf("input[3] type = %q", capturedReq.Input[3].Type)
	}
	if capturedReq.Input[3].CallID != "call_abc" {
		t.Errorf("input[3] call_id = %q", capturedReq.Input[3].CallID)
	}
	if capturedReq.Input[3].Output != `{"temp": 22, "condition": "sunny"}` {
		t.Errorf("input[3] output = %q", capturedReq.Input[3].Output)
	}
}

func TestResponses_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "invalid_request",
				"message": "model is required",
			},
		})
	}))
	defer srv.Close()

	client, _ := NewResponsesClient(Config{APIKey: "key", BaseURL: srv.URL})

	_, err := client.SendMessage(context.Background(), &Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.Message != "model is required" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

func TestResponses_IncompleteStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "resp_inc",
			"object":      "response",
			"model":       "test",
			"status":      "incomplete",
			"output_text": "partial response",
			"output":      []map[string]any{},
			"usage": map[string]any{
				"input_tokens": 5, "output_tokens": 50, "total_tokens": 55,
			},
		})
	}))
	defer srv.Close()

	client, _ := NewResponsesClient(Config{APIKey: "key", BaseURL: srv.URL})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 50,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.StopReason != StopReasonMaxTokens {
		t.Errorf("StopReason = %q, want max_tokens", resp.StopReason)
	}
}

func TestResponses_OptionalParams(t *testing.T) {
	var capturedReq responsesRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		json.NewEncoder(w).Encode(map[string]any{
			"id":          "resp_opt",
			"object":      "response",
			"model":       "test",
			"status":      "completed",
			"output_text": "ok",
			"output":      []map[string]any{},
			"usage": map[string]any{
				"input_tokens": 1, "output_tokens": 1, "total_tokens": 2,
			},
		})
	}))
	defer srv.Close()

	client, _ := NewResponsesClient(Config{APIKey: "key", BaseURL: srv.URL})

	temp := 0.7
	topP := 0.9
	_, err := client.SendMessage(context.Background(), &Request{
		Model:       "test",
		MaxTokens:   200,
		Messages:    []Message{{Role: RoleUser, Content: "Hi"}},
		Temperature: &temp,
		TopP:        &topP,
	})
	if err != nil {
		t.Fatal(err)
	}

	if capturedReq.Temperature == nil || *capturedReq.Temperature != 0.7 {
		t.Errorf("temperature = %v", capturedReq.Temperature)
	}
	if capturedReq.TopP == nil || *capturedReq.TopP != 0.9 {
		t.Errorf("top_p = %v", capturedReq.TopP)
	}
	if capturedReq.MaxOutputTokens == nil || *capturedReq.MaxOutputTokens != 200 {
		t.Errorf("max_output_tokens = %v", capturedReq.MaxOutputTokens)
	}
}

func TestResponses_MultipleToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "resp_multi",
			"object":      "response",
			"model":       "test",
			"status":      "completed",
			"output_text": "",
			"output": []map[string]any{
				{
					"type":      "function_call",
					"id":        "fc_1",
					"name":      "get_weather",
					"arguments": `{"city":"London"}`,
					"call_id":   "call_1",
				},
				{
					"type":      "function_call",
					"id":        "fc_2",
					"name":      "get_weather",
					"arguments": `{"city":"Paris"}`,
					"call_id":   "call_2",
				},
			},
			"usage": map[string]any{
				"input_tokens": 10, "output_tokens": 20, "total_tokens": 30,
			},
		})
	}))
	defer srv.Close()

	client, _ := NewResponsesClient(Config{APIKey: "key", BaseURL: srv.URL})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 1024,
		Messages:  []Message{{Role: RoleUser, Content: "Weather in London and Paris?"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.StopReason != StopReasonToolUse {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("ToolCalls len = %d, want 2", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("ToolCalls[0] = %+v", resp.ToolCalls[0])
	}
	if resp.ToolCalls[1].ID != "call_2" || resp.ToolCalls[1].Name != "get_weather" {
		t.Errorf("ToolCalls[1] = %+v", resp.ToolCalls[1])
	}
}

func TestResponses_TextAndToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "resp_both",
			"object":      "response",
			"model":       "test",
			"status":      "completed",
			"output_text": "Let me check the weather.",
			"output": []map[string]any{
				{
					"type": "message",
					"id":   "msg_1",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": "Let me check the weather."},
					},
				},
				{
					"type":      "function_call",
					"id":        "fc_1",
					"name":      "get_weather",
					"arguments": `{"city":"London"}`,
					"call_id":   "call_1",
				},
			},
			"usage": map[string]any{
				"input_tokens": 10, "output_tokens": 15, "total_tokens": 25,
			},
		})
	}))
	defer srv.Close()

	client, _ := NewResponsesClient(Config{APIKey: "key", BaseURL: srv.URL})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 1024,
		Messages:  []Message{{Role: RoleUser, Content: "Weather?"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Content != "Let me check the weather." {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.StopReason != StopReasonToolUse {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d", len(resp.ToolCalls))
	}
}

func TestResponses_NoUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "resp_nousage",
			"object":      "response",
			"model":       "test",
			"status":      "completed",
			"output_text": "ok",
			"output":      []map[string]any{},
		})
	}))
	defer srv.Close()

	client, _ := NewResponsesClient(Config{APIKey: "key", BaseURL: srv.URL})

	resp, err := client.SendMessage(context.Background(), &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 {
		t.Errorf("Usage = %+v, want zero", resp.Usage)
	}
}

func TestResponses_StatusMapping(t *testing.T) {
	tests := []struct {
		status   string
		toolCall bool
		want     StopReason
	}{
		{"completed", false, StopReasonEndTurn},
		{"completed", true, StopReasonToolUse},
		{"incomplete", false, StopReasonMaxTokens},
		{"failed", false, StopReason("failed")},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := responsesStatusToStopReason(tt.status, tt.toolCall)
			if got != tt.want {
				t.Errorf("responsesStatusToStopReason(%q, %v) = %q, want %q", tt.status, tt.toolCall, got, tt.want)
			}
		})
	}
}

func TestResponses_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {}
	}))
	defer srv.Close()

	client, _ := NewResponsesClient(Config{APIKey: "key", BaseURL: srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.SendMessage(ctx, &Request{
		Model:     "test",
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
