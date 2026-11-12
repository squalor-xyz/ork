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
  | `snapshot` | `{"project"}` | the project's `source.Snapshot` (`items`, `deps`, `goals`, `runs`, `open`, `decided`) from the cache, plus the daemon's `engine_runs` array and *proposed* `board_runs` (neither is a Part); *proposed (S103)* `daemon_build` is the daemon's build ID, omitted when empty, for refreshing the TUI stale-build marker; each part is `{"value", "age_ms", "read_at"?, "stale"?, "error"?}`: the last good value, its age in ms (0 if never read), when it was read (absent if never), and the last read's error |
  | `item.park` (proposed wire, S67) | `{"project","item"}` | `{"item"}`; slicer park at the registered root, then refresh Items and Deps; an unfinished run on the item is `refused`, naming the run |
  | `item.unpark` (proposed wire, S67) | `{"project","item"}` | `{"item"}`; slicer unpark at the registered root, then refresh Items and Deps; runs do not block unpark |
  | `item.move` (proposed wire, S67) | `{"project","item","other","position"}` | `{"item","changed"}`; position is `before` or `after`; compare the item's index in slicer list before and after the write, then refresh Items and Deps; blank fields or an invalid position are `bad_params`; slicer errors keep code and message |
  | `item.note` (proposed wire, S171) | `{"project","item","text"}` | `{"item"}`; slicer note at the registered project root, then refresh Items; a failed refresh after the write is not an error; blank item or text is `bad_params`; slicer errors keep their code |
  | `item.show` (proposed wire) | `{"project","id"}` | `{"detail"}`; slicer show for the registered project |
  | `wave.start` (`run.launch-wire`; proposed shape) | `{"items":[RunParams...]}`; each item has `project`, `item`, `well_specified`, `split`, optional `lineup`, `budgets`, `expected` and, from S265 (*proposed*, adj 191), `allow_no_check` as in `run.start` | `{"wave","items":[{"item","run"?,"error"?}]}`; one wave id, one project, admission errors kept per item; a project with no check command refuses the whole wave with `refused`, before any member is admitted, unless every item has `allow_no_check`; *proposed (S215):* an optional `"name"` starts a saved wave: the project must have a wave of that name (else `not_found`) and every item must be one of its members (else `bad_params`), both checked before any read of slicer or admission; the name is stamped on each run record as `wave_name` and returned as `"name"`, and the wave id is still a fresh one |
  | `wave.define` (proposed wire, S215) | `{"project","name","items":[ID...],"replace"?}` | the saved wave `{"project","name","items","created_at","updated_at"}`; the project must be registered (`not_found`, with the registry guide text); the name and items follow the rules in docs/state.md (`bad_params`) and every item must be listed by `slicer list --all` (`bad_params` names the unknown one); a name the project already has is `duplicate_name` unless `replace` is true; written to `waves.json` under a lock of its own |
  | `wave.list` (proposed wire, S215) | `{"project"?}` | `{"waves":[{"project","name","items":[{"item","status","title"}],"created_at","updated_at","error"?}]}` in the order saved; `status` and `title` come from one `slicer list --all` for each project that has a wave to show, and a member that list lacks has status `missing`; a project whose slicer read fails (or that is no longer registered) puts `error` on its waves and the members carry no status, and the other projects still list; an unknown `project` filter is `not_found`; slicer runs on each call; nothing is written |
  | `wave.rm` (proposed wire, S215) | `{"project","name"}` | the saved wave that was removed; `not_found` when the project has no wave of that name |
  | `wave.stop` (proposed wire) | `{"wave"}` | the same wave result; stops each unfinished member through `run.stop`, skips final members |
  | `wave.suggest` (proposed wire) | `{"project"}` | slicer's `plan-parallel` result for the registered project, limited to the fields `ParallelPlan` reads, with a second engine, daemon or cmd slice of a wave moved into a wave of its own after it (S218, *proposed*; see Self-hosting); slicer runs on each call, not from the cache |
  | `roles.matrix` (proposed wire, S168) | `{"project"}` | `{"markdown","rows":[{"id","role","agent","vendor","model","reasoning","note","use"}],"rules":[{"id","text"}]}` from `slicer relay-roles show --json`: slicer's own text (*proposed*, S173: `vendor`, `model`, `reasoning` and `note` are omitted when empty), and for each row what ork does with it (`use`: `used as claude\|codex\|grok`, `UI pass row, ...`, `Spec check row, ...` (every row that the project's `spec_check_rows` or a rule names), `skipped: no adapter for AGENT`, `not used: the gate runs the reviewer's lineup`, `not run by ork`, `not used: ork has no role ROLE`); read-only, no slicer write; slicer's error keeps its code and message; slicer runs on each call |
  | `matrix.edit` (*proposed* wire, S173) | `{"project","op","id"?,"fields"?,"before"?,"last"?,"text"?,"preview"?}`; `op` is `add`, `set`, `remove`, `move` or `rule-edit`; `fields` maps a flag name (`role size effort importance urgency well-specified split sliver agent vendor model reasoning note`) to a text value for `add` and `set`: an absent name is not passed, an empty value clears the field; `move` takes exactly one of `before` and `last`; `rule-edit` takes `id` and `text` | `{"command","warning"?,"id"?,"done"}`; previews (`preview`, `done` false) or runs one `slicer relay-roles` write in the registered project's main checkout with `--render`, with no run record; the change is left uncommitted for the owner, and slicer owns the file; `command` is the line the owner confirms, quoted for a shell; slicer's refusal comes back with its code and message; `warning` when `fields.agent` matches no adapter (`agent "X" has no ork adapter: a plan skips this row`), and the edit still runs; `id` is the new row after `add`; a missing project, an unknown op, a missing id, a field name outside the list, `fields` on `remove`, `move` or `rule-edit`, a blank text or a text that starts with `-` and has no space (slicer's parser would read it as a flag) is `bad_params`, with no slicer call; no cache refresh, because `roles.matrix` reads slicer on each call |
  | `wave.overlap` (proposed wire) | `{"project","items":[ID...]}` | `{"overlap":[{"a","b","shared"?}]}` from `slicer deps --overlap`; `shared` is absent when a pair shares no path; fewer than two items is `bad_params`; slicer runs on each call |
  | `subscribe` | `{"focus":[names]}` | none; the connection gets notices (below) and the named projects become visible |
  | `run.landcheck` (*proposed*, S217) | `{"run"}` | `{"base"?,"blockers"?,"reason"?,"error"?}`; current advisory landing check; absent fields are empty |
  | `refresh` | `{"project"}` | none; reads every part of the project now and returns once they are read |
  | `projects.list` | none | `{"projects":[...]}`; an empty registry is valid |
  | `projects.add` | `{"path","name"?}` | the added project; the first add creates the registry |
  | `projects.rm` | `{"name"}` | none; *proposed* `project_busy` refuses unfinished runs |
  | `projects.set` (*proposed*, S161) | `{"name", ...}` plus at least one of `push`, `bell` (a boolean, S224, *proposed*), `ui_doc`, `ui_pass_skills` (a list, S209), `spec_check_rows` (a list, S258), `worktree_pattern`, `branch_pattern`, `check_command`, `check_minutes`, `cap`, `budget_minutes`, `budget_tokens` and, from S174, `environment_lines` (a list), `permissions` (`{ROLE: "read-only" or [patterns] or null}`) and `vendor_env` (`{VENDOR: {KEY: "value" or null}}`), and, from S265 (*proposed*), `implementer_preset` (`go` or `python`) and `allow_add` (`{ROLE: [patterns]}`) | the project as saved; an omitted field keeps its value, `""` or 0 clears one, `[]` clears `ui_pass_skills` or `spec_check_rows`; `null` removes one role or one vendor key, and the other roles and keys stay; `allow_add` appends the patterns not yet in the role's list (from the built-in default when the role has no entry) and `implementer_preset` sets the implementer's list first, so `allow_add` in the same call follows it; `bad_params` for an unknown field, no field, an invalid value, an unknown preset, or `permissions` naming a role that `allow_add` or `implementer_preset` names; `not_found`; `project_busy` while a run is unfinished and the call sets a field other than `cap`, `bell`, `budget_minutes`, `budget_tokens`, `worktree_pattern` or `branch_pattern` (S265, S224, *proposed*; the message names the refused fields as command line flags, for example `--push`); a call that sets `cap` also starts the queued runs that now fit, as `settings.set` does |
  | `settings.get` (*proposed*, S174) | none | `{"global_cap","daemon_idle_minutes","effective_global_cap","effective_daemon_idle_minutes","owner"?,"file_answers"}`; the first two are the stored values (0 is unset); from S314 (*proposed*) `owner` is the stored name (absent when unset) and `file_answers` is true unless the registry turns it off |
  | `settings.set` (*proposed*, S174) | `{"global_cap"?,"daemon_idle_minutes"?,"owner"?,"file_answers"?}`, at least one | the same shape as `settings.get`; 0 clears one, `""` clears `owner` and `true` clears `file_answers` (on is the default and is not written); `bad_params` for no field, a negative number or an owner that is not one line; not refused while runs are unfinished; a raised `global_cap` starts the queued runs that now fit |

  Error codes the daemon adds: `unknown_method`, `bad_params`, `not_found`,
  `duplicate_name`, `invalid_path`, `registry`, *proposed* `project_busy`, *proposed*
  `refused` (an owner action the engine refused) and *proposed* `no_spec_check_row`
  (S258: a spec check with no row to run on; the message gives the fix command).
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
  records. The runs part is read every 2 s instead while the project is
  visible and its last good read holds a run that is not stale (*proposed*,
  S109; 2 s since S336, adj 301); when that read shows no such run it returns to 5 s, and a project that
  is not visible stays at 30 s. The interval belongs to each project, so the
  Running entry, which makes every project visible, reads the runs of each
  project with a live run every 2 s and no other part faster (*proposed*,
  S110); the 4-process limit holds however many projects that is. Only the runs part speeds up; a change in it
  reaches the clients through the usual notice. Each read is one CLI call. A read that is already running is
  shared, and at most 4 CLI processes run at once across all projects. A
  read that comes due, or that a HEAD change asks for, while another runs for
  its part starts when that one ends, so a part never has two reads at once. A
  failed read keeps the last good value, with its error and the time it was
  read; a `corrupt`, `locked` or timeout failure is tried once more at once.
  The cache keeps no files and watches none. *Proposed* (S336, `poll.idle-backoff`, adj 301): while no TUI is attached (no connection has called `subscribe`), the cache reads nothing on a timer, the git HEAD read included; a read that a client asks for (a snapshot, `refresh`, the refresh after a write) still runs once. When a TUI attaches, each part of the projects it shows that is due is read at once (after an idle longer than a part's interval, that is every part; a part read within its interval waits for it), goals and decided records are read for a project newly shown, and the other projects are read an interval later. Merge detection (`checkMerged`) rides on the HEAD read, so with no TUI attached it waits (owner decision 2026-11-11): merged-run state, the removal of merged spec directories and run pruning happen when a TUI attaches and the HEAD read runs.

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
   *Proposed (S215):* a wave may be named. The owner saves a set of items of
   one project under a name (`ork wave define`, `wave.define`; kept in
   `waves.json`, see docs/state.md) and starts it by name later (`ork wave
   start NAME`). A saved wave holds only the name and the item ids; status is
   read from slicer on each use, and a start leaves out the members that are
   done, retired or gone from slicer (`SavedWaveView.Startable`). Starting it is
   the ordinary start of the remaining items, with the name stamped on each run
   record (`wave_name`); each start still gets a fresh wave id, which `ork stop`
   takes. The name is a label, and a saved wave is not a claim on its items.
2. **Lineup.** For each item, ork runs `slicer relay-roles recommend ID
   --well-specified ... --split ...`; ork passes well specified = yes and
   split = no unless the owner overrides them with `--well-specified` or
   `--split` (*proposed*, adj 189, `launch.well-specified-default`; the
   spec check holds an item that is not Ready). The primary row per role becomes the
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
   needs a model, effort and a runnable vendor (`claude`, `codex` or `grok`). The gate copies the reviewer's chain; a gate
   override is refused. Without an override, the plan keeps every row with
   an adapter in matrix order: matrix agent `Claude Code` maps to `claude`,
   `Codex` maps to `codex` and `Grok` maps to `grok`, matched without regard to
   case (`engine.adapter-key`, decision 155; the `Grok` key is S72, *proposed*).
   A Grok row is an alternate: nothing makes it a first choice, so it comes
   first only where the matrix recommends it.
   *Proposed (S268; owner 2026-11-03):* Grok has no write mode (probes 10 to 12
   in `testdata/vendors/grok`), so a plan skips a `grok` or `grok-local` row of
   the implementer, unless the project's implementer permission is the
   `read-only` preset, and adds `skipped: row ID (AGENT) as implementer: grok
   has no write mode` to the plan's note. When no other row is left, the plan
   is refused with the same reasons. A launch override that names `grok` or
   `grok-local` as the implementer is refused with `bad_params`. The matrix view
   shows such a row as `skipped: grok has no write mode`. Reviewer, gate,
   architect, UI pass and spec check rows are read-only and are kept.
   *Proposed (S244; owner 2026-11-02):* a matrix model `FAMILY@latest` asks for
   the newest model of a family. FAMILY is a model name with one `*` for the
   version (digits and dots), for example `gpt-*-sol@latest` or
   `grok-*@latest`. When it builds the plan, the daemon resolves the text to the
   matching name with the highest version, compared number by number (`4.10` is
   newer than `4.9`). Names that do not match the whole family
   (`grok-4.7-build-fast` against `grok-*`) are ignored. The names come from the
   vendor's list: `codex debug models --bundled` for codex (the `slug` of each
   model) and `grok models` for grok (the first word after each bullet). The
   daemon keeps each list it read until it restarts (a read that failed is not
   kept, so the next plan tries again). For claude, the
   aliases `opus@latest`, `sonnet@latest` and `haiku@latest` resolve to the
   alias with no list call, because the claude CLI resolves the alias. A model
   without `@latest` is a pinned name and is passed unchanged. The lineup entry
   holds both texts: `model` is the resolved name, which the engine, the
   adapters, the run record and the slicer ledger get, and `requested` is the
   text the row gave (left out for a pinned model). The bare model `latest` is
   refused: `model "latest" names no family; write FAMILY@latest, for example
   grok-*@latest`. A list that cannot be read refuses the plan with the
   command, its exit and the family; a list that has no match refuses it with the
   family. A launch override is resolved the
   same way, after it replaces the role's rows, so overriding the role gets the
   owner past a refused family. A resumed or retried round uses the recorded
   `model`, so a run never changes model halfway. A local row (agent
   `claude-local`, `codex-local` or `grok-local`, or a vendor column of `Local`)
   is never resolved: its model is passed as written, `@latest` on it is
   refused because local models are pinned, and no list command runs for it.
   The same resolution gives the spec check chain its model. The test fixtures
   are `testdata/vendors/codex/models-bundled.json` (the output of `codex debug
   models --bundled`, without the `model_messages` and `base_instructions` prompt
   fields of each model, which the owner removed) and
   `testdata/vendors/grok/models.txt`. *Proposed (S245):* a lineup entry with
   `requested` set prints `latest (resolves to MODEL)` in place of the model
   (`runrec.ModelLabel`) in the plan text, the launch pane and the lineup
   editor; the narrow Waves lineup column prints `MODEL (latest)` (adj 273); a
   pinned model prints its name. The editor's model field starts from the
   requested text, so an edit asks for the latest again.
   Grok runs read-only roles only; see the Grok column below.
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
   owner structured questions (S111 shape, with an optional decision key).
   *Proposed* (S292): its read-only fingerprint uses
   `git -C PROJECT status --porcelain -- .`. It covers paths under the project
   root, including its own `.slicer/` tracking files visible to git status.
   A sibling project in the same repository can change its tracking files
   without failing this check. The spec check takes no lock. A status change
   inside the project during the role still fails the read-only check; the
   existing whole-repository HEAD check and retry rules stay in place.
   A ready slice's Failing tests name their assertions and its Check gives a
   sandbox-runnable command and expected result, or a `[HUMAN CHECK]` saying what
   the owner looks at and should see; criteria gaps that reading the slice, Files
   and `AGENTS.md` cannot settle get one question per slice, proposing concrete
   wording to tighten them, a human check, or acceptance as written, without a
   decision key. The owner answers them in one sitting. ork appends an
   `Owner decisions (date)`
   paragraph to the slice's Implement section with `slicer edit --append`, and
   files a question that has a decision key in adj the way a run's answer is
   filed (`owner.questions`, S28). *Proposed (S314; owner decisions
   owner.setting-command, owner.setting-store, adj 228 to 230):* the daemon
   files as the owner it resolves in one place, `engine.ResolveOwner`: the
   caller's `ADJ_DECIDED_BY` when set, else the registry's `owner` (set with
   `ork settings set --owner NAME`). Filing is on whenever the project has adj
   and an owner resolves; `ork settings set --file-answers off` stops it. Only
   an answer that is one of the question's options (`Label: summary`) is filed;
   free text is recorded in the slice and not filed. With no owner, nothing is
   filed and one notice for the item names the keys and says `no owner set; run
   ork settings set --owner NAME`. A filed answer says `filed in adj: KEY (adj
   N)` and its `--why` names the date and the slice. The paragraph is also stored in the item's
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
6. **Run.** Items start as slots free up under the global cap (default 2, `engine.default-cap`, adj 172)
   and the per-project cap (`wave.limits`) for frontier-primary runs. The
   first implementer row in the admitted plan fixes the cap class for the
   run's lifetime (S301, owner answer 2026-11-09, PRIMARY). A local-primary
   run instead reserves one slot in the shared local pool of four, taking
   no global or project slot (adj 192). This reservation covers cloud
   fallback, review and gate rounds, pauses and approval waits. Its local
   roles use that reservation one at a time (RESERVED-ONE); cloud roles
   still obey their cloud vendor caps. A fifth local run stays queued until
   a local slot is free. Frontier-primary runs retain their cap class even
   when they fall back to a local role; that role takes an ordinary shared
   local slot. A stop or committed result releases the lifetime reservation.
   Daemon shutdown joins roles before freeing reservations, and recovery
   restores unfinished local reservations before admitting queued work.
   Several waves,
   each in one project, may run at once (`wave.scope`). An item over a cap
   waits as a queued run and starts, oldest first, as a slot frees.
   Only a busy frontier run holds a frontier slot, so it holds none while
   paused and other frontier runs may start (adj 122). The owner's answer or
   retry of a paused frontier run then needs a slot under the project cap and the global cap
   (S274, *proposed*; adj 177). With room it starts at once. With none, ork stores the request in
   the run's record (`resume`), replies at once that the resume waits for a
   slot, and leaves the run `paused`; status says `resume waits for a slot under
   project cap N; next: ork stop ID` (or `under global cap N`). An answer filed in adj is filed at
   once, not when the run starts; only the engine resume waits. Other owner
   actions on the run are refused as busy while the adj calls run, and a stop
   waits for them and then stops the run; the daemon never starts a second job
   beside the filing. The run is not
   waiting for the owner: snapshots carry `resume_waiting`, and the TUI lists it
   under Running, not Needs you or Questions (`docs/dashboard.md`). A freed slot starts the waiting resumes
   first, oldest request first, and then the queued runs. A second answer or
   retry, of either kind, is refused while one waits; `ork stop` ends the run
   and clears the request. A restart keeps it. Approve and stop still bypass
   frontier caps; a local resume or approval reuses its held reservation.
   Beside the run caps there is a vendor cap (S169, *proposed*, owner decision
   2026-10-29): one `codex` role at a time over all runs of the daemon, and
   `claude` unlimited. It is built in; the registry has no field for it. It
   is enforced per role round, not per run (see Vendors, below). A run that
   waits for the Codex slot keeps its global-cap slot, because the wait lasts
   at most one role round. Cloud spec-check rounds take no vendor slot;
   local spec checks use the same shared local pool as local reservations.
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
                        | fix (owner's note)      | reject        | red re-check: one fix round,
                        v                         v               v then back to implement
                implement (fix round) -> checks -> gate      stopped  (a second red: stopped)
                        (back to awaiting-approval on PASS)
                                                  land --merge conflict--> paused (owner resolves, ork retry)
```

Any state can go to `stopped` on a stop rule (below), or to `paused` on a
budget limit or an owner question. The owner can resume or stop a paused
slice.

- **setup:**
  - Only when the worktree path is absent, commit uncommitted edits to this item's
    `.slicer/slices/ID.json`, `.slicer/render/slices/ID.md` and
    `.slicer/index.json` on main by literal path, with the message
    `Tracking edits on main before starting ID` (S330, `launch.tracking-commit`,
    owner scope: item-only). When committing the index, also call
    `slicer list --json --lean` through the slicer client and include each
    listed item's untracked slice JSON and render when `has_slice` is true
    (S347, adj 321). This keeps the committed index from listing slice files
    left uncommitted on main. Edits to other items' already tracked files,
    unlisted files, configuration and relay roles stay uncommitted, including
    staged edits. Clean main needs no commit. A status, prefix, list, untracked
    file read, add or commit error pauses setup with one line before creating the
    worktree. An existing worktree directory skips this commit, including on
    a retry after setup already created it.
  - `git -C P worktree add <worktree_pattern> -b <branch_pattern>`
    (`git.worktrees`);
  - `.worktrees/` is added to `info/exclude` if it is missing;
  - `slicer relay-run begin ID --worktree W` (`ledger.write`);
  - *proposed (S295):* once the worktree and the ledger run exist, ork verifies
    the check command (and the round check command) in the directory the round
    check will use (the project's folder of the worktree, S293), before any
    role starts. The verification is static and runs nothing but `sh -n`: the
    first word of each simple command resolves, and each later word that is a
    plain path (see `projects set`) exists in that directory. A command that
    cannot run pauses the run with `check command cannot run in DIR: PROBLEM
    (command: CMD); to fix the check: ork stop ID, ork projects set PROJECT
    --check CMD, then ork retry ID` (`--round-check` in place of `--check` when
    the round command is the one that fails; error pause, `pending.state`
    `implement`, so a retry does not begin the ledger run again). DIR is named
    once: the problem calls it "the check directory". The wait line's `next:`
    is `ork stop ID`, not `ork retry ID`, because a plain retry verifies the
    same command again. A plain `ork retry ID`
    verifies again with the engine's own command, which helps only when the fix
    is a file added in the worktree; `ork projects set` is refused while the run
    is paused, so the owner stops the run first and `ork retry ID` resumes the
    stopped run (S296) with the fixed command. A wave's other runs go on. The
    full check is not run at setup (owner decision 2026-11-05). A project whose
    check was stored with `projects set --no-verify` (`no_verify_check`) is not
    verified at setup or in the implement step;
  - the implementer runs `slicer start` (the contract says the implementer
    starts the item).
- **implement:** one implementer, or a lead and parts in a split
  (`slice.options`). Slivers run as their own items, under the cap.
- **checks:** ork runs the project check command and `slicer check --json`
  in the worktree (`checks.deterministic`). A red result goes back to the
  implementer, with the failing output in the brief, without spending a
  review. The check command comes from `slicer config` once slicer has the
  key; until then it comes from the registry (`project.check-command`).
  *Proposed (S285):* a project may also set `round_check_command`, a faster
  command. The checks after implementer rounds run it in place of the check
  command. When the round command is set and differs from the check command,
  the check command runs after a green round check, once, before review or
  the gate (a red result there sends a fix round, as a red round check does).
  The landing re-check always runs the check command. With the key unset, or
  equal to the check command, the check command runs everywhere and no second
  run follows a round check. `projects set` refuses the key only while the
  project would have no check command, so `--round-check` alone is valid once
  `check_command` is saved. Each run is its own check row with its own
  command, and the full run's log is `checks-full-<round>.log`, so the
  no-progress test, which reads `checks-<round>.log`, sees round checks only.
  `ork status` names the command of the newest check row (`last check:`).
  *Proposed (S239):* when the project command exits non-zero, ork runs it once
  more, with the same directory and timeout, before it spends a fix round on
  it. A pass on the rerun is a flaky pass: the project command counts as
  green (the round is still red when `slicer check` is red) and `checks[]`
  records `attempts` 2 and `flaky` true, so `flaky` true with `ok` false is
  possible. A timeout is not run
  again, `slicer check` never is, and a rerun that cannot run pauses like the
  first run. If the rerun is red too, the brief for the fix round starts with
  the failing test names and their assertion lines (at most 8000 bytes, from
  `go test` text output: `--- FAIL:` lines with the lines under them (with
  `go test -v`, the lines before them since the test's `=== RUN` line), package
  `FAIL` lines, a panic, a data race report and build errors), then the end
  of the log (30 KB) as before. Output that is not Go test output shows only
  the end of the log, as before. The landing re-check follows the same rule.
  *Proposed (S279, adj #178, `checks.outside-problems`):* a red `slicer check`
  does not make a round check red when the branch is behind its base branch
  (the base is not an ancestor of the worktree's HEAD) and every problem names
  a tracked item other than the run's own. A problem names an item when it
  reads `ITEM: message` or `ITEM / SECTION: message`; the item compares
  without regard to case. A problem with no item, a problem that is not a
  string, a non-empty `stale_render`, `orphan_render` or `stale_sync`, a git
  error and a branch that is up to date each keep the round red. The project
  check command is judged as before. When ork defers, it writes one
  `deferred to land (branch behind BASE): PROBLEM` line per problem to the
  check log after the slicer output, records the problems in
  `checks[].deferred`, and adds a "Deferred tracking problems" section to the
  reviewer and gate briefs that says the landing merge and its re-check decide
  them and asks the role to name them as deferred, not as findings. The finish
  step's brief carries the same list and says its `--check` may be red on
  exactly those problems, which is expected. Only the round checks defer. The
  landing re-check, which runs after the base is merged in, never does, so a
  problem that stays red there gets the landing's one implementer fix round.
  A run that stops after the commit (`StopAfterCommit`, state `committed`) has
  no landing in ork: the owner's own landing, with its merge and `slicer
  check`, decides the deferred problems then. ork writes no git during the
  rounds.
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
    Skills (*proposed*, S209, adj 171): `ui_pass_skills` in the project
    registry is a list of names. The engine passes the selected lineup
    vendor to the brief builder. Only a `claude` UI pass with a non-empty
    list gets a `## Skills` section just before `## UI design doc`.
    It advises the pass to load the named skills and, if one is missing,
    report that and go on. A `codex` UI pass
    never gets that line. The role prompt stays vendor-neutral and skill-free.
    An absent or empty list adds no line. ork does not check skill existence
    and never reads vendor files.
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
    `Options:` with 2 to 4 numbered labels, `Recommend: N - reason` (the dash
    may be a hyphen, an en dash or an em dash; S132, *proposed*), and an
    optional `Decision key:`. The prompts say the body holds only those lines,
    with no list marker before `Options:`, `Recommend:` or `Decision key:`.
    The parser is lenient (S288, *proposed*): it strips one leading `- `, `* `
    or `+ ` marker from `Options:`, the option lines and `Recommend:`, and
    every other body line stays in the finding's `text` as context. A
    non-blocking finding keeps that context in the run record only; a
    blocker's question carries the context lines above its block. A body
    line between two numbered options, a numbered line that is not a valid
    option, an option or numbered line after `Recommend:`, a repeated
    `Options:` or `Recommend:`, a `Recommend:` before `Options:`, a
    `Decision key:` line behind a list marker, and a body without `Options:`
    or `Recommend:` are still refused. The `## Owner needed` block and the
    spec check's question blocks are read the same way (owner decision
    2026-11-07). ork keeps the finding in `findings[]` of the run record and
    shows it in the pane. It does not block. `[owner blocker]`
    makes ork pause the round with a `question` pending, as an `## Owner needed`
    section does (`questions[]`, the answer reaches the same role, and
    `reviewDone` runs that role again). A malformed owner or later finding is
    a report shape error. Proposed (S137): every blocker is asked, with
    remaining questions persisted in `queued_questions`; the role reruns
    after all answers. Repeating an answered blocker pauses with an error
    instead of asking again. The resumed role receives all its recorded answers.
    A non-blocking finding can be answered from the pane without pausing or
    advancing the run; its answer and timestamp stay on the finding, and a
    decision key also records the answer in adj when the project has adj.
    *Proposed (S223):* a non-blocking owner finding is also asked in the
    relay ledger at once, with `slicer relay-run ask` and no `--blocker`, so
    the orchestrator step keeps running; the finding keeps the question's id
    (`ledger_question`). The owner's answer in ork is written to that question
    (`relay-run answer RUN QID --text ... --by ...`); an answer given while the
    ask was still in flight is written as soon as the id is stored. ork reads the question
    (`relay-run answer RUN QID --get`) before it builds the approval card and
    again before the finish step; an answer given in the ledger is recorded on
    the finding (`answer`, `answered_at`), as an answer given in ork is, and
    `ork show` prints it. The approval card keeps an owner question only while
    it is open, so an answered finding, whichever way it was answered, leaves
    the card. It is not filed in adj (`process.owner-answers`, adj 170),
    and an answer the owner gave in ork is never replaced. An answer that
    changes the slice reaches the implementer through the owner's fix action on
    the card; ork starts no fix round for it. A question still open stays an
    open finding. These ledger calls are advisory: a failed ask, write or read
    leaves the finding as it was and the run goes on. None is made once the run
    has a commit.
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
  ..."` (contract v6). *Proposed* (S204): from v11 the gate's ledger step closes
  with the report's verdict only when the engine accepts the round (owner
  decision 2026-11-07). A round that pauses (a shape problem, an owner
  question, a blocker finding) closes without a verdict, and its rerun is a
  new ledger step that carries the verdict.
  *Proposed* (S331; owner decision 2026-11-11, adj 281,
  `landing.auto-approve`): a gate PASS does not wait for `ork approve` when
  the run qualifies. After the card is built (`engine.autoLandReady`), the
  engine approves the run itself when the project has not opted out
  (`auto_land` false; absent means on), the run's `stop_after` is not
  `commit`, the card read the slice's human checks, the latest check in
  `checks[]` is green and no owner finding in `findings[]` is open (an
  answered one is not). It marks each open human check `later`, then runs the
  same step as `Engine.Approve` (`accept`: the checks note, the approval
  record, state `finish`), and the state machine goes on to the finish step,
  the commit and the landing in the same call. Any other run rests in
  `awaiting-approval` as before. A failed approval step (the slicer note, say)
  leaves the run in `awaiting-approval` with the reason
  `automatic approval failed: ...`; `ork approve` then works as before. An
  approval by `ork approve` and an automatic one are the same record
  (`approvals[]`, `what` `slice`). adj 162 (`landing.in-ork`) is unchanged:
  ork still lands; only who approves differs.
  *Proposed* (S50): the gate brief warns about changed paths from `git status`
  in the worktree that fall outside the item's Files paths from
  `slicer show --json` `paths` (slicer s395). An entry covers itself and
  paths under it; `.slicer/` is exempt. An empty Files paths list or a failed
  read adds no section. The gate decides whether each file is needed; this
  warning alone does not fail the gate (`engine.files-scope`, adj 119).
  Nothing from this check is recorded in `run.json`.
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
  *Proposed (S217):* before approval, the run pane asks `run.landcheck` on
  open and each refresh. This separate read opens the stored record without
  taking a run lock or running git inside a snapshot. Other states and a
  `stop_after` of `commit` return an empty result without git. Unknown runs
  are `not_found`. The check uses the landing's main-status parser and
  tracking-file exclusions below, but the slice changed set includes its
  uncommitted work: `merge-base BASE HEAD`, then
  `diff --name-only -z --no-renames --no-relative MERGE_BASE` plus untracked
  worktree paths from `ls-files --others --exclude-standard -z --full-name`.
  Blockers are sorted unique exact path overlaps, excluding this project's
  tracking files (including for a project below the repository root).
  Both names of a main-checkout rename count; directory-versus-file clashes
  are not detected. `reason` carries the landing's detached-main or
  main-on-slice-branch reason. Other git read failures set `error` and no
  warning; the TUI hides them. Every git call uses the usual 30-second read
  timeout. Nothing is persisted or written by the check. It is advisory:
  approval and the landing's own checks remain unchanged.
  *Proposed (S254):* before step 1, ork looks at the main checkout. It
  takes the paths that are uncommitted there (tracked or staged changes,
  untracked files, both sides of a rename) and the paths the slice branch
  changes since it left the base branch (`git diff --name-only
  BASE...BRANCH`), and keeps the paths in both: those are the paths the
  fast-forward would write, and the only ones git refuses on. Uncommitted
  files the slice does not change are left alone (the `ID done` commit after
  `slicer land`, step 3, still commits any `.slicer` change). The project's tracking
  files are the paths under the project's folder in the repository
  (`git rev-parse --show-prefix`; a project can be a subdirectory) that are
  in `.slicer/`, except `.slicer/config.json` and `.slicer/relay-roles.json`,
  because a matrix or config edit can be a deliberate uncommitted owner
  edit. A sibling project's `.slicer` is not this landing's.
  - If any overlapping path is not a tracking file, the run pauses
    (`paused`, pending kind `error`, state `land`) before the merge-in and the
    re-check, with one line: `landing stopped: uncommitted changes in P to
    files the slice changes: F1, F2 +N more; commit or stash them` (at most
    five names). ork commits nothing, and never stashes, checks out or resets
    in the main checkout. `ork retry` goes on after the owner commits or
    removes the files.
  - If every overlapping path is a tracking file, ork commits them on the
    base branch as `Tracking edits on main before landing ID`, by name
    (`git add -A -- PATHS`, `git commit -m MSG -- PATHS`, never a pathspec of
    `.`, so staged files stay staged). The base moved, so step 1 merges it in
    with slicer's merge drivers and re-checks. The commit is silent: it shows
    in `git log` and the run's `writes[]`. Pathspecs are literal
    (`:(literal)PATH`), so a name with a glob character takes no other file
    along; only paths that still exist are added (a deletion the owner staged
    is recorded by the commit alone). A failed add or commit pauses with one
    line that starts `git add: ` or `git commit: `; a failed commit after a
    good add ends `; ork staged PATHS` (at most five names), because the files
    stay staged. File names in these reasons have control characters and
    terminal escape sequences dropped. The diff is read with `--no-relative`,
    so a `diff.relative` setting cannot switch the check off.
  The same check runs again right before the fast-forward, for an edit made
  during the merge-in or the re-check. A tracking-only overlap there is
  committed and the landing goes back to step 1 once; any other overlap, or a
  second one, pauses as above. A failed read (`git status: `, `git diff: `,
  `git rev-parse: `) pauses with one line. When the slice branch is already
  in the base branch (a retry after the fast-forward), none of this runs.
  1. If the base branch moved (its tip is not in the slice branch), `git -C W
     merge -m "Merge BASE into BRANCH" BASE` in the worktree, then `slicer
     render`, a commit of what render rewrote, and a re-run of the project
     check command (never `round_check_command`, S285) and `slicer check`.
     With no move, none of this runs. The
     run record's `land_recheck` is set before the merge and cleared by a
     green re-check (or by the hand-off to the fix round). A retry with it set
     merges the base if it is not in yet (a crash between marker and merge),
     then renders and re-checks, and never fast-forwards a tree that was not
     checked. *Proposed (S180):* if the root `CHANGELOG.md` conflicts (even
     when other files do) and all three index stages exist, ork merges
     complete Unreleased entries in Go. The prefix and release suffix must
     stay unchanged; both sides must retain the original entries unchanged and
     in order. The result puts branch additions first, then base-branch
     additions, then the original entries. *Proposed (S327):* a `- ` line
     always starts an entry, with or without a blank line before it, so a
     compact body (no blank lines between entries, as at the top of ork's own
     `CHANGELOG.md`) is accepted as well as a blank-separated one or a mix. The
     original entries keep their separators and the text after the last entry
     byte for byte, and the body must end the same way on every side. The new
     entries are joined to each other and to the first original entry with the
     first separator at the top of the base's Unreleased (a blank line when no
     body has two entries; a side's separator is used only when the base has
     none). Identical additions appear once; every entry must survive
     exactly once. ork stages the result (`git add -- CHANGELOG.md`); the
     merge commit follows below. This resolution is silent.
     *Proposed (S238):* ork, not a role, settles a conflicted merge. After the
     changelog step it commits the merge as `Merge BASE into BRANCH` when no
     unmerged file is left. Otherwise it pauses the run (`paused`, pending
     kind `error`, state `land`) with a one-line reason that names up to five
     files and `+N more`, the worktree and `ork retry RUN`; `ork retry`
     and `t` work. The files are the owner's, as the relay contract says
     (`slicer ai relay`, Land). When every remaining conflict is under this
     project's `.slicer/slices/` (including its repository prefix for a
     project in a subdirectory), the reason names them as slicer tracking
     files changed on both sides (S289, adj 292). It gives shell-quoted
     `git -C WORKTREE show :2:FILE` (branch) and `:3:FILE` (base) commands
     for each named file, says to keep both changes and remove the markers,
     then `ork retry RUN`; no `git add` is needed. Mixed conflicts retain
     the generic reason. ork does not pick a side.
     *Proposed (S334, adj 303 and 304):* when the only unmerged path is the
     item's own `.slicer/slices/ID.json`, deleted by the branch (git's stage 2
     is missing: the branch ran `slicer done`, which moves the file to
     `.slicer/slices/done/ID.json`) and edited on the base, ork three-way
     merges the base's change into the branch's `done/` copy
     (`git merge-file`, the merge base as ancestor). A clean merge stages the
     copy and removes the old path, and the landing goes on to render with no
     pause. A conflict leaves markers in the `done/` copy and pauses with the
     tracking reason above, naming the `done/` file, with `git show
     HEAD:DONE_FILE` for the branch and `git show :3:OLD_FILE` for the base.
     On a conflict ork also removes the old path's working file, so a retry
     sees it absent. On `ork retry`, the item's unmerged old path that is
     absent from the worktree is staged as deleted (`git rm --cached`) only
     when git marks it deleted on one side (deleted by us: stages 1 and 3;
     deleted by them: stages 1 and 2) and the item's `done/` copy exists and
     holds no markers, so no `git rm` is needed. A path both sides modified
     (stages 1, 2 and 3) is no deletion; any other absent unmerged path stays
     unmerged.
     When a conflicted path is under `.slicer/`
     and a slicer driver named in `.slicer/.gitattributes` is missing from git
     config,
     the reason adds: "slicer merge drivers are not configured in this clone;
     run slicer setup-git and the lines it prints, then git -C WORKTREE merge
     --abort and ork retry RUN" (S328, adj 288). WORKTREE is the shell-quoted
     worktree path and RUN is the run ID.
     The check is advisory and does not block starting runs.
     No role resolves them: roles do not commit
     (`git.commit`) and the default implementer allowlist has no `git add` or
     `git commit`. The conflict is left in the worktree; ork never runs `git
     merge --abort` or `git reset`. On `ork retry` with the merge still open
     (`MERGE_HEAD` set), ork does not merge again: it tries the
     changelog step again if `CHANGELOG.md` still has markers, stages each
     unmerged file that exists and holds no conflict markers (`<<<<<<< ` and
     `>>>>>>> ` lines), by name and never with `git add -A`, pauses again if
     any file is left, and otherwise commits the merge, renders, commits what render
     rewrote and re-checks as above. A file that git left unmerged without
     markers (a binary file, a modify/delete) is staged only on a retry, never
     right after `git merge` stops. If the owner committed the merge, ork
     skips the merge and goes on to render and re-check; if the owner aborted
     it, ork merges again and pauses again. A failed merge commit pauses with
     git's message.
  2. `git -C P merge --ff-only BRANCH` in the main checkout. A failure (a
     file written in the main checkout after the second check, say) pauses
     the slice with the git message folded onto one line (*proposed*,
     S254); `t` retries.
  3. `slicer land ID --branch BRANCH --note "Landed BRANCH"` in the main
     checkout (it needs the branch merged, so it follows the fast-forward). In
     `handoff` mode it marks the item done there, and ork commits the `.slicer`
     change as `ID done` (`git commit -- .slicer`, so files the owner staged
     stay out of it); in `done` mode there is nothing to commit.
     *Proposed (S331, adj 281):* the same step adds the run's human checks to
     `human-checks-todo.md` at the project root: the checks of the approval
     card that are marked `later` (a ticked or an open one is left out), each
     as `- [ ] TEXT` under `## ID (run RUN, DATE)`, with a leading
     `deferred to land: ` written as `[AFTER LAND] `. DATE is the landing's
     date. A missing file is created with its header; an existing file keeps
     its text byte for byte and only gets the section appended (after a
     newline when its last line has none). A run that already has its section
     there adds nothing, so a retried landing appends once. The file is
     committed with the `.slicer` change in one commit by path
     (`git commit -m "ID done" -- .slicer human-checks-todo.md`), or alone as
     `ID human checks` when `.slicer` has nothing to commit. A run with no
     check marked `later` touches no file. A failure writing or committing the
     file pauses the landing like the other steps.
  4. `git -C P push` when the project's `push` setting is on (off by default;
     ork writes it only when the owner runs
     `ork projects set NAME --push on|off`). A failed push pauses the slice; the branch is on the
     base branch already, and a retry pushes again.
  5. Cleanup, as the owner's approval confirmed it: `git -C P worktree remove
     W`, then `git -C P branch -d BRANCH`. Never `--force` or `-D`. A cleanup
     that fails leaves the run `committed` with the reason; the merged-branch
     cleanup key (`git.cleanup-key`) then works as before.
  *Proposed (S180, S238):* an unavailable changelog stage, a failed
  changelog resolution, an edited or ambiguous entry or a changed prefix or
  suffix leaves `CHANGELOG.md` conflicted like any other file: the run pauses
  as in step 1 and the reason adds why ork could not merge it. A failed stat,
  file write or stage-add makes a best-effort attempt to restore the conflict
  markers. A red re-check after the merge (for example goldens another slice
  changed on the base branch) goes to one implementer fix round
  (`feedback.kind` `land`) with the red output as the brief. The role does not
  commit: ork commits its change at the next approval. Then the checks, the
  gate and a new approval follow. `land_fixes` counts that round only, and
  `ork status` says the approval is a repeat (`again: the landing was blocked
  and its fix round is done`). If the second re-check is red, the slice stops
  with `land blocked: checks red after merging BASE`, in Needs you, with the
  worktree and the branch kept.

*Proposed (S263):* merge detection (`docs/state.md`) prints a given run's
error once, not on every HEAD change. A committed run whose worktree path and
branch are gone and whose work is not in HEAD is marked cleaned with a
`cleaned_reason`, and is then no longer checked; git is only read for it.

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
  class (`ok`, `rate_limited`, `auth`, `error`, `connection`, `unclassified`), the session, the final
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
  `writes[]` and the relay goes on. *Proposed (S295):* the check command is
  verified after the orchestrator step (see **setup** above); `implement`
  verifies it again before round 1 when no implementer round has run and setup
  did not just do it, that is, when it is entered from a retry of the setup
  pause or from a resume of a stopped run.
- **Ledger.** The orchestrator step is ork's own (agent `ork`, no vendor or
  model). Each role round is a step with `--parent` on it, labelled `ID
  role`, `ID implementer fix round N` or `ID implementer finish step`, and
  its phase is set to `done` or `fail` when the round ends. A stop ends the
  run with outcome `stopped` and `--close-steps fail`; the finish step ends
  it with `pass` and `--close-steps done`.
- **Checks.** The project's check command runs with `sh -c` in the
  worktree, then `slicer check --json`. Either red sends the implementer
  back, resumed, with the output (last 30 KB) in the brief, and spends no
  review. The output is saved as `checks-<round>.log`. *Proposed (S285):* with
  `round_check_command` set, the round check runs it, and when it differs from
  the check command a green result is followed by the check command, saved as
  `checks-full-<round>.log`; the landing re-check runs the check command only. *Proposed (S239):* an
  exit failure of the project command is run once more first, and the brief
  starts with the failing tests (see the checks bullet above). *Proposed (S279):*
  `slicer check` problems on other items defer to the landing merge when the
  branch is behind its base (see the checks bullet above). *Proposed (S293,
  adj 211, 212, 213):* the project command runs in the project's own folder
  of the worktree, not at its root. The worktree is a checkout of the whole
  repository, so a project that lives in a subfolder of it (a monorepo, for
  example `libraries/prism-ui`) would otherwise run its command one level too
  high. ork asks git for the folder (`git rev-parse --show-prefix` in the
  project path; empty at the repository root) and joins it to the worktree. This
  holds for the round check, the S239 rerun and the landing re-check
  (`land-checks-<round>.log`). A project at the repository root is unchanged.
  When the folder is not at the root, the check log names it on a line
  `(in libraries/p)` after each `$ COMMAND` line. A folder that is missing in
  the worktree, or a git error, pauses the slice with kind `error` and a
  reason that starts `check directory:`; ork does not fall back to the root.
  `slicer check` still runs at the worktree root. S295 reuses the helper
  (`Engine.CheckDir`).
- **Review and gate.** The reviewer runs once, fresh and read-only. Findings
  start a fix round. After green checks, or a clean review, the optional
  architect runs before the gate. Its whole report starts another fix round,
  followed by checks. The gate is the resumed reviewer (role `gate`), which
  reviews and gates the fix in one pass (*proposed*, S204: this is relay v11's
  small-fix rule, met; see `slicer ai relay-orchestrator`, Gate, and the table
  in `orchestrator-parity.md`). Without an architect entry, a clean
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
  *Proposed (S294):* after round 3 ends red (checks, review or architect
  findings, or a gate FAIL), the run pauses with pending kind `rounds` and
  preserved feedback. It also pauses immediately when adjacent red checks
  have byte-identical logs and the implementer left the same tracked content
  and untracked names. Missing fingerprints in old records do not prove no
  progress. The reason names the round, failed step and last meaningful line
  from the failing check's output. A failed project command takes precedence
  over slicer output; slicer supplies the line only when it alone fails.
  Blank lines and standalone closing JSON braces/brackets are skipped.
  The reason offers more rounds (2 by default), changing the check, or
  stopping in words. CLI commands appear only in status's trailing
  "; next:" hint, which the TUI pane removes.
  `ork retry RUN [--rounds N]` grants two more implementer rounds by default;
  the durable `round_limit` is the current round plus N. A red result at that
  limit pauses again. Rounds never silently stop a run; the third gate FAIL
  still stops it. There is no project or run round-limit setting. The test
  hook `Deps.MaxRounds` overrides only the first pause threshold.
  To change the check, stop the run, set the check (`ork projects set P
  --check CMD`; a stopped run does not block it), then resume the stopped
  run with `ork retry RUN` (S296, below).
- **Resume a stopped run** (S296, *proposed*). `ork retry RUN [--rounds N]`
  also accepts a stopped run that has no commit, did not stop in setup or
  while queued (`stopped_from`), and whose worktree path exists and passes
  the same check as `ensureWorktree` (path, branch and git dir; ork never
  creates the worktree on a resume). Anything else is refused in one line
  of the form `run ID <fact>; <action>`: a committed run, a run stopped
  during setup, a missing or foreign worktree (the line adds `ork dismiss ID
  to start a new run`), a role still running, another unfinished run of the
  item, or a newer run of the item that uses the same worktree (the older run
  is superseded: its status hint and `run.show` name no retry). A retry note
  is refused for a stopped run. `Engine.CanResume` makes these checks
  without changing anything. The daemon opens the run from the
  store when it holds none (after a restart) and builds the engine again
  from the registry entry as it is at the resume, so a fixed check command
  takes effect. The
  resume clears `dismissed_at`, `reason` and `stopped_from`, grants N
  implementer rounds (2 by default) and begins a new ledger run
  (`relay-run begin`); the stopped run's ledger run stays ended as stopped.
  It re-enters `checks` in the next round number (the earlier check log is
  kept), or `implement` in the same round when no implementer round has run
  (a run stopped from S295's setup pause): implementer round 1 runs first
  with the first-round brief. N rounds mean N implementer rounds: entering
  the checks, `round_limit` is the entered round plus N (the entered round is
  not an implementer round); entering implement, round 1 is the first of the
  N, so `round_limit` is N (the first-pause floor of 3 rounds still applies).
  A red result at the limit pauses with kind `rounds` as above. `stop`
  records `stopped_from`: the state the run left, and for a paused run its
  pending step. Resuming bypasses the project and global caps, like every
  owner action (adj 122), and the resumed run counts as unfinished again,
  so `ork projects set` refuses the fields a run reads until it rests.
  *Start on an item with a stopped run (owner decision 2026-11-07, S296
  Q1, option b):* `ork run` and `ork wave start` look at the newest run of
  the item. When it is stopped, not dismissed, resumable by the rules above
  and its worktree is the planned one, ork compares the worktree with the
  last implementer round: the fingerprint of the tracked diff against HEAD
  and the untracked names (the one `roles[].tree_sha256` holds) must equal
  that round's `tree_sha256`. Equal: the start resumes that run with the
  default grant, in the wave of this start (a run started alone gets a wave
  of one), and the reply's `message` says so. The resumed run keeps the
  stopped run's lineup, budgets and stop-after; the confirmed plan's are not
  applied, and the message says so. Different, a role still running, a
  worktree that is not the run's, or no fingerprint to compare (no
  implementer round, or an old record): the start creates a new run at round
  1 in the same worktree, and the `message` says why. A dismissed run is
  never resumed by a start; dismissing is the owner's word that the run is
  done, and `ork retry RUN` still resumes it. With no free slot the start is
  refused, naming the stopped run (`ork retry RUN` gets past the caps, as
  every owner action does), so its work is not restarted behind the owner's
  back. The start fingerprints the tree before it takes the manager lock; under
  the lock it checks that the run it probed is still the newest and has the
  same `updated_at`, and that no finding action holds the run's record, or
  refuses the start with "start again". The reply is not sent until the
  engine applied the resume; a resume the engine refuses after all is
  answered as `refused`, never as resumed.
- **Resume.** A fix round resumes the implementer's latest session and the
  gate resumes the latest gate or reviewer session from the same vendor and
  model. Daemon runs copy the reviewer's lineup into the gate; a copy counts
  as following the reviewer, as does an empty gate lineup. With no gate
  records, the gate starts on the reviewer's current row. Its own records
  take precedence for later attempts. Without a matching session the role
  starts fresh and its brief carries the earlier reports
  (`role.resume`). *Proposed (S256):* a resumed round's brief also has a
  fixed `## Resumed session` section after `## Your files` (see Brief). A
  resumed role keeps its memory of the tree; the section tells it that the
  worktree may have changed and to read it again before it judges.
