#!/bin/sh
# Install InboxQL on macOS or Linux.
#
#   curl -fsSL https://techmuch.github.io/InboxQL/install.sh | sh
#
# What it does, in order:
#   1. downloads the release archive for this machine and SHA256SUMS
#   2. refuses to go on unless the archive matches its published checksum
#   3. installs iql to ~/.iql/bin, and puts that on your PATH
#   4. `iql setup`   — writes ~/.iql/settings.json and makes your mailbox
#   5. `iql service install` — starts InboxQL at login
#
# Already installed? This hands over to `iql update`, which backs your mailbox
# up and restarts the service in the right order.
#
# Environment, all optional:
#   INBOXQL_HOME              install somewhere other than ~/.iql
#   INBOXQL_VERSION           a tag such as v0.1.0, instead of the latest release
#   INBOXQL_NO_SERVICE=1      install and set up, but do not start at login
#   INBOXQL_NO_MODIFY_PATH=1  do not add ~/.iql/bin to your shell's PATH
#   INBOXQL_DOWNLOAD_BASE     download from here instead of GitHub (testing)

set -eu

REPO="techmuch/InboxQL"
HOME_DIR="${INBOXQL_HOME:-$HOME/.iql}"
BIN_DIR="$HOME_DIR/bin"
BIN="$BIN_DIR/iql"

say() { printf '%s\n' "$*"; }
fail() { printf 'install: %s\n' "$*" >&2; exit 1; }

# --- which archive ----------------------------------------------------------

os=$(uname -s)
arch=$(uname -m)
case "$os" in
  Darwin) asset="iql-darwin-universal.tar.gz" ;;
  Linux)
    case "$arch" in
      x86_64|amd64) asset="iql-linux-amd64.tar.gz" ;;
      *) fail "there is no release build for Linux on $arch yet; build from source: https://github.com/$REPO" ;;
    esac ;;
  *) fail "this script is for macOS and Linux; on Windows use install.ps1" ;;
esac

# --- already installed: update instead ------------------------------------

if [ -x "$BIN" ] && "$BIN" version >/dev/null 2>&1; then
  say "InboxQL is already installed at $BIN; updating it instead."
  exec "$BIN" update --yes
fi

# --- download and verify ----------------------------------------------------

if [ -n "${INBOXQL_DOWNLOAD_BASE:-}" ]; then
  base="$INBOXQL_DOWNLOAD_BASE"
elif [ -n "${INBOXQL_VERSION:-}" ]; then
  base="https://github.com/$REPO/releases/download/$INBOXQL_VERSION"
else
  base="https://github.com/$REPO/releases/latest/download"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading $asset"
curl -fsSL -o "$tmp/$asset" "$base/$asset" || fail "could not download $base/$asset"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || fail "could not download SHA256SUMS; not installing an unverified binary"

expected=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$tmp/SHA256SUMS")
[ -n "$expected" ] || fail "SHA256SUMS has no entry for $asset; not installing an unverified binary"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
fi
[ "$actual" = "$expected" ] || fail "$asset does not match its published checksum; not installing it"
say "Checksum verified"

tar -xzf "$tmp/$asset" -C "$tmp"
[ -f "$tmp/iql" ] || fail "the archive did not contain iql"

# --- install ----------------------------------------------------------------

mkdir -p "$BIN_DIR"
mv "$tmp/iql" "$BIN"
chmod 755 "$BIN"
# curl does not set the quarantine flag, but a copy made any other way may
# carry one, and Gatekeeper would then refuse an un-notarised binary.
if [ "$os" = "Darwin" ]; then
  xattr -d com.apple.quarantine "$BIN" 2>/dev/null || true
fi
say "Installed $BIN"

# --- PATH ---------------------------------------------------------------------

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    if [ -z "${INBOXQL_NO_MODIFY_PATH:-}" ]; then
      case "${SHELL:-}" in
        */zsh)  rc="$HOME/.zshrc" ;;
        */bash) if [ "$os" = "Darwin" ]; then rc="$HOME/.bash_profile"; else rc="$HOME/.bashrc"; fi ;;
        *)      rc="$HOME/.profile" ;;
      esac
      if ! grep -qs '# added by the InboxQL installer' "$rc"; then
        printf '\nexport PATH="%s:$PATH"  # added by the InboxQL installer\n' "$BIN_DIR" >> "$rc"
        say "Added $BIN_DIR to your PATH in $rc (open a new terminal to use it)"
      fi
    else
      say "Add $BIN_DIR to your PATH to run iql by name."
    fi ;;
esac

# --- set up and start ---------------------------------------------------------

if [ -n "${INBOXQL_HOME:-}" ]; then export INBOXQL_HOME; fi
"$BIN" setup

if [ -z "${INBOXQL_NO_SERVICE:-}" ]; then
  "$BIN" service install
else
  say "Not installing the login service (INBOXQL_NO_SERVICE). Start it any time with: iql service install"
fi

say ""
say "Done. Instructions, updates and uninstalling: https://techmuch.github.io/InboxQL/"
