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
	"strings"

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

	// maxEntries caps the stored chunks; oldest are pruned first on each
	// write. Zero means unbounded (the zero-cost path — Write skips the
	// count query entirely).
	maxEntries int

	migrated bool
	dim      int
}

// SetMaxEntries caps the stored chunks at n. Oldest chunks (lowest id) are
// pruned on each Write so the cap is enforced even mid-session. n == 0
// disables the cap. Calling SetMaxEntries(n) with n < 0 is a programming
// error and treated as 0.
func (s *Store) SetMaxEntries(n int) {
	if n < 0 {
		n = 0
	}
	s.maxEntries = n
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

	// Enforce the cap on the just-committed write. Done outside the
	// transaction so the prune never leaves the chunks table inconsistent
	// with its FTS/vec indexes — if prune fails, the new chunk is still
	// committed; we lose the cap by a few rows rather than lose the write.
	// Zero is the unbounded path; no query needed.
	if s.maxEntries > 0 {
		s.pruneToMaxEntries(ctx)
	}

	return id, nil
}

// pruneToMaxEntries deletes the oldest chunks so the row count fits
// maxEntries. Oldest = lowest id (autoincrement from SQLite). Best-effort:
// a prune failure is silently dropped — the cap is best-effort, the write
// is not. All three tables (chunks, chunks_fts, chunks_vec) are pruned
// inside one transaction so Search never sees phantom rowids.
func (s *Store) pruneToMaxEntries(ctx context.Context) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&n); err != nil {
		return
	}
	excess := n - s.maxEntries
	if excess <= 0 {
		return
	}
	// Collect the ids to delete in one round trip. The slice lets us
	// issue one DELETE per index rather than three subqueries.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM chunks ORDER BY id ASC LIMIT ?`, excess)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 0 {
		return
	}
	// Build the IN clause once.
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	in := strings.Join(placeholders, ",")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE id IN (`+in+`)`, args...); err != nil {
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks_fts WHERE rowid IN (`+in+`)`, args...); err != nil {
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks_vec WHERE rowid IN (`+in+`)`, args...); err != nil {
		return
	}
	if err := tx.Commit(); err != nil {
		return
	}
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
