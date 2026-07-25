// Package skills implements the SkillIndex port (ADR-0002): discovery of
// SKILL.md files across a priority-ordered list of directories, plus lookup.
// A skill is a directory containing SKILL.md with YAML-ish frontmatter
// (name, description) followed by the instruction body.
//
// Scan order is lowest-priority first: a skill discovered in a later directory
// overrides an earlier one with the same name (project skills beat user skills
// beat built-ins).
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill is one discovered skill.
type Skill struct {
	// Name is the frontmatter name (falls back to the directory name).
	Name string
	// Description is the frontmatter description — the text ranked against a
	// query for auto-loading (T-051).
	Description string
	// Body is the instruction text after the frontmatter block.
	Body string
	// Path is the SKILL.md file the skill was loaded from.
	Path string
	// Dir is the skill's own directory (Path's parent).
	Dir string
}

// Index discovers and holds skills across a priority-ordered set of roots.
type Index struct {
	// roots are scanned in order; later roots override earlier ones by name.
	roots []string

	byName map[string]Skill
	order  []string // sorted skill names for stable iteration
}

// NewIndex builds an Index over roots, lowest priority first.
func NewIndex(roots ...string) *Index {
	return &Index{roots: roots, byName: map[string]Skill{}}
}

// Discover scans every root for <root>/<skill>/SKILL.md, parsing frontmatter.
// Later roots override earlier ones by skill name. A missing root is skipped
// silently (not every root exists in every project). The returned slice is
// sorted by name for determinism.
func (idx *Index) Discover() ([]Skill, error) {
	idx.byName = map[string]Skill{}

	for _, root := range idx.roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue // root absent — fine
			}
			return nil, fmt.Errorf("skills: read %s: %w", root, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(root, e.Name(), "SKILL.md")
			raw, err := os.ReadFile(path)
			if err != nil {
				continue // directory without a SKILL.md is not a skill
			}
			s := parseSkill(string(raw))
			if s.Name == "" {
				s.Name = e.Name() // fall back to the directory name
			}
			s.Path = path
			s.Dir = filepath.Join(root, e.Name())
			idx.byName[s.Name] = s // later root wins
		}
	}

	idx.order = make([]string, 0, len(idx.byName))
	for name := range idx.byName {
		idx.order = append(idx.order, name)
	}
	sort.Strings(idx.order)

	out := make([]Skill, 0, len(idx.order))
	for _, name := range idx.order {
		out = append(out, idx.byName[name])
	}
	return out, nil
}

// All returns the discovered skills, sorted by name.
func (idx *Index) All() []Skill {
	out := make([]Skill, 0, len(idx.order))
	for _, name := range idx.order {
		out = append(out, idx.byName[name])
	}
	return out
}

// Find looks up a skill by exact name — the explicit `/skill <name>` path.
func (idx *Index) Find(name string) (Skill, bool) {
	s, ok := idx.byName[name]
	return s, ok
}

// parseSkill splits frontmatter from body. Frontmatter is a leading "---" line,
// simple "key: value" pairs, and a closing "---" line. Unknown keys are
// ignored; a file without frontmatter is all body.
func parseSkill(content string) Skill {
	var s Skill
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		s.Body = strings.TrimSpace(content)
		return s
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		// unterminated frontmatter — treat the whole file as body
		s.Body = strings.TrimSpace(content)
		return s
	}

	for _, line := range lines[1:end] {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			s.Name = strings.TrimSpace(value)
		case "description":
			s.Description = strings.TrimSpace(value)
		}
	}
	s.Body = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return s
}
