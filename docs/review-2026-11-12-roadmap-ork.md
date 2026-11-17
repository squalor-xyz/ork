# Roadmap

## Usage: record a step's own cost and tokens, not the session total
key: wfr-usage-deltas
short_title: Per-step cost and tokens
size: S
tree: daemon, agent
release_target: v1.2
findings: docs/review-2026-11-12-workflow.md F1: Claude cost monotonic in 63 of 63 resumed sequences, Codex tokens in 20 of 20; $542 recorded is about $194.
importance: 3
urgency: 2
effort: 1

*Proposed.* Drafted 2026-11-12, anchors checked at f48a6154 on 2026-11-17. A resumed Claude session reports its running cost, and a resumed Codex session its running tokens. ork stores each as the step's own value and adds the steps up, so run totals and token budgets are too high.

**Not in this slice:** rewriting old run records; a cost for Codex or Grok; `ork stats` output.

### Why

*Proposed.* Evidence read on 2026-11-12 from `ork status --json` (75 runs):

- Claude step costs never decrease inside a run (63 of 63 sequences). S298: $4.75, $6.40, $6.65; the third step ran 10.6 seconds.
- Codex step tokens never decrease inside a run (20 of 20). S315: 7.5M, 13.8M, 14.6M; the third step ran 20 seconds.
- Claude tokens and Grok tokens are per step (3 of 63 and 2 of 12 sequences are monotonic).
- `overBudget` (`internal/engine/round.go:285`) reads the recorded role usage, so a Codex run reaches its token budget early.

This is inferred from run records. The implementer confirms it with fixtures first. If a vendor's value turns out to be per step, that vendor is left unchanged and the slice notes say so.

No open owner question.

### Files

- `internal/agent/claude/stream.go:109`
- `internal/agent/codex/codex.go:189`
- `internal/daemon/runner.go:149`
- `internal/engine/round.go:671`
- `internal/engine/round.go:746`
- `internal/engine/watchdog.go:196`
- `internal/runrec/record.go`
- `internal/daemon/runs.go:2764`
- `docs/state.md`
- `CHANGELOG.md`

### Failing tests

- `TestResumedClaudeStepRecordsItsOwnCost`: two results of one session with `total_cost_usd` 1.00 then 1.50 record step costs 1.00 and 0.50.
- `TestResumedCodexStepRecordsItsOwnTokens`: two turns of one session with totals 100 then 250 record step tokens 100 and 150.
- `TestFreshSessionAfterFallbackStartsFromZero`: a new session id after a fallback records its full value.
- `TestRunCostSumsStepCosts`: the run total is 1.50, not 2.50.
- `TestBudgetCountsStepTokens`: a Codex run with session totals 100 then 250 and a token budget of 300 is not over budget.
- `TestOldRecordIsReadUnchanged`: a record written before this slice loads with the values it holds.

### Implement

1. Capture two resumed invocations per vendor (Claude, Codex) as fixtures under `testdata/vendors/`, with prompt and report text and any credential field removed. Record in the slice notes whether each value is a session total.
2. Keep the session's last recorded total on the role record, keyed by session id (*proposed* fields; `docs/state.md`).
3. Record the step value as the total minus the session's last total; a new session id starts from zero; a total lower than the last one is taken whole.
4. Leave old run records as they are.
5. CHANGELOG.md: one bullet under `## Unreleased`.

### Check

Acceptance criteria:

- A run whose Claude implementer was resumed twice shows a run cost equal to the session's last `total_cost_usd`, not the sum of the three values.
- A run whose Codex implementer was resumed shows run tokens equal to the session's last total.
- A fallback to another vendor or a fresh session adds that session's full value.
- A token budget pauses a run only when the step values add up to it.
- Run records written by an older build still load and display.
- The slice notes state, per vendor, what the fixture showed.

Commands:

