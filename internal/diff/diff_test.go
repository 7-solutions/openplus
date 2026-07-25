package diff

import (
	"strings"
	"testing"
)

func TestUnifiedSingleHunk(t *testing.T) {
	a := "alpha\nbeta\ngamma\n"
	b := "alpha\nBETA\ngamma\n"
	got := Unified(a, b)
	// unchanged lines elided; only the changed line shown as -/+.
	if !strings.Contains(got, "- beta") {
		t.Errorf("missing - beta in:\n%s", got)
	}
	if !strings.Contains(got, "+ BETA") {
		t.Errorf("missing + BETA in:\n%s", got)
	}
	if strings.Contains(got, "alpha") || strings.Contains(got, "gamma") {
		t.Errorf("unchanged lines should be elided:\n%s", got)
	}
}

func TestUnifiedInsertion(t *testing.T) {
	a := "a\nc\n"
	b := "a\nb\nc\n"
	got := Unified(a, b)
	if !strings.Contains(got, "+ b") || strings.Contains(got, "- ") {
		t.Errorf("insertion wrong:\n%s", got)
	}
}

func TestUnifiedDeletion(t *testing.T) {
	a := "a\nb\nc\n"
	b := "a\nc\n"
	got := Unified(a, b)
	if !strings.Contains(got, "- b") || strings.Contains(got, "+ ") {
		t.Errorf("deletion wrong:\n%s", got)
	}
}

func TestUnifiedIdenticalEmpty(t *testing.T) {
	if got := Unified("same\nsame\n", "same\nsame\n"); got != "" {
		t.Errorf("identical should be empty, got %q", got)
	}
}
