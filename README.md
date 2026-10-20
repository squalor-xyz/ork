# ork

A terminal dashboard and engine for parallel slice relays across projects.

## Status

In development: the core registry and CLI adapters (S02), the role
prompts (S10), the TUI shell with project tabs and the Overview (S11), and the
daemon's wiring into the binary (S13) are in. `go run ./cmd/ork` opens the
dashboard over the projects in your registry, through a background daemon that
starts when none runs. It can approve, stop and retry a run from the
Overview. `ork status` and `ork projects` use
the same daemon, and `ork roles print` and `ork doctor` run in the command
itself; `ork --version` prints the version. v1 is the monitoring
dashboard plus launching and supervising slice waves. See [PROJECT.md](PROJECT.md).

## Install

From the repository root, with Go installed:

    go install ./cmd/ork

Go writes `ork` to `$(go env GOBIN)`, or to `$(go env GOPATH)/bin` when
`GOBIN` is unset. `GOPATH` defaults to `~/go`, so no sudo is needed unless you
set `GOBIN` or `GOPATH` elsewhere. Put that directory on your `PATH`. `ork doctor` prints the path of the binary
it runs, which shows which copy you are calling.

## What ork is

One TUI over all your projects (slicer, adjudicate, DataBall, agents):

- See each project's roadmap and decisions, and every slice that is
  running, with each role's stage, live output, tokens, cost and tests.
- Pick a wave of slices (slicer suggests it), approve it, and ork runs
  every slice: worktree, implementer, deterministic checks, review, fix
  rounds and gate, on Claude, Codex or Grok, with vendor fallback.
- Approve a passed slice and ork commits its branch. You land it.

A background daemon keeps waves running when the TUI is closed. ork reads
and writes slicer and adj only through their CLIs.

## Documents

- [PROJECT.md](PROJECT.md): summary, v1 scope, goals, non-goals, risks.
- [docs/design.md](docs/design.md): the engine: wave lifecycle, slice state
  machine, roles, vendors, CLI, build order.
- [docs/dashboard.md](docs/dashboard.md): the screens.
- [docs/state.md](docs/state.md): the files ork owns.
- [docs/boundaries.md](docs/boundaries.md): what slicer, adjudicate, the
  agents repo and ork each own.
- [docs/glossary.md](docs/glossary.md): terms.
- [docs/manual-steps.md](docs/manual-steps.md): the manual relay steps ork
  replaces.
- [docs/review-2026-10-18.md](docs/review-2026-10-18.md): the spec review
  and how each finding was resolved.
- [AGENTS.md](AGENTS.md): rules for agents working in this repo.

Owner decisions are adj records: `adj list --status decided`. The open
questions are in `adj inbox`. The roadmap is in slicer: `slicer list`.

## License

GPL-3.0-or-later. See [LICENSE](LICENSE) and [NOTICE](NOTICE). Copyright
Squalor LLC.