- `go vet ./... && go test ./...` exits 0.
- `go test -count=1 ./internal/engine ./internal/daemon ./internal/agent/... -run 'Resumed|FreshSession|RunCostSums|BudgetCountsStep|OldRecordIsRead' -v` exits 0 and lists the six tests.
- `gofmt -l .` prints nothing.
- `rg -n 'credential' testdata/vendors` prints no new line.
- `slicer check --root /home/jon/code/squalor-xyz/ork` exits 0.

### Git

## Fix round: a section that opens with nothing is empty
key: wfr-nothing-prose
short_title: Nothing-plus-prose is no finding
size: S
tree: daemon
release_target: v1.2
findings: docs/review-2026-11-12-workflow.md F2: 29 of 65 landed runs ran a fix round with no [fix] tag; at least 12 look like the parser. Decided: adj 406 (review.nothing-section).
importance: 3
urgency: 3
effort: 1

*Proposed.* Drafted 2026-11-12, anchors checked at f48a6154 on 2026-11-17. A reviewer section that says "nothing" and then explains what was traced counts as a fix finding, so the run gets a fix round with nothing to fix. Owner decision 2026-11-17 (adj 406, `review.nothing-section`): a section whose first line says nothing or none is empty; later prose is evidence, not a finding; an untagged finding is still a fix finding.

**Not in this slice:** gate reports (`triageGate`); requiring tags on every finding; section (C), which already counts only tagged findings.

### Why

*Proposed.* Evidence read on 2026-11-12:

- `triage` adds any untagged (A), (B) or (D) section with content to the fix findings (`internal/engine/report.go:530`). `saysNothing` accepts only a body that is the one word (`internal/engine/report.go:661`).
- `review` sends a whole untagged report to a fix round (`internal/engine/states.go:681`).
- 7 reviewer-1 reports open every one of (A), (B) and (D) with "nothing" and still got a fix round (S103, S129, S236, S259, S291, S292, S293). 6 round-2 implementer reports say nothing was changed.

### Files

- `internal/engine/report.go:506-570`
- `internal/engine/report.go:636-664`
- `internal/engine/report_test.go`
- `internal/engine/states.go:681`
- `internal/engine/engine_test.go`
- `internal/roles/prompts/reviewer.md`
- `internal/roles/prompts/ui.md`
- `docs/design.md`
- `CHANGELOG.md`

### Failing tests

- `TestTriageNothingThenProseIsEmpty`: sections (A), (B) and (D) each "nothing" plus a paragraph give no fix finding and no shape error. Cases: `nothing`, `Nothing.`, `**none**`, and the word on the heading line.
- `TestTriageNothingThenTaggedFindingKeepsTheFinding`: a section that opens with nothing and later holds a `[fix]` line keeps that finding.
- `TestTriageUntaggedFindingIsStillFix`: an untagged section that does not open with nothing is a fix finding, as today.
- `TestTriageNothingAsFirstWordOfASentenceIsAFinding`: "Nothing checks the error path." is content, not an empty section.
- `TestReviewSkipsFixRoundOnNothingProse`: the run goes from review to the gate with no implementer round.
- `TestSavedReportsTriage`: the fixtures from step 1 give the expected fix or no-fix result.

### Implement

1. Read the reviewer-1 and ui-1 reports of the runs named in Why with `ork show RUN NAME` and save 6 as fixtures under `internal/engine/testdata/` (3 with nothing-plus-prose, 3 with real untagged findings), report text only.
2. In `content`, a body whose first non-blank line, on its own, says nothing or none is empty. A tagged finding later in the section still counts.
3. Prompts: say that evidence after "nothing" is allowed and is not a finding, and that a finding must not follow it.
4. Docs (*proposed*): `docs/design.md` review section cites adj 406. CHANGELOG.md: one bullet.

### Check

Acceptance criteria:

- A reviewer or UI report whose (A), (B) and (D) sections each open with a line that says nothing or none, with any prose after it and no tag, starts no fix round; the run goes to the gate.
- A `[fix]` finding anywhere in such a section still starts a fix round with that finding.
- An untagged section that does not open with nothing still starts a fix round, as today.
- A sentence that only begins with the word nothing is still a finding.
- A blank section is still a shape error.
- The reviewer and UI prompts say that evidence may follow "nothing".

