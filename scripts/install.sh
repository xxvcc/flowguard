#!/usr/bin/env sh
set -eu

REPO="${FLOWGUARD_REPO:-xxvcc/flowguard}"
VERSION="${FLOWGUARD_VERSION:-latest}"
INSTALL_DIR="${FLOWGUARD_INSTALL_DIR:-/usr/local/bin}"
BASE_URL="${FLOWGUARD_BASE_URL:-}"
SKIP_SETUP="${FLOWGUARD_SKIP_SETUP:-0}"
BIN_NAME="flowguard"
TMP_DIR=""

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command not found: $1" >&2
    exit 1
  }
}

cleanup() {
  if [ -n "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT INT TERM

need_cmd uname
need_cmd tar
need_cmd sha256sum
need_cmd mktemp
need_cmd awk
need_cmd wc
need_cmd install
need_cmd cp

reject() {
  echo "error: $1" >&2
  exit 1
}

case "$REPO" in
  ""|/*|*/*/*|*" "*|*".."*) reject "invalid FLOWGUARD_REPO, expected owner/name" ;;
esac

case "$VERSION" in
  ""|*/*|*" "*|*"?"*|*"#"*) reject "invalid FLOWGUARD_VERSION" ;;
esac

if [ -n "$BASE_URL" ]; then
  case "$BASE_URL" in
    *"?"*|*"#"*) reject "FLOWGUARD_BASE_URL must not contain query or fragment" ;;
    https://*) ;;
    http://localhost/*|http://127.0.0.1/*|http://\[::1\]/*) ;;
    *) reject "FLOWGUARD_BASE_URL must be https, except localhost test mirrors" ;;
  esac
fi

check_size() {
  file="$1"
  max="$2"
  label="$3"
  size=$(wc -c < "$file" | awk '{print $1}')
  if [ "$size" -gt "$max" ]; then
    reject "$label is too large"
  fi
}

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

TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/flowguard-install.XXXXXXXXXX")
echo "Downloading $asset from $REPO ($VERSION)..."
fetch "$base_url/$asset" "$TMP_DIR/$asset"
fetch "$base_url/$checksums" "$TMP_DIR/$checksums"
check_size "$TMP_DIR/$asset" 104857600 "$asset"
check_size "$TMP_DIR/$checksums" 1048576 "$checksums"

(cd "$TMP_DIR" && awk -v asset="$asset" '$2 == asset { print; found=1 } END { exit found ? 0 : 1 }' "$checksums" | sha256sum -c -)
tar -xzf "$TMP_DIR/$asset" -C "$TMP_DIR"
if [ ! -f "$TMP_DIR/$BIN_NAME" ]; then
  reject "$BIN_NAME not found in archive"
fi
check_size "$TMP_DIR/$BIN_NAME" 104857600 "$BIN_NAME"

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
if [ -f "$INSTALL_DIR/$BIN_NAME" ]; then
  $SUDO cp -p "$INSTALL_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME.bak"
fi
$SUDO install -m 0755 "$TMP_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"

echo "Installed $BIN_NAME to $INSTALL_DIR/$BIN_NAME"
if [ "$SKIP_SETUP" = "1" ] || [ "$SKIP_SETUP" = "true" ]; then
  if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files flowguard.service >/dev/null 2>&1; then
    if ! $SUDO systemctl restart flowguard; then
      if [ -f "$INSTALL_DIR/$BIN_NAME.bak" ]; then
        $SUDO cp -p "$INSTALL_DIR/$BIN_NAME.bak" "$INSTALL_DIR/$BIN_NAME"
        if ! $SUDO systemctl restart flowguard; then
          reject "flowguard service restart failed after upgrade; rolled back but restarting backup failed"
        fi
      fi
      reject "flowguard service restart failed after upgrade"
    fi
    echo "Restarted flowguard service if available."
  fi
  echo "Skipped setup wizard."
  exit 0
fi

echo "Starting interactive installer..."
if [ -r /dev/tty ]; then
  exec $SUDO "$INSTALL_DIR/$BIN_NAME" install "$@" < /dev/tty
fi
exec $SUDO "$INSTALL_DIR/$BIN_NAME" install "$@"
