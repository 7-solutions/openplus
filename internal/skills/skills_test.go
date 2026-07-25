package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func writeSkill(t *testing.T, dir, name, frontmatter, body string) {
	t.Helper()
	p := filepath.Join(dir, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "\n---\n" + body
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func namesOf(ss []Skill) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name
	}
	sort.Strings(out)
	return out
}

func TestDiscoverParsesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "deploy", "name: deploy\ndescription: Deploy the app", "body text")
	idx := NewIndex(dir)
	got, err := idx.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 1 || got[0].Name != "deploy" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Description != "Deploy the app" {
		t.Errorf("description = %q", got[0].Description)
	}
	if got[0].Body != "body text" {
		t.Errorf("body = %q", got[0].Body)
	}
}

func TestDiscoverMultiple(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "deploy", "name: deploy\ndescription: d1", "")
	writeSkill(t, dir, "build", "name: build\ndescription: b1", "")
	got, _ := NewIndex(dir).Discover()
	if !reflect.DeepEqual(namesOf(got), []string{"build", "deploy"}) {
		t.Fatalf("names = %v", namesOf(got))
	}
}

func TestDiscoverOverrideScanOrder(t *testing.T) {
	// earlier dirs are lower priority; later dirs override by name.
	low := t.TempDir()
	high := t.TempDir()
	writeSkill(t, low, "deploy", "name: deploy\ndescription: from-low", "")
	writeSkill(t, high, "deploy", "name: deploy\ndescription: from-high", "")
	writeSkill(t, low, "only-low", "name: only-low\ndescription: x", "")

	idx := NewIndex(low, high) // high wins
	got, err := idx.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	byName := map[string]Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if byName["deploy"].Description != "from-high" {
		t.Errorf("override failed: %q", byName["deploy"].Description)
	}
	if _, ok := byName["only-low"]; !ok {
		t.Error("only-low dropped")
	}
}

func TestDiscoverMissingDirOK(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope")
	got, err := NewIndex(missing).Discover()
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no skills, got %v", got)
	}
}

func TestFindExplicit(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "deploy", "name: deploy\ndescription: d", "")
	idx := NewIndex(dir)
	if _, err := idx.Discover(); err != nil {
		t.Fatal(err)
	}
	s, ok := idx.Find("deploy")
	if !ok || s.Name != "deploy" {
		t.Fatalf("Find: %+v ok=%v", s, ok)
	}
	if _, ok := idx.Find("absent"); ok {
		t.Error("Find(absent) should be false")
	}
}
