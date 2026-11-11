# Changelog

## Unreleased

- *Proposed (S315; adj 231, 232):* projects can set `build_command` with
  `ork projects set NAME --build CMD`. `ork daemon restart --rebuild` checks
  for running roles, builds the self-host project with a ten minute timeout,
  then restarts. A failed build keeps the daemon running. Status, doctor and
  the self-host landed action line offer the rebuild command when configured.
  `build-only-in-home-dir.sh` builds into `~/.local/bin/ork` without restarting;
  the existing manual script stays. Rebuilds are always explicit.

- Relay stats (*proposed*): `ork stats [PROJECT] [--since DATE] [--json]`
  displays slicer's relay-run aggregates on every call, without a daemon or
  local calculations. JSON preserves the aggregate keys and omits `per_run`.

- *Proposed (S312):* `ork run` and `ork wave start` print every open spec
  question with `--questions`, without planning, recording answers or
  starting runs (exit 3 when questions remain, 0 otherwise). Repeat
  `--answer KEY=N` or `KEY=text` to answer a whole batch in any order;
  waves require `ITEM:KEY=value`, and keyless questions use `#N`.
  Invalid or incomplete batches record nothing and start nothing.
  JSON question entries include `item`, `address`, `ask`, optional `key`
  and `text`; `answered` entries are `{item,key,answer}`. Text listings show
  wave answer prefixes, keyless addresses and an explicit empty result.
  Existing interactive and piped-input prompts are unchanged.

- S343 (adj 307, 308, 315): read-only claude-local roles use a fixed read-only
  tool allowlist with permission prompts disabled, avoiding plan mode's
  model-backed classifier. Local briefs request targeted reads and brief
  reasoning, and local roles get 90 minutes of active runtime. Cloud roles
  keep their existing permissions, briefs and timeout.

- *Proposed (S316; adj 233, 234):* `ork approve RUN --later-checks` marks every
  open human check `later`, approves, and prints a `deferred: N. TEXT` line for
  each check it deferred. `ork check RUN all later` marks them without
  approving. A check already `ticked` or `later` is left alone; there is no
  `all ticked`. The plain `ork approve` still stops on open checks, and its
  refusal names `--later-checks`.

- S334: a landing whose branch moved the item's slice JSON to `done/` while
  main edited the old path now merges main's change into the `done/` copy
  and goes on; a conflict pauses naming the `done/` file, and `ork retry`
  stages the absent old path as deleted.

- Polling (S336, adj 301): with no TUI attached the daemon reads nothing on a
  timer, and a TUI that attaches reads the parts of the projects it shows that are due at once. Merge
  detection rides on the git HEAD read, so with no TUI attached it waits until
  one attaches (owner decision 2026-11-11). With a TUI attached, a live run is
  read every 2 s instead of every 1 s.

## v1.1.0 (2026-11-11)

- Docs: re-anchor file:line citations in `docs/guide.md` (S219).
  Audit `docs/design.md`; its three sibling references remain unverified
  because the sibling checkouts are absent. Add a stdlib citation guard for
  both documents, including range lists, multiline spans and missing siblings.
  Leave dashboard citations out of scope. Remove the guide's unsupported
  stale-flag clause, as approved by the owner on 2026-11-11, and cite the
  current timeout description.
  Verified corrections (citation: old -> new):
  citation: `docs/design.md:162-221` -> `docs/design.md:175-361`.
  citation: `docs/design.md:162-221` -> `docs/design.md:356-361` (approval).
  citation: `docs/design.md:358-389` -> `docs/design.md:646-810`.
  citation: `README.md:46-47` -> `README.md:66-67`.
  citation: `cmd/ork/main.go:139-153` -> `cmd/ork/main.go:151-166`.
  citation: `README.md:24-33` -> `README.md:40-46`.
  citation: `internal/cli/cli.go:73-75` -> `internal/cli/cli.go:204-210`.
  citation: `docs/design.md:1165-1204` -> `docs/design.md:2368-2495`.
  citation: `docs/design.md:189` -> `docs/design.md:207-216`.
  citation: `docs/design.md:1423-1434` -> `docs/design.md:2412-2422`.
  citation: `docs/design.md:1263-1269` -> `docs/design.md:2561-2568`.
  citation: `internal/cli/cli.go:58-59` ->
    `internal/cli/daemoncmds.go:234-247`.
  citation: `docs/design.md:1261-1270` -> `docs/design.md:2561-2568`.
  citation: `docs/design.md:1273-1277` -> `docs/design.md:2604-2608`.
  citation: `docs/state.md:28-33` -> `docs/state.md:34-36`.
  citation: `internal/cli/cli.go:61-67` -> `internal/cli/cli.go:141-154`.
  citation: `internal/engine/states.go:197-202` ->
    `internal/engine/states.go:437-440`.
  citation: `docs/design.md:1278-1295` -> `docs/design.md:2704-2716`.
  citation: `docs/state.md:94-103` -> `docs/state.md:188-211`.
  citation: `docs/design.md:167-186` -> `docs/design.md:189-208`.
  citation: `docs/design.md:272-275` -> `docs/design.md:485-488`.
  citation: `internal/slicercli/write.go:96` ->
    `internal/slicercli/write.go:108`.
  citation: `docs/design.md:184-198` -> `docs/design.md:207-222`.
  citation: `docs/design.md:170-175` -> `docs/design.md:196-201`.
  citation: `internal/tui/keys.go:258-262` -> `internal/tui/keys.go:313-322`.
  citation: `docs/design.md:223-307` -> `docs/design.md:391-631`.
  citation: `internal/cli/cli.go:56` -> `internal/cli/cli.go:54`.
  citation: `internal/cli/cli.go:77` -> `internal/cli/cli.go:212`.
  citation: `internal/tui/keys.go:258-277` -> `internal/tui/keys.go:313-322`.
  citation: `docs/design.md:1034-1056` -> `docs/design.md:1958-1973`.
  citation: `internal/cli/wave.go:341` -> `internal/cli/wave.go:378-385`.
  citation: `docs/design.md:1064-1068` -> `docs/design.md:2045-2051`.
  citation: `docs/design.md:212-214` -> `docs/design.md:315-316,330-332`.
  citation: `internal/tui/keys.go:106-108` -> `internal/tui/keys.go:139`.
  citation: `internal/tui/pane.go:284-297,498` ->
    `internal/tui/pane.go:362-368,615-618`.
  citation: `internal/cli/run.go:247,264-268` ->
    `internal/cli/run.go:322,339-346`.
  citation: `internal/tui/keys.go:80` -> `internal/tui/keys.go:113`.
  citation: `internal/cli/cli.go:52` -> `internal/cli/cli.go:97`.
  citation: `docs/design.md:351-397` -> `docs/design.md:646-810`.
  citation: `internal/cli/cli.go:47-50` -> `internal/cli/cli.go:86-89`.
  citation: `internal/tui/keys.go:87-93` -> `internal/tui/keys.go:121-127`.
  citation: `docs/design.md:1088-1091` -> `docs/design.md:2070-2077`.
  citation: `docs/design.md:986-997` -> `docs/design.md:1860-1869`.
  citation: `internal/tui/pane.go:327` -> `internal/tui/pane.go:413-416`.
  citation: `internal/daemon/runs.go:1418-1429` ->
    `internal/daemon/runs.go:2515-2531`.
  citation: `internal/daemon/runs.go:919-921` ->
    `internal/daemon/runs.go:1624-1635`.
  citation: `docs/design.md:154-160` -> `docs/design.md:167-173`.
  citation: `internal/tui/keys.go:71` -> `internal/tui/keys.go:89`.
  citation: `internal/tui/keys.go:13-124` -> `internal/tui/keys.go:82-157`.
  citation: `internal/cli/cli.go:40-77` -> `internal/cli/cli.go:212`.
  citation: `internal/tui/ledgerend.go:27` -> `internal/tui/ledgerend.go:21`.
  citation: `docs/state.md:76-78` -> `docs/state.md:87-89`.
- S330: before creating a worktree, commit the item's edited slicer spec,
  render and index on main by path so the worktree receives them. Leave other
  tracking and non-tracking edits uncommitted. A failed commit pauses setup.
- Missing slicer merge drivers (*proposed*, S328, adj 288 and 289): doctor
  warns per project and prints the `slicer setup-git` commands. `doctor --fix`
  runs them after a `[y/N]` confirmation (Enter means No). A landing conflict
  under `.slicer/` also names missing drivers and the setup/retry steps.
  Docs updated: `docs/guide.md` and `docs/design.md`.
