#!/bin/zsh
# Build ork into ~/bin, restart the daemon on it, and open the TUI.
# A failed build stops the script. A refused daemon restart (roles running)
# prints a note and still opens the TUI.
set -eu
go -C "${0:A:h}" build -o ~/bin/ork ./cmd/ork
~/bin/ork daemon restart || echo "daemon not restarted (roles running?); it stays on the old build" >&2
exec ~/bin/ork
