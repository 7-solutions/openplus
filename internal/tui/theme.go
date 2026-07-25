package tui

// theme.go is the ONLY place in package tui that defines colors. The model
// references semantic roles on a Palette (see T-1710); a raw lipgloss.Color or
// ANSI literal anywhere else in the package is a bug. Change 0017 (ADR-0012).

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ContrastRatio is the WCAG 2.x relative-luminance contrast ratio between two hex
// colors ("#rrggbb"), order-independent. It returns the ratio (1.0 = identical,
// 21.0 = black/white), or a negative sentinel if a color cannot be parsed — callers
// gate palettes on a floor of 4.5:1 (WCAG AA for normal text).
func ContrastRatio(a, b string) float64 {
	la, oka := relLuminance(a)
	lb, okb := relLuminance(b)
	if !oka || !okb {
		return -1
	}
	lighter, darker := la, lb
	if lb > la {
		lighter, darker = lb, la
	}
	return (lighter + 0.05) / (darker + 0.05)
}

// relLuminance computes the WCAG relative luminance of a hex color, or ok=false.
func relLuminance(hex string) (float64, bool) {
	r, g, b, ok := parseHexRGB(hex)
	if !ok {
		return 0, false
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b), true
}

// lin maps an sRGB channel [0,1] to its linearized value per WCAG.
func lin(c float64) float64 {
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// parseHexRGB parses "#rrggbb" (or "#rgb") into three [0,1] channels.
func parseHexRGB(s string) (r, g, b float64, ok bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	switch len(s) {
	case 6:
		v, err := strconv.ParseUint(s, 16, 24)
		if err != nil {
			return 0, 0, 0, false
		}
		return ch(v >> 16), ch(v >> 8), ch(v), true
	case 3:
		expanded := string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
		v, err := strconv.ParseUint(expanded, 16, 24)
		if err != nil {
			return 0, 0, 0, false
		}
		return ch(v >> 16), ch(v >> 8), ch(v), true
	default:
		return 0, 0, 0, false
	}
}

// ch scales a uint8 color channel to [0,1].
func ch(v uint64) float64 { return float64(uint8(v)) / 255 }

// Palette is a set of semantic color roles. The model references roles, never raw
// literals; a palette maps roles to hex colors. Adding a role is one field here.
type Palette struct {
	Name       string
	Background string // base background
	Text       string // default body text
	Prompt     string // permission / prompt line
	Working    string // busy indicator (dim; NOT contrast-gated — secondary text)
	User       string // user input echo
	Assistant  string // assistant text
	Error      string // errors
	Accent     string // accents
}

// gatedForegrounds are the primary text roles the contrast floor applies to. Working
// is deliberately dim secondary text and is excluded from the WCAG AA gate.
func (p Palette) gatedForegrounds() []string {
	return []string{p.Text, p.Prompt, p.User, p.Assistant, p.Error, p.Accent}
}

// WorkingStyle renders the busy indicator (dim). Working is the only role allowed
// to drop below the AA floor — it is secondary text.
func (p Palette) WorkingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(p.Working)).Faint(true)
}

// PromptStyle renders the permission / prompt line.
func (p Palette) PromptStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(p.Prompt))
}

// PaletteByName returns the named palette and ok=false if unknown.
func PaletteByName(name string) (Palette, bool) {
	for _, p := range Palettes() {
		if p.Name == name {
			return p, true
		}
	}
	return Palette{}, false
}

// PaletteNames lists every shipped palette in presentation order: the default
// first (it is the one a user gets without choosing), then the rest as declared.
func PaletteNames() []string {
	all := Palettes()
	names := make([]string, 0, len(all))
	for _, p := range all {
		names = append(names, p.Name)
	}
	return names
}

// ResolveTheme maps a configured theme name to a palette. An empty name selects
// the default silently. An unknown name also selects the default but returns a
// non-empty warning naming it and listing the valid ones — appearance is never
// worth failing a session over, but a typo must not be swallowed either.
func ResolveTheme(name string) (Palette, string) {
	if name == "" {
		return Default(), ""
	}
	if p, ok := PaletteByName(name); ok {
		return p, ""
	}
	return Default(), fmt.Sprintf("unknown tui.theme %q; using %q (available: %s)",
		name, Default().Name, strings.Join(PaletteNames(), ", "))
}

