# Slivering in ork

*Proposed.* Nothing here is decided. It is a report for the owner on
automated "slivering": cutting an effort 1 slice into parts that cheap models
implement in parallel, with a click up to a stronger model when quality falls
short. Every option, slice and threshold below is *proposed* or *candidate*
until an adj record decides it. Written 2026-11-07 at main `820945c`.

## Summary and recommendation

- Conditional yes, default off. Do not make it automatic until a pilot shows
  it keeps quality and saves cost.
- Judgment (cost is not measured): most of the saving would come from a
  cheaper first implementer with a quality click up, not from parallel
  parts. Build that first: no new role, planner or merge work.
- What the owner describes (parallel parts, disjoint files, one review and one
  gate on the combined diff) is slicer's *split*, not slicer's *sliver*.
  Contract slivers each get their own review and gate, which costs more.
- Cut by Go package or by spec item, not by file. Go compiles a package as a
  unit, so two parts in one package break each other's tests mid-edit.
- Keep reviewer and gate rows as they are, on the combined diff. Leave
  effort 2 out until the effort 1 pilot has data.
- Today the cheap tier is Haiku only on this host: Codex does not start here
  (S307) and local cannot be spawned yet (S301). Plan for that.

## What slivering means here

The word already has a meaning in slicer and in the matrix. Three shapes are
in play. The owner's idea matches the second best.

| Shape | Source | Tracked items | Worktree | Review and gate | Parallel |
|---|---|---|---|---|---|
| Sliver | `slicer ai relay`, section Slivers; matrix u6 to u9 | one item per sliver, `parent` link, filed on main first | parent's, shared | each sliver gets its own, then one combined gate | only disjoint slivers; this matrix's u7 says one after another |
| Split | `slicer ai relay`, Roles and Order; matrix u5 | one item; lead owns tracking | the slice worktree | one review and one gate for the whole slice | parts with disjoint files |
| Owner's idea | owner, this brief | not stated | not stated | combined diff, strong model | N cheap parts at once |

Here, **contract sliver** and **split** mean slicer's terms; **slivering**
means the owner's idea, whatever shape it takes.

## Findings

Facts, with sources. Judgments are in Options and Risks.

### F1. Slicer already defines slivers and splits

- `slicer ai relay` (contract v11), Setup: "whether to sliver the slice. The
  default is no." It also says to propose slivers only when that "costs less
  than one implementer", and that the owner confirms the sliver plan at Setup.
  A split's default is one implementer.
- Section Slivers says eligibility is "effort 1 at any size; effort 2 only
  when every sliver comes out at effort 1; effort 3 never". Each sliver is
  filed on main with `slicer add TITLE --parent ID` and gets size S, effort 1.
  Each sliver "goes implement, review, gate, handoff". Then the parent gets
  "one combined gate on the whole diff".
- In a split, "the parts run in the slice worktree, in parallel only when
  their files are disjoint". "The orchestrator runs the full suite on the
  combined diff, then one review and one gate cover the whole slice." Part
  implementers "change only their own files, write and run their own tests,
  and run no slicer command that writes". The lead runs `slicer start` and
  the finish step.
- `slicer/docs/commands.md:413-430`: a sliver holds its parent; `handoff` and
  `done` on the parent refuse while slivers are open.
- Matrix rows (`slicer relay-roles show`): r8 (haiku xhigh), r27 and r28
  (terra medium and high) for a sliver; r4 (haiku xhigh), r29 and r30 (terra
  medium and high) for a split part.
- Matrix rules: u5 (parts are disjoint and have their own tests), u6
  (eligibility), u7 (slivers share the parent worktree, "run one after
  another", land once; names `ork run PARENT --slivers`), u8 (a sliver that
  fails its gate twice, or whose diff falls short, moves up: haiku to sonnet
  high; Haiku rows are for slivers and parts only), u9 (the orchestrator
  reads a Haiku diff before review and switches to Sonnet early).
