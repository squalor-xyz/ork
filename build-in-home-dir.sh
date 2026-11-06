#!/bin/sh
# Build ork into ~/bin and stop. It does not restart the daemon or open the TUI;
# run-in-home-dir.sh does both. A failed build stops the script.
# POSIX sh, so it runs the same under bash and zsh.
set -eu
here=$(cd "$(dirname "$0")" && pwd -P)
go -C "$here" build -o ~/bin/ork ./cmd/ork
echo "built ~/bin/ork"


~/bin/ork daemon restart || echo "daemon not restarted (roles running?); it stays on the old build" >&2
