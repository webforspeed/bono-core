package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/webforspeed/bono-core/llm"
)

// Backend represents which web search backend to route to.
type Backend int

const (
	BackendAnswer Backend = iota
	BackendSearch
)

// --- Interfaces (vendor-agnostic) ---

// SearchResult represents a single web search result.
type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// WebSearcher performs web searches returning structured results.
// Implementations: OpenRouter web plugin, Tavily, Brave, SerpAPI, etc.
type WebSearcher interface {
	Search(ctx context.Context, query string) ([]SearchResult, error)
}

// WebAnswerer provides synthesized answers with web sources.
// Implementations: Perplexity sonar, Google AI, any LLM with web access, etc.
type WebAnswerer interface {
	Answer(ctx context.Context, query string) (answer string, sources []string, err error)
}

// WebFetcher fetches and processes URL content into readable text.
// Implementations: Perplexity sonar, Jina Reader, headless browser, etc.
type WebFetcher interface {
	Fetch(ctx context.Context, url string, question string) (content string, err error)
}

// QueryClassifier routes queries to the appropriate backend.
// Implementations: LLM classifier, keyword matcher, etc.
type QueryClassifier interface {
	Classify(ctx context.Context, query string) (Backend, error)
}

// --- WebService (composes interfaces) ---

// WebService owns the WebSearch and WebFetch tools.
// It delegates to pluggable backends for search, answer, fetch, and classification.
type WebService struct {
	searcher   WebSearcher
	answerer   WebAnswerer
	fetcher    WebFetcher
	classifier QueryClassifier
}

// NewWebService creates a WebService with OpenRouter-backed implementations.
func NewWebService(cfg WebConfig) (*WebService, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("web: API key is required")
	}
	if cfg.Model == "" {
		cfg.Model = "perplexity/sonar"
	}
	if cfg.SearchModel == "" {
		cfg.SearchModel = cfg.Model
	}
	if cfg.SearchEngine == "" {
		cfg.SearchEngine = "exa"
	}
	if cfg.MaxResults == 0 {
		cfg.MaxResults = 5
	}
	if cfg.ClassifierModel == "" {
		cfg.ClassifierModel = "openai/gpt-4o-mini"
	}

	transport := &capturingTransport{base: http.DefaultTransport}
	inner, err := llm.NewCompletionsClient(llm.Config{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		return nil, fmt.Errorf("web: create provider: %w", err)
	}

	var p llm.Provider = inner
	if cfg.APILogPath != "" {
		p = &loggingProvider{inner: inner, transport: transport, logPath: cfg.APILogPath}
	}

	return &WebService{
		searcher:   &openRouterSearcher{provider: p, model: cfg.SearchModel, engine: cfg.SearchEngine, maxResults: cfg.MaxResults},
		answerer:   &sonarAnswerer{provider: p, model: cfg.Model},
		fetcher:    &sonarFetcher{provider: p, model: cfg.Model},
		classifier: &llmClassifier{provider: p, model: cfg.ClassifierModel},
	}, nil
}

// loggingProvider wraps an llm.Provider and logs each call to a JSONL file.
// Reuses capturingTransport + writeLogEntry from client.go (same package).
type loggingProvider struct {
	inner     llm.Provider
	transport *capturingTransport
	logPath   string
}

func (lp *loggingProvider) SendMessage(ctx context.Context, req *llm.Request) (*llm.Response, error) {
	resp, err := lp.inner.SendMessage(ctx, req)
	if captured := lp.transport.lastCapture(); captured != nil {
		entry := APILogEntry{
			Timestamp:       time.Now().UTC().Format(time.RFC3339),
			RequestURL:      captured.RequestURL,
			RequestPayload:  payloadForLog(captured.RequestBody),
			RequestHeaders:  headersForLog(captured.RequestHeaders, true),
			StatusCode:      captured.StatusCode,
			ResponsePayload: payloadForLog(captured.ResponseBody),
			ResponseHeaders: headersForLog(captured.ResponseHeaders, false),
			DurationMs:      captured.Duration.Milliseconds(),
		}
		if err != nil {
			entry.Error = err.Error()
		} else if captured.Error != nil {
			entry.Error = captured.Error.Error()
		}
		writeLogEntry(lp.logPath, entry)
	}
	return resp, err
}