- Resume sampled vendor connection failures (S329, adj 291): wait 30 seconds
  and resume the same session once in the same round. Another connection
  failure falls back like an error; without a next row, pause with the vendor
  and error on one line. Matches only the sampled Grok reqwest send error and
  Claude `connection_failed` result. Docs updated: `docs/design.md`.
- Quieter Running view (*proposed*): S333, adj 282, 283, 284. The TUI opens on
  Running. An open Running row adds one dim line per working role (role, vendor,
  model, effort, elapsed, tokens, cost) and that role's last output line, and
  nothing its row shows (a question, paused or stopped run, with no role
  working, adds one line with its question or stop reason); the detail line
  under the rows holds only what the row lacks. Recent lists real runs, not spec-check results. A stale relay ledger
  leaves the Running list for one dim `stale: N` line that `enter` or `space`
  opens, so `x end run` stays reachable. In the run detail an out-of-credits
  attempt followed by a fallback is one dim `*` note, not a red `fail` line, and the
  `S298 implementer` sub-lines are gone. Docs updated: `docs/dashboard.md`.
- Land a passing run without an approve call (*proposed*): S331, adj 281
  (`landing.auto-approve`). When the gate returns `VERDICT: PASS`, no owner
  finding is open and the latest check is green, ork approves the run itself,
  finishes, commits, lands and cleans up, for every project; `ork projects set
  NAME --auto-land off` opts a project out (registry field `auto_land`, absent
  means on; refused while runs are unfinished). Human checks never block: each
  open one is marked later, and a landing adds the checks marked later to
  `human-checks-todo.md` at the project root and commits it with the tracking
  commit. A run stopped after its commit, or one the card could not read, still
  waits for `ork approve`. Docs updated: `docs/design.md`, `docs/guide.md` and
  `docs/state.md`.
- A report-shape pause names what is wrong with an owner finding and quotes the
  line; the rerun's brief carries that finding's text; the second shape pause in
  a row for one role and round names the refused report and
  `ork retry RUN --accept-shape` (*proposed*): S298. A plain `ork retry RUN` is
  refused there; `--again` runs the role once more, and `--accept-shape` keeps
  an owner finding that has no options as an open owner finding (an
  `[owner blocker]` as a free-text question) and runs no role. Docs updated:
  `docs/design.md`, `docs/guide.md` and `docs/state.md`.
- Set the per-project bell with `ork projects set NAME --bell on|off`
  (*proposed*): S224. `on` stores `true` and `off` stores `false` in the
  registry field `bell` (S100) through `projects.set`, and the text line says
  `bell=on` or `bell=off`. Any other value is a usage error. The call is allowed
  while the project has unfinished runs (owner decision 2026-11-11), so the
  busy refusal lists `--bell` among the fields that may change. The TUI reads
  the setting at start. Docs updated: `docs/design.md`, `docs/guide.md` and
  `docs/state.md`.
- Daemon-session test builds on the BSDs (*proposed*): S225. The `getsid` test
  helper moved from `cmd/ork/getsid_darwin_test.go` to
  `cmd/ork/getsid_bsd_test.go` (darwin, dragonfly, freebsd, netbsd, openbsd), so
  `go vet ./...` passes for those targets. The release workflow's Linux test job
  gets one `Vet another unix target` step that runs `go vet ./...` with
  `GOOS=freebsd`; it builds nothing that ships.
- CHANGELOG resolver accepts compact entries (*proposed*): S327. Landing's
  merge of the root `CHANGELOG.md` (S180) treats every `- ` line as a new entry,
  so entries with no blank line between them, and a body that mixes both
  layouts as `CHANGELOG.md` does, merge instead of pausing. New entries join
  with the separator at the top of the base's Unreleased; the original entries
  keep their separators. Docs updated: `docs/design.md`.
- Non-blocking owner questions in the relay ledger (*proposed*): S223. A
  non-blocking `[owner]` finding is asked at once with `slicer relay-run ask`
  and no `--blocker`, so the orchestrator step keeps running, and its id is
  kept on the finding (`ledger_question`). The owner's answer in ork is written
  to the ledger question. ork reads the question (`relay-run answer RUN QID
  --get`) before the approval card and before the finish step, and records an
  answer given there on the finding; it is not filed in adj. Ledger calls are
  advisory. `Client.RelayAsk` takes a `blocker` argument and the paused-question
  mirror still passes it. Docs updated: `docs/design.md`, `docs/state.md` and
  `docs/orchestrator-parity.md`.
