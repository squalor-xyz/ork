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
  test counts. The prompts ask for one line `Tests: N passed, M failed`
  in the implementer's and the gate's report, and for a final `## Owner
  needed` section in every report (`role.report-shape`).
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

The S01 spike measured these cells on 2026-10-18 with real headless runs in a
throwaway repo. Each cell cites a fixture under `testdata/vendors/<vendor>/`
or says *unverified* and why. CLI versions: claude 2.1.302, codex-cli 0.163.0,
grok 1.0.62. Grok has no successful run, so most of its column is
*unverified* (see Spike results).

| Capability | claude | codex | grok |
|---|---|---|---|
| Streamed events for the output tail | `--output-format stream-json --verbose --include-partial-messages` emits `stream_event` text deltas, then `assistant`, `rate_limit_event` and `result` (`testdata/vendors/claude/02-stream-json.stdout`). `--verbose` was used; without it is untested. | `--json` emits JSONL: `thread.started`, `turn.started`, `item.completed` (agent message), `turn.completed` (`testdata/vendors/codex/01-plain-stream-jsonl.stdout`). The run had whole messages and no delta events. | *unverified*: the stream probe reached the API and got HTTP 403. Its output is an init event, then a `result` event with subtype `error_during_execution` (`testdata/vendors/grok/02-stream-messages-json.stdout`, line 2). No model output streamed. |
| Usage (tokens, cost) in the result | `result.usage` (input, output, cache tokens), `result.total_cost_usd`, and `modelUsage.<model>.costUSD` (`modelUsage` is an object keyed by model name, with `costBasis` `list`; `testdata/vendors/claude/01-plain-json.stdout`). | `turn.completed.usage` has input, cached input, cache write, output and reasoning tokens (`testdata/vendors/codex/01-plain-stream-jsonl.stdout`). No cost field appears, so cost is *unverified*. | *unverified*: the `result` event of the stream probe carries `usage` and `total_cost_usd` fields, but only zero values were seen (`testdata/vendors/grok/02-stream-messages-json.stdout`). Real values are *unverified*. `grok usage SESSION_ID` is documented as "persisted token and cost usage for a session" (`testdata/vendors/grok/usage-excerpt.txt`) and was not run. |
| Resume a session by id | `claude -p --resume <session_id>` answered "OK", the word asked for in the first run, with the same session id and cache reads (`testdata/vendors/claude/03-resume.stdout`). | `codex exec resume <thread_id> -m gpt-6-luna --json` returned the same thread id and cached input tokens (`testdata/vendors/codex/02-resume-by-id.stdout`). The `resume` help lists no `-s` or `-C` (`testdata/vendors/codex/resume-help.txt`). | *unverified*: the resume probe used a team id as the session id, which was a probe mistake. It got HTTP 404 (`testdata/vendors/grok/03-resume-by-id.stderr`). |
| Per-tool allowlist | `--allowedTools Read` with `--permission-prompts none` refused a Write. The denial is listed in `permission_denials` and the exit code is 0 (`testdata/vendors/claude/04-allowlist-denies-write.stdout`; `.repo-status` is empty). A positive control, an allowlisted tool that runs, was not measured. | No per-tool allowlist. `-s` takes `read-only`, `workspace-write` or `danger-full-access` only (`testdata/vendors/codex/help-excerpt.txt`). The one read-only run (`testdata/vendors/codex/03-readonly-sandbox-write.stdout`) ran no command: it has only agent messages. The model said the filesystem was read-only, and `.repo-status` is empty. The sandbox was not exercised, so codex's write block is *unverified*. Decided: codex runs only read-only roles in v1 (`vendor.codex-allowlist`). | *unverified*: the allowlist probe got HTTP 403 before any tool call. `--tools` takes a comma list (`testdata/vendors/grok/help-excerpt.txt`), and the init event names tools such as `read_file` and `write`, so `Read` may be the wrong name (`testdata/vendors/grok/02-stream-messages-json.stdout`). |
| Rate-limit signal | Not forced. Stream output carried `rate_limit_event` with `rate_limit_info.status` `allowed`, `rateLimitType` `five_hour` and `resetsAt`. Utilization is at `rate_limit_info.unifiedWindows.{five_hour,seven_day}.utilization` (`testdata/vendors/claude/02-stream-json.stdout`). A `rejected` status and the exit code when limited are *unverified*. The help documents only `--fallback-model`, for "overloaded or not available" (`testdata/vendors/claude/rate-limit-help-grep.txt`). | *unverified*: no rate-limit or quota term appears in `codex --help` or `codex exec --help` (`testdata/vendors/codex/rate-limit-help-grep.txt`). Nothing was observed. Decided: ork matches the reported strings, "You've hit your usage limit" and HTTP 429 `rate_limit_exceeded`, in `turn.failed` or `error` events and falls back automatically (`vendor.codex-ratelimit`); a real sample is still to be captured. | *unverified*: every run that reached the API (probes 01, 02, 04 and 05) got HTTP 403, "used all available credits or reached its monthly spending limit" (`testdata/vendors/grok/01-plain-json.stdout`). That is a quota limit, not a documented rate-limit signal, and ork has no class for it yet. No rate-limit term appears in `grok --help` (`testdata/vendors/grok/rate-limit-help-grep.txt`). |

- **Fallback** (`vendor.fallback`): an exit classified as rate-limited
  moves the role to the next entry in its chain. The new vendor starts
  fresh, with the earlier reports in the brief. An implementer that failed
  midway leaves its tree as it is, and the next vendor is told to continue
  from it.
