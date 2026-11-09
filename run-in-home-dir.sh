#!/bin/sh
# Build ork into ~/.local/bin, restart the daemon on it, and open the TUI.
# A failed build stops the script. A refused daemon restart (roles running)
# prints a note and still opens the TUI.
# POSIX sh, so it runs the same under bash and zsh (`bash run-in-home-dir.sh`,
# `zsh run-in-home-dir.sh` or direct execution).
set -eu
here=$(cd "$(dirname "$0")" && pwd -P)
go -C "$here" build -o "$HOME/.local/bin/ork" ./cmd/ork
~/.local/bin/ork daemon restart || echo "daemon not restarted (roles running?); it stays on the old build" >&2
exec ~/.local/bin/ork
