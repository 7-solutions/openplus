// Package diff produces a minimal line-level unified diff for the edit tool's
// diff view (T-031). It is a single-hunk prefix/suffix matcher: good enough for
// the Edit tool's exact-string replacements, and dependency-free.
package diff

import "strings"

// Unified returns a +/- line diff of two strings. Common leading and trailing
// lines are elided; the changed middle is shown with "- " (removed) and "+ "
// (added) prefixes. Returns "" when a and b are identical.
func Unified(a, b string) string {
	la, lb := splitLines(a), splitLines(b)

	prefix := 0
	for prefix < len(la) && prefix < len(lb) && la[prefix] == lb[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(la)-prefix && suffix < len(lb)-prefix &&
		la[len(la)-1-suffix] == lb[len(lb)-1-suffix] {
		suffix++
	}

	var out strings.Builder
	for _, l := range la[prefix : len(la)-suffix] {
		out.WriteString("- " + l + "\n")
	}
	for _, l := range lb[prefix : len(lb)-suffix] {
		out.WriteString("+ " + l + "\n")
	}
	return out.String()
}

// splitLines splits s into lines, each without the trailing "\n". A trailing
// newline does not produce an empty final element.
func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