// Tools returns both web tool definitions.
func (s *WebService) Tools() []*ToolDef {
	return []*ToolDef{
		WebSearchTool(s.search),
		WebFetchTool(s.fetch),
	}
}

// search handles a WebSearch tool call: routes to search or answer backend.
// mode ("search"|"answer") bypasses the classifier when set explicitly by the model.
func (s *WebService) search(ctx context.Context, query, mode string) ToolResult {
	var backend Backend
	var cleanQuery string

	switch mode {
	case "search":
		backend, cleanQuery = BackendSearch, query
	case "answer":
		backend, cleanQuery = BackendAnswer, query
	default:
		backend, cleanQuery = route(ctx, query, s.classifier)
	}

	switch backend {
	case BackendSearch:
		results, err := s.searcher.Search(ctx, cleanQuery)
		if err != nil {
			return ToolResult{Success: false, Output: fmt.Sprintf("web search failed: %v", err), Status: "fail", Error: err}
		}
		return formatSearchResult(results)
	default:
		answer, sources, err := s.answerer.Answer(ctx, cleanQuery)
		if err != nil {
			return ToolResult{Success: false, Output: fmt.Sprintf("web answer failed: %v", err), Status: "fail", Error: err}
		}
		return formatAnswerResult(answer, sources)
	}
}

// fetch handles a WebFetch tool call.
func (s *WebService) fetch(ctx context.Context, url, question string) ToolResult {
	content, err := s.fetcher.Fetch(ctx, url, question)
	if err != nil {
		return ToolResult{Success: false, Output: fmt.Sprintf("web fetch failed: %v", err), Status: "fail", Error: err}
	}
	return formatFetchResult(content, url)
}

// --- Routing ---

// route determines which backend to use. Override tags take priority, then classifier.
func route(ctx context.Context, rawQuery string, classifier QueryClassifier) (Backend, string) {
	// Override tags take priority — no classification needed.
	if strings.Contains(rawQuery, "<prefer_search/>") {
		return BackendSearch, strings.TrimSpace(strings.ReplaceAll(rawQuery, "<prefer_search/>", ""))
	}
	if strings.Contains(rawQuery, "<prefer_answer/>") {
		return BackendAnswer, strings.TrimSpace(strings.ReplaceAll(rawQuery, "<prefer_answer/>", ""))
	}

	// LLM classification — fall back to answer on error.
	classifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	backend, err := classifier.Classify(classifyCtx, rawQuery)
	if err != nil {
		return BackendAnswer, rawQuery
	}
	return backend, rawQuery
}

// --- Result Formatting ---
// ProgressiveDisclosure
func formatSearchResult(results []SearchResult) ToolResult {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(r.Title)
		b.WriteString(" — ")
		b.WriteString(r.URL)
		if r.Snippet != "" {
			b.WriteString(" — ")
			b.WriteString(r.Snippet)
		}
	}

	b.WriteString("\n\n<hint>These are raw search results. To read the full content of any URL above, call WebFetch with that URL. To get a synthesized answer instead, call WebSearch again with mode=\"answer\".</hint>")

	status := fmt.Sprintf("%d results", len(results))
	return ToolResult{Success: true, Output: b.String(), Status: status}
}

// ProgressiveDisclosure
func formatAnswerResult(answer string, sources []string) ToolResult {
	var b strings.Builder
	b.WriteString(answer)

	if len(sources) > 0 {
		b.WriteString("\n\nSources: ")
		b.WriteString(strings.Join(sources, ", "))
	}

	b.WriteString("\n\n<hint>This is a synthesized answer. To find specific URLs or read primary sources directly, call WebSearch again with mode=\"search\".</hint>")

	return ToolResult{Success: true, Output: b.String(), Status: "answered"}
}

