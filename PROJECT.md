# ork — dashboard and engine for parallel slice relays

Status: in development, with a Go CLI, TUI and relay engine.
License GPL-3.0-or-later (Squalor LLC).

This file says what ork is and why. The relay engine is specified in
[docs/design.md](docs/design.md), the screens in
[docs/dashboard.md](docs/dashboard.md), ork's own files in
[docs/state.md](docs/state.md), ownership in
[docs/boundaries.md](docs/boundaries.md), and terms in
[docs/glossary.md](docs/glossary.md). Every owner decision is an adj record:
`adj list --status decided`, cited here by key (`wave.end`). Paths like
`slicer/src/slicer/ai.py` are in the sibling repos under `squalor-xyz/`.

## Summary

ork is a terminal dashboard and a relay engine for several projects at
once:

- The **dashboard** shows each registered project's roadmap, its decisions,
  and every running slice with each role's stage, live output, tokens, cost
  and test state.
- The **engine** runs waves of slices in parallel. For each slice, ork does
  Setup, launches every role (implementer, reviewer, gate, architect)
  headlessly on Claude, Codex or Grok, runs deterministic checks between
  roles, falls back across vendors on rate-limited or error exits, and
  commits the slice branch once the owner approves the result.

ork replaces the outer Claude session's manual relay work and the agents
repo's scripted orchestrator (N6, `orchestrate.sh`, `relay-call.sh`)
(`boundary.agents-scripts`). It does not replace slicer or adjudicate. It
reads and writes them only through their CLIs.

## Motivation

- **No single view across projects.** Each project has its own slicer TUI
  and adj inbox. Running relays are visible only per repo, through
  `slicer relay-run list`, or inside the agent session that holds them.
- **The outer session runs the relay by hand and pays for it in context.**
  One session ran 10h46m at about 256k context and cost $83.98; a model
  cannot compact itself
  (`agents/multi-agent-workflows/info/2026-10-15-nested-relay.md:24-33`).
- **Agent-to-agent hand-offs are unreliable.** Hand-backs get misrouted or
  are not delivered, and keepalives are left running
  (`agents/multi-agent-workflows/playbooks/slice-relay.md:113-145`). A
  program that starts each role as a process and reads its report file has
  none of these failure modes.
- **The relay's rules are copied and drift.** The agents-repo orchestrator
  role was a full v4 copy of the contract until slicer s373.
- **Vendor fallback never worked.** The bash layer cannot classify a rate
  limit (`agents/multi-agent-workflows/README.md:127-130`).

## Users

- **Owner.** Watches the dashboard, picks waves, approves at checkpoints,
  and answers questions from roles.
- **Role agents.** Headless vendor sessions that ork starts with a brief
  and that end by writing a report file.
- **Scripts and agents.** May query or drive the daemon through ork's JSON
  CLI (`cli.json`).

## v1 scope

