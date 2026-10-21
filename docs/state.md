# State

The files ork owns. Everything else is read live from slicer, adj and git
(`state.files`). File names and fields are *proposed*. Every file is JSON
(`state.format`). ork keeps the latest 100 finished runs
(`state.runs-retention`).

## What ork owns

| Data | File |
|---|---|
| Project registry and per-project settings | `~/.config/ork/projects.json` |
| UI preferences, free-form until v1 | `~/.config/ork/ui.json` |
| Daemon socket and pidfile (the pidfile is locked while a daemon lives) | `~/.local/state/ork/ork.sock`, `ork.pid` |
| One record per slice run | `~/.local/state/ork/runs/<run>/run.json` |
| Role reports | `~/.local/state/ork/runs/<run>/<role>-<round>.md` |
| Check output per round (S04) | `~/.local/state/ork/runs/<run>/checks-<round>.log` |
| A role's own temp directory (S04) | `~/.local/state/ork/runs/<run>/tmp/<role>-<round>/` |
| Fixture candidates: raw output of every non-ok vendor round or round with denials, mode 0600 | `~/.local/state/ork/fixtures/<vendor>/` |

ork honors `XDG_CONFIG_HOME` and `XDG_STATE_HOME`. Nothing is written
inside a project except what git and slicer write for a slice: the
worktree, the branch, the commit, and the ledger and notes through slicer
commands.

## projects.json

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

```json
{
  "version": 1,
  "id": "ork-20261018-161200-a1b2",
  "wave": "wave-20261018-161150-77aa",
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
  "questions": [{"role": "implementer", "round": 1, "text": "...", "at": "...", "answer": "...", "answered_at": "..."}],
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
  "created_at": "...",
  "updated_at": "..."
}
```

`state` and the role fields are ork's own record of the processes it
started. They are not a copy of slicer state. The ledger link is
`relay_run`, and `orchestrator_step` is ork's own step in it.

`state` is one of `queued`, `setup`, `implement`, `checks`, `review`, `gate`,
`awaiting-approval`, `commit`, `finish`, `committed`, `paused` and `stopped`;
`committed` and `stopped` are final. `queued` is not final: the run waits for
a slot under the caps and has no process and no writes. `round` counts implementer rounds and
`fails` counts gate FAILs. `reason` says why a slice is stopped or paused.
`pending` (kind `question`, `shape`, `lost`, `interrupted` or `error`, with
the state and role to re-enter, the reason, and for a question the text and
the answer) is set while a slice is paused. `feedback` names the file (kind
`checks`, `findings` or `fail`) that the next implementer round must act on.
A role with no `ended_at` is running; a restart marks it `lost`. `commit` is
`{"sha","title","at"}` once ork has committed. The fields `orchestrator_step`,
`fails`, `reason`, `pending`, `feedback`, `created_at`, `updated_at`, the
role's `model`, `effort` and `denials`, a check's `log`, and a write's
`error` are *proposed* (S04).

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

`writes` lists every slicer, adj and git write ork makes for the run, with
its argv, root, exit, error code and retry count, so a restart or the
owner can see exactly what ork changed (`state.writes`, S04). `argv` leaves
out the `--root` or `-C` that `root` records. A call that exits with the
slicer error code `locked` (exit 3) is retried after 250 ms, 500 ms, 1 s,
2 s and 4 s, and every attempt is an entry: the first has no `retry`, the
retries count from 1. A failure that is not an exit, such as a timeout or a
missing binary, has `exit` -1 and an `error` text. Reads (`slicer show`,
`check`, `config`, `git status`, `rev-parse`) are not writes. ork's own edit
of `info/exclude` is a git write with argv `["exclude", ".worktrees/"]`.
If recording a CLI write fails, the writer returns that error and makes no
further retry. The CLI may already have changed the project.

## Engine display snapshot

The daemon's snapshot has an `engine_runs` list, built by the same code as
`ork status`. Each entry has `id`, `item`, `state`, optional `wait`, optional
`relay_run`, optional `pending`, `created_at`, and optional `denials` (an array
of one text per denial, `Tool` or `Tool: command`, collected from the role
records in role order). A denial with no command is its tool name alone. A record from before
S45 has no `denied_commands`, so its `denials` are tool names. Denials are
omitted when empty and rendered by status and the TUI (`ui.denials-display`).
For a paused run, optional `pending` is the record's pending kind: `question`,
`shape`, `lost`, `interrupted` or `error` (`ui.pending-kind`, adj #108). Other
states omit it; older snapshots may omit it too.
For a paused run, `wait` includes the pause reason or question and the next
command: `ork retry RUN` without a question, or `ork answer RUN TEXT`.
The `relay_run` and `created_at` fields link ledger progress and
order replacement runs for the Overview (`ui.engine-runs-read`,
`ui.run-join-key`, `ui.stopped-newer`). These are read-only display fields.

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
  `landed` predicate and removes nothing without one; landing detection
  comes with the landing slice.