- Role matrix editor in the TUI (*proposed*): S226, the TUI half of S173. In
  the Roadmap's matrix pane, `e` changes a row, `n` adds one, `d` removes one,
  `m` moves one and `r` edits a rule's text (`r` no longer refreshes there).
  Each key opens a form of typed-id lines in the status area: the row id (or
  rule id) first, then the lines of the key (`e`: agent, vendor, model,
  reasoning and note; `n`: role, then the same five; `d`: nothing more; `m`:
  the row to go before; `r`: the new text). `tab` and `shift+tab` move between
  lines, `enter` on the last line or `ctrl+s` sends. The form is checked
  against the loaded rows first. The status then shows `Run: <the exact slicer relay-roles command>`,
  the daemon's warning when the agent has no ork adapter, and `[y/N] (Enter is
  No)`; `y` runs it through `matrix.edit` and reads the matrix again, and the
  change is left uncommitted. Slicer's refusal shows verbatim. The daemon is
  unchanged.
- Resolved latest models in plans and lineups (*proposed*): S245. A lineup
  entry whose model was resolved from `FAMILY@latest` prints as `latest
  (resolves to MODEL)` in `ork run`'s plan, the launch confirmation and the
  lineup editor (rows, fallbacks and the UI pass row), through one helper,
  `runrec.ModelLabel`; the narrow Waves lineup column shows `MODEL (latest)`.
  The editor's custom model field
  starts from the requested `FAMILY@latest` text, so an edit round-trips. The
  run tree and the Overview rows are unchanged. Docs updated: `docs/dashboard.md`,
  `docs/design.md` and `docs/guide.md`.
- Named wave in the Waves screen (*proposed*): S216. On a project's Waves tab,
  `w` saves the ticked items under a name (a one-line input; `enter` saves,
  `esc` cancels, and a name already saved asks `Replace NAME? [y/N]`), and `L`
  lists the project's saved waves with each member's status. `enter` on a saved
  wave ticks the members that are still to do and are rows of the table, plans
  them one after another, and names the done members and the members slicer's
  suggestion does not offer; it never launches. `l` then sends the saved name on
  `wave.start` only when the launched items are exactly the saved wave's members
  that are still to do; a member the table lacks, an untick, an added tick or a
  held item sends no name. The screen does not show the name, and `ork wave rm`
  still removes a saved wave. The Waves hint adds `w save; L saved`: the tick
  hint is now `space tick` (was `space tick item`) and `enter item` is the first
  row hint to go, so `e edit lineup; w save; L saved` fit at 100x30 with items
  ticked (at 80x24 `L saved` goes). With nothing ticked `L saved` fits beside
  `tab views` at 80x24 and takes the place of `+ add project` at 100x30. Docs updated:
  `docs/dashboard.md` and `docs/guide.md`.
- S173 (*proposed*): the wire half of the role matrix editor. The slicer client
  runs `slicer relay-roles add|set|remove|move|rule-edit` (values attached as
  `--name=VALUE`, so a dash-leading value reaches slicer), and the daemon
  method `matrix.edit` previews or runs one of them in the registered project,
  with `--render`, no run record and the change left uncommitted. The result
  has the command to confirm, a warning when ork has no adapter for the agent,
  and the new row's id after an add. `roles.matrix` rows also carry `vendor`,
  `model`, `reasoning` and `note`. No TUI and no CLI command yet. Docs updated:
  `docs/design.md` and `docs/boundaries.md`.
- Named wave (*proposed*): S215. A set of items of one project can be saved
  under a name and started by name. `ork wave define --project P NAME ITEM...
  [--replace]`, `ork wave list [--project P]` and `ork wave rm [--project P]
  NAME` manage the saved waves, and `ork wave start [--project P] NAME` starts
  one: it leaves out the members that are done, retired or gone from slicer,
  names them, and plans and starts the rest as `ork wave start P ITEM...` does.
  The waves are kept in the new file `waves.json` beside `projects.json` (mode
  0600, only names and item ids; status is read from slicer on each use) and
  are dropped with `ork projects rm`. The daemon has the new methods
  `wave.define`, `wave.list` and `wave.rm`, and `wave.start` takes an optional
  `name`; each run record of a started saved wave carries `wave_name`, which an
  older ork refuses as an unknown key. The Waves screen does not use saved waves
  yet. Docs updated: `docs/state.md`, `docs/design.md`, `docs/guide.md`,
  `docs/boundaries.md`, `docs/glossary.md` and `README.md`.
- S204 (*proposed*): the engine follows two relay v11 process rules. A v11
  gate round closes its ledger step with the report's verdict only when the
  engine accepts the round; a round paused for a shape problem, an owner
  question or a blocker finding closes without one, and the rerun is a new
  step. A Check line marked `[AFTER LAND]` (with or without `[HUMAN CHECK]`)
  shows on the approval card as `deferred to land: ...`, already in state
  `later`, and the implementer and gate prompts say not to run it. Docs
  updated: `docs/design.md`, `docs/state.md` and `docs/orchestrator-parity.md`
  (a table of the v11 process rules).
- S244 (*proposed*): resolve a matrix model of `FAMILY@latest` to the newest
  model of the family. A row's model `gpt-*-sol@latest` or `grok-*@latest` (one
  `*` for the version) is resolved when the plan is built, from `codex debug
  models --bundled` or `grok models`, read once per daemon; the highest version
  wins, compared number by number. `opus@latest`, `sonnet@latest` and
  `haiku@latest` resolve to the alias, with no list call. The lineup entry
  keeps the matrix text in `requested` and the resolved name in `model`; the
  engine, the adapters, the run record and the ledger get `model`, and a resumed
  round keeps the recorded one. A bare `latest` is refused at plan, as is a list
  that cannot be read (the plan names the command, its exit and the family; a
  failed read is not kept) or has no match (the plan names the family); a launch
  override of the role gets past either. Local rows (`claude-local`,
  `codex-local`, `grok-local`, vendor `Local`) are never resolved, and `@latest` on
  one is refused. A launch override of `FAMILY@latest` is resolved too. An older
  ork refuses a `run.json` that holds `requested`. The fixture
  `testdata/vendors/codex/models-bundled.json` omits the `model_messages` and
  `base_instructions` prompt fields of each model. Docs: `docs/design.md`,
  `docs/state.md`, `docs/guide.md`, `docs/how-it-works.md`.
- S132 (*proposed*): question and answer polish. Needs you gets a `Questions`
  group (before `Paused`) for runs that wait on an owner question; `Paused`
  keeps the runs paused for another reason. Question options wrap with a hang
  under their text, drop whole on a short pane with `+N more options` as the
  last line, and show an empty summary as the label alone. `Recommend: N - reason`
  accepts an en or em dash. A cancelled adj answer confirmation puts the typed
  free answer back in the field, and the help line offers no `1-N pick` for a
  record with no options. A ledger answer filed in adj keeps its channel (`cli`
  or `tui`) in the rationale. Owner decision 2026-10-31. Docs:
  `docs/dashboard.md`, `docs/design.md`, `docs/guide.md`.
- S295 (*proposed*): verify the check command where it will run, once, before a
  role is spent on it. The check is static and runs nothing but `sh -n`: the
  shell must parse the command, the first word of each simple command must
  resolve, and the plain file names the command lists (not flags, globs,
  `./...`, redirect targets, output flags, the words after `-m`, or the words
  of commands that make files, such as `mkdir` or `git`) must exist in the
  directory the round check uses. `ork projects set --check` (and `--round-check`) refuses a command
  that fails, naming the file, the folder and the command; `--no-verify`
  stores it anyway and remembers it (`no_verify_check` in the registry entry;
  owner decision 2026-11-10), so setup and the plan skip the verification until
  a check is set again without it. A run whose check cannot run pauses at setup, before round
  1, with `check command cannot run in DIR: ...`; the fix is `ork stop ID`,
  `ork projects set PROJECT --check CMD` (`--round-check CMD` when the round
  command is the one that fails), then `ork retry ID` (S296), and the wait
  line's `next:` is `ork stop ID`. The run plan shows a warning (in the launch
  confirm, by the check line), and `ork doctor` names a later command's first word and
  a missing named file. Owner decisions 2026-11-05. Docs: `docs/design.md`,
  `docs/guide.md`.
- S285 (*proposed*): a faster check command for the checks after implementer
  rounds. `ork projects set NAME --round-check CMD` stores `round_check_command`
  (refused only while the project would have no `check_command`; empty
  clears it). With it set, the check after each implementer round runs it; when
  it differs from `check_command`, a green result is followed by one run of
  `check_command` (log `checks-full-N.log`) before the review or the gate. The
  landing re-check runs `check_command` only. With it unset nothing changes. `ork status` adds `last check: COMMAND
  (ok|red, round N)` to a run with a check row, and the plan shows `round
  check:` when the key is set. Owner decisions 2026-11-09 (adj 252 to 254):
  the key lives in the ork registry, it needs a `check_command`, and the full
  command runs after a green round check and at the landing. Docs:
  `docs/state.md`, `docs/design.md`, `docs/guide.md`, `docs/dashboard.md`.
- S264 (*proposed*): say to wait for the spec check, with its elapsed time, and
  keep the role's prose out of the Owner needed text. While the check runs
  after `l`, the Waves status line (and the Roadmap launch body) reads
  `spec check: 2 items, 1m12s; wait for the confirmation`; `l` stays refused
  until the reply. `ork run` and `ork wave start` print `spec check: S1, S2;
  wait for it to finish` before the call. The shared `Owner needed` text drops
  code fence lines and is empty when it starts with `none` (also for a stored
  `answers.json`); the Waves list shows its first line and `(+N more lines)`.
  Owner decision 2026-11-03. Docs: `docs/dashboard.md`, `docs/design.md`,
  `docs/guide.md`.

- S218 (*proposed*): wave suggest and the engine rule. `wave.suggest` and
  `ork wave suggest` keep the first slice of a wave whose Files touch
  `internal/engine`, `internal/daemon` or `cmd` and move every further one,
  alone, into a wave right after it, with the reason `engine: ID`; the
  Waves screen reads it as `one engine slice per wave; ID goes first`. The
  waves are renumbered, so a suggested wave can be launched as shown. ork
  reads each placed item with `slicer show` (eight at a time); a failed read
  leaves that item alone, and the refusal at `wave.start` stays. This is an
  exception to showing slicer's suggestion as is (owner decision 2026-11-07).
  Docs: `docs/design.md`, `docs/dashboard.md`, `docs/boundaries.md`,
  `docs/guide.md`.

- S236 (*proposed*): kill the descendants of a killed role or check that left
  its process group. A role or check killed on a hung watchdog, an owner stop,
  a timeout or a restart reconcile no longer leaves `go test` and its test
  binaries behind when they started in a session of their own. ork finds them
  from a `ps` snapshot of the role's pid taken before the kill; a process
  already reparented to pid 1 is not found. Docs: `docs/design.md`,
  `docs/state.md`.

- S267 (*proposed*): the Running entry and the run pane stop showing a stale
  cue on a run queued behind the cap: its heard cell reads `queued 19m`, dim,
  from the engine run's `created_at`. A queued role row names its planned
  vendor, model and effort (`. reviewer    queued  codex sol medium`), and
  Running line 2 reads `plan: implementer claude sonnet high`. ork's own
  orchestrator step (agent `ork`) reads `ork engine`, dim, with no clock. A
  role that fell back shows a dim `fell back from codex sol high`. The wire
  row `EngineRun` gains `lineup` (additive, `omitempty`). Docs:
  `docs/dashboard.md`.

- S293 (*proposed*): the project check command runs in the project's own
  folder of the worktree when the project lives in a subfolder of its repository
  (a monorepo), not at the worktree root, where a build file such as
  `PrismUi.slnx` was not found. ork reads the folder with `git rev-parse
  --show-prefix`. The round check, its rerun and the landing re-check all use
  it, and the check log gets a line `(in libraries/p)` after `$ COMMAND` for a
  subfolder project. A folder missing in the worktree, or a git error, pauses
  the slice with a reason that starts `check directory:`. No new setting.
  Docs: `docs/design.md`, `docs/state.md`.
- S263 (*proposed*): the daemon log prints a run's merge detection error once
  instead of on every HEAD change; it prints again when the text changes or
  after a pass for that run works. A committed run whose branch and worktree
  are gone and whose work is not in HEAD is marked cleaned with a
  `cleaned_reason` instead of failing on every pass. New run record field
  `cleaned_reason`. Docs: `docs/state.md`, `docs/design.md`.
- S257 (*proposed*): `ork wait [RUN] [--until STATE,...] [--timeout D]
  [--project P]` blocks until a run changes state and prints it, so scripts
  stop polling `ork status`. Exit 0 changed or entered an `--until` state, 3
  question pending, 4 nothing active, 5 timeout. `--until` takes `question`,
  `awaiting-approval`, `paused`, `stopped` and `landed`; there is no `failed`
  for now (adj 247). Without RUN it follows the only unfinished run
  of the project. `run.show` and `ork show --json` gain `landed`. Docs:
  `docs/guide.md`, `docs/state.md`.

- S259 (*proposed*): the sleep detector counts no sleep until the daemon
  starts it, so a watchdog look that arrives a little late can no longer be
  read as sleep and hide a hung role. A test pins the daemon's start of it
  (`Wake.Started()`). Docs: `docs/design.md`.
- S255 (*proposed*): a retry that starts a fresh round (a `rounds` pause
  retry, or the resume of a stopped run) restarts each role's vendor chain at
  its first lineup row; inside a round the fallback row stays sticky. A vendor
  still marked out is skipped as before. New run record field `chain_from`.
  Docs: `docs/design.md`, `docs/state.md`.
- S283 (*proposed*): `ork wave start` no longer fails the whole wave for an
  item with no slice spec. It starts the items that have one and prints
  `ID: no slice spec; run slicer promote ID` for the others; exit 0 when a
  wave starts, 1 when no item is left. The JSON result has a `no_spec` array
  beside `held`. Other plan failures still abort. The daemon spec check
  reports the missing spec as a `no_spec` row. Docs: `docs/guide.md`,
  `docs/design.md`.

- S131 (*proposed*): the self-host check compares the daemon build with the
  branch the main checkout is on. Unchanged tips need one git call per
  snapshot or status read; changed tips refresh a bounded result cache.
  The notice and `behind_main` field keep their names. Docs:
  `docs/design.md`, `docs/state.md`, `docs/dashboard.md`.

- S310: normalized stale macOS paths in 46 tracking items, including
  completed slices. Commands use the current checkout and scratch paths are
  repo-relative. Historical paths outside the old ork checkout use `~` for
  the home directory. The tracking search uses `slicer find`.
  S73 keeps its `--root`-first allowlist probe with `.` as the root.
  Its old probe note is retracted and replaced with the corrected path;
  slicer retains the original as audit history.
- S103 (*proposed*): `ork --version` prints the mark only on a terminal;
  on a pipe or file it prints plain `ork VERSION`. The TUI refreshes the
  daemon build from snapshots so the stale-build marker clears after a
  matching daemon restart without reopening the TUI.

- S309 (*proposed*): spec-check flags slice Check commands outside the
  implementer's resolved allowlist, with environment prefixes or with paths
  outside the worktree. Gate briefs separate runnable and orchestrator-only
  lines and include ork's round check log and result as tests-green evidence
  for the read-only gate. The Gate section requires accepting that evidence
  without running the suite; prose bullets without backticks stay out of both
  command lists. Docs: `docs/design.md`.

- S129 (*proposed*): finished roles with no recorded tokens or cost show
  `tokens -  $-`; any vendor's non-zero recorded cost is displayed. Run token
  totals keep live usage until final usage is recorded and count live tokens
  only for roles still running. Docs: `docs/dashboard.md`, `docs/design.md`,
  `docs/state.md`.

- Landing check (*proposed*, S217): the approval card warns about files in
  main that overlap the slice's changes, including uncommitted worktree
  changes, and about main being detached or on the slice branch. It refreshes
  on pane open and project refresh, excludes tracking files landing handles,
  and leaves approval available. Failed checks show no warning.
  Docs: `docs/dashboard.md`, `docs/design.md`, `docs/guide.md`.

- S308 (*proposed*): `ork status` lists stale manual relays from the same
  board data as the TUI, marked `stale`, and includes them in the active-relays
  count. Recently ended relays stay out of status. Docs: `docs/guide.md`.
- S254 (*proposed*): the landing looks at the main checkout before it merges
  the base branch in, and again right before the fast-forward. If it holds
  uncommitted changes to files the slice branch also changes, and any of them
  is not one of the project's tracking files, the run pauses at once, before
  the merge-in and the re-check, with one line that names up to five files:
  `landing stopped: uncommitted changes in P to files the slice changes: ...;
  commit or stash them`. If only tracking files overlap (`.slicer/` except
  `config.json` and `relay-roles.json`, under the project's folder), ork
  commits them on the base branch as `Tracking edits on main before landing
  ID`, by name, so staged files stay staged; the merge-in then takes them in
  and the re-check runs. A tracking edit made during the re-check is
  committed and the landing merges again once; a further overlap pauses.
  Uncommitted files the slice does not change are left alone (the `ID done`
  commit after `slicer land` still commits any `.slicer` change). A failed
  fast-forward now pauses with git's message on one line. No wire change.
  Docs: `docs/design.md`, `docs/boundaries.md`, `docs/state.md`.
- S271 (*proposed*): ork sets `TMPDIR` to a role's own temp directory in the
  process environment of every Claude and Codex role, so a role no longer
  needs `env TMPDIR=DIR cmd` or `mkdir -p DIR`, which no allowlist pattern
  matches and which were denied. A `TMPDIR` in the project's `vendor_env` does
  not win over the role's directory. Codex roles keep `GOCACHE` in the same
  directory. Every role prompt now says so. Grok roles are unchanged. No wire
  change. Docs: `docs/design.md`, `docs/state.md`.
- S266 (*proposed*): a spec check whose report has a shape error, or whose
  round ended with denied commands (for example `rg ... | head`), resumes its
  own session once with the shape problems or the denied commands and the
  expected forms, instead of failing or starting over. A second shape error or
  denial fails the check with no start over, and its message names the slices
  and says that checking again is safe. The check resumes at most once, even if
  it starts over. A check still starts over once when main moves or the main
  checkout changes outside the check, and when a shape error has no session to
  resume. The refused round is a failed `spec-check` ledger step with a short
  reason, and the resumed round is a second step. No wire change. This
  replaces the S143 rule that denials are never retried (2026-10-28). Docs:
  `docs/design.md`, `docs/dashboard.md`, `docs/guide.md`.
- S299 (*proposed*): a vendor you mark out with `ork vendor out` survives a
  daemon restart and an idle exit, and can end at a set time. The daemon
  writes your marks to `vendors-out.json` in the state directory (mode 0600)
  and reads them at start; a mark whose end passed while it was down is
  dropped. `ork vendor out NAME --until TIME` ends the mark at TIME (`HH:MM`,
  the next time the clock shows it, `YYYY-MM-DD HH:MM`, or an RFC 3339 time;
  it must be in the future), and the footer, `ork vendor` and `ork status`
  name the day too when the end is not the day the mark began (`4 Nov 07:12`),
  and the year when that differs; a start that is not today names its day.
  The 60-minute marks ork makes on a rate limit or out of credits stay in
  memory and a restart still clears them. A mark or a back that cannot be
  written is refused with an error and changes nothing; a stored vendor ork
  cannot run is not loaded; a store that cannot be read is
  reported on the daemon's standard error and starts with no marks. Wire:
  `vendors.set` takes an optional `until` (RFC 3339). The help text no longer
  says a restart clears the marks. ork does not read a reset time from a
  vendor's message. Docs: `docs/design.md`, `docs/guide.md`,
  `docs/state.md`, `docs/dashboard.md`, `docs/orchestrator-parity.md`.
- S302 (*proposed*): add several projects at once. In the TUI, `+` with a
  folder that holds projects below it opens a checklist (space ticks, `a` all,
  `n` none, `y` adds, Esc goes back; Enter does nothing); tracked projects
  (`.slicer` or `.adjudicate.json`) start ticked, plain git repos do not, a
  registered path (matched after symbolic links are resolved), a clashing name
  and all but one of several same-named folders (a tracked one is kept before a
  plain git repo, then the earlier path) are blocked with their reason, and
  the results name each row. The scan quits after 500 folders or 200 projects
  and says so (`stopped after N folders and M projects`); names and paths show
  as plain text.
  A folder that is itself a project keeps the single-add confirmation. On the
  command line, `ork projects add --scan DIR [--yes]` lists the projects and
  adds the tracked ones only, asking on standard input without `--yes`; `--json`
  needs `--yes` and reports `stopped` and `folders`. The walk is two levels deep, stops at a project and skips
  hidden folders, `node_modules` and symbolic links; each project is named after
  its folder (owner decisions 2026-11-07). No wire change. Docs:
  `docs/dashboard.md`, `docs/design.md`, `docs/guide.md`, `docs/state.md`.
- S296 (*proposed*): `ork retry RUN [--rounds N]` resumes a stopped run from its
  worktree, also after a daemon restart and for a dismissed run (which clears
  the dismissal). It needs no commit and a worktree that is still there, and
  it refuses a run stopped in setup. It reloads the project from the registry
  (a fixed check command takes effect), grants N implementer rounds (2 by
  default), begins a new ledger run and re-enters the checks, or implementer
  round 1 when no implementer round has run (a run S295 paused at setup). `stop`
  now records `stopped_from` in `run.json`. `ork status` and `ork show` name the
  command for a stopped run. `ork run` and `ork wave start` on an item whose
  newest run is a resumable stopped run resume it when the worktree equals the
  last implementer round's `tree_sha256` (owner decision 2026-11-07, Q1 option
  b); otherwise they start a new run and print why (`message` in the
  `run.start` and `wave.start` replies). A dismissed run is not resumed by a
  start. When no slot is free for the resume, the start is refused and names the
  stopped run, instead of queuing a new run that would restart at round 1.
  Docs: `docs/design.md`, `docs/guide.md`, `docs/state.md`.
- S239 (*proposed*): when the project check command exits non-zero, ork runs it
  once more before it starts a fix round. A pass on the rerun does not spend the
  round and is recorded as flaky (`checks[].attempts` 2, `checks[].flaky` true in
  `run.json`, and both runs in the check log). If it fails again, the fix-round
  brief starts with the failing Go test names and their assertion lines (at most
  8000 bytes), then the end of the log as before; the landing re-check does the
  same. A timeout and `slicer check` are not run again (owner decisions
  2026-11-02). Docs: `docs/design.md`, `docs/guide.md`, `docs/state.md`.
- S297 (*proposed*): `ork show RUN [NAME] [--json]` prints a run's state, the
  whole pause reason, its role reports, check results and findings, and with
  NAME one role report or check log, so a paused run can be read without
  opening the run directory (S297). A paused run also prints its newest report
  (the pending role's when it has one). New wire method `run.show`;
  `run.report` now also serves a check log listed in `checks[].log` (owner decisions 2026-11-05, Q1 to Q4).
  Docs: `docs/design.md`, `docs/state.md`, `docs/guide.md`.
- S288 (*proposed*): an `[owner]` or `[owner blocker]` finding in a reviewer,
  UI pass or gate report no longer pauses the run because its body holds more
  than the block (owner decision 2026-11-04, `slicer show S288`). The parser
  strips one leading `- `, `* ` or `+ ` list marker from `Options:`, the option
  lines and `Recommend:`, and keeps every other body line, before `Options:` or
  after `Recommend:`, as context in the finding's text (a blocker's question
  shows them above its block). A line between two options, a malformed numbered
  option, a repeated or misplaced `Options:` or `Recommend:`, and a body
  without both still refuse. The reviewer, UI and gate
  prompts now state the body rule and show a refused and an accepted example.
  The `## Owner needed` block and the spec check's question blocks get the same
  leniency (owner decision 2026-11-07): list markers are stripped and other
  lines inside or after the block stay as context. In an `## Owner needed`
  block the pause shows the context above the block, as for a finding; in a
  spec-check block it stays in the block's text and is filed with the adj
  record when the block has a decision key, and the TUI and CLI do not show it.
  A line between two options, an option line after `Recommend:`, a
  `Decision key:` line behind a list marker, and a missing or malformed
  `Options:` or `Recommend:` still refuse. The `VERDICT:` line is never lenient. The
  `## Owner needed` wording in all six prompts that carry the block changed to
  match. Docs: `docs/design.md`, `docs/dashboard.md`.
