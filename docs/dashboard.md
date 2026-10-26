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
  Launch or stop asks for confirmation. Retry also confirms. Approve on an
  Overview row also asks (S119, owner decision 2026-10-26: it lands the slice
  and cleans up), while the approval card's own `a` runs on one key
  (`ui.confirm`).
- **Nothing blocks.** A slow call shows as pending on its row.
- **Errors verbatim.** A failed CLI call shows its `error.code` and
  message. ork does not guess.
- **Bell.** A terminal bell plus a badge on the project sidebar row when something
  newly needs the owner (`ui.notify`).

## Welcome

With no registered projects, ork opens the Welcome view
(`registry.no-seed`, adj 157). It says "No projects yet. Press + to add one".
`+` opens a project path field. Enter with an empty field offers the current
directory only when it holds a `.slicer` directory or `.adjudicate.json`
file. Otherwise the status names the directory: "DIR holds neither .slicer
nor .adjudicate.json; type a path". The hint asks for an absolute path. Esc
cancels path entry and returns to "Press + to add one". Typed paths are
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
  Running         0  | --------------------------------------
> Needs you       3  | Needs you 3
  slicer          2* |   Decisions 2
  adjudicate      1  |   ? slicer      #11  ui.layout  Group rows?
  DataBall           |   ? adjudicate  #3   out.shape  JSON shape?
  agents             |   Held 1
                     |   # slicer      S12  Roadmap screen
                     | Running 0
                     |   nothing running
                     | --------------------------------------
                     |   #11 open | 2 options | recommends grouped
                     |   Group rows?
                     | --------------------------------------
                     | 4 projects | refreshed 16:42:00
