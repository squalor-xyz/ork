# Relay retrospective and process recommendations

2026-10-25. Covers the v1 relay run from 2026-10-23 to S65 (29 relay runs).
The relays were run by hand from an outer Claude Code session, one
orchestrator subagent per slice. Sources: `slicer relay-run list --json`,
the Review FAIL notes in slicer, the orchestrator reports, `docs/design.md`,
and notes kept during the run. Numbers are from the ledger. Step times are
approximate, because orchestrators update step phases with some delay.

Everything ork itself would add here is *proposed* until its adj record is
decided. The owner directed on 2026-10-25 to follow these recommendations;
what was done is listed under "Actions taken" at the end.

## The short version

The relays produce good code. The review loop finds real bugs on almost every
slice, and nothing broken has landed. The cost is in the scaffolding around
the roles. A Claude Code session drives every relay by hand: it writes
specs, spawns an orchestrator per slice, relays your answers and runs a
landing script. ork already has an engine that does most of this, and it has
never run a real slice (S31 is parked).

The main recommendation is to move the flow into ork now, not after v1. Do
it in steps, starting with S31. Every friction point below then becomes a
requirement for ork instead of a workaround in a shell script. Until the
switch, a few cheap changes remove most of the waiting.

## Recommendations

### R1. Switch to ork-driven relays, in three steps

You want to develop entirely through ork. Today none of the slices go
through it. The engine exists (`docs/design.md:183-243`): setup, implement,
deterministic checks, review, architect, fix rounds, gate,
awaiting-approval, finish, commit. Waves exist too (`docs/design.md:151-181`).
What is missing is use.

1. **Unpark S31 and run it next.** Run one small, well-specified slice
   through `ork run` (S78 `recoverRuns map writes under lock` or S73 are
   good candidates: size S, engine-only, no goldens). I watch from outside
   and change nothing. Every gap is filed as a slice. The S17 human check
   (`ork approve` on a live run) closes as part of it.
2. **Close the gaps that block daily use**, ahead of the remaining polish
   slices (see R7 for the order). Likely gaps, from what this run needed
   that ork does not do yet:
   - a spec readiness step before a run (R2);
   - a UI pass role (R4); `relay-run` has no `ui` role today (S87);
   - landing after approval: merge main in if it moved, re-run checks,
     ff-merge, optional push, remove worktree and branch (R5). ork stops at
     `committed` today, and "landing help" is only a ready-to-land list
     (`docs/design.md:884`);
   - structured owner questions answered by digit (S111);
   - a cockpit that stays current: S109 (pane follows the newest run) and
     S110 (Running summary).
3. **Run the rest of v1 through ork, in waves.** Use the Claude Code flow
   only as a fallback, and for slices that change the daemon's own run
   path, where a bug could stall the run that is fixing it.

Self-hosting rules to settle first:
- the daemon runs a binary built from main, not from a slice worktree;
- after a slice that changes the engine or daemon lands, ork asks to
  restart the daemon between waves;
- a wave never contains two slices that both change the engine.

### R2. Separate spec readiness from the run, and batch it

Evidence: orchestrator preflight found real gaps on S32, S97, S07, S33, S35
and S45. On S65 it found that two of the planned changes were not needed. It
pays for itself. Doing it inside each run has a cost, though: every design
question stops a running relay and waits for you. Runs that waited on owner
answers took the longest by far. S07 took about 18 h and S27 about 19 h,
against a median run of about 20 min. The wait before S74 was 95 min and
before S63 was 61 min.

Split the work in two:
- **Questions early, in a batch.** A read-only "spec check" role reads the
  next 4 to 6 slices and the code. It returns, per slice: ready, or a list of
  structured questions (title, 2 to 4 options, recommendation). You answer
  them all in one sitting in ork's Questions view. The answers go into the
  slice spec and, for design points, into adj.
- **Anchors late, just before the worktree.** Line anchors drift as other
  slices land, so re-anchoring early is wasted work. The run's first step
  (the implementer, or a cheap pre-step) re-anchors Files and Failing tests
  from the current code. That is fact work and needs no owner.

Only slices marked ready enter a wave. `slicer next` already skips slices
with empty Implement and Check; "ready" adds "no open design question".

### R3. Retire the per-slice LLM orchestrator