- S279 (*proposed*): a round check no longer goes red on `slicer check`
  problems that belong to other items when the branch is behind its base
  branch (adj 178, `checks.outside-problems`, owner decision 2026-11-05). It
  defers when every problem reads `ITEM: ...` or `ITEM / SECTION: ...` for an
  item other than the run's own, `stale_render`, `orphan_render` and
  `stale_sync` are empty, and `git merge-base --is-ancestor BASE HEAD` says the
  base is not in the branch. A git error, a problem with no item and an
  up-to-date branch keep the round red; the landing re-check never defers. ork
  writes `deferred to land (branch behind BASE): PROBLEM` lines to the check
  log, records them in `checks[].deferred`, and names them in the reviewer, gate
  and finish-step briefs (the reviewer and gate name them in their reports as
  deferred, not as findings; the finish step's `--check` may be red on exactly
  them, which is expected). The landing merge and re-check decide them; on a
  run that stops after the commit ork does not land, and the owner's own
  landing decides. No git write happens during the rounds. `slicer check`'s `orphan_render` is now read from its
  JSON. Docs: `docs/design.md`, `docs/how-it-works.md`, `docs/state.md`,
  `docs/guide.md`.
- S260 (*proposed*): launch no longer asks well specified or split (adj 189,
  `launch.well-specified-default`, owner decision 2026-11-07, replacing adj 86,
  87 and 135 once the owner supersedes them). `ork run`, `ork wave start`, a
  tick on the Waves view and the single launch on the Roadmap plan every item
  as well specified = yes and split = no; the spec check still holds an item
  that is not Ready. `--well-specified y/n` and `--split y/n` stay as optional
  overrides, `--yes` needs neither flag, and a value other than y, yes, n or no
  is still a usage error (its text is now `--well-specified y/n and --split y/n
  each take y or n`). The wire (`well_specified`, `split`) is unchanged. In the
  TUI, space ticks and plans at once with no inputs rows, the launch goes from
  the list to the plan, the input hints are gone, and the keys help reads
  `space tick item and plan it (Waves)`. The `waves-inputs-*` golden views are
  now `waves-ticked-*`. Docs: `docs/design.md`, `docs/dashboard.md`,
  `docs/guide.md`, `docs/diagrams.md`.

