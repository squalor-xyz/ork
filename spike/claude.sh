#!/bin/zsh
# S01 vendor spike: claude. Six calls, cheapest alias (haiku), trivial prompts.
# Usage: zsh spike/claude.sh PROBE_REPO RAW_DIR FIXTURE_DIR
# PROBE_REPO is a throwaway git repo under the scratch directory.
# RAW_DIR is scratch for unredacted output. FIXTURE_DIR is testdata/vendors/claude.
set -u
source "${0:A:h}/common.zsh"
REPO=$1
RAW=$2
FIX=$3
cd "$REPO" || exit 1
mkdir -p "$RAW" "$FIX"
MODEL=haiku

# 1. Plain success, single JSON result. Gives the session id for probe 3.
probe 01-plain-json "$RAW" "$FIX" \
  claude -p --output-format json --model $MODEL "Reply with exactly the word OK and nothing else."
SID=$(grep -oE '"session_id": *"[0-9a-f-]{36}"' "$RAW/01-plain-json.stdout" | head -1 | grep -oE '[0-9a-f-]{36}')
if [[ -z "$SID" ]]; then
  print -u2 "no session id in probe 01; stopping before resume"
  exit 1
fi

# 2. Streamed output: NDJSON events with partial text deltas.
probe 02-stream-json "$RAW" "$FIX" \
  claude -p --output-format stream-json --verbose --include-partial-messages --model $MODEL "Count from 1 to 5, one number per line, and nothing else."

# 3. Resume the session from probe 1 by id.
probe 03-resume "$RAW" "$FIX" \
  claude -p --resume "$SID" --output-format json --model $MODEL "What exact word were you asked to reply with at the start of this session? One word only."

# 4. Tool allowlist that allows only Read. The Write must be denied.
probe 04-allowlist-denies-write "$RAW" "$FIX" \
  claude -p --output-format json --model $MODEL --allowedTools Read --permission-prompts none "Create a file named made-by-claude.txt that contains the word hi, then reply DONE."
repo_status 04-allowlist-denies-write "$FIX"

# 5. Read-only mode (plan) asked to edit an existing file.
probe 05-plan-mode-edit "$RAW" "$FIX" \
  claude -p --output-format json --model $MODEL --permission-mode plan "Change the word hello to bye in notes.txt, then reply DONE."
repo_status 05-plan-mode-edit "$FIX"

# 6. Forced failure: a model name that does not exist.
probe 06-bad-model "$RAW" "$FIX" \
  claude -p --output-format json --model no-such-model-zz "Reply with exactly the word OK."