// ProgressiveDisclosure
func formatFetchResult(content, url string) ToolResult {
	var b strings.Builder
	b.WriteString(content)
	b.WriteString("\n\nSource: ")
	b.WriteString(url)
	b.WriteString(fmt.Sprintf("\n\n<hint>This is the content of %s. If you need to find more pages on this topic, call WebSearch.</hint>", url))

	return ToolResult{Success: true, Output: b.String(), Status: "fetched"}
}

// --- v1 Implementations ---

// openRouterSearcher uses the OpenRouter web plugin for search.
type openRouterSearcher struct {
	provider   llm.Provider
	model      string
	engine     string
	maxResults int
}

func (s *openRouterSearcher) Search(ctx context.Context, query string) ([]SearchResult, error) {
	req := &llm.Request{
		Model:     s.model,
		MaxTokens: 1024,
		System:    "Return the search results as a list. For each result, include the title, URL, and a brief snippet. Do not add commentary.",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: query},
		},
		Plugins: []llm.Plugin{
			{ID: "web", MaxResults: s.maxResults, Engine: s.engine},
		},
	}

	resp, err := s.provider.SendMessage(ctx, req)
	if err != nil {
		return nil, err
	}

	// Build results from citations if available, otherwise parse from content.
	if len(resp.Citations) > 0 {
		results := make([]SearchResult, 0, len(resp.Citations))
		for _, c := range resp.Citations {
			results = append(results, SearchResult{
				Title: c.Title,
				URL:   c.URL,
			})
		}
		return results, nil
	}

	// Fallback: return the model's text as a single result.
	// The web plugin injects search context into the model; the response
	// contains the formatted results as text.
	return []SearchResult{{Title: "Search Results", URL: "", Snippet: resp.Content}}, nil
}

// sonarAnswerer uses Perplexity sonar for synthesized answers.
type sonarAnswerer struct {
	provider llm.Provider
	model    string
}

func (a *sonarAnswerer) Answer(ctx context.Context, query string) (string, []string, error) {
	req := &llm.Request{
		Model:     a.model,
		MaxTokens: 2048,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: query},
		},
	}

	resp, err := a.provider.SendMessage(ctx, req)
	if err != nil {
		return "", nil, err
	}

	// Extract sources from citations.
	var sources []string
	for _, c := range resp.Citations {
		if c.URL != "" {
			sources = append(sources, c.URL)
		}
	}

	return resp.Content, sources, nil
}

// sonarFetcher uses Perplexity sonar to fetch and summarize URL content.
type sonarFetcher struct {
	provider llm.Provider
	model    string
}

func (f *sonarFetcher) Fetch(ctx context.Context, url string, question string) (string, error) {
	prompt := fmt.Sprintf("Read and summarize the content at: %s", url)
	if question != "" {
		prompt = fmt.Sprintf("Read the content at %s and answer this question: %s", url, question)
	}

	req := &llm.Request{
		Model:     f.model,
		MaxTokens: 4096,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: prompt},
		},
	}

	resp, err := f.provider.SendMessage(ctx, req)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// llmClassifier uses an LLM to classify queries as search or answer.
type llmClassifier struct {
	provider llm.Provider
	model    string
}

const classifierSystemPrompt = `You are a query router. Given a search query, decide which backend to use:
- "prefer_search" — for queries that need specific URLs, pages, repos, documentation, or source code
- "prefer_answer" — for queries that need a direct answer, summary, current events, or general knowledge

Respond with a single word: prefer_search or prefer_answer. Nothing else.`

func (c *llmClassifier) Classify(ctx context.Context, query string) (Backend, error) {
	req := &llm.Request{
		Model:     c.model,
		MaxTokens: 50, // small but enough for reasoning models to think then emit a single word
		System:    classifierSystemPrompt,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: query},
		},
	}

	resp, err := c.provider.SendMessage(ctx, req)
	if err != nil {
		return BackendAnswer, err
	}

	if strings.Contains(strings.ToLower(resp.Content), "prefer_search") {
		return BackendSearch, nil
	}
	return BackendAnswer, nil
}
