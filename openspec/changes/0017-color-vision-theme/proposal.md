# Change 0017 — Color-vision-abnormalities theme (PLAN)

## Why
The TUI renders color with a single hardcoded ANSI code (`lipgloss.Color("3")` in
`internal/tui/model.go`) and no palette. For users with color vision deficiencies
(deuteranopia, protanopia, tritanopia — collectively the majority of CVD cases),
hue-only distinctions collapse. This change adds a central, switchable theme: a
**universal colorblind-safe default** plus **per-deficiency overrides**, selectable
at runtime.

This is an accessibility capability, **not** a refuse-in-v1 item — no ADR trigger was
ever required. It introduces the `tui` capability spec (the existing `internal/tui`
package was previously unspecified).

## What I verified before designing
1. **Styling is minimal and centralized enough to theme.** `lipgloss` appears only in
   `internal/tui/model.go` (two styles, one ANSI color). There is no palette file,
   no theme struct — so a new `internal/tui/theme.go` is greenfield and uncontested.
2. **Lipgloss handles terminal color fidelity.** Lipgloss's colorprofile adapts
   true-color to the terminal's capability (256/16/no-color), so palette values can
   be authored as hex and degrade gracefully. No manual termenv wiring needed.
3. **The command surface accepts new commands.** `builtinCommands`
   (`internal/runtime/commands_builtin.go`) + dispatch (`command.go`) register slash
   commands; `/theme` fits the existing `/help` pattern.
4. **Contrast is computable.** WCAG relative-luminance contrast is a pure function
   of two hex colors, so the palette's text/background pairs can be **asserted by
   test** to meet a contrast floor — accessibility is verified, not eyeballed.
5. **No presentation port exists in core.** The TUI is a presentation adapter; theming
   touches no core port and no provider surface.

## What changes
Adds a theme subsystem to `internal/tui` and a `/theme` command; no core port change.

- `internal/tui/theme.go`: a `Palette` (named `lipgloss.Color`/`lipgloss.Style` values
  for the semantic roles the TUI uses: prompt, working indicator, user/assistant
  text, errors, accents). Four palettes:
  - `default` — universal colorblind-safe (Okabe–Ito / Wong-derived: hues
    distinguishable by **luminance as well as hue**, not red/green pairs).
  - `deutan`, `protan`, `tritan` — per-deficiency overrides tuned for each.
- `internal/tui`: the model reads colors from the active `Palette` instead of inline
  literals; `SetTheme(name)` swaps the palette at runtime and re-renders.
- Runtime: `/theme` lists palettes; `/theme <name>` switches. The choice persists via
  config (`tui.theme`) for the next session.
- `docs/adr/0012-color-vision-theme.md`: records the decision and the contrast floor.
- New capability spec `openspec/specs/tui/` (delta introduces it).

### The theme contract (defined by this change)
- **Semantic roles, not raw colors.** The model references roles (`PromptColor`,
  `ErrorStyle`, …); a palette maps roles → colors. Adding a role is one field.
- **Contrast floor.** Every text/background pair in every shipped palette SHALL meet
  WCAG AA (4.5:1 for normal text) — asserted by a test over the palette, so a future
  edit cannot silently regress accessibility.
- **Graceful degradation.** Colors are authored as hex; Lipgloss's colorprofile
   downgrades to the terminal's capability. A no-color terminal still reads correctly
   via luminance/bold differences.
- **No behavior change.** Theming changes appearance only; layout, keys, and the
  agent loop are untouched.

## What this deliberately does not do
- **No automatic detection of the user's CVD.** There is no reliable in-terminal
  probe; the user picks. Auto-detect is out of scope.
- **No full user-authored palette editor.** Four shipped palettes + a config name.
  Custom palettes are a future trigger.
- **No change to non-TUI output.** Logs, reports, and non-interactive streams are
  untouched.
- **No new core port.** Theming lives entirely in the presentation adapter.

## Governing decisions
ADR-0001 (cgo-free — Lipgloss is already a dependency, pure Go) · the
provider-neutrality hard rule (no provider surface touched). No existing ADR is
amended; this is a new accessibility capability with its own ADR.

## Risk
- **Contrast regression.** A palette edit that looks fine can fail WCAG. Mitigated by
  T-1713: a test computes every pair's contrast and fails below AA.
- **Terminal fidelity.** A true-color palette on a 16-color terminal could look wrong.
  Mitigated by relying on Lipgloss's colorprofile (T-1714 asserts a 16-color profile
  still produces distinct, readable roles).
- **Role sprawl.** Ad-hoc inline colors creeping back. Mitigated by T-1715: a lint
  gate that the model package defines no raw `lipgloss.Color`/ANSI literal outside
  `theme.go`.

## Verification
Each palette is testable for its role set and its contrast ratios (pure functions of
hex). `SetTheme` is testable for a re-render with the new palette. The `/theme`
command is an integration test through the real Session. Degradation is testable by
forcing a constrained colorprofile and asserting role distinctness.

## Approval
STOP — implementation begins only after this proposal + the delta spec + tasks are
approved (house Gate 1).
