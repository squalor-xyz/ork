# State

The files ork owns. Everything else is read live from slicer, adj and git
(`state.files`). File names and fields are *proposed*. Every file is JSON
(`state.format`). ork keeps the latest 100 finished runs
(`state.runs-retention`).

## What ork owns

| Data | File |
|---|---|
| Project registry and per-project settings, created on the first add | `~/.config/ork/projects.json` |
| UI preferences, free-form until v1 | `~/.config/ork/ui.json` |
| Daemon socket and pidfile (the pidfile is locked while a daemon lives) | `~/.local/state/ork/ork.sock`, `ork.pid` |
| One record per slice run | `~/.local/state/ork/runs/<run>/run.json` |
| Role reports | `~/.local/state/ork/runs/<run>/<role>-<round>.md` |
| Check output per round (S04) | `~/.local/state/ork/runs/<run>/checks-<round>.log` |
| A role's own temp directory (S04) | `~/.local/state/ork/runs/<run>/tmp/<role>-<round>/` |
| A spec check's report and temp directory (S116, *proposed*); the verdicts themselves live in the daemon's memory | `~/.local/state/ork/spec-checks/<check>/` |
| Fixture candidates: raw output of every non-ok vendor round or round with denials, mode 0600 | `~/.local/state/ork/fixtures/<vendor>/` |

ork honors `XDG_CONFIG_HOME` and `XDG_STATE_HOME`. Nothing is written
inside a project except what git and slicer write for a slice: the
worktree, the branch, the commit, and the ledger and notes through slicer
commands.

## projects.json

A missing registry is empty and valid. ork never seeds projects or writes
this file on startup (`registry.no-seed`, adj 157). The first successful
`ork projects add` or TUI add creates the config directory and this file.


```json
{
  "version": 2,
  "global_cap": 4,
  "daemon_idle_minutes": 10,
  "projects": [
    {
      "name": "DataBall",
      "path": "/Users/jon/code/squalor-xyz/DataBall",
      "adj": false,
      "cap": 2,
      "worktree_pattern": ".worktrees/{id}",
      "branch_pattern": "{id}",
      "check_command": "dotnet test",
      "check_minutes": 10,
      "environment_lines": ["zsh on darwin; no timeout command", "..."],
      "budgets": {"minutes": 120, "tokens": 2000000},
      "permissions": {
        "implementer": ["Edit", "Write", "Bash(dotnet test*)", "Bash(slicer *)"],
        "reviewer": "read-only"
      },
      "vendor_env": {"codex": {"VENDOR_CODEX_NETWORK": "1"}}
    }
  ]
}
```

Notes on the fields:
- `daemon_idle_minutes` is how long an idle daemon waits before it exits
  (`daemon.idle`). 0 or absent means 10. It is new in version 2.
- `check_command` is a fallback. ork prefers slicer's config key once
  slicer has one (`project.check-command`). The `check_minutes` value sets
  the stale flag and goes into briefs.
- `permissions` is the per-role allowlist (`launch.permissions`). Its exact
  syntax per vendor waits on the vendor spike (design.md).
- `vendor_env` is shown as an example. The DataBall entry reflects the
  agents repo's note that Codex needs network access for `dotnet test`
  (`agents/setup/lib/workflow-vendor.sh`).

## run.json

