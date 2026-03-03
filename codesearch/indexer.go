package codesearch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Indexer walks a directory, chunks files, embeds them, and stores everything.
type Indexer struct {
	store    *Store
	embedder *Embedder
}

// fileEntry holds a file to be indexed.
type fileEntry struct {
	RelPath  string
	AbsPath  string
	Hash     string
	Language string
	Content  []byte
}

// Index performs incremental indexing of rootDir.
// Only new/changed files are processed. Deleted files are removed from the index.
func (idx *Indexer) Index(ctx context.Context, rootDir string, opts IndexOptions, progress func(IndexProgress)) (IndexStats, error) {
	start := time.Now()

	gi := LoadGitIgnore(rootDir)

	// Phase 1: Scan files
	if progress != nil {
		progress(IndexProgress{Phase: "scanning", FilesTotal: 0, FilesDone: 0})
	}

	var allFiles []fileEntry
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip files we can't read
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		relPath, _ := filepath.Rel(rootDir, path)
		relPath = filepath.ToSlash(relPath)

		if info.IsDir() {
			if gi.ShouldIgnoreDir(relPath) {
				return filepath.SkipDir
			}
			return nil
		}

		if gi.ShouldIgnore(relPath) {
			return nil
		}

		if !IsTextFile(path) {
			return nil
		}

		// Apply file pattern filters
		if !matchesPatterns(relPath, opts.FilePatterns, opts.ExcludePatterns) {
			return nil
		}

		// Skip very large files (>1MB)
		if info.Size() > 1<<20 {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		hash := fmt.Sprintf("%x", sha256.Sum256(content))
		lang := DetectLanguage(path)

		allFiles = append(allFiles, fileEntry{
			RelPath:  relPath,
			AbsPath:  path,
			Hash:     hash,
			Language: lang,
			Content:  content,
		})
		return nil
	})
	if err != nil {
		return IndexStats{}, fmt.Errorf("codesearch: scan files: %w", err)
	}

	// Phase 2: Diff — find new, changed, deleted files
	existingPaths, _ := idx.store.AllFilePaths()
	existingSet := make(map[string]bool, len(existingPaths))
	for _, p := range existingPaths {
		existingSet[p] = true
	}

	var toIndex []fileEntry
	scannedSet := make(map[string]bool, len(allFiles))
	for _, f := range allFiles {
		scannedSet[f.RelPath] = true
		existingHash := idx.store.FileHash(f.RelPath)
		if existingHash != f.Hash {
			toIndex = append(toIndex, f)
		}
	}

	// Delete files that no longer exist
	for _, p := range existingPaths {
		if !scannedSet[p] {
			idx.store.DeleteFile(p)
		}
	}

	if len(toIndex) == 0 {
		stats, _ := idx.store.Stats()
		stats.Duration = time.Since(start)
		return stats, nil
	}

	totalFiles := len(toIndex)

	// Phase 3: Chunk
	if progress != nil {
		progress(IndexProgress{Phase: "chunking", FilesTotal: totalFiles, FilesDone: 0})
	}

	type fileChunks struct {
		entry  fileEntry
		chunks []Chunk
	}
	var allChunks []fileChunks
	for i, f := range toIndex {
		if ctx.Err() != nil {
			return IndexStats{}, ctx.Err()
		}
		chunks := ChunkFile(f.AbsPath, f.Content, f.Language)
		if len(chunks) > 0 {
			allChunks = append(allChunks, fileChunks{entry: f, chunks: chunks})
		}
		if progress != nil && (i+1)%10 == 0 {
			progress(IndexProgress{Phase: "chunking", FilesTotal: totalFiles, FilesDone: i + 1})
		}
	}

	if progress != nil {
		progress(IndexProgress{Phase: "chunking", FilesTotal: totalFiles, FilesDone: totalFiles})
	}

	// Flatten all chunks for batch embedding
	var flatChunks []Chunk
	for _, fc := range allChunks {
		flatChunks = append(flatChunks, fc.chunks...)
	}

	embeddings := make([][]float32, len(flatChunks))
	if idx.store.SupportsVectorSearch() {
		// Phase 4: Embed
		if progress != nil {
			progress(IndexProgress{Phase: "embedding", FilesTotal: totalFiles, FilesDone: 0})
		}

		texts := make([]string, len(flatChunks))
		for i, c := range flatChunks {
			texts[i] = c.Content
		}

		var err error
		embeddings, err = idx.embedBatchWithProgress(ctx, texts, totalFiles, progress)
		if err != nil {
			return IndexStats{}, fmt.Errorf("codesearch: embed: %w", err)
		}
	} else if progress != nil {
		progress(IndexProgress{Phase: "embedding", FilesTotal: totalFiles, FilesDone: totalFiles})
	}

	// Phase 5: Store
	if progress != nil {
		progress(IndexProgress{Phase: "storing", FilesTotal: totalFiles, FilesDone: 0})
	}

	// Group embeddings back by file
	embIdx := 0
	for i, fc := range allChunks {
		if ctx.Err() != nil {
			return IndexStats{}, ctx.Err()
		}

		f := fc.entry

		// Upsert file record
		fileID, err := idx.store.UpsertFile(f.RelPath, f.Hash, f.Language)
		if err != nil {
			return IndexStats{}, err
		}

		// Delete existing chunks for this file (if re-indexing)
		if existingSet[f.RelPath] {
			idx.store.DeleteChunksByFileID(fileID)
		}

		// Collect embeddings for this file's chunks
		fileEmbeddings := embeddings[embIdx : embIdx+len(fc.chunks)]
		embIdx += len(fc.chunks)

		// Insert new chunks with embeddings
		if err := idx.store.InsertChunks(fileID, fc.chunks, fileEmbeddings); err != nil {
			return IndexStats{}, err
		}

		if progress != nil && (i+1)%5 == 0 {
			progress(IndexProgress{Phase: "storing", FilesTotal: totalFiles, FilesDone: i + 1})
		}
	}

	stats, _ := idx.store.Stats()
	stats.Duration = time.Since(start)
	return stats, nil
}

// embedBatchWithProgress embeds all texts and reports progress based on file count.
func (idx *Indexer) embedBatchWithProgress(ctx context.Context, texts []string, totalFiles int, progress func(IndexProgress)) ([][]float32, error) {
	const batchSize = 100

	all := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		vecs, err := idx.embedder.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		copy(all[start:], vecs)

		if progress != nil {
			// Approximate file progress from chunk progress
			fileDone := totalFiles * end / len(texts)
			progress(IndexProgress{Phase: "embedding", FilesTotal: totalFiles, FilesDone: fileDone})
		}
	}
	return all, nil
}

// matchesPatterns checks if a path matches include/exclude globs.
func matchesPatterns(relPath string, includes, excludes []string) bool {
	// If include patterns are set, path must match at least one
	if len(includes) > 0 {
		matched := false
		for _, p := range includes {
			if m, _ := filepath.Match(p, relPath); m {
				matched = true
				break
			}
			// Also match against base name
			if m, _ := filepath.Match(p, filepath.Base(relPath)); m {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// If exclude patterns are set, path must not match any
	for _, p := range excludes {
		if m, _ := filepath.Match(p, relPath); m {
			return false
		}
		if m, _ := filepath.Match(p, filepath.Base(relPath)); m {
			return false
		}
		// Check if path starts with exclude pattern (for directory patterns like "vendor/")
		if strings.HasSuffix(p, "/") && strings.HasPrefix(relPath, strings.TrimSuffix(p, "/")) {
			return false
		}
	}

	return true
}
