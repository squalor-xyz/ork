#!/bin/sh
# Build only. Invoke with sh; this script never restarts the daemon.
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
go -C "$here" build -o "$HOME/.local/bin/ork" ./cmd/ork
echo "built ~/.local/bin/ork"
