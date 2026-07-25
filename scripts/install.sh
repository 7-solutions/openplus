#!/bin/sh
# OpenPlus one-shot installer.
#
# Supported platforms — these are the only ones a release publishes:
#   linux/amd64, linux/arm64   glibc distributions; WSL2 included, it is Linux
#   darwin/arm64               Apple Silicon
#
# Everything else is refused by name rather than left to fail as a download 404
# or a loader error: musl (Alpine), Intel macOS, native Windows shells.
#
#   curl -fsSL https://raw.githubusercontent.com/7-solutions/openplus/main/scripts/install.sh | sh
#
# Options (environment variables):
#   OPENPLUS_VERSION   version tag to install (default: latest release)
#   OPENPLUS_INSTALL   install directory (default: ~/.local/bin, or /usr/local/bin if writable)
#   OPENPLUS_NO_VERIFY set to 1 to skip checksum verification (not recommended)
#
# POSIX sh on purpose: this must run under dash, ash (Alpine), and bash alike.

set -eu

REPO="7-solutions/openplus"
BIN="openplus"

# --- output helpers -----------------------------------------------------------
# Colors only when stdout is a terminal; a piped installer must not emit escapes.
if [ -t 1 ]; then
    _R='\033[0m'; _B='\033[1m'; _RED='\033[31m'; _GRN='\033[32m'; _YEL='\033[33m'
else
    _R=''; _B=''; _RED=''; _GRN=''; _YEL=''
fi

info()  { printf '%b==>%b %s\n' "$_B" "$_R" "$1"; }
ok()    { printf '%b  ok%b %s\n' "$_GRN" "$_R" "$1"; }
warn()  { printf '%bwarn%b %s\n' "$_YEL" "$_R" "$1" >&2; }
die()   { printf '%berror%b %s\n' "$_RED" "$_R" "$1" >&2; exit 1; }

need() {
    command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

# --- libc detection -------------------------------------------------------------
# The release binaries need glibc. Despite CGO_ENABLED=0, the Turso driver
# reaches libturso through purego, which emits a hard DT_NEEDED on libdl.so.2.
# On musl that surfaces as a bare "not found" from the loader, which tells the
# user nothing — so detect it here and say what is actually wrong.
check_libc() {
    is_musl=0
    # ld-musl-<arch>.so.1 is musl's loader and the most reliable signal.
    for f in /lib/ld-musl-*.so.1 /usr/lib/ld-musl-*.so.1; do
        [ -e "$f" ] && is_musl=1 && break
    done
    # Fall back to ldd, which prints "musl libc" on Alpine.
    if [ "$is_musl" -eq 0 ] && command -v ldd >/dev/null 2>&1; then
        ldd --version 2>&1 | head -n1 | grep -qi musl && is_musl=1
    fi
    [ "$is_musl" -eq 1 ] || return 0

    die "this system uses musl libc (Alpine or similar), which is not a supported platform.

OpenPlus targets glibc: the Turso database driver loads libturso through purego,
which needs the glibc dynamic loader even though the build is cgo-free.
Installing here would leave you with a binary that fails with a confusing loader
error, so this stops instead.

Use a glibc base image — debian:slim, ubuntu, or fedora.

Building from source will not help; the constraint is in a dependency, not the
build. See https://github.com/${REPO}/blob/main/docs/install.md"
}

# --- platform detection -------------------------------------------------------
detect_platform() {
    os=$(uname -s)
    arch=$(uname -m)

    case "$os" in
        Linux)  GOOS=linux; check_libc ;;   # check_loader runs after GOARCH is known
        Darwin) GOOS=darwin ;;
        # WSL2 reports Linux, so it needs no special case. These are the
        # Windows-native shells, where this script cannot install a Linux binary.
        MINGW*|MSYS*|CYGWIN*)
            die "native Windows is not supported. Use WSL2 and run this from inside your Linux distribution." ;;
        *) die "unsupported operating system: $os" ;;
    esac

    case "$arch" in
        x86_64|amd64)  GOARCH=amd64 ;;
        aarch64|arm64) GOARCH=arm64 ;;
        *) die "unsupported architecture: $arch (openplus ships amd64 and arm64)" ;;
    esac

    # macOS is arm64 only. Rosetta makes an Intel Mac report x86_64 honestly,
    # and there is no darwin_amd64 archive to fetch — without this the user
    # would get a 404 from the download step and have to guess why.
    if [ "$GOOS" = darwin ] && [ "$GOARCH" = amd64 ]; then
        die "macOS on Intel (x86_64) is not a supported platform.