- **Report checks.** `CheckReport` refuses a gate report whose first line is
  not exactly `VERDICT: PASS` or `VERDICT: FAIL`, an implementer or gate
  report with no `Tests: N passed, M failed` line (the finish step needs
  none), and any report with no `## Owner needed` section. All problems are
  listed. A gate PASS with a nonzero failed-test count is also refused.
  *Proposed (S275, S287):* a shape error automatically
  retries the same reporting role once in the same round and session. Its brief
  names the shape problems. When the refused report also asks the owner (an
  `## Owner needed` body that is not blank, `none` or `none.`, or a line that
  opens an `[owner]` or `[owner blocker]` finding), the brief adds
  one sentence: keep the owner question as it was and fix only the shape. The
  owner then sees one well-formed question after the retry, or a shape pause
  if the retry is also malformed. Implementer (including finish), reviewer, gate,
  architect and UI pass all follow this rule; spec check keeps its own
  retry (S266, below).
  The retry uses no additional implementer round. The run record preserves
  both attempts and the refused report. Status and role rows say "shape retry";
  the first refusal sends no owner notice. A second malformed report pauses
  (kind `shape`) with the automatic marker cleared; `Retry` resumes the role.
  A refused report's question is not recorded; only the retry's is. Normal
  budgets still apply before the automatic attempt. A restart between attempts pauses
  the run (kind `shape`, marker cleared) because nothing relaunches it; `Retry`
  resumes the role with the shape brief. A lost running process follows normal
  recovery.
  *Proposed (S298; owner decisions 2026-11-05):* the shape error names what is
  wrong with an `[owner]` finding: a missing `Options:` line, a count of
  options other than 2 to 4, an option numbered out of turn, an option line
  with no `: summary`, a missing `Recommend:` line, a `Recommend:` number out of
  range, or a line in the wrong place, each with the offending line cut to 120
  characters. A finding that has `Options:` and `Recommend:` is never told that
  they are missing. The pause keeps the lines of each finding that failed
  (cut to 2 KiB), and the shape retry's brief carries the error and those
  lines under "Report shape", so a role that cannot resume its session still
  sees them. The run counts the consecutive shape pauses of one role and
  round (`pending.shape_pauses`; the automatic retry is not a pause; a
  round that ends clean, another role or another round starts again at 1).
  The second one is capped: its reason names the refused report file, `ork
  show RUN`, and the ways past, and a plain `ork retry RUN` is refused with one
  line that names `ork show RUN`, `ork retry RUN --accept-shape`, `ork retry RUN
  --again` and `ork stop RUN`. `--again` runs the role once more on purpose; a
  further shape failure pauses capped again. `--accept-shape` is chosen by the
  owner after the cap, never by default: it reads the refused report again,
  keeps each `[owner]` finding that failed on its options as an owner finding
  with its text and no `ask` (an `[owner blocker]` becomes a free-text
  question), runs no role and spends no round, and the run goes on as if
  the role had given that report. Any other shape problem (no `## Owner
  needed` section, no verdict, no Tests line, a malformed Owner needed block, a
  bad Decision key, a finding without a title) is refused with one line
  saying that only an owner finding's missing options can be accepted. A
  reviewer, UI pass or architect round that is accepted opens no ledger step
  (its refused attempt's step already closed). A gate round that is
  accepted opens a new gate step on the lineup row of the refused attempt and
  closes it with the verdict from the report, as any accepted gate round does
  (S204); the refused attempt's step stays closed without one. The reviewer and
  the UI pass share the single pending slot, so each role that does not hold
  it keeps its own count in its newest role record (`shape_retry`), and a pause
  with a higher count takes the slot from a lower one: a capped role always
  holds the slot, and a plain retry is refused while it does.
  *Proposed (S256):* inside `## Owner
  needed`, lines that hold only a code fence (three or more backticks, then
  at most one word such as `text`) are ignored before the strict question
  check, because the role prompt shows its example in a fence.
  Any other extra line inside the question block is still refused, and a
  fence around a whole report is not unwrapped.
- **Decision keys.** An Owner needed section may include one line
  `Decision key: area.topic` anywhere in the section. The key must be lowercase
  and dotted; a malformed or repeated key is a `role.report-shape` error.
  The full section and its optional key are stored with the question. A key
  enables optional adj filing of the owner's answer in the TUI.
- **Tool feedback** (relay v12, `relay.tool-feedback`, adj 325). Any role may
  end its report with up to 3 lines `tool feedback: N. ROLE | tool: slicer|ork|NAME
  | kind: friction|feature|deterministic-check | proposal: TEXT | evidence: TEXT`.
  `evidence` is last and takes the rest of the line. ork removes every line
  that starts with `tool feedback:` from the report before any other check, so
  a line anywhere in the report, a malformed line, a surplus line and `tool
  feedback: none` are never a shape error, a finding or an owner question. The
  well-formed lines (at most 3 per report) go into `tool_feedback` of the run
  record with the reporting role, and the record keeps the first 3 of a run, in
  report order. They are advisory: the gate never decides on them, a fix round
  and the next brief do not carry them, and nothing runs `slicer feedback` for
  them. A spec check has no run, so its lines are dropped. Showing them in the
  TUI or `ork show` is not built.
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

  The dash in `Recommend: N - reason` may be an ASCII hyphen, an en dash or an
  em dash (S132, *proposed*); a role that writes `Recommend: 2 – reason` is not
  a shape error. The prompts keep teaching the hyphen, and `Recommend: 1 -- r`
  and `Recommend: 1 r` stay shape errors.

  Free context lines may precede `Question:`. They stay in the raw question
  text and the pane shows them above the options. From `Question:` onward,
  the block is read as an owner finding's body is (S288, owner decision
  2026-11-07, *proposed*): one leading `- `, `* ` or `+ ` marker is stripped
  from `Options:`, the option lines and `Recommend:`, and any other line
  inside or after the block is context. The question text of the pause (and
  of `questions[]`) then holds the leading context, that context, and the
  block, in that order, so the pane shows the context above the options.
  A `Decision key:` line behind a list marker and an option line after
  `Recommend:` are also shape errors. A line between two numbered options, a numbered line that is not a valid
  option, a repeated or misplaced `Options:` or `Recommend:`, and a missing
  or malformed `Options:` or `Recommend:` are a shape error. This leniency
  never applies to the `VERDICT:` first line. The optional decision key line
  remains allowed anywhere in the section.

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
  0, 1 or ork's own. When the role's leader process is still alive, ork also
  kills the processes below it that left the group, found from a `ps` snapshot
  taken before the kill (S236, *proposed*); a process already reparented to
  pid 1 is not found, and a dead leader leaves only the group kill. The same
  tree kill runs when a role's or a check's context ends (timeout, owner stop
  or the hung watchdog). *Residual risk:* a recycled process group id is not
  told from ork's; the record has no start time to compare.
