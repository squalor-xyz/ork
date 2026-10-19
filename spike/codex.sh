#!/bin/zsh
# S01 vendor spike: codex. Four calls, cheapest catalog model (gpt-6-luna),
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
SID=$(grep 'thread.started' "$RAW/01-plain-stream-jsonl.stdout" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)
if [[ -z "$SID" ]]; then
  print -u2 "no thread id in probe 01; stopping before resume"
  exit 1
fi

# 2. Resume the session from probe 1 by id.
probe 02-resume-by-id "$RAW" "$FIX" \
  codex exec resume "$SID" -m $MODEL --json "What exact word were you asked to reply with at the start of this session? One word only."

# 3. Read-only sandbox asked to write a file through a shell command.
probe 03-readonly-sandbox-write "$RAW" "$FIX" \
  codex exec --json -m $MODEL -s read-only -C "$REPO" "Use a shell command to create a file named made-by-codex.txt that contains the word hi. Then reply DONE."
git -C "$REPO" status --porcelain > "$FIX/03-readonly-sandbox-write.repo-status"

# 4. Forced failure: a model name that does not exist.
probe 04-bad-model "$RAW" "$FIX" \
  codex exec --json -m no-such-model-zz -s read-only -C "$REPO" "Reply with exactly the word OK."
