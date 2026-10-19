#!/bin/zsh
# S01 vendor spike: grok. Up to six calls, trivial prompts.
# Model: grok-4.5, the lower-numbered model that `grok models` lists besides
# the default grok-4.6. The CLI prints no prices, so "cheapest" is not
# verified here.
# Probe 01 is also a gate: if it exits non-zero, the script stops and the
# remaining probes are not run (no login is attempted).
# First run: the gate only matched "not authenticated", so the 403 from probe
# 01 did not stop the script and probes 02-06 ran too. Probe 03 then took
# its session id from the team id in probe 01's error text, so it is an
# invalid probe. The fixtures record exactly what ran.
# The session-id extraction (the grep that sets SID) was also changed after
# that run, to read only the session_id key. The 03 .cmd note applies: the
# .cmd for probe 03 was regenerated with S01_DRY_RUN=1 and S01_DRY_SID set to
# the team id the first run used, so no vendor call was made. See docs/design.md.
# Usage: zsh spike/grok.sh PROBE_REPO RAW_DIR FIXTURE_DIR
# PROBE_REPO is a throwaway git repo under the scratch directory.
# RAW_DIR is scratch for unredacted output. FIXTURE_DIR is testdata/vendors/grok.
set -u
source "${0:A:h}/common.zsh"
REPO=$1
RAW=$2
FIX=$3
cd "$REPO" || exit 1
mkdir -p "$RAW" "$FIX"
MODEL=grok-4.5

# 1. Plain success, single JSON result. Also a gate (see the header).
probe 01-plain-json "$RAW" "$FIX" \
  grok -p "Reply with exactly the word OK and nothing else." --output-format json -m $MODEL --cwd "$REPO" || {
  print -u2 "grok probe 01 exited non-zero: probes 02-06 not run"
  exit 1
}
SID=$(grep -oE '"session_id": *"[0-9a-f-]{36}"' "$RAW/01-plain-json.stdout" | head -1 | grep -oE '[0-9a-f-]{36}')
# Dry run only: probe 01 printed no session id, so the id for the 03 command
# line comes from S01_DRY_SID (the team id the first run used).
if [[ -z "$SID" && -n ${S01_DRY_RUN:-} ]]; then SID=${S01_DRY_SID:-}; fi

# 2. Streamed output: NDJSON in the Anthropic Messages wire format.
probe 02-stream-messages-json "$RAW" "$FIX" \
  grok -p "Count from 1 to 5, one number per line, and nothing else." --output-format streaming-messages-json --include-partial-messages -m $MODEL --cwd "$REPO"

# 3. Resume the session from probe 1 by id.
if [[ -z "$SID" ]]; then
  print -u2 "no session id in probe 01 output; stopping before resume"
  exit 1
fi
probe 03-resume-by-id "$RAW" "$FIX" \
  grok -p "What exact word were you asked to reply with at the start of this session? One word only." --resume "$SID" --output-format json -m $MODEL --cwd "$REPO"

# 4. Tool list that allows only Read, with auto-deny for anything else.
probe 04-allowlist-denies-write "$RAW" "$FIX" \
  grok -p "Create a file named made-by-grok.txt that contains the word hi, then reply DONE." --output-format json --tools Read --permission-mode dontAsk -m $MODEL --cwd "$REPO"
git -C "$REPO" status --porcelain > "$FIX/04-allowlist-denies-write.repo-status"

# 5. Plan permission mode asked to edit an existing file.
probe 05-plan-mode-edit "$RAW" "$FIX" \
  grok -p "Change the word hello to bye in notes.txt, then reply DONE." --output-format json --permission-mode plan -m $MODEL --cwd "$REPO"
git -C "$REPO" status --porcelain > "$FIX/05-plan-mode-edit.repo-status"

# 6. Forced failure: a model name that does not exist.
probe 06-bad-model "$RAW" "$FIX" \
  grok -p "Reply with exactly the word OK." --output-format json -m no-such-model-zz --cwd "$REPO"
