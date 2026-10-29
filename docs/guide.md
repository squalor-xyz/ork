# Guide

A walk-through for a new owner: install ork, add a project, run one slice,
run a wave, answer questions, approve and land, and keep an eye on several
projects. The pictures are in [diagrams.md](diagrams.md). The details behind
each step are in [design.md](design.md), [dashboard.md](dashboard.md) and
[state.md](state.md).

Citations are `path:line` in this repository. Where a design document calls
a command *proposed* but the code has it, this guide says "built" and cites
the code. Where only a document says it, the guide says so.

## 1. What ork does

ork runs slice relays for you across projects. A slice is one tracked unit of
work in slicer. For each slice ork makes a git worktree, runs an implementer,
runs the project checks, runs a reviewer, runs fix rounds, runs a gate, and
then waits for you. You approve; ork finishes, commits and lands the slice on
the base branch (`docs/design.md:162-221`, `docs/design.md:351-382`).

What ork owns: the list of projects, a record per run, role reports, and the
daemon (`docs/state.md:8-26`). What it does not own: the roadmap and the
ledger (slicer), decisions (adj) and the code (git). ork reaches those only
through their command lines (`docs/boundaries.md`, `README.md:46-47`).

Two programs make up ork:

- The **daemon** runs the work. It keeps running when you close the TUI.
- The **TUI** and the **commands** are clients. They ask the daemon
  (`docs/design.md:22-24`).

You never start the daemon by hand. `ork` and every command that needs it
start it when nothing answers (`docs/design.md:24-40`,
`cmd/ork/main.go:139-153`). It exits after 10 idle minutes by default
(`docs/design.md:26-29`).

## 2. Install and build

From the repository root, with Go installed (`README.md:24-33`):

    go install ./cmd/ork

Go writes `ork` to `$(go env GOBIN)`, or to `$(go env GOPATH)/bin`. Put that
directory on your `PATH`. `ork --version` prints the version
(`cmd/ork/main.go:94-108`).

Keep the binary and the daemon in step. After you rebuild, run `ork daemon
restart`. A daemon from an older build makes every command print a warning
that names this command (`docs/design.md:59-75`). `ork daemon stop` refuses
while a role is running unless you add `--force` (`docs/design.md:41-48`).

## 3. Check your tools: `ork doctor`

    ork doctor

It prints the path of the ork binary first, then one line per check
(`internal/cli/cli.go:73-75`, `docs/design.md:1105-1144`):

- the slicer and adj versions;
- each vendor CLI (claude, codex, grok) and its version. ork runs roles on `claude` and `codex` only; grok has no adapter (`docs/design.md:181`). A missing vendor is
  reported and does not fail the check, because ork needs only the vendors
  your lineup uses;
- the slicer relay contract. ork accepts v7 and v8 only; any other version
  fails (`docs/design.md:1117-1121`);
- each registered project: the path exists, the check command parses, and the
  worktree and branch patterns contain `{id}` or `{id_lower}`.

`ork doctor` exits 1 when slicer, adj or the contract check fails. Fix those
before you run anything. Project findings are warnings and do not change the
exit code (`docs/design.md:1139-1140`).

ork never reads vendor credentials. Log in with each vendor CLI's own login
(`AGENTS.md`, Project Rules).

## 4. Add a project

ork needs an existing absolute directory (`docs/design.md:1203-1209`). The
TUI Welcome view offers the current directory only if it holds `.slicer` or
`.adjudicate.json` (`docs/dashboard.md:31-35`), and the roadmap and ledger
features need slicer. To set a directory up, run `slicer init` and `adj init`
in it; these are slicer and adj commands, not ork's.

Add it in either way:

- **In the TUI.** With no projects, ork opens the Welcome view. Press `+`,
  type an absolute path (a leading `~` works) and press `y` to confirm
  (`docs/dashboard.md:27-51`). `+` works from every view, and `-` removes the
  selected project (`docs/dashboard.md:65-83`).
- **On the command line.**
  `ork projects add /abs/path/to/project --name NAME`. With no path in text
  mode it offers the current directory (`internal/cli/cli.go:58-59`,
  `docs/design.md:1201-1210`).

Other commands: `ork projects list` and `ork projects rm NAME`. Removal is
refused while the project has unfinished runs (`docs/design.md:1213-1217`).
The first add creates `~/.config/ork/projects.json`; ork never seeds it
(`docs/state.md:28-33`).

