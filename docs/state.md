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
| Role reports; `roles[].report` keeps the name, read on demand by `run.report` (S176, *proposed*) | `~/.local/state/ork/runs/<run>/<role>-<round>.md` or `<role>-<round>-attempt-<attempt>.md` |
| Check output per round (S04) | `~/.local/state/ork/runs/<run>/checks-<round>.log` |
| A role's own temp directory (S04) | `~/.local/state/ork/runs/<run>/tmp/<role>-<round>/` |
| The UI pass's renders (S121, *proposed*): `VIEW-80x24.txt`, `VIEW-120x40.txt`, `VIEW-ascii.txt` | `~/.local/state/ork/runs/<run>/tmp/ui-<round>/renders/` |
| A spec check's identity, report and temp directory, plus owner answers tied to the spec fingerprint until run start or a spec change (S143); role verdicts live in daemon memory | `~/.local/state/ork/spec-checks/<project-key>/<item-key>/` |
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
      "vendor_env": {"codex": {"VENDOR_CODEX_NETWORK": "1"}},
      "push": true,
      "ui_doc": "docs/ui.md"
    }
  ]
}
```

Notes on the fields:
- `worktree_pattern` and `branch_pattern` accept `{id}` for the unchanged
  item id and `{id_lower}` (*proposed*, S124) for its lowercase form. For
  item `S78`, `.worktrees/{id_lower}` gives `.worktrees/s78` and
  `{id_lower}` gives branch `s78`. Other text keeps its case. The defaults
  remain `.worktrees/{id}` and `{id}`. A relative worktree path is resolved
  from the project directory; an absolute path is used as given, after
  cleaning. Before rolling back to an ork binary without S124, put the
  registry patterns back to `{id}`: an older ork leaves `{id_lower}` as
  literal text, and every item would plan the same worktree.
- `daemon_idle_minutes` is how long an idle daemon waits before it exits
  (`daemon.idle`). 0 or absent means 10. It is new in version 2.
- `check_command` is a fallback. ork prefers slicer's config key once
  slicer has one (`project.check-command`). The `check_minutes` value sets
  the check command timeout (`internal/engine/states.go:198-202`).
- `permissions` is the per-role allowlist (`launch.permissions`). Its exact
  syntax per vendor waits on the vendor spike (design.md).
- `push` (*proposed*, S119, adj 162) says ork runs `git push` in the main
  checkout after it lands a slice there. Absent or `false` means ork never
  pushes (the example turns it on). ork writes it only when the owner runs
  `ork projects set NAME --push on|off` (below); the owner can also edit the
  file (docs/manual-steps.md). An older ork refuses a registry with `push`
  because `Load` uses strict keys.
- `ui_doc` (*proposed*, S136, owner decision 2026-10-27) is the path of the
  project's UI design doc, relative to the project (the example uses
  `docs/ui.md`). The UI pass's brief names it, so the pass judges the screens
  against that project's own design and not ork's. Absent means the brief
  tells the UI pass to find the project's own UI docs. The owner sets it with
  `ork projects set NAME --ui-doc PATH` or by editing the file. An older ork
  refuses a registry with `ui_doc` because `Load` uses strict keys.
- `ork projects set` (*proposed*, S161, owner decision 2026-10-27) is the only
  way ork writes the optional per-project fields. It changes `push`, `ui_doc`,
  `worktree_pattern`, `branch_pattern`, `check_command`, `check_minutes`,
  `cap`, `budgets.minutes`, `budgets.tokens` and, since S174 (*proposed*,
  owner decision 2026-10-29), `environment_lines`, `permissions` and
  `vendor_env`, and leaves every other key as it was (`name`, `path` and
  `adj` are set by `projects add`; `global_cap` and `daemon_idle_minutes` are
  file-level and belong to `ork settings set`, below). The daemon validates
  each value, refuses while the project has unfinished runs, and saves under
  its registry lock.
  A `ui_doc` must be a relative path inside the project (checked by name;
  symlinks are not resolved; it is stored cleaned; the file need not exist yet). A pattern must hold `{id}` or
  `{id_lower}`. Numbers must not be negative. An empty string or 0 clears
  a field (zero means unset). The command is design.md's `projects set`.
- S174 (*proposed*) changes the three list and map fields without retyping
  what stays. `environment_lines` is replaced as a whole by the lines given;
  an empty list clears it. A line must hold text and be one line without a NUL
  byte. `permissions` changes one role at a time: the role's entry is set to
  the preset `read-only` or to the list of patterns given, or removed so the
  default applies; a null entry also removes a role key that ork does not know
  when the file holds it (a hand-edited typo), but setting an unknown role is
  refused. A pattern must not be blank or hold a NUL byte or a
  newline; the pattern syntax per vendor stays unchecked. The daemon resolves
  each changed role with the engine's rules, so an allowlist for a read-only
  role (reviewer, gate, architect, UI pass, spec check), a preset other than
  `read-only` and an unknown role are refused. `vendor_env` changes one key at
  a time: a vendor name is `[a-z0-9_-]`, a key is a shell name
  (`[A-Za-z_][A-Za-z0-9_]*`), a value holds no NUL byte, and a vendor with no
  keys left is removed. A hand-edited file that holds values these checks
  refuse still loads: `Load` does not apply them.
- `ork settings set` (*proposed*, S174, owner decision 2026-10-29) writes the
  file-level `global_cap` and `daemon_idle_minutes`; 0 clears one. Unlike
  `projects set` it is not refused while runs are unfinished: the cap is read
  for every admission and the idle minutes each time the idle clock arms.
  Raising the cap starts queued runs that now fit; lowering it never stops a
  running one. `ork settings show` prints the stored and the effective values
  (4 and 10 minutes when unset).
- `vendor_env` is not a place for secrets. `projects.json` is written at mode
  0644, so other users of the machine can read it, and ork never reads vendor
  credentials. Use it for switches such as `VENDOR_CODEX_NETWORK`. ork's text
  output and error messages name the keys of `vendor_env` and never a value.
  `--json` output prints a project as the registry holds it, as `projects list
  --json` does, so it includes the values.
- `vendor_env` is shown as an example. The DataBall entry reflects the
  agents repo's note that Codex needs network access for `dotnet test`
  (`agents/setup/lib/workflow-vendor.sh`).

## run.json

ork writes `run.json` at mode 0600 (adj #106). It creates the runs root,
each run directory, and role temp directories (including spec check temp
directories) at mode 0700. Saving a run tightens its existing run directory
to 0700 and replaces an old `run.json` at 0600. Opening or listing runs
changes no modes. Existing runs roots and temp directories keep their modes.
A vendor-written role report keeps the vendor's mode; ork writes its own
fallback report at 0600 (S75, owner decision 2026-10-23).
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
`Owner decisions` section. Before run start, the paragraph survives daemon
restarts in the item's `answers.json` beside its check, tied to the updated
spec fingerprint. Run start transfers it to this record and removes that
file; a spec change drops the stored answers. The item's check directory is
removed when its run lands, or when its project is removed. Startup sweeps
identified directories older than 14 days whose item is done, retired or gone,
using the ordinary slicer list without `--all`. It also removes old directories
for unregistered projects and legacy random-named check directories
(`YYYYMMDD-HHMMSS-xxxx`) older than 14 days. Corrupt or empty `answers.json`
files are discarded and logged with their full path, so the check asks the
owner's questions again.

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
           "untracked": ["b.go"], "human_checks": [{"text": "...", "state": "ticked"}],
           "ui_round": 1, "renders": [{"name": "launch-80x24.txt", "size": 1210}]},
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
`awaiting-approval`, `finish`, `commit`, `land`, `committed`, `paused` and `stopped`;
`committed` and `stopped` are final. `queued` is not final: the run waits for
a slot under the caps and has no process and no writes. `round` counts implementer rounds and
`fails` counts gate FAILs. `reason` says why a slice is stopped or paused.
`pending` (kind `question`, `shape`, `lost`, `interrupted`, `budget` or `error`, with
the state and role to re-enter, the reason, and for a question the optional adj
`key`, the text and the answer) is set while a slice is paused.
The proposed optional `queued_questions[]` (S135) keeps sibling owner questions
as pending objects while `pending` stays single. Answering the current question
shows the next queued question before roles resume. Each role receives its own
answer from `questions[]`. Old records without the queue still load.
When the first pause is not a question, a retry shows the first queued
question instead of running a role; the optional `pending.retry` (S159,
*proposed*) holds the pause that was retried, with the same fields, and a
role named in it resumes with that context after the answer. Records without
`retry` still load; an older ork refuses one that has it, because
`Store.Open` uses strict keys.
Both `pending` and each entry in `questions[]` may carry `ask`, the structured
question object: `title`, `options:[{label,summary}]`, one-based `recommend`
and `reason` (adj #168). The raw question text is kept alongside it. Older
records without `ask` continue to use free text. An older ork refuses a
`run.json` with `ask` because `Store.Open` uses strict keys. `feedback`
names the file (kind
`checks`, `findings`, `architect`, `fail`, `owner` or `land`) that the next implementer round must act on.
`land_recheck` (omitted when false; *proposed*, S119) is set before the base
branch is merged into the slice branch and cleared by the first green re-check
after that (or when the landing is handed to the fix round); while it is set, a
retried landing merges the base if it is not in yet, then renders and
re-checks before it fast-forwards. `landed_at` (omitted until then; *proposed*,
S119) is when ork fast-forwarded the slice onto the base branch and ran
`slicer land`; the daemon sends it as `landed: true` on the run's snapshot row,
with `cleanup_failed` holding `reason` when the cleanup after the landing
failed (the cleanup key clears that reason).
`cleaned_at` (omitted until then; *proposed*, S95) is when the cleanup key
removed the run's worktree and branch. It is set for any committed run the key
cleaned, including one landed with `slicer land` (which leaves `landed_at`
empty), and the same update clears the landing's `reason`. A run is *cleaned*
when `cleaned_at` is set, or when ork landed it and left no `reason`; the
daemon sends that as `cleaned: true` on the run's snapshot row (*proposed*).
A committed, uncleaned run whose `worktree` path no longer exists also carries
`worktree_gone: true` on its row (*proposed*, S153): the cleanup key then skips
`git worktree remove` and the confirm omits it.
The version stays 1, as for `dismissed_at`: an older ork refuses a `run.json`
with `cleaned_at` because `Store.Open` uses strict keys.
`wait_started_at` (omitted when zero; S138) records entry into
`awaiting-approval` or `paused`. Metadata updates, including human-check marks,
keep it. Leaving either state clears it; entering another wait resets it to
that transition's timestamp. A run created in either wait starts at creation.
The version stays 1 under the same additive-field policy as `landed_at` and
`cleaned_at`. Older records without the field still load; older ork versions
that do not know the field refuse records containing it through strict keys.
`stop_after` (*proposed*, S177) records the per-run stop point: `land` or
`commit`. An absent field means `land`. With `commit`, the final state is
`committed` with `landed_at` unset and the worktree and branch retained.
The version stays 1 under the additive-field policy; older records still
load, and older ork versions refuse the new field through strict keys.
The daemon echoes it on the engine-run snapshot row so approval wording
matches the run's choice.
`land_fixes` (omitted when 0; *proposed*, S119) counts the fix rounds a blocked
landing has had; the `land` state allows one (a merge conflict, or a red
re-check after the base branch was merged in), and a second block stops the run
with `land blocked: ...`. A run that landed is `committed` with its worktree and
branch removed; if the cleanup failed, `reason` says so.
The `architect` state runs only when the lineup includes that role (adj #151),
after accepted reviewer findings and before the gate (adj #123). Its whole
report becomes `architect` feedback for a fix round (adj #150).
`findings` (omitted when empty; *proposed*, S120, adj #163) holds the review
findings that are not fixes: `kind` `owner` (an owner question that does not
block, with the structured `ask` and an optional `key`) or `later` (an offer to
file a slice). `title` is the finding's one-line title and `text` its body.
`filed` and `filed_at` are set once the owner confirmed and ork ran
`slicer add` and `slicer promote` for it; `filed` is the new slice id. A finding
raised again under the same title is kept once.
Proposed (S137): optional `scores` carries a role's proposed `importance`,
`urgency` and `effort` (each 1 to 3). ork applies a complete proposal with
`slicer set` after filing; absent scores leave the slice unscored.
An answered non-blocking owner finding has `answer` and `answered_at`.
Recording it does not pause or advance the run. A key also files the answer
in adj when the project has adj.
The record version stays 1. Records without these fields still load; an older
ork that does not know `scores`, `answer` or `answered_at` refuses a
`run.json` containing them because `Store.Open` uses strict keys.
Landed, dismissed and superseded runs retain their findings for the run pane,
but those findings no longer contribute to Needs you or status counts.
`card` (omitted until the gate first passes; *proposed*, S118) is the approval
card, built each time the gate passes: the gate's `verdict` and test counts
(`passed`, `failed`, from its `Tests:` line), `diff_stat` (the output of `git
diff --stat HEAD` in the worktree, which ork has not committed yet) and
`untracked` (new files that stat does not count), and `human_checks`, the
`[HUMAN CHECK]` bullets of the slice read with `slicer show`. Each check's
`state` is empty (open), `ticked` or `later`; the owner sets it with
`run.check`. A check with the same text keeps its mark when the card is built
again after a fix round. `checks_error` says why the checks could not be read.
`ui_round`, `renders` and `renders_dropped` (omitted when empty; *proposed*,
S121, adj #165) list the views the UI pass wrote: `ui_round` is the round of
the latest UI pass that ended well (omitted when none ran), `renders` the
`.txt` files it left in `tmp/ui-<round>/renders/` as `name` and `size` (the
bytes of the plain text that `run.render` returns), in this order: each view's
`-80x24`, `-120x40` and `-ascii` files, the views by name, then other files by
name; `renders_dropped` counts the files past the limit of 24. The card holds
no render text, so neither `run.json` nor a snapshot does: the text stays in
the file and is read on demand (`run.render` in `design.md`). A render is
plain text of at most 24 KiB; ork drops terminal escape sequences and other
control characters, and cuts a longer one at the last line end within 24 KiB
(at the 24 KiB mark when there is none), with a last line `(cut: over 24
KiB)`. A renders directory that is a link is not read. The files stay in the
run directory. `renders_error` (omitted when empty; *proposed*, S147) says why
the renders directory, or a render file it lists, exists and could not be read
(for example `permission denied`), like `checks_error` for the checks. A missing
directory is no error: the card then has `ui_round` and no `renders`.
The record version stays 1; an older ork refuses a `run.json` with
`renders_error` because `Store.Open` uses strict keys.
`fixes` (omitted when empty; *proposed*, S118) keeps each note the owner sent
to a fix round from the card: the `round` that acts on it, the `note` and the
time. The note is also the file `owner-fix-N.md` in the run directory, which is
the `owner` feedback of that round. An older ork refuses a `run.json` with
`card` or `fixes` because `Store.Open` uses strict keys.
A role with no `ended_at` is running; a restart marks it `lost`. A role the
watchdog killed after 30 minutes without output has `exit` `hung` (S182,
*proposed*) and an `ended_at`; its pause is kind `lost`. `commit` is
`{"sha","title","at"}` once ork has committed. The fields `orchestrator_step`,
`fails`, `reason`, `pending`, `feedback`, `created_at`, `updated_at`, the
role's `model`, `effort` and `denials`, a check's `log`, and a write's
`error` are *proposed* (S04).

A stopped run can be dismissed with `ork dismiss RUN` (S34, adj #117).
This sets `dismissed_at` to the dismissal time. It is omitted until then.
The snapshot keeps the run marked `dismissed: true`, hidden from lists,
Needs you and `ork status` text. Its findings are excluded from counts.
`ork status --json` includes the dismissed run. Dismissal keeps the record
and its files; finished-run retention is unchanged.
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
`engine.pause-resume`). `lost` also means a hung role that the watchdog killed
(S182, *proposed*); its `reason` names the role, round, limit and, when ork recorded one, the
killed process group. `ork stop RUN` stops the run. Retrying a question
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
The optional `wait_started_at` comes from the persisted wait-entry timestamp
and is omitted when zero. Awaiting-approval and paused Overview rows count
from it using the current clock. For older records without it, they use
`updated_at`, then the run's start when neither timestamp is present. Those
older records cannot distinguish a metadata update from the start of a wait.

The optional `branch` and `worktree` display fields are *proposed* and come
from the run record. An omitted `merged` means the merge state is unknown;
true means merged and false means unmerged. The daemon supplies it from the
HEAD-change check (`landing.merge-detect`, adj #128). It remembers the last HEAD per project. The first successful HEAD
read and each changed HEAD check every committed branch with
`git -C PATH merge-base --is-ancestor BRANCH HEAD`. Exit 0 means merged,
exit 1 means unmerged, and another error leaves the state unknown. When that
error is a missing branch (`git rev-parse --verify --quiet refs/heads/BRANCH`
exits 1), the run's worktree path no longer exists and its `commit.sha` is in
HEAD, the run is from before S95: the daemon sets its `cleaned_at` (S153). A
missing branch with a worktree still there, with no recorded commit, or with a
commit not in HEAD, leaves the state unknown. A pass does not write a merge
state for a run that was cleaned while git ran. An
unchanged HEAD adds no merge reads. While a pass runs, each run keeps the
value of the last pass, so a snapshot and the cleanup key never see "unknown"
because of a pass in flight; the pass writes its results back when it ends.
A cleaned run is not checked: its branch is gone, and it shows no merge state.

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
- **Zero means unset:** `global_cap`, `daemon_idle_minutes`, `cap` and
  `check_minutes` of 0 read as absent, and the default applies
  (`registry.zero-values`). `ork settings set` and `ork projects set` clear a
  value by writing 0, and ork does not write a zero value.
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
  cached merge results. Unknown and unmerged branches stay. A cleaned run
  (S95) counts as landed for this, whatever its merge state. Pruning removes
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

### Vendor memory (S169, *proposed*)

The vendors that are out, and the rounds that wait for a vendor slot, are
daemon memory only. Neither is in `run.json` or `projects.json`, and a daemon
restart clears both: nothing remembers a vendor as out across a restart, and
a run that resumes starts with no wait. A mark made by a rate-limit or
out-of-credits exit lasts 60 minutes or until `ork vendor back`; a mark the
owner sets with `ork vendor out` has no expiry and holds until `back`. The slot cap (`codex` 1, `claude` unlimited) is built in
and is not a registry field. The proposed snapshot fields `vendors_out[]`
(`vendor`, `reason`, `since`, and `until`, absent for an owner mark) and `engine_runs[].vendor_wait` carry
them to the clients.

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
