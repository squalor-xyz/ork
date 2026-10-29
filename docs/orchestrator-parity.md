# Orchestrator parity

Can ork do every job the orchestrating outer session does today, so the owner
never needs that session? This document lists each job, says whether ork does
it (exists, partial or missing), names the ork command, key or view, cites the
evidence, and proposes a slice for each gap.

All slice titles, sizes and UI flags below are *proposed*. They are input for
the owner's decision, not tracked work. None of them is decided in adj.

## How this was checked

- The jobs come from the S165 brief and from how the outer session runs a
  slice relay (`slicer ai relay` is the contract; it is not copied here).
- Each "exists" or "partial" cites code (`path:line`) or a document. Code
  citations were read in the S165 worktree. Where only a design document
  supports a claim, the row says so.
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
| exists | 18 |
| partial | 14 |
| missing | 9 |

Total: 41 jobs in 7 groups (sections 1 to 7). The counts are of the rows in
the tables below.

## 1. Preflight

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 1.1 | Read a slice's spec and notes | exists | Roadmap `enter` opens the item pane (`internal/tui/roadmap.go`, `docs/dashboard.md:1275-1331`). Waves `enter` opens the same detail (`docs/dashboard.md:1101-1108`). The daemon method `item.show` (`internal/daemon/wire.go:25`). No CLI command prints a spec; `slicer show` does | none |
| 1.2 | Check that a spec is ready: files named, failing tests named, cited files exist | partial | The spec-check role reads a whole batch and answers `Ready` or asks questions (`internal/roles/prompts/spec-check.md`, `docs/design.md:661-689`). It runs only at wave launch: `ork wave start` and `l` in Waves (`docs/design.md:188-190`). `ork run` and the Roadmap launch never call it (`internal/cli/run.go`, `internal/tui/launch.go`: no spec call). Missing: a check on a single-slice launch | "Spec check on single-slice launch" (S, UI) |
| 1.3 | Trace failing tests to files and fix the Files list | missing | The spec-check role is read-only and edits nothing (`internal/roles/prompts/spec-check.md`, Rules). It can only ask the owner. No ork code edits a Files section | covered by 1.5 |
| 1.4 | Batch owner questions: 2 to 4 options and a recommendation | partial | Spec-check questions have this shape (title, numbered options, `Recommend`, decision key) (`docs/design.md:301-317`, `docs/design.md:191-199`). The owner answers in one sitting: Waves view `1`-`4`, `t`, `s` (`internal/tui/keys.go:253-267`), or on stdin in `ork wave start` (`docs/design.md:981-987`). Partial because ork batches only the questions that roles raise. ork cannot ask the owner a question the outer session thinks of, such as sequencing or scope | "Ad hoc owner question inbox" is in 6.2 |
| 1.5 | Fill a thin spec (slicer `edit` of sections) | missing | ork calls `slicer edit --append` only to add `Owner decisions` to the Implement section (`internal/slicercli/write.go:236-243`, `internal/engine/speccheck.go:374`). `docs/dashboard.md:3-5` and `:1330` say roadmap editing is not in v1 (`scope.v1`), so this needs an owner decision first | "Spec drafting round: a role proposes section text, the owner approves, ork runs slicer edit" (L, UI) |
| 1.6 | Record the owner's decisions as slice notes | partial | Spec-check answers are appended to the Implement section (`internal/engine/speccheck.go:374`). Run answers are kept in the run record. `slicer note` is called only for `Review FAIL` (`internal/engine/states.go:567`) and approval marks (`internal/engine/approval.go:132`). Missing: a note the owner types for any slice | "Add a note to a slice" (S, UI) |
| 1.7 | Promote notes to slices (slicer `add`, `promote`, `set`) | partial | ork files a later finding as a new slice with `slicer add` and `slicer promote` (`internal/engine/later.go:103,117`; `f` in the run pane, `ork file RUN N`). Scoring with `slicer set` is in the docs as proposed (S137, `docs/design.md:330-336`); `internal/slicercli/write.go:282` has the call. Missing: filing a slice that did not come from a run's finding | "File a slice from a note or a request" (M, UI) |

