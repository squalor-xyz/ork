# Roadmap

## Relay contract: an engine may run the closing note and finish step
key: ork-wfr-finish-contract
short_title: Engine may run the finish step (relay v13)
size: S
findings: ork docs/review-2026-11-12-workflow.md F3: 65 finish steps in five days were model rounds of two commands, median 12 s; one failed on vendor credits (ork S315). Owner decision: ork adj 407 (engine.finish-runner), 2026-11-17.
importance: 2
urgency: 2
effort: 1

*Proposed.* Drafted 2026-11-17 from an ork review. The relay contract says the implementer runs the closing note and the finish step, and its Conformance section says a program meets each obligation in its own code. This item makes the contract say plainly that an orchestrator that is a program may run both itself, with the implementer's stated outcome as the note. The owner decided this for ork on 2026-11-17 (ork adj 407).

### Why

*Proposed.* Evidence:

- ork resumes the implementer for `slicer note` and `slicer done` (`ork/internal/engine/states.go:1225-1262`): 65 such steps from 2026-11-08 to 2026-11-12, median 12 seconds; 51 Claude resumes read 56M tokens.
- On ork S315 the step failed on Codex credits and cost a second gate and another implementer round.
- The contract text: "Only after `VERDICT: PASS`, the implementer runs the closing note and finish step" (`src/slicer/ai.py:518`), and the Conformance obligation "Run the closing note and the finish step only after `VERDICT: PASS`" names no actor.
- The comment above `RELAY_CONTRACT` (`src/slicer/ai.py:254-259`) says to bump the version when a rule a role follows changes, tracking order included. Who runs the tracking steps is such a rule, so this is relay v13.

No open owner question.

### Files

- `src/slicer/ai.py:259`
- `src/slicer/ai.py:518`
- `src/slicer/ai.py:678`
- `tests/test_relay_conformance.py`
- `tests/test_ai.py`
- `tests/test_relay_pin.py`
- `CHANGELOG.md`

### Failing tests

- `test_RelayConformance_FinishStep_AProgramMayRunItWithTheOutcomeLine`: the Conformance section of both texts says a program orchestrator may run the closing note and the finish step itself, takes the note text from the `Outcome:` line of the implementer's last report, and records itself as the actor.
- `test_RelayConformance_FinishStep_AnAgentOrchestratorLeavesItToTheImplementer`: the text keeps the implementer as the runner when the orchestrator is an agent.
- `test_RelayContract_Version_Is13AndChangeIsRecorded`: `RELAY_CONTRACT` is 13 and `RELAY_CHANGES` has an entry for 13 that names the finish step.
- `test_RelayJson_Obligations_CarryTheFinishRunnerRule`: `slicer ai relay --json` carries the rule as data.

### Implement

1. Add the rule to the Conformance section and to the implementer text: with a program orchestrator, the implementer writes `Outcome:` (one line) in its report and runs neither command.
2. Set `RELAY_CONTRACT` to 13 and add the `RELAY_CHANGES` entry.
3. Carry the rule in the `--json` obligations.
4. CHANGELOG.md, as the project's rules say.

### Check

Acceptance criteria:

- `slicer ai relay` and `slicer ai relay-orchestrator` open with `Contract: relay v13`.
- Both texts state that an orchestrator that is a program may run the closing note and the finish step itself, after `VERDICT: PASS`, with the note text from the implementer's `Outcome:` line.
- Both texts still give the two steps to the implementer when the orchestrator is an agent.
- The order (PASS, closing note, finish step, then any commit) is unchanged.
- `slicer ai relay --json` carries the rule, and the change list names v13.
- A project pinned to v12 is refused the v13 text with the existing pin message.

Commands:

- `python3 -m unittest discover -s tests -t tests` exits 0.
- `python3 -m unittest discover -s tests -t tests -k '*FinishStep*'` exits 0 and runs the two new conformance tests.
- `slicer check` exits 0.

### Git

## list --touching PATH: open items whose Files name a path
key: ork-wfr-list-touching
short_title: list --touching PATH
size: S
findings: ork docs/review-2026-11-12-workflow.md F5: reviewers read slicer list --json --lean (85139 bytes for ork) to find the few items a diff affects.
importance: 1
urgency: 1
effort: 1

*Proposed.* Drafted 2026-11-17 from an ork review. A reviewer or an engine that wants the planned items a diff touches reads the whole list and filters it. `plan-parallel` already reads Files paths; this item exposes that as a filter on `list`.

### Why

*Proposed.* Evidence:

- `slicer list --json --lean` printed 85,139 bytes for the ork project on 2026-11-12 (360 items, 68 open).
- ork's reviewer and architect prompts read it to judge direction (`ork/internal/roles/prompts/reviewer.md:42`).
- `plan_parallel` (`src/slicer/queries.py:455`) already reads each item's Files paths.
- ork filters in its own code (ork key `wfr-direction-digest`), so this item is optional for ork. It helps relays that run without ork.

No open owner question.

### Files

- `src/slicer/cli_queue.py:644`
- `src/slicer/cli_parser.py`
- `src/slicer/queries.py:455`
- `tests/test_list_touching.py` (new)
- `src/slicer/ai.py`
- `CHANGELOG.md`

### Failing tests

- `test_ListTouching_PathNamedInFiles_ReturnsOnlyThoseItems`: `list --touching a/b.go --json --lean` returns only the listed items whose Files section names `a/b.go`, with or without a line anchor.
- `test_ListTouching_Directory_MatchesFilesBelowIt`: `--touching a/` matches an item that names `a/b.go`.
- `test_ListTouching_Repeated_ReturnsTheUnion`.
- `test_ListTouching_NoMatch_ReturnsEmptyAndExitsZero`.
- `test_ListTouching_NoFilesSectionConfigured_IsAUsageError`: the message names the missing section, as `plan-parallel` does.

### Implement

1. Add `--touching PATH` (repeatable) to `list`, matching Files paths as `plan_parallel` reads them. It combines with `--status` and `--all` as the other filters do.
2. Name it in the agent guide's inventory line.
3. CHANGELOG.md, as the project's rules say.

### Check

Acceptance criteria:

- The list command with the new touching flag and a PATH lists only items whose Files section names PATH or a file below it; a line anchor in the spec does not prevent a match.
- Several `--touching` flags give the union.
- No match prints an empty list and exits 0.
- It works with `--json --lean` and with the status filters.
- A project with no Files section configured gets a usage error that says so.
- `list` without the flag prints what it prints today.

Commands:

- `python3 -m unittest discover -s tests -t tests` exits 0.
- `python3 -m unittest discover -s tests -t tests -k '*ListTouching*'` exits 0 and runs the five tests.
- `slicer check` exits 0.

### Git
