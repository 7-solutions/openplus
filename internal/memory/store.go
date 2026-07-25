// Package memory is the persistent memory store (ADR-0013/0014; changes
// 0020/0021/0022). It runs on the cgo-free turso.tech/database/tursogo
// v0.7.1 driver (libturso via purego), with the native vector column
// (vector32() + vector_distance_cos()) on the primary Turso database.
// The lexical half of hybrid retrieval runs on a separate cgo-free
// modernc.org/sqlite FTS5 shadow index (change 0021); Turso's libturso
// ships no fts5 module, so the two engines are composed behind this
// package's Store. Memory + embeddings stay local; chunk text never
// leaves the host.
//
// Change 0020 replaced the prior ncruces/go-sqlite3 + sqlite-vec-go-bindings
// stack with Turso. Change 0022 re-targeted from the archived
// github.com/tursodatabase/turso-go to the canonical turso.tech/database/
// tursogo path (ADR-0015).
package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/7solutions/openplus/internal/embed"

	// Turso Go bindings register the database/sql driver under "turso".
	// Importing the package for side effects is the canonical way to
	// enable the libturso-backed connection that backs the rest of this
	// package. Canonical module path: turso.tech/database/tursogo
	// (monorepo github.com/tursodatabase/turso @ bindings/go).
	_ "turso.tech/database/tursogo"
)

//vecAsJSON helper keeps the strconv import honest (the helper is defined
// further down; see vecAsJSON below).
var _ = strconv.FormatFloat

// driverName is the name under which the Turso-backed database/sql driver
// is registered by the imported turso.tech/database/tursogo package.
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

	// fts is the optional lexical shadow index (change 0021): a derived
	// FTS5 index over (rowid, text) backed by modernc.org/sqlite. Nil
	// means vector-only — the backward-compatible default from 0020.
	fts *ftsIndex

	// rrf tunes Reciprocal Rank Fusion of the vector and lexical halves
	// (change 0024). Open initializes it to DefaultRRF() so the store
	// behaves identically to pre-0024; WithRRF overrides it. When fts is
	// nil, LexicalWeight has no effect (there is no lexical half to weight).
	rrf RRFConfig

	// wantFTS is set by the WithFTS option; Open acts on it to open the
	// shadow once the primary handle is ready.
	wantFTS bool

	migrated bool
	dim      int
}

// RRFConfig tunes Reciprocal Rank Fusion of the two retrieval halves in
// Search. The fused score for a chunk is:
//
//	VectorWeight/(K + vectorRank) + LexicalWeight·(1/(K + lexicalRank))
//
// where each rank is 0-indexed and a half that does not return the chunk
// contributes 0 for that half. K is the rank-damping constant (the standard
// RRF value is 60); lower K makes top ranks dominate more. DefaultRRF()
// returns the proven-neutral {60, 1, 1} — equal weights, standard K — which
// reproduces the change-0021 fusion exactly.
type RRFConfig struct {
	K             float64 // rank-damping constant; standard 60. Must be > 0 for a physical fusion.
	VectorWeight  float64 // weight on the vector-KNN half. 0 = disable the vector contribution.
	LexicalWeight float64 // weight on the lexical-bm25 half. 0 = disable the lexical contribution.
}

// DefaultRRF returns the neutral RRF config: K=60, both weights 1.0. This is
// the change-0021 equal-weight behavior and the value Open applies when no
// WithRRF option is passed. Tests that assert exact hybrid scores (e.g. the
// +1/60 lexical boost) rely on these defaults.
func DefaultRRF() RRFConfig {
	return RRFConfig{K: 60.0, VectorWeight: 1.0, LexicalWeight: 1.0}
}

// OpenOption configures an Open call. The zero-value option set gives the
// change 0020 vector-only behavior; WithFTS opts into hybrid retrieval.
type OpenOption func(*Store)

// WithFTS enables the FTS5 lexical shadow index for hybrid retrieval. The
// shadow lives at a path derived from the primary (":memory:" → ":memory:",
// or "<base>.fts.db" beside the primary file). It is a derived index,
// reconstructable via RebuildFTS.
func WithFTS() OpenOption {
	return func(s *Store) { s.wantFTS = true }
}

