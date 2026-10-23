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

## Layout

```
ork  ?help           | Overview | Questions | Waves | Roadmap | Decisions
> Needs you          | --------------------------------------
  slicer 2*          | Needs you
  adjudicate 1       |   active Overview rows
  DataBall 0         |
  agents 0           | --------------------------------------
                     | 4 projects | refreshed 16:42:00 | pending 0
```

The project sidebar comes from the registry (`registry.initial`,
`ui.project-nav`). "Needs you" starts selected and shows the existing
all-projects Overview. A project selection limits its rows to that project.
The view bar stays at the top of the main pane. Overview, Waves and Roadmap are selectable.

Each project row shows its needs-you count (open adj records, slicer
items flagged `needs-owner`, and engine runs awaiting approval, paused or
stopped). A stopped run stops counting once a newer engine run exists for
the same slice. `*` marks a running slice; engine runs in Needs you do not
set it. `~` marks a loaded project being refreshed. Selection uses reverse
and `>`, including
under `NO_COLOR`. Projects with needs come first, then running projects,
then the rest; registry order breaks ties. Selection follows the project.
A project with no `.slicer/` folder shows `no slicer`, dimmed, in place of its
count. It has no Overview rows and adds nothing to any count or to the bell.
When it is selected, its detail line says to run `slicer init`
(`ui.setup-errors`).

The list scrolls to keep selection visible and shows `+N more` for hidden
rows. Below 100 columns the sidebar narrows; names that do not fit end in
`...` and keep their badges. `s` hides or shows the sidebar at any width.
Keys `right`/`left` move selection and wrap through "Needs you" and the
sorted projects. `1`-`9` select projects in registry order. `tab` and
`shift+tab` switch views in the order of the view bar (`ui.views`,
`ui.view-list`). They skip views not built yet, so Overview, Waves and Roadmap are
reached for now. `/` filters project names without regard to case; `enter`
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
2. **Running:** every running slice across projects, with a stage bar,
   elapsed time and tokens.
3. **Ready to land:** committed engine runs across projects, or for the
   selected project. Each row shows the slice and branch, with
   `slicer land 'ID' --branch 'B' --note 'Landed B'` on its own indented line to
   copy. Ready rows follow Needs you and Running in the cursor order and
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
as `ork status` (`ui.engine-runs-read`). An engine row leads with the slice,
engine state and wait. Denied calls appear as `denied: Bash: git push origin main, Write: /x.txt`
(the tool, then its command or file path, capped at 200 characters; a denial
with no detail shows the tool name alone) from the snapshot's `denials` field
(`ui.denials-display`, `engine.denied-command`). Its linked ledger
run supplies step progress and elapsed
time; without a ledger run, elapsed time starts at the engine record's creation.
The join matches the engine's `relay_run` to the ledger run id, making one row
per run (`ui.run-join-key`). Only unmatched ledger runs say "started elsewhere".

Awaiting-approval, paused and stopped engine runs appear under Needs you.
Other active engine states appear under Running. Committed runs do not appear
in either section. A stopped run disappears once a newer engine record exists
for the same slice, ordered by `created_at`; ledger runs do not clear it
(`ui.stopped-newer`). Needs-you counts include open questions, held items and
these engine waits in both the sidebar and `ork status`.

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
- Stopped: show the full reason with no actions. Stop is final; relaunch to
  continue.

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

Per-role detail still planned in S07 (`dash.role-detail`):
- live output tail;
- tokens and cost from the vendor's usage fields;
- a stale flag when there is no event for N minutes;
- worktree `git diff --stat` and the latest test counts;
- small ASCII charts: stage bars, a token sparkline, and a wave timeline
  with one lane per slice.

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

The open questions first, each with its options and recommendation. The
decided records are on a second tab. The data comes from
`adj inbox --json` and `adj list --json`, called per registered project.
That needs nothing unreleased in adj. Answering owner questions from a
running slice happens on the Waves screen (`owner.questions`).
