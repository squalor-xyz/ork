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
- **Show the command.** Every write shows the exact command it will run. A
  paid or irreversible action (launch, approve and commit, stop) asks for
  confirmation.
- **Nothing blocks.** A slow call shows as pending on its row.
- **Errors verbatim.** A failed CLI call shows its `error.code` and
  message. ork does not guess.
- **Bell.** A terminal bell plus a badge on the project sidebar row when something
  newly needs the owner (`ui.notify`).

## Layout

```
ork  ?help           | Overview | Waves | Roadmap | Decisions
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
The view bar stays at the top of the main pane. Only Overview is selectable.

Each project row shows its needs-you count (open adj records plus slicer
items flagged `needs-owner`). `*` marks a running slice and `~` marks a
loaded project being refreshed. Selection uses reverse and `>`, including
under `NO_COLOR`. Projects with needs come first, then running projects,
then the rest; registry order breaks ties. Selection follows the project.
A project with no `.slicer/` folder shows `no slicer`, dimmed, in place of its
count. It has no Overview rows and adds nothing to any count or to the bell.
When it is selected, its detail line says to run `slicer init`
(`ui.setup-errors`).

The list scrolls to keep selection visible and shows `+N more` for hidden
rows. Below 100 columns the sidebar narrows; names that do not fit end in
`...` and keep their badges. `s` hides or shows the sidebar at any width.
Keys `up`/`down`, `k`/`j`, `tab`/`shift+tab`, and `left`/`right` move selection
and wrap through "Needs you" and the sorted projects. `1`-`9` select projects
in registry order. `/` filters project names without regard to case; `enter`
keeps the filter and leaves text entry, and `esc` clears it. `r` refreshes
the selected project, or all projects on "Needs you". `?` shows keys and
`q` quits.

The Overview has a row cursor (`ui.overview-cursor`). `n` and `p` move it to
the next and previous row, through "Needs you" and then Running, and it stops
at the first and last row. The row under the cursor is drawn reversed. Its
full text is on a detail line below the rows, wrapped at the pane's width so
no word is cut. `enter` selects the cursor row's project. Project keys and
`1`-`9` select a project and put the cursor on its first row. Rows are cut
with `...` to one line. A run's ID is never cut: the rest of the run's text is
cut first. The rows scroll to keep the cursor row on screen, with
`+N lines above` and `+N more lines` where lines are hidden.

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
3. **Ready to land:** committed slices whose branch is not merged, with the
   `slicer land ID --branch B` command to copy. Merged branches are
   detected and offered for cleanup (`landing.v1`, `git.cleanup`).

A failed call is an error row. A project has one error row per error code.
The parts that failed with that code come first, then the code, then the
tool's message as printed, for example `items, runs: locked: Ledger is busy.`
Different messages under one code are joined with ` / `. An items or inbox
failure sits under Needs you. A runs failure alone sits under Running. Error
rows never count toward the badge (`ui.setup-errors`).

## Waves (one project)

The wave composer and the running waves.

```
Suggested (slicer plan-parallel)            Lineup
[x] s374 Worktrees miss affected map  S     impl   claude sonnet high  -> codex gpt-6.1-sol medium
[x] s375 Order names one-pass fix     S     review claude opus medium
[ ] s377 ...                          M     gate   (reviewer, lite)
! s374 and s377 both touch src/slicer/affected.py
                                     [l] launch wave   [e] edit lineup
```

Each running slice expands to its roles:

```
s374  review  round 1  23m  41k tok  $0.62   [■■■■■□□□] ▁▃▅▇▅
  implementer  claude sonnet high   done     +142 -12  tests 2621 ok
  checks       go test / slicer     green    58s
  reviewer     claude opus medium   running  ⠹ reading diff
  └ tail: "Checking rollback path in affected.py:88 ..."
```

Per-role detail (`dash.role-detail`):
- live output tail;
- tokens and cost from the vendor's usage fields;
- elapsed time, with a stale flag when there is no event for N minutes;
- worktree `git diff --stat` and the latest test counts;
- small ASCII charts: stage bars, a token sparkline, and a wave timeline
  with one lane per slice;
- spinners on running roles.

The actions are approve, stop, answer, retry on another vendor, and open
the worktree path. Relays started outside ork appear with a "started
elsewhere" marker and no actions (`dash.external-runs`).

## Roadmap (one project, read-only in v1)

The queue in slicer's ranked order, with readiness, status, size, effort
and claim, and the selected slice in a detail pane. The data comes from
`slicer list --json --lean`, `slicer show ID --json --lean` and
`slicer goals --json`. From here an item can be added to the wave composer.
Editing comes after v1, and `ui.slicer-tui` is open.

## Decisions (one project or all, read-only in v1)

The open questions first, each with its options and recommendation. The
decided records are on a second tab. The data comes from
`adj inbox --json` and `adj list --json`, called per registered project.
That needs nothing unreleased in adj. Answering owner questions from a
running slice happens on the Waves screen (`owner.questions`).
