# Glossary

Each term names the tool that owns it and where it is defined. ork follows
the owner's definition and does not redefine it. Terms in the ork section
are ork's own.

Sources are in the sibling repos under `squalor-xyz/`. For slicer's relay
contract the source is the constant in `slicer/src/slicer/ai.py` that prints
the text (relay v7), since the line numbers move.

## Relay roles

**Relay (slice relay).** One slice taken from spec to a gated diff by an
orchestrator and its roles, under an outer session the owner attends. Owner:
slicer, `slicer ai relay`. Vendor and harness specifics are in
`agents/multi-agent-workflows/playbooks/slice-relay.md`.

**Outer session.** The session the owner talks to. Runs Setup, owns the relay
run, spawns one orchestrator per slice, reads only its run report, files later
notes, and lands branches when the owner authorizes it. Writes no product
code. Owner: slicer, `ai.py` Roles.

**Orchestrator.** Runs one slice under `slicer ai relay-orchestrator`. Briefs
every role, reads reports thin, records steps, commits on the slice branch
when the brief allows it, and returns a run report. Never begins or ends the
relay run, lands, merges, rebases onto main or pushes. Owner: slicer,
`ORCHESTRATOR_RULES`.

**Implementer.** Owns one slice: `slicer start`, red tests, implementation,
mutation checks, closing the slice. Never commits. Owner: slicer, `ai.py`
Roles.

**Lead and part implementers.** In a split, the lead runs the slicer
commands. The parts change only their own files and run no slicer command
that writes. Owner: slicer, `ai.py` Roles.

**Reviewer.** Read-only. Reviews correctness, test strength and direction
against `slicer goals`. Never wrote code in the slice it reviews. Owner:
slicer, `ai.py` Roles.

**Architect.** Read-only. Reviews shape and confirms that each finding was
fixed. Optional. The agents playbook runs it only on size L. Owner: slicer;
size policy in `slice-relay.md`.

**Gate.** The final review of a named diff. Its first line is exactly
`VERDICT: PASS` or `VERDICT: FAIL`. A FAIL goes back to the implementer. Any
change after a review needs another gate. Owner: slicer, `RELAY_GATE`.

**Verdict.** The gate's first line. A failed review is recorded with
`slicer note ID --text "Review FAIL: ..."` (contract v6). Owner: slicer.

**Fix round.** An implementer pass that applies confirmed review findings.
A small fix round is reviewed and gated in one pass (contract v7). Owner:
slicer.

## Lineup and vendors

**Role matrix.** Rows that pick agent, vendor, model and reasoning effort per
role from a slice's size, effort and flags. Stored in
`.slicer/relay-roles.json` and changed only through `slicer relay-roles`.
Owner: slicer.

**Lineup.** The roles for one slice with their chosen rows, from
`slicer relay-roles recommend`. The owner accepts it as a whole or overrides
rows. Accepting it does not by itself authorize paid sessions, commits or
landing. Owner: slicer.

**Vendor.** An agent CLI: `claude`, `codex` or `grok`. Owner: agents repo,
`agents/setup/lib/workflow-vendor.sh`.

**Vendor chain.** The fallback order of vendor, model and effort per role. One
line per entry in `agents/multi-agent-workflows/relay.conf`; line order is the
fallback order. Owner: agents repo. A slicer project's role matrix takes
precedence.

**Native subagent.** A role run on the orchestrator's own harness. Owner:
slicer, `ai.py` Setup.

**Headless session.** A role run on another harness, in the slice worktree,
with the same brief. In the agents repo it is launched by `relay-call.sh`.
Owner: slicer (definition), agents repo (launcher).

**Mailbox.** The `.relay/` directory in a slice worktree where `relay-call.sh`
writes numbered briefs, outputs and `.meta` files. Excluded from git through
`info/exclude`. Owner: agents repo.

**Read-only fingerprint.** `relay-call.sh` hashes the tree before and after a
read-only role. If the tree changed, it prepends `VERDICT: FAIL` and exits 3.
Owner: agents repo.

## Briefs and reports

**Brief.** The prompt a role starts from. It includes the slice from
`slicer show ID --json --lean`, the tracking rule, the workspace limit, the
role's own temp directory and the environment lines. Owner: slicer,
`RELAY_BRIEF`. The orchestrator's brief is printed by `slicer ai relay-brief`.

**Environment lines.** The interpreter, shell limits and scratch directory,
taken from the project's own instructions and carried verbatim in every
brief. Owner: slicer (rule). The project's AGENTS.md (content).

**Tracking rule.** "Do not open, search, parse, or edit tracking files; use
the slicer commands." Owner: slicer, `TRACKING_RULE`.

**Run report.** At most 30 lines in a fixed order. It begins with `Contract:`
and `SLICE ID OUTCOME pass|fail|stopped ...`, and ends with
`nothing left running: yes`. Owner: slicer, `ORCHESTRATOR_RULES`.

**Later notes.** Follow-up findings in a run report. The outer session files
them on the affected items. Owner: slicer.

**Stop.** A relay stops when the same slice fails its gate three times, the
spec is ambiguous or wrong, or an owner decision is needed. A permission
denial on a slicer step also stops the run (v7). Owner: slicer.

## Run tracking

