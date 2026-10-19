# Changelog

## Unreleased

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