- `slicer relay-roles recommend` reads the sliver condition from the item's
  `parent`; `--split yes|no` is the agent's judgment
  (`slicer/docs/commands.md:67`).
- The slicer roadmap has no item for a tier, ladder or escalation field
  (`slicer find escalat`, `tier`, `ladder`: none beyond s275, done).
  s445 (open) adds terra rows to slicer's default matrix.

### F2. ork runs neither slivers nor splits

- The engine has four roles: implementer, reviewer, architect, gate, plus the
  UI pass (`internal/engine/runner.go:18-22`). No lead or part role.
- `split` only reaches `slicer relay-roles recommend`
  (`internal/daemon/runs.go:386`, `internal/slicercli/write.go:107-110`).
  `Sliver` is a parsed field only (`internal/slicercli/types.go:238-239`).
- `ork run` has no `--slivers` flag (`internal/cli/cli.go:46-48`), although
  u7 names one. `docs/orchestrator-parity.md:61` lists this gap as
  "missing".
- A second run on one worktree is refused (`internal/daemon/runs.go:577-578`).
- Specs exist but are parked in v1.2: S61 (split, M, effort 2) and S62
  (slivers, L, effort 3, depends on S61). S61's owner decisions
  (2026-10-28): split is opt-in per run, "never automatic", at most 3 parts
  at once, one cap slot. S62: `ork run PARENT --slivers`, one worktree, one
  landing (adj 124).
- `PROJECT.md:167` lists "Split and slivers add parallelism inside a slice"
  as a risk, mitigated by building them after single-implementer runs work.
- adj 33 (`slice.options`): architect on L, split and slivers are in v1
  scope. adj 124 (`slice.sliver-worktree`): slivers share the parent's
  worktree and branch and land once.
- adj 189 (`launch.well-specified-default`, S260): ork plans split = no
  unless `--split` overrides. adj 86, 87 and 135 are still marked decided;
  189's context says the owner supersedes them.

### F3. Effort is a filer's estimate of implementation weight

- Slicer: effort is "a separate 1-to-3 estimate", not part of the score,
  used to sort and to choose agents in the matrix
  (`slicer/docs/how-it-works.md:129-131`); "optional implementation weight"
  (`slicer/docs/import.md:96`). Agents filing items must score it with a
  one-line reason; the owner adjusts (`slicer/src/slicer/ai.py:1530-1533`).
- It does not measure whether a slice can be cut into disjoint parts.
- Counts from `slicer list --json --lean`, 2026-11-07:

| Effort | Size | Open | All (incl. done, retired) |
|---|---|---|---|
| 1 | S | 35 | 128 |
| 1 | M | 0 | 1 |
| 2 | S | 3 | 20 |
| 2 | M | 41 | 119 |
| 3 | M | 1 | 4 |
| 3 | L | 2 | 3 |
| unset | S or M | 1 | 32 |

- So "effort 1 of any size" is, in practice, "size S": 128 of 129 effort 1
  items ever filed are size S. No item has ever had a `parent`
  (`slicer list --all`: 0 slivers).

### F4. How ork spawns and sequences roles today

See [design.md](design.md), Slice state machine and Roles, for the full
engine. The points that matter here:

- Lineup: for each role ork takes the recommended row plus every
  alternative as an ordered fallback chain, skipping rows with no adapter
  (`internal/daemon/runs.go:407-451`). Adapters exist for Claude Code, Codex
  and Grok only (`runs.go:354`), so r33 (Local) is skipped.
- The gate runs the reviewer's chain (`runs.go:495`); the matrix gate rows
  (r23, r6, r18, r20) are not used by ork.
- For S306 (effort 1, S) the implementer chain is r9 sol medium, then r26
  terra high, then r3 sonnet high (`slicer relay-roles recommend S306
  --well-specified yes --split no`). The chain is ordered by preference, not
  by strength: falling back can go down a tier.