- S306 (*proposed*): the approval card pane keeps `x stop` on the hint line
  when `p report` shows (owner decision 2026-11-07). On a card pane, when the
  line is too narrow, hints drop in this order: the scroll keys, then `p
  report`, then `x stop`, then (with open checks) the check digits, `F fix
  round`, `o answer` and `f file slice`, and `a approve` last (the way out never
  drops). With every check marked the digits and `F fix round` are not needed
  and drop before all of these, the scroll keys included. On a card with a
  report and no owner findings, at 80x24 an approvable card shows `p report`,
  `a approve` and `x stop`; with open checks the line holds `x stop`, the check
  digits and `F fix round`, and `p report` goes first. Other run panes are
  unchanged. Docs: `docs/dashboard.md`.

- S274 (*proposed*): a resumed paused run waits for a slot, ahead of queued
  runs (adj 177, `wave.resume-slot`). A paused run still holds no slot. When
  you answer or retry it and the project cap or the global cap leaves no room,
  the request is stored in the run record (`resume`), the command replies at
  once that the resume waits, and the run stays `paused`. `ork status` shows
  `resume waits for a slot under project cap N; next: ork stop ID` (or `under
  global cap N`). A
  freed slot starts waiting resumes first, oldest request first, then the
  queued runs. A second answer or retry, of either kind, is refused while one
  waits, `ork stop` clears it and a daemon restart keeps it. An answer filed in
  adj is filed at once. Approve and stop still bypass the caps. In the TUI the
  run leaves Needs you and the Questions view and shows under Running, with no
  answer field or retry hint, and the retry status says the resume waits.
  Docs: `docs/design.md`, `docs/guide.md`, `docs/state.md`,
  `docs/dashboard.md`.

- S258 (*proposed*): a project names the matrix rows its spec check runs on:
  `ork projects set NAME --spec-check-rows r16,r24` (registry
  `spec_check_rows`, owner decision 2026-11-07). The first row with an adapter
  runs and the next is its fallback. Every listed row, and every row a
  `Spec check row (rNN)` rule names, stays out of a run's reviewer and gate
  chains, and `ork roles matrix` labels each one. With no setting ork uses
  every row that a `Spec check row (rNN)` rule names, in rule order: the
  first runs and each next one is its fallback. A spec check with no row to
  run on now fails with the new `no_spec_check_row` code, not `internal`. The
  message has lines for the fix command (with a real row), the other way out,
  the reviewer rows with an adapter and a warning that a spec check row
  leaves every run's reviewer and gate chains. Docs:
  `docs/design.md`, `docs/state.md`, `docs/guide.md`, `docs/dashboard.md`,
  `docs/glossary.md`.

