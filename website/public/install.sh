#!/bin/sh
# Install the Go 9lives runner from a checksum-verified GitHub release.
# Served at https://9lives.run/install.sh (and the non-HTML root endpoint).
# Python compatibility: pip install 9lives / uv tool install 9lives.
set -eu

VERSION="${NINELIVES_VERSION:-v0.4.5}"
INSTALL_DIR="${NINELIVES_INSTALL_DIR:-$HOME/.local/share/9lives-runner/bin}"
REPOSITORY="https://github.com/Quality-Max/9lives-runner"

fail() { printf '9lives: %s\n' "$1" >&2; exit 1; }
say() { printf '9lives: %s\n' "$1"; }

case "$VERSION" in
  *[!v0-9.]*) fail "NINELIVES_VERSION must be a stable tag such as v0.4.5" ;;
esac
printf '%s\n' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' ||
  fail "NINELIVES_VERSION must be a stable tag such as v0.4.5"
case "$INSTALL_DIR" in
  /*) ;;
  *) fail "NINELIVES_INSTALL_DIR must be an absolute directory path" ;;
esac
case "$(uname -s)" in
  Darwin) platform=darwin ;;
  Linux) platform=linux ;;
  *) fail "supported platforms: macOS and Linux; use Python for other existing workflows" ;;
esac
case "$(uname -m)" in
  arm64|aarch64) architecture=arm64 ;;
  x86_64|amd64) architecture=amd64 ;;
  *) fail "supported architectures: amd64 and arm64" ;;
esac

for dependency in curl tar awk; do
  command -v "$dependency" >/dev/null 2>&1 || fail "required command missing: $dependency"
done
if command -v sha256sum >/dev/null 2>&1; then
  checksum_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
  checksum_tool=shasum
else
  fail "install sha256sum or shasum to verify the release"
fi

# Keep Go separate from uv's Python entry points. Never follow or replace a
# foreign executable when a caller chooses a custom installation directory.
if [ -L "$INSTALL_DIR/9l" ]; then
  fail "destination 9l is a symlink; choose a separate NINELIVES_INSTALL_DIR"
fi
if [ -e "$INSTALL_DIR/9l" ]; then
  [ -f "$INSTALL_DIR/9l" ] && [ -x "$INSTALL_DIR/9l" ] ||
    fail "destination 9l is not an executable file"
  case "$("$INSTALL_DIR/9l" --version 2>/dev/null)" in
    "9l "*" (Go runner)") ;;
    *) fail "destination 9l belongs to another CLI; choose a separate NINELIVES_INSTALL_DIR" ;;
  esac
fi
# Legal files replace only regular files: a symlink or directory there could
# redirect the write outside the installation directory.
for legal in LICENSE NOTICE THIRD-PARTY-NOTICES.txt; do
  if [ -L "$INSTALL_DIR/$legal" ] || { [ -e "$INSTALL_DIR/$legal" ] && [ ! -f "$INSTALL_DIR/$legal" ]; }; then
    fail "destination $legal is not a regular file; choose a separate NINELIVES_INSTALL_DIR"
  fi
done

task_tmp="$(mktemp -d "${TMPDIR:-/tmp}/9lives-install.XXXXXXXX")"
staging=""
cleanup() {
  if [ -n "$staging" ]; then rm -rf "$staging"; fi
  rm -rf "$task_tmp"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

bundle="9l-$platform-$architecture"
archive="$bundle.tar.gz"
release="$REPOSITORY/releases/download/$VERSION"
say "downloading Go runner $VERSION for $platform/$architecture"
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL "$release/$archive" -o "$task_tmp/$archive"
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL "$release/SHA256SUMS" -o "$task_tmp/SHA256SUMS"
awk -v archive="$archive" 'NF == 2 && $2 == archive { print }' "$task_tmp/SHA256SUMS" > "$task_tmp/selected.sha256"
[ "$(awk 'END { print NR }' "$task_tmp/selected.sha256")" -eq 1 ] ||
  fail "release must contain exactly one checksum for $archive"
(
  cd "$task_tmp"
  if [ "$checksum_tool" = sha256sum ]; then
    sha256sum -c selected.sha256
  else
    shasum -a 256 -c selected.sha256
  fi
) || fail "release checksum verification failed"

tar -xzf "$task_tmp/$archive" -C "$task_tmp"
for artifact in 9l LICENSE NOTICE THIRD-PARTY-NOTICES.txt; do
  [ -f "$task_tmp/$bundle/$artifact" ] && [ ! -L "$task_tmp/$bundle/$artifact" ] ||
    fail "release is missing a regular $artifact file"
done
release_identity="$("$task_tmp/$bundle/9l" --version)" || fail "release binary cannot run on this machine"
case "$release_identity" in
  "9l ${VERSION#v} (Go runner)") ;;
  *) fail "release binary version does not match $VERSION" ;;
esac
mkdir -p "$INSTALL_DIR"
# Copy into a fresh directory on the same file system, then rename each file
# into place, so no write follows an existing destination path.
staging="$(mktemp -d "$INSTALL_DIR/.9l-install.XXXXXXXX")"
for artifact in 9l LICENSE NOTICE THIRD-PARTY-NOTICES.txt; do
  cp "$task_tmp/$bundle/$artifact" "$staging/$artifact"
done
chmod 755 "$staging/9l"
for legal in LICENSE NOTICE THIRD-PARTY-NOTICES.txt; do
  mv -f "$staging/$legal" "$INSTALL_DIR/$legal"
done
mv -f "$staging/9l" "$INSTALL_DIR/9l"
rmdir "$staging"
staging=""

say "installed: $release_identity"
say "binary: $INSTALL_DIR/9l"
say "add this directory to PATH, then check '9l --version' selects the Go runner:"
# Quote the path for a POSIX shell even when it contains spaces or apostrophes.
quoted_dir="$(printf '%s' "$INSTALL_DIR" | sed "s/'/'\\\\''/g")"
printf "  export PATH='%s':\"\$PATH\"\n" "$quoted_dir"
say "from an existing Playwright project: 9l plan tests/ && 9l run tests/"
say "check the page you changed: 9l scan https://your-app.dev --format markdown"
say "install project dependencies and Playwright browsers separately"
say "guide: https://quality-max.github.io/9lives-runner/"
say "Python compatibility remains available via '9lives' or 'python -m ninelives.cli'"