- The chain advances only on a rate limit, out of credits or an error exit
  (`internal/engine/ledger.go:45-47`, `internal/engine/engine.go:178-224`;
  adj 156, sticky per adj 115). A gate FAIL never changes the row.
- Rounds: after round 3 ends red the run pauses with kind `rounds`
  (`internal/engine/engine.go:25`, `internal/engine/states.go:157-188`;
  S294). The third gate FAIL stops the run (`states.go:733-735`).
- Caps: global 2 by default (`runs.go:47-48`; adj 172), optional per project
  (`runs.go:849-873`; adj 20). One Codex slot daemon-wide
  (`internal/daemon/vendors.go:17`). A run whose implementer is local takes
  a slot in a shared local pool of 4 and no global or project slot (adj 187,
  192). No role defaults to local; an unreachable local endpoint falls back
  to the next cloud row (adj 181).
- Checks: ork runs the project check and `slicer check` after each
  implementer round, before review. Out-of-Files changes produce an
  advisory warning to the gate (`internal/engine/filescope.go:44-64`; adj
  119).
- Budgets are soft: pause at the next role boundary (adj 46).

### F5. The model ladder that exists

| Tier | Rows | Runs in ork on this host today |
|---|---|---|
| local | r33 qwen3.8-27b (candidate; Agent "Local (vLLM)") | no: no adapter; launchers are `~/.bashrc` functions (S301 Implement); S303, S304 open |
| cheap | r8, r4 haiku xhigh; r27 to r30 terra medium or high | haiku yes; terra no (Codex fails to start, S307 Environment; S258 ledger step 2) |
| mid | r26 terra high, r32 terra xhigh (whole slice) | no (Codex) |
| strong | r3 sonnet high; r9 sol medium; r12, r15 sol high | sonnet yes; sol no (Codex) |
| review | r22 Grok, r5 opus medium; r17, r19 opus high | yes |

- There is no opus or fable implementer row.
- r33's Agent value ("Local (vLLM)") is not one of the launcher names that
  adj 188 decided (`claude-local`, `codex-local`, `grok-local`).
- The owner put sol first for every whole slice, terra after it (slicer
  s445 note, 2026-11-03). That is quality first, the opposite of cheap first.
- `.scratch/local-eval/report.md` does not exist at the time of writing.
  The S304 measurement is pending, so there is no measured evidence for
  the local tier.

### F6. Quality and cost evidence from this project

- 2026-10-25 retrospective, 28 passed runs with strong implementers: 23
  needed at least one fix round, 3 needed two, 1 gate FAIL, nothing that
  passed the gate was reverted
  (`docs/review-2026-10-25-relay-retrospective.md:111-113,262-265`). "The
  roles themselves are fast. Wall time goes to owner waits, then to
  preflight and landing" (`:269-270`).
- 2026-10-26 trial: five slices on sol medium, 0 or 1 fix round each
  (`docs/review-2026-10-26-orchestration-trial.md:29-35`).
- `slicer relay-run stats`, 2026-11-07: 10 runs, 9 pass; 4 fix rounds; 3
  gate rounds, 0 FAIL; implementer median 6m15s, reviewer median 8m23s.
  Only 3 runs have measured times. Six runs from 2026-11-07 record 0 steps,
  so the ledger is too thin to compare models.
- No ledger run in this project used a Haiku or terra implementer. The only
  Haiku implementer note is S282 (another project, wave 7): "claude/haiku,
  about 30 minutes", with no quality result recorded.
- Token cost per role is not in the ledger. ork keeps usage in its run
  records under `~/.local/state/ork`, outside this workspace; not read.

### F7. Would file-by-file have fit recent effort 1 slices?

Diff stats from `git show --stat` of each slice commit:

- **S306** (S, effort 1, `d6d677b`): one code file (`internal/tui/keys.go`,
  17 lines). Its test is in `internal/tui/report_test.go` (103 lines), not a
  `keys_test.go`. Plus 4 goldens, `docs/dashboard.md` and `CHANGELOG.md`.
  Every other file depends on the one code change. Nothing to parallelize.
