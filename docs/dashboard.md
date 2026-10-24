# Dashboard

The screens of the ork TUI. Owner decisions are cited by adj key. Layouts
and keys are *proposed* drafts to change after a prototype. Roadmap and
decision editing are not in v1 (`scope.v1`). Their screens are read-only
until then.

## Principles

- **Needs-you first.** Every screen answers one question first: what needs
  the owner now.
- **One keystroke** for every common action on any screen. Mouse clicks do
  the same things.
- **Color is never the only signal.** Every colored state also has a word
  or symbol, and there is a monochrome theme.
- **Show the command.** Every write shows the exact command it will run.
  Launch or stop asks for confirmation; approve runs on one key
  (`ui.confirm`). Retry also confirms.
- **Nothing blocks.** A slow call shows as pending on its row.
- **Errors verbatim.** A failed CLI call shows its `error.code` and
  message. ork does not guess.
- **Bell.** A terminal bell plus a badge on the project sidebar row when something
  newly needs the owner (`ui.notify`).

## Welcome

With no registered projects, ork opens the Welcome view
(`registry.no-seed`, adj 157). It says "No projects yet. Press a to add one".
`a` opens a project path field. Enter with an empty field offers the current
directory only when it holds a `.slicer` directory or `.adjudicate.json`
file. Otherwise the status names the directory: "DIR holds neither .slicer
nor .adjudicate.json; type a path". The hint asks for an absolute path. Esc
cancels path entry and returns to "Press a to add one". Typed paths are
trimmed and sent as typed.

The proposed confirm bar shows `ork projects add 'PATH'? [y/N] (Enter is No)`,
with PATH shell quoted (`ui.confirm`, adj 98).
Enter, n, N and Esc cancel. y or Y adds once through the daemon. While the
add is pending, another y does nothing. The path field shows no cursor
during confirmation or the pending add. Errors show verbatim in the status
area, with the wire error code when present. The field keeps its text.
Without a daemon, the status says "No daemon connection; action unavailable".
On success the Overview appears with "Needs you" selected and status
`added NAME PATH`. The new project is fetched at once and polled.

The proposed Welcome keys keep `?` for help and `q` or ctrl+c to quit
outside text entry; ctrl+c also quits during text entry. View, project,
filter, sidebar and run keys do nothing on Welcome. The add key works only
on Welcome in this slice.

## Layout

```
ork dev  ?help       | Overview | Questions | Waves | Roadmap | Decisions
> Needs you          | --------------------------------------
  slicer 2*          | Needs you
  adjudicate 1       |   active Overview rows
  DataBall 0         |
  agents 0           | --------------------------------------
                     | 4 projects | refreshed 16:42:00 | pending 0
```

