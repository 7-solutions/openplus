# Change 0027 — Rename the module so `go install` works

> Status: SHIPPED. Mechanical rename with a verifiable end state; the change
> record was written alongside the work rather than gating it.

## Why
The module declared `github.com/7solutions/openplus` while the repository is at
`github.com/7-solutions/openplus`. The Go module proxy resolves the **declared**
path, which 404s, so:

```
go install github.com/7solutions/openplus/cmd/openplus@latest
→ git ls-remote https://github.com/7solutions/openplus: exit status 128
```

`go install` is the idiomatic way to install a Go CLI, and the v0.0.1-alpha docs
had to carry a paragraph explaining that it does not work. This change removes
that paragraph rather than explaining it better.

## What changed
- `go.mod` module path, and the import path in **84 Go files** (170 occurrences).
- `.golangci.yml` `goimports.local-prefixes`, which must match the module path or
  import grouping silently stops working.
- `README.md` and `docs/install.md`: the "not supported" note is replaced with the
  working command.

**Left untouched on purpose:** `openspec/changes/**` still contains the old path.
Those are historical records of what was true when each change shipped; rewriting
them would falsify the record. The project already treats change files this way
(see the 0022 note about not editing 0020's history).

## What this fixes
```
go install github.com/7-solutions/openplus/cmd/openplus@latest   # now resolves
```

## A pre-existing hole found while verifying
Re-proving the leak guard after the rename — its match string changed, so a stale
one would have silently stopped matching — exposed a bug **unrelated to the
rename**: the guard only recognized a *plain* import.

```go
_ "github.com/7-solutions/openplus/internal/provider"   // passed the build
```

A blank import pulls the adapter and its transitive dependencies into a core
package exactly as a plain one does, and it is precisely the form someone reaches
for to register a driver — the most likely way the hard rule would actually have
been broken.

Fixed by extracting `importsAdapterPackage`, which recognizes plain, blank,
aliased, and dot imports while still ignoring comments and prose that merely
mention the path. `leak_guard_forms_test.go` locks all twelve cases, and the
end-to-end proof was re-run: injecting a blank import into `internal/tool/glob.go`
now fails the guard naming the file.

This is the value of re-proving a guard instead of trusting that it still works.

## Verification
- `CGO_ENABLED=0 go build ./...`, `go vet ./...` clean
- `go test -count=1 ./...` — 27 packages green
- All seven architectural guards pass, including the new matcher test
- `go install github.com/7-solutions/openplus/cmd/openplus@latest` resolves

## Risk
Anyone who vendored or imported the old path must update it. At v0.0.1-alpha,
published hours earlier and installed via `curl | sh` rather than `go get`, there
are no known importers. Doing this later would be strictly more disruptive.
