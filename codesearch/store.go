package codesearch

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

var vecOnce sync.Once

// Store manages the SQLite database for code chunks, vectors, and full-text search.
type Store struct {
	db     *sql.DB
	dims   int
	hasFTS bool
	hasVec bool
}

// NewStore opens (or creates) the database at dbPath and initializes the schema.
func NewStore(dbPath string, dims int) (*Store, error) {
	vecOnce.Do(func() {
		sqlite_vec.Auto()
	})

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("codesearch: create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("codesearch: open db: %w", err)
	}

	s := &Store{db: db, dims: dims}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) initSchema() error {
	baseSchema := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS files (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			path       TEXT NOT NULL UNIQUE,
			hash       TEXT NOT NULL,
			language   TEXT NOT NULL DEFAULT '',
			indexed_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_files_path ON files(path);

		CREATE TABLE IF NOT EXISTS chunks (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id     INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
			content     TEXT NOT NULL,
			start_line  INTEGER NOT NULL,
			end_line    INTEGER NOT NULL,
			chunk_type  TEXT NOT NULL DEFAULT 'code',
			symbol_name TEXT NOT NULL DEFAULT '',
			language    TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_chunks_file_id ON chunks(file_id);
		CREATE INDEX IF NOT EXISTS idx_chunks_type ON chunks(chunk_type);

		CREATE TABLE IF NOT EXISTS meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)

	if _, err := s.db.Exec(baseSchema); err != nil {
		return fmt.Errorf("codesearch: init schema: %w", err)
	}

	vecSchema := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(
			chunk_id INTEGER PRIMARY KEY,
			embedding float[%d]
		);
	`, s.dims)

	if _, err := s.db.Exec(vecSchema); err != nil {
		// Keep indexing/search available when vec0 isn't available.
		if strings.Contains(strings.ToLower(err.Error()), "no such module: vec0") {
			s.hasVec = false
		} else {
			return fmt.Errorf("codesearch: init vec schema: %w", err)
		}
	} else {
		s.hasVec = true
	}

	ftsSchema := `
		CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
			content, symbol_name, language,
			content='chunks', content_rowid='id',
			tokenize='porter unicode61'
		);

		-- FTS sync triggers
		CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
			INSERT INTO chunks_fts(rowid, content, symbol_name, language)
			VALUES (new.id, new.content, new.symbol_name, new.language);
		END;

		CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
			INSERT INTO chunks_fts(chunks_fts, rowid, content, symbol_name, language)
			VALUES ('delete', old.id, old.content, old.symbol_name, old.language);
		END;

		CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
			INSERT INTO chunks_fts(chunks_fts, rowid, content, symbol_name, language)
			VALUES ('delete', old.id, old.content, old.symbol_name, old.language);
			INSERT INTO chunks_fts(rowid, content, symbol_name, language)
			VALUES (new.id, new.content, new.symbol_name, new.language);
		END;
	`

	if _, err := s.db.Exec(ftsSchema); err != nil {
		// Keep search available when FTS5 wasn't compiled in.
		if strings.Contains(strings.ToLower(err.Error()), "no such module: fts5") {
			s.hasFTS = false
			return nil
		}
		return fmt.Errorf("codesearch: init fts schema: %w", err)
	}

	s.hasFTS = true
	return nil
}

// UpsertFile inserts or updates a file record. Returns the file ID.
func (s *Store) UpsertFile(path, hash, language string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		INSERT INTO files (path, hash, language, indexed_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET hash=excluded.hash, language=excluded.language, indexed_at=excluded.indexed_at
	`, path, hash, language, now)
	if err != nil {
		return 0, fmt.Errorf("codesearch: upsert file: %w", err)
	}
	// Always re-read the canonical row ID by path.
	// On SQLite UPSERT update paths, LastInsertId can be stale/non-deterministic.
	return s.fileID(path)
}

func (s *Store) fileID(path string) (int64, error) {
	var id int64
	err := s.db.QueryRow("SELECT id FROM files WHERE path = ?", path).Scan(&id)
	return id, err
}

