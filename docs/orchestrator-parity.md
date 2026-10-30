# Orchestrator parity

Can ork do every job the orchestrating outer session does today, so the owner
never needs that session? This document lists each job, says whether ork does
it (exists, partial or missing), names the ork command, key or view, cites the
evidence, and proposes a slice for each gap.

Unmet gap titles, sizes and UI flags below remain *proposed* unless an
owner decision or adj record decides them. Met rows cite the implementation.

## How this was checked

- The jobs come from the S165 brief and from how the outer session runs a
  slice relay (`slicer ai relay` is the contract; it is not copied here).
- Each "exists" or "partial" cites code (`path:line`) or a document. Code
  citations and the completed gap statuses were re-checked for S183.
  Where only a design document supports a claim, the row says so.
- Status words: **exists** (ork does the job end to end), **partial** (ork
  does part of it; the row says what is missing), **missing** (ork has no
  command, key or code for it).
- A gap is a UI slice when it touches `internal/tui`, its golden views,
  `docs/dashboard.md`, `internal/cli` output or a `ui.*` adj key (`AGENTS.md`,
  Project Rules). Every CLI command counts, so almost every gap here is one.
- Not verified: how the engine behaves on a real vendor failure (no vendor
  was run), and the exact hint, golden and layout output of the TUI (the TUI
  was read, not run).

## Summary

| Status | Jobs |
|---|---|
| exists | 32 |
| partial | 3 |
| missing | 6 |

Total: 41 jobs in 7 groups (sections 1 to 7). The counts are of the rows in
the tables below.

## 1. Preflight

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 1.1 | Read a slice's spec and notes | exists | Roadmap `enter` opens the item pane (`internal/tui/roadmap.go`, `docs/dashboard.md:1400-1416`). Waves `enter` opens the same detail (`docs/dashboard.md:1219-1227`). The daemon method `item.show` (`internal/daemon/wire.go:30`). No CLI command prints a spec; `slicer show` does | none |
| 1.2 | Check that a spec is ready: files named, failing tests named, cited files exist | exists | The read-only spec-check role reads each selected spec and reports Ready or owner questions (`internal/roles/prompts/spec-check.md`). Wave launch runs it before confirmation. Single-slice CLI launch also runs it (S167; `internal/cli/run.go:127-135`), as does the Roadmap launch (`internal/tui/launch.go:288-295`) | none |
| 1.3 | Trace failing tests to files and fix the Files list | missing | The spec-check role is read-only and edits nothing (`internal/roles/prompts/spec-check.md`, Rules). It can only ask the owner. No ork code edits a Files section | covered by 1.5 |
| 1.4 | Batch owner questions: 2 to 4 options and a recommendation | partial | Spec-check questions have this shape (title, numbered options, `Recommend`, decision key) (`docs/design.md:519-542`, `docs/design.md:205-229`). The owner answers in one sitting: Waves view `1`-`4`, `t`, `s` (`internal/tui/keys.go:272-286`), or on stdin in `ork wave start` (`docs/design.md:1104-1117`). Partial because ork batches only the questions that roles raise. The owner can file other questions with `ork adj ask` (6.2) | adj questions are met in 6.2 |
| 1.5 | Fill a thin spec (slicer `edit` of sections) | missing | ork calls `slicer edit --append` only to add `Owner decisions` to the Implement section (`internal/slicercli/write.go:256-268`, `internal/engine/speccheck.go:446`). `scope.v1-ork-edits` (adj 173) keeps spec drafting after v1.0; filing, notes and adj answers are in v1.0 | "Spec drafting round: a role proposes section text, the owner approves, ork runs slicer edit" (L, UI) |
| 1.6 | Record the owner's decisions as slice notes | exists | S171 adds `ork note PROJECT ITEM TEXT` (`internal/cli/note.go:16-58`); Roadmap `N` opens the note field (`docs/dashboard.md:1552-1567`). Spec-check answers still append to Implement (`internal/engine/speccheck.go:446`), and run answers stay in the run record | none |
| 1.7 | Promote notes to slices (slicer `add`, `promote`, `set`) | exists | S170 adds `ork new PROJECT TITLE`, including `--from ITEM --from-note N` and size and score flags (`internal/cli/new.go:19-74`). It calls slicer add and promote (`internal/engine/newslice.go:49-110`). Roadmap `n` files a slice from a request. Later findings still use `ork file RUN N` or `f` in the run pane (`internal/engine/later.go:89-122`) | none |

