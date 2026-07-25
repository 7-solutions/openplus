package tui

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/7solutions/openplus/internal/ports"
)

// T-1712: ContrastRatio is the WCAG relative-luminance contrast between two hex
// colors. Pure function — the foundation every palette's accessibility gate
// depends on.
func TestContrastRatio(t *testing.T) {
	tests := []struct {
		name       string
		a, b       string
		wantApprox float64
	}{
		{"black on white", "#000000", "#ffffff", 21.0},
		{"white on black", "#ffffff", "#000000", 21.0},
		{"identical", "#777777", "#777777", 1.0},
		{"mid grey on white", "#777777", "#ffffff", 4.48}, // known ~4.48 (below AA)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ContrastRatio(tc.a, tc.b)
			if math.Abs(got-tc.wantApprox) > 0.1 {
				t.Fatalf("ContrastRatio(%q,%q) = %.3f, want ~%.2f", tc.a, tc.b, got, tc.wantApprox)
			}
			// order-independent
			rev := ContrastRatio(tc.b, tc.a)
			if math.Abs(got-rev) > 1e-9 {
				t.Fatalf("ContrastRatio not symmetric: %f vs %f", got, rev)
			}
		})
	}
}

func TestContrastRatioBadHex(t *testing.T) {
	if r := ContrastRatio("nope", "#ffffff"); r >= 0 {
		t.Fatalf("bad hex should return < 0 (sentinel), got %f", r)
	}
}

// T-1710: a Palette is a set of semantic color roles; every role is a valid hex.
func TestPaletteRoles(t *testing.T) {
	p := Default()
	if p.Name != "default" {
		t.Fatalf("Default().Name = %q, want %q", p.Name, "default")
	}
	for _, role := range []string{p.Background, p.Text, p.Prompt, p.Working, p.User, p.Assistant, p.Error, p.Accent} {
		if _, ok := relLuminance(role); !ok {
			t.Fatalf("Default palette role not a valid hex: %q", role)
		}
	}
}

// T-1713: every shipped palette meets WCAG AA — each gated foreground role vs the
// background is >= 4.5:1. This is the accessibility gate; a bad edit fails here.
func TestPalettesContrastFloor(t *testing.T) {
	const floor = 4.5
	for _, p := range Palettes() {
		for _, fg := range p.gatedForegrounds() {
			if r := ContrastRatio(fg, p.Background); r < floor {
				t.Errorf("palette %q: role %q vs bg %q = %.2f, want >= %.1f",
					p.Name, fg, p.Background, r, floor)
			}
		}
	}
}

// T-1714: the four palettes ship, each named uniquely.
func TestPalettesSet(t *testing.T) {
	want := map[string]bool{"default": true, "deutan": true, "protan": true, "tritan": true}
	seen := map[string]int{}
	for _, p := range Palettes() {
		seen[p.Name]++
	}
	for name := range want {
		if seen[name] == 0 {
			t.Errorf("missing palette %q", name)
		}
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("palette %q listed %d times", name, n)
		}
	}
}

// T-1715: a fresh model uses the default palette; WithTheme and a themeMsg swap it.
func TestModelThemeDefaultAndSwap(t *testing.T) {
	m := New(&stubRunner{}, "sys")
	if m.theme.Name != "default" {
		t.Fatalf("fresh model theme = %q, want default", m.theme.Name)
	}
	m2 := m.WithTheme(tritanPalette())
	if m2.theme.Name != "tritan" {
		t.Fatalf("WithTheme = %q, want tritan", m2.theme.Name)
	}
	upd, _ := m.Update(themeMsg{name: "deutan"})
	mm, ok := upd.(Model)
	if !ok || mm.theme.Name != "deutan" {
		t.Fatalf("themeMsg did not set deutan; got %+v", mm.theme)
	}
	// unknown name is a no-op (keeps current theme)
	upd2, _ := m.Update(themeMsg{name: "nope"})
	if mm2, ok := upd2.(Model); ok && mm2.theme.Name != "default" {
		t.Fatalf("unknown theme should keep current; got %q", mm2.theme.Name)
	}
}

// T-1715: no raw lipgloss.Color()/ANSI color literal outside theme.go — all color
// lives in the palette. A regression here means someone inlined a color.
func TestNoColorLiteralsOutsideTheme(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, f := range files {
		if f == "theme.go" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if bytes.Contains(src, []byte("lipgloss.Color(")) {
			t.Errorf("%s: raw lipgloss.Color() forbidden outside theme.go; use a Palette role style", f)
		}
	}
}

