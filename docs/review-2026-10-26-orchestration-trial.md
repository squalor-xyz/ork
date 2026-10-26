# Orchestration trial retrospective

2026-10-26. Covers the first five relays run with the outer session as the
only orchestrator (S95, S138, S139, S92, S93), compared with the 97 earlier
relay runs that used one orchestrator subagent per slice. Sources:
`slicer relay-run list --json` (102 ended runs), the "Orchestration trial"
notes on each trial slice, and notes kept during the run. Durations are
from run begin to the last ledger update, which is the landing.

Everything ork itself would add here is *proposed* until its adj record is
decided.

## The short version

One orchestrator worked. In five runs there were no stalls, every owner
question went to the owner directly, and no landing had a merge conflict.
Wall clock was a little shorter than the earlier Codex runs, but five runs
are too few to call that a speed gain. The real gain is fewer moving parts:
no hand-backs to route, no answers to relay, one place that knows the state
of every slice.

ork already works this way. Its engine is the orchestrator, and it launches
no orchestrator agent (`docs/boundaries.md:47-50`, `docs/design.md:406`).
The trial is evidence for that design, not a reason to change it. What it
does suggest is running one slice at a time by default.

## Measures

| Slice | Run | Minutes | Owner questions (before / during) | Stalls | Fix rounds | Merge conflicts | Compactions |
|---|---|---|---|---|---|---|---|
| S95 | run-20261026-172757-ae20 | 18 | 0 / 3 | 0 | 0 | 0 | 0 |
| S138 | run-20261026-175512-e95d | 45 | 2 / 4 | 0 | 1 | 0 | 0 |
| S139 | run-20261026-184018-26e1 | 11 | 1 / 0 | 0 | 0 | 0 | 0 |
| S92 | run-20261026-185146-de28 | 15 | 0 / 0 | 0 | 0 | 0 | 0 |
| S93 | run-20261026-190634-c1cc | 27 | 0 / 0 | 0 | 1 | 0 | 1 |

All five used Codex gpt-6.1-sol medium as the implementer, and Codex was
never blocked. Five slices landed in a little over two hours of wall clock
(17:27Z to 19:33Z), and the runs themselves add up to 116 minutes, so the
outer session was rarely idle between slices.

## Compared with per-slice orchestrators

| Group | Passed runs | Median minutes |
|---|---|---|
| Trial, one orchestrator, Codex implementer | 5 | 18 |
| Subagent orchestrator, Codex implementer, all dates | 49 | 24 |
| Subagent orchestrator, Codex implementer, since 2026-10-24 | 20 | 28.5 |
| Subagent orchestrator, Claude implementer (Codex out of credits) | 25 | 63 |

How to read this:
- The fair comparison is with the Codex rows. The trial median is lower,
  but the sample is small and the trial slices may simply have been
  smaller. Treat speed as not proven.
- The Claude-implementer runs are slower, but they also ran in parallel and
  overlapped owner waits, so the implementer and the orchestration are
  confounded there.
- The long tails in the subagent group (S07, S27, S78, S79, S82: 642 to
  1270 minutes) are runs that waited overnight for the owner, not slow
  orchestration.
- Stopped runs: 7 in the subagent group (S21 three times, S29 twice, S50,
  S95), 0 in the trial.

What the ledger does not record, from the run notes and memory:
- Subagent orchestrators repeatedly handed back `waiting on` mid-gate
  (S145, S95) or sent empty interim hand-backs (around S107 and S111). Each
  one needed the outer session to notice and resume them. The trial had none,
  because there was no hand-back to send.
- Owner answers had to be relayed from the outer session to the
  orchestrator and then to the role. In the trial they went straight to
  the role's next brief.
- The parallel stretch on 2026-10-26 had three merge conflicts at landing
  (S121 and S85 in `view.go`; S119 merged main mid-relay with 11
  conflicts). The serial trial had none.

## What did not work as well

- **The outer session's context.** It holds every slice now. One compaction
  happened in five runs (during S93's fix round). Nothing was lost, because
  the state lives in the ledger, slicer notes and the helper scripts, but a
  longer queue will compact more often. That is a cost a daemon such as
  ork does not have.
- **Codex cannot write the shared ledger** from its sandbox. slicer falls
  back to the worktree ledger and absorbs it at `relay-run end` (S95 note).
  That works, but the live view lags until the run ends.
- **Codex resumes need a subshell `cd`**, the one exception to "never cd",
  because `codex exec resume` has no `-C`.
- **The finish step needs one more Codex resume** for `slicer done`. It is
  cheap (under a minute) but is a hop the outer session could run itself.
- **relay-reviewer has no Skill tool**, so the UI pass became its own role
  on every size (rule u10). That came from this trial and is already done.

## Recommendations

### O1. Keep one orchestrator (done; no change for ork)

Keep the global rule as written (agents 36dff4b). For ork, the engine
already is the one orchestrator. Do not add an orchestrator agent to ork.

### O2. One slice at a time by default (*proposed*, adj ask)

ork's global run cap defaults to 4 (`docs/design.md:204`). The trial ran one
at a time with no conflicts and no idle owner. The parallel stretch before
it had three conflicts and the owner said "there's too much going on in
parallel". Proposed: default the cap to 1, and let the owner raise it per
wave or in config. Asked as adj 172 (`engine.default-cap`).

### O3. An orchestrator text for a session that orchestrates every slice (*proposed*, slicer item)

`slicer ai relay-orchestrator` starts "You run one slice for the outer
session that spawned you", and its Rules forbid `relay-run begin` and
`end`, landing and pushing. When the outer session is the orchestrator, those
rules do not fit, and the hand-back rules (s402) do not apply. Proposed: a
variant of the text (for example `--session`) where the orchestrator owns
begin, end and landing, briefs roles directly, and puts owner questions to the
owner. Filed as slicer s409.

### O4. Keep measuring

Keep the "Orchestration trial" note on each slice for the next five runs,
so the speed comparison has more than five data points. The stalls and
owner-question counts are the measures ork's own stats (S123) should keep.