Commands:

- `go vet ./... && go test ./...` exits 0.
- `go test -count=1 ./internal/engine -run 'TriageNothing|TriageUntagged|ReviewSkipsFixRound|SavedReportsTriage' -v` exits 0 and lists the six tests.
- `gofmt -l .` prints nothing.
- `slicer check --root /home/jon/code/squalor-xyz/ork` and `adj lint` exit 0.

### Git

## Cloud Claude roles: a smaller fixed context
key: wfr-cloud-fixed-context
short_title: Smaller fixed context for cloud Claude roles
size: S
tree: agent
release_target: v1.2
discovered_from: S360
findings: docs/review-2026-11-12-workflow.md F4: S360 measured 30211 to 26692 first-turn tokens (12%), claude-local only; Claude ran 245 role steps in five days. Decided: adj 408 (roles.cloud-fixed-context).
importance: 2
urgency: 2
effort: 1

*Proposed.* Drafted 2026-11-12, anchors checked at f48a6154 on 2026-11-17. S360 turned off six unused tools and the slash commands for local roles. Cloud Claude roles read the same unused context on every turn. Owner decision 2026-11-17 (adj 408, `roles.cloud-fixed-context`): cloud Claude roles get the tool list and the slash-command switch only; the UI pass keeps Skill.

**Not in this slice:** the Bash output cap for cloud roles (the owner chose the tool list only); instruction files and MCP servers, which are measured here and changed nowhere; Codex and Grok roles.

### Why

*Proposed.* Evidence:

- The S360 note of 2026-11-12: 30,211 first-turn input tokens before, 26,692 after, on cloud haiku.
- `internal/agent/claude/args.go:85` and `internal/agent/claude/args.go:136` branch on `claude-local`. `TestArgsCloudRoleUnchanged` (`internal/agent/claude/args_test.go:145`) pins the cloud arguments.
- What fills the other 26.7k tokens is not measured.

### Files

- `internal/agent/claude/args.go:85`
- `internal/agent/claude/args.go:136`
- `internal/agent/claude/args_test.go:145`
- `internal/daemon/runner_test.go`
- `docs/design.md`
- `CHANGELOG.md`

### Failing tests

- `TestArgsCloudRoleTurnsOffUnusedTools`: a cloud write role's args hold `--disallowedTools` with WebFetch, WebSearch, Task, Agent, NotebookEdit and Skill, and `--disable-slash-commands`.
- `TestArgsCloudReadOnlyRoleTurnsOffUnusedTools`: the same for a cloud reviewer or gate, with its read-only arguments otherwise unchanged.
- `TestArgsCloudUIPassKeepsSkill`: a cloud UI pass keeps Skill and has no `--disable-slash-commands`.
- `TestClaudeCloudEnvHasNoBashCap`: a cloud role's environment does not set `BASH_MAX_OUTPUT_LENGTH`.
- `TestArgsCloudRoleUnchanged` is replaced by the tests above.

### Implement

1. Check the tool names against `claude --help` and the stream's `init` event before listing them.
2. Apply the local tool list and slash-command switch to cloud Claude roles, tests first. Keep the local-only parts (own prompt file, read-only allowlist) local.
3. Measure a cloud role's first-turn input tokens (prompt "Reply with one word: ok", from the ork checkout) before and after, and with the user instruction file, the project instruction file and MCP servers off one at a time. Record each number in the slice notes. Change nothing for them; file what is large as a later note.
4. Docs (*proposed*): `docs/design.md` cites adj 408. CHANGELOG.md: one bullet.

### Check

Acceptance criteria:

- Every cloud Claude role except the UI pass starts with `--disallowedTools WebFetch,WebSearch,Task,Agent,NotebookEdit,Skill` and `--disable-slash-commands`.
- A cloud UI pass keeps Skill and the slash commands, so its design skills still load.
- A cloud role's Bash output is not capped.
- claude-local arguments do not change.
- The slice notes hold the first-turn token count before and after, and the count with each of the three other sources off.