// ThemeControl is the front-end side of the runtime's theme seam (T-1718). The
// palette itself lives in the Bubble Tea model, which only the program's own
// goroutine may touch, so switching is a message send; ThemeControl keeps the
// active name so a command can report it without reaching into the model.
//
// It is safe for concurrent use: /theme runs on the command goroutine while the
// program renders on its own.
type ThemeControl struct {
	send func(tea.Msg)

	mu     sync.Mutex
	active string
}

// NewThemeControl returns a control that switches the theme by sending into the
// program (pass program.Send). initial is the palette the model started with.
func NewThemeControl(send func(tea.Msg), initial string) *ThemeControl {
	if initial == "" {
		initial = Default().Name
	}
	return &ThemeControl{send: send, active: initial}
}

// ThemeNames lists the selectable palettes.
func (c *ThemeControl) ThemeNames() []string { return PaletteNames() }

// Theme reports the active palette's name.
func (c *ThemeControl) Theme() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

// SetTheme validates the name and sends the switch into the program. An unknown
// name is refused without sending, so a typo cannot blank the screen.
func (c *ThemeControl) SetTheme(name string) error {
	if _, ok := PaletteByName(name); !ok {
		return fmt.Errorf("tui: unknown theme %q; available: %s",
			name, strings.Join(PaletteNames(), ", "))
	}
	c.mu.Lock()
	c.active = name
	c.mu.Unlock()
	if c.send != nil {
		c.send(themeMsg{name: name})
	}
	return nil
}

// Default is the universal colorblind-safe palette: bright pastels on a dark ground,
// distinguishable by luminance as well as hue, with NO red/green pair (the
// deutan/protan collapse). Active on a fresh session (T-1711).
func Default() Palette {
	return Palette{
		Name:       "default",
		Background: "#1e1e2e",
		Text:       "#cdd6f4",
		Prompt:     "#f9e2af", // yellow
		Working:    "#6c7086", // dim grey (secondary)
		User:       "#89dceb", // cyan
		Assistant:  "#cdd6f4", // light
		Error:      "#eba0ac", // salmon
		Accent:     "#89b4fa", // blue
	}
}

// deutanPalette tunes for deuteranopia (green blindness; red/green collapse).
// Distinguishing pairs avoid red-vs-green; meaning rides on blue/yellow/orange
// and luminance.
func deutanPalette() Palette {
	return Palette{
		Name:       "deutan",
		Background: "#1e1e2e",
		Text:       "#cdd6f4",
		Prompt:     "#f9e2af", // yellow
		Working:    "#6c7086",
		User:       "#89dceb", // cyan
		Assistant:  "#cdd6f4",
		Error:      "#fab387", // orange (deutan-visible)
		Accent:     "#89b4fa", // blue
	}
}

// protanPalette tunes for protanopia (red blindness; red darkens, red/green
// collapse). Red is avoided for meaning; blue/violet/orange carry it.
func protanPalette() Palette {
	return Palette{
		Name:       "protan",
		Background: "#1e1e2e",
		Text:       "#cdd6f4",
		Prompt:     "#f9e2af", // yellow
		Working:    "#6c7086",
		User:       "#89dceb", // cyan
		Assistant:  "#cdd6f4",
		Error:      "#fab387", // orange (not darkened red)
		Accent:     "#cba6f7", // violet (protan-distinguishable)
	}
}

// tritanPalette tunes for tritanopia (blue/yellow collapse). Red and green remain
// distinguishable for tritan observers, so they carry meaning; blue/yellow pairs
// are avoided.
func tritanPalette() Palette {
	return Palette{
		Name:       "tritan",
		Background: "#1e1e2e",
		Text:       "#cdd6f4",
		Prompt:     "#f5c2e7", // mauve (avoids yellow)
		Working:    "#6c7086",
		User:       "#cdd6f4",
		Assistant:  "#cdd6f4",
		Error:      "#f38ba8", // red (tritan-visible)
		Accent:     "#a6e3a1", // green (tritan-distinguishable from red)
	}
}

// Palettes returns every shipped palette.
func Palettes() []Palette {
	return []Palette{Default(), deutanPalette(), protanPalette(), tritanPalette()}
}
