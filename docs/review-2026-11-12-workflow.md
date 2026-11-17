# Workflow review: efficiency, quality, cost

2026-11-12. Covers the 75 engine runs ork recorded from 2026-11-08 to
2026-11-12 (65 landed, 6 stopped, 4 unfinished) in the ork, slicer and
sluice projects. Everything ork or slicer would add here is *proposed* until
an adj record decides it.

Roadmap outlines for these findings, in slicer's import format:

- [review-2026-11-12-roadmap-ork.md](review-2026-11-12-roadmap-ork.md):
  seven ork items, imported on 2026-11-17 as S380 to S386. S380 to S384 are
  in v1.2 and S385 and S386 in v1.3 (owner, 2026-11-17).
- [review-2026-11-12-roadmap-slicer.md](review-2026-11-12-roadmap-slicer.md):
  two slicer items, imported into `../slicer` on 2026-11-17 as s460 and s461
  (owner's one-time permission).

The outlines were rewritten on 2026-11-17 with acceptance criteria, the
owner's four decisions and anchors checked at f48a6154. The evidence
sections below are as measured on 2026-11-12; line numbers in them are from
that day.

## How this was measured

- `ork status --json`: every run's role steps with vendor, model, round,
  exit, tokens, cost and times.
- `ork stats ork --json`: slicer's relay-run statistics (all time).
- `ork show RUN NAME`: 55 round-1 reviewer reports, the UI pass reports and
  76 gate reports, counted by section and finding kind with a script.
- The code at the lines cited.

The scripts are in `.scratch/review-1112/` in the main checkout (not
tracked). Not measured: the outer session's own tokens (ork does not record
them), Grok and Codex cost (neither vendor reports one), and the check
command's run time.

## The short version

1. **The recorded numbers are wrong in two places.** Claude cost is a
   session total that ork adds up per step, so the $542 shown is about $194.
   Codex tokens are also session totals, so 1,385M recorded tokens are about
   1,129M. Budgets read the same numbers. Fix this first; every later
   measurement depends on it.
2. **Some fix rounds fix nothing.** 29 of 65 landed runs ran a fix round
   although no reviewer or UI finding was tagged `[fix]`. In at least 12 of
   them the cause looks like the parser, not the diff: a section that says
   "nothing" and then explains what was traced counts as a finding.
3. **The implementer is 78% of all tokens** (883M of 1,129M corrected), and
   most of that is context read again on every turn. Three cheap cuts: a
   smaller fixed context for cloud roles, no model round for the finish
   step, and a digest instead of the 85 KB queue for the reviewer.
4. **Review quality is good and cheap.** The reviewer finds real defects by
   trace. Do not cut the reviewer, the UI pass or the gate. Cut what they
   are asked to read, and the mechanical findings a test could raise first.
5. **Doc accuracy is 42% of reviewer fix findings** (33 of 79), level with
   correctness (34). Many are line citations that moved. A test can own
   those.

## What the numbers show

### Runs

| Measure | Value |
|---|---|
| Runs | 75 (65 landed, 6 stopped, 4 unfinished) |
| Role steps per run, median | 7 |
| Tokens, recorded / corrected | 1,385M / 1,129M |
| Tokens per landed slice, corrected | 17.4M |
| Claude cost, recorded / corrected | $542 / $194 |
| Gate verdicts | 71 PASS, 5 FAIL |
| Runs with a fix round | 62 of 65 landed |
| Runs with a tagged `[fix]` finding in round 1 | 33 of 65 |
| Later findings on run records | 139 |
| Runs with permission denials | 19 (26 denials) |

### Tokens by role (corrected)

| Role | Steps | Tokens | Share | Role time |
|---|---|---|---|---|
| implementer | 284 | 883M | 78% | 22.7 h |
| reviewer | 72 | 153M | 14% | 12.2 h |
| gate | 81 | 67M | 6% | 2.1 h |
| UI pass | 48 | 26M | 2% | 1.0 h |

