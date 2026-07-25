package glob

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "pkg/main.go", false}, // * does not cross /
		{"**/*.go", "main.go", true},   // ** matches zero segments
		{"**/*.go", "a/b/c.go", true},
		{"**/*.go", "a/b/c.md", false},
		{"src/**", "src/x/y", true},
		{"src/**", "out/x", false},
		{"**", "anything/here", true},
		{"a/*/c", "a/b/c", true},
		{"a/*/c", "a/b/x/c", false}, // single * = one segment
		{"a/**/c", "a/b/x/c", true}, // ** = any segments
		{"foo.go", "foo.go", true},
		{"foo.go", "bar.go", false},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.path); got != c.want {
			t.Errorf("Match(%q,%q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}
