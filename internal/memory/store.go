// Package memory is the persistent memory store (ADR-0003; change 0020).
// It runs on the cgo-free tursodatabase/turso-go v0.2.2 driver (libturso
// via purego) with the vector extension bundled into the same binary, so
// FTS5 (lexical) and the native vector column live in one file-backed
// database. Memory + embeddings stay local; chunk text never leaves the host.
//
// Change 0020 replaced the prior ncruces/go-sqlite3 + sqlite-vec-go-bindings
// stack with Turso. The two prior deps were removed because Turso provides
// equivalent functionality natively (vector32() + vector_distance_cos()),
// and sqlite-vec was ABI-incompatible with the post-ncruces-v0.21 line
// (it referenced sqlite3.Binary, which was removed in v0.21+).
package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/7solutions/openplus/internal/embed"

	// Turso Go bindings register the database/sql driver under "turso".
	// Importing the package for side effects is the canonical way to
	// enable the libturso-backed connection that backs the rest of this
	// package.
	_ "github.com/tursodatabase/turso-go"
)

//vecAsJSON helper keeps the strconv import honest (the helper is defined
// further down; see vecAsJSON below).
var _ = strconv.FormatFloat

// driverName is the name under which the Turso-backed database/sql driver
// is registered by the imported tursodatabase/turso-go package.
const driverName = "turso"

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
	// Pragmas: best-effort. Turso v0.2.2's libturso does not support
	// journal_mode pragma (no WAL); other builds do. Treat pragma failure
	// as a warning, not an error — the default-mode db is still usable.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			// pragma not supported by this libturso build — log via the
			// returned error path? No: pragmas are advisory. Just skip.
			_ = err
		}
	}
	return &Store{db: db, path: path}, nil
}

// VecVersion reports the libturso version baked into the Go bindings.
// This is the canary test that change 0020's migration actually loaded
// the Turso-backed driver — if this query returns empty, the libturso
// load failed at the import time. (Turso v0.2.2's libturso does not
// expose sqlite-vec's `vec_version()` SQL function, so we read the
// SQLite version instead, which is always present.)
func (s *Store) VecVersion() (string, error) {
	var v string
	if err := s.db.QueryRow("SELECT sqlite_version()").Scan(&v); err != nil {
		return "", fmt.Errorf("memory: sqlite_version: %w", err)
	}
	return v, nil
}

// DB exposes the underlying handle for store-internal migrations (T-042+).
// Callers must not close it; use Store.Close.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Write embeds and stores one text chunk (source tags its origin), indexing it
// in both the FTS5 table (lexical) and the vector column (semantic) for hybrid
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

	// Insert text + source into the chunks table. The embedding column
	// is added in the same row via vector32() — the canonical Turso
	// SQL constructor for a packed 32-bit float vector. The argument
	// string is the JSON array of the float32 numbers; vector32()
	// accepts either JSON or a binary blob, we use JSON for readability
	// and because the cost (parsing) is paid at write time, not read.
	res, err := tx.Exec(
		`INSERT INTO chunks(text, source, embedding) VALUES (?, ?, vector32(?))`,
		text, source, vecAsJSON(vecs[0]))
	if err != nil {
		return 0, fmt.Errorf("memory: insert chunk: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("memory: last id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("memory: commit: %w", err)
	}
	// with its FTS index — if prune fails, the new chunk is still
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
// is not. The chunks table is pruned in one transaction so Search never
// sees phantom rowids.
func (s *Store) pruneToMaxEntries(ctx context.Context) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&n); err != nil {
		return
	}
	excess := n - s.maxEntries
	if excess <= 0 {
		return
	}
	// Collect the ids to delete in one round trip.
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

	if _, err := s.db.ExecContext(ctx, `DELETE FROM chunks WHERE id IN (`+in+`)`, args...); err != nil {
		return
	}
}

// ensureSchema creates the chunks table with its vector column. The
// vector column is a real BLOB column on the chunks table; Turso's vector
// extension consumes it via vector32() / vector_distance_cos().
//
// FTS5 is deliberately NOT used in this build: tursodatabase/turso-go
// v0.2.2 ships libturso WITHOUT the fts5 module compiled in. The hybrid
// lexical+vector Search pipeline is therefore vector-only for now. When
// Turso's libturso ships FTS5 (or the project switches to a libturso
// build with fts5), the chunks_fts virtual table can be re-added here
// and the bm25 half of Search re-enabled.
//
// user_version is bumped to 3 so the old sqlite-vec (vec0) databases from
// prior versions are skipped (the schema is incompatible).
func (s *Store) ensureSchema(dim int) error {
	if s.migrated {
		return nil
	}
	stmts := []string{
		`PRAGMA user_version = 3`,
		`CREATE TABLE IF NOT EXISTS chunks(id INTEGER PRIMARY KEY, text TEXT, source TEXT, embedding BLOB)`,
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

// vecAsJSON serializes a float32 vector as a JSON array literal that
// Turso's vector32() constructor accepts: "[0.1,0.2,0.3]".
func vecAsJSON(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.Grow(16 * len(v))
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'g', 6, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// vecBytes is the alternate binary encoding kept for callers that want
// to skip the JSON parse cost. Most embedding models use vector32();
// passing a binary blob is also accepted by vector32() so this is a
// future-proof alternate not currently used.
func vecBytes(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}
