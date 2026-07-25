# Change 0017 — Tasks (BACKLOG)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## H0 — Decision
- [ ] T-1700 Write `docs/adr/0012-color-vision-theme.md`: new accessibility
      capability; central switchable `Palette` of semantic roles, universal
      colorblind-safe default + deutan/protan/tritan overrides, WCAG-AA contrast
      floor enforced by test, appearance-only, no core-port change, cgo-free
      (Lipgloss already a pure-Go dependency).

## H1 — Palette core (internal/tui/theme.go)
- [ ] T-1710 `Palette` struct of semantic roles (PromptColor, WorkingIndicator,
      UserText, AssistantText, ErrorStyle, accents) as `lipgloss.Color`/`Style`.
      Red: a palette maps every role to a value; missing role → build/test fails.
- [ ] T-1711 `default` palette: colorblind-safe (Okabe–Ito/Wong-derived,
      luminance-distinguishable, no red/green pairs). Red: default palette exists and
      is returned by `Default()`.

## H2 — Contrast gate (accessibility)
- [ ] T-1712 WCAG relative-luminance contrast function for two hex colors. Red: known
      pairs return expected ratios (black/white ≈ 21, a bad pair < 4.5).
- [ ] T-1713 Contrast test over every shipped palette's text/background pairs asserts
      ≥ 4.5:1. Red: a deliberately-bad pair fails the test; shipped palettes pass.

## H3 — Per-deficiency palettes
- [ ] T-1714 `deutan`, `protan`, `tritan` palettes, each tuned + contrast-verified by
      T-1713. Red: each is selectable by name and passes the contrast gate.

## H4 — Model integration
- [ ] T-1715 The `internal/tui` model reads colors from the active palette (no raw
      `lipgloss.Color`/ANSI literal outside `theme.go`); `SetTheme(name)` swaps +
      re-renders. Red: a lint/build gate that the model package has no color literal
      outside theme.go; SetTheme changes the rendered role color.
- [ ] T-1716 Graceful degradation: force a constrained colorprofile, assert roles stay
      distinct/readable (luminance/bold), not collapsed. Red: 16-color profile still
      distinguishes PromptColor from ErrorStyle.

## H5 — Config + command
- [ ] T-1717 Config `tui.theme` (optional) selects the initial palette; default when
      unset/unknown. Red: config-driven initial palette; bad name → default + warn.
- [ ] T-1718 `/theme` command in `builtinCommands`: list (marks active) and
      `/theme <name>` (switch + persist to config). Integration test through the real
      Session: switch → re-render → persists across restart.

## H6 — Gate
- [ ] T-1719 Advisor pass (resolve every finding); update knowledge graph + memory.
      No refuse-list change (accessibility, not deferred).
