#!/bin/zsh
# S01 vendor spike: codex. Seven calls: probes 01 to 04 (S01), 05 to 07 (S48). Cheapest catalog model (gpt-6-luna),
# trivial prompts. codex has no per-tool allowlist (codex exec --help lists
# only -s read-only, workspace-write, danger-full-access), so the write
# denial probe uses the read-only sandbox.
# Usage: zsh spike/codex.sh PROBE_REPO RAW_DIR FIXTURE_DIR
# PROBE_REPO is a throwaway git repo under the scratch directory.
# RAW_DIR is scratch for unredacted output. FIXTURE_DIR is testdata/vendors/codex.
set -u
source "${0:A:h}/common.zsh"
REPO=$1
RAW=$2
FIX=$3
cd "$REPO" || exit 1
mkdir -p "$RAW" "$FIX"
MODEL=gpt-6-luna

# 1. Plain success with JSONL events on stdout (also the streamed-output probe).
probe 01-plain-stream-jsonl "$RAW" "$FIX" \
  codex exec --json -m $MODEL -s read-only -C "$REPO" "Reply with exactly the word OK and nothing else."
SID=$(grep 'thread.started' "$RAW/01-plain-stream-jsonl.stdout" 2>/dev/null | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)
if [[ -z "$SID" ]]; then
  # S48: a dry run, or a rerun after probe 02 is recorded, has no raw output
  # from probe 01 and needs no thread id (probe skips by its .exit file).
  if [[ -n ${S01_DRY_RUN:-} || -e "$FIX/02-resume-by-id.exit" ]]; then
    SID=$(grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' "$FIX/02-resume-by-id.cmd" 2>/dev/null | head -1)
    SID=${SID:-THREAD-ID-01}
  else
    print -u2 "no thread id in probe 01; stopping before resume"
    exit 1
  fi
fi

# 2. Resume the session from probe 1 by id.
probe 02-resume-by-id "$RAW" "$FIX" \
  codex exec resume "$SID" -m $MODEL --json "What exact word were you asked to reply with at the start of this session? One word only."

# 3. Read-only sandbox asked to write a file through a shell command.
probe 03-readonly-sandbox-write "$RAW" "$FIX" \
  codex exec --json -m $MODEL -s read-only -C "$REPO" "Use a shell command to create a file named made-by-codex.txt that contains the word hi. Then reply DONE."
[[ -n ${S01_DRY_RUN:-} || -e "$FIX/03-readonly-sandbox-write.repo-status" ]] || git -C "$REPO" status --porcelain > "$FIX/03-readonly-sandbox-write.repo-status"

# 4. Forced failure: a model name that does not exist.
probe 04-bad-model "$RAW" "$FIX" \
  codex exec --json -m no-such-model-zz -s read-only -C "$REPO" "Reply with exactly the word OK."

# S48: workspace-write sandbox probes 05 to 07 (owner-approved 2026-10-23:
# exactly three more calls, codex total seven). REPO is the main checkout; WT
# is its linked worktree (git worktree add), created before the run. The
# main checkout holds an empty .git/slicer directory (slicer's ledger path).
# A probe counts only if its JSONL has a command event (checked by hand).
WT=${REPO}-wt
[[ -d "$WT" ]] || { print -u2 "no worktree at $WT; stopping before probes 05 to 07"; exit 1; }
# snap NAME: record both trees after a probe that ran. Not in a dry run, and
# never over an earlier snapshot (a rerun skips the probe).
snap() {
  [[ -n ${S01_DRY_RUN:-} || -e "$FIX/$1.repo-status" ]] && return 0
  repo_status "$1" "$FIX"
  git -C "$WT" status --porcelain > "$FIX/$1.worktree-status"
  ls -A "$REPO/.git/slicer" > "$FIX/$1.slicer-dir"
}
in_wt() { ( cd "$WT" && "$@" ); }

# 5. workspace-write run in the worktree: one positive control and two writes
# that should be outside the writable root (main checkout, main .git/slicer).
probe 05-workspace-write-paths "$RAW" "$FIX" \
  codex exec --json -m $MODEL -s workspace-write -C "$WT" "Run three separate shell commands, one per file, and report for each the exact result or error. A: write the word hi to $WT/inside.txt. B: write the word hi to $REPO/outside-main.txt. C: write the word hi to $REPO/.git/slicer/ledger-probe.txt. Then reply DONE."
snap 05-workspace-write-paths
SID5=$(grep 'thread.started' "$RAW/05-workspace-write-paths.stdout" 2>/dev/null | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)
if [[ -z "$SID5" ]]; then
  if [[ -n ${S01_DRY_RUN:-} || -e "$FIX/06-resume-workspace-write.exit" ]]; then
    SID5=$(grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' "$FIX/06-resume-workspace-write.cmd" 2>/dev/null | head -1)
    SID5=${SID5:-THREAD-ID-05}
  else
    print -u2 "no thread id in probe 05; stopping before resume"
    exit 1
  fi
fi

# 6. Resume probe 05's session with -s workspace-write (resume help lists no
# -s). Run from the worktree because resume has no -C. If it refuses, the
# exit and stderr fixtures hold the exact error.
probe 06-resume-workspace-write "$RAW" "$FIX" \
  in_wt codex exec resume "$SID5" -m $MODEL -s workspace-write --json "Run a shell command that writes the word hi to $WT/resumed.txt, then run one that writes the word hi to $REPO/resumed-outside.txt. Report the exact result or error of each, then reply DONE."
snap 06-resume-workspace-write

# 7. Socket bind in the worktree. A Unix socket is the asked probe; a loopback
# TCP bind rides in the same command (extra, one call).
probe 07-socket-bind "$RAW" "$FIX" \
  codex exec --json -m $MODEL -s workspace-write -C "$WT" "Run this one shell command and report its exact output and any error: python3 -c \"import socket; s=socket.socket(socket.AF_UNIX); s.bind('$WT/probe.sock'); print('unix bind ok'); t=socket.socket(); t.bind(('127.0.0.1',0)); print('tcp bind ok')\". Then reply DONE."
snap 07-socket-bind
