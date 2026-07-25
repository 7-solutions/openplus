# ADR-0012 — Color-vision-abnormalities theme

**Status:** Accepted

## Context
The TUI renders color with a single hardcoded ANSI code and no palette. For users
with color vision deficiencies (deuteranopia, protanopia, tritanopia), hue-only
distinctions collapse and the interface becomes hard to read. There was no
accessibility story and no `tui` capability spec — `internal/tui` was unspecified.

Unlike MCP or Max Mode, this is **not** a refuse-in-v1 item; no trigger was required.
It is a new accessibility capability.

## Decision
Add a central, switchable theme in `internal/tui`. A `Palette` of **semantic roles**
(prompt, working indicator, user/assistant text, error, accents) is the only place
colors live; the model references roles, never literals. Four palettes ship: a
universal colorblind-safe **default** (Okabe–Ito/Wong-derived, hues distinguishable
by luminance as well as hue, no red/green pairs) and **deutan**, **protan**,
**tritan** overrides.

Accessibility is **verified, not eyeballed**: a test computes WCAG relative-luminance
contrast for every text/background pair in every palette and fails below AA (4.5:1),
so a future edit cannot silently regress it. A lint gate keeps raw color literals out
of the model package — all color lives in `theme.go`. Colors are authored as hex and
degrade to the terminal's capability via Lipgloss's colorprofile; a constrained
terminal still reads via luminance/bold.

`/theme` lists palettes; `/theme <name>` switches at runtime and persists via a
`tui.theme` config field. The user picks — there is no in-terminal CVD auto-detect
(no reliable probe). Theming is appearance-only: no behavior, layout, key, core-port,
or provider-surface change. Lipgloss is already a pure-Go dependency, so the build
stays cgo-free.

This ADR introduces the `tui` capability (spec first defined by change 0017).

## Consequences
- (+) The TUI is readable for color-vision-deficient users, with a safe default that
  needs no opt-in.
- (+) Accessibility is a enforced gate, not a hope — contrast and no-literal rules
  are tested.
- (+) Centralizing color behind roles makes future restyling cheap and consistent.
- (−) Four palettes must be designed and kept contrast-valid under maintenance.
- (−) No auto-detection: users must know to run `/theme` if the default doesn't fit.