- S294 (*proposed*): pause red loops after round 3, or sooner when the
  worktree content and check log repeat. `ork retry RUN [--rounds N]` grants
  two more rounds by default. Status names the failure and retry/stop
  choices. Approval-card Fix runs within the current granted limit.

- S280 (*proposed*): a Grok round that the CLI cancelled because a command
  needed approval is an error exit. The role's next row takes the same round,
  and Grok is not marked out. A finished report is kept instead: a non-empty
  report file, or the last complete message that holds `## Owner needed`. A
  read-only Grok role's brief names the command forms that run in plan mode
  and asks for one command per call. A spec check builds each row's brief
  with that row's vendor. Docs: `docs/design.md`, `docs/how-it-works.md`.

- S278 (*proposed*): the spec check counts a slice as ready only when its
  Failing tests name their assertions and its Check gives a command the
  sandbox can run with the result that means done, or a `[HUMAN CHECK]` that
  says what the owner looks at. Otherwise it asks one owner question for the
  slice that proposes concrete wording. Prompt and tests only. Docs:
  `docs/design.md`.

- S277 (*proposed*): every role brief ends with a `## Done when` checklist,
  also on resumed rounds: the role's acceptance criteria and exact report
  lines, its read-only limits or the allowed command forms, and what to report
  when a check cannot run. The implementer's lists the slice's Failing tests
  and Check items and one proving test per `[fix]` finding; a finish round
  lists its commands in order. Docs: `docs/design.md`, `docs/state.md`.

- S275 (*proposed*): a report that fails the shape check and asks no owner
  question retries the same role once, in the same round and session, with
  the shape problems as its brief. A second bad report pauses the run (kind
  `shape`). This covers the implementer and its finish step, the reviewer,
  the gate, the architect and the UI pass, and uses no extra implementer
  round. Status and role rows say "shape retry". Docs: `docs/design.md`,
  `docs/state.md`.

- S256: a resumed role is told to re-read the tree (*proposed*); a read-only
  role is told to report in its final message; a fenced owner question is
  accepted; `ork retry RUN [TEXT]` sends a note. Docs: `docs/design.md`,
  `docs/state.md`, `docs/guide.md`.

- S237: `ork answer` and `ork retry` return once the run has resumed
  (*proposed*); `--wait` waits until it rests. A relay run id passed as a run
  names the ork run. Docs: `docs/design.md`, `docs/guide.md`.

- S72: Grok adapter (*proposed*). ork runs read-only roles on the `grok` CLI
  (`--permission-mode plan`, streamed output so the watchdog sees a live role)
  as a third vendor next to `claude` and `codex`. `grok` is accepted in a
  launch override, by `ork vendor out|back`, and as the matrix agent `Grok`
  (case-insensitive); a matrix model of `latest` passes no model flag. A 403
  "used all available credits" is classed out of credits; every other failure
  is unclassified. A write role is refused: headless grok refuses every write
  under `dontAsk` and `acceptEdits` (probes 10 to 12), and the owner rules out
  `bypassPermissions` and `--always-approve`.
  Grok is never a default. Docs: `docs/design.md`, `docs/guide.md`,
  `docs/diagrams.md`.

- S235: sleep and wake awareness (*proposed*). Do not count machine sleep as
  silence for the hung-role watchdog; show working time, last output and slept
  time in `ork status`. Docs: `docs/design.md`, `docs/guide.md`,
  `docs/state.md`, `docs/dashboard.md`.

- S213: release workflow hardening (*proposed*). The release fails, before
  it is created, when `CHANGELOG.md` has no section for the tag. A signed
  tag's signature is left out of the release notes. The test and build
  checkouts set `persist-credentials: false`, and the build job's
  `setup-go` sets `cache: false`, so a release build restores no cache. The
  guard test `internal/release/workflow_test.go` reads the workflow through
  a normaliser, so a reformat (indent width, quote style, comments, blank
  lines, CRLF, list dashes at the key's indent) no longer fails it, and
  real changes still do.

- S238: landing merge-in without a role (*proposed*). When merging the base
  branch conflicts, ork merges the `CHANGELOG.md` entries itself even when
  other files conflict, then pauses the run with a one-line reason that names
  the remaining files, the worktree and `ork retry RUN`. After the owner
  resolves them (no `git add` needed), ork stages them, commits the merge,
  renders and re-checks. No role is asked to commit. The land fix round now
  covers a red re-check only, and `ork status` marks the second approval
  after it as a repeat. Docs: `docs/design.md`, `docs/guide.md`,
  `docs/state.md`, `docs/dashboard.md`.

- S190: TUI notes sweep. A launch replan that returns no plan keeps the
  confirmation of the old plan instead of closing the launch. `esc` in the
  lineup editor's custom fields steps back to the rows; a second `esc`
  discards the editor (*proposed*). The Roadmap item pane scrolls by line
  (`j`/`k`), page and to its ends, and the matrix pane takes `g`/`G` too
  (*proposed*); the matrix pane's first lines use the two columns they
  lost. The focused stop and budget rows of the lineup editor are drawn as a
  bar. After a failed answer write the launch status says
  `ITEM: answers not saved; l starts the launch again`, and
  a failed write after the launch closed shows its error. The report viewer
  no longer writes the pane in the draw path, and an older report reply no
  longer overwrites a newer one. Docs: the neutral approve question.

- S207: read the relay contract once during engine setup and use one accepted
  version phrase. Name the set once on doctor's unsupported-contract FAIL
  line, return `not_found` (exit 1) with a project-list hint for an unknown
  pin or unpin name, and shorten the gate's no-fix criterion. Refuse duplicate
  UI pass skill names on every path (*proposed*).

- S100: ring once when a run ends; add the *proposed* per-project `bell`
  registry field (on when absent). Muting a project keeps counts and badges
  updating. Restart the TUI after editing; the setter command lands in S224.

- S188: polish doctor upgrade reasons with installed versions and consistent
  checkout placeholders; note the PyPI assumption for slicer venv commands
  (*proposed*). Keep an empty `held` array in declined `ork run --json`
  results, prefix spec-check errors, wrap spec questions at 80 columns,
  filter control characters from adj record text, and put held or cancelled
  output on its own line when input ends without a newline.

- S67: add *proposed* Roadmap keys `P` to park or unpark and `J` / `K`
  to reorder, each with an exact slicer command confirmation. Refuse parking
  during an unfinished run, label parked rows, follow moved items and report
  when ranking leaves the list unchanged. Follow moves even when the snapshot
  arrives before the edit reply. Keep matrix and edit hints at 100x30 and the
  existing 80x24 hint line; keep lineup editing in compact Roadmap help.
  Docs: `docs/dashboard.md`.

- S50: add a *proposed* gate-brief warning for changed files outside the
  slice's Files list, using slicer's paths and exempting `.slicer/`.
  Fence check commands containing backticks or newlines in implementer briefs,
  and cite the default-cap decision beside the default in the design doc.

- S130: compact usage token counts, ASCII gate verdicts, repeated-role
  ordinals, stopped rows without "running", wrapped Waves conflict markers,
  an A-key toggle between three waves and all, and a short behind-main notice
  (*proposed*). Add fix-round goldens and document raw markdown tails and the
  measured 80x24 limit: fitting panes have six Overview lines above them;
  overflowing panes have five lines above them: a dim `+N lines above` marker,
  then four Overview lines. Gate FAIL verdicts omit the reason suffix and keep
  the reason on the dim task line. Collapsing Waves moves a hidden selection
  to the last item of the last visible wave, before Unplaced items.
  The `xN` count and `#N` ordinal now count the same lane steps.

- S108: done steps in the run tree show one node line without a task line.
  All other phases keep their task text, including failed steps. The gate
  FAIL line stays visible. Docs updated: `docs/dashboard.md`.

- S212: a *proposed* GitHub Actions release workflow tests on Ubuntu and
  macOS, then builds darwin/arm64, darwin/amd64 and linux/amd64 archives when
  the owner pushes a `v*` tag. Archives include the license, notice and README;
  releases include checksums. New release notes combine the annotated tag
  summary and matching CHANGELOG section; existing notes stay unchanged.
  Manual dispatch keeps build artifacts without publishing a release.
  The daemon session test now uses platform-specific getsid helpers so it
  compiles on Linux as well as macOS.

- S180: landing resolves a conflict only in the root `CHANGELOG.md` with
  an entry-aware merge in Go, preserving complete Unreleased entries exactly
  once with branch additions first, then commits silently (*proposed*).
  Changed existing entries or surrounding text, other conflicts and resolver
  failures keep the existing implementer fix round. File or staging failures
  attempt to restore conflict markers. Docs updated: `docs/design.md`,
  `docs/manual-steps.md` and `docs/orchestrator-parity.md`.

- S69: `O` opens slicer's TUI from the Roadmap and re-reads the project on
  return (*proposed*), following the selected item or clamping the old row if
  it is gone. The action needs no daemon, reports a missing slicer binary
  and exit errors, and refuses an open item pane. Add Roadmap help and a hint
  after row actions that drops with project additions and new slices.
  Docs updated: `docs/dashboard.md`.

- Non-Go projects (S265, *proposed*): `ork projects set --preset go|python`
  sets a full implementer allowlist as plain patterns, with the check command's
  pattern from the same call. `--allow` adds a pattern to the role's list (from
  the built-in default when the role has none) and skips a duplicate, and
  `--replace-allow` replaces the list as `--allow` did before (owner decision
  2026-11-07, adj 190). The run plan, the launch confirm and `ork doctor` warn
  of a missing check command or an allowlist that does not fit the project (no
  `go.mod` for the Go default, no Read, Edit or Write, no pattern for the check
  command). A run or wave start on a project with no check command is refused
  unless `ork run` or `ork wave start` gets `--allow-no-check` (adj 191); the
  TUI launch confirm shows the refusal and the fix. While runs are unfinished
  `ork projects set` still changes `cap`, the budgets and the worktree and
  branch patterns, and refuses the other fields by flag name. Raising a
  project's cap starts the queued runs that now fit. Docs updated: `docs/state.md`,
  `docs/design.md`, `docs/guide.md`, `docs/dashboard.md`.

