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
(`registry.no-seed`, adj 157). The body says "No projects yet." and the hint
line shows `+ add project; q quit`. `+` opens a project path field. Enter with
an empty field offers the current
directory only when it holds a `.slicer` directory or `.adjudicate.json`
file. Otherwise the status names the directory: "DIR holds neither .slicer
nor .adjudicate.json; type a path". The path guidance asks for an absolute path.
Esc cancels path entry and returns to "No projects yet." with
`+ add project; q quit` on the hint line. Typed paths are trimmed. A leading
`~` or `~/` expands to the home directory before confirmation and submission.
Other paths are sent as typed.

The proposed confirmation next to the field shows
`ork projects add 'PATH'? [y/N] (Enter is No)`, with PATH shell quoted (`ui.confirm`, adj 98). Its hint is
`y add project; n cancel`, or just `n cancel` without a daemon.
Enter, n, N and Esc cancel. y or Y adds once through the daemon. While the
add is pending, another y does nothing. The path field shows no cursor
during confirmation or the pending add. Errors show verbatim next to the
field, with the wire error code when present. Cancel and pending messages
also appear next to the field. The field keeps its text.
Without a daemon, the status says "No daemon connection; action unavailable".
On success the Overview appears with "Needs you" selected and status
`Added NAME PATH`. The new project is fetched at once and polled.

The proposed TUI checks the daemon project list on each schedule tick, with
one list request in flight at a time. Projects added elsewhere are fetched
and polled too. The first registered project closes Welcome and its path
prompt, and shows status `Found NAME PATH`. Leaving Welcome also closes
help. A failed list request keeps the current projects and retries on the
next tick. With no projects, the status line shows `0 projects` without the
refresh field; pending can still show.

The proposed Welcome keys keep `?` for help and `q` or ctrl+c to quit
outside text entry; ctrl+c also quits during text entry. View, project,
filter, sidebar and run keys do nothing on Welcome.

The proposed `+` key opens the same path field from every view, including
help and detail panes. Esc returns to the previous view and project. Adding
with projects registered keeps that view and selection. Existing text fields
keep `+` and `-` as text. The hint and help list `+ add project`.

The proposed `-` key removes the active registered project from every view.
Select a project in the sidebar first; aggregate entries and removed groups
cannot be removed. It asks `ork projects rm 'NAME'? [y/N] (Enter is No)`.
A dim second line says: "Files on disk are untouched; past runs stay in a
read-only removed group."
Enter, n, N and Esc cancel. y or Y calls the daemon once; further keys do
nothing while removal is pending. Without a daemon the action is unavailable.
The daemon refuses projects with unfinished runs, including running and paused
runs, and its error code and message appear verbatim. A refusal keeps the
project registered. Success selects the first remaining registered project;
removing the last project returns to Welcome. Historical runs remain in a
read-only removed group. The hint lists `- remove project` for an active
registered project, and help lists `- remove active project`. On narrow
screens, the remove hint drops before the add hint.

## Layout

```
ork dev              | Overview | Questions 2 | Waves | Roadmap | Decisions
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
                     |  enter go to project; tab views; q quit; ? help
                     | 4 projects | refreshed 16:42:00
```

The sketch uses ASCII rules. The view bar, sidebar edge and horizontal rules
use box-drawing lines when `cfg.ASCII` is false, including the rule above the
detail. The CLI already resolves that setting with `brand.ASCIILocale`.

**Hint line (*proposed*, S149).** One line sits above the status line and
shows the keys that work now, for the row under the cursor, the open pane and
the view. The status line keeps its place and its fields (refreshed, pending).
The line costs the body one row at every size: the body is `height - 5` rows
with no detail, where it was `height - 4`. The row's own keys come first, then
`tab views; q quit; ? help`; in a pane the pane's keys come first, then its
scroll keys, then `esc close pane` (or `esc back to card`) at the right end.
The way out, `esc` in a pane and `? help` elsewhere, never drops, and on the
main views and their decision and Roadmap item panes `q quit` never drops
either (S152). When the line
is too narrow, whole hints drop, lowest priority first: `f file slice`,
`v renders`, `F fix round`, the check digits, `t retry`, `x stop`, then
`ctrl+d/u`, `tab views`, `pgup/pgdown`, `a approve` and `c clean up`,
and last the row's main key. When open human checks refuse approval, the check
digits and `F fix round` rank above stop and all scroll keys; the way out stays
pinned. After dropping, the line retries each omitted hint in priority order
(lowest drop value first), at its original position, and keeps it if it fits.
A shorter hint can return even when a higher-priority hint is too wide.
A hint is never cut in the middle. Width and drop decisions use visible
columns. Keys use the default colour; descriptions use dim (SGR 2 only), in both
colour and mono. The hint line is the one source of key lists: pane and Waves
body footers do not repeat them. Input fields, confirm prompts and errors stay.
At 80x24 the Overview row may scroll out of sight.

The list is built from the key bindings and from the checks that refuse a key,
not written by hand: a key shows only when pressing it would act. So `a
approve` shows on an awaiting-approval run whose human checks are all marked and
not before, `x stop` not on a stopped run, `t retry` only on a paused run that
does not wait for an answer, `c clean up` only on a merged Ready row, and the
run keys not at all while there is no daemon connection. `f file slice` shows
only with a later finding to file, a daemon that can file, and no filing in
flight. `o answer` shows in a run pane with an unanswered structured owner
finding and a daemon connection. Both finding keys stay ahead of scroll hints
when space is short. In a finding question, Esc reads `esc back to run`;
free text first returns to the options. The option digits and `enter submit`
of a question pane show only while
the answer would go out (a daemon, and no call in flight for the run). The check
digits and `F fix round` show only for a daemon that can mark checks and send
fixes; `space tick item` not while a plan is pending for another item, and `l`
on the Roadmap and Waves only when it would act. Examples: a decision row shows
`enter go to project`; a run row shows `enter open`; an open run pane shows its
own keys, then `pgup/pgdown line; ctrl+d/u half page`;
Waves item and decision panes use `pgup/pgdown page; j/k line; ctrl+d/u half
page` for their page, line and half page movements, and the Roadmap item pane
shows `ctrl+d/u half page` (S152). The Waves item hint ends with `esc close pane`.
A pane of a relay started elsewhere shows `started elsewhere, read-only; esc close pane`.
The note has no key and is drawn in the Dim style of the theme (S152). Waves also lists
`e edit lineup` only when the selected ticked item has a plan and editing acts.
The Waves hint reads `l launch` (not `l launch wave`) so that `e edit lineup`
fits beside `q quit; ? help` at 100x30 (S152, owner decision 2026-10-29).
Welcome shows `+ add project; q quit`; its path field shows
`enter review path; esc cancel`. The lineup hint offers `left/right cycle`
only when the focused role has more than one entry. Wave inputs show
`tab next field; enter plan; esc cancel`. Launch lists `enter select slice`
when a ready slice exists, then `enter next field` for well-specified and
`enter plan` for split; `esc cancel` stays visible. The lineup editor and spec
question sitting also show their current keys in this line. A confirmation names what `y` does and nothing else,
since the prompt above it carries the rest: `y run; n cancel`, `y start; n
cancel`, `y approve; n cancel`. The help screen shows `? or esc close help; q
quit`.

