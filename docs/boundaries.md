# Boundaries

What each tool owns, and what ork may touch. Owner decisions are cited by
adj key. Paths are in the sibling repos under `squalor-xyz/`. Terms are
defined in [glossary.md](glossary.md).

## Ownership

| Concern | Owner | Source |
|---|---|---|
| Queue, items, slice specs, readiness, claims | slicer | `slicer/docs/how-it-works.md` |
| Relay contract rules: roles, gate, briefs, stop, report shape | slicer | `slicer ai relay`, `slicer ai relay-orchestrator` (`slicer/src/slicer/ai.py`) |
| Contract version | slicer | `RELAY_CONTRACT` in `ai.py` |
| Role matrix and lineup recommendation | slicer | `slicer relay-roles` (`lineup.source`) |
| Relay-run ledger | slicer (ork writes it through the CLI) | `slicer relay-run` (`ledger.write`) |
| Wave suggestion | slicer | `slicer plan-parallel`, `next --batch` (`wave.selection`) |
| Landing | after the owner approves, ork sequences the landing: merge the base branch in, re-check, ff-merge, `slicer land`, optional push, confirmed cleanup (S119); until then it lists ready runs and detects merged branches | `slicer land` stays the tracking step, run by ork through the CLI (`landing.in-ork` #162, which supersedes `landing.v1`; `landing.merge-detect`, `git.cleanup-key`) |
| Spec check verdicts (S116) | ork keeps them only as an ephemeral, in-memory job cache in the daemon, each tied to a fingerprint of the slice's spec; a restart asks again. They are not a durable record: slicer owns the spec, and the owner's answers are written into it with `slicer edit --append` (and into adj for keyed questions). The one thing ork stores is the answers' `Owner decisions` paragraph in the run record of the slice it then starts, because the run's worktree lacks the main checkout's uncommitted edit | `roles.spec-check` (#164); [design.md](design.md), Spec check |
| Slices filed from a review's `later` findings | slicer owns the record; on the owner's confirmation in the pane, ork runs `slicer add TITLE --discovered-from ITEM` and `slicer promote` (a Why section from the finding) in the project, as the writes it logs | `review.finding-kinds` (#163); `slicer add --help` |
| Goals, non-goals, catalog | slicer | `slicer goals`, `slicer catalog` |
| Owner decisions | adjudicate | `adj` |
| Global agent guidance | agents repo | `agents/GLOBAL_AGENTS.md` |
| Unattended sandbox (agentbox) | agents repo | `agents/containers/` (not used by ork v1, `launch.sandbox`) |
| Dashboard across projects | ork | [dashboard.md](dashboard.md) |
| Registry, run records, fixtures, UI preferences | ork | [state.md](state.md) |
| Relay state machine: Setup, Order, checks, rounds, gate, commit | ork | [design.md](design.md) (`launch.mode`) |
| Vendor launch, fallback, read-only fingerprint | ork, replacing `relay-call.sh` and `orchestrate.sh` | [design.md](design.md) (`boundary.agents-scripts`) |
| Role prompts | ork, canonical; the agents repo generates from `ork roles print` | [design.md](design.md) (`roles.prompts`) |
| Claude Code specifics for interactive relays | agents repo, until it retires interactive relays | `agents/multi-agent-workflows/playbooks/slice-relay.md` |

## The contract

ork implements slicer's relay contract in Go: the outer session's Setup and
Order, and the orchestrator's rules. Its role prompts are ork's own text,
seeded from contract v10 (`roles.prompt-seed`). Two things keep them in line
(`boundary.contract`):

1. ork accepts exactly v9 and v10. `ork doctor` and the engine check the
   contract with `--root`, respecting the project's pin, before a run starts.
2. ork reads `slicer ai relay --json` and validates its finding kinds,
   conformance keys, ledger roles, gate criteria and verdicts, and outcomes.
   The run keeps its contract version, ledger role names and gate criteria.
   v10-only fail reasons are gated on that recorded version.

Where ork's model differs from the contract, because there is no
orchestrator agent and ork plays both the outer session and the
orchestrator, ork documents the difference in design.md. ork does not
change the contract by itself.

## Interfaces ork uses

- **slicer CLI**, with `--json` (and `--lean`). Branch on the exit code (0
  ok, 1 failed check, 2 usage or empty queue, 3 internal or state error,
  including `locked`) and on `error.code`, never on message text (`EXITS`
  in `ai.py`). For refreshes, `slicer status -r` and
  `slicer log --since T --json` exist.
- **adj CLI**, with `--json`, one call per registered project. Nothing ork
  v1 needs is unreleased in adj 0.1.0.
- **Vendor CLIs:** `claude`, `codex`, `grok`, headless, with their own
  logins. ork never touches credentials.
- **git**, through `git -C <path>`, never by changing directory.

## Hard rules

- Read and change slicer state only through the `slicer` CLI. Never open,
  search, parse or edit `.slicer/` (`TRACKING_RULE` in `ai.py`;
  `adjudicate/AGENTS.md:455-456`).
- Read and change decisions only through `adj`. Never edit `decisions/` by
  hand.
- `adj decide` runs only on an explicit owner action, as the owner, for
  example when the owner answers a question inside a slice
  (`owner.questions`) or confirms an open record with `ork adj answer`.
  The latter always prompts and has no `--yes`. No role and no automatic
  step triggers it. ork never rejects, supersedes or withdraws a record.
- Commits only on slice branches, only after the owner approves the slice
  (`git.commit`). Confirmed cleanup may run `git worktree remove` then
  `git branch -d` for a merged run (`git.cleanup-key`). No merge, push or
  tag in v1.
- Act only on runs that ork launched. Relays started elsewhere are
  view-only (`dash.external-runs`), with one exception: ork may end a
  stale ledger run after a y/N confirm (`dash.external-runs-end`, adj 174).