**Relay run.** One recorded run in slicer's ledger. The outer session opens it
with `slicer relay-run begin` and closes it with `relay-run end`. Recording is
monitor-only and launches nothing. Owner: slicer, `slicer/docs/commands.md`.

**Step.** One role round inside a relay run (`relay-run step`), labelled
`ID role`, for example `s279 gate`. Owner: slicer.

**Ledger.** `<git common dir>/slicer/relay-runs.jsonl`. It is shared by all
worktrees of a clone. A run goes stale after 30 minutes with no event. Owner:
slicer.

**Contract version.** The integer on the first line of `slicer ai relay` and
`slicer ai relay-orchestrator` (`Contract: relay v7`). It is bumped only when a
rule changes. The agents-repo orchestrator role carries a matching
`Expects:` line. Owner: slicer, `RELAY_CONTRACT`.

## Queue and slices

**Item.** A queue row with title, status, priority and dependencies. Owner:
slicer.

**Slice.** The spec behind an item: a lead, a scope boundary and named
sections (Why, Files, Failing tests, Implement, Check, Git). "A slice is the
contract someone implements against." Owner: slicer,
`slicer/docs/how-it-works.md`.

**Readiness.** Computed, never stored. It depends on status, dependencies,
required sections, slivers and claims. Owner: slicer.

**Claim.** The marker that an item is being worked, set by `slicer start` and
cleared by `release`, `handoff` or `reject`. Separate from status and
readiness. Owner: slicer.

**Split.** One slice implemented by a lead and parts with disjoint files.
Owner: slicer.

**Sliver.** A small item carved from a slice and linked by `parent`, so that a
cheaper model can implement it. Owner: slicer.

**Wave.** A set of items that can run in parallel, from dependencies and
`Files` paths. Printed by `slicer plan-parallel` (s376, done
2026-10-18). Owner: slicer.

**Handoff.** Moving a started slice to review (`slicer handoff`). One of the
two finish policies (`implement_finish`: `done` or `handoff`). Owner: slicer.

**Land.** Checking a merged branch and marking its slice done
(`slicer land ID --branch B`), which prints the git commands left to run.
slicer never commits, pushes or tags. Owner: slicer,
`slicer/docs/landing.md`.

**Goal / non-goal.** Catalog records that steer review direction. They do not
rank or block anything. Owner: slicer (`slicer goals`).

**Catalog record.** A goal, non-goal, requirement, constraint, decision or
assumption, numbered `C01`.... Owner: slicer (`slicer catalog`).

## Decisions

**Owner.** The person who decides. Only the owner runs `adj decide`,
`reject` or `supersede`. This is a convention, not enforced. Owner:
adjudicate.

**Decision record.** One file per question or answer in `decisions/`, with
status `open`, `decided`, `rejected`, `withdrawn` or `superseded`. Owner:
adjudicate, `adjudicate/PROJECT.md`.

**Key.** A dotted topic on a record, such as `scope.attended`. Owner:
adjudicate.

**Ref.** An opaque string that links a record to something else, such as a
slice ID (`--ref s12`). It is the only link between adjudicate and slicer;
neither tool depends on the other. Owner: adjudicate.

**Inbox.** The open records waiting for the owner (`adj inbox`). Owner:
adjudicate.

## ork

In ork there is no orchestrator agent. The ork daemon plays both the outer
session and the orchestrator, and launches every role itself (`launch.mode`).
The terms below are ork's own. Those backed by an adj decision cite its
key.

**Engine.** The ork daemon's state machine that runs slices: setup,
implement, checks, review, fix round, gate, awaiting approval, commit. See
[design.md](design.md).

**Wave.** In ork, a set of items from one project that the owner launched
together (`wave.scope`). It is suggested from slicer's wave
(`wave.selection`), and its items run under the caps (`wave.limits`).

**Checkpoint.** A point where the engine waits for the owner. There are two:
the wave launch, and a slice's result after the gate. A slice can also pause
for an owner question or a budget limit (`scope.attended`).

**Fallback chain.** The ordered vendor, model and effort entries for a role:
the role matrix's primary row, then its alternatives (`lineup.source`).

**Deterministic checks.** The project check command plus `slicer check`,
run by ork after the implementer and before any review
(`checks.deterministic`).

**Fixture candidate.** The saved raw output of a role exit that was not ok,
kept so that ork's vendor classifier can learn from it
(`vendor.ratelimit-detection`).

**Needs you.** Everything waiting on the owner: approvals, questions,
stops, pauses, unclassified exits. It is counted on each project tab.

**Daemon.** The background ork process that owns waves and role processes.
The TUI and the CLI are its clients (`daemon.lifetime`).

**Project.** A repository registered with ork, which may use slicer, adj or
both (`registry.initial`). See [state.md](state.md).

**Project registry.** `projects.json`: the projects ork shows and their
settings (caps, patterns, check command, budgets, permissions).

**ork run.** One slice that ork set up and runs, in one project. ork's run
record links it to slicer's relay run by id (`ledger.write`).

**Stage.** Where a slice is in the engine's state machine. ork records it
for the processes it started; for external relays it is derived from the
ledger.

**Started elsewhere.** A relay in slicer's ledger that ork did not launch.
ork shows it and never acts on it (`dash.external-runs`).