Commands:

- `go vet ./... && go test ./...` exits 0.
- `go test -count=1 ./internal/agent/claude ./internal/daemon -run 'ArgsCloud|ArgsLocal|ClaudeCloudEnv' -v` exits 0 and lists the new tests.
- `gofmt -l .` prints nothing.
- `slicer check --root /home/jon/code/squalor-xyz/ork` and `adj lint` exit 0.
- [HUMAN CHECK] Run one size S slice with a Claude implementer and a UI pass: the implementer finishes without asking for a turned-off tool, and the UI pass report names the design skills it loaded.

### Git

## Reviewer and architect: a direction digest in the brief
key: wfr-direction-digest
short_title: Direction digest in the brief
size: M
tree: daemon
release_target: v1.2
findings: docs/review-2026-11-12-workflow.md F5: slicer list --json --lean is 85139 bytes for ork; section (C) gave 1 fix finding in 55 reports; the reviewer is 153M tokens and the slowest step. Grok review ends 2026-12-09, after which this read is billed.
importance: 2
urgency: 3
effort: 2

*Proposed.* Drafted 2026-11-12, anchors checked at f48a6154 on 2026-11-17. The reviewer and the architect read the whole queue to judge direction. ork can read it once and hand them the part that matters.

**Not in this slice:** the gate and implementer briefs; any change to slicer (`list --touching` is a separate slicer item and is not needed here).

### Why

*Proposed.* Evidence:

- `internal/roles/prompts/reviewer.md:42` and `internal/roles/prompts/architect.md:40` name `slicer list --json --lean`.
- The list output was 85 KB on 2026-11-12 and grows with the roadmap. It stays in the role's context for every later turn.
- Section (C) Direction: 1 fix and 36 later findings in 55 reports.

No open owner question. Role prompts are ork's own text (`roles.prompts`).

### Files

- `internal/engine/brief.go`
- `internal/engine/brief_test.go`
- `internal/slicercli/slicercli.go`
- `internal/roles/prompts/reviewer.md:42`
- `internal/roles/prompts/architect.md:40`
- `internal/roles/roles_test.go`
- `docs/design.md`
- `CHANGELOG.md`

### Failing tests

- `TestDirectionDigestNamesOverlappingItems`: an open item whose Files paths overlap the diff's paths is in the digest with id, title, status and dependencies.
- `TestDirectionDigestListsTheRestByTitle`: other open items appear as id and title only; done and retired items do not appear.
- `TestDirectionDigestHoldsTheGoals`: each goal and non-goal title is in the digest.
- `TestDirectionDigestIsBounded`: 300 open items give a digest under 8,000 bytes, with a line that says how many were left out.
- `TestDirectionDigestFailureKeepsTheOldRead`: when the list call fails, the brief has no digest and tells the role to run the two slicer commands.
- `TestReviewerPromptNamesTheDigest` and `TestArchitectPromptNamesTheDigest`: the prompt names the digest and no longer requires `slicer list --json --lean`.

### Implement

1. Build the digest in Go from `slicer goals --json --lean`, the open list and the diff's paths (`git diff --name-only` against the base branch). An item overlaps when a path in its Files section equals a changed path or is its directory.
2. Add it as a brief section, "Direction", for the reviewer and the architect.
3. Prompts: read the Direction section; run `slicer show ID --json --lean` for an item that needs detail.
4. Docs (*proposed*): `docs/design.md`. CHANGELOG.md: one bullet.

### Check

Acceptance criteria:

- The reviewer and architect briefs carry a Direction section with the goals, the open items that touch the diff's files (id, title, status, dependencies) and the other open items by id and title.
- The section is at most 8,000 bytes whatever the roadmap's size, and says how many items it left out.
- Neither prompt tells the role to read the whole queue; both say how to read one item.
- When ork cannot build the digest, the role is told to read the queue itself, and the run does not pause.
- Section (C) of the report keeps its shape.