// WithRRF overrides the store's Reciprocal Rank Fusion config (change 0024).
// The config is applied as-is — there is no zero-value magic, so a caller
// passing WithRRF(RRFConfig{}) gets {0,0,0} (their explicit choice to zero
// out the fusion). The neutral default comes from DefaultRRF(), which Open
// applies before options; most callers either omit WithRRF entirely or pass
// DefaultRRF() with a field or two adjusted.
func WithRRF(cfg RRFConfig) OpenOption {
	return func(s *Store) { s.rrf = cfg }
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
//
// Options: pass WithFTS() to enable the FTS5 lexical shadow index for hybrid
// (lexical+vector) retrieval. Without it, the store is vector-only (the
// change 0020 default). The shadow lives at a derived sibling path.
func Open(path string, opts ...OpenOption) (*Store, error) {
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
	s := &Store{db: db, path: path, rrf: DefaultRRF()}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	if s.wantFTS {
		shadow, err := openFTS(shadowPath(path))
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("memory: open fts shadow: %w", err)
		}
		s.fts = shadow
	}
	return s, nil
}

// shadowPath derives the FTS shadow's database path from the primary's.
// ":memory:" maps to ":memory:" (an independent ephemeral DB linked to the
// primary only by the rowids Write passes). A file path maps to a sibling
// "<base>.fts.db" so the shadow persists beside the primary.
func shadowPath(primary string) string {
	if primary == ":memory:" {
		return ":memory:"
	}
	dir := filepath.Dir(primary)
	base := filepath.Base(primary)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return filepath.Join(dir, base+".fts.db")
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

// Close closes the database and, if present, the FTS shadow index.
func (s *Store) Close() error {
	var err error
	if s.fts != nil {
		err = s.fts.close()
	}
	if cerr := s.db.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return err
}

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
	// Index the new chunk into the lexical shadow if present (change 0021).
	// Best-effort: a shadow failure never loses the primary write — the
	// shadow is a derived index, reconstructable via RebuildFTS.
	if s.fts != nil {
		if err := s.fts.index(ctx, id, text); err != nil {
			_ = err // best-effort; see comment above
		}
	}
	// Enforce the cap. Done outside the transaction so the prune never
	// leaves the chunks table inconsistent with itself. Zero is the
	// unbounded path; no query needed.
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
	// Mirror the delete into the lexical shadow so Search never returns
	// phantom lexical hits for pruned chunks (change 0021). Best-effort:
	// a shadow delete failure leaves the shadow over-full, not the
	// primary under-full; the next RebuildFTS reconciles.
	if s.fts != nil {
		_ = s.fts.delete(ctx, ids)
	}
}

// RebuildFTS reconstructs the lexical shadow index from the primary chunks
// table. It clears chunks_fts and re-indexes every (id, text) row. Use this
// to recover from a corrupt or missing shadow file, or after bulk-loading
// chunks while the shadow was disabled. No-op when the shadow is absent
// (vector-only store); returns nil in that case.
func (s *Store) RebuildFTS(ctx context.Context) error {
	if s.fts == nil {
		return nil
	}
	if _, err := s.fts.db.ExecContext(ctx, `DELETE FROM chunks_fts`); err != nil {
		return fmt.Errorf("memory: rebuild clear: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, text FROM chunks`)
	if err != nil {
		return fmt.Errorf("memory: rebuild scan: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var text string
		if err := rows.Scan(&id, &text); err != nil {
			return err
		}
		if err := s.fts.index(ctx, id, text); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ensureSchema creates the chunks table with its vector column. The
// vector column is a real BLOB column on the chunks table; Turso's vector
// extension consumes it via vector32() / vector_distance_cos().
//
// This primary Turso database is vector-only: turso.tech/database/tursogo
// v0.7.1 ships libturso WITHOUT the fts5 module (verified across every
// published binding as of 2026-07-25). The lexical half of hybrid retrieval
// lives in a separate modernc.org/sqlite FTS5 shadow index (change 0021,
// ADR-0014), opened via Open(path, WithFTS()). The shadow is a derived
// index reconstructed from this chunks table by Store.RebuildFTS.
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