## 2. Setup

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 2.1 | Show the role matrix | exists | `ork roles matrix PROJECT` shows slicer's matrix and which rows ork uses (`internal/cli/roles.go:69-89`). Roadmap `m` opens the matrix pane (S168; `internal/tui/roadmap.go:67-85`) | none |
| 2.2 | Edit the matrix (`relay-roles add/set/remove/move/rule-edit`) | missing | No ork code calls these. The matrix is slicer's (`docs/glossary.md`, `docs/boundaries.md`), so ork would only call slicer's commands. Deferred to v1.1 (adj 173) | "Role matrix editor" (L, UI) |
| 2.3 | Pick the lineup for one slice | exists | S166 adds lineup and budget overrides to the launch paths. `ork run` registers the role and budget flags (`internal/cli/run.go:51-52`, `internal/cli/lineup.go:27-35`); `ork wave start` accepts them for every item (`internal/cli/wave.go:94,108`). Waves `e` and the Roadmap launch confirmation's `e edit lineup` use the run-only editor (`internal/tui/keys.go:269-271`, `internal/tui/lineup.go`). Neither edits the matrix | none |
| 2.4 | One Codex slot at a time, Claude as stand-in when Codex is out of credits | exists | S169 shares one Codex slot across the daemon (`internal/daemon/vendors.go:17`, `internal/daemon/runs.go:164-168`). Rounds wait for a slot and skip vendors remembered as out when a later usable row exists (`internal/engine/round.go:187-211`). Rate-limit and out-of-credits exits mark a vendor out for 60 minutes; owner marks last until `ork vendor back` or daemon restart (`internal/engine/vendors.go:17-23`, `internal/engine/round.go:360-372`). The matrix must supply the Claude stand-in. Claude still has no measured rate-limit class (`internal/agent/claude/types.go:24-28`) | none |
| 2.5 | Confirm the setup with the owner before the worktree exists | exists | The plan shows worktree, branch, check command, budgets, lineup and permissions, and asks `Start this slice? [y/N]` (`docs/design.md:1173-1188`, `internal/cli/run.go`). The wave plan does the same per item (`internal/cli/wave.go`) | none |
| 2.6 | Choose how the relay ends: handoff, commit or landed | exists | S177 adds per-run `--stop-after land\|commit` (`internal/cli/cli.go:43,52`, `internal/cli/lineup.go:29`). Commit stops with the worktree and branch kept; land continues through landing (`internal/engine/states.go:650-656`, `docs/design.md:375-380`). The finish step follows slicer's `implement_finish` setting (`internal/engine/states.go:663-686`); there is no separate handoff stop point | none |
| 2.7 | Split a slice into parts or slivers | missing | The `split` input only changes the matrix recommendation (`internal/slicercli/write.go:94-101`). `rg sliver` finds only a field in `internal/slicercli/types.go`; no engine code runs parts or a sliver chain. Matrix rule u7 names `ork run PARENT --slivers`; `ork run` has no such flag (`internal/cli/cli.go:43-50`) | "Run a slice as parts or a sliver chain" (L, UI) |