- **Approval card (*proposed*, S118).** When the gate passes, the engine
  builds the card in the run record (`state.md`): the verdict, the gate's test
  counts, the diff stat against `HEAD`, and the slice's `[HUMAN CHECK]` items.
  *Proposed* (S204): each bullet or numbered line of the slice's Check section
  marked `[AFTER LAND]` (with or without `[HUMAN CHECK]`) is also a card item.
  Both markers are removed and the text starts `deferred to land: `. The item
  starts in state `later`, because the owner runs the line after landing
  (`slicer ai relay-orchestrator`, Every brief); ork never runs a slice's Check
  line (owner decision 2026-11-10, `relay.after-land-check`). An `[AFTER LAND]`
  line outside the Check section adds nothing. A mark the owner made, or took
  back, is kept as for any check.
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
  refused (not stopped) at the current round limit (the first threshold or
  an earlier retry grant). Within that limit, Fix itself is owner approval
  to run the next round immediately; no separate retry is needed.
  The gate's brief carries the
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
  none". A directory reads as its cause (`permission denied`) and a file as
  `NAME (WHY)`. The directory is cleared when the UI pass starts, so a rerun keeps
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
*Proposed (S223):* a non-blocking owner finding is asked with `relay-run ask`
without `--blocker`, which records the question and leaves the step running;
it records no wait.

