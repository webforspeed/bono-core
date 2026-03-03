package core

import (
	"context"
	"fmt"

	"github.com/webforspeed/bono-core/codesearch"
)

// CodeSearchConfig configures the code search service.
type CodeSearchConfig struct {
	APIKey  string // OpenRouter API key
	BaseURL string // API base URL, defaults to "https://openrouter.ai/api/v1"
	DBPath  string // SQLite index path, defaults to ".bono/index.db"
	Model   string // Embedding model, defaults to "openai/text-embedding-3-small"
	Dims    int    // Embedding dimensions, defaults to 1536
}

// CodeSearchIndexOptions configures indexing include/exclude patterns.
type CodeSearchIndexOptions = codesearch.IndexOptions

// CodeSearchIndexProgress reports indexing progress updates.
type CodeSearchIndexProgress = codesearch.IndexProgress

// CodeSearchIndexStats summarizes an index run.
type CodeSearchIndexStats = codesearch.IndexStats

// CodeSearchService owns code-search engine lifecycle and tool integration.
type CodeSearchService struct {
	engine *codesearch.Engine
}

// NewCodeSearchService creates a code-search service backed by sqlite + embeddings.
func NewCodeSearchService(cfg CodeSearchConfig) (*CodeSearchService, error) {
	engine, err := codesearch.NewEngine(codesearch.EngineConfig{
		DBPath:  cfg.DBPath,
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
		Dims:    cfg.Dims,
	})
	if err != nil {
		return nil, err
	}
	return &CodeSearchService{engine: engine}, nil
}

// Tool returns the standard code_search tool definition.
func (s *CodeSearchService) Tool() *ToolDef {
	return CodeSearchTool(func(query string, rawOpts map[string]any) ToolResult {
		return s.search(query, rawOpts)
	})
}

func (s *CodeSearchService) search(query string, rawOpts map[string]any) ToolResult {
	if s == nil || s.engine == nil {
		return ToolResult{
			Success: false,
			Output:  "code search unavailable",
			Status:  "fail: code search unavailable",
			Error:   fmt.Errorf("code search unavailable"),
		}
	}
	opts := parseCodeSearchOptions(rawOpts)
	results, err := s.engine.Search(context.Background(), query, opts)
	if err != nil {
		return ToolResult{
			Success: false,
			Output:  err.Error(),
			Status:  "fail: " + err.Error(),
			Error:   err,
		}
	}
	return ToolResult{
		Success: true,
		Output:  results.Format(),
		Status:  fmt.Sprintf("%d results", len(results.Items)),
	}
}

// CodeSearchSupportsVector reports whether sqlite-vec is available.
func (s *CodeSearchService) CodeSearchSupportsVector() bool {
	if s == nil || s.engine == nil {
		return false
	}
	return s.engine.SupportsVectorSearch()
}

// CodeSearchStats returns index statistics.
func (s *CodeSearchService) CodeSearchStats() (*CodeSearchIndexStats, error) {
	if s == nil || s.engine == nil {
		return nil, fmt.Errorf("code search unavailable")
	}
	return s.engine.Stats()
}

// CodeSearchIndex builds or updates the semantic index.
func (s *CodeSearchService) CodeSearchIndex(ctx context.Context, rootDir string, opts CodeSearchIndexOptions, progress func(CodeSearchIndexProgress)) (CodeSearchIndexStats, error) {
	if s == nil || s.engine == nil {
		return CodeSearchIndexStats{}, fmt.Errorf("code search unavailable")
	}
	if rootDir == "" {
		rootDir = "."
	}
	return s.engine.Index(ctx, rootDir, opts, func(p codesearch.IndexProgress) {
		if progress != nil {
			progress(p)
		}
	})
}

// Close releases sqlite and other resources.
func (s *CodeSearchService) Close() error {
	if s == nil || s.engine == nil {
		return nil
	}
	err := s.engine.Close()
	s.engine = nil
	return err
}

func parseCodeSearchOptions(opts map[string]any) codesearch.SearchOptions {
	var so codesearch.SearchOptions

	if v, ok := intFromAny(opts["max_results"]); ok {
		so.MaxResults = v
	}
	if v, ok := opts["search_type"].(string); ok {
		so.SearchType = v
	}
	if v, ok := opts["scope"].(string); ok {
		so.Scope = v
	}
	if v, ok := intFromAny(opts["context_window"]); ok {
		so.ContextWindow = v
	}
	if v, ok := intFromAny(opts["snippet_max_lines"]); ok {
		so.SnippetMaxLines = v
	}
	if v, ok := opts["group_by_file"].(bool); ok {
		so.GroupByFile = v
	}
	so.FilePatterns = stringSliceFromAny(opts["file_patterns"])
	so.ExcludePatterns = stringSliceFromAny(opts["exclude_patterns"])

	return so
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func stringSliceFromAny(v any) []string {
	switch items := v.(type) {
	case []string:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			s, ok := item.(string)
			if !ok || s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}
