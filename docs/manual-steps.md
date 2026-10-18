# Manual steps and failures

The steps the outer session or the owner does by hand in a slice relay today,
and the failures seen in relay trials. Each row names its source and the
ork capability that addresses it. Since the 2026-10-18 interview, the ork
engine replaces the outer session and the orchestrator agent outright
(`launch.mode`), so every row in "Still manual" is in v1, except landing
and changelog conflicts, which come after v1 (`landing.v1`). The harness
failures at the end no longer apply to ork runs, because no role spawns or
hands back to an agent. Paths are in the sibling repos under
`squalor-xyz/`.

Short sources used in the tables:
- `playbook`: `agents/multi-agent-workflows/playbooks/slice-relay.md`
- `trial`: `agents/multi-agent-workflows/info/2026-10-15-nested-relay.md`

## Already handled by slicer

The 2026-10-16 outer-session wishlist became slicer s322-s329, and all of them
are done. ork should call these commands and not rebuild them.

| Former manual step | slicer now | Item |
|---|---|---|
| Hand-assembled orchestrator briefs | `slicer ai relay-brief` | s322 |
| Open owner decisions kept in prose | `needs-owner` hold, `slicer decide` | s323 |
| Parallel pairs chosen by regex | `next --batch` with disjoint files | s324 |
| Stale render after every landing | `check` names a stale render | s325 |
| Closing the steps by hand at run end | `relay-run end --close-steps` | s326 |
| About seven hand-run steps per landing | `slicer land` | s327 |
| Filing with eight flags plus promote | one `import` | s328 |
| No retire command | `slicer retire` | s329 |

Source: `slicer/.slicer/render/ROADMAP.md`, rows s322-s329.

Wave planning by hand became s376 (`slicer plan-parallel`), done on
2026-10-18.

## Still manual

| Step or failure | Source | Candidate ork capability |
|---|---|---|
| Setup per slice: read the size, run `relay-roles recommend`, confirm the lineup with the owner | `slicer ai relay` Setup | Gather the inputs and present one confirmation per wave |
| Create the worktree and branch, run `relay-run begin`, then spawn the orchestrator, in that order | `slicer ai relay` Order | Run the deterministic prefix in order, with absolute paths and `git -C` |
| Copy the environment lines verbatim into every brief | trial, table of failures | Read them from the project's AGENTS.md and inject them |
| Check each run report: the first line, and a last line of `nothing left running: yes` | `slicer ai relay` Order | Parse and validate the report shape |
| File each report's later notes on the affected items | `slicer ai relay`; playbook Lessons | File them through `slicer note` after owner review |
| Forward a role report that reached the outer session to its orchestrator | playbook, Claude Code specifics | Route reports by the `.relay/<id>/<role>-report.md` file |
| Save mailbox stats before removing a worktree; the mailbox goes with it (the ledger moved to the git common dir in slicer s298) | `agents/multi-agent-workflows/README.md:114`; trial | Run `relay-stats.sh` before cleanup |
| Land passed branches one at a time, which is the bottleneck | playbook Lessons | Batch the merges, then run one suite on main |
| Changelog conflict on every merge | playbook Lessons | Resolve per `###` heading from the branch's own additions |
| Limit parallel relays to about four on 8 cores; suite takes 20-40 min under load | playbook Lessons | Throttle a wave and put realistic suite times in the brief |
| Catalog ID collisions between parallel slices; decisions filed at landing | `adjudicate/.slicer` slice S38 Git section | Queue catalog filings and apply them at landing |
| Relay contract copies drift between repos | slicer s373 | Check the contract version, keep no copy |

## Failures that point at harness behavior

ork cannot fix these. They constrain any design that spawns or waits on
agents.

| Failure | Source |
|---|---|
| One session across a queue grew to about 256k context; a model cannot compact itself | trial |
| Sleep loops and Monitor keepalives left running after a report | playbook, Claude Code specifics |
| Hand-back not delivered once; orchestrators idle without reporting | trial, Open |
| Orchestrator launched without effort had to be relaunched | playbook, Claude Code specifics |
| A `cd` in the outer session moved the directory under running agents | playbook, Claude Code specifics |
| Permission prompts from depth-2 agents never tested outside auto mode | trial, Open |
