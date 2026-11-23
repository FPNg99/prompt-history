#!/bin/sh
# Launcher for herdr-prompt-history.
#
# Herdr runs plugin commands with a minimal PATH, so extend it with the usual
# places nvim, pbcopy and herdr live (bob is a common nvim version manager).
# HOME, XDG_* and NVIM_APPNAME pass through untouched, so nvim loads the same
# config it does in a normal terminal.
PATH="$PATH:/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin:$HOME/.local/share/bob/nvim-bin:$HOME/go/bin:/usr/bin:/bin"
export PATH

root="${HERDR_PLUGIN_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
bin="$root/bin/herdr-prompt-history"

if [ ! -x "$bin" ]; then
  echo "herdr-prompt-history: binary not found, run herdr/install.sh (needs Go)" >&2
  exit 1
fi
exec "$bin" "$@"