## 3. Runs

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 3.1 | Create the worktree and branch, begin the ledger run | exists | `git worktree add ... -b` (`internal/engine/states.go:706`), `slicer relay-run begin` (`internal/engine/states.go:36`, `internal/slicercli/write.go:114-130`) | none |
| 3.2 | Spawn implementer, reviewer, UI pass, architect (size L), gate; run checks | exists | State machine in `docs/design.md:249-424`; the roles run as vendor processes (`claude`, `codex`) (`docs/design.md:665-692`). The UI pass runs beside the reviewer on a UI slice (`docs/design.md:295-319`) | none |
| 3.3 | Fix rounds and the finish step | exists | Fix round on findings, FAIL and owner `F` note (`docs/design.md:324-368`, `internal/cli/cli.go:58`). Finish step after approval (`internal/engine/states.go:663`) | none |
| 3.4 | Record ledger steps and heartbeats for each role round | exists | `slicer relay-run step` per round, a heartbeat every 10 minutes (`internal/engine/round.go:20,299,614-640`, `internal/slicercli/write.go:131-160`). No ledger step after the commit (`docs/design.md:754-763`) | none |
| 3.5 | Run 2 or 3 slices in parallel | exists | Global cap defaults to 4 (`internal/daemon/runs.go:47`), project `cap` (`ork projects set --cap`), queueing oldest first (`internal/daemon/runs.go:793-835`). S174 adds `ork settings set --global-cap N` (`internal/cli/settings.go:44-65`) | none |
| 3.6 | Stop a run that is going nowhere; cancel a wave | exists | `x`, `ork stop RUN|WAVE` (`internal/cli/cli.go:59`). The engine also has `Reject` for an awaiting-approval run (`internal/engine/engine.go:416-425`); no wire method or command reaches it (`internal/daemon/runs.go:30-43`), and `stop` covers the need | none |
| 3.7 | Order a wave by dependencies | exists | Suggestions come from slicer's `plan-parallel` (`internal/daemon/reads.go:53-74`). Before admitting any member, `startWave` reads each selected item's dependencies and refuses the whole selection if an item is blocked, even by another selected item (`internal/daemon/runs.go:626-695`). It keeps the supplied item order; it does not wait for an in-wave blocker to land. This meets the blocked-item refusal gap | none |

## 4. Owner questions during runs

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 4.1 | Ask the owner and route the answer to the right role | exists | A role's `## Owner needed` pauses the run; the answer resumes the same role (`docs/design.md:543-549`). TUI: Questions view, `1`-`4`, `t`, `enter`. CLI: `ork answer RUN TEXT` (`internal/cli/run.go:251-258`) | none |
| 4.2 | Blocker questions | exists | `[owner blocker]` findings pause the round with a question (`docs/design.md:335-346`). Non-blocking owner findings are answered with `o` (`internal/tui/keys.go:87`) | none |
| 4.3 | Questions from the outer session about design, order or scope | exists | S172 adds `ork adj ask` for a new question and `ork adj answer` for an open record (`internal/cli/adj.go:34,138`). S185 adds answers in the Questions and Decisions panes: `1`-`4` or `t`, then y/N confirmation (`internal/tui/decisionanswer.go:47-108`). The owner supplies the question | none |

## 5. Landing

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 5.1 | Merge main into the worktree, render, re-check | exists | `git merge BASE` in the worktree, `slicer render`, commit, re-run checks (`internal/engine/land.go:136`, `docs/design.md:391-402`) | none |
| 5.2 | Fast-forward merge, `slicer land`, push, remove worktree and branch | exists | `internal/engine/land.go:61,65,83,90`. Push needs `ork projects set NAME --push on` | none |
| 5.3 | End the ledger run | exists | `RelayEnd` (`internal/engine/engine.go:567-573`) | none |
| 5.4 | Resolve merge conflicts | partial | A conflict or red re-check goes to one implementer fix round; a second block stops the run with the worktree kept (`docs/design.md:417-424`). Changelog conflicts are listed as after v1 (`docs/manual-steps.md:7-9`, `:62`). Missing: resolving a conflict the fix round cannot | "Changelog conflict resolver" (M, UI) |
| 5.5 | Land a slice that ork did not run | partial | Ready rows show landing commands and `c` removes a merged worktree (`docs/dashboard.md:586-644`, `internal/tui/keys.go:75`). ork does not land a branch it did not run | none proposed; the outer session lands only branches it ran |

## 6. Bookkeeping and decisions

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 6.1 | File later notes as new slices | exists | `f` in the run pane, `ork file RUN N` (`internal/cli/run.go:235-247`, `internal/engine/later.go:103-117`) | none |
| 6.2 | `adj ask`, `adj check`, `adj decide` for an option the owner picked | exists | S172 adds `ork adj ask` and `ork adj answer` (`internal/cli/adj.go:34,138`). Asking with a key first checks it; answering re-reads the open record, validates the option, and records the owner and dated ork context (`internal/engine/adjrecord.go:40-76,112-127`). CLI answers show the options and require y/N; no `--yes` (`internal/cli/adj.go:96-128`). S185 adds the same confirmed answer from Questions and Decisions (`internal/tui/decisionanswer.go:47-108`). Both need `ADJ_DECIDED_BY`. Run answers with a decision key are filed in adj when the owner turns filing on (`ctrl+a`; `internal/engine/answer_adj.go:18-30`, `internal/tui/keys.go:113`). ork never rejects or supersedes a record | none |
| 6.3 | Browse decisions and the inbox | exists | Decisions view, Questions view (`docs/dashboard.md:1112-1148`, `:1579-1630`) | none |
| 6.4 | Keep run state across a context reset | exists | Every run is a record on disk; the daemon reconciles unfinished runs on start (`docs/state.md:141-417`, `docs/design.md:158-164`). The outer session's other memory (owner preferences, trial notes) has no ork home | none proposed |
| 6.5 | Keep trial notes and retrospectives | missing | ork has no place for owner notes about the process. They live in docs and slicer notes today | none proposed; use 1.6 |

