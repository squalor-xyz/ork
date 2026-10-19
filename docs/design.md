# Engine design

How ork runs a wave of slices. Owner decisions are cited by adj key.
Anything not backed by a decision is marked *proposed*. Vendor CLI behavior
marked *unverified* must be confirmed by the vendor spike before code
depends on it.

## Shape

```
ork (TUI)  ork status --json (CLI)
     \           /
      unix socket (JSON)
           |
       ork daemon  -- run records, registry (state.md)
       /    |    \
  slicer   adj   git        <- CLI calls only
           |
   role processes: claude -p / codex exec / grok -p   (one per role round)
```

- **The daemon** owns waves, slices and role processes. The TUI and the
  CLI are clients (`daemon.lifetime`).
- **Start.** Running `ork` starts the daemon if no live socket exists. The
  daemon exits when no wave is running and no client has been connected for
  a while (`daemon.start`). The idle timeout is *proposed* at 10 minutes.
- **Socket.** `~/.local/state/ork/ork.sock`, mode 0600, one JSON message
  per line. A pidfile next to it detects stale sockets.
- **Restart.** On start, the daemon reconciles every non-final run record
  against live process groups and the slicer ledger. A role whose process is
  gone and that left no report is marked `lost`, and its slice waits for the
  owner.

## Wave lifecycle

1. **Suggest.** For one project, ork shows `slicer plan-parallel` (or
   `slicer next --batch`) as a proposed wave (`wave.selection`). The owner
   adds or removes items, and ork warns when two items' `Files` overlap.
2. **Lineup.** For each item, ork runs `slicer relay-roles recommend ID
   --well-specified ... --split ...`. The primary row per role becomes the
   lineup, and the alternatives, in order, become that role's fallback
   chain (`lineup.source`). The owner can edit both.
3. **Plan.** For each item, ork shows the worktree path, the branch, the
   check command, the budgets, and whether it runs an architect, a split or
   slivers. Then it shows the wave's place in the global queue.
4. **Approve.** The owner approves the wave. This is checkpoint 1.
5. **Run.** Items start as slots free up under the global cap (default 4)
   and the per-project cap (`wave.limits`). Several waves, each in one
   project, may run at once (`wave.scope`).
6. **Result.** Each item that reaches a passed gate waits for the owner's
   approval. This is checkpoint 2. Once approved, ork commits. An item that
   stops waits in "needs you", and the rest continue (`wave.failure`).

## Slice state machine

```
setup -> implement -> checks --red--> implement (fix)
                        | green
                        v
                     review --findings--> implement (fix round) -> checks -> review/gate
                        | clean
                        v
                      gate --FAIL--> implement (fix round)      [3rd FAIL -> stopped]
                        | PASS
                        v
                awaiting-approval --approve--> commit -> committed
                        | reject
                        v
                      stopped
```

Any state can go to `stopped` on a stop rule (below), or to `paused` on a
budget limit or an owner question. The owner can resume or stop a paused
slice.

- **setup:**
  - `git -C P worktree add <worktree_pattern> -b <branch_pattern>`
    (`git.worktrees`);
  - `.worktrees/` is added to `info/exclude` if it is missing;
  - `slicer relay-run begin ID --worktree W` (`ledger.write`);
  - the implementer runs `slicer start` (the contract says the implementer
    starts the item).
- **implement:** one implementer, or a lead and parts in a split
  (`slice.options`). Slivers run as their own items, under the cap.
- **checks:** ork runs the project check command and `slicer check --json`
  in the worktree (`checks.deterministic`). A red result goes back to the
  implementer, with the failing output in the brief, without spending a
  review. The check command comes from `slicer config` once slicer has the
  key; until then it comes from the registry (`project.check-command`).
- **review:** the reviewer, plus the architect on size L
  (`slice.options`). Both run in parallel and are read-only, with the
  fingerprint applied (`role.readonly-fingerprint`).
- **fix round:** the implementer is resumed with the confirmed findings. A
  small fix round is reviewed and gated in one pass (contract v7).
- **gate:** the first line of the report must be exactly `VERDICT: PASS` or
  `VERDICT: FAIL`. On S and M items the gate is the resumed reviewer
  ("lite"). A FAIL is recorded with `slicer note ID --text "Review FAIL:
  ..."` (contract v6).
- **commit:** ork commits in the worktree with the title from the slice's
  Git section. There is no AI attribution, and roles never commit
  (`git.commit`).
- **Finish step:** run per the project's `implement_finish` setting (`done`
  or `handoff`), as the contract orders it after PASS.

## Stop rules

From slicer's contract:
- the same slice fails its gate three times;
- the spec turns out to be ambiguous or wrong;
- an owner decision is needed;
- a permission denial on a slicer step.

ork adds:
- a role exit that is not classified as ok or rate-limited;
- a read-only role that changed the tree;
- a lost process.

## Roles

- **Prompts.** Embedded in ork and canonical (`roles.prompts`). They are
  seeded from slicer's v7 contract, with anything else written fresh
  (`roles.prompt-seed`). They carry no Claude Code-only mechanics (Agent
  tool, SendMessage, nesting), because roles here never spawn agents.
  `ork roles print [ROLE]` prints them so the agents repo can generate its
  own role files.
- **Brief.** It contains:
  - the role prompt;
  - the slice from `slicer show ID --json --lean`;
  - the tracking rule and the workspace limit;
  - the environment lines from the project's AGENTS.md;
  - the role's own temp directory;
  - the report path;
  - earlier reports, when the role is not resumed.