Commands:

- `go vet ./... && go test ./...` exits 0.
- `go test -count=1 ./internal/engine ./internal/roles -run 'DirectionDigest|PromptNamesTheDigest' -v` exits 0 and lists the seven tests.
- `gofmt -l .` prints nothing.
- `slicer check --root /home/jon/code/squalor-xyz/ork` exits 0.

### Git

## Finish step without a model round
key: wfr-finish-in-ork
short_title: ork runs the finish step
size: M
tree: daemon
release_target: v1.2
findings: docs/review-2026-11-12-workflow.md F3: 65 finish steps, median 12 s, 56M Claude tokens; S315's finish step failed on Codex credits and cost a gate and an implementer round. Decided: adj 407 (engine.finish-runner).
importance: 2
urgency: 2
effort: 2

*Proposed.* Drafted 2026-11-12, anchors checked at f48a6154 on 2026-11-17. After the gate passes, ork resumes the implementer to run `slicer note` and `slicer done`. Owner decision 2026-11-17 (adj 407, `engine.finish-runner`): ork runs both commands, with the note text from an `Outcome:` line in the implementer's last report. adj 81 (`engine.finish-order`) keeps the order: finish, then commit.

This item waits on slicer's item "Relay contract: an engine may run the closing note and finish step" (key `ork-wfr-finish-contract` in the slicer project), which adds the rule in relay v13.

**Not in this slice:** split parts and slivers; the landing steps after the commit; any change for a project on relay v11 or v12.

### Why

*Proposed.* Evidence:

- `finish` builds a brief of two commands and runs a role round (`internal/engine/states.go:1225-1262`).
- 65 finish steps in five days, median 12 seconds; each Claude resume reads the session again (51 steps, 56M tokens).
- The step can fail as any role can (S315).
- The contract's Conformance section says a program meets each obligation in its own code, and other parts say the implementer runs the closing note and finish step (`slicer ai relay`). v13 settles which holds for an engine.

### Files

- `internal/engine/states.go:1225-1262`
- `internal/engine/report.go`
- `internal/engine/report_test.go`
- `internal/engine/engine_test.go`
- `internal/engine/contract_v12_test.go`
- `internal/slicercli/contract.go`
- `internal/slicercli/write.go`
- `internal/roles/prompts/implementer.md`
- `docs/design.md`
- `docs/boundaries.md`
- `AGENTS.md`
- `CHANGELOG.md`

### Failing tests

- `TestContractV13IsAccepted`: a project on relay v13 passes the contract check; v11 and v12 still pass.
- `TestFinishRunsSlicerWithoutARole`: on v13, after approval the fake slicer sees the note and the done call, in that order, before the commit, and the runner sees no round.
- `TestFinishUsesTheOutcomeLine`: the note text is the `Outcome:` line of the last accepted implementer report.
- `TestImplementerReportNeedsOutcomeOnV13`: a v13 implementer report with no `Outcome:` line is a shape error and gets the usual shape retry.
- `TestFinishHandoffMode`: `implement_finish` handoff runs `slicer handoff`.
- `TestFinishSlicerFailurePauses`: a failed slicer call pauses the run with slicer's message, and `ork retry` runs the call again.
- `TestFinishOnOlderContractResumesTheImplementer`: a project pinned to v12 behaves as today.

### Implement

1. Start only after the slicer item has landed and `slicer ai relay` prints `Contract: relay v13`. Add v13 to the accepted set (`boundary.contract-set`, adj 326) with its contract test, as v12 was added.
2. Report shape on v13: an implementer report carries `Outcome:` (one line, the verified outcome). `CheckReport` checks it.
3. In `finish`, on v13, run `slicer note` and then `slicer done` or `slicer handoff` through `slicercli`, in the worktree, with the run's owner name. A failed call pauses.
4. Keep the role path for v11 and v12.
5. Implementer prompt: write the `Outcome:` line; do not run the closing note or the finish step on v13.
6. Docs (*proposed*): `docs/design.md` finish step cites adj 407; `docs/boundaries.md` names ork as the runner of the step; `AGENTS.md` lists v13. CHANGELOG.md: one bullet.

