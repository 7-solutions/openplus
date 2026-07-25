package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/7-solutions/openplus/internal/config"
)

// themeNames mirrors the palettes internal/tui ships. The command depends only on
// the Themer seam, so the test does not import the front-end.
var themeNames = []string{"default", "deutan", "protan", "tritan"}

// fakeThemer stands in for the attached front-end (change 0017, T-1718). The real
// one sends a message into the Bubble Tea program.
type fakeThemer struct {
	active string
	sets   []string
}

func (f *fakeThemer) ThemeNames() []string { return themeNames }
func (f *fakeThemer) Theme() string        { return f.active }
func (f *fakeThemer) SetTheme(name string) error {
	for _, n := range themeNames {
		if n == name {
			f.active = name
			f.sets = append(f.sets, name)
			return nil
		}
	}
	return fmt.Errorf("unknown theme %q", name)
}

// themeSession builds a session with a config file and an attached themer.
func themeSession(t *testing.T) (*Session, *fakeThemer) {
	t.Helper()
	root := project(t, `{"model":"fake/fake"}`)
	s, err := Assemble(root, Options{Fake: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	f := &fakeThemer{active: "default"}
	s.Theme = f
	return s, f
}

// T-1718: /theme with no argument lists every palette and marks the active one.
func TestCmdThemeLists(t *testing.T) {
	s, _ := themeSession(t)
	out := run(t, s, "/theme")
	for _, name := range themeNames {
		if !strings.Contains(out, name) {
			t.Errorf("/theme output missing %q:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "* default") {
		t.Errorf("/theme should mark the active palette:\n%s", out)
	}
}

// T-1718: /theme <name> switches the front-end and persists to opencode.json, so
// the choice survives a restart.
func TestCmdThemeSwitchesAndPersists(t *testing.T) {
	s, f := themeSession(t)
	out := run(t, s, "/theme deutan")
	if !strings.Contains(out, "deutan") {
		t.Errorf("/theme deutan output = %q", out)
	}
	if f.active != "deutan" {
		t.Fatalf("front-end theme = %q, want deutan", f.active)
	}

	// Persisted: a fresh load of the same project sees it (the restart path).
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.TUI.Theme != "deutan" {
		t.Fatalf("persisted tui.theme = %q, want deutan", cfg.TUI.Theme)
	}
	if cfg.Model != "fake/fake" {
		t.Fatalf("persisting the theme lost the model: %q", cfg.Model)
	}
}

// T-1718: an unknown name is refused with the valid names, and nothing is written
// — a typo must not silently persist an unusable theme.
func TestCmdThemeUnknownRefused(t *testing.T) {
	s, f := themeSession(t)
	err := runErr(t, s, "/theme bogus")
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should name the bad theme: %v", err)
	}
	for _, name := range themeNames {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error should list %q: %v", name, err)
		}
	}
	if len(f.sets) != 0 {
		t.Errorf("front-end was switched anyway: %v", f.sets)
	}
	cfg, err2 := config.Load(s.ConfigPath)
	if err2 != nil {
		t.Fatalf("reload: %v", err2)
	}
	if cfg.TUI.Theme != "" {
		t.Errorf("bad theme persisted: %q", cfg.TUI.Theme)
	}
}

// T-1718: with no front-end attached (headless), /theme reports that rather than
// pretending to switch. Theming is a front-end capability, not a session one.
func TestCmdThemeHeadless(t *testing.T) {
	s, _ := themeSession(t)
	s.Theme = nil

	if err := runErr(t, s, "/theme"); !strings.Contains(err.Error(), "front-end") {
		t.Errorf("headless list error = %v", err)
	}
	if err := runErr(t, s, "/theme deutan"); !strings.Contains(err.Error(), "front-end") {
		t.Errorf("headless switch error = %v", err)
	}
}