v1 is the monitoring dashboard plus launching and supervising slice waves
(`scope.v1`). v1.0 includes filing a slice (S170), adding slice notes (S171),
and answering or asking adj records (S172, S185), as decided in
`scope.v1-ork-edits` (#173). Reordering or parking items (S67-S69), the role
matrix editor (S173) and spec drafting (S179) stay later.

**In v1:**
- Dashboard over 4 registered projects: slicer, adjudicate, DataBall and
  agents (`registry.initial`).
- Roadmap and decision views, with slice filing, notes, and asking or answering
  adj records (`scope.v1-ork-edits`).
- Relays with per-role detail: output tail, tokens and cost, elapsed time
  and stale flags, diff stat and tests, and ASCII charts and animations
  (`dash.role-detail`). Relays started outside ork appear view-only
  (`dash.external-runs`).
- Waves:
  - slicer suggests a wave and the owner edits it (`wave.selection`);
  - one project per wave, and several waves may run at once
    (`wave.scope`);
  - a global cap of 4 running slices plus an optional cap per project
    (`wave.limits`);
  - when one slice fails, the others continue (`wave.failure`).
- The engine:
  - ork is the state machine and launches every role (`launch.mode`);
  - all three vendors (`launch.vendor`), with automatic fallback
    (`vendor.fallback`);
  - the lineup comes from slicer's role matrix (`lineup.source`);
  - an architect on size L, split, and slivers (`slice.options`);
  - deterministic checks before review (`checks.deterministic`);
  - a read-only fingerprint on review roles (`role.readonly-fingerprint`).
- Each run ends with a commit on the slice branch after the owner approves
  (`wave.end`, `git.commit`).
- Landing after approval: ork merges the base branch in if it moved,
  re-checks, runs `slicer land`, fast-forwards the base branch, pushes
  when the project's setting says so, and cleans up (`landing.in-ork`,
  #162, supersedes `landing.v1`; S119 implements it). Until then, a
  ready-to-land list and detection of merged branches.
- v1 is releasable when the owner can run a wave, answer its questions,
  approve it and land it, all from ork, with no outside agent session;
  polish found after that goes to v1.1 by default (`release.v1-exit`,
  #167).
- Owner questions in the middle of a slice are answered in ork, with an
  optional adj record (`owner.questions`).
- Soft time and token budgets (`run.budgets`).
- A terminal bell plus a "needs you" badge on each tab (`ui.notify`).
- A background daemon started on demand, the TUI as a client, and a small
  JSON CLI (`daemon.lifetime`, `daemon.start`, `cli.json`).

**After v1:**
- Reordering or parking roadmap items (S67-S69), the role matrix editor
  (S173), and spec drafting (S179).
- Decision editing beyond asking and answering adj records.
- Batched landing and changelog conflict resolution.
- agentbox sandboxing.
- A web front end.

## Goals

1. One dashboard across projects showing roadmaps, decisions and live
   relays with every role's stage and progress.
2. Run waves of slices in parallel across vendors with the owner attending
   at checkpoints, and no outer agent session.
3. User experience first. The TUI is colorful and keyboard-driven, with
   mouse support. The common actions take one keystroke from any screen.
4. One clear owner for each relay step: no step lives in two tools.
5. Small, explicit state. ork owns its registry, run records, role prompts
   and preferences, and reads everything else live.
6. Deterministic before model. Anything a program can check (tests,
   `slicer check`, read-only fingerprint, report shape) is checked by ork
   before a model is asked.
7. After v1: edit roadmaps and manage the decision queue from the
   dashboard, every change made through a slicer or adj CLI call.

## Non-goals

- **No unattended runs.** The owner approves each wave launch and each
  slice's result after the gate. Roles run on their own between those
  checkpoints, on the host, with scoped permissions (`scope.attended`,
  `launch.sandbox`, `launch.permissions`).
- **No scheduler.** ork does not pick waves on its own. It suggests them,
  and the owner launches.
- **No second copy of project state.** Roadmaps, slices, readiness and
  decisions are always read live.
- **No reading or writing `.slicer/` or `decisions/` files directly.**
- **No merges or pushes without approval.** ork commits on slice
  branches, and merges into the base branch or pushes only after the owner
  approves the slice, and pushes only when the project's setting allows it
  (`landing.in-ork`, #162).
- **Not an LLM.** All model work happens in the launched role sessions.
- **One workflow kind: the slice relay** (`workflow.model`).

## Risks

| Risk | Mitigation |
|---|---|
| Three vendors and automatic fallback in v1 widen the surface a lot | A vendor capability spike first (design.md, Vendors); fallback on a rate-limited or error exit; auth, denials and unclassified exits stop for the owner (`vendor.fallback-trigger-v2`, decision 156; `vendor.ratelimit-detection`) |
| ork's Go engine and slicer's prose contract drift | Accept only listed contract versions and refuse others; slicer item for machine-readable contract parts (`boundary.contract`) |
| Embedded role prompts drift from the agents repo's copies | ork is canonical; the agents repo generates from `ork roles print` (`roles.prompts`) |
| Host-run agents with scoped permissions can still do damage | Per-role allowlists, read-only fingerprint, worktree per slice, commit only on slice branches, owner checkpoints |
| Split and slivers add parallelism inside a slice | Build them after single-implementer slices pass end to end (design.md, Build order) |
| The daemon adds IPC and lifecycle work | Unix socket with JSON, started on demand, run records make every restart recoverable |
| Rate-limit output differs by vendor and changes | Every non-ok exit is saved as a fixture candidate; confirmed samples become tests |
| Writes race with roles writing to the same slicer store | slicer reports `locked` (exit 3); ork retries with backoff and shows the conflict |
| Costs run away | Soft budgets pause at role boundaries; tokens and cost on every row |

## Dependencies and licensing

- **Language:** Go. The standard library is the default.
- **Approved for the TUI:** Bubble Tea, Lip Gloss and Bubbles from Charm
  (`stack.go-deps`). The approval is void if a license check of the
  repository fails when code starts.
- **Charts and animations:** in-house (`ui.charts`).
- **Anything else:** asked separately.
- **License:** GPL-3.0-or-later with a NOTICE file (`license.choice`).
  slicer and adj are separate programs run as processes, so their licenses
  do not apply to ork.
