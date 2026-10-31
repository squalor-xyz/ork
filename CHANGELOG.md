# Changelog

## Unreleased

## v1.0.0 (2026-10-31)

- S209: a project can name `ui_pass_skills` for its Claude UI pass
  (*proposed*, adj 171). `ork projects set NAME --ui-pass-skills a,b`
  replaces the list; an empty value clears it. Only a `claude` UI pass
  gets the brief line advising it to load the named skills. Codex gets no
  skills line, and the neutral role prompt stays unchanged. ork never
  checks skill existence or reads vendor files.
  Docs updated: `docs/state.md`, `docs/design.md`, `docs/guide.md` and
  `docs/glossary.md`.

- S208: the global run cap defaults to 2 busy runs (*proposed*, adj 172).
  An unset or cleared `global_cap` uses 2; an explicit registry value still
  overrides it. A third run queues until a slot frees.
  Docs updated: `PROJECT.md`, `docs/design.md`, `docs/guide.md`,
  `docs/diagrams.md`, `docs/orchestrator-parity.md`, `docs/state.md` and
  `docs/review-2026-10-26-orchestration-trial.md`.

- S24: ork pins and checks a project's slicer contract (*proposed*).
  `ork projects pin NAME --relay N` (N is 10 or 11) runs
  `slicer config relay_contract N --root PROJECT` and reports the pin slicer
  prints; `ork projects unpin NAME` clears it. Neither needs the daemon, and
  ork never writes the pin to `projects.json` (adj 97). `ork doctor` adds
  `contract.projects`: each registered project is asked with its own `--root`,
  and a project fails on `contract_mismatch` or a pin outside the accepted set.
  Engine start refuses a project whose slicer reports `contract_mismatch`,
  with slicer's error naming both versions and no run record. ork now reads
  the contract from `slicer ai relay --json` instead of the first text line.
  The recorded exit code for `contract_mismatch` is 2, not 3. The adj pin is a
  later slice behind adjudicate S45.
  Docs updated: `docs/boundaries.md`, `docs/design.md`, `docs/guide.md`.

- S198: ork accepts slicer relay v11 (*proposed*). The accepted set is v10 and
  v11; v9 is dropped (adj 175). The role prompts are written against v11: the
  gate fails on every `fix` finding (a minor item is `[later]`), and the
  implementer's self-check runs before each review, fix rounds included, and
  reports each check or `n/a`. v11 validation needs the conformance keys
  `role_fallback` and `blocker_wait` and the `gate` entry in `roles[]`; v10
  still needs `vendor_limit`. `stop[]` entries decode as `{key, text}` (v10's
  plain strings still decode). On v11 a gate round closes its ledger step with
  `--phase done --verdict pass|fail`; v10 sends no verdict. The engine's other
  v11 process rules are S204.
  Docs updated: `AGENTS.md`, `docs/boundaries.md`, `docs/design.md`,
  `docs/glossary.md`, `docs/guide.md`, `docs/orchestrator-parity.md`.

- S176: open a role report with `p` in an engine run pane (*proposed*).
  Reports run newest to oldest, then back to the run; `esc` returns at once.
  `run.report` reads only a stored role report name, with regular-file guards,
  terminal controls removed and a 64 KiB bound. Rows carry names only.
  Ledger panes and check logs have no report viewer.

- S177: per-run stop point (*proposed*). `--stop-after commit` on run or wave
  start, or the shared lineup editor's stop row, commits after approval and
  keeps the branch and worktree for the owner to land. The default is `land`.
  Plans confirm the choice; approval prompts, cards and status name it.
  Ready rows keep the slicer land commands; there is no land key or registry
  default.

- S181: wave start and the spec check read slicer's dependency state before
  running roles or admitting any member. A blocked item refuses the whole
  call, naming its blockers, including dependencies selected in the same
  wave. Refusals include blocker titles and a short next step naming what to
  untick or land first. Titles are read once per distinct blocker per call;
  a failed title read falls back to the id. Failed dependency reads still
  prevent launch. Waves keeps the ticks and shows the refusal; the CLI
  refuses before confirmation.

- S175: end a stale ledger run from its pane (*proposed*). On the pane of a
  ledger run that slicer calls stale (no event for 30 minutes), `x` asks
  `slicer relay-run end RUN`, `--outcome stopped --close-steps fail` and
  `End this run as stopped and fail its unfinished steps? [y/N]`, and `y`
  ends the run through the new daemon method `run.end-ledger`. The daemon
  reads the ledger again and refuses a run that is no longer stale, and a
  ledger run that an unfinished engine run owns. Active, waiting and ended
  ledger panes stay read-only. There is no `ork end` shell command.

- S141: approval card follow-ups (*proposed*). `ork check RUN N [ticked|later|open]`
  and `ork fix RUN TEXT` call `run.check` and `run.fix`, so `ork approve` works
  from the shell on a slice with human checks. When the card could not read the
  checks, approval writes a slicer note that says so. The owner's fix note goes to
  every gate round until a gate passes it (a red check or a gate FAIL no longer
  drops it). A note written before a failed save is not written twice. A
  refused owner action no longer leaves an `error:` marker on the run row.
  In the TUI, checks 10 and above are marked by typing the number (digits
  wait for the next digit or enter when the number could be longer); new files
  align with the diff stat; a blank line sets the card's blocks apart above 24
  rows (never taking room the body needs); and the landing line goes before an
  owner finding on a short pane (the push line stays above it). A waiting
  number shows in the hint line, and numbers of 10 or more checks line up.
  Refusals have a new wire code, `refused`: the CLI and the TUI show the text
  alone.

- S139: an empty or missing slice Git section falls back to `ID: slice title`
  from `slicer show` for the commit message. Explicit Git titles keep their
  precedence. There is no pre-launch refusal; retrying a commit pause resumes
  at commit without repeating the finish step.

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
