# Changelog

## Unreleased

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