## Roles

*Proposed (S309):* spec-check and gate briefs split slice Check commands into
implementer-runnable and orchestrator-only commands, using the implementer's
resolved allowlist. An environment assignment prefix, an absolute path outside
the worktree, or an unallowlisted command is orchestrator-only. Spec-check flags
these lines so the owner can replace them or assign them to the orchestrator.
Only backtick spans identify commands; prose bullets without backticks are
absent from both command lists.
The gate names the commands it ran and those it left to the orchestrator.
Per the owner answer for `gate.tests-green-evidence` (2026-11-08), the read-only
gate accepts ork's latest round check result as tests-green evidence and must
not run the suite itself. The Gate section repeats this rule. The brief
carries the round, command, log name, success status and captured log, including
failing lines. The existing report reader sanitizes the log and marks truncation
above 64 KiB. A missing or failed result does not establish tests green. The
split grants no command permissions, and inability to run tests or an
orchestrator-only line alone does not fail tests-green. Test counts in the gate
report remain counts of tests the gate itself ran; ork's evidence is cited
separately.

- **Prompts.** Embedded in ork and canonical (`roles.prompts`). They are
  seeded from slicer's v11 contract and now written against v12, with anything
  else written fresh (`roles.prompt-seed`). They carry no Claude Code-only mechanics (Agent
  tool, SendMessage, nesting), because roles here never spawn agents.
  `ork roles print [ROLE]` prints them so the agents repo can generate its
  own role files.
- **Brief.** It contains:
  - the role prompt;
  - the slice from `slicer show ID --json --lean`;
  - the tracking rule and the workspace limit;
  - the environment lines from the project's AGENTS.md;
  - the role's own temp directory;
  - the report path. *Proposed (S256):* for a read-only role (reviewer,
    gate, architect, UI pass, spec check) the line says that ork saves the
    final message there and the role cannot write it, and another line asks
    for the whole report as the final message in plain text, with no code
    fence. The role's permission mode is not a write mode on any vendor
    (`--permission-mode plan` for cloud Claude and Grok, the fixed read-only
    allowlist for claude-local, `-s read-only` for Codex),
    so the final message is the only route (`role.report-io`);
  - *proposed (S256):* for a resumed round, a `## Resumed session` section
    right after `## Your files`. It says that ork resumed the earlier
    session, that the worktree may have changed, and to run `git status` and
    `git diff` again, read again every file it cites and not repeat an earlier
    finding, verdict or answer without checking it. It then shows ork's own
    `git status --porcelain` from when the round started, in a fence, or says
    that the tree had no uncommitted changes. The listing is the same call as
    the read-only fingerprint, so a read-only role costs no extra git call;
  - for the implementer, the project check command (when configured) and
    `slicer check`, which ork runs after the round (not the finish step)
    outside the role's sandbox;
  - *proposed (S277):* a `## Done when` checklist after the check instructions
    and before the round and owner sections. It repeats on resumed rounds after
    `## Resumed session`. Each role gets its own acceptance criteria and exact
    report lines, read-only limits or resolved allowlist forms, and blocked-check
    reporting instructions. The implementer gets the slice's `Failing tests` and
    non-HUMAN `Check` items, or "the slice lists none", and one proving-test item
    per explicit `[fix]` finding in Review findings, Gate FAIL, or Architect
    findings feedback. Slice bullets retain indented continuations; unindented
    prose ends the current item and is not appended to it. Finish rounds list the
    closing note and configured finish command in order, with no test-count
    requirement. A denied finish command goes under `## Owner needed` and is
    not retried. Other denied checks go in implementer report item 6 with actual
    test counts; ork runs its checks outside the sandbox. The checklist stays
    below 40 lines, grouping overflow items without dropping their text. It
    supplements the prompts' report parts and changes no report parser;
    *proposed (S243):* the `claude` implementer also gets checklist items to
    change files with Edit and Write only, avoiding `sed -i`, scripts,
    heredocs and file redirects, and to run one plain shell command per call
    without pipes, `&&`, `;`, command substitution or `cd`. A denied command
    calls for an allowed form. These items also appear in finish briefs.
    The existing resolved allowlist appears once; prompts stay vendor-neutral;
  - earlier reports, when the role is not resumed.

  A tracking step denied to the implementer is a Deviation for the orchestrator,
  never a `[fix]` (`roles.denied-tracking-step`). The reviewer writes an untagged
  commentary line under section (C); the gate writes one "Deviation for the
  orchestrator" evidence line beside its criterion lines. Neither is a finding
  or starts a fix round.
- **Report.** The role writes its final report to
  `~/.local/state/ork/runs/<run>/<role>-<round>.md` and exits
  (`role.report-io`). If the file is missing, ork uses the vendor JSON's
  final result text. A read-only role cannot write the file, so its brief
  tells it to report in its final message, which ork saves to the report path
  (S256, *proposed*). ork validates the shape: the gate's first line, the
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
  status changes outside the role's own run, it checks once more with fresh
  specs. *Proposed (S266):* a report with a shape error, or a round that ended
  with denied commands (a piped `rg ... | head` is the case seen), resumes the
  role's own session once (`Request.Resume`, the same row) with a short prompt
  that names the shape problems or the denied commands and the expected forms
  (`Ready`, or `Question:` blocks, then `## Owner needed`; one command per
  call, no pipe). That resume is the retry: a second shape error or denial
  fails the check with no start over, and the check resumes at most once,
  even if it starts over. It is not offered when the role changed the tree or
  HEAD moved, when the exit was a vendor limit, an auth failure or unclassified
  (adj 23), or when the vendor gave no session id (a shape error then starts
  over once, as before; a denial fails at once). The refused round is a
  failed `spec-check` ledger step (reason `report shape` or `denied: ...`) and
  the resumed round is a second step (task `resumed with error`). A failure
  after the retry names the slices and says that checking again is safe
  because the check changes nothing (code `spec_check_retry`); its text does
  not say "spec check", because the CLI prefixes `spec check failed: `.
  This replaces the S143 rule that denials are never retried (2026-10-28).
  Its report has one `## Slice
  ID` section per slice, holding `Ready` or one or more question blocks; the
  multi-block parse (`ParseOwnerQuestions`) reads every `Question:` block of a
  section, each block read as an `## Owner needed` block is (list markers
  and context lines allowed, S288, *proposed*; the block's structure is still
  strict). The extra lines stay in the block's raw text (`SpecQuestion.Text`)
  and are filed with the adj record when the block has a decision key; the
  TUI and CLI show only the title, options and reasoning. Its lineup row is the
  matrix rows that the project's `spec_check_rows` list gives, in order (S258,
  *proposed*; the first row with an adapter runs and the next is its
  fallback), else every row that a rule naming "Spec check row (rNN)" gives,
  in rule order (u11 today: sonnet, medium; owner decision 2026-11-07).
  Those matrix rows, and every row a rule names, remain separate from every
  run's reviewer and gate chains. With neither a setting nor a rule, or when
  no listed row is among the reviewer rows with an adapter that slicer
  recommends, the check is refused with code `no_spec_check_row`. The message
  has one line each for the problem, `fix: ork projects set PROJECT
  --spec-check-rows ROW` (ROW is a real candidate), `or:` (the
  `slicer relay-roles rule-add` hint when the project sets no rows; when it
  sets rows and a rule names one, the command that clears the setting to go
  back to the rule), the reviewer rows with an adapter, and the warning that
  a spec check row leaves every run's reviewer and gate chains. The CLI in
  text mode prints it after `ork: spec check failed: `, with no code (the code
  shows with `--json`); the TUI shows `CODE: message`. The ledger role comes from the contract JSON as
  `spec-check`. Its report and temp directory are under
  `~/.local/state/ork/spec-checks/<project-key>/<item-key>/` (hashed identities).
  Each item has `check.json` identifying the project and item, `spec-check.md`,
  and, after an answer, `answers.json`. *Proposed (S264):* the `Owner needed`
  text ork keeps from the report (`engine.SpecOwnerText`) drops code fence lines
  and is empty when its first non-blank line is `none`; `spec-check.md` keeps
  the whole report, and a loaded `answers.json` is cleaned the same way without
  being rewritten. The batch's first item also holds the
  role's temp directory. A landed run or removed project loses its check
  directories, and so does the item of a run stopped after commit when its
  branch is found merged (*proposed*, S229). At daemon start, directories older than 14 days are swept
  when their item is done, retired or gone, using the ordinary slicer list
  without `--all`. Old directories for unregistered projects are removed too.
  Legacy random-named directories (`YYYYMMDD-HHMMSS-xxxx`) older than 14 days
  are also removed (owner decisions, S143, 2026-10-28). Empty or corrupt
  `answers.json` files are discarded and logged with their full path; the
  check asks the owner's questions again.
