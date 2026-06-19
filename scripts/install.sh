#!/usr/bin/env sh
set -eu

REPO="${FLOWGUARD_REPO:-xxvcc/flowguard}"
VERSION="${FLOWGUARD_VERSION:-latest}"
INSTALL_DIR="${FLOWGUARD_INSTALL_DIR:-/usr/local/bin}"
BASE_URL="${FLOWGUARD_BASE_URL:-}"
BIN_NAME="flowguard"
TMP_DIR="${TMPDIR:-/tmp}/flowguard-install.$$"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command not found: $1" >&2
    exit 1
  }
}

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

need_cmd uname
need_cmd tar
need_cmd sha256sum

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  echo "error: curl or wget is required" >&2
  exit 1
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in
  linux) os="linux" ;;
  *) echo "error: unsupported OS: $os" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  armv7l|armv7) arch="armv7" ;;
  *) echo "error: unsupported architecture: $arch" >&2; exit 1 ;;
esac

if [ -n "$BASE_URL" ]; then
  base_url="${BASE_URL%/}"
elif [ "$VERSION" = "latest" ]; then
  base_url="https://github.com/$REPO/releases/latest/download"
else
  base_url="https://github.com/$REPO/releases/download/$VERSION"
fi

asset="flowguard_${os}_${arch}.tar.gz"
checksums="checksums.txt"

mkdir -p "$TMP_DIR"
echo "Downloading $asset from $REPO ($VERSION)..."
fetch "$base_url/$asset" "$TMP_DIR/$asset"
fetch "$base_url/$checksums" "$TMP_DIR/$checksums"

(cd "$TMP_DIR" && grep "  $asset\$" "$checksums" | sha256sum -c -)
tar -xzf "$TMP_DIR/$asset" -C "$TMP_DIR"

if [ ! -x "$TMP_DIR/$BIN_NAME" ]; then
  chmod +x "$TMP_DIR/$BIN_NAME" 2>/dev/null || true
fi

if [ "$(id -u)" -ne 0 ]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  else
    echo "error: root permission is required; rerun as root or install sudo" >&2
    exit 1
  fi
else
  SUDO=""
fi

$SUDO mkdir -p "$INSTALL_DIR"
$SUDO install -m 0755 "$TMP_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"

echo "Installed $BIN_NAME to $INSTALL_DIR/$BIN_NAME"
echo "Starting interactive installer..."
exec $SUDO "$INSTALL_DIR/$BIN_NAME" install "$@"