## 5. Set a project's options

The registry holds optional per-project settings. Change them with
`ork projects set` (`internal/cli/cli.go:61-67`):

| Flag | What it sets |
|---|---|
| `--check CMD` | the check command ork runs after each implementer round |
| `--check-minutes N` | minutes the check command may run (default 10; `internal/engine/states.go:197-202`). `docs/state.md:76-78` also calls it the stale flag, which the code does not do |
| `--worktree-pattern P` | worktree path; must contain `{id}` or `{id_lower}` (default `.worktrees/{id}`) |
| `--branch-pattern P` | branch name; same rule (default `{id}`) |
| `--cap N` | how many runs of this project may be busy at once (unset means no project cap) |
| `--budget-minutes N` | run-time budget per slice |
| `--budget-tokens N` | token budget per slice |
| `--push on\|off` | push the base branch after landing; off by default |
| `--ui-doc PATH` | the project's UI design doc, relative to the project; the UI pass reads it |

An empty value or 0 clears a field. The command is refused while the project
has unfinished runs, and the daemon needs no restart: the next run uses the
new values (`docs/design.md:1218-1235`, `docs/state.md:94-103`).

Some fields have no command and are file edits in
`~/.config/ork/projects.json`: `environment_lines`, `permissions`,
`vendor_env`, `global_cap` and `daemon_idle_minutes` (`docs/state.md:92-103`).
`global_cap` defaults to 4 (`internal/daemon/runs.go:44`).

## 6. The role lineup

Which model runs each role comes from the role matrix, which slicer owns.
ork does not edit it. Look at it and change it with slicer:

    slicer relay-roles show --root DIR
    slicer relay-roles add|set|remove|move|rule-add|rule-edit|rule-remove

For each slice ork calls `slicer relay-roles recommend ID --well-specified
y/n --split y/n` and turns the answer into the lineup for implementer,
reviewer, gate, and an architect when the lineup has one (the matrix gives one on size L) (`docs/design.md:167-186`, `docs/design.md:265-268`,
`internal/slicercli/write.go:96`). A matrix row for a vendor ork has no adapter
for is skipped; ork runs `claude` and `codex` only (`docs/design.md:181-185`).

At launch you can change the lineup and the budgets for that one run. The
matrix stays as it was (`docs/design.md:170-175`). In the TUI the key is `e`
on the Waves view (section 9). On the command line there is no flag for it.

## 7. Run one slice

Pick a slice that is ready in the roadmap. A slice that lists its files,
failing tests, steps and checks is "well specified".

**In the TUI.** Open the Roadmap view and select the project. Press `l` to
open the list of ready slices, move to one and press `enter`. Answer the two
questions (well specified, split; `enter` moves to the next field), read the
plan (worktree, branch, check command, budgets, lineup) and press `y` to
start (`docs/dashboard.md:1300-1305`, `internal/tui/keys.go:294-307`).

**On the command line.**

    ork run PROJECT ID

It asks `Well specified?` and `Split?`, prints the plan, and asks `Start this
slice? [y/N]`. For a script, give `--yes --well-specified y/n --split y/n`
(`internal/cli/run.go:48-50`, `docs/design.md:1010-1018`).

The run is now in the daemon. Closing the TUI or the shell does not stop it.

The roles run in this order (`docs/design.md:223-300`):

1. **setup**: git worktree and branch, and a slicer ledger run.
2. **implement**: one implementer role.
3. **checks**: your check command and `slicer check`. A red result goes back
   to the implementer without spending a review.
4. **review**: a reviewer, and the UI pass beside it on a slice that touches
   the UI. Findings go to one fix round.
5. **architect**: only when the lineup has an architect entry; the matrix gives one on size L (`docs/design.md:265-268`).
6. **gate**: the first line of its report is `VERDICT: PASS` or
   `VERDICT: FAIL`. The third FAIL stops the run.
7. **awaiting approval**: your turn (section 10).

## 8. Watch it

The Overview shows **Needs you** first, then **Running**, then recent and
ready slices. Each Running row shows the role at work, the stage bar and when
ork last heard from it. `!` after 10 minutes and `!!` after 30 minutes without
an event mark a slow or stale run (`docs/dashboard.md:495-498`). `enter` opens
a run's pane with the role tree, the live output and the actions.