The header shows the version next to ork (ui.version-mark, adj #130).
Development builds show the first seven revision characters, with `+` for a
modified build, or `dev` without a revision. A narrow header clips the version
first. The header does not carry a `?help` cue: the hint line names `? help`
(S152). With the sidebar hidden, the version appears at the
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
row in project order: the project, then the item, the stage bar (with its round
word, as everywhere), last heard beside the bar (with the `!`
and `!!` cues of the run pane; S106 moved it ahead of the pipeline), the
pipeline with the running role in brackets and the ten-bucket activity
sparkline. A run that waits on the owner is marked by one
glyph in the mark slot before its project name, as a kind symbol is on
Needs you: `?` for a paused run that asks the owner, `x` for a stopped one. The
glyph is amber and bold (bold alone under `NO_COLOR`) and a space follows it,
so it never sits against the name. The space comes out of the row indent, so no
row moves or widens. A run with an error shows a red `!` in the same slot. A stopped run
names the role it stopped on, in brackets where a running role would stand
(its last role has ended); its detail line brackets the same role. The heading
counts the runs and adds `? owner questions N` and `x stopped N`, both amber
and bold like the glyphs, and the detail line of the selected row keeps the
words. With no run row at all the view says `nothing running`; a stale relay is
a row, so it is listed without that line. Only live runs are counted, in the heading and
the sidebar badge: a relay started elsewhere that has gone stale is listed, dim,
but not counted, while an engine run follows its engine state whatever its
linked ledger reads. The Overview's `Running N` counts the
live, non-stale runs its own Running section lists; a run that waits on the
owner is under Needs you there, so that number leaves it out. A run that waits
on the owner
stays on this view until it is answered or dismissed; a paused run with no
question, a run awaiting approval and a removed run stay on Needs you only.
The columns line up: the item, the bar and the pipeline are padded to the widest
row. The cells shown are chosen once for the whole view, from the widest row,
so every row shows the same cells (the selected row alone may be clipped). As
the pane narrows the cells go in this order: activity first, then the pipeline
(down to the bracketed running role), then last heard. A row never shows the
activity without its pipeline, so at 100x30 the activity is dropped. The item, the stage bar
and the running role stay, so the view is complete at 80x24. The row cursor,
`enter` (which opens that run's pane, as on the Overview), the action keys and
the pane work as on the Overview; the pane follows the newest running run
(S109). The selected row's whole text, with the run ID, state and elapsed time,
is on the detail line, which wraps between cells and never inside one. A run
that ends leaves Running and shows under Recent on the next snapshot, with no
keypress.

**Several runs open (S164).** The selected run shows its detail below the rows,
on the detail line. `space` opens the run under the cursor in place: its detail
(the run ID, state and elapsed time, the stage bar and last heard, the pipeline
and the activity, wrapped between cells) sits indented under its row, drawn
faint (dim, and SGR 2 under `NO_COLOR`, as last heard is), and the run stays
open when the cursor moves away. `space` on an open run closes it. `E` opens
every run, or closes every run when all are open; it needs no run under the
cursor and works from any row of the Running entry, a Recent or an error row
included. Only `space` ignores a row that is not a live run. Neither key works
under the help overlay or outside the Running entry. The selected run shows its
detail once: under its row while it is open, on the detail line otherwise. The
open runs are kept by project and run ID, in the model only: they survive a
refresh (on any entry) and a switch of view or entry, and a run that leaves the
list is forgotten, so it is closed if it comes back. While a run pane is open
the rows are drawn closed; they open again when the pane closes. The pane
scrolls and keeps the cursor row whole at every size, 80x24 included: the
row and all its detail lines are on the screen. `pgdown` and `pgup` count rows,
not lines, as before. The hint line offers `space expand` (`space collapse`
on an open run) on a live run's row, and `E expand all` (`E collapse all` when
all are open) on every row while any run can be opened; the help names both.
Owner decision 2026-10-28: the two hints may push `+ add project` (at 100
columns) and `tab views` (at 80 columns) off the Running entry's hint line;
they keep their place elsewhere and in the help.

While the Running entry is selected the other tabs show every project together
(S140), each row led by its project name as on Needs you. Questions and
Decisions list all projects (Decisions all that use adj); Roadmap lists every
project's items under `All projects`, with one cursor over them; Waves asks the
daemon for each project's suggestion once and lists one block per project, in
project order: the project's name, then its own waves and Unplaced items, with
no tick boxes. A project whose suggestion fails keeps its block, with a `!`
line, so every failing project is named. The combined Roadmap and Waves
only list: opening an item, ticking, launching and editing a lineup need one
project selected in the sidebar, and the hint line does not offer `enter`. With "Needs you" selected the Roadmap and Waves still ask to
select a project.

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
The first nine project rows show their number, dimmed, in the margin where the
selection arrow stands. The selected row shows its number, then the arrow, as
`1>ork` (S151). "Needs you" shows a dimmed `0` there. Selected, it shows
`0>Needs you`. The Running entry has no number, so its selected row shows the
arrow alone.
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
do nothing but set the status line to `[ and ] switch projects` (S144; no
layout cost). The help names `]` and `[` as next and previous "project or
entry", since they also land on Running and Needs you.
`1`-`9` select the first nine projects in the order the sidebar draws them,
and the sidebar shows those numbers dimmed. `0` selects "Needs you"; the
Running entry has no number. A number with no project does nothing. `tab` and
`shift+tab` switch views in the order of the view bar (`ui.views`,
`ui.view-list`). All five views are available: Overview, Questions, Waves,
Roadmap and Decisions. `/` filters project names without regard to
case; `enter`
keeps the filter and leaves text entry, and `esc` clears it. `r` refreshes the
selected project, or all projects on "Needs you". `?` shows keys and `q` quits.

The Overview has a row cursor (`ui.overview-cursor`). `down`/`j` and `up`/`k`
move it to the next and previous row. It goes through
"Needs you", Running and Ready, and it stops at the first and last row.
`g`/`home` and `G`/`end` move the cursor to the first and last row.
`pgdown` and `pgup` page like `less`: `pgdown` scrolls the window so the first
row not yet seen is its top row and puts the cursor on it; `pgup` is the mirror,
and puts the cursor on the last row above the window, which then ends the
window. Section headings, blank lines and multi-line rows are not counted as
rows. The last page ends flush with the body instead of leaving blank lines.
The window stays while `j`/`k` move the cursor inside it, and follows the cursor when it leaves. When
every row is already on screen, `pgdown` goes to the last row. On the last row
the lines after it come into view while they fit, so the empty Recent and Ready
to land sections can be seen (S151). `}` and `{` move it to
the first row of the next group and of the current or previous group. The
groups are the Needs you groups (Decisions, Held, Stopped, Paused, Approve, Findings and
Errors), then Running, Recent and Ready to land. On the Roadmap a group is a run
of items with the same pass, with the cursor on its first item under the
pass heading (S162), and on Waves a wave, then Unplaced (S151, *proposed*). The Roadmap, Questions, Decisions and Waves cursors take the first,
last and page keys; the Overview, Roadmap and Waves have the group keys. With
the help open, no row key moves the cursor, which is hidden under it. With a
pane open, no row key moves the cursor;
`pgdown` and `pgup` scroll a run, decision or Waves item pane (the Roadmap item
pane does not take them, and the Roadmap help says only `pgdown/pgup page rows`),
and in a run, decision, Roadmap item or Waves item pane
`ctrl+d` and `ctrl+u` scroll it by half the pane's scrolling part (S149, S152,
*proposed*). `ctrl+u` in an answer or fix-note field still clears the field, so
there the hint shows only `ctrl+d`. The help says so on the scroll keys' line:
`ctrl+d/u scroll a pane half a page; ctrl+u clears a field` (S152). Keys that type into a
field keep their text: `g`, `G`, `0`, `{` and `}` typed into the filter or an
answer go into it, and digits still pick options in an owner question and mark
checks on the approval card. `n` and
`p` do not move rows; `n` is No in a confirmation. The row
under the cursor is drawn reversed across the pane width. A rule separates
the rows from its detail. Decisions show status, a nonzero option count and
the recommended label when present, then the wrapped title. Metadata uses
the theme's vertical rule as its separator. An empty detail block adds no
extra rule or band. Detail height reserves room for the hint and status lines. Other rows show
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
Decisions, Held, Stopped, Paused, Approve, Findings and Errors, omitting empty
groups
(`ui.overview-layout`, adj 149). Findings contains unfiled later and unanswered
owner findings from runs that are not landed, dismissed or superseded.
Findings rows open the run pane;
they are not additional pending owner questions. Errors includes failed calls,
removed runs and projects awaiting their first reply. Cursor order follows
these groups, keeping the existing order within each group. Each row retains its kind
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
2. **Running:** every running slice across projects, two lines each
   (*proposed*, S145; the owner's decision of 2026-10-26). Line 1 is the slice
   id, its short title (the title when it has no short one), the role working
   now with its model, the stage bar with its round word, and last heard. Line 2
   is that role's latest output line, dim and set in under the id; it is
   omitted when the role has said nothing. Where the title does not fit on line
   1, every run in the section moves it down together: line 1 is then status
   only (the model comes back if it fits) and line 2 reads `Short title  latest
   line`, or the title alone when the role has said nothing, so the run has a
   line 2 even when its ledger label is bare. The title is in the default
   foreground and two spaces separate it from the output; only the output is
   dim (or amber, for a denial), so a denial does not colour the title. When
   the output would not fit beside the whole title, or would get fewer than 20
   columns, the title is cut to the room that leaves the output 20 columns,
   and never under 12 columns (*proposed*, S150); a title that fits beside its
   output stays whole. The selected run is drawn reversed
   on both lines (the second is not dimmed and not bold), which reads in
   monochrome too; a denial on the selected run keeps its warning colour as the
   reversed colour, black on amber in any terminal theme (`denied:` carries the
   cue in monochrome). The run id and `started elsewhere`
   are not on the row: the pane names the run (`Run ID`), and the hint line
   says a relay started elsewhere is read-only. The selected row's detail line
   keeps the run id, the engine state, the steps and the elapsed time in full. A project
   column leads on the all-projects view.
   - **Round word.** The bar's round counts review rounds, not implementer
     steps: `fix N` once N fix rounds have begun, and `gate` while the gate
     runs. The first round has no word. A fix round is an implementer round
     after the first review; a run of implementer steps in a row is one round
     (a resumed step does not count again), and the finish step does not count.
     The finish step is the implementer step after a passed gate. The last
     round has no review after it yet, so what came before it decides: after a
     review it is a fix round; after a gate it is a fix round when the gate
     step failed or its task says fix and not finish. For an engine run the
     engine's state decides after a gate, because the engine ends a failing
     gate step `done` and its task need not say fix: `implement`, `checks` or
     `review` is a fix round, and `finish`, `commit` or later is the finish
     step. Limit: a relay started elsewhere that records a failing gate as
     `done` with a task that does not say fix reads one fix round low until the
     next review begins. A UI pass and the orchestrator are not part of the
     count. The UI pass has no stage of its own: it counts in the reviewer
     stage of the bar and the pipeline, which reads running while the reviewer
     or the UI pass runs, failed if either failed, and done otherwise (S136). A
     UI pass step that comes after the gate is not part of the review. The pane's pipeline line keeps `round N` (the fix rounds plus one)
     only for a run whose bar has no round word. The Running row leaves the word
     `gate` off the bar when the gate is the working role (the role cell says
     it); the pane bar and the Running entry keep it.
   - **Line 2 for an engine run** is the newest nonblank line of the S99 output
     tail. A denied call shows as `denied: Bash: ...` on line 2 instead, in the
     warning colour. **For a relay started elsewhere** ork has no output stream;
     the ledger gives one line per step, its `--task` line (`slicer relay-run
     step --task`). Line 2 is the task of the running step (the last step with
     phase `start`, so a subagent shows ahead of its orchestrator). The relay
     contract asks for a bare label there, `ID role` such as `s145 implementer`,
     which line 1 already says. A leading `ID role` is cut from the task, with
     the `:`, `-` and spaces after it (case, spacing and `-`, `_`, `,` or `/`
     between the words do not matter; the ids of a run on several slices are joined), so
     `s145 implementer fix round 2` shows `fix round 2`. A task that is only that
     label (or the id or the role alone) says nothing, and gives no line 2 unless
     the title has moved there.
   - **The role** is the engine's own running role for an engine run (its state
     word, such as `checks`, between roles, drawn dim) and the running step for
     a relay started elsewhere. A stale run shows `stale` before its role,
     dimmed.
   - **One fit for the section.** The cells of line 1 are chosen once for all
     Running rows, from the widest content of each column: the tightest row
     decides which cells every row keeps, and the title and the role with its
     model are padded to shared widths, so the stage bars, the round words and
     last heard stand in one column. Narrow panes drop cells from the right of
     the id in this order: `tokens N` (an engine run's, when usage is known), the
     title shortens (to 12 columns), the model, then the title moves to line 2
     (the model returns if it fits), then last heard unless a row's carries a `!`
     or `!!` cue, the role, and the bar, so a cue outlasts the bar (S106). The id
     stays. Line 2 does not depend on the width: it is cut to the pane with
     `...`. The same order holds at 80x24, where the sidebar and the project
     column leave little for the title, so the titles usually sit on line 2
     there.
   `! heard` is amber after 10 minutes with no event; `!! heard` is red after
   30 minutes, the stale cutoff, and marks stale runs. The unselected cell uses
   the warning color (amber) or the error color (red); in monochrome the words
   `!` and `!!` carry the cue, and red is bold. Stale runs stay under Running,
   dimmed; elapsed time freezes at their last event in the detail line. A row
   with an `error` keeps the older one-line form with its `! error` label (the
   run id, state, wait, denials and error), cut to the pane with the run id
   whole; on a terminal narrower than 100 columns that form drops its run id
   (S106). Rows have a label column only while some Running row has a label
   (`! error` or `~ pending`); the other rows then pad to it so they line up
   (S33). The Running entry is unchanged but for the round word: it always
   keeps the bar and drops heard before it.
   For relays started elsewhere, the *proposed* step details (also indented
   under each relay in `ork status`) are `running: ROLE VENDOR/MODEL: TASK` for
   the last step in ledger order with phase `start` (a working subagent wins
   over its orchestrator; `ork status` cuts the leading `ID role` label from
   the running task, as line 2 does, and `--json` keeps the task as recorded),
   then `failed: ROLE: TASK` for the last step with
   phase `fail`. They are on the selected row's detail line in full and no
   longer on the row, which shows the role, the model and the task line instead.
3. **Recent (proposed):** ended ledger runs within the last hour, newest
   first. Rows show `✓ pass` for pass, `✗ fail` for fail, and `~ stopped` for
   any other outcome (`✓ landed` for a pass that ork landed, `✓ landed, cleanup
   failed` when its cleanup failed; S148), then items, wall time,
   `ended N ago`, and counts such as `impl 1  review 1  gate -`. The first
   duration is wall time from the ledger's `begun_at` to `updated_at`, including
   waits. The parenthesised figure is active time, for example
   `13h27m (active 1h40m)`: the union of `begun_at` to `updated_at` intervals
   of done or failed implementer, reviewer, gate and architect steps.
   Overlaps count once; orchestrator and still-running steps are excluded.
   Wall time appears alone when no completed worker step has usable times.
   Review and gate show `-` when the ledger records no step of that role,
   and the number of recorded steps otherwise, including nested steps and
   repeated rounds. This also applies to relays started elsewhere; a dash
   does not say whether the role ran outside the ledger. Empty state: `none`.
   Rows follow Running in cursor order. Enter opens a read-only ledger pane.
4. **Ready to land:** committed engine runs that ork did not land, plus a landed run
   whose cleanup failed (see below), across projects, or for the
   selected project. Each row shows the slice and branch, with
   `slicer land 'ID' --branch 'B' --note 'Landed B'` on its own indented line to
   copy. Ready rows follow Needs you, Running and Recent in the cursor order and
   scroll into view with their command lines. The selected row shows full
   commands wrapped in the detail area; other rows keep clipped commands.
   Enter opens the run pane, including its findings and filing action.
   On the Ready row itself, run action keys explain that only cleanup is
   available on a merged row.
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
   The confirm shows only the steps that will run (S153): when the run's
   worktree path no longer exists, the daemon sends the proposed `worktree_gone`
   flag on the row, the confirm and the detail leave out `git worktree remove`,
   and the question reads `Remove branch?` instead of `Remove worktree and
   branch?`. The status line names the item, not the commands or the run id: `cleaning up
   S119: pending` (also on a second `c` while it runs), then `cleaned up S119`,
   or `cleanup S119: CODE: REASON` when the daemon refuses (the code is said
   once). A status that shows a daemon error says its code once too.
   Cleanup never forces removal, merges or lands a branch. Git errors are
   shown verbatim. After S119, a run that ork landed itself leaves this list, and
   its ledger run reads `✓ landed` in Recent instead of `✓ pass`. A run that ork
   landed but could not clean up stays here. Its row reads `S11  s11  landed
   cleanup failed`; the line under it is the reason (the failed git command and
   its error), clipped to one dim line and not one of the copy lines, and there
   is no landing command. Its detail on the selected row holds the reason in full
   (`landed; cleanup failed at git ...: ERROR`), wrapped to the pane. When the
   run's merge state is known to be merged, a second part reads `c finishes the
   cleanup: git -C P worktree remove W; git -C P branch -d B`, and `c` runs it
   (and clears the reason); the `worktree remove` step is left out when the
   worktree is gone (S153). With the merge state unknown the part is left out,
   because `c` refuses a run that is not known to be merged.
   After S95 a successful cleanup marks the run cleaned, and a cleaned run
   leaves this list, with its landing and render lines, whether ork landed it or
   `slicer land` did. A second `c` on it is refused ("already cleaned up").
   A run from before S95 whose branch and worktree are gone but never marked
   cleaned leaves the list by itself: when a merge pass finds no worktree and no
   branch and the run's recorded commit is in HEAD, the daemon marks it cleaned
   (S153). A worktree that is still there keeps the run as it was.
   When the item has left slicer's default list (`slicer land` ran outside ork,
   or the item was retired; the list hides done and retired items) the row reads
   `S09  s09  not listed` with no `slicer land` or `slicer render` line, and `c`
   still removes the branch and worktree. A
   snapshot whose slicer list was not read, or failed, keeps the lines.
   If `git worktree remove` succeeded and `git branch -d` failed, the row stays;
   the next `c` skips the worktree step when the worktree path is gone and
   runs `git branch -d`.

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
per run (`ui.run-join-key`). Only unmatched ledger runs are started elsewhere;
the pane says so (S145 removed the label from the row).
A ledger run linked to a committed or superseded engine run is not shown.
This is by design. Ready-to-land rows do not show the run id.

Engine runs that are awaiting approval, paused, or stopped and neither
superseded nor dismissed appear under Needs you. Their kinds are
`+ approve`, `= paused`, and `x stopped`. A paused owner question uses
`? question` in amber instead. Overview and the project page also show an
amber `? owner questions N` alert beside the Running count while questions
are waiting. In monochrome, question kinds and the alert are bold. Each kind is a symbol and a word,
so monochrome still reads it.
An engine-linked row and its pane use the engine state even when the ledger
run is stale. Awaiting approval reads `awaiting approval`; the row stays
undimmed and Enter opens its engine pane with the linked ledger steps.
On Approve and Paused rows, the wait age goes immediately after the state
word, before the reason and other details: `paused  19h00m  Which store?`.
Narrow rows clip the tail after the age. Wait text that only says the state
again is left out of the row and the pane: an awaiting-approval run reads
`awaiting approval  19h00m`, not `awaiting approval  19h00m  owner approval`
(S154). Any other wait text stays.
An engine run that waits on the owner (awaiting approval or paused) counts its
steps without the word `running`, because none runs: `2 of 6 steps`, where a
working or stopped run reads `2 of 6 steps running`. A relay started elsewhere
keeps slicer's own count, `2 of 6 steps running`, even with a step waiting
(S154).
Awaiting-approval and paused ages count from the persisted `wait_started_at`
using the current clock, including while the ledger is stale. Human-check
marks do not reset the age. Older records fall back to `updated_at`, then the
run's start when both timestamps are absent (see `docs/state.md`).
Every elapsed duration, including Running ages, uses days and hours from
24 hours up: `1d00h`, `3d02h`. Below 24 hours it keeps seconds, minutes, or
hours and minutes, such as `23h59m`.
Other active engine states appear under Running. Committed runs do not appear
in either section. A stopped run disappears once a newer engine record exists
for the same slice, ordered by `created_at`; ledger runs do not clear it
(`ui.stopped-newer`). The owner can also clear a stopped run with
`ork dismiss RUN` (`ui.dismiss-run`, adj #117). The snapshot keeps the run
marked `dismissed: true`, hidden from lists, Needs you and `ork status` text.
Its findings are excluded from counts. `ork status --json` includes the
dismissed run. Dismissal keeps its run record and files.
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
shows `landing ITEM: pending` (S148), then what happened: `landed ITEM` after a
landing, or `landing ITEM blocked; fix round done, awaiting approval` when the
landing was blocked: the reply comes after the fix round has ended and the run
waits for approval again. Any other result (a pause, a stop) reads `ork approve
RUN: done`, and a failure keeps the command with its error. The pane's approval
card says the same. The TUI reads the outcome from the run record in the
`run.approve` reply (`landed_at`, `land_fixes`, `state`). Calls run asynchronously;
every completed call refreshes the project's snapshot, and a failed call shows
its code and message verbatim in the detail area, except an engine refusal
(code `refused`, *proposed*, S141), which shows its text alone after the verb
(`approve: 2 human checks still open; ...`). Actions on other runs remain
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
most `height - 11` lines (`height - 8` under 24 lines), so its row and the
rows around it stay on screen. The hint line takes the row the pane gave up (S149). It shows the full `wait` reason before
the run tree, without the daemon's closing `; next: ork ...` command: the
hint line names the key, and on a paused run `t retry` drops after the scroll
keys, so it stays at 80x24 (S134). A bare blank row sets the reason off from the
progress line when the pane has room for the reason, the blank and one
progress line (S134). `pgup` and `pgdown` scroll the reason and tree while the action
area stays visible. `esc` closes the pane (`ui.answer-ui`,
`ui.answer-pane-keys`, `ui.run-tree`). Ledger runs started elsewhere open a
read-only tree pane with no action keys or answer input. An external ledger
run waiting on the owner with an unanswered question appears in Needs you,
counts there in the sidebar and Overview, and uses the `?` mark. It leaves
Running and suppresses "nothing needs you". Its pane shows the question title,
numbered option labels and summaries, and the recommended option read-only;
answering stays with whoever started the run. Linked engine runs keep the
engine's classification and are not counted twice. Approval waits and waits
without a question stay in Running. The hint line says
"started elsewhere, read-only; esc close pane" (the inline header is the run ID
alone); the pane's footer no longer carries it (S149).

The pane follows the newest running run (*proposed*, S109). When the pane's
run is over (an engine run that is stopped, superseded or committed, or a
ledger run that has ended), the next snapshot points the pane at the newest
running run of the same project: an engine run in the Running section, or an
active ledger run that no engine run links to and that is not stale. The new
pane starts at the top, with no answer or filing state, and a line under the
header says why, cause first, for example `S27 committed; now showing S28`.
An ended ledger run reads `passed`, `failed` or `stopped`, as in Recent. The
line is dim (faint under `NO_COLOR`) and fades on the first snapshot 5 s or
more after the switch, or on the first key pressed in the pane (S134). While
the line shows, the followed run's Overview row carries a leading `>` in its
marker column, the column of the refresh `~` (S134). Only a pane opened on the Overview follows; the
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
  outside the quotes. The hint reads `y send; n cancel`; Enter or Esc also cancels.
  Only y sends it; Enter, n and esc cancel. The label and summary are the answer sent to the paused
  role and, when enabled, adj. `t` opens the free-text field; esc returns to
  the options, and a second esc closes the pane. Typing and paste are ignored
  until `t`. The options hint shows the actual range, such as
  `1-2 pick; t free text; esc close pane`. Free text shows
  `enter submit; esc back to options`. Questions from older records show the
  Answer field directly.
  The exact `ork answer RUN 'TEXT'` command follows the text live. A
  submit shows `sending answer` in the footer while the call runs, not the
  command form: the `ork answer` preview line leaves the pane while the call
  runs. The call returns only when the run rests, so the footer follows
  the snapshots meanwhile: once the run leaves paused it reads
  `answered; implementer resumed (round N)`, and after the call returns
  without a resumed role it reads `answered`. If the owner closed the pane
  first, the result shows `answered` with any adj notice, never the command
  form. Any adj notice follows. The
  status ends at the run's next state change, or when the run pauses again
  with other wait text (a new question), and does not return when a late
  result arrives; an error replaces it. It writes the status line only while
  the line still shows the status it wrote or is empty (typing in the field
  clears it), so another message stays (S134). A new question is told by its
  wait text or its decision key. Once the run is not
  paused, its Running row and pane header drop the answered question and
  their wait text (S127). Enter submits
  without y/n; an empty or whitespace-only answer is refused. The field owns
  typing keys, including the global action letters. Backspace deletes the
  previous rune, ctrl-u clears the field, and left/right move the cursor.
  Terminal paste inserts at the cursor and strips CR and LF, keeping one
  answer line; paste never submits (`ui.text-field`).
  Ctrl+a toggles optional filing in adj, off whenever the pane opens. The footer
  shows `[ ] file answer in adj as KEY`; when on, `[x]` is
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
  status, enabled toggle, off/disabled toggle and command preview
  in that priority order, dropping the lowest priority lines first.
  This toggle is available only in the TUI; `ork answer` does not file in adj.
- Paused with `pending: budget`: show the reason; the hint lists `t retry`
  when available. Retry grants exactly one
  role round, then the budget check applies again (`run.budget-resume`).
- Paused for another reason (not a question or budget): show the reason and
  `t` retries, with the same
  `ork retry RUN` y/n confirmation as the Overview. When a sibling's owner
  question is queued, the retry shows that question first; the retried role
  runs after the answer (S159, *proposed*).
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
  `git diff --stat`). The per-file diff stat, then the new files as `PATH | (new)`, scroll above the
  tree. The names are padded to one column, so the bar of every line, and the
  counts (git's own right-aligned padding is kept) and `(new)` after it, line
  up; a name longer than the pane less 12 columns is cut with `...` (S141).
- The owner findings, as under Finding kinds below (and the later findings
  count). S111's structured questions show here as `Owner question, not
  blocking: TITLE Recommended: LABEL.`
- With a terminal taller than 24 rows, a blank line sets apart the card's
  blocks: the summary, the findings, the human checks and the line below them
  (S141). At 24 rows, or when the pane is short, they are the first lines to
  go, and they never take room the scrolling body needs: when the body would
  keep fewer than 8 lines (or all of its lines, if fewer), they are left out.
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
  the pane is short, lines drop in this order: the blank lines between the
  blocks, the later findings, then this line, then each owner finding whole
  (the last first), then the status line (its last wrapped line first), and the
  human checks last (S141: this line goes before an owner finding, so a check
  that wraps at 80x24 does not push the owner's recommendation out; with push
  on, the line that says approval pushes ranks above the owner findings, and
  the owner findings drop first). When the project's `push` setting is on, it reads
  `Approve lands and pushes it, then removes its worktree and branch.` (it wraps
  to two lines at 80x24; the TUI reads the setting from the registry entry);
  with push off it reads `Approve lands it, then removes its worktree and
  branch.`
- The shared hint line lists available card actions: `a approve`, `F fix round`,
  `x stop`, `1-N tick/later`, `v renders`, `f file slice`, then scroll keys
  and `esc close pane`. Actions drop by the priorities above; the card body
  has no key footer.
- `Renders: N from the UI pass (round R)` (dim; `, K more not kept` when the
  card dropped files), under the diff line. When the UI pass ran and wrote
  none it reads `Renders: the UI pass (round R) wrote none`. Without a UI pass
  the card has no such line.

A digit marks the human check of that number and moves it on: ticked, then
later, then open again. The mark is saved in the run record at once (`run.check`).
With ten or more checks the digits make a number (S141): a digit that no other
digit can make longer marks at once (check 3 of 12), and one that could start a
longer number (1 of 12) waits. While it waits the hint line, which is always in
view, reads `enter mark check 1_; 0-2 longer; esc drop`; the next digit makes
it `12`, or `enter` marks check 1. `esc` drops the number (the pane stays
open), and so does any other key, scroll keys included, and so does the run
leaving the card. A number past the last check says `the slice has no human
check 13`. On a card of nine checks or fewer every digit marks at once, as
before. With ten or more checks the numbers are padded to one width (` 1 [ ]`,
`12 [ ]`), so the brackets and the text line up, and a wrapped check hangs
under its text.
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
note on the slice, which replaces the human-check adj records. It writes the
note once: a second approval, after a landing fix round, or after the first
approval's record could not be saved, does not write it again. When the daemon
could not read the checks (`Human checks: not read (WHY)`), `a` approves without
marks and the note says the checks could not be read (S141). From the shell the
same marks are `ork check RUN N [ticked|later|open]` and the fix note is `ork
fix RUN TEXT` (`design.md`, CLI), so `ork approve` works on a slice with human
checks.

`F` (shift-f) opens the fix note. It is a capital because `f` files later
findings on the same card, and `e` keeps its meaning in Waves. The footer shows `Fix note: TEXT` and `enter sends the
note to a fix round; esc back to card`. The field owns every key, including digits and
the action letters. Enter sends the note: an empty note is refused, and the
status line follows the call, which returns when the run rests again. Meanwhile
the run goes through its fix round, the checks and the gate, and its pane shows
the card again on PASS, with the marks it had. A failed call (the round limit,
a stopped run) keeps the note in the field. The status line tells the call in
words, not as a command: `fix note
sent; fix round running` while it runs, then `fix round finished`; a failure
reads `fix note: ERROR`.
`esc` goes back to the card, and a second `esc` closes the pane. The
`Human checks` label is a heading in every case (open, none, not read), and a
check that wraps hangs under its text, which starts at column 6. A run without a
card (a record from before S118) lists `a approve; x stop` in the hint when
those actions are available.
At 80x24 the card's footer keeps one line for the header that names the run
(`Run ID  ITEM  STATE`), so the card gives up lower-priority lines first (the
later and owner findings) before that header goes; the
tree and Overview rows may scroll out of sight.

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
fills the rows below the rule, and the footer shows `Render K of N: NAME`.
The hint shows `v next render; s sidebar` and the scroll and close keys. The card's summary,
checks and other keys give way. `v` moves to the next render; on the last one
the hint lists `v back to card` and `esc back to card`, and `v`
returns to the card, where the Overview rows return. If the card
changes while a render shows (a fix round started from another TUI), the pane
shows the card again.

`s` hides or shows the sidebar: a render is not wrapped, so a row wider than
the pane is cut with `...`, and the sidebar takes 20 columns below 100
columns and 28 from 100 columns up. A render taller than the pane scrolls, one line
for each press of `pgup` or `pgdown`, with `+N lines above` and `+N more
lines` markers. A 40-row render does not fit at 80x24 or at 120x40, since the
pane's name line and footer take rows too, so it scrolls at both. While a render shows, no card
key acts except those: the owner cannot mark a check or approve while the
card is hidden. `esc` returns to the card, and a second `esc` closes the pane.
ork draws the text it was given, after dropping control characters again; it
does not render the view itself.

**Finding kinds** (adj #163, `review.finding-kinds`). A reviewer, UI pass or
gate report tags each finding `fix`, `owner` or `later`. In every pane that is
not a question, the footer lists what the report left for the owner. Later
findings read `N later findings; next to file: "TITLE"`. When one can be
filed, the shared hint line offers `f file slice` when it fits, on a
stopped run too, and after a daemon restart for a stopped or committed run.
`f` shows the command first:
`slicer add 'TITLE' --discovered-from ITEM`, followed by the note that
ork adds a Why section. When scores were proposed, the note also names
`slicer set`. The final line asks `File this finding as a slice? [y/N]
(Enter is No)`. `y` makes ork run it in the project and add the Why section.
The status line then says `filed as ID`, and the next press offers the
next finding. Filed findings read `Filed as slices: ID` and are not offered
again. An *owner* finding that does not block reads `Owner question, not
blocking: TITLE Recommended: LABEL.`; the slice goes on. Proposed (S137):
`o answer owner finding` opens the first unanswered finding as its structured
question. Answering records it without pausing the run; a decision key also
records the answer in adj when the project has adj (the caller must set
`ADJ_DECIDED_BY`). Otherwise it only records the answer on the finding.
The confirmation names the finding's title. The footer shows all four owner
findings when there are four; with five or more, it shows three and then
`+N more owner findings`. A single remaining finding is shown in full.
Unfiled later and unanswered owner findings appear in the Needs you Findings
group and contribute one each to the counts only until their run is landed,
dismissed or superseded. Those findings remain visible in an open run pane.
Enter on a Ready-to-land row
opens its run pane, where `f` works too. `ork file RUN N` files a later
finding through the daemon. The filing confirmation says ork adds a Why section.
A role's complete proposed importance, urgency and effort scores are applied
after filing; absent scores leave it unscored.
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
Its tab in the view bar shows the number of questions the list holds ("Questions
3"; just "Questions" when there are none) on every view (S149, *proposed*). The
count is drawn in the Badge style (bold, amber in colour), the name keeps the
view bar's styles (S152). When
the bar would not fit the main pane, as with `Overview (Running)` at 80 columns,
the count goes so that every tab name stays whole.
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
errors "! error". The list has no footer: the hint line names `enter open`
(S152).

The proposed row keys (`down`/`j` and `up`/`k`) move the cursor.
In record detail, they scroll; `pgup` and `pgdown` scroll a page.
The list always shows all projects. Sidebar selection, project filtering and
`[`/`]` project keys do not change it. There is no scope toggle.
`a`, `x`, `t` and `c` have no action on the list or read-only record detail.
The engine pane behaves as on Overview. It keeps its answer typing and
submit keys. If the run changes state while the pane is open, its approve,
stop and retry keys follow the run state and the shared hint line.

## Waves (one project)

The Waves composer is available (S82). Select one project. `tab` and
`shift+tab` reach it between Questions and Roadmap. It loads `wave.suggest`
when opened or the project changes.
The first three suggested waves appear in slicer's order. A dim `N more
waves` line (`1 more wave` for one) offers `[A] show all` (shift-a, as on
Decisions; `a` is Approve and does nothing here); hidden waves cannot be
selected until expanded. Unplaced items remain available. Wave headers are full-width
box-drawing rules with item counts, or hyphens in ASCII mode. Tick, item id, title and
Shares form aligned columns. The dim Shares column contains only the
predecessor id, for example `<A` means shares files with predecessor A;
shared paths stay out of the table. `?` explains the `<` marker.
Unplaced reasons use plain words: `no_files` becomes `no files listed`.
Reasons that do not fit beside the title appear on the following lines, dim
and indented to the title column.
Long titles shorten to keep the tick, id and Shares columns visible.
The cursor bar spans the row's full width, including its Lineup continuation
lines. The single hint line stays above the status line while the body
scrolls; no body footer repeats the keys. A dim cell stays dim across all its columns.

S60 provides the CLI: `ork wave suggest PROJECT` prints the ordered waves
from `slicer plan-parallel`, including reasons. `ork wave start PROJECT
ITEM...` takes the chosen item ids (`wave.start-input`), plans each item,
runs the spec check and asks its open questions (S116; see design.md, Wave
lifecycle), then asks for confirmation. `--yes` requires explicit `--well-specified
y/n` and `--split y/n`; these flags apply to every item. One wave
contains one project (`wave.scope`). Its runs share a wave id and respect
the global and project caps. A failed or stopped item leaves the others
running (`wave.failure`). `ork stop WAVE` uses the `wave-` prefix
(`wave.stop-target`) and stops each unfinished member once, including
queued members without tool writes.

The composer uses `wave.suggest` and `wave.overlap` reads from S81 and
`wave.start` with per-item launch inputs and confirmed plans (`run.launch-wire`).
No launch writes happen before confirmation. Before the spec-check role runs,
ork reads slicer's dependencies for every ticked item. A blocked item refuses
the whole call, for example `S2 waits on S1 "Title" (in this wave, not done);
untick S2 or land S1 first` when both items are ticked. A blocker outside the
wave ends with `land S1 first`. Blocker titles are read once per distinct
blocker per call; a failed title read falls back to the id alone. The status
line shows the refusal and keeps the ticks.
The daemon checks again at wave start; a newly blocked item also refuses the
whole call and keeps the ticks. A failed dependency read prevents launch.
The CLI shows the refusal before asking `Start this wave?`. Human refusal
output shows the text alone; JSON output includes code `refused`. Other
errors show their code and message verbatim, including per-item launch errors.

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

--------------------------------------------------------------------
  2 ticked, not launched
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
the hint changes to `tab next field; enter plan; esc cancel`.
`tab` or `shift+tab` switches the active field; `enter` validates both and plans the item.
`esc` cancels that tick. The Lineup column appears only while at least
one item is ticked and fills from the plan (`ui.wave-composer`). It uses
short role labels: impl, rev, ui, gate and arch, padded so the values share one
column. The `ui` row shows only when the plan has a UI pass row (S136); it is
never marked `*`, since the launch edit does not change it. Each role shows its primary entry and a dim `+N fallbacks` line
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
check: N questions for ID, ID` in dim text, then each question in turn as
`[ID n/of] Title` in the Heading style with its numbered options
(`(recommended)` marks the recommended one),
a dim `Why N: reason` and the decision key when it has one, a blank line, and
a dim `K of N answered` line. `1`-`N` picks an option, `t` types a free answer
(`enter` submits it, `esc` goes back to the options), and `s` or `esc` skips
the rest of the sitting. When an item's last question is answered, ork
appends the answers to its Implement section, keeps them for the item's run,
and the item counts as ready. The saved answers survive a daemon restart
until run start or a spec change. Spec checks and answer writes use separate
connections so their waits do not block other sitting requests. Any shared
`Owner needed` text from the report is kept in the sitting and the Waves
list. A question with a decision key is also filed in
adj when the project has adj and `ADJ_DECIDED_BY` is set (the status line says
why not otherwise). When the sitting ends, the confirmation below covers the
ready items only, and says which items it leaves out after the command, for
example `ork wave start P A? [y/N] (Enter is No)  B waits on spec questions`
(the part after the command is not run). A held item stays ticked with `waits
on N spec questions` on its row, the launch status says `ID waits on spec
questions`, and while a ticked item is held the hint reads `l answer spec
questions` instead of `l launch`. Pressing `l` again checks the ticked
items again and reopens the sitting. A check error shows its code and message
and leaves the ticks alone. A check automatically retries once if HEAD moves,
the checkout status changes outside the role's own run, or the report has a
shape error. A second failure says a retry already happened and asks the
owner to check again. Permission denials are never retried.
At 80x24, spec notes share the rows beside the lineup so the overlap warnings
stay visible. `wave start --json` includes `held` item IDs when ready peers
start; the CLI files keyed answers in adj only when the registry enables adj
for that project.

Until launch the composer says `N ticked, not launched`
(not shown in the lineup editor). After admission it says `launched S78 as
RUN; enter opens it` for one item, or `launched N items; enter on a running
row opens its run`. A launched row stays dim, shows `[>]` instead of the tick
box and `running RUN` after the title (on its own dim line when it does not
fit), and cannot be ticked again. Its hint reads `enter open run`, and
`enter` selects the run in Overview, including when the run first appears
in the next refresh.
Every ticked item must have a successful plan before launch. One failed
plan prevents launching the chosen set; the owner can untick it. The
wave.plan-failure decision remains open (adj 146).

`e` edits the selected ticked item after its plan is ready (S83).
The editor starts from that item's plan: implementer, reviewer and architect,
then budget minutes and tokens. The architect shows `off` when absent.
The gate follows the reviewer and has no field.
When the plan has a `ui` entry (the matrix's UI pass row), a line below the
architect lists its chain for information: the UI pass runs beside the reviewer
only on a slice that touches the UI (`roles.ui-pass`, adj #165), has no field,
and the cursor skips it.
`up`/`down`, `tab` and `shift+tab` move between rows.
`left` and `right` cycle the role's chain already in
`RunPlan.Lineup[role]`: primary first, then alternatives. Until S49, the
daemon plan has one row per role, so left and right have no effect; use
`c` to change a role. The TUI adds no matrix read.
`c` opens vendor, model and effort fields for a custom primary entry.
On the architect row, `c` adds an absent architect and `d` drops its whole
chain. A dropped architect shows `architect*: off` without cycle arrows or
an alternatives label. Saving sends an empty architect override to drop it.
`tab` and `shift+tab` switch fields; `enter` uses the custom entry.
Cycling moves the chosen row to the front; the rest keep their order.
Custom entry replaces the primary; fallback entries cannot be edited here.
Budget fields accept whole numbers of zero or more; zero uses the project
default, following the daemon's budget semantics. On a budget row the hint
reads `up/down/tab row; type a whole number; enter save and replan; esc
discard`; at 80 and 100 columns `up/down/tab row` goes first and the line reads
`type a whole number; enter save and replan; esc discard` (S152).
`enter` saves and replans; unchanged values do not call Plan.
Launch waits for the replan. Errors show verbatim and keep the old plan
and overrides. `esc` discards the editor's changes, including custom fields.
The editor marks roles whose chains differ from its starting plan with `*`.
The Waves Lineup column marks overridden roles with `*` and shows a dropped
architect as `arch*: off`. At narrow widths, the drop hint disappears before
`enter save and replan`. When budgets are
overridden, it shows the effective budgets as `budgets*: MIN min, TOKENS tokens`;
a zero override shows the project default.
Edits apply to that run only and travel with that item's confirmed plan;
the matrix is unchanged (`ui.lineup-editor`, `lineup.override-scope`).

```
Edit lineup: A  Alpha
  Run only; gate follows reviewer.
  > implementer: claude sonnet high (no alternatives)
    reviewer: claude opus medium
    architect: off
    ui: claude opus medium (UI slices only; from the matrix)
    Budget minutes: 45
    Budget tokens: 9000
  Zero budget uses the project default.
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
same line. A UI pass is recorded under its own `ui` ledger role. In a linked
engine tree it shows `round N`, read from the engine's role record by step id,
and does not count toward the reviewer's `xN`. Only the `ui` node shows a round
in a linked tree. A ledger run without an engine record also names the step
`ui`. A `start` step has an ASCII spinner (`| / - \`) and uses the heading
style (bold cyan in colour, bold in mono). A `done` step has `+` and is dim
(grey in colour, faint in mono). A `fail` step has `!` and uses the error style
(bold red in colour, bold in mono). A `wait` step uses the Needs-you glyph
`?` and the warning style, with `waiting on owner` or `waiting on approval`.
It is static and shows no elapsed time; the earlier time before the wait is
not a waiting duration. Other phases use a neutral `.` and retain
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
Each run of items with the same pass sits under a dim heading line with the
pass and the run's size, for example `v1.0 (24)` (S162, owner decision
2026-10-27). Items with no pass fall under `No pass (N)`. Slicer lists a pass
together; a pass that comes back after another pass starts a new run with its
own heading and count, so `{` and `}` land on exactly the headings drawn. The
headings are not rows: the cursor skips them and lands on the first item under
one.
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
implementer, reviewer, ui (when the plan has a UI pass row, S136), architect
(when present) and gate lineup entries as vendor, model and effort, with each
role's permissions. When the plan has a `note` (the rules call failed, S159,
*proposed*), a `note: TEXT` line in the warning colour (plain in mono) follows the role rows, wrapped to the
pane; `ork run` prints the same rows in its plan text, in the same order, and
the same `note: TEXT` line after them. The confirmation bar shows
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
`All projects using adj`. The hint line lists the tab, scope and
open-record keys. On "Needs you", it omits the scope toggle hint. `left` and
`right` switch the Open and Decided tabs; `[` and `]` switch projects or entries. `A`
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
it colors finished roles green, running cyan, failed red, and unstarted roles dim.
A role in phase `wait` reads `ROLE waiting` in the pipeline and ledger row,
using the warning style with no running brackets. Its detail line reads
`waiting: ROLE: task`.
The running role uses brackets only for live, non-stale runs. In monochrome
failed is bold and unstarted is dim; passed is not bold. Warning text is plain
with the `!` cue and no underline. `round N` counts review rounds (the fix
rounds plus one, S145), appears only when positive, and drops first when
narrow. It is right-aligned on the pipeline line only for a run whose bar has no
round word: once the bar says `fix N` or `gate`, the pane's pipeline line
carries no round (S106). For live runs, `last heard
N ago` is dim through 10 minutes, amber (the warning color) past 10 minutes,
and red (the error color) past 30 minutes or for a stale run; the cue is `!` for
amber and `!!` for red, so monochrome keeps it. It sits on the bar's line, after
the bar and its round (S106). Ended, stopped, paused and committed runs have a dim pipeline,
no queued tail rows. Their last-heard age stays dim, with no warning.
Awaiting-approval runs are in the same group: none of them runs, so the pane
shows no queued rows and no activity line, and the stage bar, the pipeline and
the spinner of an unfinished step (`|`) stay still across frames; no frame tick
is armed for the pane (S154). A run that
waits on the owner (awaiting approval, paused, or any ledger step in phase
`wait`) replaces last heard with `waiting since HH:MM`, dim, with no `!` or
`!!` alarm and no stale cue, regardless of age. It sits where last heard does:
on the bar's line in the pane and in the detail line. The time is when the wait
began, in the clock's zone: the wait step's `updated_at` for a ledger step, the
persisted `wait_started_at` (then `updated_at`) for an engine run. A wait that
began on an earlier day than the clock's names the day: `waiting since Oct 27
16:42`; the same day reads `waiting since 16:42`. The phrase does not age and
the detail line never breaks inside it. The row cell is shorter, as rows are
narrow: `wait 19m`, the age as `heard 19m` gives it, dim, in the same place. A
run that is not live and not waiting on the owner (stopped, committed or ended)
never reads as waiting, even if its ledger kept a step in phase `wait`. A
live run adds `queued` tail rows only for pipeline roles with no lane step;
queued steps keep their existing tree row. Queued rows share the tree's
phase column. Repeated roles carry `xN` on the latest tree node. A failed
gate has a `✗ gate FAIL -> implementer round N` line beneath it, followed by
`: REASON` only when the task is non-empty. Started steps
in stale or ended ledger runs use `~` and stop counting at the last event.
Engine runs without a ledger derive pipeline and round from role records,
and last heard from the latest role timestamp.

S07 adds an ASCII bar above the pane pipeline. Each role has a two-column
cell: done `##`, unstarted `..`, waiting `--`, failed `!!`, and running
`~~`, `~=`, `==`, `=~`. A waiting cell stays static.
The normal order is implementer, reviewer, gate; an architect leads when
present. A new implementer round reopens its cell and resets earlier reviewer,
gate and architect steps to unstarted. The round word follows the bar once there
is one (`fix 1`, `fix 2`, `gate`; it replaced `rN`, which counted implementer
steps and so counted the finish step and resumed steps, S145), and last heard
follows it on the same line (S106); a pane
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