// DeleteFile removes a file and its chunks from the database.
// vec_chunks entries are cleaned up manually (virtual tables don't support FK cascades).
func (s *Store) DeleteFile(path string) error {
	// Get chunk IDs first for vec_chunks cleanup
	var fileID int64
	err := s.db.QueryRow("SELECT id FROM files WHERE path = ?", path).Scan(&fileID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	rows, err := s.db.Query("SELECT id FROM chunks WHERE file_id = ?", fileID)
	if err != nil {
		return err
	}
	var chunkIDs []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		chunkIDs = append(chunkIDs, id)
	}
	rows.Close()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete vec_chunks entries when vector table is available.
	if s.hasVec {
		for _, cid := range chunkIDs {
			tx.Exec("DELETE FROM vec_chunks WHERE chunk_id = ?", cid)
		}
	}
	// Delete chunks (triggers handle FTS cleanup)
	tx.Exec("DELETE FROM chunks WHERE file_id = ?", fileID)
	// Delete file
	tx.Exec("DELETE FROM files WHERE id = ?", fileID)

	return tx.Commit()
}

// DeleteChunksByFileID removes all chunks for a file ID.
func (s *Store) DeleteChunksByFileID(fileID int64) error {
	rows, err := s.db.Query("SELECT id FROM chunks WHERE file_id = ?", fileID)
	if err != nil {
		return err
	}
	var chunkIDs []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		chunkIDs = append(chunkIDs, id)
	}
	rows.Close()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if s.hasVec {
		for _, cid := range chunkIDs {
			tx.Exec("DELETE FROM vec_chunks WHERE chunk_id = ?", cid)
		}
	}
	tx.Exec("DELETE FROM chunks WHERE file_id = ?", fileID)

	return tx.Commit()
}

