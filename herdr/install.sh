#!/usr/bin/env bash
# Build step for the plugin: puts the binary in bin/.
#
# With Go installed it compiles this checkout, which always matches the
# source. Without Go it downloads the binary CI published for the manifest's
# version and checks it against its checksum.
#
# PROMPT_HISTORY_INSTALL=source or =download forces one of the two.
set -euo pipefail

REPO="FPNg99/prompt-history"
PATH="$PATH:/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$HOME/go/bin"
ROOT="${HERDR_PLUGIN_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
VERSION="$(sed -n 's/^version *= *"\(.*\)"/\1/p' "$ROOT/herdr-plugin.toml" | head -1)"
MODE="${PROMPT_HISTORY_INSTALL:-auto}"

build_from_source() {
  mkdir -p "$ROOT/bin"
  (cd "$ROOT" && go build -o bin/prompt-history ./cmd/prompt-history)
  echo "prompt-history: built bin/prompt-history from source"
}

# Runs in a subshell so its temp folder is removed however it ends. Every
# step is checked explicitly: `set -e` is off inside an `if` condition.
download_release() (
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *) echo "prompt-history: no prebuilt binary for $(uname -s)." >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    arm64|aarch64) arch="arm64" ;;
    x86_64|amd64)  arch="amd64" ;;
    *) echo "prompt-history: no prebuilt binary for $(uname -m)." >&2; exit 1 ;;
  esac

  asset="prompt-history-v${VERSION}-${os}-${arch}.tar.gz"
  base="https://github.com/${REPO}/releases/download/v${VERSION}"
  tmp="$(mktemp -d)" || exit 1
  trap 'rm -rf "$tmp"' EXIT

  echo "prompt-history: downloading ${asset}"
  curl -fsSL --retry 3 --retry-delay 2 -o "$tmp/$asset" "$base/$asset" || exit 1
  curl -fsSL --retry 3 --retry-delay 2 -o "$tmp/$asset.sha256" "$base/$asset.sha256" || exit 1

  cd "$tmp" || exit 1
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c "$asset.sha256" >/dev/null || { echo "prompt-history: checksum mismatch for ${asset}." >&2; exit 1; }
  else
    shasum -a 256 -c "$asset.sha256" >/dev/null || { echo "prompt-history: checksum mismatch for ${asset}." >&2; exit 1; }
  fi

  tar -xzf "$asset" || exit 1
  mkdir -p "$ROOT/bin" || exit 1
  install -m 0755 prompt-history "$ROOT/bin/prompt-history" || exit 1
  echo "prompt-history: installed v${VERSION} (${os}-${arch}) from the release"
)

have_go() { command -v go >/dev/null 2>&1; }

case "$MODE" in
  source)
    have_go || { echo "prompt-history: Go is not installed (https://go.dev/dl)." >&2; exit 1; }
    build_from_source
    ;;
  download)
    download_release
    ;;
  *)
    if have_go; then
      build_from_source
    elif ! download_release; then
      echo "prompt-history: could not download a prebuilt binary for v${VERSION}." >&2
      echo "Install Go (https://go.dev/dl) and run the install again to build from source." >&2
      exit 1
    fi
    ;;
esac