- **Report.** The role writes its final report to
  `~/.local/state/ork/runs/<run>/<role>-<round>.md` and exits
  (`role.report-io`). If the file is missing, ork uses the vendor JSON's
  final result text. ork validates the shape: the gate's first line, the
  test counts.
- **Resume.** For fix rounds and the lite gate, ork resumes the same vendor
  session where the vendor supports it. Otherwise it starts fresh with the
  earlier reports in the brief (`role.resume`).
- **Reviewer rule.** A reviewer never wrote code in the slice. A reviewer
  may run on another vendor if the lineup names it (`review.cross-vendor`).
- **Ledger.** Each role round is recorded with `slicer relay-run step` with
  role, vendor, model, effort and `--task "ID role"`, and its phase is
  updated on exit. A heartbeat goes out every 10 minutes (*proposed*) while
  it runs.

## Owner questions

When a role report says it needs the owner (a spec question, a design
choice), the slice pauses in "needs you" with the question. The owner types
an answer, and ork resumes the role with it. A toggle files the question and
answer in the project's adj store (`adj ask` then `adj decide --by
<owner>`), with `--ref ID` (`owner.questions`).

## Vendors

There is one Go adapter per vendor. Each implements: build a command
(model, effort, permission mode, allowlist, output format), start it in its
own process group, stream events, parse the final result and usage, and
classify the exit as ok, rate-limited, auth or error.

The `claude -p --output-format json`, `codex exec --json` and
`grok --output-format json -p` invocations, and the read-only flags
(`--permission-mode plan`, `codex -s read-only`), come from
`agents/setup/lib/workflow-vendor.sh:22-41`.

The capabilities below are *unverified* and must be settled by the spike:

| Capability | claude | codex | grok |
|---|---|---|---|
| Streamed events for the output tail | `stream-json`? | `--json` events? | ? |
| Usage (tokens, cost) in the result | ? | ? | ? |
| Resume a session by id | `--resume`? | `exec resume`? | ? |
| Per-tool allowlist | `--allowedTools`? | sandbox modes only? | ? |
| Rate-limit signal | ? | ? | ? |

- **Fallback** (`vendor.fallback`): an exit classified as rate-limited
  moves the role to the next entry in its chain. The new vendor starts
  fresh, with the earlier reports in the brief. An implementer that failed
  midway leaves its tree as it is, and the next vendor is told to continue
  from it.
- **Detection** (`vendor.ratelimit-detection`): the classifier matches only
  documented signals. Every non-ok exit saves its raw stdout, stderr and
  JSON under `~/.local/state/ork/fixtures/<vendor>/` as a candidate. An
  unclassified exit stops the slice for the owner, who can mark it
  "rate limit" in one keystroke. That sample becomes a test fixture.
- **Permissions** (`launch.permissions`): there is a per-project, per-role
  allowlist in the registry. Read-only roles run in their vendor's read-only
  mode. A denial stops the slice.
- **Credentials:** ork uses each CLI's own login. It never reads, stores
  or passes credentials.

## Budgets

The limits are per slice: wall time and total tokens, with defaults in the
registry and overrides at launch (`run.budgets`). When a slice reaches a
limit, ork lets the running role finish, then pauses the slice before the
next role starts, and asks the owner.

## CLI

`cli.json`. Every command takes `--json`:

```
ork                         open the TUI (start the daemon if needed)
ork status [--project P]    waves, slices, roles, needs-you
ork wave suggest P          the proposed wave and its lineup
ork wave start P ID...      launch (asks unless --yes)
ork approve RUN             accept a passed slice and commit
ork answer RUN TEXT         answer an owner question
ork stop RUN|WAVE           stop and record why
ork roles print [ROLE]      print the embedded role prompts
ork projects add|list|rm    manage the registry
ork doctor                  check versions, CLIs, logins, contract
```

`ork doctor` checks:
- the slicer and adj versions;
- each vendor CLI is present and logged in (by running a trivial command,
  without reading credentials);
- the slicer contract version against ork's pin (`boundary.contract`);
- each project's check command and patterns.

## Testing

`testing.strategy`:
- The state machine runs against fake `claude`, `codex` and `grok`
  executables that replay fixture JSON: success, FAIL verdict,
  rate-limited, auth error, a crash, and a read-only role that edits.
- The slicer and adj adapters are tested against recorded CLI output,
  refreshed per tool release.
- TUI models are tested as pure update functions.
- Real runs are covered by a manual checklist per vendor.

## Build order (proposed)

Each step is usable or testable on its own:

1. **Vendor spike.** Fill in the capability table with real runs on a
   throwaway repo. Capture fixtures.
2. **Core.** Registry, slicer and adj adapters, read-only dashboard
   (Overview, Roadmap, Decisions, external relays). Cut into three
   slices (`plan.s02-cut`): S02 core and adapters, S11 TUI shell and
   Overview, S12 Roadmap and Decisions. Before the daemon, the TUI calls
   the adapters directly behind one interface (`dash.pre-daemon`) and
   polls them (`dash.refresh`).
3. **Daemon, socket and CLI.** `ork status`.
4. **Engine for a single implementer.** setup → implement → checks →
   review → gate → approve → commit, on one vendor, with the ledger.
5. **Waves and caps.** Several items, queueing, failure isolation.
6. **Three vendors, fallback, fingerprint, budgets, owner questions.**
7. **Live role detail.** Output tail, tokens and cost, charts and
   animations.
8. **Architect on size L, split, slivers.**
9. **Landing help.** Ready-to-land list and merge detection.
