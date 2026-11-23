#!/usr/bin/env bash
# Build step for the plugin: compiles the binary into bin/. Needs Go.
set -euo pipefail

PATH="$PATH:/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$HOME/go/bin"
ROOT="${HERDR_PLUGIN_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"

if ! command -v go >/dev/null 2>&1; then
  echo "herdr-prompt-history: Go is not installed (https://go.dev/dl)." >&2
  exit 1
fi

mkdir -p "$ROOT/bin"
(cd "$ROOT" && go build -o bin/herdr-prompt-history ./cmd/herdr-prompt-history)
echo "herdr-prompt-history: built bin/herdr-prompt-history"
