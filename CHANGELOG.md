# Changelog

## Unreleased

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