## 2. Setup

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 2.1 | Show the role matrix | missing | ork reads `slicer relay-roles show --json` only to build plans (`internal/slicercli/write.go:199`). No view, key or command shows the matrix. `ork roles print` prints role prompts, not the matrix (`internal/cli/roles.go`) | "Role matrix view" (S, UI) |
| 2.2 | Edit the matrix (`relay-roles add/set/remove/move/rule-edit`) | missing | No ork code calls these. The matrix is slicer's (`docs/glossary.md`, `docs/boundaries.md`), so ork would only call slicer's commands. Needs an owner decision on the surface | "Role matrix editor" (L, UI) |
| 2.3 | Pick the lineup for one slice | partial | Waves view `e` edits implementer, reviewer, architect and the two budgets for one run (`internal/tui/lineup.go`, `docs/dashboard.md:1180-1215`). The daemon accepts a lineup on `run.start` (`internal/daemon/runs.go:54-55`), but `ork run` has no flag for it (`internal/cli/run.go:48-50`) and the Roadmap launch has no editor (`internal/tui/launch.go`). Missing: lineup and budgets on `ork run`, `ork wave start` and the Roadmap launch | "Lineup and budget overrides on every launch path" (M, UI) |
| 2.4 | One Codex slot at a time, Claude as stand-in when Codex is out of credits | partial | Fallback exists per round: a `rate_limited` or `error` exit moves to the next matrix row (`internal/engine/round.go:329-330`, `docs/design.md:737-765`). The Codex classifier reads "You've hit your usage limit" and "out of credits" (`internal/agent/codex/codex.go:194`). Missing: (a) a per-vendor concurrency cap; the caps count runs, not vendors (`internal/daemon/runs.go:727-745`); (b) memory that a vendor is out, so each new run tries Codex first and falls back again; (c) Claude has no measured rate-limit class (`internal/agent/claude/types.go:24-28`), so a Claude limit falls through to `error` or a stop | "Per-vendor slot cap and vendor-out memory" (M, UI) |
| 2.5 | Confirm the setup with the owner before the worktree exists | exists | The plan shows worktree, branch, check command, budgets, lineup and permissions, and asks `Start this slice? [y/N]` (`docs/design.md:1019-1024`, `internal/cli/run.go`). The wave plan does the same per item (`internal/cli/wave.go`) | none |
| 2.6 | Choose how the relay ends: handoff, commit or landed | partial | The end is fixed: approval, finish, commit, land (`docs/design.md:344-391`). The finish step follows `slicer config implement_finish` (`internal/engine/states.go:653-668`). Missing: a per-run choice to stop after commit or leave landing to the owner | "Per-run stop point: after commit, or land" (S, UI) |
| 2.7 | Split a slice into parts or slivers | missing | The `split` input only changes the matrix recommendation (`internal/slicercli/write.go:96`). `rg sliver` finds only a field in `internal/slicercli/types.go:218`; no engine code runs parts or a sliver chain. Matrix rule u7 names `ork run PARENT --slivers`; `ork run` has no such flag (`internal/cli/cli.go:42`) | "Run a slice as parts or a sliver chain" (L, UI) |

