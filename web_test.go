package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// --- Mock implementations for testing ---

type mockClassifier struct {
	result Backend
	err    error
}

func (m *mockClassifier) Classify(ctx context.Context, query string) (Backend, error) {
	return m.result, m.err
}

type mockSearcher struct {
	results []SearchResult
	err     error
}

func (m *mockSearcher) Search(ctx context.Context, query string) ([]SearchResult, error) {
	return m.results, m.err
}

type mockAnswerer struct {
	answer  string
	sources []string
	err     error
}

func (m *mockAnswerer) Answer(ctx context.Context, query string) (string, []string, error) {
	return m.answer, m.sources, m.err
}

type mockFetcher struct {
	content string
	err     error
}

func (m *mockFetcher) Fetch(ctx context.Context, url string, question string) (string, error) {
	return m.content, m.err
}

// --- Routing tests ---

func TestRoute_OverrideTagSearch(t *testing.T) {
	classifier := &mockClassifier{result: BackendAnswer} // should be ignored
	backend, query := route(context.Background(), "Go iter package <prefer_search/>", classifier)
	if backend != BackendSearch {
		t.Errorf("expected BackendSearch, got %d", backend)
	}
	if strings.Contains(query, "<prefer_search/>") {
		t.Error("tag should be stripped from query")
	}
	if strings.TrimSpace(query) != "Go iter package" {
		t.Errorf("unexpected query: %q", query)
	}
}

func TestRoute_OverrideTagAnswer(t *testing.T) {
	classifier := &mockClassifier{result: BackendSearch} // should be ignored
	backend, query := route(context.Background(), "<prefer_answer/> what is Go", classifier)
	if backend != BackendAnswer {
		t.Errorf("expected BackendAnswer, got %d", backend)
	}
	if strings.Contains(query, "<prefer_answer/>") {
		t.Error("tag should be stripped from query")
	}
}

func TestRoute_ClassifierSearch(t *testing.T) {
	classifier := &mockClassifier{result: BackendSearch}
	backend, _ := route(context.Background(), "find golang documentation", classifier)
	if backend != BackendSearch {
		t.Errorf("expected BackendSearch, got %d", backend)
	}
}

func TestRoute_ClassifierAnswer(t *testing.T) {
	classifier := &mockClassifier{result: BackendAnswer}
	backend, _ := route(context.Background(), "what is the default HTTP timeout in Go", classifier)
	if backend != BackendAnswer {
		t.Errorf("expected BackendAnswer, got %d", backend)
	}
}

func TestRoute_ClassifierErrorFallsBackToAnswer(t *testing.T) {
	classifier := &mockClassifier{err: fmt.Errorf("network error")}
	backend, _ := route(context.Background(), "some query", classifier)
	if backend != BackendAnswer {
		t.Errorf("expected BackendAnswer on classifier error, got %d", backend)
	}
}

// --- Result formatting tests ---

func TestFormatSearchResult(t *testing.T) {
	results := []SearchResult{
		{Title: "Go Docs", URL: "https://go.dev/doc", Snippet: "Official Go documentation"},
		{Title: "Go Blog", URL: "https://go.dev/blog"},
	}
	tr := formatSearchResult(results)
	if !tr.Success {
		t.Fatal("expected success")
	}
	if !strings.Contains(tr.Output, "Go Docs — https://go.dev/doc — Official Go documentation") {
		t.Errorf("missing first result in output: %s", tr.Output)
	}
	if !strings.Contains(tr.Output, "Go Blog — https://go.dev/blog") {
		t.Errorf("missing second result in output: %s", tr.Output)
	}
	if !strings.Contains(tr.Output, "<hint>") {
		t.Error("missing hint tag")
	}
	if !strings.Contains(tr.Output, "WebFetch") {
		t.Error("hint should reference WebFetch")
	}
	if !strings.Contains(tr.Output, "<prefer_answer/>") {
		t.Error("hint should reference prefer_answer tag")
	}
	if tr.Status != "2 results" {
		t.Errorf("unexpected status: %s", tr.Status)
	}
}

func TestFormatAnswerResult(t *testing.T) {
	tr := formatAnswerResult("Go 1.23 introduced iterators.", []string{"go.dev/blog", "go.dev/doc"})
	if !tr.Success {
		t.Fatal("expected success")
	}
	if !strings.Contains(tr.Output, "Go 1.23 introduced iterators.") {
		t.Error("missing answer content")
	}
	if !strings.Contains(tr.Output, "Sources: go.dev/blog, go.dev/doc") {
		t.Errorf("missing sources: %s", tr.Output)
	}
	if !strings.Contains(tr.Output, "<hint>") {
		t.Error("missing hint tag")
	}
	if !strings.Contains(tr.Output, "<prefer_search/>") {
		t.Error("hint should reference prefer_search tag")
	}
	if tr.Status != "answered" {
		t.Errorf("unexpected status: %s", tr.Status)
	}
}