ork writes `run.json` at mode 0600 (adj #106).
`lineup` and `budgets` are the launch values echoed by the plan
(`run.launch-wire`). The lineup includes any full role chains supplied at
launch (`lineup.launch-edit`); the gate uses the reviewer's chain. Budgets
merge each nonzero launch field over the registry default. These values
apply to this run only and do not change the matrix or registry
(`lineup.override-scope`).

`decisions` is optional (S116, *proposed*). When the owner answered the item's
spec check questions, it holds the `Owner decisions (date)` paragraph that was
appended to the item's spec in the main checkout. The run's worktree lacks
that uncommitted edit, so every role's brief carries the paragraph in an
`Owner decisions` section.

```json
{
  "version": 1,
  "id": "ork-20261018-161200-a1b2",
  "wave": "wave-20261018-161150-77aa",
  "decisions": "Owner decisions (2026-10-18): ...",
  "project": "adjudicate",
  "item": "S18",
  "worktree": "/Users/jon/code/squalor-xyz/adjudicate/.worktrees/S18",
  "branch": "S18",
  "relay_run": "run-20261018-161205-c0de",
  "orchestrator_step": "1",
  "contract": 7,
  "state": "review",
  "round": 1,
  "fails": 0,
  "reason": "",
  "pending": null,
  "feedback": null,
  "lineup": {
    "implementer": [{"vendor": "claude", "model": "sonnet", "effort": "high"},
                    {"vendor": "codex", "model": "gpt-6.1-sol", "effort": "medium"}],
    "reviewer": [{"vendor": "claude", "model": "opus", "effort": "medium"}]
  },
  "roles": [
    {"role": "implementer", "round": 1, "vendor": "claude", "model": "sonnet", "effort": "high",
     "session": "...", "pgid": 12345, "step": "2", "started_at": "...", "ended_at": "...",
     "exit": "ok", "tokens": 41000, "cost_usd": 0.62, "report": "implementer-1.md"}
  ],
  "checks": [{"round": 1, "command": "go test ./...", "ok": true, "seconds": 58, "log": "checks-1.log"}],
  "budgets": {"minutes": 120, "tokens": 2000000},
  "approvals": [{"what": "slice", "at": "..."}],
  "questions": [{"role": "implementer", "round": 1, "key": "area.topic", "text": "...", "at": "...", "answer": "...", "answered_at": "..."}],
  "findings": [{"kind": "later", "role": "reviewer", "round": 1, "title": "Add a --json flag", "text": "...", "at": "...", "filed": "S130", "filed_at": "..."}],
  "card": {"round": 1, "verdict": "PASS", "passed": 12, "failed": 0, "diff_stat": " a.go | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)",
           "untracked": ["b.go"], "human_checks": [{"text": "...", "state": "ticked"}]},
  "fixes": [{"round": 2, "note": "Rename the flag.", "at": "..."}],
  "writes": [
    {"at": "2026-10-19T16:12:05Z", "tool": "slicer", "root": "/.../adjudicate/.worktrees/S18",
     "argv": ["relay-run", "begin", "S18", "--worktree", "/.../S18", "--json"],
     "exit": 0, "ms": 640},
    {"at": "2026-10-19T16:40:11Z", "tool": "slicer", "root": "/.../S18",
     "argv": ["note", "S18", "--text", "Review FAIL: ...", "--render", "--json"],
     "exit": 3, "code": "locked", "ms": 5100},
    {"at": "2026-10-19T16:40:16Z", "tool": "slicer", "root": "/.../S18",
     "argv": ["note", "S18", "--text", "Review FAIL: ...", "--render", "--json"],
     "exit": 0, "retry": 1, "ms": 420}
  ],
  "commit": null,
  "dismissed_at": "2026-10-20T16:40:00Z",
  "created_at": "...",
  "updated_at": "..."
}
```

`state` and the role fields are ork's own record of the processes it
started. They are not a copy of slicer state. The ledger link is
`relay_run`, and `orchestrator_step` is ork's own step in it.

`state` is one of `queued`, `setup`, `implement`, `checks`, `review`, `architect`, `gate`,
`awaiting-approval`, `commit`, `finish`, `committed`, `paused` and `stopped`;
`committed` and `stopped` are final. `queued` is not final: the run waits for
a slot under the caps and has no process and no writes. `round` counts implementer rounds and
`fails` counts gate FAILs. `reason` says why a slice is stopped or paused.
`pending` (kind `question`, `shape`, `lost`, `interrupted`, `budget` or `error`, with
the state and role to re-enter, the reason, and for a question the optional adj
`key`, the text and the answer) is set while a slice is paused.
Both `pending` and each entry in `questions[]` may carry `ask`, the structured
question object: `title`, `options:[{label,summary}]`, one-based `recommend`
and `reason` (adj #168). The raw question text is kept alongside it. Older
records without `ask` continue to use free text. An older ork refuses a
`run.json` with `ask` because `Store.Open` uses strict keys. `feedback`
names the file (kind
`checks`, `findings`, `architect`, `fail` or `owner`) that the next implementer round must act on.
The `architect` state runs only when the lineup includes that role (adj #151),
after accepted reviewer findings and before the gate (adj #123). Its whole
report becomes `architect` feedback for a fix round (adj #150).
`findings` (omitted when empty; *proposed*, S120, adj #163) holds the review
findings that are not fixes: `kind` `owner` (an owner question that does not
block, with the structured `ask` and an optional `key`) or `later` (an offer to
file a slice). `title` is the finding's one-line title and `text` its body.
`filed` and `filed_at` are set once the owner confirmed and ork ran
`slicer add` and `slicer promote` for it; `filed` is the new slice id. A finding
raised again under the same title is kept once. An older ork refuses a
`run.json` with `findings` because `Store.Open` uses strict keys.
`card` (omitted until the gate first passes; *proposed*, S118) is the approval
card, built each time the gate passes: the gate's `verdict` and test counts
(`passed`, `failed`, from its `Tests:` line), `diff_stat` (the output of `git
diff --stat HEAD` in the worktree, which ork has not committed yet) and
`untracked` (new files that stat does not count), and `human_checks`, the
`[HUMAN CHECK]` bullets of the slice read with `slicer show`. Each check's
`state` is empty (open), `ticked` or `later`; the owner sets it with
`run.check`. A check with the same text keeps its mark when the card is built
again after a fix round. `checks_error` says why the checks could not be read.
`fixes` (omitted when empty; *proposed*, S118) keeps each note the owner sent
to a fix round from the card: the `round` that acts on it, the `note` and the
time. The note is also the file `owner-fix-N.md` in the run directory, which is
the `owner` feedback of that round. An older ork refuses a `run.json` with
`card` or `fixes` because `Store.Open` uses strict keys.
A role with no `ended_at` is running; a restart marks it `lost`. `commit` is
`{"sha","title","at"}` once ork has committed. The fields `orchestrator_step`,
`fails`, `reason`, `pending`, `feedback`, `created_at`, `updated_at`, the
role's `model`, `effort` and `denials`, a check's `log`, and a write's
`error` are *proposed* (S04).

A stopped run can be dismissed with `ork dismiss RUN` (S34, adj #117).
This sets `dismissed_at` to the dismissal time. It is omitted until then.
Dismissal removes the run from the snapshot, Needs you and `ork status`.
It keeps the record and its files; finished-run retention is unchanged.
A dismissed run still hides older stopped runs of the same slice.
Repeating dismissal does not change the record. A run that is not stopped
returns `run is not stopped` (`ErrNotStopped`). The version stays 1.
An older ork refuses a `run.json` with `dismissed_at` because `Store.Open`
uses strict keys (`DisallowUnknownFields`).

A role's `denials` lists the tool name of each permission denial the vendor
reported. `denied_commands` (S45, adj #105) holds one `{tool, command}` entry
per denial, in the same order. `command` is the Bash command of a Bash call,
or the file path (`tool_input.file_path`) of a file tool. It never holds file
content, and it is left out for a denial with neither. A recorded command can
hold secrets, so it is capped, not scrubbed. CR and LF become spaces, then
every other control character (ESC, NUL, TAB and the rest) is removed from
commands recorded from S51 on (adj #106), so no escape sequence from them
reaches `ork status` or the TUI. A pre-S51 record keeps the text it stored.
Then at most 200 characters are kept including the trailing `...` (a longer
text is cut to 197 characters and `...`). ork does not remove secrets from it.
For that reason ork writes `run.json` and the other files it writes in a run
directory (the check logs, and a report ork saves from the vendor result) at
mode 0600, and writes an existing file back at 0600 (adj #106). A record with no
denied commands is written as before, and the version stays 1. A pre-S45 ork
refuses a run.json that holds `denied_commands`, because it refuses unknown
keys.

For a paused `question`, `ork answer RUN TEXT` resumes the role with the
answer. For a `shape`, `error`, `lost` or `interrupted` pause, `ork retry RUN`
calls `Engine.Retry` to rerun the pending role or step (adj #101,
`engine.pause-resume`). `ork stop RUN` stops the run. Retrying a question
names `answer` and `stop`; answering a pause without a question names
`retry`.

Question `key` is optional and omitted when absent. It comes from a role report
line `Decision key: area.topic` under `## Owner needed` and is also copied to
`pending.key`. The proposed display field `EngineRun.key` carries it to the TUI.

`writes` lists every slicer, adj and git write ork makes for the run, with
its argv, root, exit, error code and retry count, so a restart or the
owner can see exactly what ork changed (`state.writes`, S04). `argv` leaves
out the `--root` or `-C` that `root` records. A call that exits with the
tool error code `locked` (exit 3) is retried after 250 ms, 500 ms, 1 s,
2 s and 4 s, and every attempt is an entry: the first has no `retry`, the
retries count from 1. A failure that is not an exit, such as a timeout or a
missing binary, has `exit` -1 and an `error` text. Reads (`slicer show`,
`check`, `config`, `git status`, `rev-parse`) are not writes. As an exception,
all adj calls while filing an answer (`check`, `ask`, `decide`) are recorded
with tool `adj` and the project main checkout as root. Both writes receive
the caller's attribution through `--by`. ork's own edit
of `info/exclude` is a git write with argv `["exclude", ".worktrees/"]`.
If recording a CLI write fails, the writer returns that error and makes no
further retry. The CLI may already have changed the project.

## Engine display snapshot

The daemon's snapshot has an `engine_runs` list, built by the same code as
`ork status`. Each entry has `id`, `item`, `state`, optional `wait`, optional
`relay_run`, optional `pending`, `created_at`, and optional `denials` (an array
of distinct denial texts, `Tool` or `Tool: command`, in first-seen role order).
Texts are deduplicated across all roles. Repeats append a count, such as
`Write: /x.txt x2`. A denial with no command is its tool name alone. A record from before
S45 has no `denied_commands`, so its `denials` are tool names. Denials are
omitted when empty and rendered by status and the TUI (`ui.denials-display`).
The *proposed* optional `error` field holds a recovery or background job error.
It lives in daemon memory only and is absent from `run.json`. It appears when
the run is not busy. The next job clears it; cancellation is not a failure.
The stored state stays unchanged (adj 116).
The *proposed* optional `unavailable` boolean marks a run with no engine because
its project is no longer registered. It has no owner actions and belongs in
Needs you. The daemon still returns its record and error to Go callers.
The socket reply returns the error alone; the next snapshot carries the row.
The *proposed* `projects.list.unavailable` list holds display-only project
statuses, separate from the registered `projects` list. Each has the recorded
project name, `unavailable: true` and its engine rows. Status includes these
groups with the same optional `unavailable` flag. Their snapshots contain
only stored engine rows and the *proposed* `unavailable: true` flag.
For a paused run, optional `pending` is the record's pending kind: `question`,
`shape`, `lost`, `interrupted`, `budget` or `error` (`ui.pending-kind`, adj #108). Other
states omit it; older snapshots may omit it too.
For a paused run, `wait` includes the pause reason or question and the next
command: `ork retry RUN` without a question, or `ork answer RUN TEXT`.
The `relay_run` and `created_at` fields link ledger progress and
order replacement runs for the Overview (`ui.engine-runs-read`,
`ui.run-join-key`, `ui.stopped-newer`). These are read-only display fields.
The *proposed* optional `updated_at` comes from the run record's last update.
It is omitted when zero. Snapshot and `ork status --json` include it.
Stopped Overview rows use it to show their age.

The optional `branch` and `worktree` display fields are *proposed* and come
from the run record. An omitted `merged` means the merge state is unknown;
true means merged and false means unmerged. The daemon supplies it from the
HEAD-change check (`landing.merge-detect`, adj #128). It remembers the last HEAD per project. The first successful HEAD
read and each changed HEAD check every committed branch with
`git -C PATH merge-base --is-ancestor BRANCH HEAD`. Exit 0 means merged,
exit 1 means unmerged, and another error leaves the state unknown. An
unchanged HEAD adds no merge reads.

The optional `roles` array is *proposed*. Each entry carries `role`, `round`,
`vendor`, optional `model`, `effort` and `step`, `started_at`, optional
`ended_at` and `exit`. It omits process, session, report and usage data.
The daemon builds it from the run record for the run tree (`ui.run-tree`).
The TUI uses the linked ledger steps when present, or these role rounds when
the ledger run is absent from the active snapshot. In `ork status`,
`active_relays` keeps ledger counts only. Its `engine_runs` carries the same
*proposed* `roles`. The ledger `Relay` wire gains no tree field.

## Rules

- **Atomic writes:** write a temporary file in the same directory, then
  rename it.
- **One directory per run,** so parallel runs never write the same file.
- **Strict keys:** an unknown key in `projects.json` is an error naming it;
  new fields come with a version bump (`registry.unknown-keys`).
- **Zero means unset:** `global_cap`, `cap` and `check_minutes` of 0 read
  as absent, and the default applies (`registry.zero-values`).
- **Versioned files:** every file has a `version`, and ork refuses a
  version it does not know. `projects.json` is at version 2; ork still reads
  version 1 files, which lack `daemon_idle_minutes` and are strict about it
  (a version 1 file with that key is an error), and the next save writes
  version 2.
- **No secrets:** no tokens or credentials in any ork file. Fixture
  candidates are the exception: they hold raw vendor output at mode 0600
  under the state directory, never in a repo, and are redacted when the owner
  promotes one into `testdata/` (`vendor.fixture-candidates`).
- **Retention:** when more than 100 finished runs exist, the daemon removes
  the oldest run directories. It never removes a run whose branch is not
  landed, or a run that is still active. `runrec.Store.Prune` takes a
  `landed` predicate and removes nothing without one. The daemon calls it
  once after recovery and once after each project merge-check pass, using
  cached merge results. Unknown and unmerged branches stay. Pruning removes
  run directories only; worktrees and branches require confirmed cleanup
  (`git.cleanup-key`, adj #152). Failed cleanup keeps the run record.


## Self-hosting (engine.self-host, adj 166)

*Proposed wire fields:* each status project carries `self_host` and
`behind_main`. Snapshots carry both fields and the existing global
`roles_running` state to the TUI for the idle notice. They are derived from the daemon's VCS revision and local
`main`, with no registry field. A dev build without a revision or an unknown
commit never matches. Behind means a different main head that descends from
the build revision. Failed Git reads do not establish behind state.

*Proposed behavior and wording:* `daemon behind main: restart with ork daemon restart`.
It fires on any landing on main, not only engine landings, while no run is
active. In the TUI it replaces the stale-build marker while shown. Text
doctor prints `[WARN] daemon behind main: restart with ork daemon restart`
on stdout. Status prints `ork: warning: daemon behind main: restart with ork daemon restart`
on stderr. The daemon continues using its original binary, never a relay
worktree build, and never restarts itself. See docs/design.md for wave
admission rules.

### Transient role detail (S99)

The output tail is daemon memory only. It is never a `run.json` field and is
never copied to a report file. Each role attempt resets its 2 KB tail and
live token count; ended attempts and daemon restarts expose no tail. Run
environment values selected by the rule in dashboard.md are masked before
text enters the buffer. The rule cannot mask secrets it does not know.

The proposed snapshot fields `engine_runs[].tail`, `tokens` and `cost_usd`
carry transient assistant text, recorded plus live tokens, and completed
round costs. The proposed role display fields `tokens` and `cost_usd` copy
the existing RoleRecord values. Final Result usage remains the authoritative
persisted usage; live usage never writes the record. This adds no state file.
Vendor raw output and final reports keep their existing handling; this tail
rule does not redact those independent artifacts.

Proposed snapshot flags `usage_known`, `live_usage_known` and
`tail_truncated` carry usage presence (including reported zero) and whether
the memory buffer dropped older text. These flags are not written to disk.
A stored zero cost from a vendor without a cost field is displayed as `$-`.