- **Hung role** (S182, *proposed*). Every engine run role round has a watchdog.
  Cloud roles keep
  the time of the last vendor stream event or assistant text (`Request.OnEvent`
  and `OnLive`). When a role has sent neither for 30 minutes, the watchdog ends
  the context the round runs under, and the vendor adapter kills the role's
  whole process group. ork kills only the group it recorded for that role
  (`OnStart`) and the processes below the role's own process that left that
  group, for example a `go test` run in a session of its own (S236,
  *proposed*; owner decision, 2026-11-02). It finds them from a `ps` snapshot
  of the role's pid taken just before the kill. A process already reparented
  to pid 1 before the kill is not found. It never kills a process that is
  neither in the recorded group nor below the recorded role process. The
  process table is the only thing it reads; if `ps` fails, only the group is
  killed. The limit is fixed
  at 30 minutes, the stale cutoff of the Running row (owner decision,
  2026-10-29); `Deps.HungAfter` sets it for tests, and a negative value turns
  the watchdog off. The role is recorded with exit `hung`, its ledger step
  ends `fail`, the heartbeat stops, and the slice pauses (kind `lost`) with a
  reason that names the role, the round, the limit and, when ork recorded
  one, the killed process group. A hang is not a vendor limit: it does not fall back to the next
  lineup row and marks no vendor out. There is no automatic retry. The owner
  retries (`ork retry RUN`, or `t`), and the same row runs again with the
  "Earlier process lost" section in its brief. An owner stop during the role
  stays a stop (exit `cancelled`). A role that returns a result with no error
  in the tick the watchdog fires is recorded as it exited (S194).
  Local launchers instead have a fixed 90-minute elapsed runtime limit
  (S343, adj 315, superseding 195). Events and assistant text do not reset it,
  and time the machine slept is not counted in it (the sleep detector applies,
  S235). Expiry ends
  the same process-group context and records an `error` exit, so the existing
  fallback rules try the next lineup row. It marks no vendor out. A denied
  command or a read-only tree change still prevents fallback. Cloud roles
  retain the 30-minute silence limit and the `hung` owner-retry path.
  The wait for a vendor slot is not counted
  as silence. Time the machine slept is not counted as silence either
  (S235, *proposed*), so a wake alone never kills a healthy role. One shared
  detector in the daemon samples the wall clock about every 5 seconds; a gap
  of more than 30 seconds between two samples is a sleep, and the gap less one
  step is added to the time slept. A detector counts nothing until the daemon
  starts it (S259, *proposed*), so the watchdog's own looks, which come every
  30 seconds, are never mistaken for a sleep. The watchdog subtracts the time slept since
  the role's last sign of life from its silence; silence before and after the
  sleep still counts, and the limit does not change. The time slept is kept in
  daemon memory and shown by `ork status`, which also prints, under a run row,
  `working: implementer round 2, 14m (last output 2m ago, slept 1h03m)` for
  each role or checks step that runs now (S235, *proposed*); the clauses that
  do not apply are left out. Nothing about sleep is stored in `run.json`. The
  spec-check role and the checks command are not watched; the checks command
  has its own timeout (`check_minutes`), which is a Go timer and is not
  changed.
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
classify the exit as ok, denied, rate-limited, auth, error, connection or unclassified. A class
needs a documented signal or a real sample (`vendor.ratelimit-detection`,
`vendor.unsampled-classes`).

The `claude -p --output-format json`, `codex exec --json` and
`grok --output-format json -p` invocations, and the read-only flags
(`--permission-mode plan`, `codex -s read-only`), come from
`agents/setup/lib/workflow-vendor.sh:22-41`.

The S01 spike measured these cells on 2026-10-18 with real headless runs in a
throwaway repo. Each cell cites a fixture under `testdata/vendors/<vendor>/`
or says *unverified* and why. CLI versions: claude 2.1.302, codex-cli 0.163.0 (the S48
probes 05 to 07 ran on 0.164.0), grok 1.0.62. The grok cells for the
streamed events, usage, resume and plan mode come from the S72 re-run of the six
probes on 2026-11-03 (one call each, no retry; probes 01 to 05 passed no `-m`, so
the CLI default model ran, `grok-4.7` in the init event of probe 02). The 403 cell
cites the first run, kept as `07-out-of-credits-403`. The adapter behaviour
(S72) is *proposed* until an adj record decides the write mode.

| Capability | claude | codex | grok |
|---|---|---|---|
| Streamed events for the output tail | `--output-format stream-json --verbose --include-partial-messages` emits `stream_event` text deltas, then `assistant`, `rate_limit_event` and `result` (`testdata/vendors/claude/02-stream-json.stdout`). `--verbose` was used; without it is untested. | `--json` emits JSONL: `thread.started`, `turn.started`, `item.completed` (agent message), `turn.completed` (`testdata/vendors/codex/01-plain-stream-jsonl.stdout`). The run had whole messages and no delta events. | `--output-format streaming-messages-json --include-partial-messages` emits one JSON object per line: `system` (subtype `init`, with `session_id`, `model` and `tools`), `stream_event` lines (`message_start`, `content_block_delta` with `text_delta`, `message_delta` with usage, `message_stop`), `assistant`, and `result` (`testdata/vendors/grok/02-stream-messages-json.stdout`). `--output-format json` prints nothing until it ends, so ork uses the streamed form: each line is an event for the watchdog. |
| Usage (tokens, cost) in the result | `result.usage` (input, output, cache tokens), `result.total_cost_usd`, and `modelUsage.<model>.costUSD` (`modelUsage` is an object keyed by model name, with `costBasis` `list`; `testdata/vendors/claude/01-plain-json.stdout`). | `turn.completed.usage` has input, cached input, cache write, output and reasoning tokens (`testdata/vendors/codex/01-plain-stream-jsonl.stdout`). No cost field appears, so cost is *unverified*. | `result.usage` has `input_tokens`, `output_tokens`, `cache_read_input_tokens` and `cache_creation_input_tokens`, and `result.total_cost_usd` is set (`testdata/vendors/grok/02-stream-messages-json.stdout`). The plain JSON form adds `reasoning_tokens` and `total_tokens`: 19190 = 18014 input + 1152 cache read + 24 output, so reasoning tokens are inside the output count (`testdata/vendors/grok/01-plain-json.stdout`). ork counts input, output, cache read and cache creation. `grok usage SESSION_ID` (`testdata/vendors/grok/usage-excerpt.txt`) was not run. |
| Resume a session by id | `claude -p --resume <session_id>` answered "OK", the word asked for in the first run, with the same session id and cache reads (`testdata/vendors/claude/03-resume.stdout`). | `codex exec resume <thread_id> -m gpt-6-luna --json` returned the same thread id and cached input tokens (`testdata/vendors/codex/02-resume-by-id.stdout`). The `resume` help lists no `-s` or `-C` (`testdata/vendors/codex/resume-help.txt`). S48 probe 06 (codex-cli 0.164.0) ran from the worktree through the `in_wt` helper, since resume has no `-C`, and passed `-s workspace-write` to `resume` and got exit 2, "error: unexpected argument '-s' found", before any API call (`testdata/vendors/codex/06-resume-workspace-write.stderr`). So resume cannot take `-s`. S89 probe 08 resumed probe 05's session with `-c 'sandbox_mode="workspace-write"'` from the worktree: `resume` accepted `-c` (exit 0) and a `command_execution` item shows the worktree write with exit 0 (`testdata/vendors/codex/08-resume-sandbox-config.stdout`). *Verified*: `resume` takes `-c sandbox_mode=...`. *Unverified*: whether the setting took effect, since the resumed session was already `workspace-write`, so a kept sandbox gives the same result (adj question `vendor.codex-resume-sandbox`). The write to the main checkout has no command event (see the allowlist row). | `grok -p ... --resume <sessionId> --output-format json` returned the same session id as the first run (`testdata/vendors/grok/03-resume-by-id.stdout` and `01-plain-json.stdout`, `sessionId`). The plain JSON names it `sessionId`, the streamed form `session_id`. The id in the `.cmd` file is redacted. Whether the resumed round keeps the plan mode of the first is *unverified*. |
| Per-tool allowlist | `--allowedTools Read` with `--permission-prompts none` refused a Write. The denial is listed in `permission_denials` and the exit code is 0 (`testdata/vendors/claude/04-allowlist-denies-write.stdout`; `.repo-status` is empty). A positive control, an allowlisted tool that runs, was not measured. | No per-tool allowlist. `-s` takes `read-only`, `workspace-write` or `danger-full-access` only (`testdata/vendors/codex/help-excerpt.txt`). The one read-only run (`testdata/vendors/codex/03-readonly-sandbox-write.stdout`) ran no command: it has only agent messages. The model said the filesystem was read-only, and `.repo-status` is empty. That run did not exercise the sandbox. S48 probe 05 did, with `-s workspace-write -C <worktree>` where the worktree is linked to a main checkout. Its JSONL has a `command_execution` item (`testdata/vendors/codex/05-workspace-write-paths.stdout`). The write inside the worktree ran with exit 0 (`.worktree-status` shows `inside.txt`). The model reported that the writes to the main checkout and to the main checkout's `.git/slicer` failed with "operation not permitted", and neither file exists (`.repo-status` and `.slicer-dir` are empty). But the JSONL has a command event only for the first write; the other two results are the model's words with no event, so the block is *unverified*: the files may be absent because the commands never ran. Probe 07 (a socket bind) has no command event, so the model's reported `PermissionError: [Errno 1]` at the Unix bind is *unverified*, and a loopback TCP bind was not reached. Whether `.git` of a linked worktree is writable was not measured. S89 probes 08 to 10 (codex-cli 0.164.0, one call each, no retries) aimed to close the gaps, and none produced a command event for the asked cell, so each stays *unverified*: the main checkout write (probe 08 resumed, probe 10 fresh: the model reported `operation not permitted` in 08 (words only) and refused to run the commands in 10, `testdata/vendors/codex/08-resume-sandbox-config.stdout`, `testdata/vendors/codex/10-main-checkout-blocks.stdout`), the main checkout `.git/slicer` write (probe 10, refused, no command), the Unix socket bind, the loopback TCP bind and `git add` plus `git commit` in the linked worktree (probe 09: agent messages only, the model reported `PermissionError: [Errno 1]` at the Unix bind, `testdata/vendors/codex/09-socket-and-git.stdout`). In all three, `.repo-status` and `.slicer-dir` are empty, and in 08 the only command event is the worktree write (exit 0, `.worktree-status` shows `resume-inside.txt`). Only the worktree write is *verified*, twice. The model sometimes declines or skips the command tool, so containment of the main checkout cannot yet be taken from the sandbox alone; the gaps are adj question `vendor.codex-sandbox-gaps` and stay open. Decided: codex may run write roles with `workspace-write` (`vendor.codex-write`, decision 100). The owner relies on the tree fingerprint, the project check, review and the gate as limits; the unmeasured sandbox cells remain *unverified*. | No claude-style allowlist: `--tools` takes a comma list of tool names (`testdata/vendors/grok/help-excerpt.txt`), and the init event names `read_file`, `write`, `search_replace` and `run_terminal_command` (`testdata/vendors/grok/02-stream-messages-json.stdout`), not patterns. Probe 04 (`--tools Read --permission-mode dontAsk`) wrote nothing (`.repo-status` is empty), but the run ended with `stopReason` `cancelled` and no text, and the model said it had no write tool (`testdata/vendors/grok/04-allowlist-denies-write.stdout`). It does not show that `Read` is a tool name, so ork passes no `--tools`. Probe 05 (`--permission-mode plan`) asked to edit `notes.txt`: the model read the file and replied with a plan, and `.repo-status` is empty (`testdata/vendors/grok/05-plan-mode-edit.stdout`). *Verified*: plan mode wrote nothing in that one run. *Verified* in the wave 7 and 8 fixtures (2026-11-04 and 2026-11-05): single `git status --short`, `git diff -- PATH` and `git diff --check -- PATH` commands and the built-in `read_file`, `grep` and `list_dir` tools ran. `rg`, `slicer` and test commands remain *unverified*. Grok read-only briefs name only those verified forms and require one command per call, with no compound commands, echo, redirects or `python3 -c`. Write modes were probed in S72 (grok-4.5, `--tools read_file,write,search_replace`, one call each): with `--permission-mode dontAsk` (`testdata/vendors/grok/10-write-dontask.*`) and with `acceptEdits` (`11-write-acceptedits.*`) a headless run refused the edit of `notes.txt`, with stderr "permission required: Edit ... was not run, as a headless run cannot ask for approval"; each ended exit 0 with `stopReason` `cancelled`, and `.repo-status` and `.repo-diff` are empty. An `acceptEdits` request to write a file outside the repo was refused the same way and the file is absent (`12-write-outside.stderr`, `.outside-status`). So no tested mode lets a headless write role write, and no write was seen inside a worktree. Owner 2026-11-03 (interactive): ork gets no write mode on grok and never uses `bypassPermissions` or `--always-approve`. ork therefore runs only read-only roles on grok (`--permission-mode plan`) and refuses a write role (*proposed*, no adj record yet). The sanitized `08-permission-cancelled` fixture supplies that stream sample: exit 0 with `stop_reason: cancelled` and stderr `permission required`. *Proposed* (S280): ork classifies that combination as an error exit, with the refused command truncated to 60 characters in the reason. A non-empty report file wins; otherwise the last complete assistant text is retained only if it contains the exact line `## Owner needed`. With a retained report the exit is OK and the normal shape check judges it. Without one, the next role row takes the same round and Grok is not marked out. Partial deltas and tool-only messages do not replace the last complete text. Spec-check briefs carry each row's vendor limits. |
| Rate-limit signal | Not forced. Stream output carried `rate_limit_event` with `rate_limit_info.status` `allowed`, `rateLimitType` `five_hour` and `resetsAt`. Utilization is at `rate_limit_info.unifiedWindows.{five_hour,seven_day}.utilization` (`testdata/vendors/claude/02-stream-json.stdout`). A `rejected` status and the exit code when limited are *unverified*. The help documents only `--fallback-model`, for "overloaded or not available" (`testdata/vendors/claude/rate-limit-help-grep.txt`). | *unverified*: no rate-limit or quota term appears in `codex --help` or `codex exec --help` (`testdata/vendors/codex/rate-limit-help-grep.txt`). No JSON limit signal was observed. Decided: ork matches the reported strings, "You've hit your usage limit" and HTTP 429 `rate_limit_exceeded`, in `turn.failed` or `error` events and falls back automatically (`vendor.codex-ratelimit`). The owner added "out of credits" on 2026-10-27 (S133), classified as a limit under decision 156 (`vendor.fallback-trigger-v2`). The text matches are case-sensitive substrings. The out-of-credits message was observed in text mode on 2026-10-26; its JSON event shape is *unverified*. `testdata/vendors/codex/out-of-credits-unverified.stdout` is synthetic, based on the existing `turn.failed` shape. Real JSON samples are still to be captured. | Out of credits is sampled: the first run got exit 1 and HTTP 403, "used all available credits or reached its monthly spending limit", in an `error` line (`testdata/vendors/grok/07-out-of-credits-403.stdout`). ork classifies an `error` line, or a `result` with `is_error`, that holds "used all available credits" as out of credits. That sample came from `--output-format json`; the streamed form of the error is *unverified*. A rate limit is *unverified*: no sample, and no rate-limit term appears in `grok --help` (`testdata/vendors/grok/rate-limit-help-grep.txt`), so ork maps none. Apart from the permission cancellation above and the sampled connection failure below, every other failed run (the bad model of probe 06, an unauthenticated message) is `unclassified` and stops the slice (`vendor.unsampled-classes`). |