## 7. Monitoring and config

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 7.1 | Status across projects | exists | Overview, Running entry, `ork status` (`internal/cli/daemoncmds.go:25-100`). Bell and sidebar badge on new needs (`internal/tui/model.go:1039-1107`) | none |
| 7.2 | Edit registry fields | exists | S174 adds `environment_lines`, per-role `permissions` and `vendor_env` to `ork projects set` (`internal/cli/daemoncmds.go:221-291`). `ork settings show` and `ork settings set` expose `global_cap` and `daemon_idle_minutes` (`internal/cli/settings.go:15-100`) | none |
| 7.3 | Edit `.claude/settings.json` allow rules | missing | Nothing in `internal/`, `cmd/` or `docs/` mentions `settings.json`. ork builds each role's allowlist from the registry `permissions` field or a built-in default (`internal/engine/perm.go:24-36,58-69`) and passes it as `--allowedTools` (`internal/agent/claude/args.go:59`). Codex ignores the allowlist (`internal/agent/codex/codex.go:3-4`). The registry field has a command (7.2). Whether a project needs `.claude/settings.json` rules once ork runs the roles is not verified | none proposed until the owner says it is needed |
| 7.4 | Stale runs and ledger cleanup | exists | ork stops, dismisses and retries its own runs. S175 adds `x end run` in a stale ledger run's pane with y/N confirmation (`internal/tui/keys.go:575-584`, `internal/tui/ledgerend.go:44-60`; adj 174). The daemon re-reads the ledger, refuses a non-stale run or one owned by an unfinished engine run, and ends it stopped with running steps failed (`internal/daemon/runs.go:1445-1480`). S182 adds the watchdog: after 30 minutes of silence it cancels the role context, records exit `hung`, fails the ledger step and pauses as `lost`; the owner retries (`internal/engine/watchdog.go:14-24,136-158`) | none |
| 7.5 | Upgrade slicer and adj, rebuild ork, then check `ork doctor` | exists | Met by S178: `ork doctor --upgrade` prints commands for slicer, adj and ork, even when none is behind (`internal/cli/doctor.go:280,360,420`). The owner runs them. ork performs no pull or build. This meets the owner's revised scope below | none |
| 7.6 | Show live output and tokens of a run | exists | Run pane tail, tokens, cost (`docs/dashboard.md:1730-1767`) | none |
| 7.7 | Read a raw role log or report | exists | S176 adds `run.report`, which reads a report named in the stored run record (`internal/daemon/runs.go:1390-1407`). Run pane `p` opens the report and cycles through available reports (`internal/tui/keys.go:91,591`, `internal/tui/report.go:30-37,102-118`). This covers role reports; it does not expose arbitrary raw log files | none |

## 8. Things only the owner does

These are owner acts, not orchestrator jobs, and stay with the owner:
deciding in adj (`AGENTS.md`, Decisions), approving a wave and each landing,
and choosing what to build.

## Prioritized gaps

The original order is kept so the owner decisions below retain their gap
numbers. Met rows record the later slices; other titles remain *proposed*.

