# AGENTS.md

## Scope

These instructions apply to all work in this repository unless a nested
`AGENTS.md` gives more specific guidance. Keep durable project conventions
here. Update this file when the same mistake or review note repeats.

ork is in development; code lands one tracked v1 slice at a time.
Approved dependencies are Bubble Tea, Lip Gloss and Bubbles only (`adj check "stack.go-deps"`);
verify each one's license from its repository before adding it, and ask
before any other dependency. Every new Go file starts with
`// SPDX-License-Identifier: GPL-3.0-or-later`.

## Source of truth

- [PROJECT.md](PROJECT.md): what ork is, goals, non-goals, owner decisions.
- [docs/glossary.md](docs/glossary.md): terms. Use them as defined there, and
  defer to the owning tool's definition.
- [docs/boundaries.md](docs/boundaries.md): what ork may and may not own.
- [docs/design.md](docs/design.md): the engine.
- [docs/dashboard.md](docs/dashboard.md) and [docs/state.md](docs/state.md):
  the UI and the files ork owns.
- Decided adj records override these documents. When a record is decided,
  update the documents to match it.

## Working Rules

- Read the relevant files before editing.
- Keep changes scoped to the requested outcome. No unrequested features or
  refactors.
- Do not guess project details. State only what files, tool output or the
  owner's words support, and cite sibling-repo sources as `repo/path:line`.
- Ask before adding any dependency. Go standard library first.
- Ask before destructive actions, external side effects, or access outside
  this workspace. The sibling repos `../slicer`, `../adjudicate` and
  `../agents` may be read for citations, not written.
- Do not change directory. Use absolute paths and `git -C`.

## Discovery

- Use `rg` and `rg --files`.
- Read small, targeted sections of the sibling repos, not whole files.

## Editing

- Short plain sentences, no emoji, ASCII by default.
- Mark anything ork itself would add as *proposed* or *candidate* until an adj
  record decides it.
- In a slice's Files section, put each path in backticks, for example
  `internal/tui/model.go`, so slicer can read it. Inside the backticks, allow
  only a `:N` or `:N-M` line anchor; put any other anchor after the backtick.

## Environment

- Shell: zsh on darwin. `timeout` is not installed on macOS; do not use it.
- Worktrees live under this directory, in `.worktrees/<name>` (one per
  slice, branch `<name>`). `.worktrees/` is git-excluded.
- Scratch and temporary files (scripts, configs, previews, build output that
  is not part of the repo) go in `.scratch/` in the main checkout. It is
  git-excluded (`.git/info/exclude`). Do not use `/tmp` or the harness's
  scratch directory, so every path stays inside the workspace.
- Go 1.27.2. Use `go -C DIR` instead of changing directory.

## Tracking (slicer)

Work is tracked in slicer. Run `slicer ai instructions` for the agent guide.
Do not open, search, parse, or edit tracking files; use the slicer commands.

## Decisions (adj)

This repo logs decisions with `adj`. Agents ask, check and read. Only the
owner decides, so never run `adj reject` or `adj supersede`, and run
`adj decide` only to record an option the owner explicitly picked in an
interactive question that named it (`process.owner-answers`, #170): use
`--by` with the owner's name and a `--why` that cites the session and the
date. Anything the owner did not explicitly pick stays an open ask.

- Before asking, check: `adj check "storage.engine"`. Line 1 of the output is
  `decided`, `open`, `closed` or `not found`, and the exit code is 0 for all
  four. If `decided`, follow it. If `open`, do not ask again. If `closed`,
  read its Rationale (`adj show ID`) and do not re-ask a rejected question. If
  `not found`, ask.
- Ask with `adj ask`, a dotted `area.topic` key, options and a
  recommendation. Link slices with `--ref`.
- Run `adj ai` for the full snippet.

## Project Commands

- Install dependencies: `go mod download` (Bubble Tea, Lip Gloss and Bubbles
  only, since S11).
- Rewrite TUI golden views after an intended layout change:
  `go test ./internal/tui -update`, then review the diff in `testdata/golden/`.
- Run tests: `go test ./...`, plus `go vet ./...` and `gofmt -l .`.
- Check tracking: `slicer check`.
- Check decisions: `adj lint`.
- Open questions: `adj inbox`.

## Project Rules

- ork implements slicer's relay contract and accepts only the versions it
  lists (v7 and v8 on 2026-10-23; `boundary.contract`). Do not paste the contract's text into docs; refer to it by
  command (`slicer ai relay`). Role prompts are ork's own text
  (`roles.prompts`).
- Never read vendor credentials or login files; use each vendor CLI's own
  login.
- Before specifying an ork capability, check that slicer does not already
  provide it or have it on its roadmap
  (`slicer --root ../slicer find TEXT`).
- In a slice relay or wave, a slice that touches the UI (`internal/tui`
  and its golden views, `docs/dashboard.md`, `internal/cli` output, or a
  `ui.*` adj key) also gets a UI pass: matrix row r14 (opus, medium), rule
  u10. It runs as a general-purpose subagent in parallel with the reviewer,
  invokes the `frontend-design:frontend-design` skill, edits nothing, and
  returns suggested design edits to the orchestrator for the implementer.

## Handoff

Summarize what changed, list the checks run, and call out remaining risk.