- Daemon and engine wording leftovers (S229; closes the later-notes rows S186,
  S191 and S201): the blocked-wave refusal shows a shared blocker's title once,
  joins names with `and` (`untick S2 and S3, or land S1 and S4 first`) and
  quotes a title plainly. A run stopped after commit drops its spec-check
  directory when its branch is found merged (*proposed*). An unreadable
  `run.json` is an `internal` error for finding actions too (not `not_found`),
  and its message, `the record of run ID is unreadable: WHY`, no longer repeats
  itself or shows the path. The renders error names a file as `NAME (WHY)` and
  says `replaced while being read` for a swapped link. Docs updated:
  `docs/design.md`, `docs/state.md`, `docs/dashboard.md`.

- Correctness sweep (S194): keep a clean role result when the watchdog fires
  in the same tick; refuse swapped FIFO and symlink report/render opens without
  blocking; keep vendor-slot waits fresh with a *proposed* orchestrator-step
  heartbeat; treat a successful item note as success even if its refresh fails;
  and attach owner-answer flag values so dash-leading answers reach slicer intact.

- Claude fallback implementer: the Claude implementer's brief keeps its
  resolved allowed shell commands and adds rules for editing and plain shell
  calls in `## Done when`, including finish steps (*proposed*, S243).
  `ork status` shows the distinct denied-command count and the first denial
  cut to 40 characters (*proposed*); JSON and the TUI stay unchanged.

- Running shows only its Overview and Questions tabs; a wave launch (and a
  single-item Roadmap launch) moves to Running with the new run selected, unless
  an item waits on spec questions or failed (*proposed*, S262). S140's combined
  Waves, Roadmap and Decisions tabs are removed.

- Local model through a vendor CLI (*proposed*, S301, adj 179 to 181, 183,
  186 to 188, 192, 194, 195, 198): a matrix row whose Agent is `claude-local`,
  `codex-local` or `grok-local` runs the launcher of that name as the vendor's
  binary, with the row's model and effort and the daemon's environment; ork
  holds no URL or key, and a cloud `vendor_env` does not reach a local twin.
  Each local vendor has its own out mark, and the three share one pool of four
  slots, spec checks included. A run whose first implementer row is local
  reserves one pool slot for its lifetime and takes no global or project cap
  slot. A local role is stopped after 45 minutes of runtime, machine sleep not
  counted, and falls back to the next row; cloud roles keep the 30-minute
  silence limit. A missing launcher is skipped with a named reason. The
  `grok-local` row's model is `latest`, so the Grok adapter passes no `-m`.
  Local costs show as `local`, with no dollar amount. No role defaults to
  local, and an unreachable-endpoint rule is not part of this change. Docs
  updated: `docs/design.md`, `docs/boundaries.md`, `docs/state.md` and
  `docs/dashboard.md`.
- Planning skips a Grok write row (*proposed*, S268): a plan leaves a `grok` or
  `grok-local` implementer row out (unless the implementer is the read-only
  preset) and names the reason in the plan note, and the matrix view shows the
  row as skipped. A plan with no implementer row left is refused, and a launch
  override with a Grok implementer entry is refused with `bad_params`. Docs
  updated: `docs/guide.md`, `docs/how-it-works.md` and `docs/design.md`.
- Role prompts show the report lines literally (*proposed*, S276): the
  implementer and gate prompts show `Tests: N passed, M failed` and `## Owner
  needed` as their own lines, say a list number is never part of a heading, and
  the other role prompts say the heading is exactly `## Owner needed`. The
  report-shape check is unchanged.
- Shape retry also covers a malformed report that asks the owner (*proposed*,
  S287, adj 277): the role is resumed once in the same session with a brief that
  says to keep its owner question, then the question is asked, or the run
  pauses on shape if the second report is malformed too. The run record's
  pending entry gains `asked` (additive, no migration). Docs updated:
  `docs/design.md` and `docs/state.md`.
- Landing conflicts only in tracking files (*proposed*, S289, adj 292): when the
  merge-in of the base stops with every conflicted file a slicer tracking file
  under the project's `.slicer/slices/`, the pause says both sides changed the
  file, gives the commands to see each side, and says to keep both changes,
  remove the markers and run `ork retry`. A mix with other files keeps the
  generic pause. Docs updated: `docs/design.md`.
- Subcommand help (*proposed*, S290): `ork SUB --help` and `-h` print that
  command's usage block, cut from the one usage text, and exit 0; a group
  prints all its blocks. `ork help SUB` prints the same text and an unknown SUB
  exits 2. `ork daemon run --help` prints `usage: ork daemon run` and exits 0.
  Docs updated: `docs/design.md` and `docs/guide.md`.
- Vendor names on the command line (*proposed*, S291): `--implementer`,
  `--reviewer`, `--architect`, `ork vendor out` and `ork vendor back` read a
  vendor name in any case and store it lowercase, on the CLI and on the
  `run.plan` and `run.start` wire. An unknown name is refused with the list of
  vendors, built from one list in the daemon behind `supportsVendor`. Matrix
  agent names such as `Claude Code` are not accepted there. Docs updated:
  `docs/guide.md` and `docs/design.md`.
- Spec check and a sibling project's writes (*proposed*, S292): the read-only
  fingerprint of the main checkout covers only the paths under the project's
  own root (`git status --porcelain -- .`), so a sibling project's tracking
  write in the same repository no longer fails the check. A write in the
  project's own root, its `.slicer` included, still does. The spec check takes
  no lock. Docs updated: `docs/design.md`.

## v1.0.0 (2026-10-31)

- S209: a project can name `ui_pass_skills` for its Claude UI pass
  (*proposed*, adj 171). `ork projects set NAME --ui-pass-skills a,b`
  replaces the list; an empty value clears it. Only a `claude` UI pass
  gets the brief line advising it to load the named skills. Codex gets no
  skills line, and the neutral role prompt stays unchanged. ork never
  checks skill existence or reads vendor files.
  Docs updated: `docs/state.md`, `docs/design.md`, `docs/guide.md` and
  `docs/glossary.md`.

- S208: the global run cap defaults to 2 busy runs (*proposed*, adj 172).
  An unset or cleared `global_cap` uses 2; an explicit registry value still
  overrides it. A third run queues until a slot frees.
  Docs updated: `PROJECT.md`, `docs/design.md`, `docs/guide.md`,
  `docs/diagrams.md`, `docs/orchestrator-parity.md`, `docs/state.md` and
  `docs/review-2026-10-26-orchestration-trial.md`.