// T-1716: within a palette the meaningful accent roles (Prompt/Error/Accent) never
// alias — they must stay distinguishable even when the terminal downgrades color to
// luminance/bold (Lipgloss's colorprofile). Aliasing would collapse them.
func TestPaletteAccentsAreDistinct(t *testing.T) {
	for _, p := range Palettes() {
		accents := []string{p.Prompt, p.Error, p.Accent}
		for i := range len(accents) {
			for j := i + 1; j < len(accents); j++ {
				if accents[i] == accents[j] {
					t.Errorf("palette %q: accent roles alias (%s)", p.Name, accents[i])
				}
			}
		}
	}
}

// T-1716: View renders its content under NO_COLOR without panic and without losing
// the prompt/working text — color downgrade must not erase meaning.
func TestViewRendersUnderNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := New(&stubRunner{}, "sys")
	m.h = 40
	m.w = 80
	m.busy = true
	m.pending = &ports.ToolCall{Name: "bash", Input: []byte("{}")}
	out := m.View()
	for _, want := range []string{"working", "allow bash"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() under NO_COLOR missing %q", want)
		}
	}
}

// T-1717: ResolveTheme maps a config name to a palette. Empty means default with
// no warning; an unknown name falls back to default and returns a warning, since
// an appearance setting must never block a session.
func TestResolveTheme(t *testing.T) {
	p, warn := ResolveTheme("")
	if p.Name != "default" || warn != "" {
		t.Fatalf(`ResolveTheme("") = (%q,%q), want ("default","")`, p.Name, warn)
	}
	p, warn = ResolveTheme("tritan")
	if p.Name != "tritan" || warn != "" {
		t.Fatalf(`ResolveTheme("tritan") = (%q,%q)`, p.Name, warn)
	}
	p, warn = ResolveTheme("bogus")
	if p.Name != "default" {
		t.Fatalf(`ResolveTheme("bogus") palette = %q, want default`, p.Name)
	}
	if !strings.Contains(warn, "bogus") {
		t.Fatalf("warning should name the bad theme, got %q", warn)
	}
	for _, name := range PaletteNames() {
		if !strings.Contains(warn, name) {
			t.Errorf("warning should list %q as a valid theme, got %q", name, warn)
		}
	}
}

// T-1717: PaletteNames lists every shipped palette, sorted, with default first —
// the order /theme and the warning present them in.
func TestPaletteNames(t *testing.T) {
	names := PaletteNames()
	if len(names) != len(Palettes()) {
		t.Fatalf("PaletteNames = %v, want %d entries", names, len(Palettes()))
	}
	if names[0] != "default" {
		t.Errorf("default should be listed first, got %v", names)
	}
}

// T-1718: ThemeControl is the runtime's Themer seam. It validates the name, sends
// a themeMsg into the program, and tracks what is active so /theme can mark it.
func TestThemeControl(t *testing.T) {
	var sent []tea.Msg
	c := NewThemeControl(func(m tea.Msg) { sent = append(sent, m) }, "default")

	if got := c.Theme(); got != "default" {
		t.Fatalf("Theme() = %q, want default", got)
	}
	if len(c.ThemeNames()) != len(Palettes()) {
		t.Fatalf("ThemeNames = %v", c.ThemeNames())
	}

	if err := c.SetTheme("protan"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if got := c.Theme(); got != "protan" {
		t.Fatalf("after SetTheme, Theme() = %q", got)
	}
	if len(sent) != 1 {
		t.Fatalf("want one message sent, got %v", sent)
	}
	if tm, ok := sent[0].(themeMsg); !ok || tm.name != "protan" {
		t.Fatalf("sent %#v, want themeMsg{protan}", sent[0])
	}

	// Unknown: refused, nothing sent, active unchanged.
	if err := c.SetTheme("bogus"); err == nil {
		t.Fatal("SetTheme(bogus) should error")
	}
	if len(sent) != 1 {
		t.Errorf("unknown theme should send nothing, got %v", sent)
	}
	if got := c.Theme(); got != "protan" {
		t.Errorf("unknown theme changed active to %q", got)
	}
}