### Implementer by model

| Row | Steps | Recorded tokens | Recorded cost |
|---|---|---|---|
| Claude sonnet high | 191 | 705M | $507 (cumulative) |
| Codex gpt-6.1-sol medium | 86 | 415M (cumulative) | not reported |
| Codex gpt-6.1-sol high | 3 | 19M | not reported |
| claude-local qwen3.8-27b | 4 | not reported | none |

Codex ended `out_of_credits` 21 times. 27 of the 65 landed runs therefore
began on Claude sonnet high, and that row carries all of the dollar cost.

### Review findings (55 round-1 reviewer reports)

| Section | fix | later | owner |
|---|---|---|---|
| (A) Correctness | 34 | 1 | 2 |
| (B) Test gaps | 11 | 2 | 0 |
| (C) Direction | 1 | 36 | 0 |
| (D) Doc accuracy | 33 | 3 | 0 |

The UI pass gave 62 fix, 63 later and 6 owner findings in 48 passes, for
about $0.74 a pass (recorded, so an upper bound).

## Findings

### F1. Cost and Codex tokens are session totals, added per step

Evidence:

- In all 63 runs where a Claude role ran more than once, the step costs
  never decrease. S298: $4.75, $6.40, $6.65, and the third step ran for 10.6
  seconds. A 10-second step cannot cost $6.65; the value is the resumed
  session's running total.
- `internal/agent/claude/stream.go:102` stores `total_cost_usd` as the step's
  cost. `internal/daemon/runs.go:2207` sums the steps.
- In all 20 runs where a Codex role ran more than once, the step tokens never
  decrease. S315: 7.5M, 13.8M, 14.6M, and the third step ran for 20 seconds.
  `internal/agent/codex/codex.go:189` stores input plus output as the step's
  tokens; the comment at `codex.go:132` says the count is unverified.
- Claude tokens are per step (3 of 63 sequences are monotonic). Grok tokens
  are per step (2 of 12).
- `overBudget` reads the recorded role usage
  (`internal/engine/round.go:247-261`), so a Codex run reaches a token budget
  early.

This is inferred from the run records. No vendor document was read. The
slice's first step is a fixture of two resumed invocations per vendor.

*Proposed:* record the step's own cost and tokens (the difference from the
session's last recorded total), keep the session total beside it, and sum
only the step values. Outline item "Usage: record a step's own cost and
tokens".

### F2. A fix round runs when the reviewer found nothing

Evidence:

- 29 landed runs ran implementer round 2 with no `[fix]` tag in the
  reviewer-1 or ui-1 report.
- `triage` treats any untagged section (A), (B) or (D) with content as a fix
  finding (`internal/engine/report.go:530`). `saysNothing` accepts only a
  body that is the single word (`report.go:661`). A body of "nothing" plus a
  paragraph on what was traced is content.
- `review` then sends the whole untagged report to a fix round
  (`internal/engine/states.go:619`).
- Of the 18 such reports the script could split into sections, 7 open every
  one of (A), (B) and (D) with "nothing" (S103, S129, S236, S259, S291,
  S292, S293). In 6 runs the round-2 implementer report says it changed
  nothing (S129: "No changes were made in this fix round"). One run is in
  both groups, so at least 12 runs.
- The other 17 were not classified. Some hold real untagged findings, which
  the rule "a finding without a tag is a fix finding" sends to a fix round
  on purpose.

Cost: these 29 round-2 steps took 1.3 hours of role time (median 3 minutes),
23M Claude tokens, and a second pass of context for the gate.

*Proposed:* a section whose first line says nothing is empty, whatever
follows. Outline item "Fix round: a section that opens with nothing is
empty". Open question Q1 below.

### F3. The finish step is a model round that runs two commands

Evidence:

- `finish` resumes the implementer to run `slicer note` and `slicer done`
  (`internal/engine/states.go:980-1006`).
