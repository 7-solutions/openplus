# Installation

## Supported platforms

A release publishes exactly three artifacts. Anything not in this table is
refused by the installer with the reason, not left to fail as a 404 or a loader
error.

| Platform | Architectures | Notes |
|---|---|---|
| Linux (glibc) | arm64, amd64 | Tested on Ubuntu, Fedora, Debian, openSUSE, Arch, NixOS |
| WSL2 | arm64, amd64 | Uses the Linux artifacts unchanged — WSL2 reports `Linux` |
| macOS | arm64 only | Apple Silicon (M1 and later) |

Explicitly **not** supported:

- **macOS on Intel (x86_64).** No CI runner executes an Intel Mac build, so
  publishing one would be an untested claim. Use a Linux VM or container.
- **musl systems (Alpine).** See [below](#alpine-and-other-musl-systems--out-of-scope).
- **Native Windows.** Use WSL2 and install from inside your distribution.

Other glibc distributions (Mint, Pop!\_OS, RHEL, Rocky, Alma, Gentoo) are not
blocked and will generally work — they are simply not part of the tested set.

## One-shot installer (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/7-solutions/openplus/main/scripts/install.sh | sh
```

The script detects your OS and architecture, downloads the matching release
archive, **verifies its SHA-256 checksum against the published `checksums.txt`**,
and installs the binary. A checksum mismatch aborts the install rather than
proceeding.

### Where it installs

In order of preference:

1. `$OPENPLUS_INSTALL`, if you set it
2. `/usr/local/bin`, if it is writable without `sudo`
3. `~/.local/bin`

The installer never invokes `sudo` on your behalf. If it picks `~/.local/bin` and
that is not on your `PATH`, it prints the exact line to add.

### Options

| Variable | Effect |
|---|---|
| `OPENPLUS_VERSION` | Install a specific tag instead of the latest release |
| `OPENPLUS_INSTALL` | Install directory |
| `OPENPLUS_NO_VERIFY=1` | Skip checksum verification (not recommended) |

```bash
OPENPLUS_VERSION=v0.0.1-alpha OPENPLUS_INSTALL=$HOME/bin \
  curl -fsSL https://raw.githubusercontent.com/7-solutions/openplus/main/scripts/install.sh | sh
```

### Reading the script before running it

Piping a script from the internet into a shell is worth being careful about. To
read it first:

```bash
curl -fsSL https://raw.githubusercontent.com/7-solutions/openplus/main/scripts/install.sh -o install.sh
less install.sh
sh install.sh
```

## From source

```bash
git clone https://github.com/7-solutions/openplus
cd openplus
CGO_ENABLED=0 go build -o openplus ./cmd/openplus
./openplus --version
```

Requires Go 1.26 or newer.

To stamp a version into the binary the way releases do:

```bash
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.Version=$(git describe --tags --always)" \
  -o openplus ./cmd/openplus
```

## Manual download

Grab an archive from the [releases page](https://github.com/7-solutions/openplus/releases):

```bash
VERSION=v0.0.1-alpha
curl -fsSLO https://github.com/7-solutions/openplus/releases/download/$VERSION/openplus_${VERSION}_linux_amd64.tar.gz
curl -fsSLO https://github.com/7-solutions/openplus/releases/download/$VERSION/checksums.txt

sha256sum -c checksums.txt --ignore-missing   # verify before trusting

tar -xzf openplus_${VERSION}_linux_amd64.tar.gz
sudo mv openplus /usr/local/bin/
```

## With `go install`

```bash
go install github.com/7-solutions/openplus/cmd/openplus@latest
```

Requires Go 1.26 or newer. The binary lands in `$(go env GOPATH)/bin`.

This does not stamp a version, so `openplus --version` reports `dev`. Use the
installer or a release archive if you want a version-stamped build.

## Platform notes

### WSL2

Install from **inside** your Linux distribution, not from PowerShell or CMD. The
installer refuses to run under Git Bash, MSYS, or Cygwin because it cannot install
a Linux binary into a Windows environment.

### Alpine and other musl systems — out of scope

Linux builds target **glibc**. musl systems are not a supported platform, and the
installer detects musl and stops rather than leaving you with a binary that fails
at exec time.

**Use a glibc base image:**

```dockerfile
FROM debian:stable-slim      # or ubuntu, or fedora
```

Building from source on Alpine does not help; the constraint is in a dependency,
not the build.

<details>
<summary>Why the binary needs glibc, despite <code>CGO_ENABLED=0</code></summary>

The Turso driver reaches `libturso` through
[`purego`](https://github.com/ebitengine/purego), whose no-cgo Linux path declares:

```go
//go:cgo_import_dynamic purego_dlopen dlopen "libdl.so.2"
```

That emits a hard `DT_NEEDED` on glibc's `libdl.so.2`, so the binary requires the
glibc dynamic loader. It is structural to how purego does `dlopen` on Linux — no
build flag removes it. Verified by isolation: a `CGO_ENABLED=0` build of this
project *without* the memory package is statically linked, so purego is the sole
cause.

Removing the requirement would mean dropping Turso. `modernc.org/sqlite` — already
a dependency here, serving the FTS5 shadow index — is transpiled C→Go, needs no
`dlopen`, and does link statically, but it has no `vector32()` or
`vector_distance_cos()`, so vector search would become brute-force cosine distance
in Go. Since musl is not a target, that trade is not worth making.

</details>

### NixOS

NixOS is supported, but needs one thing set up first. The release binary is
cgo-free and still dynamically linked (see the glibc note above), so it carries
an ELF interpreter of `/lib64/ld-linux-x86-64.so.2`. NixOS has no FHS, that path
does not exist, and the failure mode is a `No such file or directory` error
naming a binary that is plainly there.

Enable [`nix-ld`](https://github.com/nix-community/nix-ld):

```nix
programs.nix-ld.enable = true;
programs.nix-ld.libraries = with pkgs; [ stdenv.cc.cc ];
```

Rebuild (`sudo nixos-rebuild switch`), then run the installer. It detects NixOS,
finds nix-ld, and rewrites the binary's interpreter with `patchelf` so it starts.
Without nix-ld the installer stops and prints this same configuration rather
than installing something that cannot run.

One-off alternative, no system change:

```bash
nix-shell -p steam-run --run 'steam-run openplus --version'
```

Building from source with `go build` also works, since the Nix toolchain
supplies a correct interpreter.

### macOS

Apple Silicon only — see [Supported platforms](#supported-platforms).

The binary is not notarized. On first run Gatekeeper may block it:

```bash
xattr -d com.apple.quarantine /usr/local/bin/openplus
```

## Verify the install

```bash
openplus --version
openplus --fake -p "say hello"    # no API key required
```

The second command exercises the whole runtime with a scripted provider. If it
prints a reply, your installation works.

## Upgrade

Re-run the installer. It overwrites the existing binary atomically, so an
in-flight `openplus` is never left half-written.

```bash
curl -fsSL https://raw.githubusercontent.com/7-solutions/openplus/main/scripts/install.sh | sh
```

## Uninstall

```bash
rm -f "$(command -v openplus)"
rm -rf .openplus          # per-project memory database, if you want it gone
```

OpenPlus writes nothing outside your project directory except the binary itself.

## Troubleshooting

**`openplus: command not found` after install**
The install directory is not on your `PATH`. The installer prints the line to add;
apply it and restart your shell.

**`checksum mismatch`**
The download did not match the published checksum. Do not bypass it with
`OPENPLUS_NO_VERIFY=1` — retry first, and if it persists,
[open an issue](https://github.com/7-solutions/openplus/issues).

**`could not determine the latest release`**
The GitHub API may be rate-limiting you. Pin a version:
`OPENPLUS_VERSION=v0.0.1-alpha`.

**`unsupported architecture`**
Releases ship `linux_amd64`, `linux_arm64` and `darwin_arm64`. Build from source
for anything else.

**`macOS on Intel (x86_64) is not a supported platform`**
Expected on an Intel Mac — see [Supported platforms](#supported-platforms).

**`the dynamic loader ... is missing` / `nix-ld is not enabled`**
See [NixOS](#nixos).