| # | Gap slice (proposed) | Size | UI | Why this order |
|---|---|---|---|---|
| 1 | Lineup and budget overrides on every launch path (2.3) | M | yes | Met by S166: lineup and budget overrides on CLI and TUI launch paths |
| 2 | Spec check on single-slice launch (1.2) | S | yes | Met by S167 and the Roadmap launch: the spec check runs before confirmation |
| 3 | Role matrix view (2.1) | S | yes | Met by S168: `ork roles matrix` and Roadmap `m` |
| 4 | Per-vendor slot cap and vendor-out memory (2.4) | M | yes | Met by S169: vendor slots and vendor-out memory; Claude needs a matrix fallback row |
| 5 | File a slice from a note or a request (1.7) | M | yes | Met by S170: `ork new` files from a request or a slice note |
| 6 | Add a note to a slice (1.6) | S | yes | Met by S171: `ork note` and Roadmap `N` |
| 7 | Answer and ask adj records in ork (6.2, 4.3) | M | yes | Met by S172 and S185: CLI ask/answer and confirmed TUI answers |
| 8 | Role matrix editor (2.2) | L | yes | Deferred to v1.1 by adj 173; the matrix view exists |
| 9 | Registry settings for the remaining fields and the global cap (7.2) | M | yes | Met by S174: `ork projects set` and `ork settings` |
| 10 | End a stale ledger run from the run pane (7.4) | S | yes | Met by S175: `x end run` confirms ending a stale ledger run (adj 174) |
| 11 | Open a role report in the run pane (7.7) | S | yes | Met by S176: `run.report` and run pane `p` |
| 12 | Per-run stop point: after commit, or land (2.6) | S | yes | Met by S177: per-run `--stop-after land\|commit` |
| 13 | Upgrade command hints (7.5) | M | yes | Met by S178: `ork doctor --upgrade` prints commands; the owner runs them |
| 14 | Spec drafting round (1.5, 1.3) | L | yes | Deferred to v1.1 by adj 173 (`scope.v1-ork-edits`) |
| 15 | Changelog conflict resolver (5.4) | M | yes | Listed as after v1 |
| 16 | Run a slice as parts or a sliver chain (2.7) | L | yes | Largest; the matrix already has slivers (r8) |
| 17 | Refuse or warn on a blocked item at wave start (3.7) | S | yes | Met: wave start refuses blocked selections before admitting any member (3.7) |
| 18 | Watchdog for a hung role (7.4) | M | yes | Met by S182: silent roles pause as lost after the watchdog kills their process group |

Gaps 7 and 13 are met under the owner decisions below. Gap 14 remains
deferred to v1.1 by adj 173; it needs no new scope decision.

## What the owner still needs an orchestrator session for today

- Changing the role matrix. ork shows it and supports per-run overrides.
- Writing or fixing a thin spec. ork files slices and notes, but spec
  drafting stays later under adj 173.
- Running design interviews that no role raised.
- Running slivers and split slices.
- Pulling and rebuilding slicer, adj and ork with the commands that
  `ork doctor --upgrade` prints.

The tables name the remaining gaps in the relay, landing and cleanup.

## Owner decisions (2026-10-29)

Given by the owner after this analysis. Gap numbers are the rows of the
prioritized table.

- Every gap is a v1.0 slice, except gap 8 (role matrix editor) and gap 14
  (spec drafting round), which are v1.1.
- Gap 7 (answer and ask adj records in ork): yes. ork runs `adj decide --by
  <owner> --why <cites the ork session and the date>` only for an option the
  owner picks in a prompt that names it. ork never runs `adj reject` or
  `adj supersede`.
- Gap 13 (upgrade): `ork doctor` reports the slicer and adj versions and
  prints the exact commands. The owner runs them. ork does no network access
  and no build.

Adj 173 (`scope.v1-ork-edits`) now records the v1.0 edit scope, including
asking and answering adj records. For points without an adj record, cite
the owner's words of 2026-10-29 above.

## S183 documentation corrections

S183 corrects the stale Waves status in README, development status in
PROJECT, landing rules in boundaries, check timeout in state, the missing
`land` state in design, and the mouse promise in dashboard. The contract
wording in boundaries already said v9 and v10 and needed no change (S198 moved
it to v10 and v11).

The dated notes and review update matrix viewing (S168), vendor slots and memory
(S169), registry settings (S174), upgrade hints (S178),
stale ledger cleanup (S175), reports (S176), and the watchdog (S182).
Adj asking and answering also exist (S172, S185). The v1.0 sweep also
updates single-slice spec checks, notes, slice filing, launch overrides,
stop points, and blocked-item refusal against the code.
The code citations above are the evidence for these updates.