// InsertChunks inserts chunks and their embedding vectors for a file.
func (s *Store) InsertChunks(fileID int64, chunks []Chunk, embeddings [][]float32) error {
	if s.hasVec && len(chunks) != len(embeddings) {
		return fmt.Errorf("codesearch: chunks/embeddings count mismatch: %d vs %d", len(chunks), len(embeddings))
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	chunkStmt, err := tx.Prepare(`
		INSERT INTO chunks (file_id, content, start_line, end_line, chunk_type, symbol_name, language)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer chunkStmt.Close()

	var vecStmt *sql.Stmt
	if s.hasVec {
		vecStmt, err = tx.Prepare("INSERT INTO vec_chunks (chunk_id, embedding) VALUES (?, ?)")
		if err != nil {
			return err
		}
		defer vecStmt.Close()
	}

	for i, chunk := range chunks {
		res, err := chunkStmt.Exec(
			fileID, chunk.Content, chunk.StartLine, chunk.EndLine,
			chunk.ChunkType, chunk.SymbolName, chunk.Language,
		)
		if err != nil {
			return fmt.Errorf("codesearch: insert chunk: %w", err)
		}
		chunkID, err := res.LastInsertId()
		if err != nil {
			return err
		}

		if s.hasVec {
			vecBytes := float32ToBytes(embeddings[i])
			if _, err := vecStmt.Exec(chunkID, vecBytes); err != nil {
				return fmt.Errorf("codesearch: insert vec: %w", err)
			}
		}
	}

	return tx.Commit()
}

// VectorSearch finds the closest chunks to queryVec using cosine distance.
func (s *Store) VectorSearch(queryVec []float32, limit int, scope string) ([]scoredChunk, error) {
	if !s.hasVec {
		return nil, fmt.Errorf("codesearch: vector search unavailable")
	}

	vecBytes := float32ToBytes(queryVec)

	query := `
		SELECT v.chunk_id, v.distance
		FROM vec_chunks v
	`
	args := []any{vecBytes, limit}

	if scope != "" && scope != "all" {
		query += `
			JOIN chunks c ON c.id = v.chunk_id
			WHERE v.embedding MATCH ? AND k = ?
			AND c.chunk_type = ?
			ORDER BY v.distance
		`
		args = append(args, scopeToChunkType(scope))
	} else {
		query += `
			WHERE v.embedding MATCH ? AND k = ?
			ORDER BY v.distance
		`
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("codesearch: vector search: %w", err)
	}
	defer rows.Close()

	var results []scoredChunk
	for rows.Next() {
		var sc scoredChunk
		var distance float64
		if err := rows.Scan(&sc.ChunkID, &distance); err != nil {
			continue
		}
		// Convert cosine distance to similarity score (0–1)
		sc.Score = 1.0 - distance
		results = append(results, sc)
	}
	return results, rows.Err()
}

// SupportsVectorSearch reports whether sqlite-vec is available for this store.
func (s *Store) SupportsVectorSearch() bool {
	return s.hasVec
}

// FTSSearch performs a full-text search using FTS5.
func (s *Store) FTSSearch(query string, limit int, scope string) ([]scoredChunk, error) {
	if !s.hasFTS {
		return s.textSearchFallback(query, limit, scope)
	}

	sqlQuery := `
		SELECT f.rowid, rank
		FROM chunks_fts f
	`
	args := []any{query}

	if scope != "" && scope != "all" {
		sqlQuery += `
			JOIN chunks c ON c.id = f.rowid
			WHERE chunks_fts MATCH ?
			AND c.chunk_type = ?
			ORDER BY rank
			LIMIT ?
		`
		args = append(args, scopeToChunkType(scope), limit)
	} else {
		sqlQuery += `
			WHERE chunks_fts MATCH ?
			ORDER BY rank
			LIMIT ?
		`
		args = append(args, limit)
	}

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		// Existing DB might be from a build without FTS support.
		if strings.Contains(strings.ToLower(err.Error()), "no such table: chunks_fts") {
			return s.textSearchFallback(query, limit, scope)
		}
		return nil, fmt.Errorf("codesearch: fts search: %w", err)
	}
	defer rows.Close()

	var results []scoredChunk
	for rows.Next() {
		var sc scoredChunk
		var rank float64
		if err := rows.Scan(&sc.ChunkID, &rank); err != nil {
			continue
		}
		// BM25 rank is negative (lower = better), normalize to 0–1
		sc.Score = 1.0 / (1.0 + math.Abs(rank))
		results = append(results, sc)
	}
	return results, rows.Err()
}

func (s *Store) textSearchFallback(query string, limit int, scope string) ([]scoredChunk, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	pattern := "%" + strings.ToLower(query) + "%"
	sqlQuery := `
		SELECT id
		FROM chunks
		WHERE (
			lower(content) LIKE ?
			OR lower(symbol_name) LIKE ?
			OR lower(language) LIKE ?
		)
	`
	args := []any{pattern, pattern, pattern}

	if scope != "" && scope != "all" {
		sqlQuery += " AND chunk_type = ?"
		args = append(args, scopeToChunkType(scope))
	}

	sqlQuery += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("codesearch: fallback text search: %w", err)
	}
	defer rows.Close()

	var results []scoredChunk
	rank := 1
	for rows.Next() {
		var sc scoredChunk
		if err := rows.Scan(&sc.ChunkID); err != nil {
			continue
		}
		sc.Score = 1.0 / float64(rank)
		results = append(results, sc)
		rank++
	}
	return results, rows.Err()
}

// GetChunk retrieves a chunk by its ID with file path.
func (s *Store) GetChunk(id int64) (*Chunk, error) {
	var c Chunk
	err := s.db.QueryRow(`
		SELECT c.content, f.path, c.start_line, c.end_line, c.chunk_type, c.symbol_name, c.language
		FROM chunks c
		JOIN files f ON f.id = c.file_id
		WHERE c.id = ?
	`, id).Scan(&c.Content, &c.FilePath, &c.StartLine, &c.EndLine, &c.ChunkType, &c.SymbolName, &c.Language)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// FileHash returns the stored hash for a file path, or empty string if not found.
func (s *Store) FileHash(path string) string {
	var hash string
	s.db.QueryRow("SELECT hash FROM files WHERE path = ?", path).Scan(&hash)
	return hash
}

// AllFilePaths returns all indexed file paths.
func (s *Store) AllFilePaths() ([]string, error) {
	rows, err := s.db.Query("SELECT path FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// Stats returns index statistics.
func (s *Store) Stats() (IndexStats, error) {
	var stats IndexStats
	s.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&stats.TotalFiles)
	s.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&stats.TotalChunks)
	return stats, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// float32ToBytes serializes a float32 slice to little-endian bytes for sqlite-vec.
func float32ToBytes(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func scopeToChunkType(scope string) string {
	switch scope {
	case "functions":
		return "function"
	case "classes":
		return "class"
	case "comments":
		return "comment"
	default:
		return scope
	}
}
