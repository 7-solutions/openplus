package skills

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// BM25 parameters (Okapi BM25 defaults). k1 controls term-frequency
// saturation; b controls length normalization.
const (
	bm25K1 = 1.2
	bm25B  = 0.75

	// AutoLoadThreshold is the minimum BM25 score a skill must reach before it
	// is injected into the prompt without the user asking for it. Tuned so that
	// a single incidental word match does not drag a skill in.
	AutoLoadThreshold = 0.5
)

// Match is a ranked skill with its BM25 score.
type Match struct {
	Skill Skill
	Score float64
}

// Rank scores every discovered skill against the query with BM25 over its
// name + description, returning the top k matches (highest first). Skills with
// a non-positive score are excluded entirely.
func (idx *Index) Rank(query string, k int) []Match {
	if k <= 0 {
		return nil
	}
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}

	skills := idx.All()
	if len(skills) == 0 {
		return nil
	}

	// Build the corpus: one document per skill (name + description).
	docs := make([][]string, len(skills))
	totalLen := 0
	for i, s := range skills {
		docs[i] = tokenize(s.Name + " " + s.Description)
		totalLen += len(docs[i])
	}
	avgLen := float64(totalLen) / float64(len(docs))

	// Document frequency per query term.
	df := map[string]int{}
	for _, term := range terms {
		if _, seen := df[term]; seen {
			continue
		}
		for _, doc := range docs {
			if containsTerm(doc, term) {
				df[term]++
			}
		}
	}

	n := float64(len(docs))
	matches := make([]Match, 0, len(docs))
	for i, doc := range docs {
		score := 0.0
		docLen := float64(len(doc))
		for _, term := range terms {
			freq := float64(countTerm(doc, term))
			if freq == 0 {
				continue
			}
			// Okapi BM25 IDF with the standard +1 smoothing (always positive).
			idf := math.Log(1 + (n-float64(df[term])+0.5)/(float64(df[term])+0.5))
			denom := freq + bm25K1*(1-bm25B+bm25B*docLen/avgLen)
			score += idf * (freq * (bm25K1 + 1) / denom)
		}
		if score > 0 {
			matches = append(matches, Match{Skill: skills[i], Score: score})
		}
	}

	sort.Slice(matches, func(a, b int) bool {
		if matches[a].Score != matches[b].Score {
			return matches[a].Score > matches[b].Score
		}
		return matches[a].Skill.Name < matches[b].Skill.Name // stable tie-break
	})
	if len(matches) > k {
		matches = matches[:k]
	}
	return matches
}

// AutoLoad returns up to max skills whose BM25 score clears AutoLoadThreshold —
// the set worth injecting without an explicit request. Returns nil when nothing
// is relevant enough.
func (idx *Index) AutoLoad(query string, max int) []Skill {
	matches := idx.Rank(query, max)
	out := make([]Skill, 0, len(matches))
	for _, m := range matches {
		if m.Score >= AutoLoadThreshold {
			out = append(out, m.Skill)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// tokenize lowercases and splits on any non-letter/non-digit rune.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return fields
}

func containsTerm(doc []string, term string) bool {
	for _, w := range doc {
		if w == term {
			return true
		}
	}
	return false
}

func countTerm(doc []string, term string) int {
	n := 0
	for _, w := range doc {
		if w == term {
			n++
		}
	}
	return n
}