## 3. Runs

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 3.1 | Create the worktree and branch, begin the ledger run | exists | `git worktree add ... -b` (`internal/engine/states.go:697`), `slicer relay-run begin` (`internal/engine/states.go:36`, `internal/slicercli/write.go:114-130`) | none |
| 3.2 | Spawn implementer, reviewer, UI pass, architect (size L), gate; run checks | exists | State machine in `docs/design.md:223-300`; the roles run as vendor processes (`claude`, `codex`) (`docs/design.md:623-689`). The UI pass runs beside the reviewer on a UI slice (`docs/design.md:269-275`) | none |
| 3.3 | Fix rounds and the finish step | exists | Fix round on findings, FAIL and owner `F` note (`docs/design.md:299-340`, `internal/cli/cli.go:50`). Finish step after approval (`internal/engine/states.go:653`) | none |
| 3.4 | Record ledger steps and heartbeats for each role round | exists | `slicer relay-run step` per round, a heartbeat every 10 minutes (`internal/engine/round.go:20,270`, `internal/slicercli/write.go:131-160`). No ledger step after the commit (`docs/design.md:696-699`) | none |
| 3.5 | Run 2 or 3 slices in parallel | exists | Global cap 4 (`internal/daemon/runs.go:44`), project `cap` (`ork projects set --cap`), queueing oldest first (`internal/daemon/runs.go:723-760`). The global cap has no command; it is a file edit (see 7.2) | in 7.2 |
| 3.6 | Stop a run that is going nowhere; cancel a wave | exists | `x`, `ork stop RUN|WAVE` (`internal/cli/cli.go:51`). The engine also has `Reject` for an awaiting-approval run (`internal/engine/engine.go:384-391`); no wire method or command reaches it (`internal/daemon/runs.go:29-40`), and `stop` covers the need | none |
| 3.7 | Order a wave by dependencies | partial | `wave.suggest` returns slicer's `plan-parallel` and `wave.overlap` returns `slicer deps --overlap` (`internal/daemon/reads.go:53-74`). `startWave` admits items in the order given, refuses a second member that touches `internal/engine`, `internal/daemon` or `cmd` (the self-host guard), and runs the spec gate, but no code there checks slicer dependency blocking (`internal/daemon/runs.go:597-645`; not found after searching, not tested) | "Refuse or warn on a blocked item at wave start" (S, UI) |

## 4. Owner questions during runs

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 4.1 | Ask the owner and route the answer to the right role | exists | A role's `## Owner needed` pauses the run; the answer resumes the same role (`docs/design.md:701-707`). TUI: Questions view, `1`-`4`, `t`, `enter`. CLI: `ork answer RUN TEXT` (`internal/cli/run.go:223-228`) | none |
| 4.2 | Blocker questions | exists | `[owner blocker]` findings pause the round with a question (`docs/design.md:313-318`). Non-blocking owner findings are answered with `o` (`internal/tui/keys.go:80`) | none |
| 4.3 | Questions from the outer session about design, order or scope | missing | ork shows adj open records read-only (`docs/dashboard.md:1018`). No form or command asks the owner a new question or answers an adj record | see 6.2 |

## 5. Landing

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 5.1 | Merge main into the worktree, render, re-check | exists | `git merge BASE` in the worktree, `slicer render`, commit, re-run checks (`internal/engine/land.go:136`, `docs/design.md:358-366`) | none |
| 5.2 | Fast-forward merge, `slicer land`, push, remove worktree and branch | exists | `internal/engine/land.go:61,65,83,90`. Push needs `ork projects set NAME --push on` | none |
| 5.3 | End the ledger run | exists | `RelayEnd` (`internal/engine/engine.go:533`) | none |
| 5.4 | Resolve merge conflicts | partial | A conflict or red re-check goes to one implementer fix round; a second block stops the run with the worktree kept (`docs/design.md:383-390`). Changelog conflicts are listed as after v1 (`docs/manual-steps.md:7`, `:52`). Missing: resolving a conflict the fix round cannot | "Changelog conflict resolver" (M, UI) |
| 5.5 | Land a slice that ork did not run | partial | Ready rows show landing commands and `c` removes a merged worktree (`docs/dashboard.md:531-590`, `internal/tui/keys.go:71`). ork does not land a branch it did not run | none proposed; the outer session lands only branches it ran |