- **S229** (S, effort 1, `fc8bbfa`): 18 files, seven independent items.
  Items group by package: `build.go` with `wave_test.go`; `landed.go` with
  `landed_test.go`; `runs.go` with `render_test.go` and `file_test.go`;
  `engine/approval.go` with `renders_test.go`. But three groups share the
  `internal/daemon` package, and `docs/design.md`, `docs/state.md`,
  `docs/dashboard.md` and `CHANGELOG.md` take text from several items.
- **S284** (S, effort 2, `e545796`): test-only. A shared helper in
  `internal/engine/harness_test.go` is used by the other test files. The
  files are coupled by design.
- S279 (M, effort 2): 17 files, one concern spread through engine, runrec,
  slicercli and four docs. Not a candidate.

Where file-by-file breaks in this repo: Go compiles and tests a package
as a unit, so a part's `go test` sees other parts' half-done edits; tests are
not one per file (S306); `go test ./internal/tui -update` rewrites every
golden in the package; `CHANGELOG.md` and docs are touched by 3 of these 4
slices; shared helpers (S284) make "disjoint files" a false promise.

Bag-style slices (later notes, leftovers) are the best fit: 14 open items
match "later notes|leftover|sweep|notes" in the title, 8 of them size S,
effort 1 (S220, S259, S227, S228, S230, S231, S232, S253). Their item-level
independence has not been checked.

## Options

Each option keeps the reviewer and gate rows and one gate on the combined
diff.

**A. Cheap-first whole slice with a quality click up.** One implementer per
slice. For effort 1, the chain starts at a cheap row (haiku, later terra or
local) and moves up after review or gate failures. No new role, no planner,
no merge.
- Pro: most of the token saving; smallest change to the engine (the chain
  and `currentEntry` exist); works on every effort 1 slice, coupled or not.
- Con: no wall time gain (wall time is not in the roles, F6); needs a ladder
  ordered by strength, which the matrix order is not (F4).

**B. Cheap split (S61 shape).** A lead (strong or mid row) reads the spec,
proposes parts grouped by Go package or spec item, and owns tests that span
parts, goldens, docs and `CHANGELOG.md`. Parts on cheap rows edit only their
own files, in the slice worktree, in parallel when disjoint, at most 3. ork
runs the full check on the combined diff, then one review and one gate.
- Pro: matches the contract (split); parallel; one review and one gate.
- Con: S61 is M and parked in v1.2; a lead round plus N part rounds add
  briefing overhead to small slices; few slices cut cleanly (F7).

**C. Contract slivers (S62 shape).** Tracked sliver items, each with its own
review and gate, then a combined gate.
- Pro: already specified by slicer; each sliver is visible in the roadmap.
- Con: N reviews and N gates cost more than one implementer for most effort
  1 slices, which the contract itself forbids ("only when that costs less").
  S62 is L, effort 3, and depends on S61.

**D. One worktree per part, merged by ork.** Not recommended. The contract
puts parts in the slice worktree (relay text, Order), and merging adds
conflict handling that slicer's merge drivers do not cover for code.

**Recommended order:** A first, then a hand-run pilot of B, then build B
only if the pilot pays. Leave C parked.

### Escalation as matrix data

Two places could hold the ladder:

1. **slicer**: a row field or a rule slicer exposes as data (for example a
   `tier` or `escalate_to` per implementer row). The matrix is slicer's
   (`docs/boundaries.md:14`), so this is the clean home, but it needs a
   slicer item first. None exists (F1).
2. **ork registry**: a per-project ordered list of implementer row ids, like
   `spec_check_rows` (S258, `internal/registry/registry.go:252-256`). Faster
   to build; ork-only.

Either way the rule would read something like: start at the first row of the
ladder that matches the slice; after K gate FAILs, or J fix rounds with
`fix` findings, move up one row for the rest of the run (sticky, as adj 115);
never move down; vendor fallback (adj 156) still applies within a tier.
K and J are owner questions (Q5). u8 says K = 2.