func TestFormatFetchResult(t *testing.T) {
	tr := formatFetchResult("Package iter provides basic definitions...", "https://pkg.go.dev/iter")
	if !tr.Success {
		t.Fatal("expected success")
	}
	if !strings.Contains(tr.Output, "Package iter provides basic definitions...") {
		t.Error("missing content")
	}
	if !strings.Contains(tr.Output, "Source: https://pkg.go.dev/iter") {
		t.Error("missing source URL")
	}
	if !strings.Contains(tr.Output, "<hint>") {
		t.Error("missing hint tag")
	}
	if !strings.Contains(tr.Output, "WebSearch") {
		t.Error("hint should reference WebSearch")
	}
	if tr.Status != "fetched" {
		t.Errorf("unexpected status: %s", tr.Status)
	}
}

// --- WebService integration tests (with mocks) ---

func TestWebService_SearchRouting(t *testing.T) {
	svc := &WebService{
		searcher:   &mockSearcher{results: []SearchResult{{Title: "Result", URL: "https://example.com"}}},
		answerer:   &mockAnswerer{answer: "This is the answer"},
		fetcher:    &mockFetcher{content: "page content"},
		classifier: &mockClassifier{result: BackendSearch},
	}

	result := svc.search(context.Background(), "find golang docs", "")
	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.Output, "Result") {
		t.Error("expected search results in output")
	}
}

func TestWebService_AnswerRouting(t *testing.T) {
	svc := &WebService{
		searcher:   &mockSearcher{results: []SearchResult{{Title: "Result", URL: "https://example.com"}}},
		answerer:   &mockAnswerer{answer: "42 is the answer", sources: []string{"example.com"}},
		fetcher:    &mockFetcher{content: "page content"},
		classifier: &mockClassifier{result: BackendAnswer},
	}

	result := svc.search(context.Background(), "what is the meaning of life", "")
	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.Output, "42 is the answer") {
		t.Error("expected answer in output")
	}
}

func TestWebService_Fetch(t *testing.T) {
	svc := &WebService{
		fetcher: &mockFetcher{content: "The iter package defines Seq and Seq2."},
	}

	result := svc.fetch(context.Background(), "https://pkg.go.dev/iter", "")
	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.Output, "The iter package defines Seq and Seq2.") {
		t.Error("expected fetch content in output")
	}
	if !strings.Contains(result.Output, "Source: https://pkg.go.dev/iter") {
		t.Error("expected source URL in output")
	}
}

func TestWebService_FetchError(t *testing.T) {
	svc := &WebService{
		fetcher: &mockFetcher{err: fmt.Errorf("connection refused")},
	}

	result := svc.fetch(context.Background(), "https://example.com", "")
	if result.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(result.Output, "connection refused") {
		t.Error("expected error message in output")
	}
}

// --- Tool definition tests ---

func TestWebSearchToolDef(t *testing.T) {
	called := false
	tool := WebSearchTool(func(ctx context.Context, query, mode string) ToolResult {
		called = true
		if query != "test query" {
			t.Errorf("unexpected query: %s", query)
		}
		return ToolResult{Success: true, Output: "results", Status: "ok"}
	})

	if tool.Name != "WebSearch" {
		t.Errorf("unexpected name: %s", tool.Name)
	}
	if !tool.AutoApprove(false) {
		t.Error("WebSearch should auto-approve")
	}

	result := tool.Execute(map[string]any{"query": "test query"})
	if !called {
		t.Error("search function was not called")
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestWebSearchToolDef_EmptyQuery(t *testing.T) {
	tool := WebSearchTool(func(ctx context.Context, query, mode string) ToolResult {
		t.Fatal("should not be called")
		return ToolResult{}
	})

	result := tool.Execute(map[string]any{"query": ""})
	if result.Success {
		t.Error("expected failure for empty query")
	}
}

func TestWebFetchToolDef(t *testing.T) {
	called := false
	tool := WebFetchTool(func(ctx context.Context, url, question string) ToolResult {
		called = true
		if url != "https://example.com" {
			t.Errorf("unexpected url: %s", url)
		}
		if question != "what is it" {
			t.Errorf("unexpected question: %s", question)
		}
		return ToolResult{Success: true, Output: "content", Status: "ok"}
	})

	if tool.Name != "WebFetch" {
		t.Errorf("unexpected name: %s", tool.Name)
	}
	if !tool.AutoApprove(false) {
		t.Error("WebFetch should auto-approve")
	}

	result := tool.Execute(map[string]any{"url": "https://example.com", "question": "what is it"})
	if !called {
		t.Error("fetch function was not called")
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestWebFetchToolDef_EmptyURL(t *testing.T) {
	tool := WebFetchTool(func(ctx context.Context, url, question string) ToolResult {
		t.Fatal("should not be called")
		return ToolResult{}
	})

	result := tool.Execute(map[string]any{"url": ""})
	if result.Success {
		t.Error("expected failure for empty url")
	}
}