## 6. Bookkeeping and decisions

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 6.1 | File later notes as new slices | exists | `f` in the run pane, `ork file RUN N` (`internal/cli/run.go:207-221`, `internal/engine/later.go:103-117`) | none |
| 6.2 | `adj ask`, `adj check`, `adj decide` for an option the owner picked | partial | ork calls `adj check`, `ask` and `decide --by` only when an answer carries a decision key and the owner turned filing on (`internal/adjcli/adjcli.go:112,138,151`, `internal/engine/answer_adj.go`; TUI `ctrl+a`, needs `ADJ_DECIDED_BY`). `ork answer` never files in adj. ork never calls `reject` or `supersede`: the client has no such function (`internal/adjcli/adjcli.go`). Missing: answering an open adj record, and asking a new one, from ork | "Answer and ask adj records in ork" (M, UI) |
| 6.3 | Browse decisions and the inbox | exists | Decisions view, Questions view (`docs/dashboard.md:1004-1038`, `:1332-1418`) | none |
| 6.4 | Keep run state across a context reset | exists | Every run is a record on disk; the daemon reconciles unfinished runs on start (`docs/state.md:109-`, `docs/design.md:154-160`). The outer session's other memory (owner preferences, trial notes) has no ork home | none proposed |
| 6.5 | Keep trial notes and retrospectives | missing | ork has no place for owner notes about the process. They live in docs and slicer notes today | none proposed; use 1.6 |

## 7. Monitoring and config

| # | Job | Status | ork surface and evidence | Gap slice (size, UI) |
|---|---|---|---|---|
| 7.1 | Status across projects | exists | Overview, Running entry, `ork status` (`internal/cli/daemoncmds.go:21-95`). Bell and sidebar badge on new needs (`internal/tui/model.go:949-1014`) | none |
| 7.2 | Edit registry fields | partial | `ork projects set` changes nine fields (`internal/cli/cli.go:61-67`). No command for `environment_lines`, `permissions`, `vendor_env`, `global_cap`, `daemon_idle_minutes`, which are file edits (`docs/state.md:94-103`) | "Registry settings for the remaining fields and the global cap" (M, UI) |
| 7.3 | Edit `.claude/settings.json` allow rules | missing | Nothing in `internal/`, `cmd/` or `docs/` mentions `settings.json`. ork builds each role's allowlist from the registry `permissions` field or a built-in default (`internal/engine/perm.go:24-36,58-69`) and passes it as `--allowedTools` (`internal/agent/claude/args.go:59`). Codex ignores the allowlist (`internal/agent/codex/codex.go:3-4`). The registry field has no command (7.2). Whether a project needs `.claude/settings.json` rules once ork runs the roles is not verified | none proposed until the owner says it is needed |
| 7.4 | Stale runs and ledger cleanup | partial | A run with no event for 30 minutes shows `!!` and stale (`docs/dashboard.md:495-498`). ork stops, dismisses and retries its own runs. A lost process is found only when the daemon starts (`internal/engine/recover.go:49-114`); no watchdog kills a hung role (none found in `internal/agent`), so a hung role waits for the owner to stop it. A relay started elsewhere is read-only (`internal/tui/keys.go:515-519`), so ork cannot end its stale ledger run | "End a stale ledger run from the run pane" (S, UI); "Watchdog for a hung role" (M, UI) |
| 7.5 | Upgrade slicer and adj, rebuild ork, then check `ork doctor` | partial | `ork doctor` checks versions and the contract (`internal/cli/doctor.go`). No upgrade or pull command exists; `ork daemon restart` restarts after a rebuild (`internal/cli/lifecycle.go`). Pulling and building sibling tools is an external side effect, so this needs an owner decision | "ork upgrade: pull and build slicer and adj, then doctor" (M, UI) |
| 7.6 | Show live output and tokens of a run | exists | Run pane tail, tokens, cost (`docs/dashboard.md:1442-1478`) | none |
| 7.7 | Read a raw role log or report | missing | The report files are on disk (`docs/state.md:8-26`); no command or key prints one | "Open a role report in the run pane" (S, UI) |

## 8. Things only the owner does

These are owner acts, not orchestrator jobs, and stay with the owner:
deciding in adj (`AGENTS.md`, Decisions), approving a wave and each landing,
and choosing what to build.

## Prioritized gaps

Most valuable first. Value is how often the owner reaches for the outer
session for it today, weighed against the size.

