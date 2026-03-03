package codesearch

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Engine is the top-level API for code search. Composes Store, Embedder, and Indexer.
type Engine struct {
	store    *Store
	embedder *Embedder
	indexer  *Indexer
	config   EngineConfig
}

// NewEngine creates a code search engine. Opens (or creates) the SQLite database.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	cfg.setDefaults()

	store, err := NewStore(cfg.DBPath, cfg.Dims)
	if err != nil {
		return nil, err
	}

	embedder := NewEmbedder(cfg.APIKey, cfg.BaseURL, cfg.Model, cfg.Dims)

	return &Engine{
		store:    store,
		embedder: embedder,
		indexer:  &Indexer{store: store, embedder: embedder},
		config:   cfg,
	}, nil
}

// Index performs incremental indexing of the given root directory.
func (e *Engine) Index(ctx context.Context, rootDir string, opts IndexOptions, progress func(IndexProgress)) (IndexStats, error) {
	return e.indexer.Index(ctx, rootDir, opts, progress)
}

// Search queries the index and returns ranked results.
func (e *Engine) Search(ctx context.Context, query string, opts SearchOptions) (*SearchResults, error) {
	opts.setDefaults()

	// Over-fetch for post-filtering
	fetchLimit := opts.MaxResults * 3
	if fetchLimit < 30 {
		fetchLimit = 30
	}

	var ranked []scoredChunk

	switch opts.SearchType {
	case "exact":
		results, err := e.store.FTSSearch(query, fetchLimit, opts.Scope)
		if err != nil {
			return nil, err
		}
		ranked = results

	case "hybrid":
		if !e.store.SupportsVectorSearch() {
			results, err := e.store.FTSSearch(query, fetchLimit, opts.Scope)
			if err != nil {
				return nil, err
			}
			ranked = results
			break
		}

		// Run both searches
		vecResults, vecErr := e.vectorSearch(ctx, query, fetchLimit, opts.Scope)
		ftsResults, ftsErr := e.store.FTSSearch(query, fetchLimit, opts.Scope)

		if vecErr != nil && ftsErr != nil {
			return nil, fmt.Errorf("both searches failed: vec=%v, fts=%v", vecErr, ftsErr)
		}
		if vecErr != nil {
			ranked = ftsResults
		} else if ftsErr != nil {
			ranked = vecResults
		} else {
			ranked = MergeRRF(vecResults, ftsResults)
		}

	default: // "semantic"
		if !e.store.SupportsVectorSearch() {
			results, err := e.store.FTSSearch(query, fetchLimit, opts.Scope)
			if err != nil {
				return nil, err
			}
			ranked = results
			break
		}

		results, err := e.vectorSearch(ctx, query, fetchLimit, opts.Scope)
		if err != nil {
			return nil, err
		}
		ranked = results
	}

	if len(ranked) == 0 {
		return &SearchResults{Items: nil, TotalMatches: 0}, nil
	}

	// Enrich results with chunk content and file path
	var items []SearchResult
	seen := make(map[string]bool) // dedup by file:startLine

	for _, sc := range ranked {
		chunk, err := e.store.GetChunk(sc.ChunkID)
		if err != nil {
			continue
		}

		// Apply file pattern filters
		if !matchesPatterns(chunk.FilePath, opts.FilePatterns, opts.ExcludePatterns) {
			continue
		}

		// Apply scope filter
		if opts.Scope != "" && opts.Scope != "all" {
			if !matchesScope(chunk.ChunkType, opts.Scope) {
				continue
			}
		}

		// Dedup
		key := fmt.Sprintf("%s:%d", chunk.FilePath, chunk.StartLine)
		if seen[key] {
			continue
		}
		seen[key] = true

		// Expand context window
		snippet := expandContext(chunk, opts.ContextWindow, opts.SnippetMaxLines)

		items = append(items, SearchResult{
			FilePath:   chunk.FilePath,
			StartLine:  chunk.StartLine,
			EndLine:    chunk.EndLine,
			Score:      sc.Score,
			Snippet:    snippet,
			ChunkType:  chunk.ChunkType,
			SymbolName: chunk.SymbolName,
			Language:   chunk.Language,
		})

		if len(items) >= opts.MaxResults {
			break
		}
	}

	// Group by file if requested
	if opts.GroupByFile {
		sort.Slice(items, func(i, j int) bool {
			if items[i].FilePath != items[j].FilePath {
				return items[i].FilePath < items[j].FilePath
			}
			return items[i].StartLine < items[j].StartLine
		})
	}

	return &SearchResults{
		Items:        items,
		TotalMatches: len(items),
	}, nil
}

// Stats returns index statistics.
func (e *Engine) Stats() (*IndexStats, error) {
	stats, err := e.store.Stats()
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// Close shuts down the engine and closes the database.
func (e *Engine) Close() error {
	return e.store.Close()
}

// SupportsVectorSearch reports whether sqlite-vec is available.
func (e *Engine) SupportsVectorSearch() bool {
	return e.store.SupportsVectorSearch()
}

func (e *Engine) vectorSearch(ctx context.Context, query string, limit int, scope string) ([]scoredChunk, error) {
	if !e.store.SupportsVectorSearch() {
		return nil, fmt.Errorf("vector search unavailable")
	}

	vec, err := e.embedder.EmbedSingle(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	return e.store.VectorSearch(vec, limit, scope)
}

func matchesScope(chunkType, scope string) bool {
	switch scope {
	case "functions":
		return chunkType == "function" || chunkType == "method"
	case "classes":
		return chunkType == "class" || chunkType == "struct"
	case "comments":
		return chunkType == "comment"
	default:
		return true
	}
}

// expandContext reads the file from disk and expands the snippet with surrounding context.
func expandContext(chunk *Chunk, contextWindow, maxLines int) string {
	if contextWindow == 0 {
		// Just truncate the chunk content
		return truncateLines(chunk.Content, maxLines)
	}

	content, err := os.ReadFile(chunk.FilePath)
	if err != nil {
		// If we can't read the file, fall back to stored content
		return truncateLines(chunk.Content, maxLines)
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	start := chunk.StartLine - contextWindow
	if start < 1 {
		start = 1
	}
	end := chunk.EndLine + contextWindow
	if end > totalLines {
		end = totalLines
	}

	// Build snippet with line numbers
	var b strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(extractLines(lines, start, end)))
	lineNum := start
	linesWritten := 0
	for scanner.Scan() {
		if linesWritten >= maxLines {
			b.WriteString("  ... (truncated)\n")
			break
		}
		marker := " "
		if lineNum >= chunk.StartLine && lineNum <= chunk.EndLine {
			marker = ">"
		}
		fmt.Fprintf(&b, "%s %4d | %s\n", marker, lineNum, scanner.Text())
		lineNum++
		linesWritten++
	}

	return b.String()
}

func truncateLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n") + "\n  ... (truncated)"
}

// expandContextFromFile reads lines from a file path, used by expandContext.
// This is separate so the path can be relative or absolute.
func resolveFilePath(chunk *Chunk) string {
	// Try relative path first
	if _, err := os.Stat(chunk.FilePath); err == nil {
		return chunk.FilePath
	}
	// Try with current working directory
	cwd, _ := os.Getwd()
	abs := filepath.Join(cwd, chunk.FilePath)
	if _, err := os.Stat(abs); err == nil {
		return abs
	}
	return chunk.FilePath
}
