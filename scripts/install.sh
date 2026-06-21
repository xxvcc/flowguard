#!/usr/bin/env sh
set -eu

REPO="${FLOWGUARD_REPO:-xxvcc/flowguard}"
VERSION="${FLOWGUARD_VERSION:-latest}"
INSTALL_DIR="${FLOWGUARD_INSTALL_DIR:-/usr/local/bin}"
BASE_URL="${FLOWGUARD_BASE_URL:-}"
SKIP_SETUP="${FLOWGUARD_SKIP_SETUP:-0}"
NO_RESTART="${FLOWGUARD_NO_RESTART:-0}"
MINISIGN_PUBKEY="${FLOWGUARD_MINISIGN_PUBKEY:-}"
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

validate_download_url() {
  case "$1" in
    https://*) return 0 ;;
    http://localhost/*|http://127.0.0.1/*|http://\[::1\]/*) return 0 ;;
    *) reject "download URL must be https, except localhost test mirrors: $1" ;;
  esac
}

if command -v curl >/dev/null 2>&1; then
  fetch() {
    url="$1"
    out="$2"
    validate_download_url "$url"
    final_url=$(curl -fsSLw '%{url_effective}' "$url" -o "$out")
    validate_download_url "$final_url"
  }
elif command -v wget >/dev/null 2>&1; then
  fetch() {
    url="$1"
    out="$2"
    validate_download_url "$url"
    case "$url" in
      https://*) wget --https-only -qO "$out" "$url" ;;
      http://localhost/*|http://127.0.0.1/*|http://\[::1\]/*) wget -qO "$out" "$url" ;;
    esac
  }
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
signature="checksums.txt.minisig"

TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/flowguard-install.XXXXXXXXXX")
echo "Downloading $asset from $REPO ($VERSION)..."
fetch "$base_url/$asset" "$TMP_DIR/$asset"
fetch "$base_url/$checksums" "$TMP_DIR/$checksums"
check_size "$TMP_DIR/$asset" 104857600 "$asset"
check_size "$TMP_DIR/$checksums" 1048576 "$checksums"
if [ -n "$MINISIGN_PUBKEY" ]; then
  need_cmd minisign
  fetch "$base_url/$signature" "$TMP_DIR/$signature"
  check_size "$TMP_DIR/$signature" 1048576 "$signature"
  minisign -Vm "$TMP_DIR/$checksums" -x "$TMP_DIR/$signature" -P "$MINISIGN_PUBKEY"
fi

(cd "$TMP_DIR" && awk -v asset="$asset" '$2 == asset { print; found=1 } END { exit found ? 0 : 1 }' "$checksums" | sha256sum -c -)
tar -tzf "$TMP_DIR/$asset" > "$TMP_DIR/tar.list"
tar_entry=$(awk '$0 == "flowguard" || $0 == "./flowguard" { print; found=1; exit } END { exit found ? 0 : 1 }' "$TMP_DIR/tar.list") || reject "$BIN_NAME not found in archive"
bad_entry=$(awk '$0 != "flowguard" && $0 != "./flowguard" { print; exit }' "$TMP_DIR/tar.list")
if [ -n "$bad_entry" ]; then
  reject "archive contains unexpected entry: $bad_entry"
fi
tar -xzf "$TMP_DIR/$asset" -C "$TMP_DIR" "$tar_entry"
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

run_as_root() {
  if [ -n "$SUDO" ]; then
    "$SUDO" "$@"
  else
    "$@"
  fi
}

exec_as_root() {
  if [ -n "$SUDO" ]; then
    exec "$SUDO" "$@"
  fi
  exec "$@"
}

run_as_root mkdir -p "$INSTALL_DIR"
if [ -f "$INSTALL_DIR/$BIN_NAME" ]; then
  run_as_root cp -p "$INSTALL_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME.bak"
fi
run_as_root install -m 0755 "$TMP_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"

echo "Installed $BIN_NAME to $INSTALL_DIR/$BIN_NAME"
if [ "$SKIP_SETUP" = "1" ] || [ "$SKIP_SETUP" = "true" ]; then
  if [ "$NO_RESTART" = "1" ] || [ "$NO_RESTART" = "true" ]; then
    echo "Skipped flowguard service restart."
  elif command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files flowguard.service --no-legend 2>/dev/null | awk '$1 == "flowguard.service" { found=1 } END { exit found ? 0 : 1 }'; then
    if ! run_as_root systemctl restart flowguard; then
      if [ -f "$INSTALL_DIR/$BIN_NAME.bak" ]; then
        run_as_root cp -p "$INSTALL_DIR/$BIN_NAME.bak" "$INSTALL_DIR/$BIN_NAME"
        if ! run_as_root systemctl restart flowguard; then
          reject "flowguard service restart failed after upgrade; rolled back but restarting backup failed"
        fi
        reject "flowguard service restart failed after upgrade; rolled back to previous binary and service restarted"
      fi
      reject "flowguard service restart failed after upgrade; no backup binary was available"
    fi
    echo "Restarted flowguard service if available."
  fi
  echo "Skipped setup wizard."
  exit 0
fi

echo "Starting interactive installer..."
if [ -r /dev/tty ]; then
  exec_as_root "$INSTALL_DIR/$BIN_NAME" install --install-dir "$INSTALL_DIR" "$@" < /dev/tty
fi
exec_as_root "$INSTALL_DIR/$BIN_NAME" install --install-dir "$INSTALL_DIR" "$@"