```

The sketch uses ASCII rules. The view bar, sidebar edge and horizontal rules
use box-drawing lines when `cfg.ASCII` is false, including the rule above the
detail. The CLI already resolves that setting with `brand.ASCIILocale`.

The header shows the version next to ork (ui.version-mark, adj #130).
Development builds show the first seven revision characters, with `+` for a
modified build, or `dev` without a revision. A narrow header clips the version
first and keeps `?help`. With the sidebar hidden, the version appears at the
right of the status line when it fits after the existing text.

A stale or unknown daemon build puts
`daemon build differs: ork daemon restart` first in the status line before
the project and refresh fields, unless the behind notice is shown (see
Self-hosting). The behind notice replaces the stale-build marker. At narrow
widths, trailing fields are dropped first so the behind notice keeps priority.

The six-row text mark appears above `?` help only when the pane can fit the
mark, a blank line and every help line, and is at least twelve columns wide
(ui.version-mark, adj #131). At 100x30 it is omitted so quit stays visible.
`ork --version` and text `ork doctor` also print the mark. Non-UTF-8 locales
use ASCII; `NO_COLOR` and redirected output use no colour. Doctor JSON stays
unchanged.

The project sidebar comes from the registry (`registry.initial`,
`ui.project-nav`). "Needs you" starts selected and shows the existing
all-projects Overview. A project selection limits its rows to that project.
The view bar stays at the top of the main pane. Overview, Questions, Waves,
Roadmap and Decisions are selectable.

**Running (*proposed*, S110, adj 169 `ui.sidebar-running`).** The first sidebar
entry, above "Needs you", opens a live summary of every project that has a live
run. It is always shown, so no row moves when a run starts: it carries the
count of live runs, and with none it is dim and its view says `nothing running`.
It is reached with the sidebar keys `]`/`[` like any other entry, not
with a digit key (`1`-`9` stay the projects, `Jump` is unchanged). "Needs you"
still starts selected. While the entry is selected the first tab of the view bar
reads `Overview (Running)`; the view is the Overview's Running section full
screen, then Recent, with no Needs you or Ready section. Each live run is one
row in project order: the project, then the item, the stage bar (with ` rN`
from the second round, as everywhere), last heard beside the bar (with the `!`
and `!!` cues of the run pane; S106 moved it ahead of the pipeline), the
pipeline with the running role in brackets and the ten-bucket activity
sparkline. A run that waits on the owner is marked by one
glyph in the mark slot before its project name, as a kind symbol is on
Needs you: `?` for a paused run that asks the owner, `x` for a stopped one. The
glyph is the only change to its row, so no row moves or widens. The heading
counts the runs and adds `! owner questions N` and `x stopped N`, and the
detail line of the selected row keeps the words. A run that waits on the owner
stays on this view until it is answered or dismissed; a paused run with no
question, a run awaiting approval and a removed run stay on Needs you only.
The columns line up: the item, the bar and the pipeline are padded to the widest
row. The cells shown are chosen once for the whole view, from the widest row,
so every row shows the same cells (the selected row alone may be clipped). As
the pane narrows the cells go in this order: activity first, then the pipeline
(down to the bracketed running role), then last heard. The item, the stage bar
and the running role stay, so the view is complete at 80x24. The row cursor,
`enter` (which opens that run's pane, as on the Overview), the action keys and
the pane work as on the Overview; the pane follows the newest running run
(S109). The selected row's whole text, with the run ID, state and elapsed time,
is on the detail line, which wraps between cells and never inside one. A run
that ends leaves Running and shows under Recent on the next snapshot, with no
keypress.

"Needs you" shows the aggregate Needs you row count, including error and
pending rows, matching the main heading (`ui.overview-layout`, adj 149).
Per-project counts keep their existing meaning and hide zero counts.
Badge numbers align in a digit column; the next column reserves `*` for a
running project or `~` for a refresh. During refresh, `~` takes precedence.
Each project row shows its nonzero needs-you count (open adj records, slicer
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
In every view, keys `]` (next) and `[` (previous) move selection and wrap
through "Running", "Needs you" and the sorted projects
(`ui.project-keys`, adj 160). `left` and `right` never change the project:
they belong to the view's own tabs and fields (the Open and Decided tabs on
Decisions, the lineup, and text fields such as an answer). Outside those they
do nothing.
`1`-`9` select projects in registry order. `tab` and
`shift+tab` switch views in the order of the view bar (`ui.views`,
`ui.view-list`). All five views are available: Overview, Questions, Waves,
Roadmap and Decisions. `/` filters project names without regard to
case; `enter`
keeps the filter and leaves text entry, and `esc` clears it. `r` refreshes the
selected project, or all projects on "Needs you". `?` shows keys and `q` quits.

The Overview has a row cursor (`ui.overview-cursor`). `down`/`j` and `up`/`k`
move it to the next and previous row. It goes through
"Needs you", Running and Ready, and it stops at the first and last row. `n` and
`p` do not move rows; `n` is No in a confirmation. The row
under the cursor is drawn reversed across the pane width. A rule separates
the rows from its detail. Decisions show status, a nonzero option count and
the recommended label when present, then the wrapped title. Metadata uses
the theme's vertical rule as its separator. An empty detail block adds no
extra rule or band. Detail height reserves room for the status line. Other rows show
their full text, wrapped at the pane's width so no word is cut. `enter` opens
a run's detail pane; on decision and held-item rows it selects the row's project
(`ui.answer-pane-keys`). Project keys and `1`-`9` select a project and
put the cursor on its first row. Rows are cut with `...` to one line. A run's ID
is never cut: the rest of the run's text is cut first. The rows scroll to keep
the cursor row on screen, with `+N lines above` and `+N more lines` where lines
are hidden.

The terminal bell rings when a project's count rises past an earlier good
reading (`ui.notify`). The status line shows project count, last refresh,
and the pending count only when it is nonzero. Daemon uptime and token
charts await their slices.

The TUI reads the daemon's shared snapshot and listens for change notices
(`dash.cache`). The daemon starts when none answers. Each snapshot and
manual refresh uses its own request connection, so later reads can reconnect
and a slow refresh does not hold another project's request. The notice
connection stays open when focus changes. Failed notice feeds retry on the
next timer tick, including when a focused project was removed. Notices that
arrive during a fetch queue a further read. On "Needs you" and "Running" the
daemon focuses all registered projects; on a project row it focuses that one
(`dash.refresh`).
While a visible project has a live run, the daemon reads its runs every 1 s
(*proposed*, S109) and the TUI refetches on the notice; see `docs/design.md`.
On "Running" that holds for every project with a live run at once (S110); a
visible project without one stays at 5 s.
The local fallback schedule reads the selected project's items every 5 s and
other projects every 30 s. On "Needs you" and "Running", all projects poll
every 30 s. Goals
and decided records refresh every 5 min or on HEAD changes and manual refresh. `NO_COLOR` selects the monochrome theme.

## Overview (all projects)

Needs you and Running headings carry row counts. Needs you groups rows as
Decisions, Held, Stopped, Paused, Approve and Errors, omitting empty groups
(`ui.overview-layout`, adj 149). Errors includes failed calls, removed runs
and projects awaiting their first reply. Cursor order follows these groups,
keeping the existing order within each group. Each row retains its kind
symbol (`?`, `#`, `x`, `=`, `+`, `!` or `~`). Decision IDs, keys and titles
align in separate columns; keys use at most a third of the pane and longer
keys end in `...`. Held rows omit the redundant flag suffix. On the all-projects view,
Needs you, Running, Recent and Ready rows show a project column sized from
projects with any row. Selecting one project hides that column. A loaded
project refreshing shows `~` before its name. Ready commands hang beneath
the text column, falling back to a shorter indent when needed to fit.

1. **Needs you:**
   - slices awaiting approval after PASS;
   - owner questions;
   - stopped, paused or lost slices;
   - budget pauses;
   - unclassified vendor exits.
2. **Running:** every running slice across projects, with elapsed time and
   an ASCII stage bar after it (S07). Engine rows have no label of their own.
   When every Running row is an engine row without an error, the rows start at
   the run id with no blank label column. When any Running row has a label
   (`> started elsewhere` or `! error`), engine rows keep the padded label
   column so the run ids line up (S33).
   The *proposed* relay row adds `heard 2m` beside the stage bar (and its
   `rN` from the second round) and then a compact pipeline such as
   `[implementer]>reviewer>gate` (S106 put heard ahead of the pipeline). An
   architect leads when present. `! heard` is amber after 10 minutes with no
   event; `!! heard` is red after 30 minutes, the stale cutoff, and marks stale
   runs. The unselected cell uses the warning color (amber) or the error color
   (red); in monochrome the words `!` and `!!` carry the cue, and red is bold.
   The selected row detail includes the full pipeline and heard cell even when
   the row drops them. Stale runs stay under Running, dimmed and labelled stale; elapsed time
   freezes at their last event. Engine rows show total `tokens N` only when
   engine usage is available. Narrow rows drop tokens first, then pipeline,
   role details (the `running:` and `failed:` steps), then heard and the stage
   bar, whole. A heard cell that carries a `!` or `!!` cue outlasts the bar:
   when the two do not fit, the bar goes and the cue stays (S106). The Running
   entry is unchanged: it always keeps the bar and drops heard before it. The first
   item title is not added.
   *Proposed* (S106): on a terminal narrower than 100 columns, such as 80x24, a
   run row drops its run ID and the vendor and model of its running step (`running:
   reviewer codex/gpt-5: task` reads `running: reviewer: task`). The ID
   stays in the pane title and on the selected row's detail line, and both stay
   on rows at 100 columns and wider. Until the terminal reports a size the rows
   are whole.
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
4. **Ready to land:** committed engine runs that ork did not land, plus a landed run
   whose cleanup failed (see below), across projects, or for the
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
   shown verbatim. After S119, a run that ork landed itself leaves this list, and
   its ledger run reads `✓ landed` in Recent instead of `✓ pass`. A run that ork
   landed but could not clean up stays here. Its row reads `S11  s11  landed
   cleanup failed`; the line under it is the reason (the failed git command and
   its error), which is not one of the copy lines, and there is no landing
   command. Its detail reads `c finishes the cleanup: git -C P worktree remove W;
   git -C P branch -d B`, and `c` runs it (and clears the reason).

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
`+ approve`, `= paused`, and `x stopped`. A paused owner question uses
`? question` in amber instead. Overview and the project page also show an
amber `! owner questions N` alert beside the Running count while questions
are waiting. In monochrome, question kinds and the alert are bold. Each kind is a symbol and a word,
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

On the cursor's engine row, `a` approves an awaiting-approval run (once its
human checks are marked; see the approval card), `x` stops a run, and `t`
retries a paused run. Stop and retry show the exact command (`ork stop RUN` or
`ork retry RUN`) in the detail area and ask `[y/N] (Enter is No)`. Approve asks
like the cleanup does: the command `ork approve RUN`, then the question on its
own line, naming what the approval does: `Land ITEM, then remove its worktree and
branch? [y/N] (Enter is No)`, or `Land and push ITEM, then remove its worktree
and branch? [y/N] (Enter is No)` when the project's `push` setting is on. When
the question and `[y/N] (Enter is No)` do not fit one line, the keys go on a line
of their own, so they are never cut. In the
pane, on the approval card, `a` still approves at once; the card's own line says
the same (below). Only `y` confirms; `n`, `esc` and Enter cancel. The
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

