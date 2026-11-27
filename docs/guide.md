# Guide

A walk-through for a new owner: install ork, add a project, run one slice,
run a wave, answer questions, approve and land, and keep an eye on several
projects. The pictures are in [diagrams.md](diagrams.md). The details behind
each step are in [design.md](design.md), [dashboard.md](dashboard.md) and
[state.md](state.md).

Citations are `path:line` in this repository. Where a design document calls
a command *proposed* but the code has it, this guide says "built" and cites
the code. Where only a document says it, the guide says so.

## 1. What ork does

ork runs slice relays for you across projects. A slice is one tracked unit of
work in slicer. For each slice ork makes a git worktree, runs an implementer,
runs the project checks, runs a reviewer, runs fix rounds, runs a gate, and
then waits for you. You approve; ork finishes, commits and lands the slice on
the base branch (`docs/design.md:356-361`, `docs/design.md:646-810`). *Proposed
(S331, adj 281):* a run that passes everything lands with no approve step (see
"Approve and land").

What ork owns: the list of projects, a record per run, role reports, and the
daemon (`docs/state.md:8-26`). What it does not own: the roadmap and the
ledger (slicer), decisions (adj) and the code (git). ork reaches those only
through their command lines (`docs/boundaries.md`, `README.md:66-67`).

Two programs make up ork:

- The **daemon** runs the work. It keeps running when you close the TUI.
- The **TUI** and the **commands** are clients. They ask the daemon
  (`docs/design.md:22-24`).

You never start the daemon by hand. `ork` and every command that needs it
start it when nothing answers (`docs/design.md:24-40`,
`cmd/ork/main.go:151-166`). It exits after 10 idle minutes by default
(`docs/design.md:26-29`).

## 2. Install and build

From the repository root, with Go installed (`README.md:40-46`):

    go install ./cmd/ork

Go writes `ork` to `$(go env GOBIN)`, or to `$(go env GOPATH)/bin`. Put that
directory on your `PATH`. *Proposed (S103):* `ork --version` prints the mark
only on a terminal (a character-device stdout). Otherwise its first line is
plain `ork VERSION` (`cmd/ork/main.go`, `run`).

Keep the binary and the daemon in step. After you rebuild, run `ork daemon
restart`. `ork daemon status` shows whether it runs, its pid and build,
whether it is stale, whether roles run and whether the self-host project is
behind main. It never starts a daemon, and exits 0 when none runs. Add
`--json` for structured output. A failed status read omits roles and main
but still shows the build and pid.
A daemon from a different code build makes every command print a warning
that names restart. Clean revisions that differ only in tracking, docs,
root Markdown, tests, root testdata or GitHub configuration do not warn
when a registered repository proves that difference. Embedded prompts still
count; unknown, modified or unverifiable builds keep the warning. Doctor
always shows a daemon line, and shows behind-main even while roles run.
`ork daemon stop` refuses
while a role is running unless you add `--force` (`docs/design.md:41-48`).

When the slicer project is an editable install, protect its landings with a
probe that imports the worktree's source (S353; adj 484,
`land.contract-bump-guard`):

    ork projects set slicer --contract-probe 'PYTHONPATH="$PWD/src" python3 -m slicer ai relay'

The probe runs in the slice worktree before the fast-forward. Its first
output line must name a relay contract ork accepts. A newer contract, failed
command or missing contract line pauses the landing. Update ork to accept
the new contract, then use `ork retry RUN` to probe again and land. The
command must be read-only. Clear it with `--contract-probe ""`. The installed
`slicer` command can still import main, even with `--root WORKTREE`; the
explicit `PYTHONPATH` makes this probe read the worktree instead.

## 3. Check your tools: `ork doctor`

    ork doctor

It prints the path of the ork binary first, then one line per check
(`internal/cli/cli.go:204-210`, `docs/design.md:2368-2495`):

- the slicer and adj versions;
- each vendor CLI (claude, codex, grok) and its version. ork runs roles on `claude`, `codex` and `grok` (`docs/design.md:207-216`); grok runs read-only roles in plan mode and write roles under `dontAsk` with allow rules (*proposed*; adj 373, S305). A missing vendor is
  reported and does not fail the check, because ork needs only the vendors
  your lineup uses;
- the slicer relay contract (`slicer ai relay --json`), for the directory you
  run doctor in and for each registered project with its own pin. ork accepts
  v12 and v13 only (`boundary.contract-set`, adj 470); any other version fails.
  So does a project whose slicer
  refuses its pin (`contract_mismatch`); an unpinned project is checked against
  the accepted set alone (`docs/design.md:2412-2422`). *Proposed:* a project
  with adj on is checked the same way (`adj contract --help`). An ok project
  puts the adj column on the next indented line: `adj-json v1 pinned`,
  `adj-json v1 unpinned`, or `adj -` when adj is off. A failed adj check
  names `ork projects pin NAME --adj 1` or `ork projects unpin NAME --adj`;
- each registered project: the path exists, the check command parses, and the
  worktree and branch patterns contain `{id}` or `{id_lower}`;
- the owner ork files spec answers under in adj (`ADJ_DECIDED_BY`, else the
  one set with `ork settings set --owner NAME`), and whether that filing is on
  (*proposed*, S314). It is advisory and does not change the exit code.

`ork doctor` exits 1 when slicer, adj or the contract check fails. Fix those
before you run anything. Advisory project findings do not change the exit code.
A failed `--fix` exits 1.

Doctor also checks the slicer merge drivers named in each project's
`.slicer/.gitattributes`. A missing driver produces a warning and the commands
from `slicer setup-git --root PROJECT`. Run those commands in the project before
starting parallel work, or run `ork doctor --fix`. That option names the missing
drivers in the project header, shows the commands and asks
`Configure slicer merge drivers? [y/N]` for each affected project.
Enter means No. Yes runs the commands in that project and changes the clone's
git configuration (S328, adj 288 and 289). Success replaces the missing finding
with `merge drivers configured: NAME, NAME`; the project shows `[ok]` when no
other warning remains. With `--json`, commands are in
`merge_driver_commands`; confirmation prompts go to stderr.

The run plan names missing merge drivers too, in the same words, as
`warning: merge drivers missing: NAME, NAME (fix: ...)` in `ork run` and
`ork wave start` and in the TUI launch confirm. It is shown before the start and
does not refuse it; the plan never runs the commands (S348, adj 480).

ork never reads vendor credentials. Log in with each vendor CLI's own login
(`AGENTS.md`, Project Rules).

## 4. Add a project

*Proposed (S241):* `ork projects list` shows name and path, followed by
the applicable words `adj`, `push`, `ui-pass-skills=a,b` in that order.
With none set the third column is `-`. JSON output is unchanged.

ork needs an existing absolute directory (`docs/design.md:2561-2568`). The
TUI Welcome view offers the current directory only if it holds `.slicer` or
`.adjudicate.json` (`docs/dashboard.md:31-35`), and the roadmap and ledger
features need slicer. *Proposed (S249):* after registering a directory, run
`ork projects setup NAME STEP [--dry-run] [--json]`. Choose `slicer-init`
for `slicer init`, `relay-roles-init` for `slicer relay-roles init --render`,
`adj-init` for `adj init` and enabling adj in ork, or `adj-off` to disable
adj in ork without calling adj or removing its files. A dry run prints
`would run: COMMAND` and changes nothing. A successful step prints
`ran: COMMAND`, with `(adj on)` or `(adj off)` for adj steps. JSON returns
`{command, done, adj?}`. The tools run in the registered main checkout,
leave changes uncommitted and never receive `--force`. Adj steps refuse
while the project has unfinished runs; slicer steps defer to slicer.

After `slicer init`, run `slicer setup-git` and run the `git config` lines it
prints in the same project. They configure slicer's merge drivers once per
clone, including the generated files and index used during landing.

Add it in either way:

- **In the TUI.** With no projects, ork opens the Welcome view. Press `+`,
  type an absolute path (a leading `~` works) and press `y` to confirm
  (`docs/dashboard.md:27-51`). `+` works from every view, and `-` removes the
  selected project (`docs/dashboard.md:65-83`).
- **On the command line.**
  `ork projects add /abs/path/to/project --name NAME`. With no path in text
  mode it offers the current directory (`internal/cli/daemoncmds.go:234-247`,
  `docs/design.md:2561-2568`).

After adding a path, the CLI prints advisory `setup:` lines with fix commands
for missing setup. A Python project with `pyproject.toml` and no `go.mod` gets
a `--preset python` suggestion. A missing check command gets both
`slicer config check_command CMD` and `ork projects set NAME --check CMD`
as fixes. The check command is read from slicer first, with the registry as
fallback. These warnings keep exit 0 and apply no settings. JSON output has
the same lines in a `setup` array beside `project`, empty when none apply.

To add every project in a folder such as `~/code`, press `+` in the TUI, type
the folder and press Enter: when it holds projects below it, ork shows a
checklist (space ticks, `a` all, `n` none, `y` adds; Enter does nothing there).
Tracked projects (a
`.slicer` directory or `.adjudicate.json`) start ticked and plain git repos
do not. On the command line,
`ork projects add --scan /abs/folder [--yes]` lists them and adds the tracked
ones after a yes (`docs/dashboard.md`, `docs/design.md`). Each is named after
its folder. A row is skipped when its path or its name is registered, or when
another row has the same name (a tracked project keeps it ahead of a plain git
repo; add the other with `--name`). The scan looks at 500
folders at most, and says when it stopped.

Other commands: `ork projects list`, `ork projects show NAME` and
`ork projects rm NAME`. Removal is refused while the project has unfinished
runs (`docs/design.md:2604-2608`).

*Proposed (S247):* `ork projects show NAME` prints every setting of one
project in one read: ork's registry entry and global settings, slicer's config
and role matrix, and adj's state. It changes nothing. Text leaves out the
`vendor_env` values; `--json` prints the whole result.