- **Fallback** (`vendor.fallback-trigger-v2`, decision 156): an exit
  classified as rate-limited, out of credits or error moves the role to the next eligible entry in
  its chain, in the same state and round. Auth and unclassified exits, an
  unsuccessful round with permission denials, and a changed tree in a
  read-only role still stop.
  A `connection` exit (S329, adj 291, `vendor.connection-error`) first waits
  30 seconds, then resumes the same session once on the same row and round.
  The vendor slot is released during the wait. A second connection failure
  uses the `error` fallback path. With no next row, it pauses with one line
  naming the vendor and the connection error. Without a session id it uses
  that fallback path directly. With no next row, the saved exit remains
  `connection`: an owner retry runs that row again, resuming its session when
  present, without another automatic wait. Denials and read-only tree changes
  still stop.
  The only connection signals are Grok's non-zero exit with both `reqwest
  error` and `error sending request` in stderr, and Claude's result event
  with `api_error` equal to `connection_failed`. The sanitized S268 and S216
  samples are `testdata/vendors/grok/connection.stderr` and
  `testdata/vendors/claude/connection.stdout`. Codex has no sampled signal.
  For other fallback exits, if no row remains, the stop reason names each
  failed row and its exit.
  Out of credits skips all later rows on that vendor; a rate limit takes
  the plain next row. The failed ledger step includes the classified
  vendor message as `--reason`. Falling back on a plain error exit was an
  owner-approved deviation from v10 (decision 156, S142). v11 and later follow
  ork: the `role_fallback` conformance key falls back on an error exit of the
  agent process too, so ork's behaviour conforms.
  The approval card names each switch as `role switched: ROLE FROM to TO
  (REASON)`, identifying rows by vendor, model and effort. It has no
  "Deviation:" prefix. The reason reads "out of credits", "rate limited" or
  "error exit".
  The role stays on the fallback row inside a round and across its fix rounds
  (`vendor.fallback-sticky`, narrowed by decision 244). The ordered role
  records determine the row from vendor, model, effort and exit.
  *Proposed (S255; decisions 214, 215, 216):* a retry that starts a fresh
  round restarts every role's chain at its first row: a retry at a `rounds`
  pause and the resume of a stopped run (S296) set the run record's
  `chain_from` to the number of role records, and the row is then derived from
  the records from that index on. Other retries (error, lost, shape, budget,
  interrupted) rerun the same role and round and keep the sticky row. The
  restarted row still goes through the vendor out marks (S169): a vendor that
  is still out is skipped, so the chain starts at the first row whose vendor is
  back. Session resume is unchanged (same vendor and model), and a brief for
  the first row has no earlier-rows note.
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
- **Local launchers** (S301, adj 183, 188). Matrix Agent values
  `claude-local`, `codex-local` and `grok-local` select the existing adapters
  with those exact binary and vendor names. A missing launcher on PATH is
  skipped in a recommended or an explicit run chain, with its name in the
  plan's note.
  The launcher inherits the daemon environment. Cloud `vendor_env` entries
  do not apply to a local twin; ork sets no local endpoint or credentials.
  Model, effort and resume arguments follow the existing adapter rules.
  The owner selected `latest` for a grok-local row: Grok's existing rule
  omits `-m` for that value, leaving model selection to the launcher.
  Claude-local and codex-local pass their row model and effort. Grok-local
  retains Grok's read-only restriction; write roles remain S305. Local rows
  are opt-in and never become a default primary. Fixture candidates use
  `fixtures/<local-vendor>/`, and run-tree costs say `local` without dollars.
  New endpoint-error classification remains S303 and quality measurements
  remain S304.
  Local briefs add targeted-read guidance (adj 308): use offset and limit or
  grep first, never read a whole file over about 500 lines, and keep reasoning
  brief. Cloud briefs omit that paragraph.
- **Vendor slots and the vendor-out memory** (S169, *proposed*; owner decisions
  2026-10-29). One `engine.Vendors` value in `engine.Deps` is shared by every
  run of the daemon, as `Landings` is. It is in memory only, so a daemon
  restart clears it. A nil value means no cap and no memory, so engine tests
  that set none are unchanged.
  - *Slots.* A round that starts a vendor process first takes that vendor's
    slot and frees it when the process ends, except a local role in a
    local-primary run, which uses its run's reservation serially. The
    built-in caps are `codex` 1
    and `claude` unlimited. The three local launchers share four slots total
    (S301, adj 187), independently of cloud vendor slots. Rounds on a full
    vendor wait, oldest first. The
    wait comes before the ledger step and the role record, so it is not role
    run time (the budget never counts it) and not silence for the watchdog.
    A frontier-primary run keeps its global-cap slot while it waits, and its Running row says
    `waits for codex`; its heard cell is dim, with no `!` or `!!` cue, while it
    waits. *Proposed (S194):* the wait keeps the ledger run fresh with a
    heartbeat on its orchestrator step every 10 minutes, when that step exists
    and the ledger run has not ended. `ork stop`
    cancels the wait; it leaves no role record and no ledger step. The
    reviewer and the UI pass of one state can wait for each other's slot, but
    they cannot deadlock: each round holds one vendor slot only, and a slot is
    freed when its process ends. Cloud spec-check rounds (`speccheck.go`)
    take no slot. Local spec checks take a slot from the same shared local
    pool and use the same 90-minute elapsed limit (adj 315); their ledger step can
    appear before the slot is free. Their output does not extend the limit.
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
    holds until `back` (owner decision 2026-10-29), or ends at the time given
    with `--until TIME` (S299, *proposed*). Owner marks are written to
    `vendors-out.json` in the state directory (docs/state.md, Vendor memory)
    and loaded when the daemon starts, so a restart or an idle exit keeps
    them; exit marks are memory only. ork reads no reset time from a vendor's
    message: a real captured message with one must exist as a test fixture
    first. A second exit mark renews the exit text and the expiry and keeps the
    first time; an exit mark does not change an owner mark, with or without an
    end.
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
    (`codex is out until back (...)` for an owner mark with no end). The end
    takes the day before the time, as `4 Nov 07:12`, when it is not the day the
    mark began, and the year when that differs; a start that is not today
    takes the day too. The note also gives `ork vendor back NAME`. `ork status`
    and `ork vendor` say it the same way. A plan the owner confirmed before the
    note changed is refused as changed.
  - *Wire.* `vendors.list` and `vendors.set` (params
    `{vendor, out, reason, until}`). `until` is an RFC 3339 time that only an
    `out` call may set and that must be after now. The vendor must be one ork
    can run, `claude`, `codex` or `grok`, else `bad_params`. A mark or a back
    that cannot be written to the state directory is refused with an
    `internal` error and changes nothing. `status` and `snapshot` carry
    `vendors_out`, and an engine run row carries `vendor_wait`.
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
  mode, except claude-local (adj 307): its fixed read-only allowlist replaces
  plan mode to avoid the model-backed classifier. It allows `Read`, `Grep`,
  `Glob`, `git diff`, `git log`, `git status`, `git show` (also with `git -C`),
  `slicer show`, `slicer goals`, `slicer list`, `slicer deps`, `slicer find`,
  `adj show`, `adj check`, `go test`, `go vet` (also with `go -C`)
  and `gofmt -l`, with trailing argument wildcards. It allows no Edit, Write,
  broad Bash or tracking-write commands. Cloud permissions stay the same.
  An implementer with no entry (or an empty list) gets the built-in
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
  *Proposed* (S265): the built-in default is the `go` preset, and `python`
  is the second one. Both start from the same base, today's default without its
  five Go entries (`Bash(go -C * test*)`, `Bash(go -C * vet*)`,
  `Bash(go test*)`, `Bash(go vet*)`, `Bash(gofmt *)`); `go` adds them back
  and `python` adds `Bash(python3 -m unittest*)`; each ends with
  `Bash(<check command>*)` when the command is set. `ork projects set --preset`
  writes the expanded list into the registry as plain patterns. Broader
  Python forms such as `Bash(python3 *)` are left to `--allow`.
- **Credentials:** ork uses each CLI's own login. It never reads, stores
  or passes credentials.

- **Codex adapter** (`internal/agent/codex`, S47). A fresh round runs
  `codex exec --json -m MODEL -c model_reasoning_effort=LEVEL -s MODE
  -C WORKTREE PROMPT`. MODEL is the lineup entry's `model`, which is the
  resolved name for a `FAMILY@latest` row (S244, *proposed*). MODE is `workspace-write` for write roles and
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
  `TEMP/gocache` and `TMPDIR` to `TEMP` (S271, *proposed*), replacing any
  inherited value of either and keeping other entries.
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
  stream-json --verbose --include-partial-messages [--model M] [--effort E]`
  (M is the lineup entry's `model`, the alias itself for `opus@latest`;
  S244, *proposed*),
  then, for claude-local only (S360, *proposed*; owner decision 2026-11-12),
  `--disallowedTools WebFetch,WebSearch,Task,Agent,NotebookEdit,Skill
  --disable-slash-commands`, which leaves the role less fixed context to
  prefill each turn (`claude --help` lists both flags). A UI pass keeps
  `Skill` and the slash commands, since its brief names skills to load. The
  flags come before `--allowedTools`, because `--disallowedTools` takes a
  variadic list. A claude-local role's environment also sets
  `BASH_MAX_OUTPUT_LENGTH` to a character cap on one Bash result
  (`localBashOutputCap`, `internal/daemon/runner.go`), replacing an inherited
  value. The owner measured the variable on 2026-11-12 with claude 2.1.322
  and a cloud model (the cap is client-side): unset, an 18.9 KB output
  reached the model whole; with `BASH_MAX_OUTPUT_LENGTH=2000` the same
  output became a "persisted-output" notice (the full output saved to a
  file) plus a ~2.2 KB preview. The owner chose 10000 for local roles. Cloud
  roles get neither change.
  Then `--allowedTools a,b --permission-prompts none` for a write role or a
  read-only claude-local role (using its fixed read-only list), and
  `--permission-mode plan` for a cloud read-only role, then the prompt. `--effort
  <level>` is listed by `claude --help` ("Effort level for the current
  session", claude 2.1.x). When the role has a temp directory, the process
  environment (the daemon's, plus the project's `vendor_env` for `claude`)
  sets `TMPDIR` to it (S271, *proposed*), replacing any inherited or
  `vendor_env` value, so the role needs no `env TMPDIR=DIR` prefix and no
  `mkdir`; the engine makes the directory before the role starts
  (`internal/engine/round.go`, `internal/engine/speccheck.go`). Whether the
  Claude CLI's own tool shell keeps `TMPDIR` is *unverified*. The process runs in its own process group, using
  `internal/clirun`'s helpers (`code.process-groups`). `stream_event` text
  deltas reach the output tail as they arrive. Session id, final text,
  tokens and `total_cost_usd` come from the `result` event. Cancelling the
  context kills the whole group. The report is the report file, else the
  result text (`role.report-io`). The exit class is:
  - ok: exit 0, `subtype` `success`, `is_error` false, including denials.
  - denied: not ok and `permission_denials` is non-empty.
  - connection: `api_error` is `connection_failed` (S329, adj 291).
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
  (`testdata/vendors/codex/06-resume-workspace-write.stderr`). grok exits 1 on the 403
  (`testdata/vendors/grok/07-out-of-credits-403.exit`) and on an unknown model
  (`testdata/vendors/grok/06-bad-model.stdout`, `.exit`), and 0 on the other five
  probes, including the plan-mode run that refused to edit. A denial or a
  blocked write is not an exit code, so the classifier must read the output.
- **Plan mode (claude).** No edit was attempted: `permission_denials` is empty
  and `notes.txt` is unchanged (`testdata/vendors/claude/05-plan-mode-edit.repo-status`
  is empty). The result text says a plan was saved under `<home>/.claude/plans`.
  That was not checked, and nothing under `~/.claude` was opened. A worktree
  fingerprint would not see it. This probe is also the sonnet call above.
- **Grok auth.** `grok models` printed "You are not authenticated."
  (`testdata/vendors/grok/models.txt`). The `-p` call still reached the API with
  the init event's auth source `oauth`
  (`testdata/vendors/grok/02-stream-messages-json.stdout`, field name redacted).
  The first run had no credits left (HTTP 403); the S72 re-run did. No login was
  attempted, and no command was found that reports login state without a model
  call (`grok models` says "not authenticated" while `-p` works), so `ork doctor`
  has no grok login line.
- **Grok first run and S72 re-run.** In the first run (2026-10-18) probes 01, 02,
  04 and 05 reached the API and got HTTP 403, probe 03 got HTTP 404 (it used a
  team id as the session id) and probe 06 failed on the model name. The 403
  sample is kept as `testdata/vendors/grok/07-out-of-credits-403.*`. The S72
  re-run (2026-11-03, one call each, run by the orchestrator) replaced probes 01
  to 06: 01 to 05 exit 0, 06 exits 1 with "unknown model id". Three more probes
  (10 to 12, grok-4.5) tried a write: `dontAsk` and `acceptEdits` both refused
  every edit and write in a headless run (see the allowlist row). Gaps left: no
  successful write, no rate-limit sample, and probe 04's `--tools Read` did not
  show whether `Read` is a tool name.
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

*Proposed* (S290): `ork COMMAND --help` and `-h` print that command's blocks of
`ork help` on stdout and exit 0, without the daemon. A help flag is recognised
wherever the command's flag parser accepts flags, so it may follow the
arguments (`ork retry RUN --help`). It is not recognised where the usage text
makes the argument literal: `ork note`'s TEXT after ITEM and `ork adj ask`'s
TITLE stay literal (`ork note P ITEM --help` adds a note). A group (`wave`,
`adj`, `projects`, `settings`, `vendor`, `roles`, `daemon`) prints all its
blocks. `ork help COMMAND` prints the same text, and an unknown COMMAND is a
usage error; a help flag among its words changes nothing, so `ork help -h` is
`ork help`. The blocks are cut from the one usage text (`usageText`), so help and
`ork help` cannot differ. The one exception is `ork daemon run`, the daemon's
entry point: both `ork daemon run --help` and `ork help daemon run` print its
own line, `usage: ork daemon run`.

```
ork                         open the TUI (start the daemon if needed)
ork status [--project P]    waves, slices, roles, needs-you
ork stats [PROJECT] [--since DATE] [--json]
                            slicer's relay-run statistics (S123, proposed)
ork wave suggest P          slicer's suggested waves, in order, with reasons
ork wave start P ITEM...    check the specs, then launch (asks unless --yes)
ork wave define --project P NAME ITEM... [--replace]
                            save the items as waves.json's wave NAME (S215,
                            proposed)
ork wave list [--project P] list saved waves with item status (S215, proposed)
ork wave rm [--project P] NAME
                            remove a saved wave (S215, proposed)
ork wave start [--project P] NAME
                            start a saved wave (S215, proposed)
ork run P ID                show a plan, check the spec, then launch one slice
ork approve RUN [--later-checks]
                            finish a passed slice, commit and land it; with
                            --later-checks, mark every open human check later
                            first (S316, proposed)
ork check RUN N [STATE]     mark human check N: ticked (the default),
                            later or open
ork check RUN all later     mark every open human check later; no approval
                            (S316, proposed)
ork fix RUN TEXT            send a fix note to a fix round
ork answer RUN TEXT         answer an owner question
ork file RUN N              file a later finding through the daemon
ork new P TITLE [FLAGS]     file a slice from a request or note
ork dismiss RUN             clear a stopped run from Needs you
ork retry RUN [TEXT]        retry a paused role or step; TEXT is a note for
                            the rerun (S256, proposed); resume a stopped
                            run from its worktree, --rounds N more rounds
                            (S296, proposed); after two shape pauses in a
                            row a plain retry is refused, --accept-shape
                            keeps owner findings that have no options and
                            --again reruns the role (S298, proposed)
ork stop RUN|WAVE           stop and record why
ork daemon run              serve the daemon in the foreground
ork daemon stop [--force]   stop the daemon; on a daemon that predates
                            daemon.stop, print the pid and a kill hint
ork daemon restart [--force] [--rebuild]
                            stop and start the current executable
ork roles print [ROLE]      print the embedded role prompts
ork roles matrix P          show the role matrix as slicer prints it, then
                            which rows ork uses, skips or does not run (S168)
ork projects add|list|rm|set|pin|unpin
                            manage the registry
ork doctor [--upgrade]      check versions, CLIs, logins, contract, ork path,
                            projects; print upgrade commands (S178)
```

S03 ships `status`, `projects`, `roles print` and `doctor`
(`cli.s03-commands`) in `internal/cli`; the binary wires them in S13
(`cmd/ork`). The rest
come with the engine. S17 adds `run`, `approve`, `stop` and `answer`
(`engine.owner-surface`, adj #73).
S44 adds `retry` for pauses without a question (`engine.pause-resume`, adj #101).
S60 adds `wave suggest P`, `wave start P ITEM...` and `stop WAVE`.
*Proposed (S215; owner decisions 2026-11-07):* `wave define`, `wave list` and
`wave rm` manage saved waves, and `wave start NAME` starts one. `--project P` is
required on `define`, optional on `list`, and optional on `rm` and `start` when
the name is saved for one project only (the saved wave then supplies the
project; a name saved for several projects is a usage error that names them).
`wave start` with one argument is a name; two or more stay `PROJECT ITEM...`, and
`--project` with the typed form is a usage error. `define` refuses a name the
project already has unless `--replace`. `list` prints a block for each wave,
`NAME  PROJECT` and then `  ITEM  STATUS  TITLE` lines (`--json` prints the
`wave.list` result). `start NAME` reads the wave with `wave.list`, prints a line
`skipped ID: STATUS` for each member it leaves out (on the prompts stream:
stdout, or stderr with `--json`), and then plans, checks the specs, confirms and
starts the others as `wave start` does, with the same flags for every member and
`name` set on `wave.start`. A wave with no member left to start prints
`no item is ready to start`, makes no `wave.start` call and exits 1; with
`--json` the result is `{"started":false,"held":[],"no_spec":[],"skipped":[...]}`.
The JSON result of a started saved wave adds `name` and `skipped` (an empty
array when none). `ork stop` still takes only a run id or a `wave-` id.
S116 makes `wave start` run the spec check after it shows the plans: each
open question is printed with its options and read from standard input (a
number picks an option and a number outside the options is asked again,
other text is the answer, an empty line holds the item back). Each question
is labeled with its item, as `[S1 1/2]`. An item whose questions stay open is
left out of the wave and named as `held`; a wave with no item left does not start and exits 1. With
`--json` the questions and the shared `Owner needed` text go to stderr.
*Proposed (S264):* before it calls `spec.check`, `wave start` prints one line
on the same stream, `spec check: S1, S2; wait for it to finish`, because the
call returns only when the role has finished; `ork run` prints it the same way
with its one item (`spec check: S1; wait for it to finish`).
The wave start or cancellation JSON result includes a `held` array of excluded
item IDs; it is `[]` when nothing is held. The `--questions` result and an
incomplete `--answer` batch instead return the spec-question object
`{questions,answered}` without `held` (*proposed*, S312).
*Proposed (S283, adj 245, `wave.plan-failure-missing-spec`):* a missing spec is
the one plan failure that does not abort the wave. `spec.check` answers an
item whose `slicer show` has no slice with a row `{"item","ready":false,
"no_spec":true}` instead of an error (`engine.ErrNoSpec`); no role runs for it
and the daemon remembers nothing. `wave start` leaves such an item out before
the sitting and the confirmation, prints `ID: no slice spec; run slicer
promote ID` on the prompts stream (stderr with `--json`) and starts the rest.
Exit is 0 when a wave starts; with no item left it does not start and exits 1.
The wave start or cancellation JSON result has a `no_spec` array of skipped
IDs beside `held`, `[]` when none. Every other failure, including a `run.plan`
error, still aborts before anything starts. `ork run` leaves the same item out, prints the
same line and exits 1; its `--json` result has no `no_spec` array. The TUI is
unchanged (adj 218).
S167 gives `ork run` the same sitting after the plan and before
`Start this slice? [y/N]`. A number picks an option; text answers directly.
An empty line or end of input holds the slice and exits 1, without starting.
`--yes` skips only confirmation and still runs the spec check. A failed
check blocks launch. With `--json`, the sitting goes to stderr and stdout
holds one result; a held slice returns `{"started":false,"held":["ID"]}`.
*Proposed (S312):* `run` and both forms of `wave start` accept `--questions`
and repeatable `--answer`. Listing runs only `spec.check`, prints every
question with its number, decision key when present and answer address, and
returns without planning or starting a run or recording an answer. It exits
3 (`ExitWaitQuestion`, S257) with questions pending, otherwise 0. The spec
check's existing cache is retained. Text listings with no open questions
print `no open spec questions: S1, S2`; a saved wave with no startable items
names the wave instead. Keyed wave questions show
`Decision key: k1 (answer as S1:k1)`; keyless questions show `Answer as: #N`
for run or `Answer as: ITEM:#N` for wave. `--questions` refuses `--yes` and
`--answer`. Run answers use `KEY=value`; wave answers use `ITEM:KEY=value`.
For a question with options, an in-range integer selects its option. An
integer outside that range, including one that overflows, is a usage error;
any other nonempty value is free text. The CLI
validates the whole batch before the first `spec.answer`: unknown, duplicate,
empty or out-of-range answers are usage errors naming the address. Missing
answers list their addresses and hold the whole batch with exit 3 and no
recording. Complete batches use the existing `RecordSpecAnswers` path, so
the Owner decisions paragraph retains each decision key. Answers with no
pending question are refused. With `--json`, batch results include
`questions` (entries with `item`, `address` and the embedded
`engine.SpecQuestion` fields: `ask`, optional `key`, `text`) and `answered`
(`{item,key,answer}`, where keyless questions use `#N`
and answer is the recorded `Label: summary` or free text). No interactive
prompt text changes.
The Roadmap launch uses the same sitting after its plan and before its
confirmation (S184). Skipping leaves the slice held and closes the launch;
a failed check blocks launch with no skip key. Editing the confirmed lineup
replans without another spec check.
`ork run` refuses a blocked item through the spec check (S181); raw
`run.start` does not read dependencies yet.

Suggestions call `slicer plan-parallel --json --lean --root ROOT` for the
registered project. Tool errors keep their code and message, including a
missing `files_section`. The owner chooses the item ids (`wave.start-input`).
The CLI plans each item as well specified = yes and split = no, shows each
`run.plan`, then asks `Start this wave? [y/N]` (*proposed*, adj 189,
`launch.well-specified-default`). It asks neither input. `--well-specified`
and `--split` are optional overrides that apply to every item; `--yes` needs
neither. In JSON mode, prompts and plans go to stderr and stdout holds one
result.

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
vendor and effort are the first and last parts. The vendor is read in any case
and kept lowercase (`ork vendor out|back NAME` reads its name the same way), and
the CLI refuses a name that is not in the daemon's list of vendors
(`daemon.Vendors()`, the one list behind `supportsVendor`) with that list;
`run.plan` and `run.start` lowercase the vendor of each wire entry and refuse an
unknown one with the list too (*proposed*, S291). `--architect off` alone
removes the architect. Gate follows reviewer and the UI pass stays from the
matrix; neither has a flag. `--budget-minutes N` and `--budget-tokens N`
accept whole numbers of zero or more; zero uses the project default. Omitted
flags keep the recommendation and project defaults. Invalid flag syntax,
empty parts, mixed architect off and entries, and negative budgets exit 2
before a daemon call, and so does an unknown vendor. Model and effort
validation stays in the daemon.
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

`ork run P ID` shows the plan, runs the spec check, then asks
`Start this slice?` (`[y/N]`, Enter takes No). It asks neither `Well
specified?` nor `Split?`: the plan is made as well specified = yes and
split = no, and the spec check holds an item that is not Ready, which is the
test rule u4 of the matrix names for "well specified" (*proposed*, adj 189,
`launch.well-specified-default`, replacing adj #86 and #87 once the owner
supersedes them). `--well-specified` and `--split` stay as optional
overrides; they take y, yes, n or no in any case, and any other value is a
usage error (exit 2). `--yes` needs neither flag.
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
`{project,item,well_specified,split,lineup?,budgets?}`. The wire is unchanged
(adj 189). The CLI sends `well_specified: true` and `split: false` unless the
owner overrides them with `--well-specified` or `--split`; the TUI always
sends `well_specified: true` and `split: false`. `lineup` maps each
overridden role to its full chain of `{vendor,model,effort}` entries
(`lineup.launch-edit`); a `model` of `FAMILY@latest` is resolved as a matrix
row's is (S244, *proposed*). Start also accepts `expected`, the plan the owner
confirmed, including lineup and merged budgets. `run.approve`, `run.stop`
and `run.answer` take
`{run,text?}`. *Proposed (S237):* `run.answer` and `run.retry` also take an
optional `no_wait` boolean, which only the CLI sends. With it, the reply
returns the run record once the engine has applied the action and the run has
left its pause, and the job goes on under the daemon; a failure of the job
shows in `ork status` as the run's error. A refusal or a failure that comes
first is the reply. Without it (the TUI), the reply waits until the run rests.
A second owner action is refused as busy until the job ends.
Plan returns `{project,item,worktree,branch,check_command,round_check_command?,
lineup,budgets,permissions,note?,warnings?}`; the project contains only its name and path.
Each lineup entry is `{vendor,model,effort,requested?}`: `requested` (S244,
*proposed*) is the matrix text that `model` was resolved from, and is absent for
a pinned model.
The proposed optional `note` (S135) explains a failed `relay-roles show` read.
The plan then treats UI and spec-check rules as absent; a failed recommendation
still prevents planning. `ork run` and the TUI launch confirm show the note
under the plan rows (S159, *proposed*).
The proposed optional `warnings` (S265) lists setup problems
(`engine.SetupWarnings`), in this order: no check command; the built-in Go
allowlist on a project with no `go.mod`; an implementer allowlist without
`Read`, `Edit` or `Write`; an implementer allowlist with no pattern for the
check command. *Proposed (S295):* a last warning, `check command cannot run in
DIR: PROBLEM (command: CMD) (fix: ork projects set NAME --check CMD)`
(`--round-check` when the round command fails; the TUI launch confirm shows it
by the check line instead of after the roles, where a long plan can clip it), when
the static check (`engine.VerifyCheck`) of the check command, or of the round
check command, fails in the project path; the worktree does not exist yet, so
the project's folder of the main checkout stands in for the folder the check
runs in, and setup verifies again in the worktree. The daemon checks the project path for `go.mod` and
`pyproject.toml` when it plans. `ork run` prints each as `warning: LINE` after
the role rows and before the note, and the TUI launch confirm shows each in
the Warn style, wrapped. Both show an empty check command as `check: none`.
The `expected` plan check compares the JSON, so a plan with warnings starts as
any other. A start on a project with no check command is refused (adj 191,
`launch.no-check-command`, *proposed*): `run.start` carries `allow_no_check`
(`ork run P ID --allow-no-check`; `ork wave start` sets it on every item), and
without it the daemon replies `refused` with the message `project NAME: no
check command: ork refuses to start a run (fix: ork projects set NAME --check
CMD, or start with --allow-no-check)`. `run.start` makes this refusal before
the spec gate asks anything, and `wave.start` before it admits a member. `ork
run` and `ork wave start` refuse right after they print the plan, before the
spec check and the confirm; the plan text leaves out the daemon's no-check
warning (the error prints it), and with `--allow-no-check` the plan warns
`no check command: only slicer check runs after landing` instead. The TUI
launch confirm shows the refusal, the `ork projects set` fix and the `ork run
... --allow-no-check` command in the Warn style by the check line, offers no
`y` in its bar or hint, and starts nothing on `y`; the TUI has no way to give
the flag.
Start and owner commands return the run record. With `--json`, run prints
the plan on stderr and a single result JSON value on stdout; declining
prints `{"started":false,"held":[]}` on stdout. Interactive questions go on
stderr in JSON mode. Real spec-check failures carry the `spec check failed: `
prefix in text mode; refusals do not.

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

`run.retry` takes `{run}` and returns the run record (adj #101). *Proposed
(S294):* `rounds` is an optional positive integer at a `rounds` pause; absent
or zero grants two rounds. Other pause kinds refuse a nonzero count. *Proposed
(S256):* it also takes an optional `text`, the owner's note for the rerun. The
brief gets it as `## Owner note` after the pause's own section, when the retry
runs a role (a retried step, such as the checks, runs no role and the note has
no reader). It is held in `pending.note`, or in
`pending.retry.note` while a sibling's queued question is asked first, and
leaves the record when the round ends. A question pause still refuses a
retry. *Proposed
(S237):* it also takes `no_wait` (see the wire fields). `run.answer` and
`run.retry` with a slicer relay run id, not an ork run id, answer `not_found`
with `no run ID; that is the slicer run of ork run ORKID`; the id is not an
alias.
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
*Proposed (S316; adj 233, 234):* `run.approve` takes `later_checks` and
`run.check` takes `all`. With `later_checks`, `Engine.ApproveLater` marks every
open check `later` (the state `MarkCheck` writes for one), then approves under
one engine lock; if the approval fails, the checks it deferred are open again.
`run.check` with `all` and `state` `later` calls `Engine.MarkOpenLater` and
approves nothing; `all` with any other state is refused (ticking says the owner
looked, so it stays per check). A check already `ticked` or `later` is never
changed. Both replies carry `deferred`, the checks the call marked, each as
`N. TEXT` with TEXT cut to its first 80 characters (`...` marks a cut). The CLI
prints one `deferred: N. TEXT` line for each, then `RUN STATE`; with `--json`
it prints the reply. A plain `run.approve` is unchanged, and its refusal now
names `--later-checks`. The flag is the owner's explicit choice, never a
default. The follow-up list is built from the card at landing, so the later
checks reach it as they do after `ork check RUN N later`.
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
code `internal` (no new wire code) and the message `the record of run ID is
unreadable: WHY`, where WHY is the cause without the record's path (S229).
`run.report` and the run actions that open a finished run (such as `run.file`)
answer the same way, where they used to say `not_found` `no run ID`. The TUI calls it when the owner opens the
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

*Proposed (S297):* `run.report` also serves a check log. The name may be one
listed in the stored record's `checks[].log` (`checks-ROUND.log`, and
`land-checks-ROUND.log` from the landing re-check), and the file
may be a regular `.log` file; every other guard above holds (plain name in the
run directory, no link, the same file as listed, plain text, at most 64 KiB).
Nothing else is served: no live tail, no `run.json`, no vendor output and no
fixture directory (owner decision 2026-11-05, Q3 and Q4). The check log is a
raw tail of each command's output (30000 bytes per command) and
`slicer check --json`, so it can hold what a command printed.

*Proposed (S297):* `run.show` takes `{run}` and returns what the stored record
says of a run, as file names and text fields, never file text: `id`, `item`,
`state`, `reason`, `next`, `pending` (`kind`, `state`, `role`, `round`,
`reason`, `question`, `ask`, `key`; no owner retry note), `roles[]` (`role`,
`round`, `attempt`, `exit`, `report`, `denied` as a count and `denied_first`
as the first three denied commands), `checks[]` (`round`, `command`, `ok`, `seconds`, `log`, `deferred`;
not the record's log hash), `findings[]` (`n`, `kind`, `title`, `text`, `ask`, `answer`,
`filed`) and `latest_report`. For a paused run `reason` is the whole stored
pause reason, without the `; next: ...` suffix that `ork status` adds, and
`next` is that suffix's command, set for every paused run, also one whose
record has no pending. `latest_report` is the newest report of the
pending role and round, else the last role record that has a report.
Like `run.report` it reads the stored record without waiting for the run and
writes nothing. An unknown run or a run id that is not a plain name is
`not_found`, and an unreadable record is the `internal` error above. The CLI
command `ork show RUN [NAME] [--json]` calls it and `run.report`: with no NAME
it prints the header (`ID ITEM STATE`), the pause (kind, role and round) with
its whole reason, the question with its numbered options and `Recommend`
when the kind is `question`, the next command, each role attempt with its
report name and, when it had denials, `denied N:` and the first three, the
checks with their log names, the findings with their text and, for a paused
run, `latest_report` (the pending role and round's newest report when it has
one, else the last report of the run) under a `-- NAME --` rule, followed by
the `next:` line again so a long report does not scroll the command away. With a NAME it
prints that one report or check log, and an unknown name exits 1 with the
names the run has. Both forms take `--json` (the `run.show` reply, or
`{name,text}`). `ork status` is unchanged.

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
`run.retry` of a stopped run (S296, *proposed*) takes `{run, rounds?, no_wait?}`
and resumes it, from the store when the manager holds none; refusals are
`refused`. `run.start` answers the run record plus an optional `message`
(a bare-record reader ignores it), and each `wave.start` item has an optional
`message`: ork resumed the item's stopped run, or started a new run and why.
`run.show` and `ork status` name `ork retry RUN [--rounds N] or ork dismiss
RUN` for a stopped run that can resume.

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
  prints all three whether or not anything is behind. Contract reasons say
  `older than the versions ork accepts (v11 or v12)` or
  `newer than the versions ork accepts (v11 or v12)`. With `--upgrade`, a
  current slicer or adj reason includes `(installed: VERSION)` when known.
  Text prints `[upgrade] TOOL  REASON`, an optional `note:` line, then each
  command on its own line, indented, after the contract line. slicer: a checkout
  gets `git -C DIR pull --ff-only` and `DIR/.venv/bin/pip install -e DIR`, a
  venv gets `V/bin/pip install --upgrade squalor-slicer`, with the note `PyPI
  install assumed; a checkout venv needs git pull and pip install -e`.
  Recognising checkout venvs outside `.venv` is not part of this hint. adj and
  ork get a pull and the documented build with the installed path filled in and
  the checkout named as a placeholder (`ADJ_CHECKOUT`, `ORK_CHECKOUT`), and ork
  also gets `ork daemon restart` by its installed path. A source that is not
  recognized gets a `note` naming the sibling README section and no commands.
  Doctor reads only local facts: symlinks, build information, and `git -C DIR`
  `rev-parse`, `describe` and `rev-list`. It never fetches, pulls, builds, makes
  a network call or runs a printed command, and a hint never changes the exit
  code;
- the slicer contract: the `contract` string of `slicer ai relay --json --root
  ROOT` against the accepted set (`roles.AcceptedContracts`, v11 and v12;
  `boundary.contract-set`, adj 326, which supersedes adj 175). Any other version fails. The check runs
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
  required conformance keys (including `role_fallback` and `blocker_wait`),
  ledger role names (including `gate`), gate criteria and verdicts, outcomes,
  and on v12 the `tool_feedback` key. The run records its version, roles and
  gate criteria for resumed runs. A gate round closes its ledger step with
  `--phase done --verdict pass|fail`. A gate report with no valid verdict line
  closes without one. *Proposed* (S204): a gate round closes with its verdict
  only when the engine accepts the round; otherwise it closes without one, and
  the retried round is a new ledger step. Embedded role prompts
  are written against v12;
- each project's path exists and is a directory; a configured check command
  passes `sh -n -c` and its first word after leading `VAR=value` assignments
  resolves on PATH, or against the project directory when it contains `/`.
  Lookup is skipped for POSIX reserved words and special builtins, plus `cd`
  and `command`, plus `time`, and words containing `$` or a backtick or
  starting with `~`. A syntax finding drops the shell prefix up to its first
  marker: `sh: -c: line N: ` or `sh: N: `, and any text before that `sh: `.
  *Proposed (S295):* doctor uses the shared static check (`engine.CheckFaults`,
  the one `projects set` and run setup use): it also resolves the first word of
  each later simple command (a relative path after an earlier step is not
  looked for), skips the builtins of `sh` such as `echo`, `test` and `true`,
  and names a plain file the command lists that does not exist in the project
  directory (`check_command: "App.slnx" does not exist in project directory`).
  Doctor never executes the check command. Nonempty worktree and branch
  patterns must contain `{id}` or `{id_lower}` (*proposed*, S124); empty
  patterns use the defaults. A finding names the fix, `ork projects set NAME
  --worktree-pattern PATTERN` or `--branch-pattern PATTERN` (S161).
  *Proposed* (S265): a project whose path is a directory also gets the
  `engine.SetupWarnings` lines as findings: no check command (worded as the
  refusal, adj 191), the Go allowlist on a project with no `go.mod`, an
  implementer allowlist without `Read`, `Edit` or `Write`, and one with no
  pattern for the check command. Each names its fix. Advisory project findings
  print `[WARN]` and keep exit 0, the no-check finding included (owner-side
  decision of the S265 UI pass).
  Doctor also reads the distinct `merge=NAME` attributes in each project's
  `.slicer/.gitattributes` and checks `git config --get merge.NAME.driver`.
  Git's built-in `text`, `binary` and `union` need no configuration and are
  excluded. Missing slicer drivers produce `merge drivers missing: NAME, NAME
  (fix: run the lines slicer setup-git prints)` and the commands printed by
  `slicer setup-git --root PROJECT` (S328, adj 288 and 289). Without `--fix`,
  doctor only prints the commands. With `--fix`, its project header names the
  missing drivers, then it shows the commands and asks
  `Configure slicer merge drivers? [y/N]` per project, with Enter meaning No.
  Yes executes the printed lines in that project, configuring the clone's
  git merge drivers. Success replaces the missing finding with
  `merge drivers configured: NAME, NAME`; the project shows `[ok]` when no
  other warning remains. Failed setup or execution is reported; a failed fix
  exits 1. JSON project entries include `merge_driver_commands` when available;
  prompts and command previews go to stderr with `--json`. The registry stays
  read only. The explicit confirmation authorizes the git config change.
  The registry is read only; a missing registry is empty and is not created.
  A registry read error is advisory: JSON has `registry_error` (omitted when
  there is no error), separate from the daemon's `warning`. Text prints
  `[WARN] registry  ERR` just before the project lines, with the leading
  word `registry` of the error text dropped so the label is not repeated;
  the JSON field keeps the full text. With a readable registry that holds no
  projects (missing or empty), text prints
  `[--] projects  none registered: run ork projects add PATH` in the same
  place. Neither line changes `ok` or the exit code.
  *Proposed (S314):* with a readable registry, text also prints one owner
  line before the project lines, in one of three forms (each fits 80 columns
  for a name of about 11 characters): `[ok] owner  NAME (SOURCE); spec answers
  are filed in adj`, `[ok] owner  NAME (SOURCE); spec answers not filed
  (file_answers off)` and `[--] owner  not set; spec answers not filed: ork
  settings set --owner NAME`. SOURCE is `ADJ_DECIDED_BY` when the variable is
  set, else `setting`. The line speaks of spec answers only: run answers and
  ledger answers still need `ADJ_DECIDED_BY`. JSON has `owner` (empty when
  unset), `owner_source` (`environment`, `setting` or empty) and
  `file_answers` (the registry's value, true when the key is absent; false
  when the registry could not be read). The line does not change `ok` or the
  exit code.

Doctor exits 1 when slicer, adj or the contract check fails. Advisory project
findings do not change `ok` or the exit code. A failed `--fix` sets `ok` to false
and exits 1. `roles print`
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
  (`ui.denials-display`, `engine.denied-command`). *Proposed (S243):* text
  `ork status` shows `(denied: N commands, first: TEXT)` (singular `command`
  for one). It counts distinct texts and cuts the first to 40 runes, using
  37 runes plus `...` when longer. `--json` retains every full denial;
  the TUI keeps its existing three-and-more display. `wait`
  contains the approval prompt, owner question, pause or stop reason. The optional
  `pending` string holds the paused run record's pending kind (`question`,
  `shape`, `lost`, `interrupted`, `budget`, `rounds` or `error`); other states omit it
  (`ui.pending-kind`). The optional `question` object carries `title`,
  `options:[{label,summary}]`, one-based `recommend` and `reason` for a paused
  structured question. Older snapshots without it still load. *Proposed (S308):*
  `active_relays` holds slicer's active relay runs plus stale manual relays from
  the board read. Both count in the text's `active relays M` line.
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
- `projects add --scan DIR [--yes]` (S302, *proposed*): scans DIR with the same
  walk as the TUI checklist (`internal/projscan`; the folder itself, or two
  levels below it; stops at a project; skips hidden folders, `.worktrees`
  and `node_modules`; no symbolic links). DIR must be absolute and an existing
  directory; none found is `not_found`, exit 1. Text mode prints `found N
  projects under DIR:` and a row per candidate: `[x]` tracked and free, `[ ]` a
  plain git repo, `[-]` blocked (the path is registered, or another project
  holds the folder name, or another free row does) with the reason. Only the `[x]` rows are added: it asks
  `Add the N marked projects? [y/N]: ` on standard input (as for the offer
  above), or does not ask with `--yes`. With none to add it prints `nothing to
  add` and exits 0. Each row goes through `projects.add` one by one, named after
  its folder. Text: `added NAME PATH [(adj)]` or `failed NAME: MESSAGE` per row;
  any failure makes exit 1 with `N of M projects were not added`, and the others
  stay added. `--json` needs `--yes` and prints
  `{"root":DIR,"folders":N,"stopped":BOOL,"results":[{"name","path","added",
  "project"|"error"}],"skipped":[{"name","path","reason"}]}`; exit 1 when any row failed. `--scan` takes no
  PATH and no `--name`, and `--yes` is valid only with `--scan` (usage, exit 2).
  A plain git repo is listed and skipped; add it with `projects add PATH`, and
  a line `[ ] plain git repos are not added; use ork projects add PATH` follows
  the rows when one is listed (owner 2026-11-07: `--yes` stays tracked-only).
  Of free rows with one folder name one holds it and the rest are blocked
  (`name "N" is also found at PATH; add it with ork projects add PATH --name N`,
  PATH being the holder). A tracked row holds the name before a plain git repo,
  and rows of one kind go by path. A registered
  path matches after symbolic links are resolved. Names, paths and errors print
  as one plain line each. The scan quits after 500 folders or 200 projects: text
  mode then prints `stopped after N folders and M projects; scan a narrower
  folder to see more` (a limit hit in a nested folder ends the whole walk), the JSON has `"stopped":true` and `"folders":N` (both always present),
  and the none-found error adds the same words. The scan runs before the
  daemon is asked and is not asynchronous. No wire change: it uses
  `projects.add` and `projects.list`.
  An empty registry is valid; startup writes nothing and never seeds
  (`registry.no-seed`, adj 157). The first add writes projects.json.
  The TUI Welcome add guides registration through this same daemon method;
  its proposed confirmation and error layout is in dashboard.md.
- `projects rm NAME`: `{"removed":"NAME"}`. Removal is refused with
  *proposed* `project_busy` while any run of the project is not committed or
  stopped. This includes queued, paused, awaiting-approval and running states.
  The message names the run ids. The project stays registered.
  A planned start that loses the race to removal is refused with `not_found`.
- `projects set NAME [--push on|off] [--bell on|off] [--auto-land on|off] [--ui-doc PATH]
  [--ui-pass-skills A,B] [--spec-check-rows rNN,rNN] [--worktree-pattern P]
  [--branch-pattern P] [--check CMD] [--round-check CMD] [--no-verify] [--check-minutes N] [--cap N]
  [--budget-minutes N] [--budget-tokens N] [--env-line LINE]...
  [--preset go|python] [--permission ROLE=read-only|ROLE=]...
  [--allow ROLE=PATTERN]... [--replace-allow]
  [--vendor-env VENDOR:KEY=VALUE]... [--unset-vendor-env VENDOR:KEY]...`
  (*proposed*, S161; the last five groups S174): one flag per optional
  registry field, as in state.md. A flag that is given sets its field; a flag
  that is left out keeps the value. An empty string or 0 clears a field.
  `--ui-pass-skills` (*proposed*, S209) takes one comma-separated value
  (`a,b`) and replaces the JSON array `ui_pass_skills`. An empty value
  clears the list. The CLI trims whitespace around each name, so `a, b`
  stores `["a","b"]`. The shared validator refuses blank names, commas,
  leading or trailing whitespace, NUL, CR, LF and duplicate names in wire
  calls and on registry load and save. Errors quote the bad name; for a
  blank name they give the item's position, counted from 1. Duplicate errors
  quote the name and give both positions. `a,,b` and `a,` report item 2.
  Text uses `ui_pass_skills="a,b"` or `ui_pass_skills=unset`.
  `--spec-check-rows` (*proposed*, S258) takes one comma-separated list of
  matrix row ids (`r16,r24`) and replaces the JSON array `spec_check_rows`;
  an empty value clears it. The CLI trims whitespace around each id; the
  shared validator refuses an id that is not `r` and digits, and a duplicate
  (`bad_params`). Text uses `spec_check_rows="r16,r24"` or
  `spec_check_rows=unset`.
  `--check` and `--round-check` (*proposed*, S295) are verified before they
  are stored, in the project's folder of the main checkout, which is the
  registry path and the same folder the round check uses in the worktree
  (S293), not the top of the repository. The verification is static
  (`engine.VerifyCheck`) and runs nothing but `sh -n`: the shell must parse the
  command; the first word of each simple command must resolve (`PATH`, or
  relative to the folder; a path built by an earlier step is not looked for);
  and each later word that is a plain path (an extension of letters, or a
  leading `./`, `../` or `/`, or a trailing `/`; no glob, `$` or `~`; not a
  flag, a redirect target, the value of an output flag such as `-o`, a word
  after `-m`, or `./...`) must exist there. The words of `echo`, `rm`, `mkdir`,
  `touch`, `cp`, `mv`, `ln`, `tar`, `make`, `npm`, `git` and a few like them
  are not looked at, because they name files the command makes, and a `cd`
  ends the check. A `$(...)` is one skipped word and the check goes on after it;
  a subshell, a function definition or a here-document ends it (the words
  before a here-document are kept). A command that fails is refused with `bad_params`, in one
  line: `check command cannot run in DIR: PROBLEM (command: CMD); fix it, or
  store it anyway with --no-verify`. `--no-verify` (`no_verify` on the wire; not
  a field, so it alone sets nothing) stores it anyway. A project path that is
  not a directory is not verified (`ork doctor` reports it). `--no-verify` is
  remembered per project (owner decision 2026-11-10, adj
  `check.no-verify-at-setup`): the registry entry gets `no_verify_check: true`,
  and run setup, the implement-step verification and the plan warning skip the
  project's check commands. A call that sets `--check` or `--round-check`
  without `--no-verify` clears the key; a call that sets neither leaves it.
  `ork doctor` runs the same
  check on the stored command, in the project path, and adds the findings for a
  later command's first word and a named file that does not exist.
  S174 flags: `--env-line` repeats and the lines given replace the list (one
  empty `--env-line ""` clears it). `--permission ROLE=read-only` sets the
  role's preset and `--permission ROLE=` removes its entry, so the default
  applies; `ROLE=` also removes a role key that ork does not know when the
  file holds it (a hand-edited typo), while setting an unknown role is
  refused. `--allow ROLE=PATTERN` repeats and, since S265 (*proposed*, adj 190),
  adds the patterns to the role's list, from the built-in default when the role
  has no entry, and skips a pattern already there (the S174 decision made it
  replace the list; `--replace-allow` keeps that behavior). `--replace-allow`
  without `--allow` is a usage error. `--preset go|python` (S265) sets the
  implementer's list to the preset with the check command of the same call (or
  the saved one); `--allow implementer=...` after it adds, and `--preset` with
  `--permission implementer=...` or with `--replace-allow --allow
  implementer=...` is a usage error. The text line counts the roles changed
  by `--preset` and `--allow` and names the preset applied
  (`implementer:18 patterns (preset python)`). `--vendor-env VENDOR:KEY=VALUE` sets one key (the
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
  includes the `vendor_env` values. `--push` takes `on` or `off`. `--bell`
  (*proposed*, S224) takes `on` or `off` too and sets the registry field `bell`
  (`bell` on the wire, a boolean): `on` stores `true`, `off` stores `false`,
  and the text line says `bell=on` or `bell=off`. The TUI reads the setting at
  start, so a running TUI needs a restart to apply it. `--auto-land`
  (*proposed*, S331; adj 281) takes `on` or `off` and sets the registry field
  `auto_land` (`auto_land` on the wire, a boolean): `on` stores `true`, `off`
  stores `false`, an absent field reads as on, and the text line says
  `auto_land=on` or `auto_land=off`. A started run reads it at its gate, so
  the call is refused with `project_busy` while the project has unfinished
  runs, as `--push` is. At least one
  flag is required, and an unknown flag, a bad `--push`, `--bell` or `--auto-land` value or a non-number is a usage error
  (exit 2) before the daemon starts. The daemon (`projects.set`) validates the
  values and refuses with `bad_params` (a `ui_doc` that is absolute or leaves
  the project, an invalid `ui_pass_skills` name, an invalid `spec_check_rows` id, a pattern without `{id}` or
  `{id_lower}`, a negative number) or
  `project_busy` (while runs are unfinished, only for a call that sets a field
  other than `--cap`, `--bell`, the budgets and the worktree and branch
  patterns, S265 and S224 *proposed*; owner decision 2026-11-11: a started run
  does not read `bell`; the message names the refused flags), and then changes nothing. JSON:
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
  missing `--relay` (`projects pin needs --relay N (v11 or v12)`), an N
  outside the accepted set, or a wrong argument count. An unregistered NAME
  is `not_found` (exit 1), with a hint to run `ork projects list`.
  Exit 1 also covers when slicer fails the set or unset call (its message is
  shown), or slicer refuses the read-back after the set, for example a pin of
  v11 on a slicer that serves v12. In that case the pin stays set, and the
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
  (*proposed*, S314: `--owner NAME` and `--file-answers on|off` also set the
  registry's `owner` and `file_answers`; `show` prints `owner=NAME|unset` and
  `file_answers=on|off` after the two numbers; `--file-answers` takes only
  `on` or `off`)
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
  when the path cannot be read. In `contract`, `found` is `Contract: ` plus
  the `contract` string from `slicer ai relay --json --root ROOT`, where
  ROOT is the directory doctor runs in ("Contract: relay vN"), and `ok` is
  false when the global check or any project entry fails. Each
  `contract.projects` entry is a registered project asked with its own
  `--root`: `pinned` is the pin or null when the project is unpinned or when
  slicer refused (the `error` then names the pinned and the served version),
  and `found` is the bare contract string ("relay vN"). Both formats are
  kept on purpose. A failed project's text line ends with ork's next step,
  `run ork projects pin NAME --relay N ... or ork projects unpin NAME`,
  for a refused pin or a pin outside the accepted set.
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

*Proposed (S218):* `wave.suggest` and `ork wave suggest` put a second such
slice in a wave of its own after the first, so every suggested wave can be
launched as shown. In each of slicer's waves the first slice whose Files
`EngineSlice` flags stays; every further one moves, alone, into a new wave
right after that wave, with the reason `engine: ID` naming the slice kept.
The waves are renumbered 1 to n, so a slice that waited on a deferred one
still comes after it. ork reads each placed item's Files with `slicer show`,
at most eight at a time, and skips the reads when no wave holds two items.
A read that fails leaves that item as a plain slice and the suggestion still
succeeds; the refusal at `wave.start` stays the guard. A deferred slice runs
alone unless the owner adds another slice by hand. This is an exception to
showing slicer's suggestion as is (`wave.selection`; owner decision
2026-11-07).

*Proposed wire fields:* each status project carries `self_host` and
`behind_main`. A self-hosted daemon is behind when its build revision differs
from the tip of the branch the main checkout is on and is an ancestor of
that tip. Detached HEAD, an unborn branch or a failed Git read does not
establish that it is behind. The notice and wire field still say `main`.

*Proposed (S131):* each snapshot or status read checks the checkout with one
`git rev-parse HEAD --symbolic-full-name HEAD`. The daemon caches the result
by project path, build ID, tip and ref, with at most one entry per registered
path. A changed key replaces the entry; failed reads are not cached.

*Proposed behavior and wording:* `daemon behind main: restart with ork daemon restart`.
It fires on any landing on the base branch, not only engine landings, while
no run is active. In the status line it replaces the stale-build marker while shown;
otherwise the stale marker still shows for a differing build. Text doctor
prints `[WARN] daemon behind main: restart with ork daemon restart` on stdout.
Status prints `ork: warning: daemon behind main: restart with ork daemon restart`
on stderr. ork never restarts the daemon on its own.

*Proposed (S315; adj 231, 232):* a project's optional `build_command`
is set or cleared with `ork projects set NAME --build CMD`. Status carries
the command with the self-host flags. When it is set, status, doctor and the TUI status line use
`daemon behind main: restart with ork daemon restart --rebuild`. The TUI's
landed action line offers the same command after reading the post-landing
snapshot, only for a self-host project that is behind and has the command.
Without a command the notices keep the plain restart hint.

`ork daemon restart --rebuild` reads status before building. It refuses if a
relay or engine run has a role running, naming that run, even with `--force`.
An unknown or unreadable status fails closed. A non-self-host project is
refused; a missing command names `ork projects set NAME --build CMD`.
The CLI runs the command through `sh -c` in the registered project directory,
with a ten minute timeout and clirun process-group cleanup. A build failure
prints its last twenty output lines and `daemon not restarted`; the old
daemon stays running. Only a successful build reaches the existing stop and
restart path, whose busy check still applies if work started during the build.
Before stopping, the CLI validates and captures an on-disk executable for
the restart. A deleted-executable suffix is removed before validation. If
the installed file is missing, it says `daemon not restarted` and keeps the
old daemon running. A build that cannot start includes its underlying
start error when there is no captured output.
There is no automatic rebuild. For ork use
`ork projects set ork --build 'sh ./build-only-in-home-dir.sh'`.
This writes outside the workspace to `~/.local/bin/ork`, so the owner or a
session with approval must run it. `build-in-home-dir.sh` stays available
for manual build and restart. Doctor checks the build command without running it.

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
and add no persisted record fields. *Proposed* (S129): a finished role has
recorded usage when its tokens or cost is non-zero, and a known cost when
`cost_usd` is non-zero, regardless of vendor. Zero tokens with zero cost
show `tokens -  $-`.

*Proposed* (S129): a round writes final usage before ending its live attempt.
The daemon reads active live usage by role before reading the run record.
It adds live tokens only for role names still running in that record, once
per name. A role that ends between the reads contributes its recorded tokens
only, so the total neither dips nor counts that role twice.
