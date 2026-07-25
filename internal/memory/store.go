// Package memory is the persistent memory store (ADR-0003). It runs on the
// cgo-free ncruces/go-sqlite3 driver (real SQLite compiled to WASM on wazero)
// with sqlite-vec embedded into the same binary, so FTS5 (lexical) and vec0
// (semantic) live in one file-backed database. Memory + embeddings stay local;
// chunk text never leaves the host.
package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/7solutions/openplus/internal/embed"

	// sqlite-vec embedded into the WASM build (sets sqlite3.Binary). Must be
	// imported so vec_* functions are available without LOAD EXTENSION.
	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	// database/sql driver registration ("sqlite3").
	_ "github.com/ncruces/go-sqlite3/driver"
)

// driverName is the registered ncruces database/sql driver name.
const driverName = "sqlite3"

// Store is an open memory database. Methods are safe for concurrent use as the
// underlying *sql.DB manages a connection pool.
type Store struct {
	db   *sql.DB
	path string

	// Embedder turns written text into vectors (T-042+). Nil → Write errors.
	Embedder embed.Embedder

	migrated bool
	dim      int
}

// Open opens (or creates) the database at path. Use ":memory:" for an ephemeral
// in-memory store. Recommended pragmas (WAL, sane busy timeout) are applied.
func Open(path string) (*Store, error) {
	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("memory: open: %w", err)
	}
	// Single writer, sane wait. WAL gives concurrent readers.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			// WAL is a no-op on :memory:; ignore pragma errors there.
			if path != ":memory:" {
				db.Close()
				return nil, fmt.Errorf("memory: %s: %w", pragma, err)
			}
		}
	}
	return &Store{db: db, path: path}, nil
}

// VecVersion returns the sqlite-vec version string, proving the vector
// extension is loaded into the same database (T-040).
func (s *Store) VecVersion() (string, error) {
	var v string
	if err := s.db.QueryRow("SELECT vec_version()").Scan(&v); err != nil {
		return "", fmt.Errorf("memory: vec_version: %w", err)
	}
	return v, nil
}

// DB exposes the underlying handle for store-internal migrations (T-042+).
// Callers must not close it; use Store.Close.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Write embeds and stores one text chunk (source tags its origin), indexing it
// in both the FTS5 table (lexical) and the vec0 table (semantic) for hybrid
// retrieval (T-042). Returns the chunk row id. Chunking is one-chunk-per-write
// for v1; richer splitting lands later.
func (s *Store) Write(ctx context.Context, text, source string) (int64, error) {
	if s.Embedder == nil {
		return 0, fmt.Errorf("memory: no embedder configured")
	}
	vecs, err := s.Embedder.Embed(ctx, []string{text})
	if err != nil {
		return 0, fmt.Errorf("memory: embed: %w", err)
	}
	if len(vecs) != 1 {
		return 0, fmt.Errorf("memory: embed returned %d vectors", len(vecs))
	}
	dim := len(vecs[0])
	if err := s.ensureSchema(dim); err != nil {
		return 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("memory: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.Exec(`INSERT INTO chunks(text, source) VALUES (?, ?)`, text, source)
	if err != nil {
		return 0, fmt.Errorf("memory: insert chunk: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("memory: last id: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO chunks_fts(rowid, text) VALUES (?, ?)`, id, text); err != nil {
		return 0, fmt.Errorf("memory: insert fts: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO chunks_vec(rowid, embedding) VALUES (?, ?)`, id, serializeVec(vecs[0])); err != nil {
		return 0, fmt.Errorf("memory: insert vec: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("memory: commit: %w", err)
	}
	return id, nil
}

// ensureSchema creates the chunks table, its FTS5 index, and the vec0 table
// (one-shot, dim pinned from the first Write). Subsequent calls are no-ops.
func (s *Store) ensureSchema(dim int) error {
	if s.migrated {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS chunks(id INTEGER PRIMARY KEY, text TEXT, source TEXT)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(text, content='chunks', content_rowid='id')`,
		fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_vec USING vec0(embedding float[%d])`, dim),
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("memory: migrate: %w (stmt: %s)", err, q)
		}
	}
	s.migrated = true
	s.dim = dim
	return nil
}

// serializeVec encodes a float32 vector as the little-endian byte blob vec0
// expects (matches sqlite-vec's SerializeFloat32).
func serializeVec(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}