### Check

Acceptance criteria:

- On a relay v13 project, a run that passes its gate and is approved writes the closing note and runs the finish step with no implementer round; the run's role list ends at the gate.
- The closing note's text is the `Outcome:` line of the implementer's last report.
- The note and the finish step still come before ork's commit (adj 81).
- A v13 implementer report without `Outcome:` is retried for shape, not accepted.
- A slicer failure in the finish step pauses the run with slicer's message and resumes with `ork retry`.
- A project on relay v11 or v12 runs the finish step through the implementer, as today.
- ork accepts relay v11, v12 and v13.

Commands:

- `go vet ./... && go test ./...` exits 0.
- `go test -count=1 ./internal/engine ./internal/slicercli -run 'Finish|ContractV13|NeedsOutcome' -v` exits 0 and lists the seven tests.
- `slicer ai relay --root /home/jon/code/squalor-xyz/ork` prints `Contract: relay v13` on its first line.
- `gofmt -l .` prints nothing.
- `slicer check --root /home/jon/code/squalor-xyz/ork` and `adj lint` exit 0.

### Git

## Docs citations by symbol, checked by a test
key: wfr-doc-cites
short_title: Docs cite symbols, not lines
size: M
tree: docs
release_target: v1.3
findings: docs/review-2026-11-12-workflow.md F6: (D) Doc accuracy is 33 of 79 reviewer fix findings; 5 of 14 sampled are moved citations. Decided: adj 409 (docs.citation-form).
importance: 2
urgency: 1
effort: 2

*Proposed.* Drafted 2026-11-12, anchors checked at f48a6154 on 2026-11-17. The guide and the design cite code by line. Lines move with every slice, the test only checks that the line exists, and the reviewer finds the stale ones by reading. Owner decision 2026-11-17 (adj 409, `docs.citation-form`): the two docs cite a path and a symbol with no line number, checked by a test.

**Not in this slice:** line anchors in slice specs, which slicer's `refs` command owns; the other files in `docs/`; citations of `.md` files.

### Why

*Proposed.* Evidence:

- `docs/guide.md` and `docs/design.md` hold 66 backticked `path:line` citations at f48a6154.
- Reviewer findings on moved citations: S132, S215 and three on S219.
- `TestGuideAndDesignCitationsResolve` (`internal/docscheck/cite_test.go:79`) fails on a missing file or a line outside the file, and nothing else.

### Files

- `internal/docscheck/cite_test.go`
- `docs/guide.md`
- `docs/design.md`
- `AGENTS.md`
- `CHANGELOG.md`

### Failing tests

- `TestSymbolCitationParser`: `internal/cli/wave.go` followed by a backticked symbol in the same parentheses or sentence parses as one citation.
- `TestSymbolCitationResolves`: it passes when the file contains the symbol as a whole word.
- `TestSymbolCitationFailsWhenTheSymbolIsGone`: it fails and names the doc file and line.
- `TestGuideAndDesignHaveNoLineCitationsOfGoFiles`: a `path.go:N` citation in either doc fails and names the line.
- `TestGuideAndDesignSymbolCitationsResolve`: every symbol citation in the two docs resolves.

### Implement

1. Fix the written form first and record it in the slice notes, for example (`internal/cli/wave.go`, `startWave`). The implementer picks the form that needs the fewest prose changes and keeps it uniform.
2. Extend the parser and the tests.
3. Convert the 66 citations. A citation with no clear symbol (a constant block, a table) cites a string that is unique in the file, or the path only.
4. `AGENTS.md` Editing: one line on the citation form, citing adj 409. CHANGELOG.md: one bullet.

### Check

Acceptance criteria:

