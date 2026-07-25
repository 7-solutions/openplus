# Change 0013 — Tasks (BACKLOG)

> One task = one vertical slice. TDD red-first, ports/adapters, cgo-free.
> `[ ]` open · `[~]` in progress · `[x]` done.

## H0 — Symbol index (internal/symbols)
- [x] T-1300 `Symbol{File, Name, Kind, StartLine, EndLine}` and
      `IndexFile(path) ([]Symbol, error)` over `go/parser`: functions, methods
      (receiver-qualified), types. Unparseable source errors naming the file.
- [x] T-1301 `IndexDir(root) (map[string][]Symbol, error)` walking `*.go`,
      skipping `.git`, `vendor`, and `testdata`. `Ref(file, name)` renders the
      `file.go::Name` reference form, and `Parse(ref)` splits it back.
- [x] T-1302 `Exists(root, ref) (bool, error)` — the check a claim needs. A
      non-Go file errors with the Go-only limitation and points at grit.

## H1 — Lock store (internal/coordinate)
- [x] T-1310 `Store{Dir, Expiry}` under `.openplus/locks/`. One file per symbol,
      created with `O_CREATE|O_EXCL` so the filesystem itself decides the winner.
      Contents: agent, intent, symbol, unix timestamp.
- [x] T-1311 `Acquire(agent, intent string, symbols []string) (Held, error)`:
      all-or-nothing. On any refusal, release everything taken in that attempt.
      Report the holder of the first blocking symbol.
- [x] T-1312 Expiry: a lock older than `Expiry` is reclaimable, and the takeover
      is reported in the result rather than being silent. A live lock is never
      stolen. Zero `Expiry` means locks never expire.
- [x] T-1313 `ReleaseAgent(agent)` frees everything an agent holds; releasing an
      agent that holds nothing succeeds. `Holder(symbol)` for inspection.
- [x] T-1314 Concurrency test: N goroutines claim one symbol, exactly one wins.
      Run under `-race`.

## H2 — NativeCoordinator (internal/coordinate)
- [x] T-1320 `NativeCoordinator` implementing `orchestrate.Coordinator`.
      `Available` is true whenever the repo root is a git repository — no external
      binary. `Claim` validates every symbol exists, acquires locks, then creates a
      detached worktree for the agent.
- [x] T-1321 `Done`: commit everything in the agent's worktree, merge that commit
      into the base branch, remove the worktree, release locks. A merge conflict is
      reported and the locks still release.
- [x] T-1322 `Release`: remove the worktree and free locks, merging nothing. The
      failure path.
- [x] T-1323 Disjoint-edit test on a scratch repo: two agents, one file, different
      functions, both `Done` — assert both changes are on the base branch. This is
      the behavior the whole change exists for.

## H3 — Selection (internal/runtime, internal/config)
- [x] T-1330 `config.Coordination{Backend string}` from `opencode.json`
      (`"native"` default, `"grit"`, `"none"`).
- [x] T-1331 `Assemble` wires the chosen coordinator. Native is the default, so
      coordinated fan-out works out of the box; `none` restores change 0011
      behavior exactly.
- [x] T-1332 `/subagents --coordinated` reports which backend it used, so a user
      can tell native from grit at a glance.

## Verification (Gate 5 — before declaring 0013 done)
- [x] `go build ./...` clean; `go test ./...` green (24/24 packages); `-race`
      clean on `internal/symbols`, `internal/coordinate`,
      `internal/orchestrate`, and `internal/runtime`.
- [x] `gofmt -l .` empty; `go vet ./...` clean; `CGO_ENABLED=0 go build ./...`
      green, and `go.mod` is unchanged — native coordination adds no dependency.
- [x] Functions, receiver methods (receiver-qualified as Type.Method), and types
      all index with line ranges (`internal/symbols`, 13 tests).
- [x] A claim on a nonexistent symbol is refused with the symbol named.
- [x] A claim naming a non-Go file errors with the Go-only limitation and points
      at grit.
- [x] N=20 concurrent claims on one symbol yield exactly one winner, under
      `-race`.
- [x] A partially-blocked multi-symbol claim leaves nothing locked.
- [x] An expired lock is reclaimable and the takeover is reported; a live one is
      never stolen.
- [x] Two agents editing different functions in one file both land on the base
      branch (`TestNCDisjointEditsBothLand` — the behavior the change exists for).
- [x] A failed agent releases its locks and merges nothing.
- [x] `/subagents --coordinated` works with no external binary installed, and
      the report names the native backend. Verified at the CLI.

## Out of scope (per proposal — each needs its own change)
Non-Go languages (tree-sitter is cgo; grit covers them) · custom three-way symbol
merge (git already merges disjoint symbols — measured, not assumed) ·
cross-machine lock backends · symbol dependency graphs · anything on the v1 refuse
list.