The header shows the version next to ork (ui.version-mark, adj #130).
Development builds show the first seven revision characters, with `+` for a
modified build, or `dev` without a revision. A narrow header clips the version
first and keeps `?help`. With the sidebar hidden, the version appears at the
right of the status line when it fits after the existing text.

A stale or unknown daemon build puts
`daemon build differs: ork daemon restart` first in the status line, before
the project and refresh fields. Trailing fields are dropped to fit the pane width.

The six-row text mark appears above `?` help only when the pane can fit the
mark, a blank line and every help line, and is at least twelve columns wide
(ui.version-mark, adj #131). At 100x30 it is omitted so quit stays visible.
`ork --version` and text `ork doctor` also print the mark. Non-UTF-8 locales
use ASCII; `NO_COLOR` and redirected output use no colour. Doctor JSON stays
unchanged.

The project sidebar comes from the registry (`registry.initial`,
`ui.project-nav`). "Needs you" starts selected and shows the existing
all-projects Overview. A project selection limits its rows to that project.
The view bar stays at the top of the main pane. Overview, Waves and Roadmap are selectable.

Each project row shows its needs-you count (open adj records, slicer
items flagged `needs-owner`, and engine runs awaiting approval, paused, or
stopped and neither superseded nor dismissed). A stopped run stops counting
once a newer engine run exists for the same slice or the owner dismisses it
with `ork dismiss RUN` (`ui.dismiss-run`, adj #117). `*` marks a running
slice; engine runs in Needs you do not set it. `~` marks a loaded project
being refreshed.
Selection uses reverse and `>`, including under `NO_COLOR`. Projects with
needs come first, then running projects, then the rest; registry order breaks
ties. Selection follows the project.
A project with no `.slicer/` folder shows `no slicer`, dimmed, in place of its
count. It has no Overview rows and adds nothing to any count or to the bell.
When it is selected, its detail line says to run `slicer init`
(`ui.setup-errors`).

The list scrolls to keep selection visible and shows `+N more` for hidden
rows. Below 100 columns the sidebar narrows; names that do not fit end in
`...` and keep their badges. `s` hides or shows the sidebar at any width.
Outside Decisions, keys `right`/`left` move selection and wrap through
"Needs you" and the sorted projects. On Decisions they switch the Open and
Decided tabs; use `1`-`9` to select a project. To reach
"Needs you" by key, leave Decisions with `tab` and use `right`/`left`.
`1`-`9` select projects in registry order. `tab` and
`shift+tab` switch views in the order of the view bar (`ui.views`,
`ui.view-list`). They skip views not built yet, so Overview, Waves, Roadmap and
Decisions are reached for now. `/` filters project names without regard to
case; `enter`
keeps the filter and leaves text entry, and `esc` clears it. `r` refreshes the
selected project, or all projects on "Needs you". `?` shows keys and `q` quits.

The Overview has a row cursor (`ui.overview-cursor`). `down`/`j` and `up`/`k`
move it to the next and previous row, and so do `n` and `p`. It goes through
"Needs you", Running and Ready, and it stops at the first and last row. The row
under the cursor is drawn reversed. Its full text is on a detail line below the
rows, wrapped at the pane's width so no word is cut. `enter` opens a run's
detail pane; on decision and held-item rows it selects the row's project
(`ui.answer-pane-keys`). Project keys and `1`-`9` select a project and
put the cursor on its first row. Rows are cut with `...` to one line. A run's ID
is never cut: the rest of the run's text is cut first. The rows scroll to keep
the cursor row on screen, with `+N lines above` and `+N more lines` where lines
are hidden.

The terminal bell rings when a project's count rises past an earlier good
reading (`ui.notify`). The status line shows project count, last refresh,
and pending count. Daemon uptime and token charts await their slices.

The TUI reads the daemon's shared snapshot and listens for change notices
(`dash.cache`). The daemon starts when none answers. Each snapshot and
manual refresh uses its own request connection, so later reads can reconnect
and a slow refresh does not hold another project's request. The notice
connection stays open when focus changes. Failed notice feeds retry on the
next timer tick, including when a focused project was removed. Notices that
arrive during a fetch queue a further read. On "Needs you" the daemon focuses
all registered projects; on a project row it focuses that one (`dash.refresh`).
The local fallback schedule reads the selected project's items every 5 s and
other projects every 30 s. On "Needs you", all projects poll every 30 s. Goals
and decided records refresh every 5 min or on HEAD changes and manual refresh. `NO_COLOR` selects the monochrome theme.

## Overview (all projects)

1. **Needs you:**
   - slices awaiting approval after PASS;
   - owner questions;
   - stopped, paused or lost slices;
   - budget pauses;
   - unclassified vendor exits.
2. **Running:** every running slice across projects, with elapsed time and
   an ASCII stage bar after it (S07).
   The *proposed* relay row adds a compact pipeline such as
   `[implementer]>reviewer>gate` and `heard 2m`. An architect leads when
   present. `! heard` warns after 15 minutes; `!! heard` marks stale runs.
   The unselected warning cell uses the warning color. The selected row
   detail includes the full pipeline and heard cell even when the row drops
   them. Stale runs stay under Running, dimmed and labelled stale; elapsed time
   freezes at their last event. Narrow rows drop heard, pipeline, role details,
   then the stage bar, whole. The first item title is not added.
   For relays started elsewhere, the *proposed* row details (also indented
   under each relay in `ork status`) are `running: ROLE VENDOR/MODEL: TASK`
   for the last step in ledger order with phase `start` (a working subagent
   wins over its orchestrator), then `failed: ROLE: TASK` for the last step
   with phase `fail`; details that do not fit the Overview row are dropped
   whole, a task that partly fits is cut with `...`, and the selected row's
   detail shows the full text.
3. **Recent (proposed):** ended ledger runs within the last hour, newest
   first. Rows show `✓ pass` for pass, `✗ fail` for fail, and `~ stopped` for
   any other outcome, then items, run time,
   `ended N ago`, and counts such as `impl 1  review 1  gate 0`. Empty state: `none`.
   Rows follow Running in cursor order. Enter opens a read-only ledger pane.
4. **Ready to land:** committed engine runs across projects, or for the
   selected project. Each row shows the slice and branch, with
   `slicer land 'ID' --branch 'B' --note 'Landed B'` on its own indented line to
   copy. Ready rows follow Needs you, Running and Recent in the cursor order and
   scroll into view with their command lines. The selected row shows full
   commands wrapped in the detail area; other rows keep clipped commands.
   Enter does nothing on these rows. Run action keys explain that only
   cleanup is available on a merged row.
   An unknown merge state says "merge state unknown" and still offers the
   land command; slicer checks that the branch is merged. A known unmerged
   branch says "merge first" with no command.
   A known merged branch also offers `slicer render`, since render files can
   be stale after a merge (R8). The daemon supplies merge state when a
   project's HEAD changes (`landing.merge-detect`, adj #128). A missing
   branch says "branch unknown" with no command. With no committed runs,
   the section says "none" (`landing.v1`). Merged branches are detected and
   offered for cleanup (`git.cleanup`, `landing.v1`). On a selected merged row, `c` shows
   a y/n confirmation with each shell-quoted command on its own line:
   `git -C PATH worktree remove WORKTREE` then
   `git -C PATH branch -d BRANCH`. The question is on a separate line.
   Enter is No; `n` cancels; `y` calls `run.cleanup` once (`git.cleanup-key`, adj #152).
   Cleanup never forces removal, merges or lands a branch. Git errors are
   shown verbatim.

The Overview reads engine runs in the daemon snapshot, using the same builder
as `ork status` (`ui.engine-runs-read`). An engine row leads with the ork run
id, then slice, engine state and wait. Denied calls appear as
`denied: Bash: git push origin main, Write: /x.txt`
(the tool, then its command or file path, capped at 200 characters; a denial
with no detail shows the tool name alone) from the snapshot's `denials` field
(`ui.denials-display`, `engine.denied-command`). Duplicate texts appear once,
with a repeat count such as `Write: /x.txt x2`, in first-seen order across roles.
At most three denials appear, then `and N more` counts the remaining texts.
When set, the *proposed*
`error` text follows the denials as `error: TEXT`. Its linked ledger
run supplies step progress and elapsed
time; without a ledger run, elapsed time starts at the engine record's creation.
A removed project's non-queued runs appear in a removed group named from
the stored project name. Needs you marks them `! removed`. The sidebar uses
`removed` and dims the label. Running rows with an error use `! error` so the
failure stays visible when the rest of the row is cut. The run id stays first.
The *proposed* `unavailable` flag disables retry, stop, approve and answer on a row.
The group is read once at TUI startup. It is not polled, refreshed, focused,
counted as a registered project, or offered for launches. It is separate from
the no-slicer setup case, whose rows and counts stay hidden (adj 83).
`ork status` marks the group as `(removed)`. A run's error is indented by four
spaces and stays on one line. The *proposed* wire flag is named `unavailable`.
Action keys and the pane explain: `No actions: project "NAME" is not registered.`
They direct the owner to re-add the project with
`ork projects add PATH --name NAME`, then run `ork daemon restart`.
The wire row's wait text gives the same steps. With no registered projects,
Welcome still offers Add. Removed groups remain reachable from the sidebar.
Re-adding the same name replaces its removed group.
Stopped rows show their age from the *proposed* optional `updated_at` field.
The age uses the largest unit: `45s ago`, `12m ago`, `3h ago`, or `2d ago`.
A stopped row with no `updated_at` shows no age.
The join matches the engine's `relay_run` to the ledger run id, making one row
per run (`ui.run-join-key`). Only unmatched ledger runs say "started elsewhere".
A ledger run linked to a committed or superseded engine run is not shown.
This is by design. Ready-to-land rows do not show the run id.

Engine runs that are awaiting approval, paused, or stopped and neither
superseded nor dismissed appear under Needs you. Their kinds are
`+ approve`, `= paused`, and `x stopped`. Each kind is a symbol and a word,
so monochrome still reads it.
Other active engine states appear under Running. Committed runs do not appear
in either section. A stopped run disappears once a newer engine record exists
for the same slice, ordered by `created_at`; ledger runs do not clear it
(`ui.stopped-newer`). The owner can also clear a stopped run with
`ork dismiss RUN` (`ui.dismiss-run`, adj #117). Dismissal keeps its run
record and removes it from the snapshot, Needs you and `ork status`.
A dismissed run still hides older stopped runs of the same slice.
Needs-you counts include open questions, held items and these engine waits
in both the sidebar and `ork status`. `ork status` text lists each open
record as `  #ID KEY  TITLE` and each held item as
`  SID  TITLE  (needs-owner)` before relay and engine rows. Its engine list
uses the same Needs, Running and Hidden classification as the Overview, so
the needs list matches the count (`ui.status-list`, adj #125).
`ork status --json` includes hidden engine runs still in the snapshot and
the *proposed* optional `open:[{id,key,title}]` and `held:[{id,title}]` fields.
Those fields are omitted when empty.

On the cursor's engine row, `a` approves an awaiting-approval run immediately,
`x` stops a run, and `t` retries a paused run. Stop and retry show the exact
command (`ork stop RUN` or `ork retry RUN`) in the detail area and ask
`[y/N] (Enter is No)`. Only `y` confirms; `n`, `esc` and Enter cancel. The
confirmation keeps its original run target while snapshots change. Approve
shows `ork approve RUN` as pending, then its result. Calls run asynchronously;
every completed call refreshes the project's snapshot, and a failed call shows
its code and message verbatim in the detail area. Actions on other runs remain
available while a call is pending. Stop can interrupt a pending approve or
retry; another action on that run names the command that is pending. A stopped
run refuses stop. Each result names its command; the latest completed call
replaces the previous detail message. Moving to another row or project clears
the message; a late result appears only if its run is still selected.
Ineligible actions explain why there,
and ledger rows started elsewhere have no actions. `esc` dismisses
a pending message or action result and restores the selected row's detail.
Hiding a pending message leaves its call running.

Retry uses the optional engine `pending` kind (`ui.pending-kind`). When it is
`question`, the detail area says to answer with `ork answer RUN TEXT` instead.
Other pending kinds, including an absent kind from an older snapshot, offer
retry. Launch is available from the Roadmap ready list described below.

### Run detail pane

`enter` opens the cursor's run below the Overview. The pane keeps that run's
ID while snapshots reorder the rows. It shows the full `wait` reason before
the run tree. `pgup` and `pgdown` scroll the reason and tree while the action
area stays visible. `esc` closes the pane (`ui.answer-ui`,
`ui.answer-pane-keys`, `ui.run-tree`). Ledger runs started elsewhere open a
read-only tree pane with no action keys or answer input. Its header says
"started elsewhere". Its footer says
"started elsewhere, read-only; esc close pane".

The actions follow the state (`ui.answer-pane-scope`):

- Paused with `pending: question`: show the question and an Answer field.
  The exact `ork answer RUN TEXT` command follows the text live. Enter submits
  without y/n; an empty or whitespace-only answer is refused. The field owns
  typing keys, including the global action letters. Backspace deletes the
  previous rune, ctrl-u clears the field, and left/right move the cursor.
  Terminal paste inserts at the cursor and strips CR and LF, keeping one
  answer line; paste never submits (`ui.text-field`).
- Paused without a question: show the reason and `t` retries, with the same
  `ork retry RUN` y/n confirmation as the Overview.
- Stopped: show the full reason with no key actions. Stop is final; relaunch,
  or clear the row with `ork dismiss RUN`.

Awaiting-approval runs offer `a` approve and `x` stop; other active runs offer
`x` stop. A successful answer clears the field and refreshes the pane and row.
A failed call shows
its error code and message verbatim and permits another attempt. Closing the
pane leaves a pending call running. No structured answer options are offered;
the separate Questions tab follows in S27.

A failed call is an error row. A project has one error row per error code.
The parts that failed with that code come first, then the code, then the
tool's message as printed, for example `items, runs: locked: Ledger is busy.`
Different messages under one code are joined with ` / `. An items or inbox
failure sits under Needs you. A runs failure alone sits under Running. Error
rows never count toward the badge (`ui.setup-errors`).

## Waves (one project)

The Waves composer is available (S82). Select one project. `tab` and
`shift+tab` reach it between Overview and Roadmap, skipping Questions until
that view is built. It loads `wave.suggest` when opened or the project changes.
Rows follow slicer's wave order. Unplaced items include their reasons.

S60 provides the CLI: `ork wave suggest PROJECT` prints the ordered waves
from `slicer plan-parallel`, including reasons. `ork wave start PROJECT
ITEM...` takes the chosen item ids (`wave.start-input`), plans each item,
then asks for confirmation. `--yes` requires explicit `--well-specified
yes|no` and `--split yes|no`; these flags apply to every item. One wave
contains one project (`wave.scope`). Its runs share a wave id and respect
the global and project caps. A failed or stopped item leaves the others
running (`wave.failure`). `ork stop WAVE` uses the `wave-` prefix
(`wave.stop-target`) and stops each unfinished member once, including
queued members without tool writes.

The composer uses `wave.suggest` and `wave.overlap` reads from S81 and
`wave.start` with per-item launch inputs and confirmed plans (`run.launch-wire`).
No launch writes happen before confirmation. Errors show their code and
message verbatim, including per-item launch errors.

```
Suggested (slicer plan-parallel)
Items                   Lineup (* edited for this run)
  Wave 2
  [x] A  Alpha            implementer*: claude custom-model high
                          reviewer: claude opus medium
                          gate: claude opus medium
  [x] B  Beta             implementer: claude sonnet high
                          reviewer: claude opus medium
                          gate: claude opus medium
  Wave 4
  [ ] C  Gamma
  Unplaced
  [ ] D  Delta (no_files)
  ! A and B both touch shared.go
  ! A and B both touch second.go
  [space] tick item   [e] edit lineup   [l] launch wave
```

`space` ticks or unticks the selected item. Ticking shows both y/n inputs
at once, with no defaults: well-specified and split. `tab` or `shift+tab`
switches the active field; `enter` validates both and plans the item.
`esc` cancels that tick. The Lineup column fills from the plan, including
its fallback chains (`ui.wave-composer`). `down`/`j`/`n` and `up`/`k`/`p`
move between items. Unplaced items can also be ticked.

Two or more ticked items trigger an overlap read. Each shared path gets a
warning; warnings permit launch (`wave.selection`). `l` launches the ticked
items with one y/n confirmation showing `ork wave start PROJECT ID...`.
Enter, n and esc cancel; y submits each item's answers and confirmed plan.
Every ticked item must have a successful plan before launch. One failed
plan prevents launching the chosen set; the owner can untick it. The
wave.plan-failure decision remains open (adj 146).

`e` edits the selected ticked item after its plan is ready (S83).
The editor starts from that item's plan: implementer and reviewer, then
budget minutes and tokens. The gate follows the reviewer and has no field.
`up`/`down`, `tab` and `shift+tab` move between rows.
`left` and `right` cycle the role's chain already in
`RunPlan.Lineup[role]`: primary first, then alternatives. Until S49, the
daemon plan has one row per role, so left and right have no effect; use
`c` to change a role. The TUI adds no matrix read.
`c` opens vendor, model and effort fields for a custom primary entry.
`tab` and `shift+tab` switch fields; `enter` uses the custom entry.
Cycling moves the chosen row to the front; the rest keep their order.
Custom entry replaces the primary; fallback entries cannot be edited here.
Budget fields accept whole numbers of zero or more; zero uses the project
default, following the daemon's budget semantics.
`enter` saves and replans; unchanged values do not call Plan.
Launch waits for the replan. Errors show verbatim and keep the old plan
and overrides. `esc` discards the editor's changes, including custom fields.
The editor marks roles whose chains differ from its starting plan with `*`.
The Waves Lineup column marks overridden roles with `*` and shows saved
budget overrides as `budgets*: MIN min, TOKENS tokens`.
Edits apply to that run only and travel with that item's confirmed plan;
the matrix is unchanged (`ui.lineup-editor`, `lineup.override-scope`).

```
Edit lineup: A  Alpha
  Run only; gate follows reviewer.
  > implementer: claude sonnet high (no alternatives; c for custom)
    reviewer: claude opus medium
    Budget minutes: 45
    Budget tokens: 9000
  Zero budget uses the project default.
  [up/down/tab] row   [left/right] cycle   [c] custom
  [enter] save and replan   [esc] discard
```

The run tree (S55) appears in the Overview detail pane. The wave charts and
animations below remain proposed work for S07.

Each running slice expands to its roles:

```
s374  review  round 1  23m  41k tok  $0.62   [■■■■■□□□] ▁▃▅▇▅
  implementer  claude sonnet high   done     +142 -12  tests 2621 ok
  checks       go test / slicer     green    58s
  reviewer     claude opus medium   running  ⠹ reading diff
  └ tail: "Checking rollback path in affected.py:88 ..."
```

The detail pane shows ledger steps under their parent, with the orchestrator
at the root (`ui.run-tree`). Unknown parents appear at the root. Cycles visit
each step once. An engine run uses its linked ledger tree when available;
otherwise its recorded role rounds appear flat under the run. The two sources
are never combined.

Each node shows `glyph role phase elapsed  vendor model effort` in that order.
Role and phase columns use text widths to align the phase and elapsed time,
with padding capped to fit deep nodes. Engine role nodes add `round N` on the
same line. A `start` step has an ASCII spinner (`| / - \`) and uses the heading
style (bold cyan in colour, bold in mono). A `done` step has `+` and is dim
(grey in colour, faint in mono). A `fail` step has `!` and uses the error style
(bold red in colour, bold in mono). Other phases use a neutral `.` and retain
their raw text in plain style. An unfinished engine role in a stopped, paused
or committed run uses that state word and the neutral glyph.
Elapsed time advances from the injected clock for running steps and stops at
`updated_at` for finished steps. Task text is dim, clipped to the pane width,
and starts four columns past its node's glyph. The run header uses the
heading style.

S07 adds a stage bar, a pulsing running cell and an activity sparkline.
Live output tail, tokens and cost, worktree diff stats, test counts and a wave
timeline remain future role detail. The activity series counts vendor stream
events, not tokens. There is no bell or budget bar in S07.

The actions are approve, stop, answer, retry on another vendor, and open
the worktree path. Relays started outside ork appear with a "started
elsewhere" marker and no actions (`dash.external-runs`).

## Roadmap (one project)

Goals head the screen. The queue follows `slicer list` order and shows each
item's id, short title (or title), readiness, status, size and effort,
and the selected slice in a detail pane. Readiness comes from status and
dependencies only. An item is ready when `slicer deps` includes it, its status
is open and it has a slice. No claim is shown (record 140, `roadmap.readiness`,
decided 2026-10-21). The data comes from
`slicer list --json --lean`, `slicer show ID --json --lean` and
`slicer goals --json`, plus `slicer deps --json --lean`.
A Goals error keeps the list. A Deps error makes readiness unknown (`?`).
`enter` loads the selected item's spec and shows pending until it arrives.
The detail wraps and clips to the pane. `esc` returns to the list.
On "Needs you", select a project to see its roadmap.
`l` opens a ready list when a project is selected and no item detail is open.
It lists only open items with a slice included by `slicer deps`, in slicer
list order. Each row shows id, short title (or title), size and effort.
There is no claim check (record 140). A Deps error leaves nothing launchable.
An empty list says "No ready slices". Without a daemon, launch says
"No daemon connection; action unavailable". Up/down, j/k and n/p move through
the list. `enter` picks a slice; `esc` cancels.

Launch asks "Well specified? [y/n]:" and then "Split? [y/n]:". Neither has a
default. Enter submits y, yes, n or no in any case; invalid input asks again.
The TUI calls `run.plan` after both answers and shows "pending: run.plan".
The plan summary shows project, item, worktree, branch, check command, and
implementer, reviewer and gate lineup entries as vendor, model and effort,
with each role's permissions. The confirmation bar shows
`ork run PROJECT ITEM? [y/N] (Enter is No)`. The displayed command omits the
two answers. `y` or `Y` calls `run.start` with those answers and the displayed
plan as Expected. At confirmation, Enter, n, N and Esc cancel. Esc cancels
an unfinished plan. Changing project or view drops it, and late plan replies
do not restore it. Once Start is sent, Esc and navigation close the launch
without cancelling Start. The status stays `ork run PROJECT ITEM: pending`.
Pressing `l` for that project says `ork run PROJECT ITEM is pending` until
the reply. Success shows `ork run PROJECT ITEM: done` and refreshes the
project, even after leaving the Roadmap or project. Plan and Start errors
show their code and message verbatim. A failed Start is not retried.

Adding an item to the wave composer comes in a later slice.
Editing comes after v1, and `ui.slicer-tui` is open.

## Decisions (one project or all, read-only in v1)

Open questions are on the first tab, each with its options, tradeoffs and
recommendation. The active tab uses the view heading style, distinct from the
record cursor. The scope heading names the project or says
`All projects using adj`. The final body line lists the tab, scope and
open-record keys. On "Needs you", it omits the scope toggle hint. `left` and
`right` switch the Open and Decided tabs. `a`
toggles between the selected project and all projects that use adj. The toggle
persists when switching projects. With "Needs you" selected, Decisions shows
all projects that use adj. A project without adj shows `no adj in this project`.
If no registered project uses adj, the all-projects view says `No project uses adj`.

All-projects rows put the project name first, padded to the widest adj project
name in scope, capped at 20 columns. Each record takes one line, with a clipped
title. The cursor highlights the whole line. Tradeoffs and recommendation text
are dimmed and indented under their labels. Rows follow registry order. Within
each project, Open keeps `adj inbox` order (oldest first) and Decided keeps
`adj list` order. `down`/`j`/`n` and `up`/`k`/`p` move the record cursor.
`enter` opens the record from the snapshot, including options, recommendation,
decision, rationale and history; `esc` returns. The detail has project and record
headings, indented body text and a separate History heading. Row keys scroll
one line; `pgup`/`pgdown` scroll one page. Scrolling stops at either end. Leaving
the view or selecting a project closes the detail and resets the record cursor.
There is no run cursor or write action on this screen. Decisions help hides
Approve and Add, and explains how to reach "Needs you" through another view.

The data comes from `adj inbox --json` and `adj list --json --status decided`,
called per registered project. Failed inbox or list calls show their code and
message with wrapped continuations aligned under the message, while other
projects' rows remain visible. Non-visible projects poll every 30 seconds, so all-projects rows can be up to 30 seconds old. Opening a
record makes no additional adj call. Answering owner questions from a running
slice happens on the Waves screen (`owner.questions`).

The run pane's *proposed* board details keep the S55 tree. A pipeline above
it colors finished roles green, running cyan, failed red, and waiting dim.
The running role uses brackets only for live, non-stale runs. In monochrome
failed is bold and waiting is dim; passed is not bold. Warning text is plain
with the `!` cue and no underline. `round N` counts implementer steps, appears
only when positive, and drops first when narrow. For live runs, `last heard
N ago` is dim through 15 minutes, warning after 15 minutes, and error for a
stale run. Ended, stopped, paused and committed runs have a dim pipeline,
no waiting rows. Their last-heard age stays dim, with no warning. A
live run adds waiting rows only for pipeline roles with no lane step;
queued steps keep their existing tree row. Waiting rows share the tree's
phase column. Repeated roles carry `xN` on the latest tree node. A failed
gate has a `✗ gate FAIL -> implementer round N` line beneath it, followed by
`: REASON` only when the task is non-empty. Started steps
in stale or ended ledger runs use `~` and stop counting at the last event.
Engine runs without a ledger derive pipeline and round from role records,
and last heard from the latest role timestamp.

S07 adds an ASCII bar above the pane pipeline. Each role has a two-column
cell: done `##`, waiting `..`, failed `!!`, and running `~~`, `~=`, `==`, `=~`.
The normal order is implementer, reviewer, gate; an architect leads when
present. A new implementer round reopens its cell and resets earlier reviewer,
gate and architect steps to waiting. `rN` follows the bar from round 2 on
(`r2`, `r3`).
The pipeline also resets earlier-round results. Finished cells never pulse.
Stopped, paused, stale and ended runs freeze the running cell at `~~` and
show no sparkline.

For the current engine role, a line below last heard reads
`activity <10 glyphs>  last 5m`. Ten wall-clock 30-second buckets count vendor
stream events, oldest first, scaled to the maximum in the window. No events
for five minutes gives a flat line before the stale cutoff. A new role attempt
starts empty. A source-less started-elsewhere run has no sparkline, but still
gets a bar from its steps. UTF-8 locales use `▁▂▃▄▅▆▇█`; other locales use
`._-=+#`. Locale precedence is LC_ALL, LC_CTYPE, LANG. This changes only
sparkline glyphs; the bar is always ASCII and existing check/cross glyphs stay.

A separate 250 ms frame timer advances the bar and tree animation while the
Overview has a Running-section or live ledger row, or a live run pane is open.
It arms once and disarms on the next frame when no run qualifies. Idle ork
has no 4 fps timer. The existing one-second schedule tick stays unchanged.
