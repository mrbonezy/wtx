#!/bin/sh
set -eu

REPO="mrbonezy/wtx"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"

if command -v curl >/dev/null 2>&1; then
  HTTP_GET="curl -fsSL"
elif command -v wget >/dev/null 2>&1; then
  HTTP_GET="wget -qO-"
else
  echo "error: need curl or wget" >&2
  exit 1
fi

TAG="$($HTTP_GET "$API_URL" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
if [ -z "$TAG" ]; then
  echo "error: could not determine latest release tag" >&2
  exit 1
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "error: unsupported arch $ARCH" >&2
    exit 1
    ;;
esac

case "$OS" in
  darwin|linux) ;;
  *)
    echo "error: unsupported os $OS" >&2
    exit 1
    ;;
esac

ARCHIVE="wtx_${TAG}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

echo "Downloading $URL"
$HTTP_GET "$URL" > "${TMP_DIR}/${ARCHIVE}"

tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR"

BIN_DIR="/usr/local/bin"
if [ ! -w "$BIN_DIR" ]; then
  BIN_DIR="${HOME}/.local/bin"
  mkdir -p "$BIN_DIR"
fi

mv "${TMP_DIR}/wtx" "${BIN_DIR}/wtx"
chmod +x "${BIN_DIR}/wtx"

echo "Installed wtx to ${BIN_DIR}/wtx"
echo "Run: wtx version"
