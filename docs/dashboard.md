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
- **Bell.** A terminal bell plus a badge on the project tab when something
  newly needs the owner (`ui.notify`).

## Layout

```
+-- ork --------------------------------------------------------------------+
| [slicer 2*] [adjudicate 1] [DataBall] [agents]         needs you: 3  ?help |
+---------------------------------------------------------------------------+
| Overview | Waves | Roadmap | Decisions                                     |
+---------------------------------------------------------------------------+
|                               active view                                  |
+---------------------------------------------------------------------------+
| daemon up 2h | running 4/4 | queued 2 | tokens today 1.2M ▁▂▅▇▅▃ | 16:42  |
+---------------------------------------------------------------------------+
```

- **Project tabs** come from the registry (`registry.initial`). The number
  counts what needs the owner, and `*` marks a running slice.
- **Status line:** daemon uptime, global cap use, queued items, and a token
  sparkline (`ui.charts`).

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