| # | Gap slice (proposed) | Size | UI | Why this order |
|---|---|---|---|---|
| 1 | Lineup and budget overrides on every launch path (2.3) | M | yes | The lineup is chosen for every slice; the Roadmap launch and `ork run` cannot change it |
| 2 | Spec check on single-slice launch (1.2) | S | yes | Cheap; closes the preflight hole for one-off runs |
| 3 | Role matrix view (2.1) | S | yes | Read-only; lets the owner see the lineup source without slicer |
| 4 | Per-vendor slot cap and vendor-out memory (2.4) | M | yes | The one-Codex-slot and stand-in rule is owner policy now done by hand |
| 5 | File a slice from a note or a request (1.7) | M | yes | New slices and promotion are the main bookkeeping the outer session does |
| 6 | Add a note to a slice (1.6) | S | yes | Decisions recorded as notes today go through the outer session |
| 7 | Answer and ask adj records in ork (6.2, 4.3) | M | yes | Needs an owner decision on the surface first |
| 8 | Role matrix editor (2.2) | L | yes | Large; after the view, and needs an owner decision |
| 9 | Registry settings for the remaining fields and the global cap (7.2) | M | yes | File edits work today |
| 10 | End a stale ledger run from the run pane (7.4) | S | yes | Small recovery gap |
| 11 | Open a role report in the run pane (7.7) | S | yes | Helps debugging; files exist |
| 12 | Per-run stop point: after commit, or land (2.6) | S | yes | Only if the owner wants it |
| 13 | ork upgrade (7.5) | M | yes | External side effects; owner decision first |
| 14 | Spec drafting round (1.5, 1.3) | L | yes | Conflicts with `scope.v1`; owner decision first |
| 15 | Changelog conflict resolver (5.4) | M | yes | Listed as after v1 |
| 16 | Run a slice as parts or a sliver chain (2.7) | L | yes | Largest; the matrix already has slivers (r8) |
| 17 | Refuse or warn on a blocked item at wave start (3.7) | S | yes | Small, but the owner can launch an item whose dependency is not done |
| 18 | Watchdog for a hung role (7.4) | M | yes | A hung role waits for the owner today |

Gaps 7, 13 and 14 touch a boundary (adj, external tools, roadmap editing).
Per `AGENTS.md` they need an owner decision (`adj ask`) before any slice is
specified.

## What the owner still needs an orchestrator session for today

- Choosing or changing the role matrix and the lineup for a launch from the
  Roadmap or `ork run`.
- Filing new slices, adding notes to slices, and writing or fixing a thin
  spec.
- Running design interviews that no role raised.
- Answering and filing adj decisions for open records.
- Applying the one-Codex-slot rule and switching the queue to Claude when
  Codex is out of credits.
- Running slivers and split slices.
- Pulling and rebuilding slicer, adj and ork, and editing registry fields that
  have no command.
- Ending stale ledger runs of relays that ork did not start.

Everything else in the relay, from the worktree to the landing and cleanup,
ork does itself.

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

These decisions are not yet adj records. Until they are, slices written from
this analysis should cite this section as the owner's words of 2026-10-29.

## Documents that disagree with the code

Found while writing the guide and this analysis. These are not part of the
S165 files and are reported for follow-up:

- `README.md:21-22` says the Waves screen is later work; `docs/dashboard.md:1043`
  says the composer is available (S82), and the code has it
  (`internal/tui/waves.go`).
- `PROJECT.md:3` says "spec only, no code"; the repository has code.
- `docs/boundaries.md:76-78` says "No merge, push or tag in v1"; landing is in
  ork (`docs/boundaries.md:17`, `docs/design.md:351-391`).
- `docs/boundaries.md:38` says "v7 today"; ork accepts v7 and v8
  (`AGENTS.md`, Project Rules; `docs/design.md:1118`).
- `docs/design.md:423-426` leaves `land` out of the state list; the state exists
  (`internal/runrec/record.go:31`).
- `docs/dashboard.md:12-13` says mouse clicks do the same things as keys; a search
  of `internal/tui` and `cmd` finds no mouse handling.
- `docs/state.md:76-78` says `check_minutes` sets the stale flag; in the code it
  is only the check command's timeout (`internal/engine/states.go:197-202`).

Slice S183 fixes these statements.
