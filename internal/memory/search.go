// Package memory — hybrid retrieval (T-042, change 0020).
//
// Search uses Turso's vector_distance_cos against the embedded column on
// the chunks table. The RRF fusion harness is preserved so the lexical
// (FTS5) half can be re-added in a later change when Turso ships a
// libturso build with the fts5 module compiled in.
package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Result is one ranked retrieval hit.
type Result struct {
	ID     int64
	Text   string
	Source string
	Score  float64 // fused RRF score (higher = more relevant)
}

// rrfK is the Reciprocal Rank Fusion constant (standard value 60).
const rrfK = 60.0

// Search runs vector-distance retrieval, returning the top-k chunks
// ordered by Turso's vector_distance_cos. Returns nil (no error) if
// nothing has been written yet.
//
// Hybrid lexical+vector search is the post-0020 stretch goal; the RRF
// harness is preserved in case the lexical half returns in a future
// change. Until then, Search is a single-source vector KNN.
func (s *Store) Search(ctx context.Context, query string, k int) ([]Result, error) {
	if !s.migrated || s.Embedder == nil {
		return nil, nil
	}
	if k <= 0 {
		return nil, nil
	}

	vecs, err := s.Embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("memory: embed query: %w", err)
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("memory: embed query returned %d vectors", len(vecs))
	}
	qvec := vecAsJSON(vecs[0])

	scores := map[int64]float64{}

	// Vector KNN via Turso's vector_distance_cos on the embedded column.
	// The column is on the chunks table itself (post-0020), so the FROM
	// is chunks, not chunks_vec. ORDER BY distance gives the KNN order.
	vecRows, err := s.db.QueryContext(ctx,
		`SELECT id FROM chunks
		 ORDER BY vector_distance_cos(embedding, vector32(?))
		 LIMIT ?`,
		qvec, k)
	if err != nil {
		return nil, fmt.Errorf("memory: vec query: %w", err)
	}
	for rank := 0; vecRows.Next(); rank++ {
		var id int64
		if err := vecRows.Scan(&id); err != nil {
			vecRows.Close()
			return nil, err
		}
		scores[id] += 1.0 / (rrfK + float64(rank))
	}
	vecRows.Close()

	if len(scores) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return scores[ids[i]] > scores[ids[j]] })
	if len(ids) > k {
		ids = ids[:k]
	}

	// Fetch text/source for the survivors.
	q, args := idsQuery(ids)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: fetch rows: %w", err)
	}
	defer rows.Close()
	byID := map[int64]Result{}
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.Text, &r.Source); err != nil {
			return nil, err
		}
		r.Score = scores[r.ID]
		byID[r.ID] = r
	}
	out := make([]Result, 0, len(ids))
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// idsQuery builds a "SELECT id, text, source FROM chunks WHERE id IN (?,?,...)"
// statement plus its args, handling the empty case defensively.
func idsQuery(ids []int64) (string, []any) {
	if len(ids) == 0 {
		return "SELECT id, text, source FROM chunks WHERE 0", nil
	}
	q := "SELECT id, text, source FROM chunks WHERE id IN (?" + strings.Repeat(",?", len(ids)-1) + ")"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return q, args
}