## Pilot plan

Run it by hand first, as the outer session, so no ork code is needed to
learn whether this pays.

1. **Pick 8 slices**: open, size S, effort 1, spec check Ready, from the
   candidates in F7 or others the owner picks. Pair them by likeness (two
   bag-style, two single-concern TUI, and so on).
2. **Arms.** In each pair, one slice runs the baseline (r3 sonnet high, the
   implementer that runs on this host) and one runs the test arm:
   - arm A: whole slice on r8 haiku xhigh, click up to r3 sonnet high after
     two gate FAILs or two fix rounds;
   - arm B (only on bag-style slices): a sonnet high lead with haiku xhigh
     parts (r4), cut by package or item, per the contract's split.
   Reviewer, UI pass and gate stay on their current rows in every arm.
3. **Record** per run: first-gate verdict, fix rounds, gate FAILs, click-ups,
   implementer and total tokens per vendor, wall time per role, owner
   minutes, later-notes count, and any defect found after landing within two
   weeks. Record every role step in the ledger (`relay-run step`), so
   `slicer relay-run stats` has the data that F6 lacks.
4. **Success** (*proposed* thresholds for the owner to set):
   - no gate FAIL that the baseline arm would not have had, and no
     post-land defect;
   - fix rounds per slice no more than baseline plus 0.5;
   - total tokens (all roles, review rounds included) at least 30 percent
     lower than baseline;
   - wall time no more than baseline plus 20 percent.
5. **Stop early** if two test-arm slices need a click-up and still fail a
   gate, or if review tokens grow by more than the implementer tokens saved.
6. **After S301 and S304**, repeat arm A with a local row as the first rung.
7. Write the result as a review doc beside the 2026-10-25 retrospective and
   bring the defaults question (Q2) back to the owner with the numbers.

## Proposed slices

Sizes and trees are *proposed*.

