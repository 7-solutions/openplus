package skills

import (
	"testing"
)

// rankFixture builds an index with three skills whose descriptions are
// deliberately distinct so ranking is unambiguous.
func rankFixture(t *testing.T) *Index {
	t.Helper()
	dir := t.TempDir()
	writeSkill(t, dir, "deploy", "name: deploy\ndescription: Deploy the service to kubernetes production", "")
	writeSkill(t, dir, "migrate", "name: migrate\ndescription: Run database schema migrations safely", "")
	writeSkill(t, dir, "lint", "name: lint\ndescription: Format and lint Go source files", "")
	idx := NewIndex(dir)
	if _, err := idx.Discover(); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return idx
}

func TestTokenize(t *testing.T) {
	got := tokenize("Deploy the Service, to Kubernetes-production!")
	want := []string{"deploy", "the", "service", "to", "kubernetes", "production"}
	if len(got) != len(want) {
		t.Fatalf("tokenize = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tokenize = %v, want %v", got, want)
		}
	}
}

func TestRankPutsBestMatchFirst(t *testing.T) {
	idx := rankFixture(t)
	got := idx.Rank("kubernetes deploy", 3)
	if len(got) == 0 {
		t.Fatal("no matches")
	}
	if got[0].Skill.Name != "deploy" {
		t.Fatalf("top = %q, want deploy (all=%v)", got[0].Skill.Name, matchNames(got))
	}
	if got[0].Score <= 0 {
		t.Errorf("score = %v, want > 0", got[0].Score)
	}
}

func TestRankDatabaseQuery(t *testing.T) {
	idx := rankFixture(t)
	got := idx.Rank("database migration", 3)
	if len(got) == 0 || got[0].Skill.Name != "migrate" {
		t.Fatalf("top = %v, want migrate", matchNames(got))
	}
}

func TestRankExcludesNonMatching(t *testing.T) {
	idx := rankFixture(t)
	got := idx.Rank("kubernetes", 5)
	// only the deploy skill mentions kubernetes; the others score zero and are
	// dropped entirely.
	if len(got) != 1 || got[0].Skill.Name != "deploy" {
		t.Fatalf("matches = %v, want only deploy", matchNames(got))
	}
}

func TestRankNoMatchIsEmpty(t *testing.T) {
	idx := rankFixture(t)
	if got := idx.Rank("quantum entanglement", 5); len(got) != 0 {
		t.Fatalf("expected no matches, got %v", matchNames(got))
	}
}

func TestRankRespectsTopK(t *testing.T) {
	idx := rankFixture(t)
	// a query hitting every description ("the"/"and" are common; use words that
	// appear across skills)
	got := idx.Rank("deploy database go", 2)
	if len(got) > 2 {
		t.Fatalf("got %d matches, want <= 2", len(got))
	}
}

func TestAutoLoadAboveThreshold(t *testing.T) {
	idx := rankFixture(t)
	// a strong, specific query should clear the threshold
	got := idx.AutoLoad("deploy to kubernetes production", 1)
	if len(got) != 1 || got[0].Name != "deploy" {
		t.Fatalf("AutoLoad = %v, want [deploy]", skillNames(got))
	}
}

func TestAutoLoadBelowThresholdLoadsNothing(t *testing.T) {
	idx := rankFixture(t)
	if got := idx.AutoLoad("quantum entanglement", 3); len(got) != 0 {
		t.Fatalf("AutoLoad = %v, want none", skillNames(got))
	}
}

func TestAutoLoadRespectsMax(t *testing.T) {
	idx := rankFixture(t)
	got := idx.AutoLoad("deploy database lint go kubernetes migrations", 1)
	if len(got) > 1 {
		t.Fatalf("AutoLoad returned %d, want <= 1", len(got))
	}
}

func matchNames(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Skill.Name
	}
	return out
}

func skillNames(ss []Skill) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name
	}
	return out
}
