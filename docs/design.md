# Engine design

How ork runs a wave of slices. Owner decisions are cited by adj key.
Anything not backed by a decision is marked *proposed*. Vendor CLI behavior
marked *unverified* must be confirmed by the vendor spike before code
depends on it.

## Shape

```
ork (TUI)  ork status --json (CLI)
     \           /
      unix socket (JSON)
           |
       ork daemon  -- run records, registry (state.md)
       /    |    \
  slicer   adj   git        <- CLI calls only
           |
   role processes: claude -p / codex exec / grok -p   (one per role round)
```

- **The daemon** owns waves, slices and role processes. The TUI and the
  CLI are clients (`daemon.lifetime`).
- **Start.** Running `ork` starts the daemon if no live socket exists. A
  CLI command that needs the daemon does the same: it calls its start
  function once when nothing answers, then retries the socket for a few
  seconds. The daemon exits when no wave is running and no client has been
  connected for `daemon_idle_minutes` (10 by default; 0 means the default;
  `daemon.idle`, `daemon.start`). The timer stops while any client is
  connected and starts again, in full, when the last one leaves. An active
  engine run also holds off idle exit (S17). A run that failed to reconcile
  does not hold off idle exit or daemon stop. A background failure still
  counts as active while its state is active. A run waiting
  for approval or an answer may idle-exit; it is recovered on restart. The start runs `ork daemon run`
  from the same binary, detached in its own session with stdin and stdout on
  `/dev/null` and stderr appended to `daemon.log` beside its socket (mode
  0600 on creation), and does not wait: the command waits for the socket for
  five seconds (S13). A daemon that finds the lock held while nothing
  answers retries it for two seconds, because an idle exit unlinks its
  socket before it releases the lock (S13).
- **Stop and restart (S46).** `ork daemon stop` requests shutdown over the
  socket. It refuses while a role or engine job is active, unless `--force`
  is given. The daemon closes admission before cancelling and waiting for
  jobs. It writes the reply before closing the socket. The CLI waits for
  the socket to disappear. `ork daemon restart` then starts the current
  executable and waits for it to answer. Stopping an absent daemon succeeds.
  A socket that refuses connections also means absent; restart uses the normal
  start path, which clears the dead daemon's stale socket.
  *Proposed:* both commands use the normal exit codes and accept `--json`.
  A daemon that predates `daemon.stop` answers `unknown_method`. Both
  commands then exit 1 without starting anything. They print the pid from the
  pid file and `kill -TERM PID`, and warn that a role is running (an active
  relay with running roles, or an engine run not idle) or may be running
  (the status call failed). The hint's status call has a five-second timeout;
  on expiry it warns that a role may be running. ork sends no signal itself
  (decision 118). The owner runs the kill, then `ork daemon restart`.
  An unreadable or invalid
  pid file gives the pid file path instead of a pid. *Proposed:* the wording.
