# Pre-bump dep snapshot

## go.mod
```
module github.com/7solutions/openplus

go 1.26

require (
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/dop251/goja v0.0.0-20260723142020-b4aef50fa347
	github.com/ncruces/go-sqlite3 v0.20.3
	github.com/pkoukk/tiktoken-go v0.1.8
	github.com/pkoukk/tiktoken-go-loader v0.0.2
	golang.org/x/term v0.45.0
)

require (
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.9.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.5.0 // indirect
	github.com/dlclark/regexp2 v1.10.0 // indirect
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/tetratelabs/wazero v1.8.2 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.39.0 // indirect
)
```

## go list -m all
```
github.com/7solutions/openplus
github.com/MakeNowJust/heredoc v1.0.0
github.com/Masterminds/semver/v3 v3.5.0
github.com/asg017/sqlite-vec-go-bindings v0.1.6
github.com/atotto/clipboard v0.1.4
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
github.com/chzyer/readline v1.5.0
github.com/clipperhouse/displaywidth v0.9.0
github.com/clipperhouse/stringish v0.1.1
github.com/clipperhouse/uax29/v2 v2.5.0
github.com/davecgh/go-spew v1.1.1
github.com/dchest/siphash v1.2.3
github.com/dlclark/regexp2 v1.10.0
github.com/dlclark/regexp2/v2 v2.5.2
github.com/dop251/goja v0.0.0-20260723142020-b4aef50fa347
github.com/dop251/goja_nodejs v0.0.0-20211022123610-8dd9abb0616d
github.com/dustin/go-humanize v1.0.1
github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f
github.com/go-sourcemap/sourcemap v2.1.3+incompatible
github.com/goccy/go-yaml v1.19.2
github.com/google/pprof v0.0.0-20230207041349-798e818bf904
github.com/google/uuid v1.6.0
github.com/ianlancetaylor/demangle v0.0.0-20220319035150-800ac71e25c2
github.com/kylelemons/godebug v1.1.0
github.com/lucasb-eyer/go-colorful v1.3.0
github.com/mattn/go-isatty v0.0.20
github.com/mattn/go-localereader v0.0.1
github.com/mattn/go-runewidth v0.0.19
github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6
github.com/muesli/cancelreader v0.2.2
github.com/muesli/termenv v0.16.0
github.com/ncruces/go-sqlite3 v0.20.3
github.com/ncruces/julianday v1.0.0
github.com/ncruces/sort v0.1.2
github.com/pkoukk/tiktoken-go v0.1.8
github.com/pkoukk/tiktoken-go-loader v0.0.2
github.com/pmezard/go-difflib v1.0.0
github.com/psanford/httpreadat v0.1.0
github.com/rivo/uniseg v0.4.7
github.com/sahilm/fuzzy v0.1.1
github.com/stretchr/testify v1.8.2
github.com/tetratelabs/wazero v1.8.2
github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e
golang.org/x/crypto v0.29.0
golang.org/x/exp v0.0.0-20231006140011-7918f672742d
golang.org/x/mod v0.37.0
golang.org/x/sync v0.21.0
golang.org/x/sys v0.47.0
golang.org/x/term v0.45.0
golang.org/x/text v0.39.0
golang.org/x/tools v0.47.0
gopkg.in/yaml.v3 v3.0.1
lukechampine.com/adiantum v1.1.1
```