The orchestrator's job is spawning roles in order, recording ledger steps,
passing reports, and stopping on a stop rule. That is deterministic. It is
exactly what ork's engine state machine does, and it does not need a
sonnet-high model per slice. Most of the friction in this run came from the
orchestrator layer:
- the never-cd rule broken repeatedly;
- interim hand-backs that wake the outer session with no content;
- an API error that killed one mid-gate (S33: the gate took 87 min);
- fragile Codex resume handling: cwd, `GOCACHE`, stdin stall.

In ork, the role adapters own those mechanics once, in code and under test.

The judgment the orchestrator added (preflight, deciding whether a finding
is real) moves to the spec check role (R2) and to finding triage (R4).

Until R1 step 2 is done, keep the orchestrator as it is. Spend no more
effort tuning the Claude Code flow beyond the cheap fixes in R8.

### R4. Keep the quality gates; sharpen what each one returns

Evidence: in 28 passed runs, 23 needed at least one fix round. Three needed
two (S49, S97, S35). The gate failed once (S97, a constant "last heard 0s
ago"). Nothing that passed the gate has needed a revert. The Review FAIL
notes are real defects, not style:
- races and stale handles: S15, S25, S36, S52;
- daemon lifecycle: S13, S17, S46;
- wiring that never reached the adapter: S47;
- docs out of step with code: S47, S48, S49;
- layout regressions: S84, S97.

The reviewer is the most valuable role per minute: about 2 to 4 min per
review.

Keep:
- **Reviewer:** opus medium, read-only, after deterministic checks.
- **Gate:** the resumed reviewer, lite on S and M. It is cheap (about 1 min)
  and caught S97.
- **UI pass:** for UI slices, in parallel with the reviewer. It found
  things the gate did not (S33 clipping at 80x24, S84 key width).
- **Architect:** only on size L (2 slices of 113).
- **Implementer and reviewer on different vendors:** Codex implements and
  Claude reviews, so the review is already cross-vendor.

Change:
- **Triage every finding into one of three kinds:** `fix` (goes to the fix
  round), `owner` (a design call: goes to you as a structured question and
  does not block the gate unless it marks a blocker), and `later` (filed as
  a slice). Today owner calls get mixed into fix rounds or end up in "later
  notes" that only I read. Examples: S65's spent-grant behavior, and S84's
  four UI suggestions.
- **The UI pass returns pictures, not only text.** It renders the changed
  views at 80x24, 120x40 and ASCII, and attaches them to the approval card.
  Your human check then happens at approval, in ork. Today human checks pile
  up as adj records (141 to 145, 147, some days old), and the S84 UI pass
  ran on goldens only.
- **Only a real FAIL writes a `Review FAIL` note.** S65 got
  "Review FAIL: none (no blockers)" because a fix round ran for UI edits.
  The hand-run orchestrator wrote it; ork's engine already writes the note
  only on a gate FAIL (`internal/engine/states.go:298`).
- **Add a self-check to the implementer brief** built from these defect
  categories: concurrency and handle lifetime, docs updated with code, a
  test that fails when the wiring is removed. That aims to cut first-round
  FAILs without dropping a gate. Measure the fix-round rate before and
  after; do not assume it works.

### R5. Make landing an ork action after your approval

The hand-run landing script has been reliable. It runs the full checks,
commits, merges main in if needed, ends the ledger run, ff-merges, pushes
and cleans up. It ran on all 28 passed slices with no failure. Move that
into ork as the step after checkpoint 2 (approve). This changes a decided
record: `landing.v1` (#49) keeps landing out of ork, and
`docs/boundaries.md:17` says ork never merges or lands. adj 162 asks the
owner to supersede it.
- a per-project setting for push (off by default);
- the commit title comes from the slice's Git section;
- no AI attribution.

Approval should show what you need to decide in one screen: the gate
verdict, test counts, the diff stat, the triaged `owner` findings, UI
renders, and human-check items. Approve then lands. Reject, or "fix this",
sends a typed note to a fix round.

### R6. Let slices run in parallel where it is safe

This run was strictly serial. The serial order came from a rule I adopted:
no slicer filings on main while a worktree runs. Slicer now ships merge
drivers, and they are configured in this clone:
- `log.jsonl` merges by union;
- `index.json` merges by item id (`slicer merge-index`);
- render files keep the current branch's copy, then `slicer render`.

The landing script already merges main into the branch when main moved.
Tested on 2026-10-25 in a throwaway clone: a branch that started, noted
and finished one item, merged with a main that added an item, noted
another and edited a third. The merge was clean, ids did not collide, and
`slicer check` passed after `slicer render`. The render files keep the
branch's copy, so a merge must be followed by `slicer render`, a commit and
a re-check. The landing script now does that, and re-runs the tests after
the merge. The rule is dropped.

With that, waves can run 2 to 4 slices at once (the design's global cap is
4). Two kinds of overlap stay serial:
- **Shared Files.** `slicer plan-parallel` and ork's overlap warning
  already cover this.
- **Golden churn.** S84 rewrote about 107 golden files, and S106 will
  rewrite them again. Put layout slices that touch the same views in the
  same wave, so goldens are re-baselined once, not twice.

### R7. Reorder the remaining queue around dogfooding, and fence v1

Today v1 has 22 open slices and v1.1 has 19. Nine slices were filed this
session (S105 to S113), so the queue grows almost as fast as it drains.
Order, as set on 2026-10-25 (score in brackets):
1. **Dogfood enablers [33]:** S87 (Codex GOCACHE and temp dir in ork's
   sandbox, a known failure that would hit S31 first); S31 (runs S78
   through ork); S114 (self-hosting); S115 (implementer brief); S70
   (heartbeat); S99 (live role detail); plus the gaps S31 finds.
2. **Owner cockpit and gates in ork [32]:** S111, S109, S110 (moved from
   v1.1 to v1; without them you cannot follow and answer runs from ork),
   S117 (UI pass role), S118 (approval card), S119 (land after approval),
   S120 (finding kinds), S121 (UI renders), S116 (spec check).
3. **Engine correctness [23]:** S92, S93, S102, S113 (both moved to v1),
   S73, S75, S71.
4. **v1 polish, run through ork [22]:** S85, S86, S94, S98, S67, S68,
   S69.
5. **Last:** S72 (Grok, after 2026-11-01) and S91 [21]; docs S90 and S39
   [11].

v1.1 keeps its order: S105 to S108, S104, S101, S103, S50, S61, S66,
S24, S100, S112, S62.

Define what "v1 is releasable" means in one adj record. My suggestion: you
can run a wave, answer its questions, approve and land it, all from ork,
with no Claude Code session. Polish found after that goes to v1.1 by
default.

### R8. Cheap fixes for the interim (until R1 lands)

1. **Batch questions.** For the next slices, I do the spec check for 4 to 6
   slices at once and ask you everything in one round.
2. **Stronger never-cd rule.** It goes at the top of `.scratch/relay-brief.md`
   with a worked `git -C` and `go -C` example. Codex resume always runs in a
   subshell.
3. **Tighter UI pass and reviewer briefs.** UI pass suggestions come back
   triaged (R4). The reviewer marks owner calls as owner calls.
4. **One report for you per slice, at landing.** Today you also get
   "waiting on" pings that carry no content.

### R9. Matrix and role assignments

| Role | Today | Recommendation |
|---|---|---|
| implementer | Codex gpt-6.1-sol, medium (high on L) | keep; it finished all 28 slices |
| reviewer | opus medium (r5) | keep; add triage kinds (R4) |
| gate | resumed reviewer, opus medium (r6) | keep; lite on S and M |
| UI pass | opus medium (r14), text only | keep; add renders; add a `ui` role to the ledger (S87) |
| architect | opus high (r7), size L only | keep |
| orchestrator | sonnet high (r10), per slice | retire once ork runs relays (R3) |
| spec check | none | new, read-only, batched; a row in the matrix (model to choose: sonnet medium is my starting proposal; measure) |
| fallback | matrix alternatives | keep; S92 and S93 fix the brief and the gate row after a fallback |

## Appendix A. What the numbers show

| Measure | Value |
|---|---|
| relay runs since 2026-10-23 | 29 (28 pass, 1 stopped on purpose: S50 held for v1.1) |
| median run wall time | about 20 min |
| implementer first round | usually 2 to 12 min |
| review | usually 1 to 4 min (S63 10 min, S97 22 min) |
| gate | usually 0 to 2 min (S33 87 min, an API error) |
| runs with at least 1 fix round | 23 of 28 |
| runs with 2 fix rounds | 3 (S49, S97, S35) |
| gate FAILs | 1 (S97) |
| slices landed with no fix round | 5 (S40, S74, S88, S76, S77) |
| longest runs | S27 about 19 h and S07 about 18 h (waiting on the owner) |
| idle gaps between runs | mostly 0 to 1 min; 95 min (S74), 61 min (S63), 59 min (S47) |

The roles themselves are fast. Wall time goes to owner waits, then to
preflight and landing. That is why R2 (batch questions) and R5 (approval
with everything on one screen) matter more than any model or effort change.

## Appendix B. Pros, cons and friction by area

**Spec and preflight**
- Pro: orchestrator preflight caught real gaps and shrank S65.
- Con: anchors went stale as slices landed, and re-anchoring was repeated
  per slice.
- Friction: design questions arrived one slice at a time and held runs for
  hours.

**Orchestration**
- Pro: stop and resume via SendMessage kept context across owner answers,
  and recovered from an API error.
- Con: a model doing deterministic work, and a turn per slice.
- Friction: the never-cd rule broken repeatedly; "waiting on" hand-backs
  with no content.

**Implementer (Codex)**
- Pro: finished every slice, and ran `slicer done` in its sandbox on most
  runs.
- Friction: resume only works with cwd = the worktree; `GOCACHE` must sit
  under its `--add-dir`; headless stdin can stall the finish step (S82).
  All of these belong in ork's codex adapter.

**Review and gate**
- Pro: found real defects on 23 of 28 slices; gate caught S97.
- Con: owner calls mixed into fix rounds; a false "Review FAIL" note on
  S65.

**UI pass**
- Pro: caught layout problems that tests passed.
- Con: works from goldens only; its suggestions end up in later notes that
  only the outer session reads.

**Landing and tracking**
- Pro: `land.sh` was reliable on all 28.
- Con: serial landing by my own rule, likely stricter than the configured
  merge drivers need; large golden churn on layout slices.
- Friction: later notes have to be turned into slices by hand-written
  scripts (`.scratch/followups/*.sh`).

**Owner experience**
- Pro: questions came with options and recommendations, so they were quick
  to answer.
- Con: everything reached you through the Claude Code chat, not ork. Human
  checks piled up in the adj inbox. You looked at ork screenshots to see
  run state, and found that the run pane does not follow the current run
  (S109).

**Process scope**
- Con: the queue grew as it drained, with nine slices filed in one day, and
  v1 has no exit line.

## Appendix C. Open owner calls carried from the run

- S65: is a spent grant re-pausing an accepted limit, or do you want a
  `Grant` field on Pending? Should an approved run be able to pause on
  budget before `slicer done`?
- S84: the decision key width cap; the wording of pending rows under
  Errors; a status marker on non-open Roadmap rows; the S1 column position;
  per-project badges that do not sum to the sidebar total.
- S28: reset the toggle after each submit? Record your acceptance on the
  slice?
- adj human checks 141 to 145 and 147.

## Actions taken (2026-10-25)

Owner direction: follow every recommendation, file new slices, reorder the
roadmap around the ork-driven workflow, and commit this report.

**Owner decisions asked** (open; each has a recommendation):

| adj | Key | Recommendation |
|---|---|---|
| 162 | `landing.in-ork` | land after approval (supersedes #49 if decided) |
| 163 | `review.finding-kinds` | fix, owner or later |
| 164 | `roles.spec-check` | batched spec check before a wave |
| 165 | `roles.ui-pass` | UI pass in the engine, with renders |
| 166 | `engine.self-host` | base-branch binary, restart offer, one engine slice per wave |
| 167 | `release.v1-exit` | the ork-only loop works end to end |

**Slices filed** (all v1):

| Slice | Recommendation | Waits for |
|---|---|---|
| S114 Self-hosting | R1 | adj 166 |
| S115 Implementer brief: re-anchor first, self-check | R2, R4 | none |
| S116 Spec check | R2 | adj 164, S111 |
| S117 UI pass role in the engine | R4 | adj 165 |
| S118 Approval card | R5 | S111 |
| S119 Land after approval | R5 | adj 162, S118 |
| S120 Finding kinds | R4 | adj 163, S111 |
| S121 UI pass renders | R4 | S117, S118 |

**Slices changed:**
- S31 unparked and specified: run S78 through ork. Depends on S87.
- S87 narrowed to the Codex sandbox gap in ork's own adapter: no
  `--add-dir` for the role temp dir and no `GOCACHE`
  (`internal/agent/codex/codex.go`, `internal/daemon/runner.go:79`).
- S71 got a Check section, so `slicer next` no longer skips it.
- S109, S110, S111, S102 and S113 moved from v1.1 to v1.
- Scores set by tier (R7).

**Process:**
- The serial-landing rule is dropped after the merge test (R6).
- No slice needed for the false Review FAIL note: the engine already
  writes it only on a gate FAIL.