The first add creates `~/.config/ork/projects.json`; ork never seeds it
(`docs/state.md:34-36`).

To pin the project's slicer relay contract, run
`ork projects pin NAME --relay 12` (v12 or v13, adj 470). ork runs
`slicer config relay_contract 12 --root PROJECT` and prints the pin slicer
then reports; the pin lives in the project's slicer config, not in
`projects.json`. On a project with adj off, `ork projects unpin NAME` clears
the slicer pin (`--json` prints `{"project":NAME,"pinned":null}`). On a
project with adj on, that same command clears the slicer pin and the adj pin.
If your slicer serves a different version than the pin, slicer refuses and the
command exits 1 naming both versions; the pin stays set, and `ork doctor`
lists the project as failed until you run `ork projects pin NAME --relay N`
with the version slicer serves, or `ork projects unpin NAME` (on a project
with adj on, that clears both pins), or install a slicer that serves the pin.
Neither command needs the daemon.

*Proposed:* `ork projects pin NAME --adj 1` pins the adj contract. The version
ork accepts is adj-json v1 (`adj contract --help`). With `--relay` and `--adj`
together, ork pins slicer first and pins adj only if that call succeeded, then
prints both pins. The printed adj version is the pin, `adj-json vN`. A
read-back whose pin is not the version adj speaks exits 1 and says the pin
stays set. If adj's call fails after the slicer pin was set, the message says
the relay pin stays set. `ork projects unpin NAME` clears the slicer pin and,
when the project has adj on, the adj pin; that is the recovery after a refused
slicer pin, and the message says so. If the adj unpin then fails, the message
says the relay pin was cleared and the adj pin is unchanged. `ork projects
unpin NAME --adj` clears only the adj pin. A project with adj off is unchanged:
unpin clears the slicer pin only. None of these write the pin into
`projects.json`.

## 5. Set a project's options

The registry holds optional per-project settings. Change them with
`ork projects set` (`internal/cli/cli.go:154-167`):

| Flag | What it sets |
|---|---|
| `--check CMD` | the check command ork runs after each implementer round. With none set, `ork run` and `ork wave start` refuse to start unless you add `--allow-no-check`, and `ork doctor` reports it (*proposed*, S265, adj 191) |
| `--no-verify` | with `--check` or `--round-check`: store the command even when ork finds it cannot run in the project's folder. ork remembers it for the project (`no_verify_check`), so runs do not pause at setup on that command; a later `--check` or `--round-check` without `--no-verify` verifies again and clears it. Without it ork checks, without running anything but `sh -n`, that the shell parses the command, the first word of each command is found, and the plain file names the command lists (`App.slnx`, `scripts/test.sh`, not flags, globs or `./...`) exist in the folder the check runs in, and refuses a command that fails, naming the file and the folder (*proposed*, S295). A run whose check cannot run pauses before round 1 with the same line; to fix it, `ork stop ID`, `ork projects set NAME --check CMD`, then `ork retry ID` |
| `--round-check CMD` | a faster command for the checks after implementer rounds (*proposed*, S285). Your `--check` command still runs in the landing re-check and, when it differs from the round command, after a green round check, before the review or the gate. Refused only while the project would have no `--check` command (a saved one is enough); empty clears it; refused while the project has unfinished runs |
| `--check-minutes N` | minutes the check command may run (default 10; `internal/engine/states.go:437-440`, `docs/state.md:87-89`) |
| `--worktree-pattern P` | worktree path; must contain `{id}` or `{id_lower}` (default `.worktrees/{id}`) |
| `--branch-pattern P` | branch name; same rule (default `{id}`) |
| `--cap N` | how many runs of this project may be busy at once (unset means no project cap) |
| `--budget-minutes N` | run-time budget per slice |
| `--budget-tokens N` | token budget per slice |
| `--push on\|off` | push the base branch after landing; off by default |
| `--auto-land on\|off` | ork approves and lands a run whose gate passes with no open owner finding and a green check, with no `ork approve`; on when unset, `off` is the opt-out (*proposed*, S331, adj 281). Refused while runs are unfinished: a started run reads it at its gate |
| `--bell on\|off` | the terminal bell for the project's runs and counts; on when unset (*proposed*, S224). Allowed while runs are unfinished; the TUI reads it at start, so restart ork to apply it |
| `--notify CMD` | *Proposed (S311):* run this executable when a run needs the owner or lands; `--notify ''` clears it. The whole value is one executable name or path, including spaces, with no arguments or implicit shell (adj 358). Allowed while runs are unfinished (adj 279). `ork projects list` and `ork doctor` display it; doctor warns if it cannot resolve to an executable |
| `--ui-doc PATH` | the project's UI design doc, relative to the project; the UI pass reads it |
| `--ui-pass-skills A,B` | comma-separated skill names stored in `ui_pass_skills`; only a Claude UI pass is advised to load them (*proposed*, S209) |
| `--spec-check-rows rNN,rNN` | the matrix rows the spec check runs on, in order: the first with an adapter runs, the next is its fallback; **a row listed here leaves every run's reviewer and gate chains**; `""` clears it, and with none set ork uses every row the matrix's `Spec check row (rNN)` rules name, in rule order (*proposed*, S258) |
| `--dry-run` | takes no value: every check a real call runs, including the busy check, still runs, and the output shows the project as it would be saved, without saving it; it still needs a field flag, so `--dry-run` alone is a usage error (*proposed*, S248) |

*Proposed (S311):* the notification executable runs in the project directory
with `ORK_EVENT`, `ORK_PROJECT`, `ORK_RUN`, `ORK_ITEM`, `ORK_STATE`,
`ORK_REASON` and `ORK_NEXT` in its environment. `ORK_EVENT` is `question`,
`awaiting-approval`, `paused`, `stopped` or `landed`; `ORK_STATE` is the raw
run state. Reasons are cleaned to one line without terminal controls.
`ORK_NEXT` holds the next owner command when one is available. Use an
executable wrapper script when the integration needs arguments.
Each fresh transition into a need is attempted once, including across daemon
restarts (adj 359). A later question, pause or approval in the same run
notifies again; same-state polls do not repeat an occurrence. Setting the
command does not notify for an existing state.
Delivery runs independently of the slice, has a ten-second timeout, and
logs a failure without retrying or failing the run. Daemon shutdown cancels
and joins outstanding deliveries. A recorded attempt may be lost if the
daemon exits before delivery; notification is best effort. The `failed`
event waits for a ledger read, as with `ork wait`.

Give one flag value, for example `--ui-pass-skills tui-design,frontend-design`.
The list replaces the previous list; `--ui-pass-skills ""` clears it.
The CLI trims whitespace around each comma-separated name, so `a, b`
stores `["a","b"]`. The shared validator refuses blank names, commas,
leading or trailing whitespace, NUL, CR and LF on every settings and registry
load or save path. Errors quote the bad name and identify a blank item's
position (counted from 1); `a,,b` and `a,` report blank item 2.
ork does not check whether a skill exists or read vendor files. With no list, the UI pass
gets only the neutral prompt; a Codex UI pass always gets no skills line.

An empty value or 0 clears a field. While the project has unfinished runs the
command is refused, except a call that sets only `--cap`, `--budget-minutes`,
`--budget-tokens`, `--worktree-pattern` or `--branch-pattern`, which no running
slice reads (*proposed*, S265). The refusal names the fields it will not change
now; set them after the runs finish or stop. The daemon needs no restart: the
next run uses the new values (`docs/design.md:2704-2716`, `docs/state.md:188-211`).

Three more fields take repeatable flags (*proposed*, S174):

| Flag | What it sets |
|---|---|
| `--env-line LINE` | an environment line for the role briefs; the lines given replace the list, and one empty `--env-line ""` clears it |
| `--permission ROLE=read-only` | the role's preset; `--permission ROLE=` removes the role's entry, so the default applies |
| `--preset go\|python` | the implementer's full allowlist for a language, written as plain patterns with the check command's pattern from the same call; for a Python project run `ork projects set NAME --check "python3 -m unittest discover -s tests -t tests" --preset python` (*proposed*, S265) |
| `--allow ROLE=PATTERN` | adds a tool pattern to the role's list, starting from the default list when the role has none, and skips a pattern already there (*proposed*, S265, adj 190; before S265 it replaced the list) |
| `--replace-allow` | with `--allow`, the patterns of the call become the role's whole list; it needs `--allow` and cannot go with `--preset` for the implementer |
| `--vendor-env VENDOR:KEY=VALUE` | one environment variable for a vendor's processes; `--unset-vendor-env VENDOR:KEY` removes one |

The presets include git ls-files, git mv, git rm (plain and `git -C` forms)
and non-recursive rm (adj 478). Existing stored lists keep their patterns.
Run `ork projects set NAME --preset go|python` again to replace the list
with the current preset, or add an entry with
`--allow implementer=PATTERN` (adj 190). The approval hook checks paths and
recursive rm flags for claude-local roles and for cloud Claude write roles
(`launch.cloud-remove-paths`). For cloud it denies paths outside the
worktree, a recursive rm and a removal it cannot check, and leaves every
other call to the CLI's own allowlist: it asks no owner questions and applies
no Read cap (`launch.cloud-hook-scope`, `launch.cloud-hook-passthrough`).
Cloud Claude also receives conservative
recursive rm deny rules; their runtime behavior is unprobed.

