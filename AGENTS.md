# AGENTS.md

## Scope

These instructions apply to all work in this repository unless a nested
`AGENTS.md` gives more specific guidance. Keep durable project conventions
here. Update this file when the same mistake or review note repeats.

ork is spec only: there is no code. Do not write implementation code or add
a `go.mod` until the owner starts the first v1 slice. Approved dependencies
are Bubble Tea, Lip Gloss and Bubbles only (`adj check "stack.go-deps"`);
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

## Environment

- Shell: zsh on darwin. `timeout` is not installed on macOS; do not use it.
- Scratch files go in the session's scratch directory, never `/tmp` by
  default.
- No Go toolchain commands until code exists.

## Tracking (slicer)

Work is tracked in slicer. Run `slicer ai instructions` for the agent guide.
Do not open, search, parse, or edit tracking files; use the slicer commands.

## Decisions (adj)

This repo logs decisions with `adj`. Agents ask, check and read. Only the
owner decides, so never run `adj decide`, `adj reject` or `adj supersede`.

- Before asking, check: `adj check "storage.engine"`. Line 1 of the output is
  `decided`, `open`, `closed` or `not found`, and the exit code is 0 for all
  four. If `decided`, follow it. If `open`, do not ask again. If `closed`,
  read its Rationale (`adj show ID`) and do not re-ask a rejected question. If
  `not found`, ask.
- Ask with `adj ask`, a dotted `area.topic` key, options and a
  recommendation. Link slices with `--ref`.
- Run `adj ai` for the full snippet.

## Project Commands

There is no application build.

- Install dependencies: none.
- Run tests: none.
- Check tracking: `slicer check`.
- Check decisions: `adj lint`.
- Open questions: `adj inbox`.

## Project Rules

- ork implements slicer's relay contract and pins its version (v7 on
  2026-10-18). Do not paste the contract's text into docs; refer to it by
  command (`slicer ai relay`). Role prompts are ork's own text
  (`roles.prompts`).
- Never read vendor credentials or login files; use each vendor CLI's own
  login.
- Before specifying an ork capability, check that slicer does not already
  provide it or have it on its roadmap
  (`slicer --root ../slicer find TEXT`).

## Handoff

Summarize what changed, list the checks run, and call out remaining risk.
