#!/bin/zsh
# Build ork into ~/bin and stop. It does not restart the daemon or open the TUI;
# run-in-home-dir.sh does both. A failed build stops the script.
set -eu
go -C "${0:A:h}" build -o ~/bin/ork ./cmd/ork
echo "built ~/bin/ork"


~/bin/ork daemon restart || echo "daemon not restarted (roles running?); it stays on the old build" >&2
