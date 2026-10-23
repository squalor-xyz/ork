# Shared helpers for the S01 vendor spike. Sourced by spike/<vendor>.sh.
# Throwaway: the probes make real, billed vendor calls. Owner budget: about
# six calls per vendor, trivial prompts, cheapest model.

# Redact one file (or stdin) and print the result to stdout. Path rules run
# first, so the placeholders are readable and the long-run rule does not
# eat the project directory name. Key-like values come next, then key-like
# field names: the S01 check pattern matches apiKeySource, so the field name
# is renamed too. Values of such fields that hold secrets are caught by the
# value rule above it.
redact_file() {
  sed -E \
    -e 's#/Users/jon/code/squalor-xyz/ork/\.scratch/s48/probe-repo-wt#<worktree>#g' \
    -e 's#/Users/jon/code/squalor-xyz/ork/\.scratch/s48/probe-repo#<probe-repo>#g' \
    -e 's#/private/tmp/claude-501/-Users-jon-code-squalor-xyz-ork/[0-9a-f-]{36}/scratchpad/s01/implementer/probe-repo#<probe-repo>#g' \
    -e 's#/private/tmp/claude-501/[A-Za-z0-9_./-]*#<scratch>#g' \
    -e 's#-Users-jon-code-squalor-xyz-ork[A-Za-z0-9_.-]*#<project-dir>#g' \
    -e 's#/tmp/cc-socks/[A-Za-z0-9_./-]*#<tmp>/cc-socks/<socket>#g' \
    -e 's#/Users/jon#<home>#g' \
    -e 's#/private/tmp#<tmp>#g' \
    -e 's/sk-[A-Za-z0-9_-]{8,}/sk-REDACTED/g' \
    -e 's/[Bb][Ee][Aa][Rr][Ee][Rr] [A-Za-z0-9._~+\/-]{8,}/Bearer REDACTED/g' \
    -e 's/(([Aa][Pp][Ii][_-]?[Kk][Ee][Yy]|[Tt][Oo][Kk][Ee][Nn]|[Ss][Ee][Cc][Rr][Ee][Tt]|[Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd])"?[[:space:]]*[:=][[:space:]]*"?)[A-Za-z0-9._+\/=-]{8,}/\1REDACTED/g' \
    -e 's/[A-Za-z0-9_-]{40,}/REDACTED-LONG/g' \
    -e 's/team [0-9a-f]{8}-[0-9a-f-]{27}/team REDACTED-TEAM-ID/g' \
    -e 's/Session "[0-9a-f]{8}-[0-9a-f-]{27}"/Session "REDACTED-ID"/g' \
    -e 's/(--resume )[0-9a-f]{8}-[0-9a-f-]{27}/\1REDACTED-ID/g' \
    -e 's/[Aa][Pp][Ii][_-]?[Kk][Ee][Yy][A-Za-z_]*/REDACTED-KEY-NAME/g' \
    "${1:-/dev/stdin}"
}

# probe NAME RAW_DIR FIX_DIR CMD...
# Runs CMD once with stdin closed. Raw stdout and stderr stay in RAW_DIR
# (outside the worktree). The redacted stdout, stderr, exit code and the
# command line (with the probe repo shown as <probe-repo>) go to FIX_DIR.
# With S01_DRY_RUN set, only the redacted command line is written and CMD is
# not run; this regenerates .cmd files without a vendor call.
# Returns CMD's exit code (0 for a skipped or dry-run probe).
probe() {
  local name=$1 raw=$2 fix=$3
  shift 3
  local shown=${(j: :)@}
  # S48: WT (the linked worktree) is a path under REPO's parent; map it first.
  [[ -n ${WT:-} ]] && shown=${shown//$WT/<worktree>}
  shown=${shown//$REPO/<probe-repo>}
  if [[ -n ${S01_DRY_RUN:-} ]]; then
    print -r -- "$shown" | redact_file > "$fix/$name.cmd"
    print -r -- "probe $name dry run: command line written"
    return 0
  fi
  # Re-running a script skips probes whose exit code is already recorded, so
  # a later run never spends a second call on the same probe.
  if [[ -e "$fix/$name.exit" ]]; then
    print -r -- "probe $name skipped: already recorded"
    return 0
  fi
  print -r -- "$shown" | redact_file > "$fix/$name.cmd"
  local rc
  "$@" </dev/null >"$raw/$name.stdout" 2>"$raw/$name.stderr"
  rc=$?
  print -r -- "$rc" > "$fix/$name.exit"
  redact_file "$raw/$name.stdout" > "$fix/$name.stdout"
  redact_file "$raw/$name.stderr" > "$fix/$name.stderr"
  print -r -- "probe $name exit=$rc"
  return $rc
}

# repo_status NAME FIX_DIR: record the probe repo's git state after a probe.
repo_status() {
  git -C "$REPO" status --porcelain > "$2/$1.repo-status"
}