- **Build identity** (`daemon.build-id`, adj #102). The executable reads
  `vcs.revision` and `vcs.modified` from `runtime/debug.ReadBuildInfo`.
  Without a revision it uses the build-time `version` variable. *Proposed:*
  the wire spelling is `REVISION+modified=true|false`. Status includes
  `build_id` and `roles_running`; the latter conservatively includes active
  engine jobs and unreadable run state. *Proposed:* `daemon.info` returns only
  `build_id`, without loading the registry or reading projects. Commands,
  TUI startup, stop/restart and doctor use this cheap call for build checks.
  Doctor also reads status for the idle self-host restart notice; a failed
  status read omits that notice and does not fail doctor.
  `ork status` makes one full status read. Commands warn on stderr when the
  daemon build differs, including an older daemon with no build ID. The
  warning names `ork daemon restart`. An `unknown_method` reply from an older
  daemon means build `unknown`, which is stale. Doctor probes an existing
  daemon without starting one and reports a mismatch as a warning. A mismatch
  does not change the command's exit code. Revision plus modified state
  cannot distinguish two builds of different edits at the same revision.
- **Socket.** `$XDG_STATE_HOME/ork/ork.sock`, else
  `~/.local/state/ork/ork.sock`. The directory is created with mode 0700 and
  the socket is mode 0600.
- **Single instance.** The daemon holds an exclusive lock on `ork.pid`, beside
  the socket, and writes its pid there. A second daemon cannot take the lock
  and exits with an error that names the running pid. A daemon that takes the
  lock removes a socket left by a dead one (a stale socket) and replaces it,
  but only after a short dial shows that nothing answers on it; a socket that
  answers, for example under a deleted pidfile, also gives the refusal. It
  refuses to remove a file at the socket path that is not a socket. The pid is
  written before the file is shortened, so it is never read empty. The socket
  path is limited to 103 bytes (macOS), and the directory is set to mode 0700
  even if it existed. The pidfile stays after exit, because the lock, not the
  pid in it, says whether a daemon lives.
- **Protocol.** One JSON message per line (`internal/ipc`). A client sends
  `{"id","method","params"}` and the daemon answers `{"id","result"}` or
  `{"id","error":{"code","message"}}`, with the id as the client sent it.
  Requests on one connection are answered in order. A slicer or adj tool error
  keeps its `code` and `message`; any other failure has code `internal`.
  Methods (`internal/daemon`; S03, and `subscribe` and `refresh` from S15):

  | Method | Params | Result |
  |---|---|---|
  | `status` | `{"project"?}` | `{"build_id","roles_running","projects":[{name, path, adj, needs_you, open?, held?, active_relays, engine_runs, errors?}]}` |
  | `daemon.info` (proposed wire) | none | `{"build_id"}`; no registry or project reads |
  | `daemon.stop` (proposed wire) | `{"force"?}` | `{"stopped":true}`; `daemon_busy` refuses active work without force |
  | `snapshot` | `{"project"}` | the project's `source.Snapshot` (`items`, `deps`, `goals`, `runs`, `open`, `decided`) from the cache, plus the daemon's `engine_runs` array and *proposed* `board_runs` (neither is a Part); each part is `{"value", "age_ms", "read_at"?, "stale"?, "error"?}`: the last good value, its age in ms (0 if never read), when it was read (absent if never), and the last read's error |
  | `item.note` (proposed wire, S171) | `{"project","item","text"}` | `{"item"}`; slicer note at the registered project root, then refresh Items; blank item or text is `bad_params`; slicer errors keep their code |
  | `item.show` (proposed wire) | `{"project","id"}` | `{"detail"}`; slicer show for the registered project |
  | `wave.start` (`run.launch-wire`; proposed shape) | `{"items":[RunParams...]}`; each item has `project`, `item`, `well_specified`, `split`, optional `lineup`, `budgets` and `expected` as in `run.start` | `{"wave","items":[{"item","run"?,"error"?}]}`; one wave id, one project, admission errors kept per item |
  | `wave.stop` (proposed wire) | `{"wave"}` | the same wave result; stops each unfinished member through `run.stop`, skips final members |
  | `wave.suggest` (proposed wire) | `{"project"}` | slicer's `plan-parallel` result for the registered project, limited to the fields `ParallelPlan` reads; slicer runs on each call, not from the cache |
  | `roles.matrix` (proposed wire, S168) | `{"project"}` | `{"markdown","rows":[{"id","role","agent","use"}],"rules":[{"id","text"}]}` from `slicer relay-roles show --json`: slicer's own text, and for each row what ork does with it (`use`: `used as claude\|codex`, `UI pass row, ...`, `Spec check row, ...`, `skipped: no adapter for AGENT`, `not used: the gate runs the reviewer's lineup`, `not run by ork`, `not used: ork has no role ROLE`); read-only, no slicer write; slicer's error keeps its code and message; slicer runs on each call |
  | `wave.overlap` (proposed wire) | `{"project","items":[ID...]}` | `{"overlap":[{"a","b","shared"?}]}` from `slicer deps --overlap`; `shared` is absent when a pair shares no path; fewer than two items is `bad_params`; slicer runs on each call |
  | `subscribe` | `{"focus":[names]}` | none; the connection gets notices (below) and the named projects become visible |
  | `refresh` | `{"project"}` | none; reads every part of the project now and returns once they are read |
  | `projects.list` | none | `{"projects":[...]}`; an empty registry is valid |
  | `projects.add` | `{"path","name"?}` | the added project; the first add creates the registry |
  | `projects.rm` | `{"name"}` | none; *proposed* `project_busy` refuses unfinished runs |
  | `projects.set` (*proposed*, S161) | `{"name", ...}` plus at least one of `push`, `ui_doc`, `worktree_pattern`, `branch_pattern`, `check_command`, `check_minutes`, `cap`, `budget_minutes`, `budget_tokens` and, from S174, `environment_lines` (a list), `permissions` (`{ROLE: "read-only" or [patterns] or null}`) and `vendor_env` (`{VENDOR: {KEY: "value" or null}}`) | the project as saved; an omitted field keeps its value, `""` or 0 clears one; `null` removes one role or one vendor key, and the other roles and keys stay; `bad_params` for an unknown field, no field or an invalid value, `not_found`, `project_busy` while a run is unfinished |
  | `settings.get` (*proposed*, S174) | none | `{"global_cap","daemon_idle_minutes","effective_global_cap","effective_daemon_idle_minutes"}`; the first two are the stored values (0 is unset) |
  | `settings.set` (*proposed*, S174) | `{"global_cap"?,"daemon_idle_minutes"?}`, at least one | the same shape as `settings.get`; 0 clears one; `bad_params` for no field or a negative number; not refused while runs are unfinished; a raised `global_cap` starts the queued runs that now fit |

  Error codes the daemon adds: `unknown_method`, `bad_params`, `not_found`,
  `duplicate_name`, `invalid_path`, `registry`, *proposed* `project_busy`, *proposed*
  `refused` (an owner action the engine refused).
  Registry writes happen in the daemon, one at a time. The TUI reads through
  the daemon with `snapshot` from S13. Before the daemon it called the
  adapters directly (`dash.pre-daemon`).

  **Snapshot cache (S15, `dash.cache`, `dash.refresh`, `boundary.git-head`).**
  The daemon holds one in-memory value per project and part, shared by all
  clients, and reads each part on its schedule: items, deps, runs and open records
  every 5 s for a visible project and every 30 s for the others; goals and
  decided records every 5 min, on a git HEAD change, when a project is opened
  and on `refresh`. A git HEAD change is `git -C PATH rev-parse HEAD` and
  nothing else, read on the same 5 s / 30 s tick as items, deps, runs and open
  records. The runs part is read every 1 s instead while the project is
  visible and its last good read holds a run that is not stale (*proposed*,
  S109); when that read shows no such run it returns to 5 s, and a project that
  is not visible stays at 30 s. The interval belongs to each project, so the
  Running entry, which makes every project visible, reads the runs of each
  project with a live run every 1 s and no other part faster (*proposed*,
  S110); the 4-process limit holds however many projects that is. Only the runs part speeds up; a change in it
  reaches the clients through the usual notice. Each read is one CLI call. A read that is already running is
  shared, and at most 4 CLI processes run at once across all projects. A
  read that comes due, or that a HEAD change asks for, while another runs for
  its part starts when that one ends, so a part never has two reads at once. A
  failed read keeps the last good value, with its error and the time it was
  read; a `corrupt`, `locked` or timeout failure is tried once more at once.
  The cache keeps no files and watches none.

  **Notices (S15).** A connection that called `subscribe` gets one line per
  change, without an id: `{"method":"snapshot.changed","params":{"project",
  "parts":["items"],"at"}}`. It is sent when a part's value or error state
  changes, and never when a read gives the same data again. Read notices with
  the client's `Next`, on a connection of their own: a request on the same
  connection is answered in line with the notices. A project removed from the
  registry stops being read. The TUI keeps its notice connection in
  `daemon.Feed`: a change of the project in view dials a new connection that
  subscribes to that project, because a connection that waits in `Next`
  cannot take a request (S13).
- **Restart.** On start, the daemon reconciles every non-final run record
  against live process groups and the slicer ledger. A role whose process is
  gone and that left no report is marked `lost`, and its slice waits for the
  owner. Runs with an unregistered project or a reconciliation error are
  skipped and logged to stderr (daemon.log); other runs remain available.
  Run-list errors are also logged, and readable runs remain available during
  recovery and status requests.

## Wave lifecycle

1. **Suggest.** For one project, ork shows `slicer plan-parallel` (or
   `slicer next --batch`) as a proposed wave (`wave.selection`). The owner
   adds or removes items, and ork warns when two items' `Files` overlap.
2. **Lineup.** For each item, ork runs `slicer relay-roles recommend ID
   --well-specified ... --split ...`. The primary row per role becomes the
   lineup, and the alternatives, in order, become that role's fallback
   chain (`lineup.source`). The owner can edit both.
   An edit at launch applies to that run only (`lineup.override-scope`).
   It may set any vendor, model and effort per role; ork refuses a vendor
   it has no adapter for (`lineup.launch-edit`). The edits travel as the
   full lineup per role plus budgets, and the plan echoes them
   (`run.launch-wire`). The Waves editor and Roadmap launch confirmation
   share the same run-only editor (`e`). Neither writes the matrix or registry.
   The daemon accepts implementer, reviewer and architect chains on
   `run.plan`, `run.start` and `wave.start`. An empty architect chain drops
   the architect; an omitted override keeps the recommendation. Adding an
   architect resolves its read-only permissions. A matched architect row
   with no ork adapter refuses planning, even with an override. Each entry
   needs a model, effort and a runnable vendor (`claude` or `codex`). The gate copies the reviewer's chain; a gate
   override is refused. Without an override, the plan keeps every row with
   an adapter in matrix order: matrix agent `Claude Code` maps to `claude`, and
   `Codex` maps to `codex` (`engine.adapter-key`, decision 155).
3. **Plan.** For each item, ork shows the worktree path, the branch, the
   check command, the budgets, and whether it runs an architect, a split or
   slivers. Then it shows the wave's place in the global queue.
4. **Check.** ork first reads `slicer deps ID --json --lean` for each
   selected item. Any `blocked_by` entry refuses the whole call before a
   spec-check role runs, naming the item and its blockers. Each blocker shows
   its id and quoted title from `slicer show ID --json --lean`, read once per
   distinct blocker per call. Failed title reads fall back to the id alone.
   The refusal ends with a next step: land the blockers first, or untick the
   blocked items when a blocker is selected in this wave. A blocker selected
   in the same wave is still not done and is marked `in this wave`. A failed
   dependency read also refuses the call. The daemon repeats this read at
   `wave.start`, before admitting any member or writing a run record. This
   dependency check applies to wave start and the spec check; `run.start`
   does not repeat the dependency read.
   At launch (`l` in Waves or Roadmap, `ork wave start`, or `ork run`), before
   the owner approves, ork runs one spec check over the selected items (adj 164,
   `roles.spec-check`; *proposed* until the Wave lifecycle is reviewed). It
   comes after the lineup and the plan because the owner ticks and plans items
   first. The check is one read-only pass of the spec-check role over the
   whole batch: for each slice it answers ready, or asks the
   owner structured questions (S111 shape, with an optional decision key). The
   owner answers them in one sitting. ork appends an `Owner decisions (date)`
   paragraph to the slice's Implement section with `slicer edit --append`, and
   files a question that has a decision key in adj the way a run's answer is
   filed (`owner.questions`, S28). The paragraph is also stored in the item's
   run record and added to every role's brief, because the run's worktree does
   not have the main checkout's edit. Only ready items, or items whose
   questions are all answered, can be approved: the daemon refuses an item
   whose check, against its current spec, left questions open
   on both `wave.start` and `run.start`
   (`spec_questions`). A failed spec check blocks launch; there is no skip
   flag. The daemon keeps each verdict in memory, as an
   ephemeral job cache with a fingerprint of the slice's spec (a restart asks
   again for role verdicts; see boundaries.md), so a changed spec is checked
   again and an unchanged one is not. The clients send every ticked item each time; the
   daemon runs the role only for the stale ones. Owner answers are saved beside
   each item's check, tied to the updated spec fingerprint. They survive a
   daemon restart and are removed when the run record takes them at run start,
   or when the spec changes (owner decisions, S143, 2026-10-27).
5. **Approve.** The owner approves the wave. This is checkpoint 1.
6. **Run.** Items start as slots free up under the global cap (default 2)
   and the per-project cap (`wave.limits`, `engine.default-cap`). Several waves,
   each in one project, may run at once (`wave.scope`). An item over a cap
   waits as a queued run and starts, oldest first, as a slot frees.
   Beside the run caps there is a vendor cap (S169, *proposed*, owner decision
   2026-10-29): one `codex` role at a time over all runs of the daemon, and
   `claude` unlimited. It is built in; the registry has no field for it. It
   is enforced per role round, not per run (see Vendors, below). A run that
   waits for the Codex slot keeps its global-cap slot, because the wait lasts
   at most one role round. Spec-check rounds are not counted: they run before
   a run and take no vendor slot.
7. **Result.** Each item that reaches a passed gate waits for the owner's
   approval. This is checkpoint 2. Once approved, ork runs the finish step, then
   commits (adj #81), then lands the slice (`landing.in-ork`, adj #162; the
   **land** state below). The approval is also the confirm of the cleanup
   (`git.cleanup-key`, adj #152), and the approval card says so. An item that
   stops waits in "needs you", and the rest continue (`wave.failure`).

## Slice state machine

```
setup -> implement -> checks --red--> implement (fix)
                        | green
                        v
                     review --findings--> implement (fix round) -> checks
                     (reviewer; beside it the UI pass on a UI slice)
                        | clean, or green checks after reviewer fixes
                        v
         [architect -> implement (fix round) -> checks]  (lineup only; once)
                        | green, or no architect entry
                        v
                      gate --FAIL--> implement (fix round)      [3rd FAIL -> stopped]
                        | PASS
                        v
                awaiting-approval --approve--> finish -> commit -> land -> committed
                        | fix (owner's note)      | reject        | blocked: one fix round,
                        v                         v               v then back to implement
                implement (fix round) -> checks -> gate      stopped  (a second block: stopped)
                        (back to awaiting-approval on PASS)
```

Any state can go to `stopped` on a stop rule (below), or to `paused` on a
budget limit or an owner question. The owner can resume or stop a paused
slice.

- **setup:**
  - `git -C P worktree add <worktree_pattern> -b <branch_pattern>`
    (`git.worktrees`);
  - `.worktrees/` is added to `info/exclude` if it is missing;
  - `slicer relay-run begin ID --worktree W` (`ledger.write`);
  - the implementer runs `slicer start` (the contract says the implementer
    starts the item).
- **implement:** one implementer, or a lead and parts in a split
  (`slice.options`). Slivers run as their own items, under the cap.
- **checks:** ork runs the project check command and `slicer check --json`
  in the worktree (`checks.deterministic`). A red result goes back to the
  implementer, with the failing output in the brief, without spending a
  review. The check command comes from `slicer config` once slicer has the
  key; until then it comes from the registry (`project.check-command`).
- **review:** the reviewer runs read-only, with the fingerprint applied
  (`role.readonly-fingerprint`). The architect runs after accepted findings
  have been addressed and before the gate (`slice.architect-order`, adj #123),
  exactly when the lineup has an architect entry. ork does not read the slice
  size (`slice.architect-size`, adj #151).
  - **UI pass** (`roles.ui-pass`, adj #165). When the lineup has a `ui` entry
    and the slice's Files section names a UI path, the UI pass starts with
    the reviewer, at the same time, and neither waits for the other. A UI
    path is one in backticks under `internal/tui` (its golden views too) or
    `internal/cli`, `docs/dashboard.md`, or a `ui.*` adj key (AGENTS.md; matrix
    rule u10). On any other slice no UI round runs. The UI pass is read-only
    with the fingerprint applied, gives no verdict, and reports sections
    (A) to (D) like the reviewer. It runs once, with the first review. The
    findings of the two reports go to one fix round. A sibling owner
    question is queued in `queued_questions[]` and asked after the first
    answer; other sibling pauses are still dropped. A stop overrides a pause
    and cancels the running sibling, recorded with exit `cancelled`. After a
    pause, a retry runs again only the role whose report does not stand.
    When the first pause is not a question and a sibling question is queued,
    a retry asks that question first and runs no role. The retried pause
    rides on the question as `pending.retry`; after the answer the retried
    role runs with its own context (the report shape or lost process section;
    a question or shape pause also resumes the role's session) and the sibling
    role with its answer (S159, *proposed*).
    Renders (*proposed*, S121; adj #165): the
    UI pass writes each changed view as plain text files at 80x24, 120x40 and in
    ASCII mode, in `tmp/ui-N/renders/` of the run directory, using the
    project's own tooling. ork has no render code. When the gate passes, the
    approval card keeps the files of the latest UI pass that ended well (see
    Approval card).
- **architect:** one read-only review with the fingerprint applied. The report
  needs only the shared `## Owner needed` section. The whole report goes to an
  implementer fix round, even when it has no findings; ork adds no parser
  (`slice.architect-report`, adj #150). Green checks after that fix lead to the
  gate. The architect is not repeated after a gate FAIL.
- **fix round:** the implementer is resumed with the confirmed findings. A
  small fix round is reviewed and gated in one pass (contract v7).
- **finding kinds** (adj #163, `review.finding-kinds`): the reviewer, the UI
  pass and the gate tag each finding `[fix]`, `[owner]`, `[owner blocker]` or
  `[later]`. A finding is a list item or paragraph that opens with the tag;
  lines indented under it belong to it. A finding without a tag is a fix, so a
  report without tags behaves as before. `Triage` and `TriageGate`
  (`internal/engine/kinds.go`) sort a report:
  - *fix*: the only findings the fix round reads. A tagged report goes to the
    fix round as its fix findings, without the others; an untagged one goes
    whole.
  - *owner*: the finding is an owner question in the shape of S111: a title,
    `Options:` with 2 to 4 numbered labels, `Recommend: N - reason`, and an
    optional `Decision key:`. ork keeps it in `findings[]` of the run record
    and shows it in the pane. It does not block. `[owner blocker]` makes ork
    pause the round with a `question` pending, as an `## Owner needed`
    section does (`questions[]`, the answer reaches the same role, and
    `reviewDone` runs that role again). A malformed owner or later finding is
    a report shape error. Proposed (S137): every blocker is asked, with
    remaining questions persisted in `queued_questions`; the role reruns
    after all answers. Repeating an answered blocker pauses with an error
    instead of asking again. The resumed role receives all its recorded answers.
    A non-blocking finding can be answered from the pane without pausing or
    advancing the run; its answer and timestamp stay on the finding, and a
    decision key also records the answer in adj when the project has adj.
  - *later*: ork keeps it in `findings[]` as an offer. Nothing is filed until
    the owner confirms in the pane (`run.file`). Then ork itself runs
    `slicer add TITLE --discovered-from ITEM --key RUN-later-N --render` in
    the project's main checkout, and `slicer promote ID --file later-N.md`
    with a Why section made from the finding, both as `writes[]` entries. The
    key makes a retry return the slice the first call filed; when that slice
    already has its spec, ork skips `promote`. slicer owns the
    record (`docs/boundaries.md`). Proposed (S137): a role supplies
    `Importance: N`, `Urgency: N` and `Effort: N` on indented lines, each
    from 1 to 3. Non-numeric lines with these labels remain prose and do not
    count as proposed scores. ork applies a complete proposal with `slicer set`
    after promotion. A missing proposal leaves the slice unscored. Failed scoring
    keeps the finding retryable.
  The same finding raised again in a later round under the same title is kept
  once. A gate FAIL sends back the gate's report without its owner and later
  findings.
- **gate:** the first line of the report must be exactly `VERDICT: PASS` or
  `VERDICT: FAIL`. On S and M items the gate is the resumed reviewer
  ("lite"). A FAIL is recorded with `slicer note ID --text "Review FAIL:
  ..."` (contract v6).
- **commit:** ork commits in the worktree with the title from the slice's
  Git section. An empty or missing Git section falls back to `ID: slice title`
  from `slicer show`; it does not block launch (S139, owner decision 2026-10-26).
  There is no AI attribution, and roles never commit
  (`git.commit`). The finish step precedes this commit (adj #81).
  The per-run `stop_after` input (*proposed*, S177) is `land` by default or
  `commit`. With `commit`, this step ends in `committed`, clears `pending`,
  leaves `landed_at` unset and keeps the worktree and branch. The ledger has
  already ended with outcome `pass`. No landing, push or cleanup follows;
  the owner uses the Ready row's slicer land commands. The finish step still
  follows slicer's `implement_finish`; there is no separate handoff stop point.
- **Finish step:** run per the project's `implement_finish` setting (`done`
  or `handoff`), after PASS and owner approval, before the commit.
- **land** (S119, adj #162 `landing.in-ork`, which supersedes `landing.v1`):
  the step after the commit when `stop_after` is `land` or absent, all through
  git and slicer, each step logged in
  `writes[]`. One landing per project runs at a time: the engines of a project
  share a lock (`engine.Landings`), and the next landing waits, then reads the
  base branch the first one moved. The base branch is the branch the main checkout is on. Each step
  reads git first, so a retry after a pause continues where the landing
  stopped.
  1. If the base branch moved (its tip is not in the slice branch), `git -C W
     merge -m "Merge BASE into BRANCH" BASE` in the worktree, then `slicer
     render`, a commit of what render rewrote, and a re-run of the project
     check command and `slicer check`. With no move, none of this runs. The
     run record's `land_recheck` is set before the merge and cleared by a
     green re-check (or by the hand-off to the fix round). A retry with it set
     merges the base if it is not in yet (a crash between marker and merge),
     then renders and re-checks, and never fast-forwards a tree that was not
     checked.
  2. `git -C P merge --ff-only BRANCH` in the main checkout. A failure (for
     example an untracked file in the way) pauses the slice with the git
     message; `t` retries.
  3. `slicer land ID --branch BRANCH --note "Landed BRANCH"` in the main
     checkout (it needs the branch merged, so it follows the fast-forward). In
     `handoff` mode it marks the item done there, and ork commits the `.slicer`
     change as `ID done` (`git commit -- .slicer`, so files the owner staged
     stay out of it); in `done` mode there is nothing to commit.
  4. `git -C P push` when the project's `push` setting is on (off by default;
     ork writes it only when the owner runs
     `ork projects set NAME --push on|off`). A failed push pauses the slice; the branch is on the
     base branch already, and a retry pushes again.
  5. Cleanup, as the owner's approval confirmed it: `git -C P worktree remove
     W`, then `git -C P branch -d BRANCH`. Never `--force` or `-D`. A cleanup
     that fails leaves the run `committed` with the reason; the merged-branch
     cleanup key (`git.cleanup-key`) then works as before.
  A merge conflict, or a red re-check after the merge (for example goldens that
  another slice changed on the base branch), is not fixed by ork and is not
  aborted by ork. The slice goes to one implementer fix round
  (`feedback.kind` `land`) with the conflict or the red output as the brief,
  then the checks, the gate and a new approval. `land_fixes` counts it. If the
  second landing is blocked the same way, the slice stops with `land blocked:
  merge conflict` or `land blocked: checks red after merging BASE`, in Needs
  you, with the worktree and the branch kept.

### Engine (S04)

`internal/engine` runs one slice through these states against a `Runner`
(`plan.s04-cut`). The real Claude adapter is S16 and the owner surface is
S17; S04 is tested with a fake runner, fake `slicer`, `adj` and `git`
(`internal/clitest`) and a fake clock.

- **Runner.** One call runs one role round to its end. The request carries
  role, round, vendor, model, effort, brief, worktree, report path, the
  allowlist or the read-only flag, the session to resume, and an `OnStart`
  callback that gives the process group id. The Claude adapter
  calls `Spec.OnStart` after a successful `cmd.Start` and before `cmd.Wait`
  (adj #85, `engine.adapter-start`); S17 glue forwards `Request.OnStart`.
  The optional vendor-neutral `OnEvent func()` callback counts each stream
  event. Claude and Codex adapters forward their event callbacks to it.
  The result carries the exit
  class (`ok`, `rate_limited`, `auth`, `error`, `unclassified`), the session, the final
  text, tokens, cost and the permission denials. A runner error counts as an
  exit of class `error`. Auth and unclassified exits stop the slice with
  the reason even when a next lineup entry exists.
- **Role activity (S07).** The engine keeps a ring of ten 30-second buckets
  for each current role attempt, with its own mutex because `Run` holds the
  engine mutex. Every role attempt starts empty. The daemon reads the series
  against its own clock at snapshot time, oldest first, on wall-clock bucket
  boundaries. The proposed optional `engine_runs[].activity` display field
  carries ten counts summed across active roles and is omitted otherwise. Live
  token counts are summed too; non-empty live text is joined with a newline
  between roles, in role-name order. Ending one parallel role leaves the
  sibling's activity intact. Restarts and
  source-less ledger runs have no series. Activity stays in daemon memory;
  it is not in `run.json` and adds no state-file field.
- **States.** `queued` (waits for a slot; `Dequeue` moves it to `setup`), `setup`, `implement`, `checks`, `review`, `architect`, `gate`,
  `awaiting-approval`, `finish`, `commit`, `land`, `committed`, plus the two rests
  `paused` and `stopped`. The owner moves a slice with `Approve`, `Reject`,
  `Fix`, `Answer`, `Retry` and `Stop`.
- **Setup order.** Add `.worktrees/` to `info/exclude` if missing, then
  `git -C P worktree add <worktree> -b <branch>` with the registry's
  patterns (default `.worktrees/{id}` and `{id}`), then `slicer relay-run
  begin`, then the orchestrator step. An interrupted setup can reuse an existing worktree
  when its checkout root, branch and common Git repository match the run.
  All slicer calls use the worktree as
  `--root`. The ledger is advisory: a failed `relay-run` call is in
  `writes[]` and the relay goes on.
- **Ledger.** The orchestrator step is ork's own (agent `ork`, no vendor or
  model). Each role round is a step with `--parent` on it, labelled `ID
  role`, `ID implementer fix round N` or `ID implementer finish step`, and
  its phase is set to `done` or `fail` when the round ends. A stop ends the
  run with outcome `stopped` and `--close-steps fail`; the finish step ends
  it with `pass` and `--close-steps done`.
- **Checks.** The project's check command runs with `sh -c` in the
  worktree, then `slicer check --json`. Either red sends the implementer
  back, resumed, with the output (last 30 KB) in the brief, and spends no
  review. The output is saved as `checks-<round>.log`.
- **Review and gate.** The reviewer runs once, fresh and read-only. Findings
  start a fix round. After green checks, or a clean review, the optional
  architect runs before the gate. Its whole report starts another fix round,
  followed by checks. The gate is the resumed reviewer (role `gate`), which
  reviews and gates the fix in one pass. Without an architect entry, a clean
  review goes straight to the gate. The gate brief includes the latest
  architect report and the latest UI pass report even when the gate resumes a
  session. A gate FAIL is recorded
  with `slicer note ID --text "Review FAIL: ..." --render`, sends the
  implementer back, and the third FAIL stops
  the slice. *Proposed:* review findings are the text of sections (A), (B)
  and (D) of the reviewer's report when they are not "nothing"; (C) holds
  notes for later, and starts a fix round only through a finding tagged
  `[fix]`. Only fix findings count (finding kinds above). A report without
  sections (A) and (B) is a shape error.
- **Rounds.** `round` counts implementer rounds and names the report
  files (`implementer-2.md`). A repeat of the same role and round uses a
  new path (`implementer-2-attempt-2.md`), preserving the earlier report.
  *Proposed:* a slice stops after 8 implementer
  rounds (`MaxRounds`), so a check that never goes green cannot loop.
  `MaxRounds` stays: it limits red loops, not spend (`run.budget-resume`).
- **Resume.** A fix round resumes the implementer's latest session and the
  gate resumes the latest gate or reviewer session from the same vendor and
  model. Daemon runs copy the reviewer's lineup into the gate; a copy counts
  as following the reviewer, as does an empty gate lineup. With no gate
  records, the gate starts on the reviewer's current row. Its own records
  take precedence for later attempts. Without a matching session the role
  starts fresh and its brief carries the earlier reports
  (`role.resume`).
- **Report checks.** `CheckReport` refuses a gate report whose first line is
  not exactly `VERDICT: PASS` or `VERDICT: FAIL`, an implementer or gate
  report with no `Tests: N passed, M failed` line (the finish step needs
  none), and any report with no `## Owner needed` section. All problems are
  listed. A gate PASS with a nonzero failed-test count is also refused.
  The slice pauses (kind `shape`) with them; `Retry` resumes the
  role and tells it what was wrong.
- **Decision keys.** An Owner needed section may include one line
  `Decision key: area.topic` anywhere in the section. The key must be lowercase
  and dotted; a malformed or repeated key is a `role.report-shape` error.
  The full section and its optional key are stored with the question. A key
  enables optional adj filing of the owner's answer in the TUI.
- **Structured questions** (`ui.structured-questions`, adj #168). Roles write
  this block under `## Owner needed`, with 2 to 4 options, distinct labels,
  consecutive numbers starting at 1, and a recommendation naming an option:

  ```text
  Question: ONE LINE TITLE
  Options:
  1. LABEL: summary
  2. LABEL: summary
  Recommend: 1 - one-line reasoning
  Decision key: area.topic
  ```

  Free context lines may precede `Question:`. They stay in the raw question
  text and the pane shows them above the options. From `Question:` onward,
  the block keeps its strict shape: extra context lines inside or after it
  are a shape error. The optional decision key line remains allowed anywhere
  in the section.

  The decision key is optional. Title, labels, summaries and reasoning must
  be non-empty and each fits on one line. A malformed structured block is a
  report shape error. Legacy free text still pauses for an answer. The raw
  section stays in `Question`; the parsed `Ask` accompanies it in pending
  state and question history. Adj uses the structured title when filing.
- **Owner questions.** A non-empty `## Owner needed` pauses the slice (kind
  `question`) and is recorded in `questions[]`. `Answer` resumes the same
  role with the question and the answer. The pending answer stays in the
  run record until the resumed role ends, so recovery can put it in the
  next brief after a crash. Optional answer filing is S28; when it
  files, `adj ask` runs in the project's main checkout (`Engine.AdjRoot`),
  never in a worktree.
- **Stops.** `rate_limited` and `error` exits fall back while a next row
  remains (adj 156); otherwise they stop with the reason. Auth and
  unclassified exits, unsuccessful rounds with permission denials, and a
  read-only role that changed the tree (`git status --porcelain` before
  and after) stop the slice with the reason. A successful round with
  permission denials proceeds. The role record
  keeps the denied tool names and each denied command or file path, capped at
  200 characters (`engine.denied-command`); status and the TUI show them as
  warnings (`engine.denials`, `ui.denials-display`). An unsuccessful round with
  denials stops with the denied tools and commands in its reason. A failed commit stops the
  slice (adj #81). Other tool failures the engine
  cannot go on from (a check run, `git status`) pause it (kind `error`); `Retry` runs the
  state again.
- **Restart.** `Recover` loads every unfinished run. A role with no end
  whose process group is alive is killed; only after a successful kill
  (or when the group is gone) is that role marked `lost`,
  its ledger step is marked `fail`, and the slice pauses (kind `lost`). A
  run between roles pauses (kind `interrupted`). Awaiting-approval runs
  keep their state even if a stale role has no end. A failed kill leaves
  the role running in the record and blocks Retry until reconciliation
  succeeds. `Retry` runs the role again
  (resumed if it had a session) and `Stop` ends the slice. Only process
  groups named by the run records of unfinished runs are killed, never group
  0, 1 or ork's own. *Residual risk:* a recycled process group id is not told
  from ork's; the record has no start time to compare.
- **Approval card (*proposed*, S118).** When the gate passes, the engine
  builds the card in the run record (`state.md`): the verdict, the gate's test
  counts, the diff stat against `HEAD`, and the slice's `[HUMAN CHECK]` items.
  `MarkCheck(n, state)` ticks a check, marks it `later`, or reopens it.
  `Approve` refuses while any check is open, then records the marks as one
  `slicer note` on the slice (`Human checks at approval (ID):` and one
  `- ticked: TEXT` or `- later: TEXT` line per check) and goes on to the finish
  step. This note replaces the human-check adj records. A note that fails
  refuses the approval, so no mark is lost; the owner approves again. A
  second approval, after a landing fix round (S119), does not write the note
  again (S148); nor does one after the first approval's record could not be
  saved once the note was written (S141). When the card could not read the
  slice's human checks (`checks_error`), `Approve` does not wait on marks: it
  writes a note that says they could not be read (`Human checks at approval
  (ID): the slice's human checks could not be read, so none were marked:
  WHY`) and goes on (S141, owner decision 2026-10-26).
  An owner action the engine refuses before it changes anything (a wrong
  state, an open check, an empty note, the round limit) returns a refusal
  (`engine.IsRefusal`). The daemon returns its text to the caller and does not
  keep it as the run's `error`, so the run row shows no `error:` marker for it
  (S141); a failure that is not a refusal still does. *Proposed (S141):* the
  daemon sends a refusal with its own wire error code `refused` (a failure keeps
  `internal`). `ork` prints the text alone (`ork: TEXT`, and `{"error":
  {"code":"refused","message":"TEXT"}}` with `--json`), and the TUI's status line
  reads `approve: TEXT`, without the code.
  `Fix(note)` is the owner's third action beside `Approve` and `Reject`: it
  starts an implementer fix round with the note as `owner` feedback, resumed
  as any fix round is, then the checks and the gate, with no review in
  between, and returns to `awaiting-approval` on PASS. A gate FAIL goes back to
  the implementer as usual. It counts as an implementer round, so it is
  refused (not stopped) at the round limit. The gate's brief carries the
  owner's note as `Owner fix request` in every gate round until a gate passes
  the change: a red check or a gate FAIL after the fix sends the slice to a
  later round, and the note goes with it (S141).
  The card also holds the UI pass's renders (*proposed*, S121): of the latest
  UI pass that ended well, the `.txt` files of its `tmp/ui-N/renders/`
  directory, each view's 80x24, 120x40 and ascii file in that order, the views
  by name, then other files by name. ork keeps only regular files (neither a
  linked file nor a linked directory is followed, and a file must be the one
  the listing named), at most 24 of them and 24 KiB of each, as plain text
  (escape sequences and other control characters dropped, so none can reach the
  owner's terminal). The card lists each render by name and size and holds no
  text; the text is read on demand (`run.render`, wire section). A renders
  directory, or a listed file, that exists and cannot be read is named in the
  card's `renders_error` (S147), so the owner does not read it as "wrote
  none". The directory is cleared when the UI pass starts, so a rerun keeps
  no file of an earlier attempt. After an owner fix round the card is built again with the
  same renders; its `ui_round` says which round they come from.
- **Commit and finish.** On `Approve`, the implementer is resumed for the
  finish step first: the closing note, then `slicer done` or `slicer handoff`
  per `slicer config implement_finish`. Then ork commits in the worktree with
  `git add -A` and `git commit -m "<title>"`, the title being the quoted text
  of the slice's Git section (or its first non-blank line without quotes).
  An empty or missing Git section uses `ID: slice title` from `slicer show`
  (S139). There is nothing else in the message (no attribution), so one
  commit carries the code and the tracking change. A rejected slice runs no
  finish step. A failed commit stops the slice in needs-you with git's error
  and does not undo the finish step or close its passed ledger run again
  (adj #81, `engine.finish-order`). Retrying a commit pause resumes at commit
  without repeating the finish step.

## Stop rules

From slicer's contract:
- the same slice fails its gate three times;
- the spec turns out to be ambiguous or wrong;
- an owner decision is needed;
- a permission denial on a slicer step.

Outside the finish step, ork follows decision 93 (`engine.denials`): a denial
stops only an unsuccessful round. A successful round with denials proceeds and
shows them as warnings. In the finish step, a denied `slicer note`, `done` or
`handoff` pauses for the owner. The implementer reports it under `## Owner needed`.
See `slicer ai relay` for the contract.

ork adds:
- an auth or unclassified role exit;
- an unsuccessful round with permission denials;
- a `rate_limited` or `error` exit with no next row (otherwise it falls back,
  adj 156);
- a read-only role that changed the tree;
- a lost process.

An orchestrator waiting for an owner answer records `--phase wait --waiting-on
owner`; awaiting approval records `--waiting-on approval`. A waiting step
never goes stale. Structured blocker questions are mirrored with `relay-run
ask`, and the owner's answer with `relay-run answer`. Legacy unstructured
questions retain ork's owner wait but cannot be filed as a structured ledger
question. Answer, approve, fix and retry record `--phase start` to continue.

## Roles

- **Prompts.** Embedded in ork and canonical (`roles.prompts`). They are
  seeded from slicer's v11 contract, with anything else written fresh
  (`roles.prompt-seed`). They carry no Claude Code-only mechanics (Agent
  tool, SendMessage, nesting), because roles here never spawn agents.
  `ork roles print [ROLE]` prints them so the agents repo can generate its
  own role files.
- **Brief.** It contains:
  - the role prompt;
  - the slice from `slicer show ID --json --lean`;
  - the tracking rule and the workspace limit;
  - the environment lines from the project's AGENTS.md;
  - the role's own temp directory;
  - the report path;
  - for the implementer, the project check command (when configured) and
    `slicer check`, which ork runs after the round (not the finish step)
    outside the role's sandbox;
  - earlier reports, when the role is not resumed.
- **Report.** The role writes its final report to
  `~/.local/state/ork/runs/<run>/<role>-<round>.md` and exits
  (`role.report-io`). If the file is missing, ork uses the vendor JSON's
  final result text. ork validates the shape: the gate's first line, the
  test counts. The prompts ask for one line `Tests: N passed, M failed`
  in the implementer's and the gate's report, and for a final `## Owner
  needed` section in every report (`role.report-shape`).
- **Resume.** For fix rounds and the lite gate, ork resumes a session from the same
  vendor and model where the vendor supports it. Otherwise it starts fresh with the
  earlier reports in the brief (`role.resume`).
- **UI pass.** A read-only role with its own prompt (`ui.md`: 80x24, ASCII and
  monospace, clipping, key hints, and the renders it writes), the same for every vendor. It runs beside
  the reviewer on a slice that touches the UI (see the review state). Its
  lineup row is the matrix row that the rule naming "UI pass row (rNN)" gives
  (u10 today). The matrix may still label that row `reviewer`: ork keeps
  it out of the reviewer's and the gate's fallback
  chains and puts it in the `ui` lineup. The contract JSON supplies the
  ledger role `ui`, with the task `ID ui`. In any fallback chain, an entry identical
  to the one before it is dropped.
- **Spec check.** A read-only role with its own prompt (`spec-check.md`) that
  runs once over a batch of slices before a wave (see the Check step of the
  Wave lifecycle). It works in the project checkout, not in a worktree, and
  has no engine run record. It begins a ledger run on the batch, records
  `spec-check` steps with no parent, and ends it with `pass` after reading the
  report, or `stopped` on failure or cancellation. The daemon runs it as a job,
  reads HEAD and `git status --porcelain` before and after, and fails the check, keeping no verdict, when
  the tree changed during the role's own run with HEAD unchanged
  (`role.readonly-fingerprint`). If HEAD moves, the checkout's working-tree
  status changes outside the role's own run, or the report has a shape error,
  it checks once more with fresh specs. A second failure says the retry was
  used and asks the owner to check again. Permission denials are never retried
  (owner decisions, S143, 2026-10-28). Its report has one `## Slice
  ID` section per slice, holding `Ready` or one or more question blocks; the
  multi-block parse (`ParseOwnerQuestions`) reads every `Question:` block of a
  section with the strict shape of an owner question. Its lineup row is the
  matrix row that the rule naming "Spec check row (rNN)" gives (u11 today:
  sonnet, medium). That matrix row remains separate from every run's
  reviewer and gate chains. The ledger role comes from the contract JSON as
  `spec-check`. Its report and temp directory are under
  `~/.local/state/ork/spec-checks/<project-key>/<item-key>/` (hashed identities).
  Each item has `check.json` identifying the project and item, `spec-check.md`,
  and, after an answer, `answers.json`. The batch's first item also holds the
  role's temp directory. A landed run or removed project loses its check
  directories. At daemon start, directories older than 14 days are swept
  when their item is done, retired or gone, using the ordinary slicer list
  without `--all`. Old directories for unregistered projects are removed too.
  Legacy random-named directories (`YYYYMMDD-HHMMSS-xxxx`) older than 14 days
  are also removed (owner decisions, S143, 2026-10-28). Empty or corrupt
  `answers.json` files are discarded and logged with their full path; the
  check asks the owner's questions again.
- **Hung role** (S182, *proposed*). Every role round has a watchdog. It keeps
  the time of the last vendor stream event or assistant text (`Request.OnEvent`
  and `OnLive`). When a role has sent neither for 30 minutes, the watchdog ends
  the context the round runs under, and the vendor adapter kills the role's
  whole process group. ork kills only the group it recorded for that role
  (`OnStart`); it never kills a process it did not start. The limit is fixed
  at 30 minutes, the stale cutoff of the Running row (owner decision,
  2026-10-29); `Deps.HungAfter` sets it for tests, and a negative value turns
  the watchdog off. The role is recorded with exit `hung`, its ledger step
  ends `fail`, the heartbeat stops, and the slice pauses (kind `lost`) with a
  reason that names the role, the round, the limit and, when ork recorded
  one, the killed process group. A hang is not a vendor limit: it does not fall back to the next
  lineup row and marks no vendor out. There is no automatic retry. The owner
  retries (`ork retry RUN`, or `t`), and the same row runs again with the
  "Earlier process lost" section in its brief. An owner stop during the role
  stays a stop (exit `cancelled`). The wait for a vendor slot is not counted
  as silence. The spec-check role and the checks command are not watched; the
  checks command has its own timeout (`check_minutes`).
- **Reviewer rule.** A reviewer never wrote code in the slice. A reviewer
  may run on another vendor if the lineup names it (`review.cross-vendor`).
- **Ledger.** Each role round is recorded with `slicer relay-run step` with
  role, vendor, model, effort and `--task "ID role"`, and its phase is
  updated on exit. A heartbeat goes out every 10 minutes while
  it runs. The heartbeat says only that ork is alive; it does not show
  that the role is. The first finish step ends the ledger run (`relay-run end`). A
  landing fix round (S119) starts after that, so its rounds, the checks, the
  gate and the second finish step run without ledger steps: `relay-run`
  would refuse them with `run_ended`. The engine run record keeps those
  rounds, and ork records no ledger step once the run has a commit (S153).

## Owner questions

When a role report says it needs the owner (a spec question, a design
choice), the slice pauses in "needs you" with the question. The owner types
an answer, and ork resumes the role with it. A toggle files the question and
answer in the project's adj store (`adj ask` then `adj decide --by
<owner>`), with `--ref ID` (`owner.questions`).

## Vendors

There is one Go adapter per vendor. Each implements: build a command
(model, effort, permission mode, allowlist, output format), start it in its
own process group, stream events, parse the final result and usage, and
classify the exit as ok, denied, rate-limited, auth, error or unclassified. A class
needs a documented signal or a real sample (`vendor.ratelimit-detection`,
`vendor.unsampled-classes`).

The `claude -p --output-format json`, `codex exec --json` and
`grok --output-format json -p` invocations, and the read-only flags
(`--permission-mode plan`, `codex -s read-only`), come from
`agents/setup/lib/workflow-vendor.sh:22-41`.

The S01 spike measured these cells on 2026-10-18 with real headless runs in a
throwaway repo. Each cell cites a fixture under `testdata/vendors/<vendor>/`
or says *unverified* and why. CLI versions: claude 2.1.302, codex-cli 0.163.0 (the S48
probes 05 to 07 ran on 0.164.0), grok 1.0.62. Grok has no successful run, so most of its column is
*unverified* (see Spike results).

| Capability | claude | codex | grok |
|---|---|---|---|
| Streamed events for the output tail | `--output-format stream-json --verbose --include-partial-messages` emits `stream_event` text deltas, then `assistant`, `rate_limit_event` and `result` (`testdata/vendors/claude/02-stream-json.stdout`). `--verbose` was used; without it is untested. | `--json` emits JSONL: `thread.started`, `turn.started`, `item.completed` (agent message), `turn.completed` (`testdata/vendors/codex/01-plain-stream-jsonl.stdout`). The run had whole messages and no delta events. | *unverified*: the stream probe reached the API and got HTTP 403. Its output is an init event, then a `result` event with subtype `error_during_execution` (`testdata/vendors/grok/02-stream-messages-json.stdout`, line 2). No model output streamed. |
| Usage (tokens, cost) in the result | `result.usage` (input, output, cache tokens), `result.total_cost_usd`, and `modelUsage.<model>.costUSD` (`modelUsage` is an object keyed by model name, with `costBasis` `list`; `testdata/vendors/claude/01-plain-json.stdout`). | `turn.completed.usage` has input, cached input, cache write, output and reasoning tokens (`testdata/vendors/codex/01-plain-stream-jsonl.stdout`). No cost field appears, so cost is *unverified*. | *unverified*: the `result` event of the stream probe carries `usage` and `total_cost_usd` fields, but only zero values were seen (`testdata/vendors/grok/02-stream-messages-json.stdout`). Real values are *unverified*. `grok usage SESSION_ID` is documented as "persisted token and cost usage for a session" (`testdata/vendors/grok/usage-excerpt.txt`) and was not run. |
| Resume a session by id | `claude -p --resume <session_id>` answered "OK", the word asked for in the first run, with the same session id and cache reads (`testdata/vendors/claude/03-resume.stdout`). | `codex exec resume <thread_id> -m gpt-6-luna --json` returned the same thread id and cached input tokens (`testdata/vendors/codex/02-resume-by-id.stdout`). The `resume` help lists no `-s` or `-C` (`testdata/vendors/codex/resume-help.txt`). S48 probe 06 (codex-cli 0.164.0) ran from the worktree through the `in_wt` helper, since resume has no `-C`, and passed `-s workspace-write` to `resume` and got exit 2, "error: unexpected argument '-s' found", before any API call (`testdata/vendors/codex/06-resume-workspace-write.stderr`). So resume cannot take `-s`. S89 probe 08 resumed probe 05's session with `-c 'sandbox_mode="workspace-write"'` from the worktree: `resume` accepted `-c` (exit 0) and a `command_execution` item shows the worktree write with exit 0 (`testdata/vendors/codex/08-resume-sandbox-config.stdout`). *Verified*: `resume` takes `-c sandbox_mode=...`. *Unverified*: whether the setting took effect, since the resumed session was already `workspace-write`, so a kept sandbox gives the same result (adj question `vendor.codex-resume-sandbox`). The write to the main checkout has no command event (see the allowlist row). | *unverified*: the resume probe used a team id as the session id, which was a probe mistake. It got HTTP 404 (`testdata/vendors/grok/03-resume-by-id.stderr`). |
| Per-tool allowlist | `--allowedTools Read` with `--permission-prompts none` refused a Write. The denial is listed in `permission_denials` and the exit code is 0 (`testdata/vendors/claude/04-allowlist-denies-write.stdout`; `.repo-status` is empty). A positive control, an allowlisted tool that runs, was not measured. | No per-tool allowlist. `-s` takes `read-only`, `workspace-write` or `danger-full-access` only (`testdata/vendors/codex/help-excerpt.txt`). The one read-only run (`testdata/vendors/codex/03-readonly-sandbox-write.stdout`) ran no command: it has only agent messages. The model said the filesystem was read-only, and `.repo-status` is empty. That run did not exercise the sandbox. S48 probe 05 did, with `-s workspace-write -C <worktree>` where the worktree is linked to a main checkout. Its JSONL has a `command_execution` item (`testdata/vendors/codex/05-workspace-write-paths.stdout`). The write inside the worktree ran with exit 0 (`.worktree-status` shows `inside.txt`). The model reported that the writes to the main checkout and to the main checkout's `.git/slicer` failed with "operation not permitted", and neither file exists (`.repo-status` and `.slicer-dir` are empty). But the JSONL has a command event only for the first write; the other two results are the model's words with no event, so the block is *unverified*: the files may be absent because the commands never ran. Probe 07 (a socket bind) has no command event, so the model's reported `PermissionError: [Errno 1]` at the Unix bind is *unverified*, and a loopback TCP bind was not reached. Whether `.git` of a linked worktree is writable was not measured. S89 probes 08 to 10 (codex-cli 0.164.0, one call each, no retries) aimed to close the gaps, and none produced a command event for the asked cell, so each stays *unverified*: the main checkout write (probe 08 resumed, probe 10 fresh: the model reported `operation not permitted` in 08 (words only) and refused to run the commands in 10, `testdata/vendors/codex/08-resume-sandbox-config.stdout`, `testdata/vendors/codex/10-main-checkout-blocks.stdout`), the main checkout `.git/slicer` write (probe 10, refused, no command), the Unix socket bind, the loopback TCP bind and `git add` plus `git commit` in the linked worktree (probe 09: agent messages only, the model reported `PermissionError: [Errno 1]` at the Unix bind, `testdata/vendors/codex/09-socket-and-git.stdout`). In all three, `.repo-status` and `.slicer-dir` are empty, and in 08 the only command event is the worktree write (exit 0, `.worktree-status` shows `resume-inside.txt`). Only the worktree write is *verified*, twice. The model sometimes declines or skips the command tool, so containment of the main checkout cannot yet be taken from the sandbox alone; the gaps are adj question `vendor.codex-sandbox-gaps` and stay open. Decided: codex may run write roles with `workspace-write` (`vendor.codex-write`, decision 100). The owner relies on the tree fingerprint, the project check, review and the gate as limits; the unmeasured sandbox cells remain *unverified*. | *unverified*: the allowlist probe got HTTP 403 before any tool call. `--tools` takes a comma list (`testdata/vendors/grok/help-excerpt.txt`), and the init event names tools such as `read_file` and `write`, so `Read` may be the wrong name (`testdata/vendors/grok/02-stream-messages-json.stdout`). |
| Rate-limit signal | Not forced. Stream output carried `rate_limit_event` with `rate_limit_info.status` `allowed`, `rateLimitType` `five_hour` and `resetsAt`. Utilization is at `rate_limit_info.unifiedWindows.{five_hour,seven_day}.utilization` (`testdata/vendors/claude/02-stream-json.stdout`). A `rejected` status and the exit code when limited are *unverified*. The help documents only `--fallback-model`, for "overloaded or not available" (`testdata/vendors/claude/rate-limit-help-grep.txt`). | *unverified*: no rate-limit or quota term appears in `codex --help` or `codex exec --help` (`testdata/vendors/codex/rate-limit-help-grep.txt`). No JSON limit signal was observed. Decided: ork matches the reported strings, "You've hit your usage limit" and HTTP 429 `rate_limit_exceeded`, in `turn.failed` or `error` events and falls back automatically (`vendor.codex-ratelimit`). The owner added "out of credits" on 2026-10-27 (S133), classified as a limit under decision 156 (`vendor.fallback-trigger-v2`). The text matches are case-sensitive substrings. The out-of-credits message was observed in text mode on 2026-10-26; its JSON event shape is *unverified*. `testdata/vendors/codex/out-of-credits-unverified.stdout` is synthetic, based on the existing `turn.failed` shape. Real JSON samples are still to be captured. | *unverified*: every run that reached the API (probes 01, 02, 04 and 05) got HTTP 403, "used all available credits or reached its monthly spending limit" (`testdata/vendors/grok/01-plain-json.stdout`). That is a quota limit, not a documented rate-limit signal, and ork has no class for it yet. No rate-limit term appears in `grok --help` (`testdata/vendors/grok/rate-limit-help-grep.txt`). |

- **Fallback** (`vendor.fallback-trigger-v2`, decision 156): an exit
  classified as rate-limited, out of credits or error moves the role to the next eligible entry in
  its chain, in the same state and round. Auth and unclassified exits, an
  unsuccessful round with permission denials, and a changed tree in a
  read-only role still stop.
  If no row remains, the stop reason names each failed row and its exit.
  Out of credits skips all later rows on that vendor; a rate limit takes
  the plain next row. On v10 and v11 the failed ledger step includes the classified
  vendor message as `--reason`. Falling back on a plain error exit was an
  owner-approved deviation from v10 (decision 156, S142). v11 follows ork: its
  `role_fallback` conformance key (v10 called it `vendor_limit`) falls back on
  an error exit of the agent process too, so ork's behaviour conforms.
  The approval card names each switch as `role switched: ROLE FROM to TO
  (REASON)`, identifying rows by vendor, model and effort. It has no
  "Deviation:" prefix. The reason reads "out of credits", "rate limited" or
  "error exit".
  The role stays on the fallback row for the rest of the run
  (`vendor.fallback-sticky`, decision 115). The ordered role records determine
  the row from vendor, model, effort and exit; no new run record field is needed.
  Daemon runs copy the reviewer's lineup into the gate. An equal copy or an
  empty gate lineup follows the reviewer: with no gate records, it starts on
  the reviewer's current row. Existing gate records determine its own sticky
  row and fallback progression. A different gate lineup starts on its own
  first row.
  Resume uses only a session from the same vendor and model. Otherwise the
  next row starts fresh, with the earlier reports in the brief. Both role
  records are kept; the fallback report uses the next attempt suffix.
  A fallback brief names this role's earlier lineup rows and their failed
  exits, and says the worktree may hold partial work: check `git status`
  before continuing. The note also accompanies later rounds on the sticky
  row, including resumed sessions. A first-row brief has no such note.
  The note is also omitted when this role has no earlier failed attempts,
  including the gate's first attempt on the reviewer's fallback row when its
  lineup is empty or a copy of the reviewer's.
  Repeated `slicer start --render --strict` succeeded in the isolated S92
  probe, so the fallback note adds no skip-start instruction.
  Claude has no measured rate-limit class, so its rows can fall back only
  on an error until a sample exists
  (`vendor.unsampled-classes`).
- **Vendor slots and the vendor-out memory** (S169, *proposed*; owner decisions
  2026-10-29). One `engine.Vendors` value in `engine.Deps` is shared by every
  run of the daemon, as `Landings` is. It is in memory only, so a daemon
  restart clears it. A nil value means no cap and no memory, so engine tests
  that set none are unchanged.
  - *Slots.* A round that starts a vendor process first takes that vendor's
    slot and frees it when the process ends. The built-in caps are `codex` 1
    and `claude` unlimited. Rounds on a full vendor wait, oldest first. The
    wait comes before the ledger step and the role record, so it is not role
    run time (the budget never counts it) and not silence for the watchdog.
    The run keeps its global-cap slot while it waits, and its Running row says
    `waits for codex`; its heard cell is dim, with no `!` or `!!` cue, while it
    waits (a ledger heartbeat during the wait is a later slice). `ork stop`
    cancels the wait; it leaves no role record and no ledger step. The
    reviewer and the UI pass of one state can wait for each other's slot, but
    they cannot deadlock: each round holds one vendor slot only, and a slot is
    freed when its process ends. Spec-check rounds (`speccheck.go`) take no
    slot and are not counted.
  - *Memory.* A round that exits `rate_limited` or `out_of_credits` marks its
    vendor out, with the time and the exit text. An `error` exit, an auth exit
    and a role the watchdog killed (S182) mark nothing. The mark is made
    before the round frees its slot, so a round that waits for the slot finds
    the vendor out when it gets it. An exit mark expires after 60 minutes
    (`engine.VendorOutFor`, a fixed expiry; the classifier cannot tell a 429,
    a usage window and out of credits apart, and adj 51 records that the
    usage-limit text can appear with quota left). The owner ends it earlier
    with `ork vendor back NAME`. The owner can also mark a vendor out before a
    round fails, with `ork vendor out NAME`: that mark has no expiry and
    holds until `back` or a daemon restart (owner decision 2026-10-29). A
    second exit mark renews the exit text and the expiry and keeps the first
    time; an exit mark does not change an owner mark.
  - *Effect.* Before a round starts, rows of a vendor that is out are skipped
    to the first later row on a vendor that is not out. With no such row the
    round tries its row anyway, as before. This skips rows before a round;
    stickiness (`vendor.fallback-sticky`) is unchanged, because the role's
    records then match the later row going forward, and a skipped row is not a
    failed attempt (no fallback note). A round that waited for a slot picks
    its row again when it gets the slot: if its vendor went out meanwhile (the
    holder of the slot hit a limit, or the owner marked it), it frees the slot
    and takes the row of another vendor, before any ledger step or role record
    is written. This differs from slicer s405, which on a rate limit takes the
    next matching row: after a rate limit ork also skips later rows of the same
    vendor while it is out, because the exit cannot tell a 429, a usage window
    and out of credits apart.
  - *Plan.* `run.plan` carries a note for each vendor that is out and that a
    chain of the plan uses, as `codex is out until HH:MM (since HH:MM; reason)`
    (`codex is out until back (...)` for an owner mark) and `ork vendor back
    NAME`. `ork status` and `ork vendor` say it the same way. A plan the owner
    confirmed before the note changed is refused as changed.
  - *Wire.* `vendors.list` and `vendors.set` (params `{vendor, out, reason}`;
    the vendor must be one ork can run, `claude` or `codex`, else
    `bad_params`). `status` and `snapshot` carry `vendors_out`, and an engine
    run row carries `vendor_wait`.
- **Detection** (`vendor.ratelimit-detection`): the classifier matches only
  documented signals. Every non-ok round and every round with permission
  denials saves its raw stdout, stderr and JSON at mode 0600 under
  `~/.local/state/ork/fixtures/<vendor>/` as a candidate. Redaction happens
  when the owner promotes one into `testdata/` (`vendor.fixture-candidates`). An
  unclassified exit stops the slice for the owner, who can mark it
  "rate limit" in one keystroke. That sample becomes a test fixture. The
  spike's grok 403 (`testdata/vendors/grok/01-plain-json.stdout`) would be
  such a candidate: no documented rate-limit signal matches it, so it stops
  the slice for the owner.
- **Permissions** (`launch.permissions`): there is a per-project, per-role
  allowlist in the registry. Read-only roles run in their vendor's read-only
  mode. An implementer with no entry (or an empty list) gets the built-in
  default, which the registry replaces as a whole: `Read`, `Grep`, `Glob`,
  `Edit`, `Write`, `Bash(slicer *)`, `Bash(git status*)`, `Bash(git
  diff*)`, `Bash(git log*)` and `Bash(<check command>*)`
  (`launch.default-allowlist`). The default also includes
  `Bash(go -C * test*)`, `Bash(go -C * vet*)`, `Bash(go test*)`,
  `Bash(go vet*)`, `Bash(gofmt *)`, `Bash(git -C * status*)`,
  `Bash(git -C * diff*)`, `Bash(git -C * log*)`, `Bash(adj check*)`,
  `Bash(adj lint*)`, `Bash(adj show*)` and `Bash(adj inbox*)`
  (`engine.default-allowlist`). A registry allowlist for a read-only role is
  an error. A denial stops an unsuccessful round; a success proceeds. The
  spike measured the claude allowlist
  denial: it exits 0 and shows only in `permission_denials`, so ork must read
  the JSON to see it. codex has no per-tool allowlist and ignores the registry allowlist. It may
  run write roles with `-s workspace-write` (`vendor.codex-write`, decision
  100). Read-only roles use `-s read-only`. S48 probe 05 and S89 probe 08
  verified worktree writes; the other sandbox cells remain *unverified*
  as listed in the capability table. The owner relies on the tree
  fingerprint, project check, review and gate as limits.
- **Credentials:** ork uses each CLI's own login. It never reads, stores
  or passes credentials.

- **Codex adapter** (`internal/agent/codex`, S47). A fresh round runs
  `codex exec --json -m MODEL -c model_reasoning_effort=LEVEL -s MODE
  -C WORKTREE PROMPT`. MODE is `workspace-write` for write roles and
  `read-only` for read-only roles. Resume runs from the worktree as
  `codex exec resume ID -m MODEL --json -c model_reasoning_effort=LEVEL
  -c 'sandbox_mode="MODE"' PROMPT`, with no `-s` or `-C` (S89 probe 08).
  For write roles, a fresh round adds `--add-dir TEMP` after `-C WORKTREE`
  (`testdata/vendors/codex/11-fresh-add-dir.stdout`).
  A resumed write role adds
  `-c 'sandbox_workspace_write.writable_roots=["TEMP","WORKTREE"]'`
  after the sandbox mode setting
  (`testdata/vendors/codex/12-resume-writable-roots.stdout`). Both probes
  wrote to the temp directory and the worktree (command events, exit 0).
  The model reported the write outside them denied with "operation not
  permitted"; that write has no command event, and `.repo-status` is empty.
  If no worktree is set, the array contains only TEMP. Read-only roles get neither extra writable-root setting.
  When the role has a temp directory, its environment sets `GOCACHE` to
  `TEMP/gocache`, replacing any inherited value and keeping other entries.
  JSONL thread, item and turn events supply the session, last agent
  message, command lifecycle events and usage. Input plus output tokens
  form the total. Whether cached input and reasoning output are subsets
  of input and output is *unverified*. Cost remains unverified. The report
  file takes precedence over the last agent message. Every non-ok round saves a raw fixture candidate under
  the state directory at `fixtures/codex/`, with files at mode 0600
  (`vendor.fixture-candidates`). The process group is killed on cancellation
  and exit.

- **Claude adapter** (`internal/agent/claude`, S16, `plan.s04-cut`). One
  call runs one role round: `claude -p [--resume SESSION] --output-format
  stream-json --verbose --include-partial-messages [--model M] [--effort E]`,
  then `--allowedTools a,b --permission-prompts none` for a write role or
  `--permission-mode plan` for a read-only one, then the prompt. `--effort
  <level>` is listed by `claude --help` ("Effort level for the current
  session", claude 2.1.x). The process runs in its own process group, using
  `internal/clirun`'s helpers (`code.process-groups`). `stream_event` text
  deltas reach the output tail as they arrive. Session id, final text,
  tokens and `total_cost_usd` come from the `result` event. Cancelling the
  context kills the whole group. The report is the report file, else the
  result text (`role.report-io`). The exit class is:
  - ok: exit 0, `subtype` `success`, `is_error` false, including denials.
  - denied: not ok and `permission_denials` is non-empty.
  - unclassified: everything else. That includes the bad-model run (exit 1,
    status 404; `testdata/vendors/claude/06-bad-model.stdout`), any 401, 429
    or 503, and a `rejected` `rate_limit_event`. No auth failure, bad model
    or rate limit has a real sample yet, so none has a class of its own
    (`vendor.unsampled-classes`). The rate-limited class is never returned.
  - Every round that is not ok or has denials saves its command, exit
    code, raw stdout, stderr and the result JSON under
    `$XDG_STATE_HOME/ork/fixtures/claude/` (else
    `~/.local/state/ork/fixtures/claude/`) at mode 0600. Nothing is redacted
    on save. Redaction happens when the owner promotes a candidate into
    `testdata/` (`vendor.fixture-candidates`).


The S17 engine-to-Claude bridge forwards the worktree, brief, model,
effort, permissions, resume id and per-attempt report path. It forwards
`OnStart` (adj #85) to persist the process group before waiting, and maps
the adapter's exit class, denials, report, session and usage to the engine.

### Spike results

- **Calls.** claude 6 invocations (probes 01 to 06), codex 7 (probes 01 to
  04 in S01, probes 05 to 07 in S48; probe 06 was refused by the CLI before
  any API call), grok 6. Each
  probe's command is in `testdata/vendors/<vendor>/NN-*.cmd`, with the probe
  repo shown as `<probe-repo>`. The scripts are `spike/claude.sh`,
  `spike/codex.sh`, `spike/grok.sh` and `spike/common.zsh`. The `.cmd` files
  were regenerated with `S01_DRY_RUN=1`, which writes the command line without
  running it, so that step made no vendor call. The grok probe 03 line used
  the team id from the first run as `S01_DRY_SID`.
- **Models.** claude probes 01 to 04 used `haiku`, whose `modelUsage` key is
  `claude-haiku-5-5`. Probe 05 ran on `claude-sonnet-5-5`, although its command
  line says `--model haiku` (`testdata/vendors/claude/05-plan-mode-edit.stdout`,
  `modelUsage` key, cost 0.0818584). That was a call on a non-cheapest model.
  The cause is not known. Probe 06 failed before any model ran, so its
  `modelUsage` is empty. codex `gpt-6-luna`, described as "Fast and affordable
  model for easier tasks" (`testdata/vendors/codex/models.txt`). grok
  `grok-4.5`. The CLIs print no prices, so "cheapest" is not verified for codex
  or grok. `grok models` also lists `ollama-local-*` entries
  (`testdata/vendors/grok/models.txt`), which were not used.
- **Exit codes.** claude exits 0 on success and on the denied write. It exits 1
  on an unknown model, with JSON that has `is_error` true and
  `api_error_status` 404 and `subtype` "success"
  (`testdata/vendors/claude/06-bad-model.stdout`). codex exits 0 on success and
  on the read-only run, and 1 on an unknown model, as a `turn.failed` event
  (`testdata/vendors/codex/04-bad-model.stdout`). codex exits 2 on an argument
  error, with the message on stderr and no JSONL
  (`testdata/vendors/codex/06-resume-workspace-write.stderr`). grok exits 1 on the 403 and on
  an unknown model (`testdata/vendors/grok/06-bad-model.stderr`). A denial or a
  blocked write is not an exit code, so the classifier must read the output.
- **Plan mode (claude).** No edit was attempted: `permission_denials` is empty
  and `notes.txt` is unchanged (`testdata/vendors/claude/05-plan-mode-edit.repo-status`
  is empty). The result text says a plan was saved under `<home>/.claude/plans`.
  That was not checked, and nothing under `~/.claude` was opened. A worktree
  fingerprint would not see it. This probe is also the sonnet call above.
- **Grok auth.** `grok models` printed "You are not authenticated."
  (`testdata/vendors/grok/models.txt`). The `-p` call still reached the API with
  the init event's auth source `oauth`
  (`testdata/vendors/grok/02-stream-messages-json.stdout`, field name redacted),
  and the team had no credits left (HTTP 403). No login was attempted.
- **Grok is not measured.** Probes 01, 02, 04 and 05 reached the API and got
  HTTP 403. Probe 03 got HTTP 404 and probe 06 failed on the model name. Probes
  02 to 06 ran after the 403 because the first gate in `spike/grok.sh` only
  stopped on an auth message. Probe 03 used a team id as the session id. Probe
  04 used `--tools Read`, which may not be a grok tool name. Grok needs restored
  credits and a new call-budget decision from the owner before any of its cells
  can be measured.
- **Redaction.** `spike/common.zsh` replaces the home directory with `<home>`,
  the probe repo with `<probe-repo>`, the S48 linked worktree with `<worktree>`
  (both `.scratch/s48` path rules sit at the top of `redact_file`), other scratch paths with `<scratch>`, the
  project directory name with `<project-dir>`, and `/tmp/cc-socks` sockets with
  `<tmp>/cc-socks/<socket>`. It also redacts `sk-` keys, bearer tokens, key,
  token, secret and password values, runs of 40 or more characters, the grok
  team id, the id after `--resume` in `.cmd` files, and key-like field names.
  Session ids in outputs are kept: they are local run ids, not credentials.
  Fixtures rename `apiKeySource` to `REDACTED-KEY-NAME`, so parser tests must
  not read that field from fixtures. The check
  `rg -i 'sk-|api[_-]?key|token"\s*:\s*"[A-Za-z0-9]{20,}' testdata/` finds
  nothing. The sweep for `/Users/jon`, `/private/tmp` and `/tmp/cc-socks` finds
  nothing in `testdata/`. In `spike/` it finds only the rule lines in
  `common.zsh`, which must name those strings.
- **Help text.** Full help texts are not stored. The key pattern matches
  words such as "disk-" in them, so the excerpts keep only the lines that
  cells cite. Rate-limit terms were searched in full help text, and the
  matches are in each `rate-limit-help-grep.txt`.

## Budgets

The limits are per slice: minutes of role run time and total tokens, with
defaults in the registry and overrides at launch (`run.budgets`). When a slice reaches a
limit, ork lets the running role finish, then pauses the slice before the
next role starts with pending kind `budget`, and waits for the owner.
Minutes sum each role's `StartedAt` to `EndedAt` (to now for a running role);
time spent paused does not count. A limit is reached when usage is equal to
or above it. Retry grants exactly one more role round, then the check applies
again (`run.budget-resume`, adj #127). A question, shape or error pause spends
that grant, including an error before the role starts. The next retry checks
the budget again.
Launch overrides merge per field: zero or omitted minutes or tokens keep
the registry value. Negative limits are refused. The plan shows the merged
budgets, and `run.json` records them (`run.launch-wire`). They apply to
that run only (`lineup.override-scope`).

## CLI

`cli.json`. Every command takes `--json`. Exit codes: 0 ok, 1 failure, 2
usage error (an unknown command, flag or role, a missing or extra argument, a
relative project path).

```
ork                         open the TUI (start the daemon if needed)
ork status [--project P]    waves, slices, roles, needs-you
ork wave suggest P          slicer's suggested waves, in order, with reasons
ork wave start P ITEM...    check the specs, then launch (asks unless --yes)
ork run P ID                show a plan, check the spec, then launch one slice
ork approve RUN             finish a passed slice, commit and land it
ork check RUN N [STATE]     mark human check N: ticked (the default), later or open
ork fix RUN TEXT            send a fix note to a fix round
ork answer RUN TEXT         answer an owner question
ork file RUN N              file a later finding through the daemon
ork new P TITLE [FLAGS]     file a slice from a request or note
ork dismiss RUN             clear a stopped run from Needs you
ork retry RUN               retry a paused role or step
ork stop RUN|WAVE           stop and record why
ork daemon run              serve the daemon in the foreground
ork daemon stop [--force]   stop the daemon; on a daemon that predates
                            daemon.stop, print the pid and a kill hint
ork daemon restart [--force]
                            stop and start the current executable
ork roles print [ROLE]      print the embedded role prompts
ork roles matrix P          show the role matrix as slicer prints it, then
                            which rows ork uses, skips or does not run (S168)
ork projects add|list|rm|set|pin|unpin
                            manage the registry
ork doctor [--upgrade]      check versions, CLIs, logins, contract, ork path, projects;
                            print upgrade commands (S178)
```

S03 ships `status`, `projects`, `roles print` and `doctor`
(`cli.s03-commands`) in `internal/cli`; the binary wires them in S13
(`cmd/ork`). The rest
come with the engine. S17 adds `run`, `approve`, `stop` and `answer`
(`engine.owner-surface`, adj #73).
S44 adds `retry` for pauses without a question (`engine.pause-resume`, adj #101).
S60 adds `wave suggest P`, `wave start P ITEM...` and `stop WAVE`.
S116 makes `wave start` run the spec check after it shows the plans: each
open question is printed with its options and read from standard input (a
number picks an option and a number outside the options is asked again,
other text is the answer, an empty line holds the item back). Each question
is labeled with its item, as `[S1 1/2]`. An item whose questions stay open is
left out of the wave and named as `held`; a wave with no item left does not start and exits 1. With
`--json` the questions and the shared `Owner needed` text go to stderr.
The JSON result always includes a `held` array of excluded item IDs, including
when the wave starts or the owner cancels; it is `[]` when nothing is held.
S167 gives `ork run` the same sitting after the plan and before
`Start this slice? [y/N]`. A number picks an option; text answers directly.
An empty line or end of input holds the slice and exits 1, without starting.
`--yes` skips only confirmation and still runs the spec check. A failed
check blocks launch. With `--json`, the sitting goes to stderr and stdout
holds one result; a held slice returns `{"started":false,"held":["ID"]}`.
The Roadmap launch uses the same sitting after its plan and before its
confirmation (S184). Skipping leaves the slice held and closes the launch;
a failed check blocks launch with no skip key. Editing the confirmed lineup
replans without another spec check.
`ork run` refuses a blocked item through the spec check (S181); raw
`run.start` does not read dependencies yet.

Suggestions call `slicer plan-parallel --json --lean --root ROOT` for the
registered project. Tool errors keep their code and message, including a
missing `files_section`. The owner chooses the item ids (`wave.start-input`).
The CLI asks the two y/n inputs for each item, shows each `run.plan`, then
asks `Start this wave? [y/N]`. `--well-specified` and `--split` apply to
every item; both are required with `--yes`. In JSON mode, prompts and plans
go to stderr and stdout holds one result.

`ork run` and `ork wave start` accept `--stop-after land|commit` (*proposed*,
S177). The plan prints `stop: after landing` or
`stop: after commit (you land it)`. Invalid values exit 2 before any daemon
call. Wave start applies the flag to every item. `run.plan` validates and
normalizes an absent value to `land`; `RunPlan` echoes `stop_after`, and
`run.start` refuses a changed value in the confirmed plan. Each run record
keeps the chosen value. The choice is per run; there is no registry default.

`ork run` and `ork wave start` accept `--implementer`, `--reviewer` and
`--architect`, each taking `VENDOR:MODEL:EFFORT`. Repeat a role flag to set
its whole chain in order, primary first. The model may contain colons;
vendor and effort are the first and last parts. `--architect off` alone
removes the architect. Gate follows reviewer and the UI pass stays from the
matrix; neither has a flag. `--budget-minutes N` and `--budget-tokens N`
accept whole numbers of zero or more; zero uses the project default. Omitted
flags keep the recommendation and project defaults. Invalid flag syntax,
empty parts, mixed architect off and entries, and negative budgets exit 2
before a daemon call. Vendor, model and effort validation stays in the daemon.
On `wave start` these overrides apply to every item. The CLI sends the same
overrides on planning and starting, with the printed plan as Expected.
Edits apply to those runs only; the matrix and registry stay unchanged.

Wave items carry optional lineup, budgets and a confirmed `expected` plan.
Each item uses the existing `run.start` override validation and plan check
(`run.launch-wire`). As with `run.start`, an absent `expected` permits a
start from the current plan. The CLI supplies it for every item. A stale
plan or admission error is returned for that item; later items still start.
All admitted records share one `wave-YYYYMMDD-HHMMSS-xxxx` id and use the
global and project caps. One failed or stopped item leaves its peers
running (`wave.failure`). `ork stop` selects a wave by its `wave-` prefix
(`wave.stop-target`). Queued members stay queued until stopped, with no
tool writes, even as stopping active members frees slots. A wave result
with any item error exits 1; successful peers remain visible in the result.

`ork run P ID` asks `Well specified?` and `Split?` as y/n questions
(`[y/n]`, no default) before showing the plan, runs the spec check, then asks
`Start this slice?` (`[y/N]`, Enter takes No) (adj #86,
`run.lineup-inputs`). The prompts take y, yes, n or no in any case, and
Enter alone is an error where there is no default (adj #87,
`run.prompt-answers`). Each answer can also be given with its flag,
`--well-specified` or `--split`, which take the same spellings. With
`--yes`, both flags are required; a missing or invalid input is a usage
error (exit 2).
The plan shows the worktree, branch, check command, launch budgets, lineup
and each role's resolved permissions. Without an override, the lineup keeps
every row with an adapter from `slicer relay-roles recommend --json` in
matrix order: `Claude` maps to `claude`, and `OpenAI` maps to `codex`. The
engine runs only the first entry until S49. The gate uses the reviewer's
lineup. The daemon refuses to start if the plan changed during confirmation. It starts one engine run
in the background; CLI disconnection does not cancel it. The daemon owns
the run store and reconciles stored runs before accepting clients.
`approve` runs the finish step, then commits once (adj #81), then lands it (land state, adj #162); `answer`
resumes a paused role that asked a question. `retry` calls `Engine.Retry`
to rerun the role or step of a `shape`, `error`, `lost` or `interrupted`
pause (adj #101). A `lost` pause also covers a role the watchdog killed as hung
(S182); the retry runs the same row. A question needs `answer` or `stop`. An `answer` on a
pause without a question names `ork retry RUN`. Status shows the pause
reason or question and the next command: `ork retry RUN` or
`ork answer RUN TEXT`. These owner commands wait for the engine
to rest. `stop` cancels the running job, waits for the role to exit,
reconciles any surviving process group, and stops the run. No TUI actions
are added in S17.

*Wire fields (`run.launch-wire`).* `run.plan` and `run.start` take
`{project,item,well_specified,split,lineup?,budgets?}`. `lineup` maps each
overridden role to its full chain of `{vendor,model,effort}` entries
(`lineup.launch-edit`). Start also accepts `expected`, the plan the owner
confirmed, including lineup and merged budgets. `run.approve`, `run.stop`
and `run.answer` take
`{run,text?}`. Plan returns `{project,item,worktree,branch,check_command,
lineup,budgets,permissions,note?}`; the project contains only its name and path.
The proposed optional `note` (S135) explains a failed `relay-roles show` read.
The plan then treats UI and spec-check rules as absent; a failed recommendation
still prevents planning. `ork run` and the TUI launch confirm show the note
under the plan rows (S159, *proposed*).
Start and owner commands return the run record. With `--json`, run prints
the plan on stderr and a single result JSON value on stdout; declining
prints `{"started":false}` on stdout. Interactive questions go on stderr
in JSON mode.

*Proposed (S172):* `ork adj answer PROJECT ID (--option LABEL | --text TEXT)`
reads the project's open records, prints `#ID Title` and the options, marks
the recommended option with ` (recommended)`, and asks
`Decide #ID as LABEL in adj as NAME? [y/N]`. A typed answer is quoted in the
prompt. Enter and closed input cancel. The command always prompts; it has
no `--yes`. `ADJ_DECIDED_BY` must name the owner before the prompt.
`adj.answer` takes `{project,id,option?,text?,by,via}` and returns `{message}`.
The engine reads the record again and refuses a record that is not open or
an option it does not offer. Only the confirmed option or typed answer goes
to `adj decide`, with `--by` set to the owner and a `--why` naming ork,
the surface (`cli` or `tui`) and the date. No role or automatic step calls
this method.

`ork adj ask PROJECT TITLE [--key K] [--context TEXT|-]` also accepts repeated
`--option LABEL:SUMMARY`, `--tradeoffs LABEL:TEXT`, `--ref R` and `--tag T`,
plus `--recommend LABEL` and `--reasoning TEXT`. It is CLI only and has no
confirmation because it decides nothing. Context `-` reads stdin. The asker
is `ADJ_ASKED_BY`, falling back to `ADJ_DECIDED_BY`, or empty. It offers zero
options or two to four. A title starting with `-` is refused because adj
treats it as a flag. A recommendation must name an option, and reasoning
needs a recommendation. With a key, `adj check` runs first; an existing
open, decided or closed record is reported by id and prevents asking again.
`adj.ask` takes `{project,title,key?,context?,options?,recommend?,reasoning?,
refs?,tags?,by?}` and returns `{id}`. Both methods require a registered project
that uses adj and refresh its Open and Decided parts after a successful
write. The CLI prints the notice or new id; `--json` prints `{message}` or
`{id}` and sends the confirmation to stderr. Declining prints `cancelled`,
or `{"cancelled":true}` in JSON mode, and exits 0. ork never rejects,
supersedes or withdraws a record. TUI answer controls are S185.

`run.retry` takes `{run}` and returns the run record (adj #101).
*Proposed (S118):* `run.fix` takes `{run,text}` and returns the run record
once the fix round, the checks and the gate have ended and the run rests
again; `run.check` takes `{run,check,state}` (the one-based human check;
`state` `ticked`, `later` or empty to reopen) and returns the run record.
`run.approve` returns the run record once the run rests again (S148): the TUI
reads `landed_at` (landed) or `state` `awaiting-approval` with `land_fixes` above
0 (a blocked landing went to its fix round) from it, with no new field.
`run.approve` fails with the open checks named while any is open. The engine
run rows of a run that awaits approval carry `card` (`state.md`). *Proposed
(S141):* `ork check RUN N [ticked|later|open]` calls `run.check` (ticked unless
told; `open` reopens) and `ork fix RUN TEXT` calls `run.fix`, which returns when
the fix round, the checks and the gate have ended. Both follow `ork answer`:
they wait for the engine, print `RUN STATE` or, with `--json`, the run record,
and exit 1 with the daemon's error when it refuses; a missing or non-numeric N
or an empty TEXT is a usage error (exit 2). So `ork approve` works from the
shell on a run with human checks: it fails naming the open checks, `ork check`
marks them, and the next `ork approve` goes through.
*Proposed (S121):* `run.render` takes `{run,name}` and returns `{name,text}`:
the plain text of one render of the run's card, read from
`tmp/ui-<ui_round>/renders/NAME` in the run directory. It reads the stored
record and the file, so it works while a role runs and on a finished run. The
name must be one the run's card lists; the file must be a regular `.txt` file of
that directory (a link, a path or a directory is refused), and the text is
plain text of at most 24 KiB. A run without a card, an unknown run, a run id
that is not a plain name, a name the card does not list and a file that is not
there are all `not_found`. A run whose `run.json` exists and cannot be read
(not valid JSON, an unknown version) is not "no run": the call fails with the
code `internal` (no new wire code) and the message `run ID cannot be read: WHY`.
`run.report` answers the same way. The TUI calls it when the owner opens the
renders and again for each render it moves to, and keeps only the answer to its
latest call for a render (each call carries a number from a counter on the
model that never restarts); no snapshot carries render text.
*Proposed (S176):* `run.report` takes `{run,name}` and returns `{name,text}`.
The name must be listed in the stored record's `roles[].report`. The daemon
reads the record and file without waiting for the run. The file must be a
regular `.md` file directly in the run directory. Links, paths, directories,
unlisted names, missing files and unknown runs are `not_found`. The engine
checks that the opened file is the same regular file that was listed,
drops terminal controls with `runrec.PlainText`, and bounds the read at
64 KiB, cutting at a line end with `(cut: over 64 KiB)` when longer. Status
role rows carry `report` names (omitted when empty), never report text. The
TUI reads text on demand when `p` opens or moves to a report.

*Proposed (S112):* `relay.answer` takes
`{project,run,question,option?,text?,by,via,adj?}` and returns `{message}`.
The daemon re-reads the ledger and refuses unregistered projects, missing,
ended or stale runs, linked engine runs, missing or answered questions,
unoffered options and missing owner names. Exactly one option or non-empty
text answer is required. The TUI calls only this daemon method. It writes
`slicer relay-run answer` with the option or text and `--by`, without
`--why`. The optional `adj` integer carries the record ID named in the owner's
confirmation. The TUI sends it only when the confirmation says "and adj #N".
When the question has an adj key and the project uses adj, the daemon checks
that key again. It answers through `engine.AnswerRecord` with Via `tui` only
if the same confirmed ID is still open. Otherwise it writes only the ledger
and explains the missing confirmation, different ID, or non-open key.
The message preserves ledger success and reports adj failures. Questions
without a key and projects without adj receive only the ledger success
message, with no adj suffix.
The daemon refreshes the ledger and decision cache after writing.

*Proposed (S175):* `run.end-ledger` takes `{project,run}` and returns `{run}`
once it has ended a stale slicer ledger run with `slicer relay-run end RUN
--outcome stopped --close-steps fail --json` (its running steps did not
finish). It reads the ledger again (`relay-run list`) and refuses with
`refused` unless the run is still stale, so an event that arrived after the
owner opened the pane wins; it refuses a ledger run that an unfinished engine
run links to (`relay_run`), whose owner stops that run on its own pane. A run
the ledger does not list and an unregistered project are `not_found`. The call
writes no run record; slicer's failure comes back as the error.
*Proposed:* `run.file` takes `{run,finding}` (the one-based `findings[]` entry)
and returns the run record with a `message` (`filed as ID`) once ork has run
`slicer add` and `slicer promote` for that later finding (adj #163). It does
not wait for the run, so it works while a role runs and on a stopped or
committed run that the daemon did not rebuild at its start (the record comes
from the store), and it refuses a finding that is not a later finding or is
already filed. The engine run rows carry
`findings:[{n,kind,title,ask?,key?,scores?,answer?,filed?}]` for the pane.
Proposed (S137): finding calls share a per-run lock. Finished-run calls open
the record and load an engine fresh inside that lock on every call, preserving
dismissal and cleanup timestamps. Dismissal and cleanup use the same lock;
no finished-run engine is cached. `ork file RUN N` calls `run.file`.
`run.answer` with `finding: N` answers a non-blocking owner finding without advancing the run;
a keyed finding in a project with adj requires the caller's `by` and files
in adj before recording the answer. Without adj it records the answer only.
Unfiled later findings and unanswered owner findings each add one to Needs you
and status counts only while their run is not landed, dismissed or superseded.
The wire retains dismissed runs with `dismissed: true`, hidden from lists,
so an open run pane can still show their findings.
`run.dismiss` takes `{run}` and returns the run record (adj #117). It refuses
a run that is not stopped or is busy. An unknown run returns `not_found`.

`ork new PROJECT TITLE [--why TEXT|-] [--from ITEM] [--from-note N]`
accepts optional `--size S|M|L`, `--importance N`, `--urgency N`, `--effort N`
and `--json` (S170). `--why -` reads stdin verbatim. A blank or multi-line
title, invalid size, invalid score or invalid note argument exits 2 before
dialing. Scores are 1-3; omitted scores stay unspecified. `--from-note` is
one-based and requires `--from`. The submitted command is the owner's act;
there is no confirmation prompt.

The proposed `item.new` wire method takes
`{project,title,why,from,from_note,size,importance,urgency,effort}` and returns
`{id,existing}`. The daemon resolves the registered project and calls
`engine.FileNewSlice` without a run. It uses `slicer show` to read the source
note if requested, `slicer add` with non-empty optional fields, then
`slicer promote` with a temporary, mode-0600 outline removed after the call.
The outline has only Why: owner text, else the quoted note, else
`Filed in ork on DATE. No description was given.` A source adds
`Discovered from ITEM.` The key is `ork-new-` plus 16 hex digits of SHA-256
over the project path, normalized title, owner Why, source and note number.
This identity excludes the generated date so retries on another day keep
their key. An existing slice skips promote. A failed promote names the filed
ID and a retry promotes that same item. Success refreshes Items and Deps.
Text output is `filed ID` or `ID is already filed`; JSON is
`{"id":"ID","existing":false}` (true on an already filed request).
Filing is in v1.0 (`scope.v1-ork-edits`, owner decision 2026-10-29).

`ork note PROJECT ITEM [--] TEXT [--json]` calls `item.note` (S171).
Quote TEXT as one argument. Text after ITEM is literal even when it starts
with `-`: `ork note P S12 "- text"` and `ork note P S12 -- "-x"` both work.
`--json` may precede PROJECT or follow TEXT. TEXT `-` reads stdin, removing
its trailing newline. A blank ITEM, blank note or wrong argument count exits
2 before dialing the daemon. Blank text says `Note must not be empty`;
wrong counts say `note: takes PROJECT ITEM TEXT, got N arguments; quote TEXT`. Success prints `noted ITEM` or
`{"item":"ITEM","noted":true}`. The daemon validates the item and text,
resolves the registered project, and runs
`slicer note ITEM --text TEXT --render --json --root PROJECT`. It uses a
writer with no run sink because this write is outside a run. After success
it refreshes only Items before returning. Notes belong to slicer; snapshots
and item detail carry its notes without an ork-owned copy. Notes are part
of v1.0 (`scope.v1-ork-edits`, owner decision 2026-10-29).

`ork doctor` checks:
- the running ork binary: its path, resolved through symlinks
  (`os.Executable`). It is printed first and is not a pass or fail check;
- an existing daemon's build ID against this CLI's ID (S46). A mismatch
  warns and names `ork daemon restart`; it does not fail doctor or start a
  daemon. *Proposed JSON fields:* `daemon_build_id` and `warning`;
- the slicer and adj versions (`--version`);
- each vendor CLI is present, with its `--version` output; a vendor CLI that
  is missing or broken is reported and does not fail the check, because ork
  needs only the vendors a lineup uses (`doctor.vendors-optional`). The login check (by
  running a trivial command, without reading credentials) comes in S06, and
  doctor makes no model call;
- *Proposed (S178):* where slicer and adj came from, and the commands to
  upgrade them. Each of the two tools gains `resolved` (symlinks followed) and
  `install`: for slicer `checkout` (the resolved path is `D/.venv/bin/slicer`
  and `D` is a git root; `checkout` then holds `dir`, `describe`, `branch`,
  `upstream` and `behind`, the commits the upstream branch has that HEAD lacks
  as of the last fetch, or -1 without an upstream) or `venv` (`V/bin/slicer`
  with `V/pyvenv.cfg`); for adj `go-binary` (the standard library's
  `debug/buildinfo` reads `build`: `module`, `version`, `revision`,
  `modified`); otherwise `unknown`. The report gains `upgrade`, always
  present and empty when nothing is behind: a list of `{tool, reason,
  commands[], note?}` for slicer, adj and ork. A hint appears when the
  contract line is older than the lowest accepted version (slicer is behind),
  when it is newer than the highest (ork is behind), when a slicer checkout
  is behind its upstream, or when slicer or adj is missing; `--upgrade`
  prints all three whether or not anything is behind. Text prints
  `[upgrade] TOOL  REASON`, an optional `note:` line, then each command on its
  own line, indented, after the contract line. slicer: a checkout gets
  `git -C DIR pull --ff-only` and `DIR/.venv/bin/pip install -e DIR`, a venv
  gets `V/bin/pip install --upgrade squalor-slicer`. adj and ork get a pull
  and the documented build with the installed path filled in and the checkout
  named as a placeholder (`ADJ_CHECKOUT`, `ORK_CHECKOUT`), and ork also gets
  `ork daemon restart` by its installed path. A source that is not recognized
  gets a `note` naming the sibling README section and no commands. Doctor
  reads only local facts: symlinks, build information, and `git -C DIR`
  `rev-parse`, `describe` and `rev-list`. It never fetches, pulls, builds,
  makes a network call or runs a printed command, and a hint never changes the
  exit code;
- the slicer contract: the `contract` string of `slicer ai relay --json --root
  ROOT` against the accepted set (`roles.AcceptedContracts`, v10 and v11;
  `boundary.contract-set`, adj 175). Any other version fails. The check runs
  for the directory doctor is run in, and again for each registered project
  with that project as `--root`, listed as `contract.projects` in the JSON
  (`project`, `pinned`, `found`, `ok`, `error`) and as one `contract NAME` line
  each in the text. A project fails when its pin is outside the accepted set
  or its slicer refuses with `contract_mismatch` (exit 2, no contract text;
  `slicer/docs/agents.md:712`); slicer's error object names the pinned and the
  served version. An unpinned project is checked against the accepted set alone. A
  project whose path is gone is skipped (its project finding says so). The pin
  is set with `ork projects pin NAME --relay N`, which refuses N outside the
  accepted set before it runs anything, runs `slicer config relay_contract N
  --root PROJECT`, and reports the `pinned` value that `slicer ai relay
  --json` then prints; `ork projects unpin NAME` runs `slicer config
  relay_contract --unset --root PROJECT`. Neither needs the daemon, and ork
  never writes the pin to `projects.json` or to slicer's config file itself
  (`boundary.contract-pin`, adj 97). Engine start reads the same string, and a
  `contract_mismatch` refuses the run with slicer's error and no run record.
  Doctor and the engine also validate `ai relay --json`: exact finding kinds `fix`, `owner`, `later`,
  required conformance keys (v11: `role_fallback` and `blocker_wait` in place of
  `vendor_limit`), ledger role names (v11 includes `gate`), gate criteria and
  verdicts, and outcomes. The run records its version, roles and gate criteria
  for resumed runs. On v11 a gate round closes its ledger step with
  `--phase done --verdict pass|fail`; v10 closes it without the flag. A gate
  report with no valid verdict line closes without one. Embedded role prompts
  are written against v11;
- each project's path exists and is a directory; a configured check command
  passes `sh -n -c` and its first word after leading `VAR=value` assignments
  resolves on PATH, or against the project directory when it contains `/`.
  Lookup is skipped for POSIX reserved words and special builtins, plus `cd`
  and `command`, plus `time`, and words containing `$` or a backtick or
  starting with `~`. A syntax finding drops the shell prefix up to its first
  marker: `sh: -c: line N: ` or `sh: N: `, and any text before that `sh: `.
  Doctor never executes the check command. Nonempty worktree and branch
  patterns must contain `{id}` or `{id_lower}` (*proposed*, S124); empty
  patterns use the defaults. A finding names the fix, `ork projects set NAME
  --worktree-pattern PATTERN` or `--branch-pattern PATTERN` (S161).
  The registry is read only; a missing registry is empty and is not created.
  A registry read error is advisory: JSON has `registry_error` (omitted when
  there is no error), separate from the daemon's `warning`. Text prints
  `[WARN] registry  ERR` just before the project lines, with the leading
  word `registry` of the error text dropped so the label is not repeated;
  the JSON field keeps the full text. With a readable registry that holds no
  projects (missing or empty), text prints
  `[--] projects  none registered: run ork projects add PATH` in the same
  place. Neither line changes `ok` or the exit code.

Doctor exits 1 when slicer, adj or the contract check fails. Project findings
do not change `ok` or the exit code. `roles print`
and `doctor` run in the CLI process. `status` and `projects` go through the
daemon, which the CLI starts on demand; with `--json` an error is
`{"error":{"code","message"}}` on stdout, and the text form is `ork: MESSAGE`
on stderr.

### Output

Text output is plain lines meant to be stable. `--json` output, indented:

- `status`: `{"projects":[{"name","path","adj","needs_you","open"?,"held"?,
  "engine_runs","active_relays":[
  {"id","items","state","worktree","begun_at","updated_at","steps",
  "running"}],"parts":{"items":{"age_ms","stale"?,"error"?}},
  "errors":[{"part","code","message"}]}]}`. `needs_you` is the project's count
  of open adj records, slicer items flagged `needs-owner`, and engine runs
  awaiting approval, paused, or stopped and not superseded (`status.needs-you`,
  adj #69). A stopped run stops counting once a newer engine run exists for
  the same slice, ordered by created time (adj #92). The *proposed* optional
  `open:[{id,key,title}]` and `held:[{id,title}]` fields list each open adj
  record and needs-owner item; empty lists are omitted. Text status prints
  these entries under the project before relay and engine rows, using the
  Overview wording. Its engine rows use the same Needs, Running and Hidden
  classification as the Overview: committed and superseded runs are hidden
  (`ui.status-list`, adj #125). JSON keeps every engine run, including hidden
  ones. The *proposed*
  `engine_runs:[{id,item,branch?,merged?,state,wait?,pending?,question?,denials?,error?,unavailable?,
  relay_run?,roles?,activity?,tail?,tokens?,cost_usd?,created_at}]` field per project includes the linked slicer
  ledger run ID in `relay_run`, when present, and the engine run's creation
  time in `created_at`. The optional `roles`
  array and the proposed `branch` and tri-state `merged` fields are described
  in [state.md](state.md). `denials` is an optional
  array of distinct `Tool` or `Tool: command` texts from role records.
  Texts are deduplicated across roles in first-seen order. Repeats append
  ` xN`, such as `Write: /x.txt x2`
  (`ui.denials-display`, `engine.denied-command`). `wait`
  contains the approval prompt, owner question, pause or stop reason. The optional
  `pending` string holds the paused run record's pending kind (`question`,
  `shape`, `lost`, `interrupted` or `error`); other states omit it
  (`ui.pending-kind`). The optional `question` object carries `title`,
  `options:[{label,summary}]`, one-based `recommend` and `reason` for a paused
  structured question. Older snapshots without it still load. `active_relays` are slicer's active relay runs.
  The *proposed* optional `error` is a daemon-memory job or recovery error.
  The *proposed* optional `unavailable` marks a row without an engine.
  Removed-project groups are display-only and have `unavailable: true`.
  They appear in status and the TUI without changing the registry. They do
  not participate in project counts, launches, refresh or focus.
  `errors` lists failed parts (`items`, `goals`, `runs`, `open`, `decided`)
  with the tool's own code. `parts` gives each part's age in ms (0 for a part never read) and `stale`
  when the part keeps an older value; the error is the one that failed. Text: one line `NAME: needs
  you N, active relays M`, then one line per relay, engine run (with state,
  wait reason and the `denied:` line), and error. The denied line shows at
  most three texts, then `and N more` for the remaining texts. The text
  does not show ages yet.
  With no projects, text `status` prints
  `no projects: run ork projects add PATH (or run ork to add one in the TUI)`
  and exits 0. Project commands (`run`, `wave suggest`, `wave start`, and
  `status --project`) report that guide as an error and exit 1. JSON status
  keeps an empty projects array; JSON project errors use the usual error object.
- `projects list`: `{"projects":[...]}`, each as in `projects.json`. Text: name,
  path and `adj`, in columns.
- `projects add [PATH] [--name N]`: `{"project":{...}}`. The name defaults to the
  path's base name. `adj` is true when `PATH/.adjudicate.json` exists. A
  duplicate name, a relative path, or a path that is not an existing
  directory is refused (`projects.add-path`). Text: `added NAME PATH [(adj)]`.
  Without PATH, text mode offers the current directory with
  `Add DIR as project NAME? [y/N]: `. Only y or yes accepts, without regard
  to case. Enter, EOF and other answers cancel with exit 1. JSON mode
  requires PATH and never prompts. No marker is required for the CLI offer.
  An empty registry is valid; startup writes nothing and never seeds
  (`registry.no-seed`, adj 157). The first add writes projects.json.
  The TUI Welcome add guides registration through this same daemon method;
  its proposed confirmation and error layout is in dashboard.md.
- `projects rm NAME`: `{"removed":"NAME"}`. Removal is refused with
  *proposed* `project_busy` while any run of the project is not committed or
  stopped. This includes queued, paused, awaiting-approval and running states.
  The message names the run ids. The project stays registered.
  A planned start that loses the race to removal is refused with `not_found`.
- `projects set NAME [--push on|off] [--ui-doc PATH] [--worktree-pattern P]
  [--branch-pattern P] [--check CMD] [--check-minutes N] [--cap N]
  [--budget-minutes N] [--budget-tokens N] [--env-line LINE]...
  [--permission ROLE=read-only|ROLE=]... [--allow ROLE=PATTERN]...
  [--vendor-env VENDOR:KEY=VALUE]... [--unset-vendor-env VENDOR:KEY]...`
  (*proposed*, S161; the last five groups S174): one flag per optional
  registry field, as in state.md. A flag that is given sets its field; a flag
  that is left out keeps the value. An empty string or 0 clears a field.
  S174 flags: `--env-line` repeats and the lines given replace the list (one
  empty `--env-line ""` clears it). `--permission ROLE=read-only` sets the
  role's preset and `--permission ROLE=` removes its entry, so the default
  applies; `ROLE=` also removes a role key that ork does not know when the
  file holds it (a hand-edited typo), while setting an unknown role is
  refused. `--allow ROLE=PATTERN` repeats, and the patterns given for a role
  replace that role's list. `--vendor-env VENDOR:KEY=VALUE` sets one key (the
  first `:` ends the vendor and the first `=` ends the key) and
  `--unset-vendor-env VENDOR:KEY` removes one key, and the vendor once it is
  empty; an empty value (`VENDOR:KEY=`) is kept as an empty string. A role or a key changes alone, so a second command does not need the
  first one's values. A malformed value, `--permission` and `--allow` for the
  same role, or `--vendor-env` and `--unset-vendor-env` for the same key is a
  usage error (exit 2) before the daemon starts, and a usage error never
  quotes a `--vendor-env` value. The text line names the fields and counts
  (`environment_lines=2 lines`, `permissions=implementer:3 patterns;
  reviewer:read-only`, `vendor_env=codex:A,B`, with `unset` or `default` for
  a removal) and never prints a `vendor_env` value; `--json` prints the
  project as the registry holds it, as `projects list --json` does, so it
  includes the `vendor_env` values. `--push` takes `on` or `off`. At least one
  flag is required, and an unknown flag, a bad `--push` value or a non-number is a usage error
  (exit 2) before the daemon starts. The daemon (`projects.set`) validates the
  values and refuses with `bad_params` (a `ui_doc` that is absolute or leaves
  the project, a pattern without `{id}` or `{id_lower}`, a negative number) or
  `project_busy`, as `projects rm` does, and then changes nothing. JSON:
  `{"project":{...}}`. Text: `updated NAME: FIELD=VALUE, ...` in flag order,
  with `unset` for a cleared field; the text shows the values as saved (a
  `ui_doc` is cleaned). The daemon reads the registry for each start, so a run
  that starts later uses the new values and the daemon needs no restart. A
  start whose plan was read before the change and that is admitted after it is
  refused with `plan changed; run the command again to confirm it`; run
  the command again.
- `projects pin NAME --relay N` and `projects unpin NAME` (S24): the slicer
  contract pin of a registered project, set through slicer
  (`boundary.contract-pin`, adj 97). Neither starts or needs the daemon; both
  resolve NAME in the registry file. JSON: `{"project":"NAME","pinned":N}` for
  `pin` (the `pinned` value that `slicer ai relay --json` prints after the
  set) and `{"project":"NAME","pinned":null}` for `unpin`. Text: `pinned NAME
  at relay vN` and `unpinned NAME`. Exit 2 (usage, before any tool runs): a
  missing `--relay` (`projects pin needs --relay N (relay v10, v11)`), an N
  outside the accepted set, a wrong argument count, or a NAME that is not
  registered. Exit 1: slicer fails the set or unset call (its message is
  shown), or slicer refuses the read-back after the set, for example a pin of
  v10 on a slicer that serves v11. In that case the pin stays set, and the
  message names the project, quotes slicer's error and gives the next step,
  `ork projects pin NAME --relay N` with the version slicer serves or `ork
  projects unpin NAME` (owner, 2026-10-31). `pin` also exits 1 when slicer
  prints no pin after a successful set.
- `settings show` and `settings set [--global-cap N] [--daemon-idle-minutes N]`
  (*proposed*, S174, owner decision 2026-10-29): the file-level registry
  settings, over `settings.get` and `settings.set`. `show` prints
  `global_cap=VALUE (effective N)` and `daemon_idle_minutes=VALUE (effective
  N)`, with `unset` for a stored 0 and the effective value 4 and 10 when
  unset. `set` needs at least one flag; 0 clears one; a negative number, a
  non-number, an unknown subcommand or flag and no flag are usage errors
  (exit 2) before the daemon starts. Text: `updated settings: FIELD=VALUE,
  ...` with the stored value (`unset` for 0). JSON: `{"settings":{...}}` for
  both, with the keys of `settings.get`. Unlike `projects set`, `settings set`
  is not refused while runs are unfinished: the global cap is read for every
  admission and the idle minutes each time the idle clock arms. Raising the
  cap starts queued runs that now fit, with no other event; lowering it
  stops no running run. The per-vendor cap (S169) is built in and is not a
  setting; `ork vendor` shows and sets the vendor memory.
- `roles print [ROLE]`: `{"roles":[{"name","prompt"}]}`. Text: the prompt of
  ROLE as embedded, or all four with a `==== NAME ====` line before each.
- `roles matrix PROJECT` (S168): the `roles.matrix` result as one value. Text:
  slicer's matrix text, a blank line, `## ork`, then one line per row,
  `- ID ROLE: USE`. It reads through the daemon (`print` does not) and writes
  nothing; the matrix is slicer's and stays so. An unknown project is
  `not_found` (exit 1); a missing or extra argument is a usage error (exit 2).
  The Gate rows are marked unused because the gate follows the reviewer's
  lineup (S83). The TUI pane shows the same lines with the `## ork` section
  first, then slicer's text (owner, 2026-10-29).
- `doctor`: `{"ok","tools":[{"name","required","present","path","version",
  "ok","error"}],"contract":{"accepted":[10,11],"found","ok","error",
  "projects":[{"project","pinned","found","ok","error"}]},"binary",
  "projects":[{"name","path","findings":[]}],"registry_error"?}`,
  where `binary` is the path of the running ork binary (empty when it cannot
  be read). Text: one `[ok]`, `[--]`, `[WARN]` or `[FAIL]` line per check, and
  the first line is `[ok] ork  PATH`, or `[--] ork  path unknown: ERROR`
  when the path cannot be read. In `contract`, `found` is the first line of
  `slicer ai relay` for the directory doctor runs in ("Contract: relay vN"),
  and `ok` is false when the global check or any project entry fails. Each
  `contract.projects` entry is a registered project asked with its own
  `--root`: `pinned` is the pin or null when the project is unpinned or when
  slicer refused (the `error` then names the pinned and the served version),
  and `found` is the bare contract ("relay vN"). A failed project's text line
  ends with ork's next step, `run ork projects pin NAME --relay N ... or ork
  projects unpin NAME`, for a refused pin or a pin outside the accepted set.
  Projects have one `project NAME  PATH` line each, with
  findings indented below. Every finding is stored on one line.

## Testing

`testing.strategy`:
- The state machine runs against fake `claude`, `codex` and `grok`
  executables that replay fixture JSON: success, FAIL verdict,
  rate-limited, auth error, a crash, and a read-only role that edits.
- The slicer and adj adapters are tested against recorded CLI output,
  refreshed per tool release.
- TUI models are tested as pure update functions.
- Real runs are covered by a manual checklist per vendor.

## Build order (proposed)

Each step is usable or testable on its own:

1. **Vendor spike.** Fill in the capability table with real runs on a
   throwaway repo. Capture fixtures.
2. **Core.** Registry, slicer and adj adapters, read-only dashboard
   (Overview, Roadmap, Decisions, external relays). Cut into three
   slices (`plan.s02-cut`): S02 core and adapters, S11 TUI shell and
   Overview, S12 Roadmap and Decisions. Before the daemon, the TUI calls
   the adapters directly behind one interface (`dash.pre-daemon`) and
   polls them (`dash.refresh`).
3. **Daemon, socket and CLI.** `ork status`.
4. **Engine for a single implementer.** setup → implement → checks →
   review → gate → approve → commit, on one vendor, with the ledger.
   Cut into three slices (`plan.s04-cut`): S04 engine core on a fake role
   runner, S16 the Claude adapter, and S17 the CLI to run, approve, answer
   and stop one slice (`engine.owner-surface`). A role left running by a
   crashed daemon is marked lost and its slice paused (`engine.restart`).
   An implementer with no registry allowlist gets a narrow built-in one
   (`launch.default-allowlist`).
5. **Waves and caps.** Several items, queueing, failure isolation.
6. **Three vendors, fallback, fingerprint, budgets, owner questions.**
7. **Live role detail.** S07 adds stage bars, a stream-event sparkline and
   4 fps animation while a run is visible. S99 adds live assistant output and
   usage detail.
8. **Architect on size L, split, slivers.**
9. **Landing help.** Ready-to-land list and merge detection.

### Relay board information (S97, proposed)

Running rows and the run pane show slicer's relay board information
(`slicer/docs/tui.md:183-270`). The compact row pipeline marks the running
role in brackets and adds last heard beside the bar with `!` after 10 minutes and `!!`
after 30 minutes (the stale cutoff) or for stale runs. Stale runs stay visible and freeze elapsed time at their
last event. The pane keeps the S55 tree and adds the colored role pipeline,
implementer round count, last heard, waiting roles, repeated role counts,
and failed-gate reasons. Engine-only panes derive these from role records.

The proposed `source.Snapshot.BoardRuns` and snapshot wire `board_runs`
carry stale and recently ended ledger runs. A second `RelayRuns` read uses
`active=false`; the active read and `ork status` output stay unchanged.
This read is not a new cached Part. Direct snapshots and daemon snapshot
requests read it separately; status makes no board read. The adapter bounds the payload to stale runs
and ended runs updated within an hour of read time. Recent applies the
exact hour cutoff with the injected render clock, sorts newest first,
and sits between Running and Ready to land in the cursor order. It shows
outcome, items, wall time, ended age, and role counts. Enter opens
a read-only ended-run pane. Run duration and ended age use `begun_at` and
`updated_at`; the ledger has no ended timestamp. The first duration is wall
time, including waits. The parenthesised figure is active time: the union
of `begun_at` to `updated_at` intervals for done or failed implementer,
reviewer, gate and architect steps. A completed step's `updated_at` is its
end time. Overlaps count once; orchestrator and still-running steps are
excluded. Wall time appears alone when no completed worker step has usable
times. Recent counts all recorded
steps of each role, including nested steps and repeated rounds. Review and
gate show a dash when no step of that role is recorded, also for relays
started elsewhere. The dash makes no claim about unrecorded work.

### Live role activity (S07)

The Overview puts the ASCII stage bar after elapsed time. Narrow rows drop
tokens, pipeline, role details, heard, then the bar. The pane puts the bar above the
pipeline, with last heard on the bar's line, and the ten-cell activity sparkline below. The bar measures
completed roles, with an architect cell when present; a fix round resets roles
whose latest steps precede the new implementer step. A running cell pulses
through `~~`, `~=`, `==`, `=~`; stopped, paused, stale and ended runs freeze at
`~~` and omit activity. Sparkline levels scale to the window maximum, using
blocks in UTF-8 locales and `._-=+#` otherwise. An all-zero window is flat.

A separate `Config.FrameAfter` seam sends `frameMsg` every 250 ms while an
Overview Running row, live ledger row or live run pane is visible. One timer
is armed at a time and the next frame disarms it when nothing qualifies.
`treeFrame` advances only on frames. The one-second schedule ticker is
unchanged. There is no S07 bell, budget bar or write action.


## Self-hosting (engine.self-host, adj 166)

The daemon keeps running the binary it started from. Relay worktree builds
never replace it. A project is self-hosted when the daemon's VCS build
revision is a commit in that repository (`git cat-file -e REV^{commit}`).
Development builds without a revision and unknown revisions do not match.
There is no registry setting for self-hosting.

A wave may contain at most one slice whose Files touch `internal/engine`,
`internal/daemon` or `cmd`. The daemon refuses a conflicting wave before
starting any member and names both slices. Waves marks the conflict before
launch. One such slice may run with other slices.

*Proposed wire fields:* each status project carries `self_host` and
`behind_main`. A self-hosted daemon is behind when its build revision differs
from local `main` and is an ancestor of that branch. A missing branch or a
failed Git read does not establish that it is behind.

*Proposed behavior and wording:* `daemon behind main: restart with ork daemon restart`.
It fires on any landing on main, not only engine landings, while no run is
active. In the status line it replaces the stale-build marker while shown;
otherwise the stale marker still shows for a differing build. Text doctor
prints `[WARN] daemon behind main: restart with ork daemon restart` on stdout.
Status prints `ork: warning: daemon behind main: restart with ork daemon restart`
on stderr. ork never restarts the daemon on its own.

### Live role output and usage (S99)

`Request.OnEvent func()` still counts stream events. Its sibling
`OnLive func(Live)` carries assistant text and token increments. The engine
keeps a UTF-8-safe 2048-byte tail and a token counter beside the activity
ring under the same mutex. Hooks capture the attempt generation; old hooks
cannot update a newer attempt. Each attempt resets the data; after it ends
the live read is empty. Nothing in this buffer is written to the run record
or report files.

The daemon adapter masks qualifying run-environment values before invoking
OnLive (the rule is in dashboard.md). It buffers possible secret prefixes
across text events, masking an unfinished prefix at final flush. Claude
message-start usage establishes the new message's
input, output and cache counts. Message-delta usage replaces cumulative
counts for that message, emitting only the difference. The measured stream
fixture's final message total and result total both equal 23763 tokens;
message-start usage is provisional and must not be added a second time.
Codex assistant text comes from completed agent-message items. Its usage
arrives at turn completion, with input plus output tokens. The measured
Codex fixtures contain no per-item usage to compare with the turn total.

Final Result tokens and cost still populate RoleRecord. The proposed
`engine_runs[].tail` is transient text; `tokens` sums recorded roles and live
usage, and `cost_usd` sums recorded costs only. Proposed role `tokens` and
`cost_usd` display fields carry recorded values. The pane joins engine roles
to linked ledger nodes by step ID, retaining the ledger's tree. Source-less
ledger runs have no engine output or usage.

The proposed snapshot flags `usage_known` and `live_usage_known` distinguish
available run usage from current-attempt usage, including reported zero.
The live hook marks a usage event with `Live.UsageKnown`; text alone does
not mark usage. The proposed `tail_truncated` flag records memory-cap cuts
so the pane can mark the first wrapped tail row. These flags stay in memory
and add no persisted record fields. Vendor identity determines whether a
recorded cost is available: Claude has a cost field; Codex does not.
