// Package memory — hybrid retrieval (T-042; change 0020 vector-only;
// change 0021 restored the lexical half via an FTS5 shadow index;
// change 0024 made the RRF fusion tunable).
//
// Search fuses two ranked lists via weighted Reciprocal Rank Fusion:
//   - vector KNN: Turso's vector_distance_cos against the embedding column.
//   - lexical bm25: the modernc.org/sqlite FTS5 shadow index (when present).
//
// Each half contributes w/(K+rank) where w is the half's weight and K the
// rank-damping constant (standard 60), both from Store.rrf (DefaultRRF =
// {60, 1, 1} — the change-0021 equal-weight behavior). When the shadow is
// absent (Open without WithFTS), Search is vector-only; LexicalWeight has
// no effect. See WithRRF / RRFConfig.
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
	Score  float64 // fused weighted-RRF score (higher = more relevant)
}

// Search runs hybrid vector+lexical retrieval, returning the top-k chunks
// ranked by weighted Reciprocal Rank Fusion of the two halves. Returns nil
// (no error) if nothing has been written yet.
//
// The fusion weights and K come from Store.rrf (DefaultRRF = {60, 1, 1},
// applied by Open). Override with the WithRRF option. When the FTS shadow
// is absent, Search is the change-0020 vector-only path.
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
	// Contribution is VectorWeight/(K+rank) (change 0024).
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
		scores[id] += s.rrf.VectorWeight / (s.rrf.K + float64(rank))
	}
	vecRows.Close()

	// Lexical half (change 0021; weighted in 0024): when the FTS shadow is
	// present, fuse its bm25-ranked results into the same score map. The
	// shadow returns raw 1/(K+rank) contributions; Search scales them by
	// LexicalWeight so both weights live in one config. A chunk the vector
	// half ranked outside its top-k can still surface here, because RRF
	// unions the two ranked lists before trimming to k.
	if s.fts != nil {
		ftsScores, err := s.fts.search(ctx, query, k, s.rrf.K)
		if err != nil {
			return nil, fmt.Errorf("memory: fts search: %w", err)
		}
		for id, contribution := range ftsScores {
			scores[id] += s.rrf.LexicalWeight * contribution
		}
	}

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
