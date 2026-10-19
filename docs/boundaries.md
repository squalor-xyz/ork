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
| Landing | slicer and the owner, outside ork in v1 | `slicer land` (`landing.v1`) |
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
seeded from contract v7 (`roles.prompt-seed`). Two things keep them in line
(`boundary.contract`):

1. ork lists the contract versions it supports (an accepted set, v7 today).
   `ork doctor` and every wave launch compare slicer's version with that
   set and refuse any other. Adding a version means checking what the bump
   changed (slicer s382 moves v7 to v8).
2. A slicer item asks for the machine-readable parts of the contract in
   `ai relay --json`: the Order steps, the report fields, the outcomes and
   the stop rules. Once slicer has them, ork validates against slicer's own
   description.

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
  (`owner.questions`). No role and no automatic step triggers it.
- Commits only on slice branches, only after the owner approves the slice
  (`git.commit`). No merge, push or tag in v1.
- Act only on runs that ork launched. Relays started elsewhere are
  view-only (`dash.external-runs`).
