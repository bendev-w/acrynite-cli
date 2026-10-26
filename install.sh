#!/bin/sh
# Install acrynite: curl -fsSL https://raw.githubusercontent.com/bendev-w/acrynite-cli/main/install.sh | sh
set -eu

REPO="bendev-w/acrynite-cli"
BIN="acrynite"
VERSION="${ACRYNITE_VERSION:-latest}"
INSTALL_DIR="${ACRYNITE_INSTALL_DIR:-/usr/local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $os (use install.ps1 on Windows)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
  [ -n "$VERSION" ] || { echo "Could not determine latest version" >&2; exit 1; }
fi
num="${VERSION#v}"

archive="${BIN}_${num}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$VERSION"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $archive ..."
curl -fsSL "$base/$archive" -o "$tmp/$archive"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

expected="$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
fi
[ -n "$expected" ] && [ "$expected" = "$actual" ] || { echo "Checksum verification failed" >&2; exit 1; }

tar -xzf "$tmp/$archive" -C "$tmp" "$BIN"

if [ -w "$INSTALL_DIR" ] || { [ ! -e "$INSTALL_DIR" ] && mkdir -p "$INSTALL_DIR" 2>/dev/null; }; then
  install -m 0755 "$tmp/$BIN" "$INSTALL_DIR/$BIN"
else
  echo "Elevated permissions needed for $INSTALL_DIR"
  sudo install -m 0755 "$tmp/$BIN" "$INSTALL_DIR/$BIN"
fi

echo "Installed $BIN $VERSION to $INSTALL_DIR/$BIN"