- S24: ork pins and checks a project's slicer contract (*proposed*).
  `ork projects pin NAME --relay N` (N is 10 or 11) runs
  `slicer config relay_contract N --root PROJECT` and reports the pin slicer
  prints; `ork projects unpin NAME` clears it. Neither needs the daemon, and
  ork never writes the pin to `projects.json` (adj 97). `ork doctor` adds
  `contract.projects`: each registered project is asked with its own `--root`,
  and a project fails on `contract_mismatch` or a pin outside the accepted set.
  Engine start refuses a project whose slicer reports `contract_mismatch`,
  with slicer's error naming both versions and no run record. ork now reads
  the contract from `slicer ai relay --json` instead of the first text line.
  The recorded exit code for `contract_mismatch` is 2, not 3. The adj pin is a
  later slice behind adjudicate S45.
  Docs updated: `docs/boundaries.md`, `docs/design.md`, `docs/guide.md`.

- S198: ork accepts slicer relay v11 (*proposed*). The accepted set is v10 and
  v11; v9 is dropped (adj 175). The role prompts are written against v11: the
  gate fails on every `fix` finding (a minor item is `[later]`), and the
  implementer's self-check runs before each review, fix rounds included, and
  reports each check or `n/a`. v11 validation needs the conformance keys
  `role_fallback` and `blocker_wait` and the `gate` entry in `roles[]`; v10
  still needs `vendor_limit`. `stop[]` entries decode as `{key, text}` (v10's
  plain strings still decode). On v11 a gate round closes its ledger step with
  `--phase done --verdict pass|fail`; v10 sends no verdict. The engine's other
  v11 process rules are S204.
  Docs updated: `AGENTS.md`, `docs/boundaries.md`, `docs/design.md`,
  `docs/glossary.md`, `docs/guide.md`, `docs/orchestrator-parity.md`.

- S176: open a role report with `p` in an engine run pane (*proposed*).
  Reports run newest to oldest, then back to the run; `esc` returns at once.
  `run.report` reads only a stored role report name, with regular-file guards,
  terminal controls removed and a 64 KiB bound. Rows carry names only.
  Ledger panes and check logs have no report viewer.

- S177: per-run stop point (*proposed*). `--stop-after commit` on run or wave
  start, or the shared lineup editor's stop row, commits after approval and
  keeps the branch and worktree for the owner to land. The default is `land`.
  Plans confirm the choice; approval prompts, cards and status name it.
  Ready rows keep the slicer land commands; there is no land key or registry
  default.

- S181: wave start and the spec check read slicer's dependency state before
  running roles or admitting any member. A blocked item refuses the whole
  call, naming its blockers, including dependencies selected in the same
  wave. Refusals include blocker titles and a short next step naming what to
  untick or land first. Titles are read once per distinct blocker per call;
  a failed title read falls back to the id. Failed dependency reads still
  prevent launch. Waves keeps the ticks and shows the refusal; the CLI
  refuses before confirmation.

- S175: end a stale ledger run from its pane (*proposed*). On the pane of a
  ledger run that slicer calls stale (no event for 30 minutes), `x` asks
  `slicer relay-run end RUN`, `--outcome stopped --close-steps fail` and
  `End this run as stopped and fail its unfinished steps? [y/N]`, and `y`
  ends the run through the new daemon method `run.end-ledger`. The daemon
  reads the ledger again and refuses a run that is no longer stale, and a
  ledger run that an unfinished engine run owns. Active, waiting and ended
  ledger panes stay read-only. There is no `ork end` shell command.

- S141: approval card follow-ups (*proposed*). `ork check RUN N [ticked|later|open]`
  and `ork fix RUN TEXT` call `run.check` and `run.fix`, so `ork approve` works
  from the shell on a slice with human checks. When the card could not read the
  checks, approval writes a slicer note that says so. The owner's fix note goes to
  every gate round until a gate passes it (a red check or a gate FAIL no longer
  drops it). A note written before a failed save is not written twice. A
  refused owner action no longer leaves an `error:` marker on the run row.
  In the TUI, checks 10 and above are marked by typing the number (digits
  wait for the next digit or enter when the number could be longer); new files
  align with the diff stat; a blank line sets the card's blocks apart above 24
  rows (never taking room the body needs); and the landing line goes before an
  owner finding on a short pane (the push line stays above it). A waiting
  number shows in the hint line, and numbers of 10 or more checks line up.
  Refusals have a new wire code, `refused`: the CLI and the TUI show the text
  alone.

- S139: an empty or missing slice Git section falls back to `ID: slice title`
  from `slicer show` for the commit message. Explicit Git titles keep their
  precedence. There is no pre-launch refusal; retrying a commit pause resumes
  at commit without repeating the finish step.

- S121: renders on the approval card (adj #165, *proposed*). The UI pass
  writes the changed views as text files at 80x24, 120x40 and in ASCII mode in
  `tmp/ui-N/renders/` of the run directory, with the project's own tooling;
  ork has no render code. The card keeps them (plain text without escape
  sequences, at most 24 files of 24 KiB, links not followed, each view's
  80x24, 120x40 and ascii file in that order) and `v view renders` shows them
  in the pane, which then takes the whole body (the Overview rows collapse).
  The card and snapshots hold only each render's name and size; the pane reads
  the text on demand through a new `run.render` call. `run.json` gains
  `card.ui_round`, `card.renders` (name, size) and `card.renders_dropped`.

- S120: finding kinds (adj #163). The reviewer, the UI pass and the gate tag
  each finding `[fix]`, `[owner]` (`[owner blocker]` to block the gate) or
  `[later]`; an untagged finding is a fix. Only fix findings go to the fix
  round. An owner finding is a structured question: it blocks only when
  marked, and otherwise shows in the run pane. A later finding is offered in
  the pane, and `f` then `y` makes ork run `slicer add TITLE
  --discovered-from ID` and `slicer promote` (a Why section from the finding)
  in the project. `run.json` gains `findings[]`; the daemon gains `run.file`.

- S96: first run opens a blank Welcome view. Projects are never seeded.
  The TUI add confirms the exact command, then registers and fetches the
  project. The registry is created on the first add. Empty CLI status shows
  the registration guide; `projects add` without PATH offers the current
  directory in text mode.

- S13: the binary joins the parts. `ork` opens the TUI over the daemon's
  socket and starts the daemon when none answers; `ork status`, `projects`,
  `roles`, `doctor` and `help` go through `internal/cli`; `ork daemon run`
  serves the socket. The start runs `ork daemon run` detached, in its own
  session with its stdio on /dev/null. The TUI's `r` asks the daemon to read
  the project now, and the TUI tells the daemon which project it shows
  (`daemon.Feed` keeps the notice connection apart from the requests). A
  daemon start that races an exiting daemon's lock retries it for two
  seconds.

- S04: the engine core for one slice (`internal/engine`): setup, implement,
  checks, review, gate, awaiting approval, commit and the finish step, run
  against a `Runner` interface (the Claude adapter is S16, the owner surface
  S17), with the slicer ledger, stop rules, owner questions and restart
  reconcile. Run records (`internal/runrec`) hold `writes[]`, every slicer,
  adj and git write, and `locked` slicer writes are retried with backoff. ork
  and `ork doctor` accept a set of slicer contract versions (v7) instead of
  one pin. The role prompts now say ork shows an owner question to the owner
  (and may file it).

- S16: the Claude role adapter (`internal/agent/claude`). Builds the
  `claude -p` command, streams events, classifies the exit (ok, denied,
  unclassified) and saves a raw 0600 fixture candidate for every round that
  is not ok. Tested against a fake `claude` that replays the S01 fixtures.
  Not yet joined to the engine (S17). `clirun` now exports `OwnProcessGroup`
  and `KillGroup`.

- S11: `ork` opens a read-only TUI with project tabs, a badge and bell for
  what needs the owner, and the Overview (Needs you, Running with relays
  started elsewhere, Ready to land). Polls the visible project every 5 s and
  the others every 30 s. Direct.Snapshot now runs its calls at the same time.
  Adds Bubble Tea, Lip Gloss and Bubbles (NOTICE).

- Project docs and definitions: README, PROJECT, glossary, boundaries,
  manual steps, AGENTS. Spec only, no code.
- Dashboard goal: cross-project TUI for roadmaps, decisions and live relays;
  dashboard and state docs.
- Spec review (docs/review-2026-10-18.md) and v1 interview: v1 is the
  monitoring dashboard plus launching slice waves; engine design
  (docs/design.md); 48 owner decisions recorded in adj; license
  GPL-3.0-or-later.
- S03: the daemon (`internal/daemon`), its Unix socket protocol
  (`internal/ipc`) and the first JSON CLI commands (`internal/cli`): status,
  projects add|list|rm, roles print and doctor. Registry version 2 adds
  `daemon_idle_minutes`; version 1 files still load. `clirun` kills a call's
  process group when the call ends, so a grandchild holding stdout no longer
  stalls it. The binary is wired in S13.