OpenPlus publishes darwin_arm64 only: no CI runner executes an Intel Mac build,
so shipping one would be an untested claim rather than support.

Apple Silicon (M1 and later) is supported. On an Intel Mac, run OpenPlus in a
Linux VM or container instead. See https://github.com/${REPO}/blob/main/docs/install.md"
    fi

    PLATFORM="${GOOS}_${GOARCH}"
    check_loader
}

# --- dynamic loader detection ---------------------------------------------------
# The Linux binaries are cgo-free but still dynamically linked: purego's dlopen
# of libturso puts a hard DT_NEEDED on libdl.so.2, libpthread.so.0 and libc.so.6,
# and an ELF interpreter of /lib64/ld-linux-x86-64.so.2 (or the arm64 equivalent).
#
# NixOS is a supported distribution but has no FHS: that interpreter path does
# not exist, so the binary dies with "no such file or directory" naming a file
# that plainly is there. Detect it and either patch the interpreter to nix-ld or
# say exactly what to enable.
nixos_loader() {
    # nix-ld advertises itself through NIX_LD, and installs a shim loader at a
    # well-known path. Either is enough to run an FHS binary.
    if [ -n "${NIX_LD:-}" ] && [ -e "${NIX_LD}" ]; then
        printf '%s' "${NIX_LD}"
        return 0
    fi
    for f in /run/current-system/sw/share/nix-ld/lib/ld.so; do
        [ -e "$f" ] && { printf '%s' "$f"; return 0; }
    done
    return 1
}

check_loader() {
    [ "$GOOS" = linux ] || return 0

    case "$GOARCH" in
        amd64) INTERP=/lib64/ld-linux-x86-64.so.2 ;;
        arm64) INTERP=/lib/ld-linux-aarch64.so.1 ;;
    esac
    [ -e "$INTERP" ] && return 0

    # A missing loader on a normal FHS distribution means something else is
    # wrong, so only claim to understand the NixOS case.
    #
    # The two paths are overridable so the test suite can exercise the NixOS
    # branch from a non-NixOS machine. Not documented in the options block on
    # purpose: it is a test seam, not a knob for users.
    marker=${OPENPLUS_NIXOS_MARKER:-/etc/NIXOS}
    osrel=${OPENPLUS_OS_RELEASE:-/etc/os-release}

    is_nixos=0
    [ -e "$marker" ] && is_nixos=1
    if [ "$is_nixos" -eq 0 ] && [ -r "$osrel" ]; then
        . "$osrel" 2>/dev/null || true
        [ "${ID:-}" = nixos ] && is_nixos=1
    fi

    if [ "$is_nixos" -eq 0 ]; then
        die "the dynamic loader ${INTERP} is missing.

The release binary is cgo-free but still dynamically linked against glibc,
because the Turso driver reaches libturso through purego. Without that loader it
cannot start. Install glibc, or use a glibc-based distribution or container."
    fi

    if NIX_LOADER=$(nixos_loader); then
        ok "NixOS detected, will point the binary at nix-ld (${NIX_LOADER})"
        return 0
    fi

    die "NixOS detected, and nix-ld is not enabled.

OpenPlus ships an FHS binary: cgo-free, but dynamically linked against glibc
because the Turso driver loads libturso through purego. NixOS has no
${INTERP}, so the binary cannot start until a loader is provided.

Enable nix-ld in your configuration:

  programs.nix-ld.enable = true;
  programs.nix-ld.libraries = with pkgs; [ stdenv.cc.cc ];

then rebuild (sudo nixos-rebuild switch) and re-run this installer.

Alternatives that need no system change:
  nix-shell -p steam-run --run 'steam-run ${BIN} --version'
  nix-shell -p pkgs.stdenv.cc.cc.lib   # then build from source with go build

See https://github.com/${REPO}/blob/main/docs/install.md#nixos"
}

