# Diagrams

Six pictures of how ork works. Each one shows one idea. They are Mermaid
text, so a Markdown viewer that supports Mermaid draws them. The walk-through
that goes with them is [guide.md](guide.md). The engine details are in
[design.md](design.md) and the files in [state.md](state.md).

Every command and key named here was checked against the code (see
[guide.md](guide.md)). The design documents still call some of them
*proposed*; the code has them. A step that does not exist yet is marked
*proposed* in the text under its diagram.

## 1. Components and data flow

Who talks to whom. The daemon is the only writer. The TUI and the CLI are
clients that call it over a Unix socket (`docs/design.md:10-20`). ork reaches
slicer, adj and git only through their command lines
(`docs/boundaries.md`).

```mermaid
flowchart TD
    owner["You"] --> tui["ork TUI"]
    owner --> cli["ork commands"]
    tui -->|"JSON over a socket"| daemon["ork daemon"]
    cli -->|"JSON over a socket"| daemon
    daemon --> engine["Engine: one run per slice"]
    engine -->|"CLI calls"| slicer["slicer: roadmap, ledger, matrix"]
    engine -->|"CLI calls"| adj["adj: decisions"]
    engine -->|"CLI calls"| git["git: worktrees, commits, merges"]
    engine -->|"one process per role round"| vendors["claude and codex"]
    daemon --> files["ork files: projects.json and run records"]
```

What to take from it:

- Close the TUI and the daemon keeps the runs going.
- Roles (implementer, reviewer and so on) are plain vendor processes. They
  do not spawn agents.
- ork keeps its own files only in `~/.config/ork` and `~/.local/state/ork`.
  Everything else is read live from slicer, adj and git.

## 2. A run's states

One slice run, from launch to landed. Any state can go to `paused` (a
question, a budget limit or an error) or to `stopped` (a stop rule or your
stop). The names are the states in `internal/runrec/record.go:22-35`.

```mermaid
stateDiagram-v2
    [*] --> queued: over a cap
    [*] --> setup
    queued --> setup: slot frees
    setup --> implement
    implement --> checks
    checks --> implement: red
    checks --> review: green
    checks --> gate: green after a fix round
    review --> implement: findings, fix round
    review --> gate: clean
    gate --> implement: FAIL, fix round
    gate --> awaiting_approval: PASS
    awaiting_approval --> finish: you approve
    awaiting_approval --> implement: you send a fix note
    finish --> commit
    commit --> land
    land --> committed: merged, cleaned up
    land --> implement: merge conflict, one fix round
    committed --> [*]
    implement --> paused: question, budget, error
    paused --> implement: answer or retry
    gate --> stopped: third FAIL
    land --> stopped: second conflict
```

Notes:

- `awaiting_approval` is the state `awaiting-approval` (Mermaid does not
  allow the hyphen).
- When the lineup has an architect (the matrix gives one on size L), an `architect` round runs after review and before the
  gate (`docs/design.md:233`). It is left out here to keep the picture short.
- A stopped run waits in Needs you until you dismiss it (`ork dismiss RUN`).

## 3. One relay round

The roles of one slice, in order, with what you do. The reviewer and the UI
pass run at the same time. The UI pass runs only on a slice that touches the
UI (`docs/design.md:269-275`).

```mermaid
sequenceDiagram
    participant E as Engine
    participant I as Implementer
    participant R as Reviewer
    participant U as UI pass
    participant G as Gate
    participant O as You
    E->>I: brief, then checks
    I-->>E: report
    E->>R: review
    E->>U: UI slices only
    R-->>E: findings
    U-->>E: findings
    E->>I: fix round
    I-->>E: report
    E->>G: gate
    G-->>E: VERDICT PASS
    E->>O: awaiting approval
    O->>E: approve
    E->>I: finish step
    E->>E: commit, then land
```

Notes:

- A gate FAIL sends the engine back to the implementer for a fix round. A
  third FAIL stops the run.
- If a role needs you (an `Owner needed` section), the run pauses and
  waits for your answer. See diagram 6.
- Landing is: merge the base branch into the worktree if it moved, re-check,
  fast-forward the main checkout, `slicer land`, push if the project's `push`
  is on, then remove the worktree and branch (`docs/design.md:351-382`).

## 4. A wave

A wave is a set of slices from one project that start together. Slicer
suggests it. You pick the items, ork plans them and checks the specs, and you
confirm once.

```mermaid
flowchart TD
    a["Suggest: slicer plan-parallel"] --> b["You tick items"]
    b --> c["Lineup and plan per item"]
    c --> d["Spec check over all items"]
    d -->|"open questions"| e["You answer in one sitting"]
    e --> f["You confirm the launch"]
    d -->|"all ready"| f
    f --> g["Runs start under the caps"]
    g --> h["Each run reaches approval"]
    h --> i["You approve each, ork lands it"]
```

Notes:

- One wave holds one project. Several waves can run at once
  (`docs/design.md:212-214`).
- The global cap is 4 busy runs unless the registry sets `global_cap`
  (`internal/daemon/runs.go:44`). A project has no cap unless its `cap` is set
  (`internal/daemon/runs.go:727-745`). An item over a cap waits as `queued`.
- One failed or stopped item leaves the others running. `ork stop WAVE`
  stops every unfinished member.

## 5. Setup to first run

From nothing to a first landed slice.

```mermaid
flowchart TD
    a["Install and build ork"] --> b["ork doctor"]
    b --> c["Add a project: + in the TUI, or ork projects add"]
    c --> d["Project settings: ork projects set"]
    d --> e["Check the role matrix in slicer"]
    e --> f["Pick a slice: l on the Roadmap, or ork run"]
    f --> g["Answer well specified and split, read the plan, confirm"]
    g --> h["Watch it on the Overview"]
    h --> i["Approve and land"]
```

Notes:

- Step "Check the role matrix" is done with `slicer relay-roles` outside ork
  today. ork shows the chosen lineup in the plan and lets you change it for
  one run (`e` in Waves). It does not edit the matrix.
- `ork doctor` reports whether the slicer contract is one ork accepts. It is
  not a gate: the engine makes the same contract check itself before it
  creates a run (`docs/design.md:1117-1121`).

## 6. Daily monitoring and answering

What to do each time you open ork. Needs you is the first screen.

```mermaid
flowchart TD
    a["Open ork"] --> b["Overview: Needs you"]
    b --> c{"What kind of row?"}
    c -->|"owner question"| d["Open it, pick an option or type"]
    c -->|"awaiting approval"| e["Open it, tick checks, approve"]
    c -->|"paused or lost"| f["Retry, or answer"]
    c -->|"stopped"| g["Read why, then fix, file or dismiss"]
    d --> h["Run resumes"]
    e --> i["Run lands"]
    f --> h
    g --> j["Needs you is empty again"]
    h --> j
    i --> j
    j --> k["Running: watch the other runs"]
```

Notes:

- The Questions view lists every owner question from every project in one
  place, so you can answer them without visiting each project.
- The sidebar shows a count and a bell sounds when something newly needs you.
- `ork status` prints the same Needs you counts on the command line.
