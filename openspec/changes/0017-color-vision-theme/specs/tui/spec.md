# TUI Specification (delta — change 0017, introduces the capability)

## Purpose
A central, switchable color theme for the terminal UI so users with color vision
deficiencies can read it. Introduces the `tui` capability: a `Palette` of semantic
color roles, a universal colorblind-safe default, per-deficiency overrides, a
runtime `/theme` switch, and a WCAG-AA contrast floor enforced by test. Appearance
only — no behavior, layout, or core-port change.

## Requirements

### Requirement: Colors come from semantic roles
The TUI SHALL draw every color from a `Palette` of named semantic roles (prompt,
working indicator, user text, assistant text, error, accents), with no raw color
literal outside the theme package.

#### Scenario: The model uses roles, not literals
- **WHEN** the model renders the prompt indicator
- **THEN** it reads the `PromptColor` role from the active palette, not an inline literal

#### Scenario: No raw color escapes the theme
- **WHEN** the `internal/tui` model package is built
- **THEN** it defines no `lipgloss.Color` or ANSI literal outside `theme.go`

### Requirement: A universal colorblind-safe default exists
A `default` palette SHALL be colorblind-safe — hues distinguishable by luminance as
well as hue (not red/green pairs) — and SHALL be active on a fresh session.

#### Scenario: Default is active initially
- **WHEN** a session starts with no `tui.theme` config
- **THEN** the `default` palette is active

### Requirement: Per-deficiency overrides exist
Palettes for `deutan`, `protan`, and `tritan` SHALL be selectable, each tuned for its
deficiency.

#### Scenario: Each deficiency palette is selectable
- **WHEN** `/theme deutan` (or `protan`, `tritan`) is invoked
- **THEN** that palette becomes active and the model re-renders with it

### Requirement: Every palette meets a contrast floor
Every text/background role pair in every shipped palette SHALL meet WCAG AA
(≥ 4.5:1 for normal text), asserted by a test over the palette.

#### Scenario: Contrast is verified, not eyeballed
- **WHEN** the contrast test runs over all shipped palettes
- **THEN** every text/background pair is ≥ 4.5:1, or the test fails

#### Scenario: A bad palette edit fails the gate
- **WHEN** a palette pair drops below 4.5:1
- **THEN** the contrast test fails, blocking the change

### Requirement: The theme switches at runtime and persists
`/theme` SHALL list palettes; `/theme <name>` SHALL switch immediately and persist
the choice via `tui.theme` config for the next session.

#### Scenario: List and switch
- **WHEN** the user runs `/theme` with no argument
- **THEN** the available palettes are listed with the active one marked

#### Scenario: Choice persists
- **WHEN** `/theme tritan` is set and the session restarts
- **THEN** the `tritan` palette is active on the new session

### Requirement: Colors degrade gracefully
Colors authored as hex SHALL degrade to the terminal's capability via Lipgloss's
colorprofile; a constrained terminal SHALL still render distinct, readable roles.

#### Scenario: A 16-color terminal stays readable
- **WHEN** the colorprofile is constrained to 16 colors
- **THEN** roles remain distinguishable (by luminance/bold), not collapsed