# --- NixOS interpreter patch ----------------------------------------------------
# Called after the binary is in place. Rewriting the ELF interpreter to nix-ld's
# loader is what makes the downloaded artifact runnable on NixOS; patchelf is in
# nixpkgs, so asking for it is reasonable there. Best effort: if patchelf is
# missing, the binary may still run under a nix-ld setup that intercepts the FHS
# path, so warn rather than fail an otherwise complete install.
patch_nixos_interpreter() {
    [ -n "${NIX_LOADER:-}" ] || return 0
    target=$1

    if ! command -v patchelf >/dev/null 2>&1; then
        warn "patchelf not found; leaving the ELF interpreter as-is.
     If '${BIN} --version' fails with a missing ${INTERP}, run:
       nix-shell -p patchelf --run 'patchelf --set-interpreter ${NIX_LOADER} ${target}'"
        return 0
    fi

    patchelf --set-interpreter "${NIX_LOADER}" "$target" \
        || warn "patchelf could not rewrite the interpreter; ${BIN} may not start"
    ok "interpreter set to nix-ld"
}

# --- download helper ----------------------------------------------------------
# Wraps curl or wget so the rest of the script does not care which exists.
download() {
    url=$1
    dest=$2
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" -o "$dest"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$dest" "$url"
    else
        die "need curl or wget to download"
    fi
}

download_stdout() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1"
    else
        wget -qO- "$1"
    fi
}

# api_get fetches a GitHub API URL, sending a token when one is available.
# Unauthenticated calls are limited to 60/hour per IP, which CI runners on
# shared cloud addresses exhaust routinely — the resulting 403 looks exactly
# like "no release exists" unless it is handled apart.
api_get() {
    url=$1
    token="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
    if command -v curl >/dev/null 2>&1; then
        if [ -n "$token" ]; then
            curl -fsSL -H "Authorization: Bearer $token" "$url"
        else
            curl -fsSL "$url"
        fi
    else
        if [ -n "$token" ]; then
            wget -qO- --header="Authorization: Bearer $token" "$url"
        else
            wget -qO- "$url"
        fi
    fi
}

# rate_limited reports whether the API is currently refusing us for quota.
# Only called on the failure path, so it costs nothing in the normal case.
rate_limited() {
    command -v curl >/dev/null 2>&1 || return 1
    curl -fsSL "https://api.github.com/rate_limit" 2>/dev/null \
        | tr ',' '\n' \
        | grep -m1 '"remaining"' \
        | grep -q ': *0'
}

# --- resolve the version ------------------------------------------------------
tag_from_json() {
    # Pull the first "tag_name" out of an API response without requiring jq.
    grep -m1 '"tag_name"' \
        | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/'
}

resolve_version() {
    if [ -n "${OPENPLUS_VERSION:-}" ]; then
        VERSION="$OPENPLUS_VERSION"
        return
    fi
    info "resolving latest release"

    # /releases/latest is the stable channel: GitHub excludes prereleases from
    # it, and 404s when every release is one. While OpenPlus is pre-1.0 that is
    # the normal case, so fall back to the newest published release. Once a
    # stable release exists this prefers it, which is the behavior we want.
    VERSION=$(api_get "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | tag_from_json || true)

    if [ -z "$VERSION" ]; then
        VERSION=$(api_get "https://api.github.com/repos/${REPO}/releases" 2>/dev/null \
            | grep -v '"draft":[[:space:]]*true' \
            | tag_from_json || true)
        [ -n "$VERSION" ] && warn "no stable release yet; installing prerelease $VERSION"
    fi

    if [ -z "$VERSION" ]; then
        # Distinguish the two ways this fails. Reporting "no release exists"
        # when the truth is "you are rate limited" sends people looking in
        # entirely the wrong place.
        if rate_limited; then
            die "the GitHub API is rate-limiting this machine (60 requests/hour for anonymous callers, shared per IP — CI runners hit this routinely).

Either wait for the quota to reset, install a known version directly:
  OPENPLUS_VERSION=v0.0.1-alpha.1 curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | sh

or authenticate:
  GITHUB_TOKEN=<token> curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | sh"
        fi
        die "could not determine a release to install.
Set an explicit version and retry:
  OPENPLUS_VERSION=v0.0.1-alpha.1 curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | sh"
    fi
}

# --- choose an install directory ----------------------------------------------
# Preference order: explicit override, then a writable /usr/local/bin, then
# ~/.local/bin. We never sudo on the user's behalf.
choose_dir() {
    if [ -n "${OPENPLUS_INSTALL:-}" ]; then
        INSTALL_DIR="$OPENPLUS_INSTALL"
    elif [ -w /usr/local/bin ] 2>/dev/null; then
        INSTALL_DIR=/usr/local/bin
    else
        INSTALL_DIR="$HOME/.local/bin"
    fi
    mkdir -p "$INSTALL_DIR" || die "cannot create install directory: $INSTALL_DIR"
    [ -w "$INSTALL_DIR" ] || die "install directory is not writable: $INSTALL_DIR
Set OPENPLUS_INSTALL to a directory you own, e.g.:
  OPENPLUS_INSTALL=\$HOME/bin curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | sh"
}

# --- verify the download ------------------------------------------------------
# A checksum mismatch means the archive is not what the release published. That
# is a hard stop: installing it anyway would defeat the point of publishing sums.
verify_checksum() {
    archive=$1
    sums=$2
    name=$3

    if [ "${OPENPLUS_NO_VERIFY:-0}" = "1" ]; then
        warn "checksum verification skipped (OPENPLUS_NO_VERIFY=1)"
        return
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$archive" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$archive" | awk '{print $1}')
    else
        warn "no sha256sum or shasum found; cannot verify the download"
        return
    fi

    expected=$(grep " $name\$" "$sums" 2>/dev/null | awk '{print $1}' | head -n1)
    if [ -z "$expected" ]; then
        warn "no checksum published for $name; skipping verification"
        return
    fi
    if [ "$actual" != "$expected" ]; then
        die "checksum mismatch for $name
  expected $expected
  actual   $actual
Refusing to install. Report this at https://github.com/${REPO}/issues"
    fi
    ok "checksum verified"
}

