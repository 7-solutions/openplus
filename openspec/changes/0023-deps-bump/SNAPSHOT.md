# SNAPSHOT — pre-bump lockfile state (change 0023)

> Audit artifact. Captured BEFORE `go get -u ./...`. The durable dep-bump
> discipline (from change 0019, REVERTED): every OpenSpec change that bumps
> deps MUST snapshot pre-bump `go.mod`/`go.sum`/`go list -m all` plus a
> byte-compile confirmation. The pre-bump `go.mod`/`go.sum` themselves are
> preserved at git HEAD below; this file captures the resolved module graph.

- **Captured at git HEAD:** `45286c9` (`chore(driver): re-target Turso to canonical turso.tech/database/tursogo v0.7.1 (0022)`)
- **Pre-bump build:** `go build ./...` → **clean** (the 0019 lesson: include a byte-compile pass on the SNAPSHOT, not just `go list`).
- **Pre-bump `go.mod`/`go.sum`:** at HEAD `45286c9`; `git show 45286c9:go.mod` restores them.

## go list -m all (pre-bump)

```
github.com/7solutions/openplus
github.com/MakeNowJust/heredoc v1.0.0
github.com/Masterminds/semver/v3 v3.5.0
github.com/atotto/clipboard v0.0.1
github.com/aymanbagabas/go-osc52/v2 v2.0.1
github.com/aymanbagabas/go-udiff v0.3.1
github.com/bits-and-blooms/bitset v1.24.4
github.com/charmbracelet/bubbles v1.0.0
github.com/charmbracelet/bubbletea v1.3.10
github.com/charmbracelet/colorprofile v0.4.1
github.com/charmbracelet/harmonica v0.2.0
github.com/charmbracelet/lipgloss v1.1.0
github.com/charmbracelet/x/ansi v0.11.6
github.com/charmbracelet/x/cellbuf v0.0.15
github.com/charmbracelet/x/exp/golden v0.0.0-20241011142426-46044092ad91
github.com/charmbracelet/x/term v0.2.2
github.com/chzyer/readline v1.5.1
github.com/clipperhouse/displaywidth v0.9.0
github.com/clipperhouse/stringish v0.1.1
github.com/clipperhouse/uax29/v2 v2.5.0
github.com/davecgh/go-spew v1.1.1
github.com/dlclark/regexp2 v1.10.0
github.com/dlclark/regexp2/v2 v2.5.2
github.com/dop251/goja v0.0.0-20260723142020-b4aef50fa347
github.com/dop251/goja_nodejs v0.0.0-20211022123610-8dd9abb0616d
github.com/dustin/go-humanize v1.0.1
github.com/ebitengine/purego v0.10.0-alpha.2          # WATCH: alpha → v0.10.2 stable
github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f
github.com/go-sourcemap/sourcemap v2.1.3+incompatible
github.com/goccy/go-yaml v1.19.2
github.com/google/pprof v0.0.0-20250317173921-a4b03ec1a45e
github.com/google/uuid v1.6.0
github.com/hashicorp/golang-lru/v2 v2.0.7
github.com/ianlancetaylor/demangle v0.0.0-20240312041847-bd984b5ce465
github.com/kylelemons/godebug v1.1.0
github.com/lucasb-eyer/go-colorful v1.3.0
github.com/mattn/go-isatty v0.0.20                     # WATCH: → v0.0.24
github.com/mattn/go-localereader v0.0.1
github.com/mattn/go-runewidth v0.0.19                  # WATCH: → v0.0.27 (8 patches)
github.com/mattn/go-sqlite3 v1.14.42                   # indirect (turso driver test dep)
github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6
github.com/muesli/cancelreader v0.2.2
github.com/muesli/termenv v0.16.0
github.com/ncruces/go-strftime v1.0.0                  # stale transitive; tidy may drop
github.com/pkoukk/tiktoken-go v0.1.8
github.com/pkoukk/tiktoken-go-loader v0.0.2
github.com/pmezard/go-difflib v1.0.0
github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec
github.com/rivo/uniseg v0.4.7
github.com/sahilm/fuzzy v0.1.1
github.com/stretchr/testify v1.11.1
github.com/tursodatabase/turso-go-platform-libs v0.7.1
github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e
golang.org/x/exp v0.0.0-20231006140011-7918f672742d
golang.org/x/mod v0.37.0
golang.org/x/sync v0.21.0
golang.org/x/sys v0.47.0
golang.org/x/term v0.45.0
golang.org/x/text v0.39.0
golang.org/x/tools v0.47.0
gopkg.in/yaml.v3 v3.0.1
modernc.org/cc/v4 v4.29.0
modernc.org/ccgo/v4 v4.34.6
modernc.org/fileutil v1.4.0
modernc.org/gc/v2 v2.6.5
modernc.org/gc/v3 v3.1.4
modernc.org/goabi0 v0.2.0
modernc.org/libc v1.74.1                              # WATCH: → v1.74.3
modernc.org/mathutil v1.7.1
modernc.org/memory v1.11.0
modernc.org/opt v0.2.0
modernc.org/sortutil v1.2.1
modernc.org/sqlite v1.54.0
modernc.org/strutil v1.2.1
modernc.org/token v1.1.0
turso.tech/database/tursogo v0.7.1
```

## How to use this snapshot
- `diff` the post-bump `go list -m all` against the block above to see every
  version that moved.
- Pre-bump `go.mod`/`go.sum` are at git HEAD `45286c9`; `git show 45286c9:go.mod`
  restores the exact pre-bump lockfile if a rollback is needed.
- The `# WATCH` markers flag the bumps most likely to surface a problem (per
  the proposal's watch-items); they are verified first during T-2305..T-2310.