- **Detection** (`vendor.ratelimit-detection`): the classifier matches only
  documented signals. Every non-ok exit saves its raw stdout, stderr and
  JSON under `~/.local/state/ork/fixtures/<vendor>/` as a candidate. An
  unclassified exit stops the slice for the owner, who can mark it
  "rate limit" in one keystroke. That sample becomes a test fixture. The
  spike's grok 403 (`testdata/vendors/grok/01-plain-json.stdout`) would be
  such a candidate: no documented rate-limit signal matches it, so it stops
  the slice for the owner.
- **Permissions** (`launch.permissions`): there is a per-project, per-role
  allowlist in the registry. Read-only roles run in their vendor's read-only
  mode. A denial stops the slice. The spike measured the claude allowlist
  denial: it exits 0 and shows only in `permission_denials`, so ork must read
  the JSON to see it. codex has no per-tool allowlist, so in v1 it runs only
  read-only roles in its read-only sandbox (`vendor.codex-allowlist`).
- **Credentials:** ork uses each CLI's own login. It never reads, stores
  or passes credentials.

### Spike results

- **Calls.** claude 6 invocations (probes 01 to 06), codex 4, grok 6. Each
  probe's command is in `testdata/vendors/<vendor>/NN-*.cmd`, with the probe
  repo shown as `<probe-repo>`. The scripts are `spike/claude.sh`,
  `spike/codex.sh`, `spike/grok.sh` and `spike/common.zsh`. The `.cmd` files
  were regenerated with `S01_DRY_RUN=1`, which writes the command line without
  running it, so that step made no vendor call. The grok probe 03 line used
  the team id from the first run as `S01_DRY_SID`.
- **Models.** claude probes 01 to 04 used `haiku`, whose `modelUsage` key is
  `claude-haiku-5-5`. Probe 05 ran on `claude-sonnet-5-5`, although its command
  line says `--model haiku` (`testdata/vendors/claude/05-plan-mode-edit.stdout`,
  `modelUsage` key, cost 0.0818584). That was a call on a non-cheapest model.
  The cause is not known. Probe 06 failed before any model ran, so its
  `modelUsage` is empty. codex `gpt-6-luna`, described as "Fast and affordable
  model for easier tasks" (`testdata/vendors/codex/models.txt`). grok
  `grok-4.5`. The CLIs print no prices, so "cheapest" is not verified for codex
  or grok. `grok models` also lists `ollama-local-*` entries
  (`testdata/vendors/grok/models.txt`), which were not used.
- **Exit codes.** claude exits 0 on success and on the denied write. It exits 1
  on an unknown model, with JSON that has `is_error` true and
  `api_error_status` 404 and `subtype` "success"
  (`testdata/vendors/claude/06-bad-model.stdout`). codex exits 0 on success and
  on the read-only run, and 1 on an unknown model, as a `turn.failed` event
  (`testdata/vendors/codex/04-bad-model.stdout`). grok exits 1 on the 403 and on
  an unknown model (`testdata/vendors/grok/06-bad-model.stderr`). A denial or a
  blocked write is not an exit code, so the classifier must read the output.
- **Plan mode (claude).** No edit was attempted: `permission_denials` is empty
  and `notes.txt` is unchanged (`testdata/vendors/claude/05-plan-mode-edit.repo-status`
  is empty). The result text says a plan was saved under `<home>/.claude/plans`.
  That was not checked, and nothing under `~/.claude` was opened. A worktree
  fingerprint would not see it. This probe is also the sonnet call above.
- **Grok auth.** `grok models` printed "You are not authenticated."
  (`testdata/vendors/grok/models.txt`). The `-p` call still reached the API with
  the init event's auth source `oauth`
  (`testdata/vendors/grok/02-stream-messages-json.stdout`, field name redacted),
  and the team had no credits left (HTTP 403). No login was attempted.
- **Grok is not measured.** Probes 01, 02, 04 and 05 reached the API and got
  HTTP 403. Probe 03 got HTTP 404 and probe 06 failed on the model name. Probes
  02 to 06 ran after the 403 because the first gate in `spike/grok.sh` only
  stopped on an auth message. Probe 03 used a team id as the session id. Probe
  04 used `--tools Read`, which may not be a grok tool name. Grok needs restored
  credits and a new call-budget decision from the owner before any of its cells
  can be measured.
- **Redaction.** `spike/common.zsh` replaces the home directory with `<home>`,
  the probe repo with `<probe-repo>`, other scratch paths with `<scratch>`, the
  project directory name with `<project-dir>`, and `/tmp/cc-socks` sockets with
  `<tmp>/cc-socks/<socket>`. It also redacts `sk-` keys, bearer tokens, key,
  token, secret and password values, runs of 40 or more characters, the grok
  team id, the id after `--resume` in `.cmd` files, and key-like field names.
  Session ids in outputs are kept: they are local run ids, not credentials.
  Fixtures rename `apiKeySource` to `REDACTED-KEY-NAME`, so parser tests must
  not read that field from fixtures. The check
  `rg -i 'sk-|api[_-]?key|token"\s*:\s*"[A-Za-z0-9]{20,}' testdata/` finds
  nothing. The sweep for `/Users/jon`, `/private/tmp` and `/tmp/cc-socks` finds
  nothing in `testdata/`. In `spike/` it finds only the rule lines in
  `common.zsh`, which must name those strings.
- **Help text.** Full help texts are not stored. The key pattern matches
  words such as "disk-" in them, so the excerpts keep only the lines that
  cells cite. Rate-limit terms were searched in full help text, and the
  matches are in each `rate-limit-help-grep.txt`.

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
