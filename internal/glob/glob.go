// Package glob is a dep-free path-glob matcher supporting *, ?, and **.
// ** matches zero or more path segments (recursive); * and ? match within a
// single segment. Shared by the tool and policy packages.
package glob

import (
	"path/filepath"
	"strings"
)

// Match reports whether path matches pattern. Pattern segments are split on
// "/". A leading "/" or "./" in either operand is treated per filepath
// semantics after ToSlash normalization.
func Match(pattern, path string) bool {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func matchSegments(pat, path []string) bool {
	if len(pat) == 0 {
		return len(path) == 0
	}
	if pat[0] == "**" {
		// ** consumes zero or more leading path segments.
		for i := 0; i <= len(path); i++ {
			if matchSegments(pat[1:], path[i:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	ok, err := filepath.Match(pat[0], path[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], path[1:])
}