*Proposed (S346; adj 479):* when a run's implementer was denied a call that you
want to allow (`ork show RUN` names the denied calls), retry the run with the
pattern: `ork retry RUN --allow "Bash(git mv*)"`. Repeat `--allow` to grant
several. The grant goes to that run's implementer only (and the lead or parts of
a split run, less the slicer writes for a part); the reviewer, the gate and the
other read-only roles stay read-only, other runs of the project do not get it,
and the registry is not written. It works on a paused run and on a stopped run
you resume, also while other runs of the project are running. The run keeps the
grant for its later rounds; `ork show RUN` lists it as `granted: ...`. A blank
pattern, or one with a newline or a NUL byte, is refused before anything
changes, and so is a project whose implementer is `read-only`. To change the
default for every run, use `ork projects set --allow implementer=PATTERN`; it is
refused while the project has an unfinished run, so set it once none is left.

A role or a vendor key changes alone, so you do not retype the others. ork's
text output and error messages name vendor variables and never print their
values; `--json` prints the project as the registry holds it, values included.
Do not put secrets in `vendor_env`: `projects.json` is readable by other users
of the machine (`docs/state.md`).

Two settings belong to the whole registry. Change them with `ork settings set
[--global-cap N] [--daemon-idle-minutes N]` and read them with `ork settings
show`. `global_cap` defaults to 2 and `daemon_idle_minutes` to 10; 0 clears
one. Unlike `projects set`, `settings set` works while runs are unfinished, and
a higher cap starts queued runs at once.

*Proposed (S314):* two more settings say who you are to adj. `ork settings set
--owner NAME` stores your name once for all projects (an `ADJ_DECIDED_BY` in
the environment of the caller still wins). With a name known, the answers you
give to spec questions are filed in adj under it, for each question that has a
decision key and an adj-enabled project, and only when you picked one of the
offered options (a typed answer stays in the slice). `ork settings set
--file-answers off` turns that filing off. Without a name nothing is filed and
one notice says `no owner set; run ork settings set --owner NAME`. `ork
doctor` shows the owner and whether spec answers are filed.

## 6. The role lineup

Which model runs each role comes from the role matrix, which slicer owns.
ork does not edit it. Read it in ork: press `m` on the Roadmap, or run

    ork roles matrix PROJECT

