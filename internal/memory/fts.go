// Package memory — FTS5 lexical shadow index (change 0021).
//
// ftsIndex is a derived full-text index over the chunks table's (id, text),
// backed by modernc.org/sqlite (transpiled C→Go, cgo-free, FTS5-capable).
// It exists because Turso's libturso binary does not ship the fts5 module
// (verified 2026-07-25 across both turso-go v0.2.2 and
// turso.tech/database/tursogo v0.7.1). The shadow is a pure derived index:
// reconstructable from chunks at any time via Store.RebuildFTS.
//
// The shadow registers under the database/sql driver name "sqlite", which
// does not collide with the primary "turso" driver — both engines coexist
// in one binary. No core package imports modernc.org/sqlite; this adapter
// is an internal collaborator of Store behind the existing MemoryStore port.
package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	// modernc.org/sqlite registers the database/sql driver "sqlite".
	// Side-effect import is the canonical registration pattern.
	_ "modernc.org/sqlite"
)

// ftsDriverName is the name modernc.org/sqlite registers under.
const ftsDriverName = "sqlite"

// ftsIndex is a derived FTS5 shadow index over (rowid, text). It is NOT a
// port; it is an internal collaborator of Store. A nil *ftsIndex means the
// store runs vector-only (the backward-compatible default from change 0020).
type ftsIndex struct {
	db *sql.DB
}

// openFTS opens (or creates) the shadow index at path. ":memory:" gives an
// ephemeral shadow tied to the process; a file path persists. The chunks_fts
// virtual table is created if absent.
func openFTS(path string) (*ftsIndex, error) {
	db, err := sql.Open(ftsDriverName, path)
	if err != nil {
		return nil, fmt.Errorf("memory: open fts: %w", err)
	}
	// WAL on the shadow gives concurrent-reader semantics matching the
	// primary store. modernc.org/sqlite accepts these pragmas (unlike the
	// Turso libturso build); failure here would indicate a real problem,
	// so it is surfaced rather than swallowed.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("memory: fts %s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(
		`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(text)`,
	); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory: create chunks_fts: %w", err)
	}
	return &ftsIndex{db: db}, nil
}

// index inserts (or replaces) one document into the shadow. The rowid is
// the matching chunks.id, so the shadow is joinable to the primary table
// by rowid. Idempotent on rowid: a re-index of an existing rowid replaces
// the prior text (FTS5 rowid is the key).
func (f *ftsIndex) index(ctx context.Context, id int64, text string) error {
	// DELETE-then-INSERT keeps the index idempotent on rowid without
	// depending on FTS5 special INSERT semantics. FTS5 has no native
	// ON CONFLICT clause; the two-statement form is the portable path.
	if _, err := f.db.ExecContext(ctx,
		`DELETE FROM chunks_fts WHERE rowid = ?`, id); err != nil {
		return fmt.Errorf("memory: fts delete-before-index: %w", err)
	}
	if _, err := f.db.ExecContext(ctx,
		`INSERT INTO chunks_fts(rowid, text) VALUES (?, ?)`, id, text); err != nil {
		return fmt.Errorf("memory: fts index: %w", err)
	}
	return nil
}

// delete removes the given rowids from the shadow. Used by prune and by
// any future explicit delete path. Missing rowids are not an error.
func (f *ftsIndex) delete(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `DELETE FROM chunks_fts WHERE rowid IN (` + strings.Join(placeholders, ",") + `)`
	if _, err := f.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("memory: fts delete: %w", err)
	}
	return nil
}

// search runs an FTS5 MATCH query and returns id -> raw RRF rank contribution
// (1/(rrfK+rank)). The bm25() ordering is applied server-side (most relevant
// first); only the RRF rank leaks out, never bm25's raw (negative) magnitude.
// The Store applies the per-half LexicalWeight at fusion time; this method
// stays a pure lexical ranker and does not know its own weight.
//
// rrfK is the rank-damping constant (standard 60); it is passed in by Search
// so the lexical and vector halves share one config source (Store.rrf.K).
//
// An empty result set returns a non-nil empty map, not nil.
func (f *ftsIndex) search(ctx context.Context, query string, k int, rrfK float64) (map[int64]float64, error) {
	out := map[int64]float64{}
	if k <= 0 || strings.TrimSpace(query) == "" {
		return out, nil
	}
	// bm25() is negative (more negative = better); ASCENDING order puts
	// the best match first. We do not return bm25() itself — only the
	// RRF rank derived from the position in this ordered stream.
	rows, err := f.db.QueryContext(ctx,
		`SELECT rowid FROM chunks_fts
		 WHERE chunks_fts MATCH ?
		 ORDER BY bm25(chunks_fts)
		 LIMIT ?`,
		query, k)
	if err != nil {
		return nil, fmt.Errorf("memory: fts search: %w", err)
	}
	defer rows.Close()
	for rank := 0; rows.Next(); rank++ {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = 1.0 / (rrfK + float64(rank))
	}
	return out, nil
}

// close releases the shadow's database handle.
func (f *ftsIndex) close() error {
	if f == nil || f.db == nil {
		return nil
	}
	return f.db.Close()
}