On the command line, `ork status [--project P]` prints the same counts and
rows. Add `--json` to any command for scripts (`internal/cli/cli.go:56`,
`internal/cli/cli.go:77`).

## 9. Run a wave

A wave is several slices of one project that start together
(`docs/design.md:162-221`).

**In the TUI.** Open the Waves view (`tab` from Questions). It loads slicer's
suggested waves. For each item you want:

1. Press `space` to tick it, then answer well specified and split.
2. Press `e` to edit the lineup or budgets for that run, if you want to.
3. Press `l` to launch. ork first runs the spec check over every ticked item.
   If it has questions, they come one at a time: `1`-`4` picks an option, `t`
   types an answer, `s` or `esc` skips the rest. Items with open questions are
   held back.
4. Confirm with `y`. The status line shows `ork wave start PROJECT ID...`
   (`docs/dashboard.md:1041-1215`, `internal/tui/keys.go:253-293`).

**On the command line.**

    ork wave suggest PROJECT
    ork wave start PROJECT ITEM... [--yes --well-specified y/n --split y/n]
    ork stop WAVE-ID

`wave start` asks the spec-check questions on standard input and prints each
plan before it asks to start (`docs/design.md:974-996`,
`internal/cli/wave.go:90-92`). `ork stop` takes the `wave-` id it prints and
stops every unfinished run in that wave, queued ones included
(`docs/design.md:1004-1008`).

Runs start as slots free. The global cap is 4 and a project's cap is its
`cap` setting. A run over a cap waits as `queued`
(`docs/design.md:212-214`). One failed run does not stop the others.

## 10. Answer questions, approve and land

### Answer questions

A run pauses when a role says it needs you. Questions from every project
appear in the **Questions** view and under **Needs you** on the Overview.

- In the TUI: `enter` opens the run. A digit `1`-`4` loads that option and `y`
  sends it; `t` types a free answer and `enter` sends it. `ctrl+a` toggles filing the answer in adj. Filing
  needs `ADJ_DECIDED_BY` set in the environment of the TUI
  (`internal/tui/keys.go:106-108`, `internal/tui/pane.go:284-297,498`).
- On the command line: `ork answer RUN TEXT`. The CLI sends the text only; it
  does not file in adj (`internal/cli/run.go:223-228`).
- Findings tagged owner but not blocking do not pause the run. Answer one
  with `o` in the run pane (`internal/tui/keys.go:80`).
- A finding tagged later can become a new slice: `f` in the run pane, or
  `ork file RUN N` (`internal/cli/cli.go:52`).

Spec-check questions (before a wave) are answered in the sitting that opens
at launch. See section 9.

### Approve and land

When the gate passes, the run waits in **Needs you** as "Approve". Open it.
The approval card shows the gate result, the diff summary, owner findings and
the slice's human checks (`docs/dashboard.md:826-935`).

- Press the check's number to mark each human check: ticked, then later, then
  open again. `a` works only when none is open.
- `F` opens a fix note. Type it and press `enter`. The run goes through a fix
  round and comes back to the card.
- `v` shows the UI pass's renders, if it wrote any.
- `a` approves. ork runs the finish step, commits, and lands the slice: it
  merges the base branch into the worktree if the base moved, re-runs the
  checks, fast-forwards the main checkout, runs `slicer land`, pushes if the
  project's `push` setting is on, and removes the worktree and the branch
  (`docs/design.md:344-390`).

From the shell: `ork check RUN N [ticked|later|open]`, `ork fix RUN TEXT`,
`ork approve RUN` (`internal/cli/cli.go:47-50`).

If the merge has a conflict or the re-check is red, ork does not fix it
itself. The slice gets one implementer fix round and comes back for a new
approval. A second block stops the run with the worktree kept
(`docs/design.md:383-390`).

## 11. Watch several projects

Use this loop each day:

1. Open `ork`. The sidebar lists **Running**, **Needs you** and each project.
   `0` jumps to Needs you, `1`-`9` to a project, `[` and `]` move between them
   (`internal/tui/keys.go:87-93`).
2. Clear **Needs you** from the top. Group order is Decisions, Held, Stopped,
   Paused, Approve, Findings, Errors (`docs/dashboard.md:396-399`).