# --- PATH advice --------------------------------------------------------------
path_hint() {
    case ":${PATH}:" in
        *":${INSTALL_DIR}:"*) return ;;
    esac

    printf '\n'
    warn "$INSTALL_DIR is not on your PATH"
    case "$(basename "${SHELL:-sh}")" in
        zsh)  rc="~/.zshrc" ;;
        bash) rc="~/.bashrc" ;;
        fish)
            printf '  Add it with:\n    fish_add_path %s\n' "$INSTALL_DIR"
            return ;;
        *)    rc="your shell profile" ;;
    esac
    printf '  Add it with:\n    echo '"'"'export PATH="%s:$PATH"'"'"' >> %s\n' "$INSTALL_DIR" "$rc"
}

# --- main ---------------------------------------------------------------------
main() {
    need uname
    need tar

    detect_platform
    resolve_version
    choose_dir

    ARCHIVE="${BIN}_${VERSION}_${PLATFORM}.tar.gz"
    BASE="https://github.com/${REPO}/releases/download/${VERSION}"

    info "installing ${BIN} ${VERSION} (${PLATFORM}) to ${INSTALL_DIR}"

    TMP=$(mktemp -d)
    # Clean up on any exit path, including failure.
    trap 'rm -rf "$TMP"' EXIT INT TERM

    info "downloading ${ARCHIVE}"
    download "${BASE}/${ARCHIVE}" "${TMP}/${ARCHIVE}" \
        || die "download failed: ${BASE}/${ARCHIVE}
Check that release ${VERSION} publishes an asset for ${PLATFORM}."

    if download "${BASE}/checksums.txt" "${TMP}/checksums.txt" 2>/dev/null; then
        verify_checksum "${TMP}/${ARCHIVE}" "${TMP}/checksums.txt" "${ARCHIVE}"
    else
        warn "no checksums.txt in the release; skipping verification"
    fi

    info "extracting"
    tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP" || die "extract failed"
    [ -f "${TMP}/${BIN}" ] || die "archive did not contain a ${BIN} binary"

    # Install to a temporary name first, then move into place. A rename is
    # atomic, so a running openplus is never a half-written file.
    chmod +x "${TMP}/${BIN}"
    patch_nixos_interpreter "${TMP}/${BIN}"
    mv -f "${TMP}/${BIN}" "${INSTALL_DIR}/${BIN}.new" || die "cannot write to ${INSTALL_DIR}"
    mv -f "${INSTALL_DIR}/${BIN}.new" "${INSTALL_DIR}/${BIN}"

    ok "installed ${INSTALL_DIR}/${BIN}"

    if [ -x "${INSTALL_DIR}/${BIN}" ]; then
        printf '\n'
        "${INSTALL_DIR}/${BIN}" --version 2>/dev/null || true
    fi

    path_hint

    cat <<EOF

Next steps:
  1. Set a provider key:   export ANTHROPIC_API_KEY=sk-ant-...
  2. Try it without one:   ${BIN} --fake -p "say hello"
  3. Read the docs:        https://github.com/${REPO}#readme

EOF
}

main "$@"