- `docs/guide.md` and `docs/design.md` hold no `path.go:N` citation.
- Each code citation in the two docs names a path and a symbol or unique string, and `go test` fails when that symbol is no longer in the file, naming the doc line.
- A missing file still fails the test.
- `AGENTS.md` states the citation form.
- No doc sentence changed meaning; the diff of the two docs is citations only.

Commands:

- `go vet ./... && go test ./...` exits 0.
- `go test -count=1 ./internal/docscheck -v` exits 0 and lists the five tests.
- `rg -n '\.go:[0-9]' docs/guide.md docs/design.md` prints nothing.
- `gofmt -l .` prints nothing.
- `slicer check --root /home/jon/code/squalor-xyz/ork` and `adj lint` exit 0.

### Git

## ork stats: usage, exits and fix-round causes per role and model
key: wfr-stats-usage
short_title: ork stats --usage
size: M
tree: cli, daemon
release_target: v1.3
depends: Usage: record a step's own cost and tokens, not the session total
findings: docs/review-2026-11-12-workflow.md F7: the review needed four scripts over ork status --json and 190 ork show calls.
importance: 2
urgency: 1
effort: 2

*Proposed.* Drafted 2026-11-12, anchors checked at f48a6154 on 2026-11-17. `ork stats` prints slicer's time statistics. ork's own run records hold tokens, cost, exits and findings, and no command adds them up.

**Not in this slice:** a TUI view; the outer session's tokens; changes to slicer's statistics.

### Why

*Proposed.* Evidence:

- `internal/cli/stats.go:16-70` prints only `slicercli.RelayStats`.
- The 2026-11-12 review computed, by script: tokens and cost per role, vendor and model; non-ok exits; rounds per run; runs whose fix round had no fix finding; finding kinds per role.
- The trial review asked to keep measuring (`docs/review-2026-10-26-orchestration-trial.md`, O4).

No open owner question on scope. The text layout goes through the UI pass.

### Files

- `internal/cli/stats.go:16-70`
- `internal/cli/stats_test.go`
- `internal/cli/help.go`
- `internal/daemon/runs.go`
- `internal/daemon/wire.go`
- `docs/guide.md`
- `docs/design.md`
- `CHANGELOG.md`

### Failing tests

- `TestStatsUsagePerRoleAndModel`: three recorded runs give steps, tokens, cost and role time per role, vendor and model.
- `TestStatsUsageCountsExits`: non-ok exits are counted per role, vendor and exit class.
- `TestStatsUsageCountsEmptyFixRounds`: a run whose round-1 reports had no fix finding and that ran round 2 is counted.
- `TestStatsUsageCountsFindingKinds`: fix, owner and later findings are counted per role.
- `TestStatsUsageJSON`: `--json` carries the same numbers.
- `TestStatsUsageSince`: `--since DATE` leaves earlier runs out.
- `TestStatsUsageUnknownCostIsADash`: a vendor that reports no cost shows `-`, not `$0.00`.

### Implement

1. Daemon: one read that folds the run records into the totals (no report text on the wire).
2. CLI: `ork stats [PROJECT] --usage [--since DATE] [--json]` (*proposed*). Without `--usage` the output does not change.
3. Docs (*proposed*): the guide's command list and `docs/design.md`. CHANGELOG.md: one bullet.

### Check

Acceptance criteria:

- `ork stats P --usage` prints, per role, vendor and model: steps, tokens, cost and role time; then non-ok exits per role, vendor and class; rounds per run; the count of runs whose fix round followed no fix finding; and finding kinds per role.
- Cost is `-` for a vendor that reports none.
- `--since DATE` and `--json` work with `--usage`; the JSON holds every number the text shows.
- `ork stats P` without `--usage` prints what it prints today.
- The text fits 80 columns.

Commands:

- `go vet ./... && go test ./...` exits 0.
- `go test -count=1 ./internal/cli ./internal/daemon -run 'StatsUsage' -v` exits 0 and lists the seven tests.
- `gofmt -l .` prints nothing.
- `slicer check --root /home/jon/code/squalor-xyz/ork` exits 0.

### Git