`enter` opens the cursor's run inline under its row (*proposed*, S106): the
rows after it move down, so no blank lines come before the pane and the
sections keep their order, "Needs you" first (`ui.overview-layout`, adj 149).
It sits under the row's landing commands on a Ready row and under the Running
entry's row on that entry. The pane keeps that run's
ID while snapshots reorder the rows, and stays under that run's row even when
the cursor is on another. If the run has left the snapshot the pane stays under
the cursor. Every pane line is indented four columns, so the pane sits inside
its row, and its title is "Run" and the run ID alone, in the row title style
(bold), not the section heading's colour: the row above names the item and the
stage. The Questions tab, with no row above its pane, keeps the full header.
The body scrolls to the pane's last line, and the pane takes at
most `height - 10` lines (`height - 7` under 24 lines), so its row and the
rows around it stay on screen. It shows the full `wait` reason before
the run tree. `pgup` and `pgdown` scroll the reason and tree while the action
area stays visible. `esc` closes the pane (`ui.answer-ui`,
`ui.answer-pane-keys`, `ui.run-tree`). Ledger runs started elsewhere open a
read-only tree pane with no action keys or answer input. Its footer says
"started elsewhere, read-only; esc close pane" (the inline header is the run ID
alone).

The pane follows the newest running run (*proposed*, S109). When the pane's
run is over (an engine run that is stopped, superseded or committed, or a
ledger run that has ended), the next snapshot points the pane at the newest
running run of the same project: an engine run in the Running section, or an
active ledger run that no engine run links to and that is not stale. The new
pane starts at the top, with no answer or filing state, and a line under the
header says why, cause first, for example `S27 committed; now showing S28`.
An ended ledger run reads `passed`, `failed` or `stopped`, as in Recent. The
line is dim (faint under `NO_COLOR`) and fades on the first snapshot 5 s or
more after the switch. Only a pane opened on the Overview follows; the
Questions tab's pane stays on its run. Paused and awaiting-approval runs stay
in the pane, because they wait for the owner. The pane also stays put while its answer field holds text or a confirmation is
open, and when nothing else is running it stays on the ended run with its
final state. A run of another project never takes the pane.

The actions follow the state (`ui.answer-pane-scope`):