| # | Title | Size | Tree | Depends on |
|---|---|---|---|---|
| P1 | Pilot: cheap-first and cheap-split on 8 effort 1 slices, by hand, with full ledger steps | S (docs) | none | Q1, Q2 answered |
| P2 | Implementer ladder: move up a tier after K gate FAILs or J fix rounds (ladder from Q6's home) | M | engine, daemon | Q5, Q6; slicer item if Q6 = slicer |
| P3 | Show the ladder and click-ups in the plan, the run pane and the report | S | tui, cli | P2 |
| P4 | Re-scope S61 to cheap parts cut by package or item; lead owns docs, goldens, CHANGELOG | M to L | engine, daemon, roles | P1 result, Q1, Q4 |
| P5 | Local first rung: local row as ladder rung 1 with fallback to haiku | S | daemon | S301, S303, S304, P2 |
| P6 | Effort 2 eligibility, only if P1 and P4 data support it | S | daemon | P4 data, Q3 |

S62 (contract slivers) stays parked in v1.2. If Q1 picks the split shape,
S62's `--slivers` wording in u7 and in `docs/orchestrator-parity.md:61` is a
later cleanup.

## Risks

- **Fix rounds eat the saving.** 23 of 28 strong-model runs needed a fix
  round (F6); each extra round is a Grok or opus review. Count all roles'
  tokens, not only the implementer's.
- **Missed defects.** A cheap diff gives the reviewer more to find. Keep the
  reviewer and gate rows and one gate on the combined diff.
- **Go package coupling.** Parallel parts in one package see each other's
  half-done edits. Cut by package, or run same-package parts in sequence.
- **Shared files.** Docs, goldens and `CHANGELOG.md` drift unless one role
  (the lead) owns them.
- **Escalation runs down.** The fallback chain is by preference and can go
  sol to terra (F4). A quality ladder must be ordered by strength.
- **Weak trigger.** Effort is a filer's estimate, not separability (F3). Use
  it to rule slices out, not in.
- **Host limits.** Codex and local do not run here (F5). A ladder whose cheap
  rungs cannot start falls to the strong rung and saves nothing.
- **Caps.** N parts multiply vendor sessions inside one slot (S61). A strong
  lead with local parts is not covered by adj 192.
- **Contract drift.** "Always on" conflicts with Setup ("The default is no",
  the owner confirms the plan); it needs an owner decision and maybe a slicer
  change.
- **Thin evidence.** 10 ledger runs with few steps (F6); no measured Haiku
  or local implementer run in this project.

When slivering must not apply (*proposed*): effort 3; any slice with an open
owner question or not spec-check Ready; a slice whose Files put all code in
one package; a slice that changes a wire, a state file or a shared type;
UI slices whose goldens span parts; and any run where a cheap rung cannot
start on the host.

## Questions for the owner

None of these keys is in adj (`adj check`: not found). Each would be asked
with `adj ask` and `--ref` to the slices above.

1. **`slice.sliver-shape`**: what does ork's slivering run?
   (a) split shape: lead plus cheap parts, one review and gate;
   (b) contract slivers: tracked items, a review and gate each;
   (c) neither: cheap-first whole slice only (option A).
   *Recommend (c) now, (a) after the pilot.*
2. **`slice.sliver-default`**: default on or off?
   (a) off, opt-in per run (S61's decision);
   (b) on for eligible slices, owner can turn it off per run;
   (c) on after the pilot meets its thresholds.
   *Recommend (a), then (c) on data.*
3. **`slice.sliver-eligibility`**: which slices qualify?
   (a) effort 1 only;
   (b) effort 1, Ready, and two or more package or item groups;
   (c) also effort 2 when every part is effort 1 (contract u6).
   *Recommend (b).*
4. **`slice.part-unit`**: what is a part?
   (a) one file; (b) one Go package (directory); (c) one spec item, with its
   files. *Recommend (b), or (c) for bag-style slices.*
5. **`roles.quality-escalation`**: when does the implementer move up?
   (a) after 2 gate FAILs (u8);
   (b) after 1 gate FAIL or 2 fix rounds;
   (c) never automatic: pause and ask.
   *Recommend (b): at most one wasted gate; stricter than u8 for quality.*
6. **`roles.ladder-source`**: where does the ladder live?
   (a) slicer matrix field or rule as data (file a slicer item);
   (b) ork registry list of row ids per project, like `spec_check_rows`;
   (c) the matrix order as is.
   *Recommend (a), with (b) as a stopgap if slicer's item waits. Not (c):
   the order is by preference (F4).*
7. **`roles.review-floor`**: may reviewer or gate rows ever change for a
   slivered run? (a) never; (b) only up. *Recommend (a).*
8. **`roles.local-model.mixed-run-cap`**: a strong lead with local parts:
   (a) the run holds a normal slot, local parts add local-pool slots;
   (b) the run counts as local (adj 192 extended). *Recommend (a).*

## Evidence and sources

Read-only commands, 2026-11-07, on this worktree: `slicer ai relay` (v11);
`slicer relay-roles show` and `recommend S306` (split no and yes);
`slicer find` (sliver, split, parts, escalat, local) here and in
`../slicer` (sliver, split part, escalat, tier, ladder); `slicer show` S61,
S62, S229, S260, S282, S284, S294, S301, S303, S304, S306, S307 and slicer
s445; `slicer list --json --lean` with and without `--all`;
`slicer relay-run stats` and `list`; `adj show` 20, 25, 33, 46, 86, 87,
115, 124, 135, 146, 156, 172, 177, 179 to 181, 183, 186 to 189, 192;
`adj check` on each key in Questions; `git show --stat` d6d677b, fc8bbfa,
e545796, 706c47d.

Not verified:

- Token cost per role and model (in ork run records outside the workspace).
- Whether the bag-style candidates in F7 split cleanly.
- Any measured local-model result: `.scratch/local-eval/report.md` was
  absent when this was written.
- How many review rounds S306 had: the ledger records one implementer step
  only.