- 65 finish steps, median 12 seconds. Claude: 51 steps, 56M tokens (each
  resume reads the whole session again). Codex: 14 steps.
- It can fail like any role. On S315 the finish step hit Codex
  `out_of_credits`, fell back to Claude twice, and the run needed another
  gate and a fourth implementer round.
- The only thing the model adds is the closing note's text. The implementer
  already writes the verified outcome in its last report.

Constraint: slicer's relay contract gives the tracking steps to the
implementer (`slicer ai relay`), and adj 81 (`engine.finish-order`) orders
the finish step before the commit. Changing who runs it needs an owner
decision and a contract change in slicer.

*Proposed:* the implementer's last report carries an `Outcome:` line; ork
runs the closing note and the finish step itself. Outline items "Finish step
without a model round" (ork) and "Relay contract: the engine may run the
closing note and finish step" (slicer). Open question Q2.

### F4. Cloud roles carry fixed context they never use

Evidence:

- S360 measured a Claude role's first turn at 30,211 input tokens, and 26,692
  with six unused tools and the slash commands off (12% less). ork applies
  that only to `claude-local` (`internal/agent/claude/args.go:57`).
- The fixed context is read on every turn. A round-1 Claude implementer step
  records 11M to 24M tokens, which is mostly that re-reading.
- Claude ran 245 role steps in this period.
- The Bash output cap is also local only
  (`internal/daemon/runner.go:250`).

The saving is not measured for a whole run; 12% is the first turn only.
What makes up the remaining 26.7k tokens is not measured either.

*Proposed:* give cloud Claude roles the same tool list, and measure what
else is in the first turn (project and user instruction files, MCP servers).
Outline item "Cloud Claude roles: a smaller fixed context". Open question
Q3.

### F5. The reviewer reads the whole queue to judge direction

Evidence:

- The reviewer and architect prompts tell the role to run
  `slicer list --json --lean` (`internal/roles/prompts/reviewer.md:41-42`,
  `internal/roles/prompts/architect.md:39-40`). Today that prints 85,139
  bytes for ork.
- Section (C) produced 1 fix finding and 36 later notes in 55 reports.
- The reviewer is the second largest token user (153M) and the slowest step
  (median 9m53s in `ork stats`).

*Proposed:* ork builds a direction digest in Go (the goals, plus the open
items whose Files paths overlap the diff, plus id and title of the rest) and
puts it in the brief. Outline items "Reviewer and architect: a direction
digest in the brief" (ork) and "list --touching PATH" (slicer, optional).

### F6. Line citations in docs go stale and reviewers find them

Evidence:

- `docs/*.md` hold 257 backticked `path:line` citations.
- (D) Doc accuracy is 33 of 79 reviewer fix findings. Of 14 sampled, 5 are
  citations that point at the wrong lines (S132, S215, S219 three times);
  the rest are sentences the diff made false.
- `internal/docscheck/cite_test.go` checks only that the file exists and
  the range is inside it (`cite_test.go:47-155`).

*Proposed:* cite a symbol, not a line, in `docs/guide.md` and
`docs/design.md`, and let the test check that the symbol is in the file.
Outline item "Docs citations by symbol, checked by a test". Open question
Q4.

### F7. This review needed four scripts

`ork stats` prints slicer's time statistics only
(`internal/cli/stats.go:16-70`). Tokens, cost, exits, fix-round causes and
finding kinds per role and model came from scripts over `ork status --json`
and 190 `ork show` calls.

*Proposed:* `ork stats --usage`. Outline item "ork stats: usage, exits and
fix-round causes per role and model". It depends on F1.

## What works; keep it

- **Deterministic before model (C18).** Gate FAIL is 5 of 76. The gate is
  cheap (median 1m20s).