3. Open **Questions** for every open question in one list.
4. Open **Decisions** for adj records and **Roadmap** for what is next.
5. Press `r` to refresh now. `/` filters projects. `?` shows every key.

ork rings the terminal bell and puts a badge on the sidebar row when
something newly needs you (`internal/tui/model.go:949-1014`).

## 12. Recover

| Situation | What to do |
|---|---|
| A run is paused with no question (shape, error, lost, interrupted) | `t` on the row, or `ork retry RUN` (`docs/design.md:1028-1031`) |
| A run is paused at a budget limit | `t` or `ork retry RUN` grants one more role round (`docs/design.md:926-937`) |
| A run is stopped | Read the reason in the pane. Fix the cause and launch again, or clear it with `ork dismiss RUN` |
| Nothing for 30 minutes on a running row (`!!`) | ork marks the row stale (`docs/dashboard.md:495-498`). Open it and read the last output. If it is not coming back, stop it with `x` or `ork stop RUN`, then launch again |
| You want to stop work | `x` on a row or `ork stop RUN`; `ork stop WAVE-ID` for a wave |
| The daemon died or you stopped it | Start `ork` again. On start the daemon checks every unfinished run against live processes and marks a role with no process and no report as lost, then the slice waits for you (`docs/design.md:154-160`) |
| Commands warn that the daemon build differs | `ork daemon restart`. It refuses while a role runs unless you add `--force` (`docs/design.md:41-48`) |
| A merged slice left its worktree behind | `c` on the Ready row removes the merged worktree and branch (`internal/tui/keys.go:71`) |

Run `ork doctor` after you upgrade slicer or adj. The slicer contract must
still be one ork accepts.

## 13. Keys at a glance

The TUI draws the keys that work right now on its hint line, and `?` lists
them all (`internal/tui/keys.go:13-124`).

| Where | Key | Action |
|---|---|---|
| Anywhere | `tab` / `shift+tab` | next / previous view |
| Anywhere | `[` `]`, `1`-`9`, `0` | previous or next project, jump to project, Needs you |
| Anywhere | `+` / `-` | add / remove project |
| Anywhere | `/`, `s`, `r`, `?`, `q` | filter, sidebar, refresh, help, quit |
| Lists | `j` `k`, `g` `G`, `pgup` `pgdown`, `ctrl+d` `ctrl+u` | move and scroll |
| Overview | `enter`, `esc` | open / close a run |
| Overview, run pane | `a`, `x`, `t`, `c` | approve, stop, retry, clean up a merged worktree |
| Run pane | `1`-`9`, `F`, `v`, `f`, `o` | mark check, fix note, renders, file slice, answer owner finding |
| Question | `1`-`4` then `y`; `t` then `enter`; `ctrl+a` | load an option and send it; type a free answer and send it; file in adj |
| Running entry | `space`, `E` | expand a run, expand all |
| Roadmap | `l` | launch a ready slice |
| Waves | `space`, `e`, `l`, `A` | tick, edit lineup, launch, show all waves |
| Decisions | `A` | all projects / one project |
| Confirmations | `y` / `n` | yes / no (Enter is No) |

## 14. Commands at a glance

    ork                          open the TUI
    ork --version
    ork status [--project P]
    ork projects list | add [PATH] [--name N] | rm NAME | set NAME [flags]
    ork run P ID
    ork wave suggest P
    ork wave start P ITEM...
    ork answer RUN TEXT
    ork check RUN N [ticked|later|open]
    ork fix RUN TEXT
    ork approve RUN
    ork retry RUN
    ork stop RUN|WAVE
    ork dismiss RUN
    ork file RUN N
    ork roles print [ROLE]
    ork daemon run | stop [--force] | restart [--force]
    ork doctor

Every command takes `--json`. Exit codes: 0 ok, 1 failure, 2 usage
(`internal/cli/cli.go:40-77`).

## 15. What ork does not do yet

The owner still does these outside ork today. The list and a proposed slice
for each is in [orchestrator-parity.md](orchestrator-parity.md).

- Edit the role matrix (`slicer relay-roles ...`).
- Write or fill in slice specs, and add or promote roadmap items by hand.
- Decide in adj: ork can file an answer you give, but you make decisions
  with `adj` (`AGENTS.md`, Decisions).
- Pull and rebuild slicer or adj, and rebuild ork.
