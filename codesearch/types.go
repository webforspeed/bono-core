package codesearch

import (
	"fmt"
	"time"
)

// Chunk represents a parsed unit of code extracted from a source file.
type Chunk struct {
	Content    string // raw code text
	FilePath   string
	StartLine  int
	EndLine    int
	ChunkType  string // "function", "method", "struct", "class", "comment", "code"
	SymbolName string // e.g., "HandleLogin", "UserService"
	Language   string // e.g., "go", "python", "javascript"
}

// SearchResult is a single match from a code search query.
type SearchResult struct {
	FilePath   string  `json:"file_path"`
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
	Score      float64 `json:"score"` // 0.0–1.0 similarity
	Snippet    string  `json:"snippet"`
	ChunkType  string  `json:"chunk_type"`
	SymbolName string  `json:"symbol_name"`
	Language   string  `json:"language"`
}

// SearchResults holds the full response from a search query.
type SearchResults struct {
	Items       []SearchResult `json:"items"`
	TotalMatches int           `json:"total_matches"`
}

// Format renders search results as a human-readable string for LLM consumption.
func (sr *SearchResults) Format() string {
	if len(sr.Items) == 0 {
		return "No results found."
	}
	var b []byte
	for i, r := range sr.Items {
		if i > 0 {
			b = append(b, '\n')
		}
		b = appendResult(b, i+1, &r)
	}
	return string(b)
}

func appendResult(b []byte, n int, r *SearchResult) []byte {
	b = appendf(b, "--- Result %d (score: %.2f) ---\n", n, r.Score)
	b = appendf(b, "File: %s:%d-%d", r.FilePath, r.StartLine, r.EndLine)
	if r.SymbolName != "" {
		b = appendf(b, "  [%s: %s]", r.ChunkType, r.SymbolName)
	}
	b = append(b, '\n')
	b = append(b, r.Snippet...)
	b = append(b, '\n')
	return b
}

func appendf(b []byte, format string, args ...any) []byte {
	return append(b, fmt.Sprintf(format, args...)...)
}

// IndexProgress reports indexing status to the caller.
type IndexProgress struct {
	Phase      string // "scanning", "chunking", "embedding", "storing"
	FilesTotal int
	FilesDone  int
}

// IndexStats summarizes the state of an index.
type IndexStats struct {
	TotalFiles  int
	TotalChunks int
	Duration    time.Duration
}

// EngineConfig configures the code search engine.
type EngineConfig struct {
	DBPath  string // path to SQLite database, default ".bono/index.db"
	APIKey  string // OpenRouter API key
	BaseURL string // API base URL, default "https://openrouter.ai/api/v1"
	Model   string // embedding model, default "openai/text-embedding-3-small"
	Dims    int    // embedding dimensions, default 1536
}

func (c *EngineConfig) setDefaults() {
	if c.DBPath == "" {
		c.DBPath = ".bono/index.db"
	}
	if c.BaseURL == "" {
		c.BaseURL = "https://openrouter.ai/api/v1"
	}
	if c.Model == "" {
		c.Model = "openai/text-embedding-3-small"
	}
	if c.Dims == 0 {
		c.Dims = 1536
	}
}

// IndexOptions configures which files to index.
type IndexOptions struct {
	FilePatterns    []string // include globs, e.g. ["*.go", "src/**/*.ts"]
	ExcludePatterns []string // exclude globs, e.g. ["vendor/", "*_test.go"]
}

// SearchOptions configures a search query.
type SearchOptions struct {
	FilePatterns    []string
	ExcludePatterns []string
	MaxResults      int    // default 10
	SearchType      string // "semantic", "hybrid", "exact"
	Scope           string // "functions", "classes", "comments", "all"
	ContextWindow   int    // lines of context around match, default 5
	SnippetMaxLines int    // max lines per snippet, default 20
	GroupByFile     bool
}

func (o *SearchOptions) setDefaults() {
	if o.MaxResults <= 0 {
		o.MaxResults = 10
	}
	if o.SearchType == "" {
		o.SearchType = "semantic"
	}
	if o.Scope == "" {
		o.Scope = "all"
	}
	if o.ContextWindow <= 0 {
		o.ContextWindow = 5
	}
	if o.SnippetMaxLines <= 0 {
		o.SnippetMaxLines = 20
	}
}

// scoredChunk is an internal type used during search ranking.
type scoredChunk struct {
	ChunkID int64
	Score   float64
}
