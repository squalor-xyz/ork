# Changelog

## Unreleased

- S121: renders on the approval card (adj #165, *proposed*). The UI pass
  writes the changed views as text files at 80x24, 120x40 and in ASCII mode in
  `tmp/ui-N/renders/` of the run directory, with the project's own tooling;
  ork has no render code. The card keeps them (plain text without escape
  sequences, at most 24 files of 24 KiB, links not followed, each view's
  80x24, 120x40 and ascii file in that order) and `v view renders` shows them
  in the pane, which then takes the whole body (the Overview rows collapse).
  The card and snapshots hold only each render's name and size; the pane reads
  the text on demand through a new `run.render` call. `run.json` gains
  `card.ui_round`, `card.renders` (name, size) and `card.renders_dropped`.

- S120: finding kinds (adj #163). The reviewer, the UI pass and the gate tag
  each finding `[fix]`, `[owner]` (`[owner blocker]` to block the gate) or
  `[later]`; an untagged finding is a fix. Only fix findings go to the fix
  round. An owner finding is a structured question: it blocks only when
  marked, and otherwise shows in the run pane. A later finding is offered in
  the pane, and `f` then `y` makes ork run `slicer add TITLE
  --discovered-from ID` and `slicer promote` (a Why section from the finding)
  in the project. `run.json` gains `findings[]`; the daemon gains `run.file`.

- S96: first run opens a blank Welcome view. Projects are never seeded.
  The TUI add confirms the exact command, then registers and fetches the
  project. The registry is created on the first add. Empty CLI status shows
  the registration guide; `projects add` without PATH offers the current
  directory in text mode.

- S13: the binary joins the parts. `ork` opens the TUI over the daemon's
  socket and starts the daemon when none answers; `ork status`, `projects`,
  `roles`, `doctor` and `help` go through `internal/cli`; `ork daemon run`
  serves the socket. The start runs `ork daemon run` detached, in its own
  session with its stdio on /dev/null. The TUI's `r` asks the daemon to read
  the project now, and the TUI tells the daemon which project it shows
  (`daemon.Feed` keeps the notice connection apart from the requests). A
  daemon start that races an exiting daemon's lock retries it for two
  seconds.

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