- **The reviewer.** Its correctness findings are traced defects with a
  failure scenario and a fix (for example S298's dropped shape-pause count).
  11 test-gap findings name tests that would stay green on a revert.
- **The UI pass.** 62 fix findings for 2% of tokens.
- **Vendor fallback.** 21 Codex credit failures cost 3.5 seconds each and
  no run stopped on one.
- **Finding kinds (adj 163).** 139 later findings stayed out of fix rounds.

## Observations with no slice

These have too little evidence, or an existing item or decision covers them.

- **Codex credits decide the cost.** With Codex out, every slice runs on
  Claude sonnet high. A cheaper stand-in for a well-specified size S slice
  is a matrix decision for the owner. S304 is measuring the local rows.
- **Later findings pile up.** 139 in five days, beside the sweep items S227
  to S232 and S307. Whether a cross-run view to file or drop them in bulk
  exists was not checked.
- **Human checks.** `human-checks-todo.md` lists 41 runs with open checks.
  Adj 281 says they never block; no change proposed. Many are fixed-size
  terminal checks that a scripted frame capture could replace, which was
  not investigated.
- **Permission denials.** 26 in 19 runs. S345, S346 and S350 cover the
  known causes.
- **Stale daemon.** `ork status` reported a daemon build behind the CLI
  build with roles running. S221 covers the restart helper.
- **Scratch files in a diff.** One reviewer finding (S334, an untracked
  `zz_debug_test.go`). One case is not enough for a check.
- **Large files.** `internal/daemon/runs.go` is 2,566 lines and
  `internal/tui/overview.go` 2,126. Every role that edits them reads them.
  No refactor is proposed without an owner request.

## Owner decisions (2026-11-17)

Given by the owner in interactive questions on 2026-11-17 and recorded in
adj. Each took the recommended option.

- **F2, adj 406 (`review.nothing-section`).** A section whose first line
  says nothing or none is empty; later prose is evidence, not a finding. An
  untagged finding is still a fix finding. S381.
- **F3, adj 407 (`engine.finish-runner`).** ork runs the closing note and
  the finish step, with the note text from an `Outcome:` line in the
  implementer's last report. It needs relay v13 (slicer s460). S384.
- **F4, adj 408 (`roles.cloud-fixed-context`).** Cloud Claude roles get the
  tool list and the slash-command switch only; no output cap. S382.
- **F6, adj 409 (`docs.citation-form`).** The guide and the design cite a
  path and a symbol with no line number, checked by a test. S385.

Still open, with no adj record: after Grok ends on 2026-12-09 the reviewer
and the gate fall back to Claude opus. Whether every slice keeps both on
opus is a matrix decision for the owner; S380 gives the numbers for it.

## Suggested order

| # | Item | Target | Size | Why this order |
|---|---|---|---|---|
| 1 | S380 Per-step cost and tokens | v1.2 | S | Every later number depends on it; budgets are wrong today |
| 2 | S381 Nothing-plus-prose is no finding | v1.2 | S | Removes a whole round from about one run in five |
| 3 | S382 Smaller fixed context for cloud Claude roles | v1.2 | S | One flag list; applies to every Claude step |
| 4 | S383 Direction digest in the brief | v1.2 | M | Cuts the reviewer's largest read before Grok review ends on 2026-12-09 |
| 5 | slicer s460, then S384 ork runs the finish step | v1.2 | S, M | One fewer role step per run; S384 waits on relay v13 |
| 6 | S385 Docs cite symbols, not lines | v1.3 | M | Moves a class of reviewer findings to `go test` |
| 7 | S386 ork stats --usage | v1.3 | M | Makes the next review one command; waits on S380 |

S380, S381 and S382 touch different files and can run as one wave.

## Limits of this review

- Five days of runs, mostly on one project.
- The fix-round classification in F2 is by pattern over report text. The
  slice's first step replays the saved reports through `triage`.
- Token counts mix vendors whose counting differs. Compare shares within a
  vendor, not across.
- No slice was run to test a proposal. Every saving named is an estimate
  from the records.
