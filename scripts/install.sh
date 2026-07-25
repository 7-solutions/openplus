#!/bin/sh
# OpenPlus one-shot installer for Linux, macOS, and WSL2.
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

# --- platform detection -------------------------------------------------------
detect_platform() {
    os=$(uname -s)
    arch=$(uname -m)

    case "$os" in
        Linux)  GOOS=linux ;;
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

    PLATFORM="${GOOS}_${GOARCH}"
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

# --- resolve the version ------------------------------------------------------
resolve_version() {
    if [ -n "${OPENPLUS_VERSION:-}" ]; then
        VERSION="$OPENPLUS_VERSION"
        return
    fi
    info "resolving latest release"
    # Parse the tag out of the releases API without requiring jq.
    VERSION=$(download_stdout "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep -m1 '"tag_name"' \
        | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
    [ -n "$VERSION" ] || die "could not determine the latest release. Set OPENPLUS_VERSION=<tag> and retry."
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