- Paused with `pending: question`: free context lines before `Question:` stay
  in the raw question and appear above the options in the pane's scrollable
  reason area; the raw `Question:` and `Options:` block is not repeated
  there. Structured questions show a bold title,
  2 to 4 numbered options, the inline `(recommended)` marker and a
  `Why N: reason` line (`ui.structured-questions`, adj #168). A digit opens a confirmation with the
  exact, pasteable `ork answer RUN 'LABEL: summary'` command. Answer text is
  shell-quoted, including embedded apostrophes; the confirmation suffix stays
  outside the quotes. The hint reads `y sends; enter, n or esc cancel`.
  Only y sends it; Enter, n and esc cancel. The label and summary are the answer sent to the paused
  role and, when enabled, adj. `t` opens the free-text field; esc returns to
  the options, and a second esc closes the pane. Typing and paste are ignored
  until `t`. The options hint shows the actual range, such as
  `1-2 pick; t free text; esc close pane`. Free text shows
  `enter submit; esc back to options`. Questions from older records show the
  Answer field directly.
  The exact `ork answer RUN 'TEXT'` command follows the text live. A
  submit shows `sending answer` in the footer while the call runs, not the
  command form. The call returns only when the run rests, so the footer follows
  the snapshots meanwhile: once the run leaves paused it reads
  `answered; implementer resumed (round N)`, and after the call returns
  without a resumed role it reads `answered`. If the owner closed the pane
  first, the result shows `answered` with any adj notice, never the command
  form. Any adj notice follows. The
  status ends at the run's next state change and does not return when a late
  result arrives; an error replaces it. Once the run is not
  paused, its Running row and pane header drop the answered question and
  their wait text (S127). Enter submits
  without y/n; an empty or whitespace-only answer is refused. The field owns
  typing keys, including the global action letters. Backspace deletes the
  previous rune, ctrl-u clears the field, and left/right move the cursor.
  Terminal paste inserts at the cursor and strips CR and LF, keeping one
  answer line; paste never submits (`ui.text-field`).
  Ctrl+a toggles optional filing in adj, off whenever the pane opens. The footer
  shows `[ ] file answer in adj as KEY  ctrl+a toggles`; when on, `[x]` is
  highlighted. A dim, inert line names why filing is disabled: `adj filing off:
  no adj in this project` or `adj filing off: question has no decision key`.
  Filing requires the caller's `ADJ_DECIDED_BY`. When on with that value unset,
  the toggle also warns `ADJ_DECIDED_BY unset`. Submitting refuses the answer
  before launching an action, making an adj call or setting a run error.
  ork checks the exact key, asks if missing or closed, and decides the open
  record with the typed answer. Success shows `decided in adj as #N`.
  An existing decision resumes the role and shows `KEY already decided as #N;
  not changed`. Errors keep the text and leave the role paused; retry reuses
  an ask left open by a failed decide. Short panes keep the Answer field,
  status, enabled toggle, submit hint, off/disabled toggle and command preview
  in that priority order, dropping the lowest priority lines first.
  This toggle is available only in the TUI; `ork answer` does not file in adj.
- Paused with `pending: budget`: show the reason; the footer says
  `t retry: one more role round; esc close pane`. Retry grants exactly one
  role round, then the budget check applies again (`run.budget-resume`).
- Paused for another reason (not a question or budget): show the reason and
  `t` retries, with the same
  `ork retry RUN` y/n confirmation as the Overview.
- Stopped: show the full reason with no key actions. Stop is final; relaunch,
  or clear the row with `ork dismiss RUN`.

Awaiting-approval runs offer `a` approve and `x` stop (and the approval card
below, when the run has one); other active runs offer `x` stop. A successful answer clears the field and refreshes the pane and row.
A failed call permits another attempt. Tool-coded errors retain their code and
message. Plain `adj ask` and `adj decide` errors show the tool, exit code and
trimmed stderr, without the request argv or question context. The daemon wraps
these plain errors with code `internal` in the TUI status. Closing the
pane leaves a pending call running. No structured answer options are offered.
The Questions tab also opens this pane for engine questions.

**Approval card** (*proposed*, S118; the retrospective's R5). The pane of a run
that awaits approval and has a card lists what the owner needs to decide, in
the action area under the run tree, in this order:

- `Gate PASS (round N)  Tests: P passed, F failed` (the gate's test counts) and
  `Diff: 2 files, +48 -4, 1 new file` (one short line, from the last line of
  `git diff --stat`). The per-file diff stat, then the new files as `PATH  (new)`, scroll above the
  tree.
- The owner findings, as under Finding kinds below (and the later findings
  count). S111's structured questions show here as `Owner question, not
  blocking: TITLE Recommended: LABEL.`
- `Human checks (K of N open)` and one line per `[HUMAN CHECK]` item of the
  slice: `1 [ ] TEXT` open, `2 [x] TEXT` ticked, `3 [~] TEXT (later)` marked
  for later. With no items the line reads `Human checks: none`; if the daemon
  could not read them it reads `Human checks: not read (WHY)`.
- `Approve lands it, then removes its worktree and branch.` (S119): the
  approval is the owner's confirm of the landing and its cleanup (adj 162 and
  adj 152). ork then merges the base branch in if it moved, re-checks, merges
  into the base branch, pushes when the project's `push` setting is on, and
  removes the worktree with `git worktree remove` and the branch with `git
  branch -d`. A blocked landing returns to this card after one fix round, or
  stops in Needs you with `land blocked: ...` and the worktree kept. When
  the pane is short, lines drop in this order: the later findings, then each
  owner finding whole (the last first), then the status line and the key line
  (each its last wrapped line first), then this line, and the human checks last. When the project's `push` setting is on, it reads
  `Approve lands and pushes it, then removes its worktree and branch.` (it wraps
  to two lines at 80x24; the TUI reads the setting from the registry entry);
  with push off it reads `Approve lands it, then removes its worktree and
  branch.`
- The key line: `a approve; F fix round; x stop; 1-N tick/later; v view renders;
  f file slice; esc close pane`. `v view renders;` shows only when the card
  has renders.
- `Renders: N from the UI pass (round R)` (dim; `, K more not kept` when the
  card dropped files), under the diff line. When the UI pass ran and wrote
  none it reads `Renders: the UI pass (round R) wrote none`. Without a UI pass
  the card has no such line.

A digit marks the human check of that number and moves it on: ticked, then
later, then open again. The mark is saved in the run record at once (`run.check`).
`a` approves only when every check is ticked or marked later. Otherwise the
status line says `Approve needs every human check marked; open: 2, 3 (press the
number)` in the pane, naming the open checks, and `Approve needs every human
check marked; 2 open; enter opens the card` on the Overview row. The daemon
applies one mark at a time, so a digit pressed while an earlier mark is still
pending waits: the status line names the pending mark (`mark of check N is
pending`) and the owner presses again. A mark says what it does in words, while
it runs and when it is done: `marking check 2 done` then `check 2 marked done`,
`marking check 2 for later` then `check 2 marked for later`, and `reopening
check 2` then `check 2 reopened`. When ork approves, it records the marks as a slicer
note on the slice, which replaces the human-check adj records. A card needs the
owner to mark checks in the TUI; `ork approve` cannot.

`F` (shift-f) opens the fix note. It is a capital because `f` files later
findings on the same card, and `e` keeps its meaning in Waves. The footer shows `Fix note: TEXT` and `enter sends the
note to a fix round; esc back to card`. The field owns every key, including digits and
the action letters. Enter sends the note: an empty note is refused, and the
status line follows the call, which returns when the run rests again. Meanwhile
the run goes through its fix round, the checks and the gate, and its pane shows
the card again on PASS, with the marks it had. A failed call (the round limit,
a stopped run) keeps the note in the field. The status line tells the call in
words, not as a command, because there is no `ork` command for it: `fix note
sent; fix round running` while it runs, then `fix round finished`; a failure
reads `fix note: ERROR`.
`esc` goes back to the card, and a second `esc` closes the pane. The
`Human checks` label is a heading in every case (open, none, not read), and a
check that wraps hangs under its text, which starts at column 6. A run without a
card (a record from before S118) keeps the old key line `a approve; x stop`.
At 80x24 the card's footer keeps one line for the header that names the run
(`Run ID  ITEM  STATE`), so the card gives up lower-priority lines first (the
later and owner findings, then the key line) before that header goes; the
tree scrolls out of sight and the Overview rows above stay.

**Renders** (*proposed*, S121; adj #165). `v` on the card opens the viewer on
the first render the UI pass wrote. The order is each view's `VIEW-80x24.txt`,
`VIEW-120x40.txt` and `VIEW-ascii.txt`, the views by name, then any other file
by name. The card holds only each render's name and size; the pane asks the
daemon for the text (`run.render`) when the viewer opens and again for each
render the owner moves to. Until the answer arrives the body reads `Loading
NAME ...`; if the call fails it reads `Could not read NAME: WHY` (for example
`not_found: no render NAME for run ID`), and `v` and `esc` still work.

While a render shows, the Overview rows above the pane are gone: the pane has
the whole body, from the rule under the view bar to the rule above the status
line. The pane's first line still names the run (`Run ID  ITEM  STATE`); under
it a dim rule (the theme's horizontal rule, `-` with the ASCII option) sets the
render apart from that line and takes one row of the render's room; the render
fills the rows below the rule, and the footer shows `Render K of N: NAME` and
`v next; s sidebar; pgup/pgdown scroll; esc back to card`. The card's summary,
checks and other keys give way. `v` moves to the next render; on the last one
the key line reads `v/esc back to card; s sidebar; pgup/pgdown scroll` and `v`
returns to the card, where the Overview rows return. If the card
changes while a render shows (a fix round started from another TUI), the pane
shows the card again.

`s` hides or shows the sidebar: a render is not wrapped, so a row wider than
the pane is cut with `...`, and the sidebar takes 20 columns below 100
columns and 28 from 100 columns up. A render taller than the pane scrolls, one line
for each press of `pgup` or `pgdown`, with `+N lines above` and `+N more
lines` markers. A 40-row render does not fit at 80x24 or at 120x40, since the
pane's name line and footer take rows too, so it scrolls at both. While a render shows, no card
key acts except those: the owner cannot mark a check or approve what the footer
does not show. `esc` returns to the card, and a second `esc` closes the pane.
ork draws the text it was given, after dropping control characters again; it
does not render the view itself.

**Finding kinds** (adj #163, `review.finding-kinds`). A reviewer, UI pass or
gate report tags each finding `fix`, `owner` or `later`. In every pane that is
not a question, the footer lists what the report left for the owner. Later
findings read `N later findings; next to file: "TITLE"`. When one can be
filed, the key line shows `f file slice` just before `esc close pane`, on a
stopped run too, and after a daemon restart for a stopped or committed run.
`f` shows the command first:
`slicer add 'TITLE' --discovered-from ITEM? [y/N] (Enter is No)`. `y` makes
ork run it in the project, then give the new slice a Why section from the
finding; the status line then says `filed as ID`, and the next press offers the
next finding. Filed findings read `Filed as slices: ID` and are not offered
again. An *owner* finding that does not block reads `Owner question, not
blocking: TITLE Recommended: LABEL.`; it only informs, and the slice goes on.
An owner finding marked a blocker pauses the slice as an ordinary structured
question (the question pane above). A fix finding goes to the fix round and
shows nowhere in the pane. `f` and `F fix round` in the key help sit on the line with
approve, stop and retry, in the pane's order. The help has no room for a line
of its own for `F` or for the digits at the standard help height.

A failed call is an error row. A project has one error row per error code.
The parts that failed with that code come first, then the code, then the
tool's message as printed, for example `items, runs: locked: Ledger is busy.`
Different messages under one code are joined with ` / `. An items or inbox
failure sits under Needs you. A runs failure alone sits under Running. Error
rows never count toward the badge (`ui.setup-errors`).

## Questions (all projects)

Questions is the second view, after Overview (`ui.answer-ui`, `ui.view-list`).
It lists engine runs paused with `pending: question` and open adj records
from every project. Enter on an engine question opens the run detail pane
and its Answer field and optional adj filing toggle. Enter on an adj question
opens read-only record detail,
including options and the recommendation. `esc` returns to the list.
An adj answer path is outside this view's current scope.

The proposed rows follow registry order. Within a project, engine questions
come first, then adj records in inbox order. Every row names its project.
Names are padded to the widest project name, capped at 20 columns.
Removed projects and unavailable runs are skipped. An unloaded project shows
"~ pending" with "waiting for the first reply". A failed adj inbox call shows its
error code and message. Pending and error rows are display-only and never
take the cursor. An empty list says "No open questions" in dim text.
The "All projects" heading states the scope. A kind column marks engine
rows "? question", adj rows "? decision", pending rows "~ pending", and
errors "! error". Nonempty lists end with `[enter] open`.

The proposed row keys (`down`/`j` and `up`/`k`) move the cursor.
In record detail, they scroll; `pgup` and `pgdown` scroll a page.
The list always shows all projects. Sidebar selection, project filtering and
`[`/`]` project keys do not change it. There is no scope toggle.
`a`, `x`, `t` and `c` have no action on the list or read-only record detail.
The engine pane behaves as on Overview. It keeps its answer typing and
submit keys. If the run changes state while the pane is open, its approve,
stop and retry keys follow the run state and the footer hints.

## Waves (one project)

The Waves composer is available (S82). Select one project. `tab` and
`shift+tab` reach it between Questions and Roadmap. It loads `wave.suggest`
when opened or the project changes.
The first three suggested waves appear in slicer's order. A dim `N more
waves` line (`1 more wave` for one) offers `[a] show all`; hidden waves cannot be selected until
expanded. Unplaced items remain available. Wave headers are full-width
box-drawing rules with item counts, or hyphens in ASCII mode. Tick, item id, title and
Shares form aligned columns. The dim Shares column contains only the
predecessor id, for example `<A` means shares files with predecessor A;
shared paths stay out of the table. `?` explains the `<` marker.
Unplaced reasons use plain words: `no_files` becomes `no files listed`.
Reasons that do not fit beside the title appear on the following lines, dim
and indented to the title column.
Long titles shorten to keep the tick, id and Shares columns visible.
The cursor bar spans the row's full width, including its Lineup continuation
lines. The hint lines are pinned below the scrolling body, after a blank
line, so they stay visible when rows are ticked; the status line sits under
them. A dim cell stays dim across all its columns.

S60 provides the CLI: `ork wave suggest PROJECT` prints the ordered waves
from `slicer plan-parallel`, including reasons. `ork wave start PROJECT
ITEM...` takes the chosen item ids (`wave.start-input`), plans each item,
runs the spec check and asks its open questions (S116; see design.md, Wave
lifecycle), then asks for confirmation. `--yes` requires explicit `--well-specified
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
         Items                 Shares  Lineup (* edited)
-- Wave 2 --------------------------------------------------- 2 items
  [x] A  Alpha                         impl*: claude custom-model high
                                              +2 fallbacks
                                       rev:   claude opus medium
                                       gate:  claude opus medium
  [x] B  Beta                          impl:  claude sonnet high
                                       rev:   claude opus medium
                                       gate:  claude opus medium
-- Wave 4 ---------------------------------------------------- 1 item
  [ ] C  Gamma                 <A
-- Unplaced -------------------------------------------------- 1 item
  [ ] D  Delta (no files listed)
  ! A and B both touch shared.go
  ! A and B both touch second.go

  [enter] item   [space] tick item   [e] edit lineup   [l] launch wave
--------------------------------------------------------------------
  2 ticked, not launched; [l] launches
```

`enter` on a plain row opens a read-only item pane with its full title
and item detail from `item.show`. A shares reason appears as `Shares files
with ID`, followed by the single shared path on an indented line. Spaces
are part of that path; long paths wrap. Other reasons use the same plain words as the table and
remain visible in full.
`up`/`down` and `j`/`k` scroll one line. `pgup`/`pgdown` scroll one
visible page. `esc` returns to the same row. The pane offers no launch or
editing actions.

`space` ticks or unticks the selected item. Ticking shows both y/n inputs
directly under that item, with no defaults: well-specified and split. A dim
`launch inputs` line and both fields are indented to the title column. The
item and both fields stay in view. Only the active field shows a cursor, and
the hints change to `[tab] next field   [enter] plan   [esc] cancel`.
`tab` or `shift+tab` switches the active field; `enter` validates both and plans the item.
`esc` cancels that tick. The Lineup column appears only while at least
one item is ticked and fills from the plan (`ui.wave-composer`). It uses
short role labels: impl, rev, gate and arch, padded so the values share one
column. Each role shows its primary entry and a dim `+N fallbacks` line
under the values; `e` keeps the full chain available.
The column fits its longest line, capped at half the pane width. Long
primary entries wrap within the column.
`down`/`j` and `up`/`k` move between items. Unplaced items can also
be ticked.

Two or more ticked items trigger an overlap read. Each shared path gets a
warning; warnings permit launch (`wave.selection`). `l` launches the ticked
items with one y/n confirmation showing `ork wave start PROJECT ID...`.
Enter, n and esc cancel; y submits each item's answers and confirmed plan.
`l` first runs the spec check (S116, adj 164) for every ticked item and shows
`pending: spec.check`. The daemon asks the role only about specs with no check
yet or changed since it, so a ready item is asked again at no cost, and one
whose spec changed is not trusted. A ready item's row says `spec ready` after
its title; an item with open questions says `waits on N spec questions`. Items
with questions open one sitting that replaces the table: the heading `Spec
check: N questions for ID, ID`, then each question in turn as `[ID n/of]
Title` with its numbered options (`(recommended)` marks the recommended one),
a dim `Why N: reason` and the decision key when it has one, a blank line, and
a dim `K of N answered` line. `1`-`N` picks an option, `t` types a free answer
(`enter` submits it, `esc` goes back to the options), and `s` or `esc` skips
the rest of the sitting. When an item's last question is answered, ork
appends the answers to its Implement section, keeps them for the item's run,
and the item counts as ready; a question with a decision key is also filed in
adj when the project has adj and `ADJ_DECIDED_BY` is set (the status line says
why not otherwise). When the sitting ends, the confirmation below covers the
ready items only, and says which items it leaves out after the command, for
example `ork wave start P A? [y/N] (Enter is No)  B waits on spec questions`
(the part after the command is not run). A held item stays ticked with `waits
on N spec questions` on its row, the launch status says `ID waits on spec
questions`, and while a ticked item is held the hint reads `[l] answer spec
questions` instead of `[l] launch wave`. Pressing `l` again checks the ticked
items again and reopens the sitting. A check error shows its code and message
and leaves the ticks alone.

Until launch the composer says `N ticked, not launched; [l] launches`
(not shown in the lineup editor). After admission it says `launched S78 as
RUN; enter opens it` for one item, or `launched N items; enter on a running
row opens its run`. A launched row stays dim, shows `[>]` instead of the tick
box and `running RUN` after the title (on its own dim line when it does not
fit), and cannot be ticked again. Its hint reads `[enter] open run`, and
`enter` selects the run in Overview, including when the run first appears
in the next refresh.
Every ticked item must have a successful plan before launch. One failed
plan prevents launching the chosen set; the owner can untick it. The
wave.plan-failure decision remains open (adj 146).

`e` edits the selected ticked item after its plan is ready (S83).
The editor starts from that item's plan: implementer and reviewer, then
budget minutes and tokens. The gate follows the reviewer and has no field.
When the plan has a `ui` entry (the matrix's UI pass row), a line below the
reviewer lists its chain for information: the UI pass runs beside the reviewer
only on a slice that touches the UI (`roles.ui-pass`, adj #165), has no field,
and the cursor skips it.
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
    ui: claude opus medium (UI slices only; from the matrix)
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
row title style (bold), not the section heading's colour (S106).

S07 adds a stage bar, a pulsing running cell and an activity sparkline.
S99 adds live assistant output and usage (see below). Worktree diff stats,
test counts and a wave timeline remain future role detail. The activity series counts vendor stream
events, not tokens. There is no bell or budget bar in S07.

The actions are approve, stop, answer, retry on another vendor, and open
the worktree path. Relays started outside ork appear with a "started
elsewhere" marker and no actions (`dash.external-runs`).

## Roadmap (one project)

Goals head the screen. The queue follows `slicer list` order and shows each
item's id and short title (or title), with size and effort combined as `S1`
in a right-aligned column. A dim `·` marks ready items (`*` in ASCII mode). On the cursor row the
marker shares the row's uninterrupted reverse style. Missing size and
effort show as `--`. The heading carries the counts, for example
`Roadmap  33 ready of 41, all open`; mixed statuses replace `all open` with
`N open, M other`. The selected slice opens in a detail pane. Readiness
comes from status and dependencies only. An item is ready when `slicer deps` includes it, its status
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
"No daemon connection; action unavailable". Up/down and j/k move through
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
`right` switch the Open and Decided tabs; `[` and `]` switch projects. `A`
(shift-a) toggles between the selected project and all projects that use adj. The toggle
persists when switching projects. With "Needs you" selected, Decisions shows
all projects that use adj. A project without adj shows `no adj in this project`.
If no registered project uses adj, the all-projects view says `No project uses adj`.

All-projects rows put the project name first, padded to the widest adj project
name in scope, capped at 20 columns. Each record takes one line, with a clipped
title. The cursor highlights the whole line. Tradeoffs and recommendation text
are dimmed and indented under their labels. Rows follow registry order. Within
each project, Open keeps `adj inbox` order (oldest first) and Decided keeps
`adj list` order. `down`/`j` and `up`/`k` move the record cursor.
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
only when positive, and drops first when narrow. It is right-aligned on the
pipeline line only for round 1: from round 2 the bar's own `rN` says it and the
pane's pipeline line carries no round (S106). For live runs, `last heard
N ago` is dim through 10 minutes, amber (the warning color) past 10 minutes,
and red (the error color) past 30 minutes or for a stale run; the cue is `!` for
amber and `!!` for red, so monochrome keeps it. It sits on the bar's line, after
the bar and its round (S106). Ended, stopped, paused and committed runs have a dim pipeline,
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
(`r2`, `r3`), and last heard follows the round on the same line (S106); a pane
too narrow for both keeps last heard on a line of its own.
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


## Self-hosting (engine.self-host, adj 166)

*Proposed behavior and wording:* `daemon behind main: restart with ork daemon restart`.
While no run is active, the status line shows it when a self-hosted daemon's
revision is behind local `main`. It fires on any landing on main, not only
engine landings. It replaces the stale-build marker while shown; otherwise
the stale marker still shows for a differing build. At narrow widths the
behind notice takes priority over other status fields. Text `ork doctor`
prints `[WARN] daemon behind main: restart with ork daemon restart` on stdout.
`ork status` prints `ork: warning: daemon behind main: restart with ork daemon restart`
on stderr. ork never restarts on its own.

Waves marks an engine conflict after planning two selected slices whose Files
touch `internal/engine`, `internal/daemon` or `cmd`, naming both:

```text
! engine conflict: A and B touch internal/engine, internal/daemon or cmd; launch separately
```

Pressing `l` puts the same conflict text, without the leading `! ` marker, in
the action status. Launch is blocked until the selection holds at most one
such slice. The daemon checks Files again before starting any member. See
docs/design.md for detection.

### Live output and usage (S99)

The running engine role shows up to three dim rows of assistant text after
its task. The tail has its own ASCII-safe `    > ` gutter. Text wraps to the
remaining pane width, blank lines are removed, and the newest text is last.
When older text is cut by the three-row window or memory cap, only the first
row begins with `...`. The tail keeps at most 2 KB in daemon memory and
resets at each role attempt. Stopped, paused, stale and ended runs show no
tail. Relays started elsewhere have no tail or usage. Claude text streams as
deltas. Codex shows whole assistant messages when an item completes; it does
not expose text deltas in the measured fixtures. Tool calls and tool output
do not enter the tail.

Only the running node gets a separate `tokens N  $C` line. Until its first
usage event, it shows `tokens -  $-`. A reported zero is shown as zero.
Finished roles append recorded usage to their node line; that whole suffix
drops first when narrow. An interrupted role in a stopped run also uses the
node suffix: missing recorded usage and cost show `tokens -  $-`, with no
live tail. A failed gate's verdict stays directly under its node, before any
separate usage or task line. The pane scrolls inside its own lines, which sit under their row with the
Overview rows around them (S106).

Running roles show `$-` until their round ends. Vendors with no cost field,
including Codex, show `$-` after completion too; zero stored cost does not
mean a reported zero price. The Overview's `tokens N` is the run total:
completed roles plus the current role's live tokens. The cell is omitted
until engine usage is available, and drops before heard when a Running row
narrows. Claude usage advances at message start and message delta; Codex
usage arrives only at turn completion. These counters show reported usage
and cannot estimate tokens between usage events.

Before text enters the tail, ork replaces run-environment values of at
least eight characters with `***` when their variable name contains KEY,
TOKEN, SECRET, PASSWORD or CREDENTIAL (case insensitive). A possible secret
prefix waits for the next text event so split values are masked too. Final
flush masks any held prefix instead of exposing it. This rule does not
recognize secrets absent from those environment variables.