Both show slicer's matrix text and a short `ork` section that says, row by
row, what ork does with it (the pane puts the `ork` section first, the command
puts slicer's text first): `used as claude`, `used as codex` or `used as grok`
(a Grok Implementer row too, *proposed*, S305), `skipped: no
adapter for AGENT`, `UI pass row` for every reviewer row that a rule or the
project's `ui_pass_rows` names and `Spec check row` for every row that a rule
or the project's `spec_check_rows`
names, `not used: the gate runs the reviewer's lineup` for Gate rows
(ork's gate follows the reviewer), `not run by ork` for the Orchestrator
row (ork is the orchestrator), and `not used: ork has no role ROLE` for any other
role. The view is read-only. Change the matrix with
slicer:

    slicer relay-roles show --root DIR
    slicer relay-roles add|set|remove|move|rule-add|rule-edit|rule-remove

To make other projects' matrices the same as one project's (*proposed*,
S261), copy it:

    ork roles matrix copy ork adjudicate --dry-run
    ork roles matrix copy ork adjudicate
    ork roles matrix copy ork --all

The first shows what would change and writes nothing. The second shows it,
then asks `Copy ork's matrix to adjudicate? [y/N]` (Enter is no); `--yes`
skips the question for a script. `--all` copies to every other registered
project, checks all of them before it writes any, and asks per project.
Each project keeps its own spec check and UI pass rows: the rows its
`Spec check row (rNN)` and `UI pass row (rNN)` rules name, the rows in its
`spec_check_rows` and `ui_pass_rows` settings and its Local trial rows, and
the rules that name them. A project with no `Spec check row (rNN)` rule is
refused: add one with `slicer relay-roles rule-add` first. The source's own
such rows and rules are not copied. Everything else is replaced, so
rules that describe only the source (ork's, for example, name `internal/tui`)
land in the other projects. Row ids in copied rules are rewritten to the
project's own. The change is left uncommitted in each project; a project
with no matrix is skipped, with the `ork projects setup NAME relay-roles-init`
command to run. It needs a slicer that has `relay-roles apply`.

`ork projects set PROJECT --ui-pass-rows r36,r14` selects the UI pass rows
in order: the first usable row runs and later rows are fallbacks. The list
uses matrix row IDs, trims spaces and refuses duplicates and invalid IDs.
`--ui-pass-rows ""` restores the rule defaults (r14 in ork). Every listed
row stays out of reviewer and gate chains. An unfinished run refuses this
setting change.

For the S304 trial only, ork's matrix classifies r35 as the local spec check
row and r36 as the local UI pass row (adj 341, 342). They stay excluded from
reviewer and gate chains even when unlisted. Normal runs use the cloud
defaults. The trial sets `--spec-check-rows r35,r16` and
`--ui-pass-rows r36,r14`, then clears both lists after the trial.

For each slice ork calls `slicer relay-roles recommend ID --well-specified
y/n --split y/n` (with yes and no unless you override them, *proposed*, adj 189;
the spec check holds a slice that is not ready) and turns the answer into the lineup for implementer,
reviewer, gate, and an architect when the lineup has one (the matrix gives one on size L) (`docs/design.md:189-208`, `docs/design.md:485-488`,
`internal/slicercli/write.go:108`). A matrix row for a vendor ork has no adapter
for is skipped; ork runs `claude`, `codex` and `grok` (`docs/design.md:207-222`). Grok
runs read-only roles (reviewer, gate, architect, spec check) in plan mode and
write roles (the implementer) under `--permission-mode dontAsk` with `--allow`
rules (adj 373, S305, *proposed*): `Edit` becomes `search_replace`, `Write`
becomes `write` and a `Bash(...)` entry passes as it is. A plan keeps a Grok
implementer row, and a launch override may name Grok as the implementer. The
rules do not contain Grok; ork relies on the tree fingerprint, the project
check, review and the gate. A matrix model written `FAMILY@latest`, such as `gpt-*-sol@latest` or
`grok-*@latest`, runs the newest model of that family: ork reads the vendor's
model list when it builds the plan and shows the result in the lineup (*proposed*,
S244). The plan, the launch pane and the lineup editor print such an entry as
`latest (resolves to MODEL)`, for example `codex latest (resolves to
gpt-6.2-sol) medium`, in place of the model name; the Waves lineup column shows
the compact `MODEL (latest)`; the lineup editor's custom
fields start from the text that was asked for (`gpt-*-sol@latest`), so keeping
the field asks for the latest again (*proposed*, S245). A bare `latest` is refused. A model without `@latest` is passed as written.

At launch you can change the lineup and the budgets for that one run. The
matrix stays as it was (`docs/design.md:196-201`). In the TUI the key is `e`
on the Waves view (section 9), or at the Roadmap launch confirmation.
To plan a split slice or one that is not well specified, use the command line:
`ork run PROJECT ID --split y` or `ork wave start PROJECT ITEM... --well-specified n`.
The TUI always plans well specified and not split; `e` edits the lineup, not
these two inputs.
On the command line, `--implementer`, `--reviewer` and `--architect` take
`VENDOR:MODEL:EFFORT`. The vendor name may be in any case (`Claude` and
`claude` are the same) and ork keeps it lowercase; an unknown name is refused
with the list of vendors. Repeat a role flag to give its whole chain, primary
first; `--architect off` alone drops that role. Gate follows reviewer and the
UI pass stays from the matrix. `--budget-minutes N` and `--budget-tokens N`
set run budgets; zero uses the project default. On `ork wave start`, these
flags apply to every selected item.

## 7. Run one slice

Pick a slice that is ready in the roadmap. A slice that lists its files,
failing tests, steps and checks is "well specified".

**In the TUI.** Open the Roadmap view and select the project. Press `l` to
open the list of ready slices, move to one and press `enter`. ork asks
nothing about well specified or split (it plans as well specified, not split,
*proposed*, adj 189); read the plan (worktree, branch, check command and lineup, plus budgets when overridden;
setup warnings follow the roles, and a project with no check command shows
`check: none` and cannot start, *proposed*, S265).
Before confirmation, ork runs the spec check. While it runs, the launch body
reads `spec check: ID, 1m12s; wait for the confirmation` (*proposed*, S264);
the clock counts from the plan reply, and the status line stays empty. Questions use the same
sitting as Waves: pick with `1`-`N`, or press `t`, type an answer and press
`enter`.
The last answer is recorded before confirmation. `s` or `esc` skips the rest
and closes the launch with `ITEM waits on N spec questions; l starts the
launch again`.
A failed check also blocks launch; there is no skip key. A refusal shows its
message without `refused:`; other errors keep their code. A project with no
spec check row fails with `no_spec_check_row`; the message gives the fix:
`ork projects set NAME --spec-check-rows rNN` or a `Spec check row (rNN)` rule
in the matrix.
Press `e` at the
confirmation to edit the lineup and budgets, `enter` to save and replan, or
`esc` to discard. Replanning an edited lineup does not repeat the spec check.
Press `y` at the confirmation to start (`docs/dashboard.md:1374-1402`,
`internal/tui/keys.go:313-322`).

**On the command line.**

    ork run PROJECT ID [--slivers]

It plans the slice as well specified and not split, prints the plan, and runs
the spec check before asking `Start this slice? [y/N]` (*proposed*, adj 189).
*Proposed (S241):* before the check it prints
`spec check: reading ID (it runs even with --well-specified)`, then
`spec check: ID ready` for ready items. The call waits for the role to finish.
`--well-specified` and `--split` do not skip it. `--slivers` (*proposed*, S62)
runs the parent's slivers one after another in the parent's worktree and
branch, as one plan and one landing. Answer each spec question
with an option number or text. A role's question text is shown as plain
text, and its line breaks are kept. An empty line or end of input holds the
slice and exits 1. A failed check also blocks launch. Other spec-check errors
carry the `spec check failed: ` prefix in text mode; refusals do not. Text
mode prints only `ork: spec check failed: MESSAGE`; the error code shows with
`--json`. A spec check whose report has a shape error, or whose commands were
denied, resumes its own session once with the error before it fails (*proposed*,
S266). A failure after that retry names the slices and says that checking
again is safe: the check changes nothing. A project with no spec check row
fails with `no_spec_check_row`; the message gives the fix:
`ork projects set NAME --spec-check-rows rNN` or a
`Spec check row (rNN)` rule in the matrix.
For a script, give `--yes`; it needs no other flag and still runs the spec
check by default. `--well-specified y/n` and `--split y/n` override the defaults (yes and
no). With `--json`, the sitting goes to stderr and
stdout holds one result. A held slice returns
`{"started":false,"held":["ID"]}`; declining the start returns
`{"started":false,"held":[]}` (`internal/cli/run.go`).

Use `--spec-check off` on `ork run` or `ork wave start` to skip the check
for one start (adj 473, `cli.spec-check-skip`). Only `off` is accepted;
it cannot be combined with `--questions` or `--answer`. Ork prints
`spec check: off (--spec-check off)`, keeps every planned item, and asks
the usual start confirmation unless `--yes` is given. With `--json`, that
line goes to stderr. The TUI still runs the check.
A skip produces no new answers' decisions paragraph, local orientation pack
or local carve. Existing cached material is retained. A local slice above
size S still needs its slivers filed before it can start, and the daemon
still refuses unanswered questions from the last check of the same spec.

*Proposed (S312):* `ork run PROJECT ID --questions` runs only the spec check
and prints every open question, numbered with its options, recommendation
and decision key. It records no answers, changes nothing in the project and
starts no run. It exits 3 when questions are pending (the same question code
as `ork wait`, S257), or 0 when none are pending. An item held only by a Check
finding prints its `held:` line and gives 0. The daemon may cache the check.
`ork wave start PROJECT ITEM... --questions` does the same for all items;
the saved-wave form accepts the flag too. When no questions are open, text
mode prints `no open spec questions: S1, S2`; a saved wave with no startable
items names the wave instead.

Answer all questions in one call with repeatable `--answer KEY=LABEL`, which
picks the option whose label is LABEL (trimmed, upper or lower case), plus
`--yes` to confirm the launch. `--answer KEY=N` picks an option by its
position, and other text is free text; a label does not move when the options
change, a number does (*proposed*, S404). A
question without a key uses `#N`, its one-based position within the item.
Quote that address in a shell, for example `--answer '#2=custom limit'`.
Flag order does not matter. A wave requires an item prefix on every answer:
`--answer ITEM:KEY=N` or `--answer 'ITEM:#N=text'`.
The wave listing shows `Decision key: k1 (answer as S1:k1)`. Keyless questions
show `Answer as: #N` for run and `Answer as: ITEM:#N` for wave.
Unknown keys, duplicate keys, empty answers and option numbers outside the
range are refused before any answer is recorded. Missing answers hold the
whole batch, list the missing addresses, print each missing question in full
with its options and answer address, and exit 3 without recording answers
or starting anything. Answers with no pending questions are refused.
Standard input that is not a terminal is never read for answers (*proposed*,
S404): `printf '1\n1\n' | ork run P ID` with open questions prints every
question with its answer address, prints
`held: ID: input is not a terminal; answer with --answer KEY=LABEL`,
starts nothing and exits 3. `ork wave start` does the same for the whole wave,
and its line names the prefixed form: `answer with --answer ITEM:KEY=LABEL`.
`--questions` cannot be combined with `--yes` or `--answer`.

With `--json`, these flags add `questions` and `answered` arrays to the one
stdout result. Each question has `item` and `address` plus the existing `ask`,
optional `key` and `text` fields. Use `address` as the left side of `--answer`;
it includes the item prefix for waves. `answered` entries are
`{item,key,answer}`, using `#N` for a keyless question and the recorded
`Label: summary` or free text as `answer`.
Listings and incomplete batches return only `{questions,answered}`, with an
empty `answered` array and no `held` field. The
interactive and piped-input prompts retain their existing behaviour.

For example, to use a Codex primary with a Claude fallback for this run:

    ork run PROJECT ID --implementer codex:gpt-6.1-sol:medium --implementer claude:sonnet:high --budget-minutes 20

The printed plan includes lineup overrides and, when overridden, budgets.
It is the plan confirmed at start.

*Proposed (S241):* the plan prints `push: on` or `push: off` after `stop:`;
the CLI reads the setting and the UI skills from `projects.list`, because the
daemon's plan carries only the project name and path (text mode only).
Fallback role entries end in ` (fallback)`; permissions stay once per role.
The UI role lists configured skills as `  skills: a, b` after permissions.
After the plan, `files:` says whether the slice touches `internal/engine`,
`internal/daemon` or `cmd`, has no engine path, lists no files, or could not
be read. A failed file read does not block launch. A started run prints
`RUNID STATE`, then `follow: ork status --project PROJECT` and
`stop it: ork stop RUNID`. JSON results keep their existing shape.
The run is now in the daemon. Closing the TUI or the shell does not stop it.

An agent that drives these commands can read `ork ai instructions` for the
`RUN` id rule, the steering commands and the owner-only steps (*proposed*,
S233; section 14).

The roles run in this order (`docs/design.md:391-631`):

1. **setup**: git worktree and branch, and a slicer ledger run.
2. **implement**: one implementer role.
3. **checks**: your check command and `slicer check`. A red result goes back
   to the implementer without spending a review. *Proposed (S239):* when your
   check command exits non-zero, ork runs it once more before it sends the
   round back; if the rerun passes, the round goes on and `run.json` marks the
   check `flaky`. If it fails again, the implementer's brief starts with the
   failing Go tests and their assertion lines. A timeout is not run again.
   *Proposed (S285):* with a round check command set (`--round-check`), this
   step runs it instead of your check command, and when it differs from your
   check command a green result is followed by one run of your check command
   before the review or the gate. The landing re-check always runs your check
   command.
   *Proposed:* when your branch
   is behind main and `slicer check` is red only on other items, the round
   goes on to review; ork names those problems to the reviewer, the gate and
   the finish step, and the landing merge and re-check decide them. On a run
   that stops after the commit, ork does not land, so your own landing decides
   them.
4. **review**: a reviewer, and the UI pass beside it on a slice that touches
   the UI. Findings go to one fix round.
5. **architect**: only when the lineup has an architect entry; the matrix gives one on size L (`docs/design.md:485-488`).
6. **gate**: the first line of its report is `VERDICT: PASS` or
   `VERDICT: FAIL`. The third FAIL stops the run.
7. **awaiting approval**: your turn (section 10).

**Writing Check lines.** Before the gate, ork runs each backticked plain
command in the slice's Check section (`docs/design.md`, slice checks). *Proposed
(S419, `checks.prints-nothing-exit`, adj 468):* word a line that finds nothing
as "prints nothing", "must print nothing", "finds nothing", "matches nothing"
or "returns nothing" (case-insensitive; S525). Such a line passes when its
output is empty and its exit is 0, or 1 from `rg`, `grep` or `git grep` (their
no-match code). Output, exit 2 or more, or exit 1 from any other command
fails it. ork does not run a piped line; the orchestrator runs it and judges it
by its last command, by the same rule. A line not worded that way must exit 0.
Backticked entries that are not commands refuse a start and hold the spec
check or draft; paths listed in Files that do not exist yet are exempt, so edit Check before
starting again when ork names a non-command entry. Give each command its own
backticked entry, because ork applies the entry's wording to the backticked
command only; an entry whose text names a second command outside the
backticks is flagged the same way.

## 8. Watch it

The Overview shows **Needs you** first, then **Running**, then recent and
ready slices. Each Running row shows the role at work, the stage bar and when
ork last heard from it. `!` after 10 minutes and `!!` after 30 minutes without
an event mark a slow or stale run (`docs/dashboard.md:503-506`). `enter` opens
a run's pane with the role tree, the live output and the actions.

When Codex runs out of credits or hits a limit, ork marks it out for 60
minutes (S169, *proposed*): the status line shows `codex out until 17:02`, and
new rounds start on the next row of the chain on another vendor, if there is
one. Only one Codex role runs at a time over all runs (spec-check rounds are
not counted); a run that waits for the slot says `waits for codex` on its
Running row. `ork vendor` lists the vendors that are out, `ork vendor out NAME
[--reason TEXT] [--until TIME]` marks one until you say it is back, or until
TIME (S299, *proposed*), and `ork vendor back NAME` clears it. TIME is `HH:MM`
(the next time the clock shows it), `YYYY-MM-DD HH:MM` in your zone, or a full
date and time with a zone such as `2026-11-04T07:12:00+01:00`. It must be in
the future. When the end is not the day the mark began, the footer and the list
show the day too (`codex out until 4 Nov 07:12`), and the year when that
differs. The list also shows the day of a start that is not today.

Marks you set are kept in the state directory, so a daemon restart or an idle
exit does not lose them. A mark whose end passed while the daemon was down is
not brought back. The 60-minute marks that ork makes on its own are not kept,
and a restart clears them. ork does not read a reset time out of a vendor's
message, so give the end yourself.

On the command line, `ork status [--project P]` prints the same counts and
rows, plus open spec questions, which the dashboard does not show yet.
*Proposed (S340, adj 475):* an item whose spec check left questions open
shows one line after the open adj records and held items, such as
`S1  2 open spec questions: a.b, c.d (answer: ork wave start P S1 --questions)`,
and each question counts in needs you. The line goes away once the questions
are answered or the spec changes; an item that waits on an adj record shows
that record instead. Answer with `ork wave start --answer` or
`ork run --answer`; a daemon restart asks again. *Proposed (S308):* `ork status` also lists stale manual relays, marked `stale`,
and includes them in the active-relays count. Recently ended manual relays
are not listed. It prints a line such as
`codex is out until 17:02 (since 16:02; reason)` for each vendor that is out.
Under the row of a run whose role or checks step is
running, it prints `working: implementer round 2, 14m (last output 2m ago,
slept 1h03m)` (S235, *proposed*): how long the step has run, how long ago the
role last sent output, and how long the machine slept meanwhile.
*Proposed (S285):* under a run with a check row, it first prints `last check:
CMD (ok|red, round N)`, the command of the newest check, so you can tell the
round command from the full one. *Proposed (S240, adj 526):* the first line
reads `NAME: needs you N, in progress M, active relays K`. A run's row starts
with its `ork-` id, the `RUN` that `ork approve`, `ork answer`, `ork retry` and
the other commands take; a row that starts with `relay` is slicer's, and its
`run-` id is not a `RUN` (a command given one says so). Each run row says when its
state began, as `waiting since 00:55 (2h10m)` or `updated 02:41 (4m ago)`. A
question with several lines prints one line per line, then `next: COMMAND`.
Every open finding that `needs you` counts has a `finding RUN N ...` line.
Add `--json` to any command for scripts (`internal/cli/cli.go:54`,
`internal/cli/cli.go:212`).

*Proposed (S318):* `ork status --summary [--project P]` shows one project's
Running, Needs you, Blocked and Next sections. Without `--project`, the current
folder selects the registered project. Each section shows up to five rows and
`+N more` for the rest; an empty section says `none`. Running names the run,
item, state, current vendor and role round, and elapsed working time. Needs you
names open adj and spec questions, approval and paused runs, and human checks
marked `later`, with commands to inspect or act on them. After approval, the
summary keeps the check text and points to `ork show RUN` for run details.
Landed checks also live in
`human-checks-todo.md`. Blocked uses slicer's dependency reasons and items flagged
`needs-owner`. Next starts with slicer's next item, followed by unblocked items
in slicer's order, excluding held items, with a note for existing open spec
questions. ork does not
score items or compute dependencies. `--json` has `project`, `running`,
`needs_you`, `blocked` and `next`; each section has `rows` and a `more` count.
Text wraps each logical row at 80 columns, with two spaces on its first line
and four on continuation lines. JSON keeps each row whole. Runs awaiting
approval point to `ork approve RUN`; other needs-you runs point to `ork show RUN`
for the reason and next steps. Failures reading items or open adj records, or
slicer status or deps, are errors rather than empty sections. Failed snapshot
parts the summary does not read do not prevent it from printing.

### Wait for a change

*Proposed (S257, S403):* `ork wait [RUN...] [--until STATE,...] [--timeout D]
[--project P] [--stage]` blocks until a run changes, so a script or an agent
does not poll `ork status`. With one RUN it prints `RUN STATE` (for a pause
also the pause kind, the question title and decision key of a question, and
`next:`), exits, and the exit code says why:

| Code | Meaning |
| --- | --- |
| 0 | a run changed state, or entered a state in `--until` |
| 3 | a run is paused on an owner question |
| 4 | nothing is active: every run is final or unknown, or the project has no unfinished run |
| 5 | `--timeout` passed with no stop condition |
| 6 | `--stage`: a role round ended, and no run changed state |
| 1, 2 | failure, usage error, as every command |

When one read has several results, 3 wins over 0, and 0 over 6.

- Without `--until`, any change of the run's state from the first read ends the
  wait. Output from a running role does not change the state, so it does not
  end it.
- With `--until`, the wait ends when the run is in one of the listed states,
  also at the first read. The states are `question`, `awaiting-approval`,
  `paused`, `stopped` and `landed` (a committed run that ork landed); the list
  is in `docs/state.md`. There is no `failed` state for now: a run's record
  does not say whether a ledger run ended with outcome fail, so `--until
  failed` is an unknown state (S311 can add it later).
- A pending question ends the wait with code 3, whatever `--until` says,
  except when you have answered it and the run only waits for a slot under
  the global cap. That run counts as in progress and matches no wait state.
- A run that is final (`committed`, `stopped`) and matches nothing ends the
  wait with code 4 and a one-line message.
- Name several runs (`ork wait r1 r2 r3`) to follow them together. Each read
  reads every followed run. The wait ends on the first read in which at least
  one run is done, and prints one line per run that is done, in the order
  given: `RUN ITEM STATE`, then ` key: KEY` for a question (its state reads
  `question`), then ` next: COMMAND` when there is one. For example
  `r2 S2 question key: cache.store next: ork answer r2 TEXT`. A run that is
  final and matches nothing is dropped, with `r1 is stopped, so nothing is
  active; still waiting on r2` on stderr; when none is left the wait ends with
  code 4 and `nothing is active: r1 stopped, r2 landed`. A name the daemon does
  not know is dropped the same way, as `unknown`.
- Without RUN, the wait follows every unfinished run of `--project P`, or of
  the only registered project, as of the start. A run that starts later is not
  followed. It is a usage error when there are several projects and no
  `--project`. When that is one run, the output is that of one RUN.
- `--stage` also ends the wait when a role round of a followed run ends: the
  number of its role rounds with an end grew since the last read. It prints
  `RUN ITEM stage ROLE round N ended EXIT`, for example
  `r1 S1 stage implementer round 1 ended clean`, and exits 6 when no run
  changed state in the same read. A round that ended before the first read is
  not a stage end. A state change in the same read is printed as a state
  change.
- With `--json` and one run the state it stopped on is one object:
  `{"run","item","state","reason","next","pending":{"kind"}}` (empty fields
  left out). With several runs it is an array of these objects. A stage end is
  such an object with `state` `stage` and `reason` `ROLE round N ended EXIT`
  (adj 460, `cli.wait-stage-json`). A timeout or nothing-active result is the
  usual `{"error":...}` object with code `timeout` or `nothing_active`.
- The wait subscribes to the daemon's change notices. The daemon sends one when
  a run record's state, pending question or ended role count changes
  (`docs/design.md`), so the wait reads at once. It also reads every 30 seconds
  as a safety net. If the daemon connection drops it connects again once (and
  starts the daemon as other commands do); a second drop before a read is a
  failure.

For example `ork wait r1 r2 --until awaiting-approval,paused,stopped,landed;
echo $?`.

## 9. Run a wave

A wave is several slices of one project that start together
(`docs/design.md:175-361`).

**In the TUI.** Open the Waves view (`tab` from Questions). It loads slicer's
suggested waves. For each item you want:

1. Press `space` to tick it. ork plans it at once as well specified and not
   split, and asks nothing (*proposed*, adj 189). `e` edits the lineup, not
   these two inputs; for a split or not-well-specified item use
   `ork wave start PROJECT ITEM... --split y` or `--well-specified n`.
2. Press `e` to edit the lineup or budgets for that run, if you want to.
3. Press `l` to launch. ork first runs the spec check over every ticked item.
   If it has questions, they come one at a time: `1`-`4` picks an option, `t`
   types an answer, `s` or `esc` skips the rest. Items with open questions are
   held back. The Roadmap launch uses the same sitting for one slice
   (section 7); skipping closes that launch.
4. Confirm with `y`. The status line shows `ork wave start PROJECT ID...`
   (`docs/dashboard.md:1187-1223`, `internal/tui/keys.go:313-322`).

**On the command line.**

    ork wave suggest PROJECT
    ork wave start PROJECT ITEM... [--yes] [--well-specified y/n] [--split y/n]
    ork stop WAVE-ID

**Save a wave and start it by name** (S215, *proposed*). A wave you start
often can be saved under a name. ork keeps the name and the item ids of one
project in `waves.json`, beside `projects.json`, and nothing in the project's
repository:

    ork wave define --project PROJECT NAME ITEM... [--replace]
    ork wave list [--project PROJECT]
    ork wave start [--project PROJECT] NAME [--yes] [--well-specified y/n]
                   [--split y/n]
    ork wave rm [--project PROJECT] NAME

- A name is 1 to 32 characters of `a-z`, `0-9`, `.`, `_` and `-`, starts with a
  letter or digit, and does not start with `wave-`. A wave holds 1 to 20 items
  that slicer knows, each once. A name the project already has is refused
  unless you add `--replace`.
- `--project` is required on `define`. On `list` it limits the output. On `rm`
  and `start` you can leave it out when the name is saved for one project only;
  if two projects each have a wave of that name, ork says so and asks for
  `--project`.
- `ork wave list` prints each wave as `NAME  PROJECT` and then one line for each
  member with its status and title, read from slicer each time. A member that
  has finished shows as `done`; one that slicer no longer lists shows as
  `missing`.
- `ork wave start NAME` leaves out the members that are done, retired or
  missing, and prints `skipped ID: STATUS` for each. It then plans, checks the
  specs, confirms and starts the others exactly as `ork wave start PROJECT
  ITEM...` does, with the same flags for every member. If nothing is left, it
  starts nothing, prints `no item is ready to start` and exits 1. Each run's
  record carries the name as `wave_name`; each start is still a new wave id,
  and `ork stop` takes that id, not the name.
- `ork projects rm PROJECT` drops the project's saved waves.

In the Waves view (S216, *proposed*), tick the items you want and press `w` to
save them under a name: type the name and press `enter` (`esc` cancels). A name
the project already has asks `Replace NAME? [y/N]`. Press `L` for the project's
saved waves, each with its members, their status and titles; `up` and `down`
pick one and `enter` loads it. A load ticks the members that are still to do and
that the table lists, plans them, and names what it left out (done members, and
members slicer's suggestion does not offer, with the reason). It never starts
anything: press `l` as for any ticked set. The start carries the saved name only
when the items you launch are exactly the saved wave's members that are still to
do. It starts as a plain wave, with no name, if you untick or add an item, if a
member is still to do but the table does not list it (blocked, held or
unspecified), or if an item is held for spec questions. The screen does not show
the name, and removing a saved wave stays `ork wave rm`.

A wave may hold only one slice that touches `internal/engine`,
`internal/daemon` or `cmd` (`docs/design.md`, Self-hosting). The suggestion,
in the TUI and in `ork wave suggest`, keeps the first such slice of a wave and
puts each further one alone in the next wave, with the reason `engine: ID`
(S218, *proposed*). Such a slice runs alone unless you tick another slice
with it.

Like `ork run`, `wave start` asks the spec-check questions on standard input
unless `--spec-check off` skips the check for this start (also for saved waves),
and prints each plan before it asks to start (`docs/design.md:1958-1973`,
`internal/cli/wave.go:445-449`). `ork stop` takes the `wave-` id it prints and
stops every unfinished run in that wave, queued ones included
(`docs/design.md:2045-2051`).

*Proposed (S337):* a caller with no terminal (standard input from a pipe or
`/dev/null`) cannot confirm the start. Without `--yes`, `--questions` or
`--answer`, `wave start` prints the plans and
`not started: input is not a terminal to confirm on; rerun with --yes, or with --questions to see the spec questions`,
exits 2 and runs no spec check, so it makes no model call. Pass `--yes`, or
run `--questions` and then `--answer`. With `--json` it first prints
`{"started":false,"held":[],"no_spec":[]}` on stdout. `ork run` is unchanged.

*Proposed (S241):* when a launch plans two or more items, it prints
`pair rule: ok, no engine slice` or
`pair rule: ok, one engine slice (ID)` before the spec check. Two engine
slices are refused with exit 1 before the spec check, including with `--yes`
or `--json`. `--questions` plans nothing, so it prints no pair rule line and
refuses nothing. A started wave prints its wave id, one `ITEM RUNID STATE`
line per successful item, `follow: ork status --project PROJECT` and
`stop the wave: ork stop WAVEID`. Item errors stay on stderr. When no item
started, the two commands are not printed.

An item that has no slice spec does not fail the wave (S283, *proposed*, adj
245). `wave start` leaves it out, prints `ID: no slice spec; run slicer
promote ID`, and starts the others; the exit code is 0 when a wave starts. If
no item is left, nothing starts, it prints `no item is ready to start` and
exits 1. With `--json`, the line goes to stderr and the result has a
`no_spec` array of the skipped IDs beside `held` (`[]` when none). Any other
failure while planning an item still stops the whole command before anything
starts.

Runs start as slots free. The global cap is 2 and a project's cap is its
`cap` setting. A run over a cap waits as `queued`
(`docs/design.md:315-316,330-332`). One failed run does not stop the others.

A paused run holds no slot, so other runs start while it waits for you. When
you then answer or retry it and the project cap or the global cap leaves no
room (S274, *proposed*), the command replies at once with `resume waits for a
slot; it starts before queued runs`. The run stays `paused`, `ork status` shows
`resume waits for a slot under project cap N; next: ork stop RUN` (or `under
global cap N`), and it
starts as soon as a slot frees, ahead of any queued run. An answer you file in
adj from the TUI is filed at once; only the run's resume waits. A second answer
or retry is refused while one waits; `ork stop RUN` ends the run and drops the
request. Approve and stop do not wait for a slot. In the TUI the run leaves
Needs you and the Questions view and shows under Running while it waits.

*Proposed (S307, adj 472, adj 511):* `ork answer --wait` and
`ork retry --wait` also wait for a queued resume to get a slot and rest again.
Without `--wait`, and in the TUI, the slot-wait notice still returns at once.
A waiting answer's adj filing finishes even if its client disconnects.
Stopping an answer after its adj filing succeeds reports
`run RUN was stopped; answer filed in adj as #N`.

## 10. Answer questions, approve and land

### Answer questions

A run pauses when a role says it needs you. Questions from every project
appear in the **Questions** view and under **Needs you** on the Overview.

- In the TUI: `enter` opens the run. A digit `1`-`4` loads that option and `y`
  sends it; `t` types a free answer and `enter` sends it. *Proposed (S355):*
  the daemon files the answer in adj when the question has a decision key, the
  project has adj and your name is known (the TUI's `ADJ_DECIDED_BY`, else
  `ork settings set --owner NAME`); `ctrl+a` turns that off for the pane you
  have open.
- On the command line: `ork answer RUN TEXT`. *Proposed (S355):* the daemon
  files the answer in adj by the same default, under the owner setting (the CLI
  sends no name; `ork settings set --file-answers off` stops it, and the CLI has
  no flag to opt out). *Proposed (S237):* the
  command returns as soon as the run has resumed and prints its new state
  (for example `r1 implement`); `ork status` shows the role at work. Add
  `--wait` to wait until the run rests again. `ork retry RUN` works the same
  way, also for a stopped run (S296). RUN is the ork run id; a slicer relay
  run id gets an error that names the ork run.
- Findings tagged owner but not blocking do not pause the run. Answer one
  with `o` in the run pane (`internal/tui/keys.go:113`).
- A finding tagged later can become a new slice: `f` in the run pane, or
  `ork file RUN N` (`internal/cli/cli.go:97`).

Spec-check questions (before a wave) are answered in the sitting that opens
at launch. See section 9.

*Proposed (S313):* ork reuses an exact decision key's decided adj option when
it still matches the question's label and summary. It records the reused
answer with `[KEY, adj N]` in the slice and proceeds without asking again.
When the option changed or disappeared, the question shows `decided as X on
DATE` and asks again. An open record holds the item with `waits on adj N` in
the CLI and spec pane; answer it in adj, then run the check again. Questions
for that item wait until its open adj records resolve. Missing records are
asked normally. A lookup failure keeps the question and prints one notice
explaining the failure.

### Approve and land

A slice's Git section names its commit title in double quotes, for example
`One commit on the slice branch, message "S04: engine core".` ork uses the
first double-quoted title. Anything without a double-quoted title, including
an empty or missing Git section, gives `ID: slice title` from `slicer show`.
An explicit empty quoted title (`""`) pauses the run at commit
(adj 485, `git.commit-title`).

*Proposed (S331; owner decision 2026-11-11, adj 281):* there is no approve step
for a run that passes everything. When the gate says `VERDICT: PASS`, no owner
finding is still open and the latest check is green, ork approves the run
itself, runs the finish step, commits, lands and cleans up, with no
`ork approve`. This holds for every project; `ork projects set NAME --auto-land
off` opts one out. Human checks never hold a landing back: ork marks each open
one *later*, and at landing it adds the checks marked later to
`human-checks-todo.md` at the project root, under `## ID (run RUN, DATE)` as
`- [ ] TEXT` lines (a deferred line starts with `[AFTER LAND]`). The file is
created with a header when it is missing, and the section is committed with the
landing's tracking commit. A run goes to the card below instead when the gate
fails, an owner finding is open, the check is red, the project opted out, the
run was started with `--stop-after commit`, the card could not read the
slice's human checks, or ork could not record the approval (the reason shows on
the run). A landing that pauses (a merge conflict, a red re-check, a failed
push) pauses as before, and `ork retry` carries on with the landing. A landing
that already used its one fix round stops with `land blocked: ...; ork retry RUN re-lands with one more fix round`. `ork retry RUN` on that run merges the base branch again and lands when the re-check is green (adj 458, adj 459). A landing
you approve by hand adds the checks you marked *later* to the same file.

When the gate passes and the run does not land by itself, it waits in **Needs
you** as "Approve". Open it.
The approval card shows the gate result, the diff summary, owner findings and
the slice's human checks (`docs/dashboard.md:836-945`).

*Proposed (S217):* on open and refresh, the card warns about overlapping
uncommitted files in main (excluding tracking files landing handles), or
main being detached or on the slice branch; the warning does not disable
approval, and directory-versus-file clashes are not detected.

- Press the check's number to mark each human check: ticked, then later, then
  open again. `a` works only when none is open.
- `F` opens a fix note. Type it and press `enter`. The run goes through a fix
  round and comes back to the card.
- `v` shows the UI pass's renders, if it wrote any.
- `a` approves. ork runs the finish step, commits, and lands the slice: it
  merges the base branch into the worktree if the base moved, re-runs the
  checks, fast-forwards the main checkout, runs `slicer land`, pushes if the
  project's `push` setting is on, and removes the worktree and the branch
  (`docs/design.md:646-810`).

For ork CLI text views, the UI pass brief gives this command with the temp
directory named under Your files in its brief in place of DIR:

    go test ./internal/cli -run TestRenderCLIViews -renders DIR

It writes `status.txt`, `status-summary.txt`, `status-summary-long.txt`,
`show.txt` and `help.txt` from the CLI test fixtures to that existing
directory, without changing the tree. Add `-render-fixture NAME[,NAME...]` to
write only `NAME.txt` for each name. The names are the five above (without
`.txt`) and `projects-set`, `projects-set-refusals`, `wave-start`,
`status-queued-held`, `status-engine`, `summary-real`, `projects-setup`,
`fake-reply`, `matrix-copy-preview`, `matrix-copy-ask`,
`matrix-copy-stale-preview` and `wave-start-finding` (a wave start with `--yes` that holds one
item on a Check finding and starts the other). A view of several commands puts a `$ ork ARGS` line, with the
arguments quoted for a shell, above each command's output. The files contain
plain text as printed, without imposing a terminal width. Without `-renders`
the render test writes nothing. The pass copies selected scratch files into
its `renders/` directory with the prompt's `VIEW-80x24.txt` names so they
appear on the approval card.

`TestRenderFixtureDocsListEveryFixture` checks that every additional render
fixture has an exact name in the `-render-fixture` lists in this guide and the
design document, ignoring mentions elsewhere in the documents.

The UI pass brief also says that a `go test -run` pattern must not contain a
pipe, because `|` makes a compound command that the permission check denies.
It gives the form that is allowed: one test per command, for example
`go -C WORKTREE test -run TestRenderCLIViews ./internal/cli`, and the next
test in its own command.

*Proposed (S254):* before it merges, ork looks at the main checkout. If you
have uncommitted changes there to a file the slice also changes, the run
pauses at once with one line that names the files; commit or stash them and
press `t` (`ork retry`). If the only such files are the project's tracking
files (`.slicer/`, except `config.json` and `relay-roles.json`), ork commits
the item's own and shared files on the base branch itself as
`Tracking edits on main before landing ID` and goes on. Another item's
overlapping slice JSON (including under `slices/done/`) or slice render
pauses the landing before any commit (adj 483, which narrows adj 193).
The reason names at most five files and a count of the rest. Commit them
with `ork tracking commit` or discard them, then `ork retry`. This also
applies to edits made during the landing's re-check.
Files the slice does not change are left alone (the `ID done`
commit after `slicer land` still commits any `.slicer` change).

From the shell: `ork check RUN N [ticked|later|open]`, `ork fix RUN TEXT`,
`ork approve RUN` (`internal/cli/cli.go:86-93`).

*Proposed (S316):* `ork approve RUN` stops while a human check is open and
names the open ones. `ork approve RUN --later-checks` marks every open human
check `later` first, then approves in the one call, and prints a line
`deferred: N. TEXT` for each check it deferred (TEXT cut to its first 80
characters, with `...` after a cut), so you see what was pushed out. `ork check RUN all later` does the
same marking and approves nothing. A check already `ticked` or `later` is left
as it is. The marks are the ones `ork check RUN N later` writes, so the
follow-up list and the `[AFTER LAND]` handling do not change. `later` means you
have not looked: use the flag when you mean that. There is no `all ticked`;
ticking stays per check.

*Proposed (S222):* when a wave has two or more runs waiting at the card, you can
approve them as one action instead of one after the other: `ork approve
WAVE-ID`, or `A` on an Overview row or in a run pane (it acts on the wave of the
selected run and asks `y/N` first, naming the runs in start order). ork approves
the first run, waits until it has landed (or come back to its card after a fix
round), then approves the next, in the order the runs were started. It stops at
the first run that does not end committed: that run keeps its state (a fix
round or a pause), the runs after it stay at their cards, and `ork approve`
prints `failed` and `not-attempted` for them and exits 1. A run that landed
stays landed. A run you started with `--stop-after commit` ends committed
without landing; that counts as done, the wave goes on, and you land that run
yourself later. It never ticks a check for you: if any waiting run has a human check
that is neither ticked nor marked later, the whole call is refused, approving
nothing, and names each run and its open checks. Mark them (`ork check RUN N`),
then run it again. `--later-checks` is for one run and is refused with a wave
id. For a single run use `a` or `ork approve RUN` as before.

*Proposed (S238):* if the merge has a conflict, ork merges the `CHANGELOG.md`
entries itself and pauses the run on any other file, with a one-line message
that names the files and the worktree. Resolve them in the worktree (no need
to `git add`), then run `ork retry RUN`: ork stages the files, commits the
merge, renders, re-checks and lands. No role takes part. If the re-check is
red, the slice gets one implementer fix round and comes back for a new
approval; `ork status` then says the approval is a repeat. A second red
re-check stops the run with the worktree kept (`docs/design.md`, land).

## 11. Watch several projects

Use this loop each day:

1. Open `ork`. The sidebar lists **Running**, **Needs you** and each project.
   `0` jumps to Needs you, `1`-`9` to a project, `[` and `]` move between them
   (`internal/tui/keys.go:121-127`).
2. Clear **Needs you** from the top. Group order is Decisions, Held, Stopped,
   Questions, Paused, Approve, Findings, Errors (`docs/dashboard.md:463-464`).
3. Open **Questions** for every open question in one list.
4. Open **Decisions** for adj records and **Roadmap** for what is next.
5. Press `r` to refresh now. `/` filters projects. `?` shows every key.

ork rings the terminal bell once when an open adj or held-item count rises,
or a run ends (`Model.apply` in `internal/tui/model.go`, S100).
An engine run ends at `committed` or `stopped`; a ledger-only relay ends at
`ended`. Linked engine and ledger runs ring only once. The first good
reading is a baseline and rings nothing. The per-project registry field
`bell` (*proposed*) is on when absent or `true`; `"bell": false` silences
every bell for that project. Counts and sidebar badges still update.
The TUI reads the setting at start, so restart ork after changing it.
Set it with `ork projects set NAME --bell on|off` (*proposed*, S224).

## 12. Recover

| Situation | What to do |
|---|---|
| A run is paused with no question (shape, error, lost, interrupted) | `t` on the row, or `ork retry RUN` (`docs/design.md:2070-2077`). *Proposed (S256):* `ork retry RUN "TEXT"` also sends a note, for example after you fixed the tree: the rerun brief shows it as an owner note. A resumed role is told to read `git status` and the diff again; the `t` key sends no note. `ork answer` and `ork fix` do not work on such a pause |
| A run is paused on report shape twice in a row for one role and round (*proposed*, S298) | The reason names what is wrong (for an owner finding: the missing or misnumbered part, with the offending line), the refused report file and `ork show RUN`. A plain `ork retry RUN` (and `t`) is refused. Read the report with `ork show RUN`, then choose: `ork retry RUN --accept-shape` keeps each owner finding that has no options as an open owner finding (an `[owner blocker]` becomes a question you answer in words) and runs no role; it refuses any other shape problem. `ork retry RUN --again` runs the role once more; if that fails too the pause is capped again. Or `ork stop RUN`. The first shape pause is not capped: `ork retry RUN` works as before, and its brief now carries the exact error and the finding's lines |
| You need to know why a run is paused, or read the report that was refused | `ork show RUN` (*proposed*, S297) prints the whole pause reason (and the question with its options), the roles with their report names, the check results with their log names, the owner and later findings, and the paused role's newest report (or the run's last report when that role has none). `ork show RUN NAME` prints one report (`reviewer-1-attempt-3.md`) or one check log (`checks-2.log`). Both take `--json`. It reads only reports and check logs, so you need not open `~/.local/state/ork` |
| A run is paused at a budget limit | `t` or `ork retry RUN` grants one more role round (`docs/design.md:1860-1869`) |
| A run is paused after red rounds or no progress (*proposed*, S294) | Read the named failure in Needs you or `ork status`. `t` or `ork retry RUN` grants two more implementer rounds; `ork retry RUN --rounds N` grants a positive count. If still red at the granted limit, it pauses again. To change the check, `ork stop RUN`, set the check (`ork projects set P --check CMD`), then `ork retry RUN` resumes the stopped run at the checks with the new command (*proposed*, S296). Or stop the run. The pane shows the choices in words; status adds retry and stop commands in its trailing next-step hint |
| A run is stopped | Read the reason in the pane. Fix the cause and continue, or clear it with `ork dismiss RUN`. *Proposed (S296):* `ork retry RUN [--rounds N]` resumes a stopped run that has no commit and whose worktree is intact: it grants N more implementer rounds (2 by default), reloads the project's settings, begins a new ledger run and resets the gate FAIL count. It re-enters the checks (a green check goes on to review) when the stopped run's last pause held a red check or no feedback; it starts an implementer fix round that carries the findings (the gate's, the reviewer's, the architect's or the owner's) when the pause held those (*proposed*, S349); and it starts implementer round 1 when no implementer round has run. A dismissed run can be resumed too, which clears the dismissal. `ork status` and `ork show RUN` name the command (`ork retry RUN [--rounds N] or ork dismiss RUN`), and name none for a run that a newer run of the item has superseded. `ork retry` gets past the project and global caps, like every owner action. `ork run` or `ork wave start` on the same item resumes the newest stopped run by itself when its worktree is exactly what the last implementer round left; it joins the new run's wave and keeps the stopped run's lineup, budgets and stop-after (the message says so). Otherwise it starts a new run at implementer round 1 in the same worktree and prints why. A dismissed run is not resumed that way. With no free slot the start is refused, naming the stopped run: resume it with `ork retry RUN`, or `ork dismiss RUN` to start a new run. A committed run that stopped while landing (`land blocked: ...`) is different (adj 458, adj 459): `ork retry RUN` merges the base branch again, re-runs the landing checks and lands when they are green. A red re-check gets one implementer fix round, then stops with `land blocked` again if it is still red. `ork retry RUN --rounds N` is refused, because that landing gets one fix round. A committed run that stopped in any other state, or whose worktree or branch is gone, is refused with a line that says why. `ork run` and `ork wave start` do not resume a committed run stopped in land |
| A run is paused with a reason like `implementer round 1 hung: no output for 30m` | ork killed a role that sent nothing for 30 minutes. Time the machine slept does not count, so a wake alone does not cause this (S235, *proposed*). Read the reason in the pane, then retry with `t` or `ork retry RUN`: the same role runs again and is told that the earlier process was lost. ork does not retry for you |
| A relay that ork did not start shows `!!` and `stale` | Open its pane and press `x`. It ends the ledger run as stopped (`internal/tui/pane.go:413-416`, `internal/tui/ledgerend.go:21`), but only when the run is stale and no unfinished ork run owns it (`internal/daemon/runs.go:2515-2531`). `ork stop RUN` does not take such a run: it names ork runs and waves, and the daemon answers `no run` for a ledger id (`internal/daemon/runs.go:1624-1635`) |
| Codex is out of credits or at a limit | Nothing is needed: ork marks it out for 60 minutes and starts new rounds on the next row of another vendor. Check with `ork vendor`. To keep it out longer, `ork vendor out codex` holds until you run `back` (it survives a daemon restart), and `ork vendor out codex --until 07:12` holds until then. `ork vendor back codex` ends the mark early, for example after you add credits. A run that waits for the one Codex slot is not stuck: it starts when the other Codex role ends |
| You want to stop work | `x` on a row or `ork stop RUN`; `ork stop WAVE-ID` for a wave |
| Status or the TUI status line says `rebuild pending: waiting for N roles` (*proposed*, S431) | The project has `ork projects set NAME --auto-rebuild on`, a landing left the daemon behind main, and the daemon is draining: it starts no new role, lets the N running roles finish and kills none, then builds and restarts itself. A run that reaches its next role waits with `waiting for daemon rebuild`. Nothing is needed. After the restart each held run is paused as interrupted: `t` or `ork retry RUN` starts its role on the new build. While the drain runs `ork daemon stop` is refused, because the held runs still count as active work; `ork daemon stop --force` ends the drain but also cancels any role that runs |
| Status says `daemon not rebuilt: build failed (exit 1)` with output lines (*proposed*, S431) | The build command of a project with `--auto-rebuild on` failed, the built file was not an executable, or the build left the daemon's own executable at its old revision (the message names the file: the build command must write the file the daemon runs from). The old daemon keeps running, the hold is released and held runs went on with the old build. Fix the build, then land the next change: the daemon tries again only when main moves. Or run `ork daemon restart --rebuild` by hand when no role runs. `ork projects set NAME --auto-rebuild off` returns to the manual restart |
| The daemon died or you stopped it | Start `ork` again. On start the daemon checks every unfinished run against live processes and marks a role with no process and no report as lost, then the slice waits for you (`docs/design.md:167-173`) |
| Commands warn that the daemon build differs | `ork daemon restart`. It refuses while a role runs unless you add `--force` (`docs/design.md:41-48`) |
| The self-host daemon is behind main (*proposed*, S315) | Set a build-only command once with `ork projects set ork --build 'sh ./build-only-in-home-dir.sh'`, then run `ork daemon restart --rebuild`. It refuses while a role runs, naming the run, even with `--force`. A failed build prints its last twenty output lines and `daemon not restarted`; the old daemon stays running. The script writes `~/.local/bin/ork` outside the workspace, so the owner or a session with approval runs it. ork does not rebuild on its own unless the project sets `--auto-rebuild on` (next rows). `--build ''` clears the setting; without it the behind notice keeps the plain restart hint. `build-in-home-dir.sh` remains the manual build-and-restart script |
| A merged slice left its worktree behind | `c` on the Ready row removes the merged worktree and branch (`internal/tui/keys.go:89`) |

Run `ork doctor` after you upgrade slicer or adj. The slicer contract must
still be one ork accepts.

Upgrade the CLI and TUI together with the daemon. An older client against a
restarted newer daemon may see `plan changed; run the command again to confirm
it` for a plan with warnings. Since S307 (adj 512), `projects.set` replies with
a wrapper containing the project and per-role added and already-listed pattern
lists; an older CLI cannot read this reply. `ork projects set --allow` now
reports both counts, for example `implementer: 1 pattern added, 1 already listed`.

## 13. Keys at a glance

The TUI draws the keys that work right now on its hint line, and `?` lists
them all (`internal/tui/keys.go:82-157`).

| Where | Key | Action |
|---|---|---|
| Anywhere | `tab` / `shift+tab` | next / previous view |
| Anywhere | `[` `]`, `1`-`9`, `0` | previous or next project, jump to project, Needs you |
| Anywhere | `+` / `-` | add / remove project |
| Anywhere | `/`, `s`, `r`, `?`, `q` | filter, sidebar, refresh, help, quit |
| Anywhere (*proposed*, S251) | `S` | open the project settings pane: every setting by owner (`esc` closes) |
| Project settings pane | `i`, `m`, `j` `k` | run a setup step after a `[y/N]` that shows the command; open the role matrix; scroll |
| Lists | `j` `k`, `g` `G`, `pgup` `pgdown`, `ctrl+d` `ctrl+u` | move and scroll |
| Overview | `enter`, `esc` | open / close a run |
| Overview, run pane | `a`, `x`, `t`, `c` | approve, stop, retry, clean up a merged worktree |
| Overview, run pane (*proposed*, S222) | `A` | approve every waiting run of the selected run's wave, in start order (asks first) |
| Run pane | `1`-`9`, `F`, `v`, `f`, `o` | mark check, fix note, renders, file slice, answer owner finding |
| Question | `1`-`4` then `y`; `t` then `enter`; `ctrl+a` | load an option and send it; type a free answer and send it; turn adj filing off or on for this pane |
| Running entry | `space`, `E` | expand a run, expand all |
| Roadmap | `l`, `m` | launch a ready slice, show the role matrix (read-only; `esc` closes) |
| Waves | `space`, `e`, `l`, `A` | tick, edit lineup, launch, show all waves |
| Waves (*proposed*, S216) | `w`, `L` | save the ticked items under a name, list and load saved waves |
| Decisions | `A` | all projects / one project |
| Confirmations | `y` / `n` | yes / no (Enter is No) |

## 14. Commands at a glance

### Relay statistics (S123, proposed)

`ork stats [PROJECT] [--since DATE] [--json]` reads slicer's relay-run
statistics on every call, without the daemon. PROJECT is a registered project
name; omit it to use the current directory. The default is all-time. `--since`
passes your date to slicer, which validates it and chooses the range.

The text shows runs and outcomes, measured wall time, fix rounds, gates
(including those counted by proxy), owner wait and role times. Durations use
slicer's units; `-` means no measurement. `--json` returns slicer's aggregate
keys with their null values and omits `per_run`. Ork computes no statistics,
caches nothing and preserves slicer's error code and message when it fails.

### Command list

*Proposed (S103):* `ork --version` prints the mark only on a terminal;
on a pipe or file line 1 is plain `ork VERSION`.

    ork                          open the TUI
    ork --version
    ork status [--project P] [--summary]
    ork show RUN [NAME]
    ork wait [RUN...] [--until STATE,...] [--timeout D] [--project P] [--stage]
    ork projects list | add [PATH] [--name N] | rm NAME | set NAME [flags]
    ork projects show NAME
    ork projects pin NAME [--relay N] [--adj M] | unpin NAME [--adj]
    ork run P ID [--slivers]
    ork wave suggest P
    ork wave start P ITEM...
    ork wave define --project P NAME ITEM... [--replace]
    ork wave list [--project P]
    ork wave start [--project P] NAME
    ork wave rm [--project P] NAME
    ork answer RUN TEXT [--wait]
    ork check RUN N [ticked|later|open]
    ork check RUN all later
    ork fix RUN TEXT
    ork approve RUN [--later-checks]
    ork approve WAVE-ID
    ork retry RUN [TEXT] [--wait] [--rounds N] [--accept-shape | --again]
              [--allow PATTERN ...]
    ork stop RUN|WAVE
    ork dismiss RUN
    ork file RUN N
    ork tracking commit P [--base BRANCH] [--leave PATH]...
    ork vendor [list] | out NAME [--reason TEXT] [--until TIME] | back NAME
    ork roles print [ROLE]
    ork roles matrix PROJECT
    ork daemon status
    ork daemon run | stop [--force] | restart [--force] [--rebuild]
    ork doctor
    ork ai instructions
    ork ai skill [--output PATH | --install] [--force]

*Proposed (S233):* `ork ai instructions` prints a short guide for an agent that
drives ork from the command line, and `ork ai skill` prints a `SKILL.md` built
from the same text (front matter `name: ork`, then the guide). Both read the
text embedded in the binary, so it follows the CLI, and neither needs the
daemon. `--output PATH` writes the skill to one file, creating parent
directories. `--install` writes `.claude/skills/ork/SKILL.md`,
`.agents/skills/ork/SKILL.md` and `.grok/skills/ork/SKILL.md` under the current
directory. A file that holds other text is kept and the command exits 1 naming
it, with nothing written, unless you add `--force`; a file that already holds
the text is left alone. With `--json`, `ai instructions` prints `{"text": ...}`
and a write prints `{"written": [PATH, ...]}`, plus `unchanged` when a file
needed no write. Checking a skill into a project is the owner's call.

*Proposed (S405; adj 462 for the ranges):* `ork tracking commit P` commits the tracking files that
filing slices, spec answers and owner decisions leave uncommitted on the base
branch, in one commit, in place of `git add .slicer decisions` by hand. It takes
`.slicer/` without `config.json` and `relay-roles.json`, plus the adj store
folder named in `.adjudicate.json`; the subject names the slice IDs and adj
numbers (consecutive runs as ranges, such as `S387-S389; adj 402-405, 410-446`),
and there is no message flag. `--leave PATH` (repeatable) keeps a
tracking path out. It refuses, committing nothing, when any other path is
staged, when the main checkout is not on the base branch (`--base BRANCH`,
default `main`), or when there is nothing to commit (code `nothing_to_commit`,
exit 1). An unstaged edit to another file stays as it is. It prints
`committed 3b1c2d4 Tracking edits on main: S9; adj 7 (3 files)`.

Every command takes `--json`. Exit codes: 0 ok, 1 failure, 2 usage
(`internal/cli/cli.go:212`); `ork wait` also returns 3 question pending, 4
nothing active, 5 timeout (S257, *proposed*) and 6 a stage end (S403,
*proposed*).

`ork COMMAND --help` (or `-h`) and `ork help COMMAND` print that command's
usage and exit 0; `ork projects --help` prints every `ork projects` form
(S290, *proposed*).

## 15. What ork does not do yet

The owner still does these outside ork today. The list and a proposed slice
for each is in [orchestrator-parity.md](orchestrator-parity.md).

- Edit the role matrix (`slicer relay-roles ...`); ork shows it and can copy
  one project's matrix to others (`ork roles matrix copy`).
- Write or fill in slice specs, and add or promote roadmap items by hand.
- Decide in adj: ork can file an answer you give, but you make decisions
  with `adj` (`AGENTS.md`, Decisions).
- Pull and rebuild slicer or adj, and rebuild ork.
