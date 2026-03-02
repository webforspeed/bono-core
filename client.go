package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client handles communication with the chat completions API.
type Client struct {
	config     Config
	httpClient *http.Client
	logPath    string

	modelLimitsMu sync.RWMutex
	modelLimits   map[string]modelLimitCacheEntry

	lastUsage   *ResponseUsage
	middlewares []MessageMiddleware
}

// APILogEntry represents a logged API call.
type APILogEntry struct {
	Timestamp       string            `json:"ts"`
	RequestPayload  any               `json:"request_payload,omitempty"`
	RequestURL      string            `json:"request_url,omitempty"`
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	ResponsePayload any               `json:"response_payload,omitempty"`
	ResponseUsage   *ResponseUsage    `json:"response_usage,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	StatusCode      int               `json:"status_code,omitempty"`
	Error           string            `json:"error,omitempty"`
	DurationMs      int64             `json:"duration_ms"`
}

// ResponseUsage contains token usage plus computed context-window percentages.
type ResponseUsage struct {
	Model               string   `json:"model,omitempty"`
	Provider            string   `json:"provider,omitempty"`
	PromptTokens        *int64   `json:"prompt_tokens,omitempty"`
	CompletionTokens    *int64   `json:"completion_tokens,omitempty"`
	TotalTokens         *int64   `json:"total_tokens,omitempty"`
	ContextLimit        *int64   `json:"context_limit,omitempty"`
	MaxPromptTokens     *int64   `json:"max_prompt_tokens,omitempty"`
	MaxCompletionTokens *int64   `json:"max_completion_tokens,omitempty"`
	TurnUsagePct        *float64 `json:"turn_usage_pct,omitempty"`
	PromptUsagePct      *float64 `json:"prompt_usage_pct,omitempty"`
	CompletionUsagePct  *float64 `json:"completion_usage_pct,omitempty"`
	LimitProvider       string   `json:"limit_provider,omitempty"`
	ProviderMatched     bool     `json:"provider_matched"`
	LimitSource         string   `json:"limit_source,omitempty"`
	LimitFetchedAt      string   `json:"limit_fetched_at,omitempty"`
}

type modelLimitCacheEntry struct {
	Model     string
	FetchedAt time.Time
	Endpoints []modelEndpointLimit
}

type modelEndpointLimit struct {
	ProviderName        string
	ContextLength       int64
	MaxPromptTokens     int64
	MaxCompletionTokens int64
	IsDefault           bool
}

// NewClient creates a new API client with the given configuration.
func NewClient(config Config) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	c := &Client{
		config:      config,
		httpClient:  &http.Client{Timeout: config.HTTPTimeout},
		logPath:     config.APILogPath,
		modelLimits: make(map[string]modelLimitCacheEntry),
	}

	// Warm model limits once at startup so response usage can be computed cheaply per request.
	warmCtx, cancel := context.WithTimeout(context.Background(), modelLimitsWarmupTimeout(config.HTTPTimeout))
	defer cancel()
	_ = c.WarmModelUsageLimits(warmCtx, config.Model)

	return c, nil
}

// Use registers message middleware that runs before each API call.
func (c *Client) Use(mw ...MessageMiddleware) {
	c.middlewares = append(c.middlewares, mw...)
}

// LastUsage returns the ResponseUsage from the most recent API call, or nil.
func (c *Client) LastUsage() *ResponseUsage {
	return c.lastUsage
}

func (c *Client) applyMiddleware(messages []Message) []Message {
	for _, mw := range c.middlewares {
		messages = mw(messages)
	}
	return messages
}

func modelLimitsWarmupTimeout(httpTimeout time.Duration) time.Duration {
	const defaultTimeout = 10 * time.Second
	if httpTimeout > 0 && httpTimeout < defaultTimeout {
		return httpTimeout
	}
	return defaultTimeout
}

// WarmModelUsageLimits fetches and caches endpoint-specific limits for a model.
// Safe to call multiple times; cached models are no-ops.
func (c *Client) WarmModelUsageLimits(ctx context.Context, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("warm model limits: empty model")
	}

	if _, ok := c.getModelLimitEntry(model); ok {
		return nil
	}

	entry, err := c.fetchModelUsageLimits(ctx, model)
	if err != nil {
		return err
	}

	c.modelLimitsMu.Lock()
	c.modelLimits[model] = entry
	c.modelLimitsMu.Unlock()
	return nil
}

func (c *Client) fetchModelUsageLimits(ctx context.Context, model string) (modelLimitCacheEntry, error) {
	author, slug, ok := strings.Cut(model, "/")
	if !ok || author == "" || slug == "" {
		return modelLimitCacheEntry{}, fmt.Errorf("warm model limits: invalid model %q", model)
	}

	requestURL := fmt.Sprintf("%s/models/%s/%s/endpoints", c.config.BaseURL, url.PathEscape(author), url.PathEscape(slug))
	httpReq, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return modelLimitCacheEntry{}, fmt.Errorf("warm model limits: create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	httpReq.Header.Set("HTTP-Referer", "http://localhost")
	httpReq.Header.Set("X-Title", "Agent")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return modelLimitCacheEntry{}, fmt.Errorf("warm model limits: http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return modelLimitCacheEntry{}, fmt.Errorf("warm model limits: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return modelLimitCacheEntry{}, fmt.Errorf("warm model limits: status %d", resp.StatusCode)
	}

	payload, err := decodeJSONMap(body)
	if err != nil {
		return modelLimitCacheEntry{}, fmt.Errorf("warm model limits: decode json: %w", err)
	}

	data, ok := asMap(payload["data"])
	if !ok {
		return modelLimitCacheEntry{}, fmt.Errorf("warm model limits: missing data")
	}

	endpointItems, ok := asSlice(data["endpoints"])
	if !ok {
		return modelLimitCacheEntry{}, fmt.Errorf("warm model limits: missing endpoints")
	}

	limits := make([]modelEndpointLimit, 0, len(endpointItems))
	for _, item := range endpointItems {
		endpoint, ok := asMap(item)
		if !ok {
			continue
		}

		providerName, _ := asString(endpoint["provider_name"])
		contextLength, _ := asInt64(endpoint["context_length"])
		maxPromptTokens, _ := asInt64(endpoint["max_prompt_tokens"])
		maxCompletionTokens, _ := asInt64(endpoint["max_completion_tokens"])

		if contextLength <= 0 && maxPromptTokens <= 0 && maxCompletionTokens <= 0 {
			continue
		}

		limits = append(limits, modelEndpointLimit{
			ProviderName:        providerName,
			ContextLength:       contextLength,
			MaxPromptTokens:     maxPromptTokens,
			MaxCompletionTokens: maxCompletionTokens,
			IsDefault:           isDefaultEndpoint(endpoint["status"]),
		})
	}

	if len(limits) == 0 {
		return modelLimitCacheEntry{}, fmt.Errorf("warm model limits: no endpoint limits for model %q", model)
	}

	return modelLimitCacheEntry{
		Model:     model,
		FetchedAt: time.Now().UTC(),
		Endpoints: limits,
	}, nil
}

func (c *Client) getModelLimitEntry(model string) (modelLimitCacheEntry, bool) {
	c.modelLimitsMu.RLock()
	entry, ok := c.modelLimits[model]
	c.modelLimitsMu.RUnlock()
	return entry, ok
}

// logAPICall appends an API call entry to the JSONL log file.
func (c *Client) logAPICall(entry APILogEntry) {
	os.MkdirAll(filepath.Dir(c.logPath), 0755)
	f, err := os.OpenFile(c.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if data, err := json.Marshal(entry); err == nil {
		f.Write(data)
		f.WriteString("\n")
	}
}

func payloadForLog(body []byte) any {
	if len(body) == 0 {
		return nil
	}

	cloned := bytes.Clone(body)
	if json.Valid(cloned) {
		return json.RawMessage(cloned)
	}

	return string(cloned)
}

func headersForLog(headers http.Header, redactAuthorization bool) map[string]string {
	if len(headers) == 0 {
		return nil
	}

	out := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) == 0 {
			continue
		}

		value := strings.Join(values, ", ")
		if redactAuthorization && strings.EqualFold(key, "Authorization") {
			value = "Bearer [REDACTED]"
		}

		out[key] = value
	}

	return out
}

func decodeJSONMap(body []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asSlice(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func asString(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(s), true
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i, true
		}
		if f, err := n.Float64(); err == nil {
			return int64(f), true
		}
	case float64:
		return int64(n), true
	case float32:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case string:
		n = strings.TrimSpace(n)
		if n == "" {
			return 0, false
		}
		if i, err := strconv.ParseInt(n, 10, 64); err == nil {
			return i, true
		}
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return int64(f), true
		}
	}
	return 0, false
}

func isDefaultEndpoint(status any) bool {
	if status == nil {
		return false
	}
	if value, ok := asString(status); ok {
		return strings.EqualFold(value, "default")
	}
	if value, ok := asInt64(status); ok {
		return value == 0
	}
	return false
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func providerMatches(a, b string) bool {
	a = normalizeProviderName(a)
	b = normalizeProviderName(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}

func selectEndpointLimit(entry modelLimitCacheEntry, provider string) (modelEndpointLimit, bool, bool) {
	if len(entry.Endpoints) == 0 {
		return modelEndpointLimit{}, false, false
	}

	provider = normalizeProviderName(provider)
	if provider != "" {
		matched := modelEndpointLimit{}
		hasMatched := false
		for _, endpoint := range entry.Endpoints {
			if !providerMatches(endpoint.ProviderName, provider) {
				continue
			}
			if !hasMatched || (endpoint.IsDefault && !matched.IsDefault) {
				matched = endpoint
				hasMatched = true
			}
		}
		if hasMatched {
			return matched, true, true
		}
	}

	fallback := entry.Endpoints[0]
	for _, endpoint := range entry.Endpoints[1:] {
		if endpoint.IsDefault && !fallback.IsDefault {
			fallback = endpoint
		}
	}
	return fallback, true, false
}

func int64Ptr(v int64) *int64 {
	value := v
	return &value
}

func percentage(numerator *int64, denominator *int64) *float64 {
	if numerator == nil || denominator == nil || *denominator <= 0 {
		return nil
	}
	pct := (float64(*numerator) / float64(*denominator)) * 100
	pct = math.Round(pct*100) / 100
	return &pct
}

func firstPositive(a *int64, b *int64) *int64 {
	if a != nil && *a > 0 {
		return a
	}
	if b != nil && *b > 0 {
		return b
	}
	return nil
}

func (c *Client) buildResponseUsage(responseBody []byte, requestedModel string) *ResponseUsage {
	payload, err := decodeJSONMap(responseBody)
	if err != nil {
		return nil
	}

	usageMap, ok := asMap(payload["usage"])
	if !ok {
		return nil
	}

	promptTokens, hasPrompt := asInt64(usageMap["prompt_tokens"])
	completionTokens, hasCompletion := asInt64(usageMap["completion_tokens"])
	totalTokens, hasTotal := asInt64(usageMap["total_tokens"])
	if !hasPrompt && !hasCompletion && !hasTotal {
		return nil
	}

	model, _ := asString(payload["model"])
	if model == "" {
		model = requestedModel
	}

	provider, _ := asString(payload["provider"])

	result := &ResponseUsage{
		Model:           model,
		Provider:        provider,
		ProviderMatched: false,
	}
	if hasPrompt {
		result.PromptTokens = int64Ptr(promptTokens)
	}
	if hasCompletion {
		result.CompletionTokens = int64Ptr(completionTokens)
	}
	if hasTotal {
		result.TotalTokens = int64Ptr(totalTokens)
	}

	entry, ok := c.getModelLimitEntry(model)
	if !ok && requestedModel != "" && requestedModel != model {
		entry, ok = c.getModelLimitEntry(requestedModel)
	}
	if !ok {
		return result
	}

	endpoint, ok, matched := selectEndpointLimit(entry, provider)
	if !ok {
		return result
	}

	if endpoint.ContextLength > 0 {
		result.ContextLimit = int64Ptr(endpoint.ContextLength)
	}
	if endpoint.MaxPromptTokens > 0 {
		result.MaxPromptTokens = int64Ptr(endpoint.MaxPromptTokens)
	}
	if endpoint.MaxCompletionTokens > 0 {
		result.MaxCompletionTokens = int64Ptr(endpoint.MaxCompletionTokens)
	}

	result.TurnUsagePct = percentage(result.TotalTokens, result.ContextLimit)
	result.PromptUsagePct = percentage(result.PromptTokens, firstPositive(result.MaxPromptTokens, result.ContextLimit))
	result.CompletionUsagePct = percentage(result.CompletionTokens, result.MaxCompletionTokens)
	result.ProviderMatched = matched
	result.LimitProvider = endpoint.ProviderName
	result.LimitSource = "model_endpoint_cache"
	result.LimitFetchedAt = entry.FetchedAt.Format(time.RFC3339)

	return result
}

// ChatCompletion sends messages to the API without any tools.
// Use ChatCompletionWithTools to include tool definitions.
func (c *Client) ChatCompletion(ctx context.Context, messages []Message) (*Message, error) {
	return c.ChatCompletionWithTools(ctx, messages, nil)
}

// ChatCompletionWithTools sends messages with a custom tool set.
// Use this when you need to override the default tools (e.g., for subagents).
func (c *Client) ChatCompletionWithTools(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	start := time.Now()
	req := ChatRequest{
		Model:    c.config.Model,
		Messages: c.applyMiddleware(messages),
		Tools:    tools,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	requestURL := c.config.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "http://localhost")
	httpReq.Header.Set("X-Title", "Agent")

	requestPayload := payloadForLog(body)
	requestHeaders := headersForLog(httpReq.Header, true)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logAPICall(APILogEntry{
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
			RequestPayload: requestPayload,
			RequestURL:     requestURL,
			RequestHeaders: requestHeaders,
			Error:          err.Error(),
			DurationMs:     time.Since(start).Milliseconds(),
		})
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		c.logAPICall(APILogEntry{
			Timestamp:       time.Now().UTC().Format(time.RFC3339),
			RequestPayload:  requestPayload,
			RequestURL:      requestURL,
			RequestHeaders:  requestHeaders,
			StatusCode:      resp.StatusCode,
			ResponseHeaders: headersForLog(resp.Header, false),
			Error:           readErr.Error(),
			DurationMs:      time.Since(start).Milliseconds(),
		})
		return nil, fmt.Errorf("read response: %w", readErr)
	}

	responsePayload := payloadForLog(responseBody)
	responseHeaders := headersForLog(resp.Header, false)
	responseUsage := c.buildResponseUsage(responseBody, req.Model)
	if responseUsage != nil {
		c.lastUsage = responseUsage
	}

	if resp.StatusCode != http.StatusOK {
		apiErr := &APIError{StatusCode: resp.StatusCode, Body: string(responseBody)}
		c.logAPICall(APILogEntry{
			Timestamp:       time.Now().UTC().Format(time.RFC3339),
			RequestPayload:  requestPayload,
			RequestURL:      requestURL,
			RequestHeaders:  requestHeaders,
			StatusCode:      resp.StatusCode,
			ResponsePayload: responsePayload,
			ResponseUsage:   responseUsage,
			ResponseHeaders: responseHeaders,
			Error:           apiErr.Error(),
			DurationMs:      time.Since(start).Milliseconds(),
		})
		return nil, apiErr
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(responseBody, &chatResp); err != nil {
		c.logAPICall(APILogEntry{
			Timestamp:       time.Now().UTC().Format(time.RFC3339),
			RequestPayload:  requestPayload,
			RequestURL:      requestURL,
			RequestHeaders:  requestHeaders,
			StatusCode:      resp.StatusCode,
			ResponsePayload: responsePayload,
			ResponseUsage:   responseUsage,
			ResponseHeaders: responseHeaders,
			Error:           err.Error(),
			DurationMs:      time.Since(start).Milliseconds(),
		})
		return nil, fmt.Errorf("decode response: %w", err)
	}

	c.logAPICall(APILogEntry{
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		RequestPayload:  requestPayload,
		RequestURL:      requestURL,
		RequestHeaders:  requestHeaders,
		StatusCode:      resp.StatusCode,
		ResponsePayload: responsePayload,
		ResponseUsage:   responseUsage,
		ResponseHeaders: responseHeaders,
		DurationMs:      time.Since(start).Milliseconds(),
	})

	if len(chatResp.Choices) == 0 {
		return nil, ErrNoChoices
	}

	return &chatResp.Choices[0].Message, nil
}
