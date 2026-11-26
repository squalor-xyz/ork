# Human checks to do

Checks that a person runs by hand. They never block a landing: a slice that passes its gates and checks lands, and its human checks are added here (owner decision 2026-11-11, adj 281). The owner usually tests after a release, and anything found becomes a fix slice.

Tick a box when the check is done, or delete the line. Write `-> SNNN` after a check that found a problem, naming the fix slice.

## S217 (run ork-20261108-031326-416c, 2026-11-08)

- [ ] At 100x30 and 80x24 in a live terminal: start a small slice, dirty a file it changes in the main checkout while it awaits approval, open the pane and read the warning; clean the file and see it go away on the next refresh; leave a `.slicer/` file modified and see no warning.
- [ ] [AFTER LAND] The next real landing with an unrelated dirty `.slicer/` file shows no warning and lands.

## S129 (run ork-20261108-050917-2c18, 2026-11-08)

- [ ] Open a project that holds a run recorded by an older build (no `tokens` on its roles) at 100x30 and 80x24 and confirm the run pane shows `tokens - $-` on finished roles and the Overview row shows no `tokens` cell.

## S243 (run ork-20261108-173940-bee0, 2026-11-08)

- [ ] `ork status` in a terminal at 100x30 and 80x24 with a run that has five long denied commands: the denied part is one bounded piece of the run's line (about 70 characters), not several lines of command text.
- [ ] [AFTER LAND] After the daemon is restarted on the landed build, watch the next Claude stand-in implementer run (Codex out of credits) once: its Done when checklist holds the Claude-only shell rules (owner a2, no new section), and status shows the short denied count.

## S103 (run ork-20261109-062107-5b00, 2026-11-09)

- [ ] [AFTER LAND] With a stale daemon, open the TUI at 100x30 and 80x24; restart the rebuilt daemon while no role runs and confirm the marker clears without reopening. Owner runs the new build from ~/bin/ork.

## S292 (run ork-20261109-123943-0249, 2026-11-09)

- [ ] [AFTER LAND] Start `ork wave start` on prism-ui and, within seconds, on prism (same repo). Both spec checks pass.

## S283 (run ork-20261109-144832-c7de, 2026-11-09)

- [ ] `ork wave start PROJECT READY-ITEM UNSPECCED-ITEM` against a real project: the person confirms the ready item starts and the other prints the `slicer promote` hint.

## S255 (run ork-20261109-162648-88ba, 2026-11-09)

- [ ] The owner has superseded adj 115 with the narrowed text before the relay starts (the owner looks at `adj show 115`).

## S257 (run ork-20261109-164713-3e64, 2026-11-09)

- [ ] Start a real small run with `ork wait RUN --until awaiting-approval,paused,stopped,landed` in a second terminal; the person confirms it prints once, at the right state, and the exit code (`echo $?`) matches the table.

## S267 (run ork-20261109-172316-057f, 2026-11-09)

- [ ] Open the Running pane with one run queued behind the cap, one running, and one that fell back: the person confirms the queued row is dim with no `!`, the orchestrator reads `ork engine` with no clock, and the fell-back row names the skipped entry.

## S293 (run ork-20261109-172735-a04e, 2026-11-09)

- [ ] On a project inside a monorepo subfolder (prism-ui), start a small run: the person confirms the check log shows the command succeeding from the project folder.

## S236 (run ork-20261109-200509-488e, 2026-11-09)

- [ ] [AFTER LAND] Start a run whose role runs a long `go test`, `ork stop` it, and confirm with `ps -axo pid,ppid,etime,command | rg 'go test|\.test'` that no `go test` or test binary of that worktree remains.

## S262 (run ork-20261109-203116-e85a, 2026-11-09)

- [ ] At 100x30 and 80x24: select Running; the bar shows `Overview (Running)` and `Questions` (with its count at both sizes); `tab` and `shift+tab` move between the two; from a project's Waves, `[` to Running opens its Overview.
- [ ] [AFTER LAND] After rebuilding `~/bin/ork` and restarting the daemon on it: launch a two-item wave with no spec questions; the screen moves to Running with the first new run selected and the launch text on the status line. Launch a wave where one item has spec questions and skip them; the screen stays on Waves with the held item ticked.

## S218 (run ork-20261109-203425-8ad5, 2026-11-09)

- [ ] At 100x30 and 80x24 in a live terminal: open Waves on a project with several engine slices and read the deferred items' reason lines; confirm opening the screen is not noticeably slower (it adds the parallel `show` reads).

## S264 (run ork-20261109-231648-f30b, 2026-11-10)

- [ ] In the TUI, tick two items on Waves and press `l`: the status line reads `spec check: 2 items, Ns; wait for the confirmation` and the clock moves each second until the confirmation opens. On a check whose report ends with `none` and prose, the Waves list shows no `Owner needed` line.

## S285 (run ork-20261109-231932-1c95, 2026-11-10)

- [ ] [AFTER LAND] On the slicer project with a round command set: the round checks' logs start with the round command, the check before the gate and the landing re-check run the full one, and the status line names which ran.

## S295 (run ork-20261110-005207-8a84, 2026-11-10)

- [ ] [AFTER LAND] `ork projects set prism-ui --check 'dotnet build Missing.slnx'` refuses with the file and the directory; the real prism-ui check is accepted.
- [ ] [AFTER LAND] A run paused at setup on a bad check is fixed with `ork stop ID`, `ork projects set PROJECT --check CMD` and `ork retry ID`; implementer round 1 runs with the new check, without a new `ork run`.

## S244 (run ork-20261110-015348-c816, 2026-11-10)

- [ ] [AFTER LAND] After the owner rebuilds and restarts the daemon and sets one scratch project's Codex row to `gpt-*-sol@latest`, `ork run ITEM --json` shows the resolved model and `requested`.

## S132 (run ork-20261110-022842-1d19, 2026-11-10)

- [ ] At 100x30 and 80x24, with a run paused on a structured question and another paused on an error: the Overview shows `Questions 1` and `Paused 1` in that order; open the question pane, shrink the terminal to a short height and see `+N more options` instead of silently missing options; wrapped options hang under their text.
- [ ] In the Decisions pane of an open record, press `t`, type, Enter, `n`: the typed answer is still in the field.

## S204 (run ork-20261110-023739-91bf, 2026-11-10)

- [ ] [AFTER LAND] After this lands, on a scratch project with slicer v1.17 installed, one relay whose slice has a `[AFTER LAND]` Check line reaches approval with that line shown as "deferred to land" and approves without a tick.

## S215 (run ork-20261110-033739-3cc4, 2026-11-10)

- [ ] [AFTER LAND] After the owner rebuilds and restarts the daemon (`ork daemon restart`, which refuses while a role runs), run `ork wave define --help`-style usage, define a wave of two open items, `ork wave list` (members show their status; a done item shows done), and `ork wave rm` it. Do not start a real wave for this check. Confirm the file exists beside `projects.json` with the agreed mode.

## S216 (run ork-20261110-123716-4600, 2026-11-10)

- [ ] At 100x30 and 80x24 in a live terminal: tick two items, save under a name, close and reopen Waves, open the saved list, load the wave, and confirm the ticks, the done member, and the status text read clearly and nothing wraps badly.
- [ ] [AFTER LAND] Launch a saved wave from the screen once with a real pair of small slices, then check the run record carries `wave_name` (`ork status --json`).

## S245 (run ork-20261110-142155-9975, 2026-11-11)

- [ ] At 100x30 and 80x24, the launch lineup of a scratch project's slice whose implementer row is `gpt-*-sol@latest` reads `latest (resolves to ...)` and fits.

## S226 (run ork-20261111-014137-e9b1, 2026-11-11)

- [ ] At 100x30 and 80x24, in a scratch project (`slicer init`, `slicer relay-roles init`, registered with `ork projects add`), open the matrix with `m`, change a model with `e`, confirm that the command shown is the one that ran (`slicer relay-roles show`), that `n` and Enter cancel, and that an unknown agent shows the warning. Do not edit the real project's matrix for this check.

## S223 (run ork-20261111-024142-12c2, 2026-11-11)

- [ ] [AFTER LAND] After landing and an `ork daemon restart` (the owner rebuilds `~/bin/ork` first), a relay whose reviewer raises an `[owner]` finding shows the question in `slicer relay-run list RUN` with the orchestrator step still running, and `slicer relay-run answer RUN QID --option LABEL` is visible on the finding in ork before approval.

## S291 (run ork-20261111-033730-1ee8, 2026-11-11)

- [ ] `ork run P ID --implementer Claude:sonnet:high` shows a plan whose implementer row reads `claude`.

## S290 (run ork-20261111-034737-4976, 2026-11-11)

- [ ] `ork retry --help`, `ork projects -h` and `ork help wave` in a terminal print the expected blocks and exit 0.

## S287 (run ork-20261111-115520-fdff, 2026-11-11)

- [ ] [AFTER LAND] On the next wave with Claude reviewers or gates, a report with a malformed `[owner]` finding and an `## Owner needed` question shows "shape retry" once and then asks the owner, with no `ork retry` by hand.

## S225 (run ork-20261111-120354-e6fc, 2026-11-11)

- [ ] [AFTER LAND] The owner runs the workflow once with `workflow_dispatch` and confirms the new `Vet another unix target` step passes on the Linux test job and does not appear on the macOS one.

## S224 (run ork-20261111-121508-fc5d, 2026-11-11)

- [ ] [AFTER LAND] After a rebuild and `ork daemon restart`, `ork projects set NAME --bell off` prints `updated NAME: bell=off`, `~/.config/ork/projects.json` has `"bell": false` for that project, and a restarted TUI rings no bell for it.

## S298 (run ork-20261111-124323-1746, 2026-11-11)

- [ ] [AFTER LAND] A reviewer report with a misnumbered option under an owner finding pauses with a message that names that line, a detail line after `Recommend:` no longer pauses (S288), and a second shape pause on the same role shows the report file name and the accept command.

## S333 (run ork-20261111-132100-a59e, 2026-11-11)

- [ ] Open the TUI at 100x30 and 80x24 with two runs live: it starts on Running; `E` adds only role lines; Recent shows no `pass` rows; an open run shows a Codex out-of-credits fallback as a dim note.

## S289 (run ork-20261111-150602-fa84, 2026-11-11)

- [ ] [AFTER LAND] The next tracking-only conflict in a landing shows the new reason, and after the owner follows it, `slicer show ID` still holds the notes and section edits of both sides.

## S220 (run ork-20261111-160241-e417, 2026-11-11)

- [ ] [AFTER LAND] The owner reads the CHANGELOG v1.1.0 section and the tag summary, runs the workflow dry run if needed, creates and pushes the annotated tag, and checks the release page. ork does none of these.

## S123 (run ork-20261111-215047-32d5, 2026-11-11)

- [ ] The owner reads the display against `slicer relay-run stats` and confirms the numbers match; for a TUI panel, at 100x30 and 80x24 in a live terminal.

## S315 (run ork-20261111-215046-00fa, 2026-11-11)

- [ ] [AFTER LAND] HUMAN CHECK : with main ahead of the daemon, run `ork daemon restart --rebuild` and see the new build id in `ork status`.

## S356 (run ork-20261113-011329-81c8, 2026-11-14)

- [ ] [AFTER LAND] `` S304 trial 3 runs a claude-local implementer on a size S slice with the approval point live, and records the denials and the time in S304's notes.

## S363 (run ork-20261115-014017-53a1, 2026-11-15)

- [ ] [AFTER LAND] `` S304 trial 4 runs a claude-local implementer on S248 with this brief and records sessions, restarts, edits and time in S304's notes.

## S364 (run ork-20261115-015903-0b71, 2026-11-15)

- [ ] [AFTER LAND] `` The orchestrator runs the micro-task for settings 1-4 and records the table in this slice's notes and S304's.

## S365 (run ork-20261115-030723-2a71, 2026-11-15)

- [ ] [AFTER LAND] `` S304 trial 4 or 5 runs S248 with the pack and records it in S304's notes.

## S311 (run ork-20261115-033114-333e, 2026-11-15)

- [ ] [AFTER LAND] HUMAN CHECK : set a command that appends to a file, start a small slice, and see one line for each time it needs you and one on landing.

## S366 (run ork-20261115-150511-7d21, 2026-11-15)

- [ ] [AFTER LAND] `` S304 trial 5 runs S248 with the pack and records it in S304's notes.

## S241 (run ork-20261116-015040-d93b, 2026-11-16)

- [ ] After landing and a rebuild, the owner runs `ork wave start ork ITEM ITEM` for two slices where only one touches the engine paths, reads the plan before answering `n`, and confirms that the labels, the pair rule line and the spec check lines are clear at 80 columns.

## S242 (run ork-20261116-015041-3103, 2026-11-16)

- [ ] After landing and a rebuild of `~/bin/ork` and a restart, the owner runs `ork daemon status`, `ork doctor` and `ork status` once with the daemon running, and once after a commit that only touches `.slicer/` (no warning expected), and after a Go change (warning expected).

## S371 (run ork-20261116-155616-1207, 2026-11-16)

- [ ] [AFTER LAND] `` S304 trial 6 runs a local implementer on a slice with a large file and records sessions, reads and edits in S304's notes.

## S247 (run ork-20261116-175515-ebde, 2026-11-16)

- [ ] `ork projects show ork` and `ork projects show ork --json` against the owner's running daemon after rebuild: the matrix lines match `slicer relay-roles show`.

## S206 (run ork-20261116-172629-3a8b, 2026-11-16)

- [ ] [AFTER LAND] On a scratch project with `adj: true`: `ork projects pin NAME --adj 1`, then `ork doctor` shows the adj pin; edit nothing by hand; `ork projects unpin NAME --adj` clears it.

## S222 (run ork-20261116-190149-a0bf, 2026-11-16)

- [ ] [AFTER LAND] With a real pair wave of two small slices, both at `awaiting-approval`: tick every human check, press the key, read the confirm at 100x30 and 80x24, confirm, and watch the first land and then the second. Then repeat with a pair whose second landing hits a conflict and confirm the first lands, the second goes to a fix round, and nothing else is approved.

## S179 (run ork-20261116-203847-b14e, 2026-11-16)

- [ ] review the diff in testdata/golden/; the owner should see only the new spec-draft views

## S335 (run ork-20261117-014448-b5d2, 2026-11-17)

- [ ] [AFTER LAND] With the TUI open on a project with a live run, the run's state changes still show within about a second, and `ps` sampling shows far fewer slicer processes than before.

## S66 (run ork-20261117-022055-182f, 2026-11-17)

- [ ] At 100x30 and 80x24 in a real terminal: select text with the mouse at launch, press `M`, click a tab, a project and a row, press `M` again and select text again.

## S397 (run ork-20261117-215731-f05c, 2026-11-17)

- [ ] In an 80x24 terminal, run `ork run ork ID` on a slice whose spec check asks a question with an option longer than one line: the option's second line starts under the label, and the `Answer 1-N` prompt fits within 80 columns with the cursor after `back): `.

## S390 (run ork-20261117-233555-6d1c, 2026-11-18)

- [ ] At 80x24, start `LC_ALL=C ork` with a run that ended within the hour; press `tab` until the Overview shows; the Recent row reads `+ pass` (or `x fail`) in green (red), and the rules are `|` and `-`.
- [ ] At 80x24 in a UTF-8 terminal, open Waves (`tab` to Waves), tick two items with `space` that share a long path; the `! A and B both touch` line wraps with its path four columns in.

## S388 (run ork-20261117-233556-f24d, 2026-11-18)

- [ ] In a real run, a feature line reaches Needs you, and answering `file` files one slice in the target.

## S431 (run ork-20261118-130342-787e, 2026-11-18)

- [ ] With `ork projects set ork --auto-rebuild on`, land a slice that changes internal/engine while another run is implementing; the daemon restarts on the new build after that role ends.

## S393 (run ork-20261118-151519-8395, 2026-11-18)

- [ ] At 100x30, open a run with reports (`enter` on the run, then `p`): the heading names the file once; a report with a long `- ` list item wraps under the text. At 80x24, `4` (Roadmap), `enter` on an item with notes: each note starts with its plain date.

## S432 (run ork-20261118-172104-db18, 2026-11-18)

- [ ] [AFTER LAND] after slicer s460 lands and the daemon is rebuilt, ork doctor shows the contract line as ok for relay v13.

## S437 (run ork-20261118-225522-3de5, 2026-11-19)

- [ ] in the next ork run on this project, the implementer's report shows the daemon tests ran with no "no temp directory short enough for a socket" skip.

## S353 (run ork-20261120-001408-907b, 2026-11-20)

- [ ] [AFTER LAND] the owner sets the probe on the slicer project and runs ork doctor; it shows the contract line as ok.

## S449 (run ork-20261120-133801-8a30, 2026-11-20)

- [ ] The owner looks at the Waves launch with no check command and a queued-resume run in the TUI at 80x24.

## S389 (run ork-20261120-145742-cab5, 2026-11-20)

- [ ] UI pass (r14): `ork show RUN` with three entries reads clearly at 80 columns.

## S394 (run ork-20261120-152522-c419, 2026-11-22)

- [ ] At 80x24 and 100x30: `4` (Roadmap), press `N` and type past the field's width: the field starts with `...`. With a scratch daemon on an older build and `ork vendor out codex`, `ork vendor out claude` (scratch daemon only), the status line reads `restart daemon | claude out until back | codex out until ...` at 100x30, or at 80x24 with the sidebar hidden (`s`); at 80x24 with the sidebar showing the status line has 60 columns and drops the codex field.

## S400 (run ork-20261122-031627-c98e, 2026-11-22)

- [ ] In a 100x30 terminal, open the Overview with a stopped engine run selected: the detail under the rows shows two lines, the second `N of M steps  AGE`. Resize to 80x24: still two lines, nothing cut.
- [ ] In a 100x30 terminal, park an item on the Roadmap with `P`: the heading changes to `... open, 1 parked` without `other` when no other status is present.
- [ ] In a colour terminal at 100x30, open the run pane of a run in its second implementer round: the times, vendor, `#1` and token count line up readably; then start ork with `LC_ALL=C` (ASCII, `internal/brand/mark.go:60-68`) and the gate verdict uses `x`.

## S240 (run ork-20261120-152559-6a31, 2026-11-22)

- [ ] After landing and a rebuild, with a paused run that has a multi-line question and a run in `checks`, the owner runs `ork status --project ork` in an 80-column terminal and reads: the ids, the times, the question, the counts.

## S399 (run ork-20261122-043418-2637, 2026-11-22)

- [ ] In a 200x40 terminal, open the Roadmap: the hint line starts `O slicer tui;`. At 100x30 and 80x24 the hint line does not show it (unchanged drop tier).
- [ ] In a 100x30 terminal, open the Roadmap, select the third item, press `O`; inside slicer's TUI resize the terminal to 80x24, then quit it normally: ork redraws at 80x24 with the cursor on the same item and no stale lines.
- [ ] In a 100x30 terminal, press `O` on the Roadmap, then quit slicer's TUI with ctrl+c: record whether the status line shows `slicer tui: exit status N` (and which N), and that ork stays responsive.

## S401 (run ork-20261122-081958-ba4a, 2026-11-25)

- [ ] In an 80x24 terminal, on the Roadmap press `?`, then `down` 3 times, `pgdown` until the end, `pgup` to the top, and `?` again: the text moves smoothly, the markers count right, nothing is cut mid-word, and the second `?` closes help. Repeat on the Overview and Waves views.

## S251 (run ork-20261125-015357-ca10, 2026-11-25)

- [ ] At 100x30 and 80x24, with a scratch project registered (no `.slicer/`, no `.adjudicate.json`), open the pane, run `slicer-init`, `relay-roles-init` and `adj-init` from it, and see each block fill in. Not against this repository.

## S233 (run ork-20261125-015129-4c21, 2026-11-25)

- [ ] After landing and a rebuild, the owner reads "ork ai instructions" once and says whether it matches how an agent should drive ork, and decides whether to install the skill in a project.

## S554 (run ork-20261125-175814-f2ae, 2026-11-25)

- [ ] In the next real reviewer round, open the reviewer's brief under the run's role files. You should see a `## Base overlays` section with one `go -C WORKTREE test -overlay ... ./DIR` command for each changed non-test Go file. Run one of them from your shell. It should fail (red) for the test the slice added, and `git -C WORKTREE status --porcelain` should be unchanged.

## S565 (run ork-20261125-182244-1153, 2026-11-25)

- [ ] Read the rendered UI brief. It should show `## Test run patterns` after `## CLI text renders`, with the one-test-per-command example. The implementer, reviewer and gate briefs should not show it.

## S564 (run ork-20261125-184829-ca71, 2026-11-25)

- [ ] In the next real reviewer round for a slice that adds a function tests call only directly, open the reviewer's brief under the run's role files. You should see a `## Untested call sites` section naming the function and its unreached call site. The run should not have paused for it.

## S531 (run ork-20261125-185405-f675, 2026-11-25)

- [ ] Read the TestScrollSweep table in internal/tui/scroll_sweep_test.go. It should name the help overlay on each tab view, the Roadmap row list, the launch confirm and the Roadmap item pane, and no scrollable view you know of in the launch harness is left out.

## S579 (run ork-20261125-203305-0dc5, 2026-11-25)

- [ ] Open a long item on the Roadmap at 80x24 and press pgdown until the end. Each page should start with the line that sat under the "+N more lines" marker on the page before. Press pgup back up the same way. Open the spec draft pane on a long draft and page it; it should behave the same.

## S586 (run ork-20261125-203302-a88e, 2026-11-25)

- [ ] Probe the real Claude Code matcher (not run by this draft). In a scratch git repo, start Claude Code with the allow rule Bash(git -C * show*) and ask it to run git -C /scratch commit --allow-empty -m show. If it runs without a prompt, the CLI's * spans the subcommand like ork's hook does. Then repeat with Bash(git -C /scratch show*): the commit command should prompt or be denied, and git -C /scratch show HEAD should run. Record the result on S586.

## S571 (run ork-20261125-203303-45c3, 2026-11-25)

- [ ] Read the rendered UI brief and the reviewer brief's ## Base overlays section. Each should say one test per command with go -C DIR test -run NAME PKG, with no | character in the -run text. Read internal/roles/prompts/gate.md and internal/roles/prompts/spec-draft.md; each should carry the same rule.

## S593 (run ork-20261125-211625-10ac, 2026-11-25)

- [ ] In the next reviewer round for a slice that changes a signature, open the reviewer's brief under the run's role files. The ## Base overlays section should show does not build: ... with the compiler error for the file whose revert cannot compile. That entry should have no go -C ... test -overlay command. The other files should keep their commands.

## S580 (run ork-20261125-204703-eec0, 2026-11-25)

- [ ] Run ork in a terminal of 80x10, open a project's Roadmap with several goals and press pgup on the first row. The Goals lines should scroll into view and +N lines above should count down to the Goals heading. Then press down: the cursor should reach the second row and its row should be on screen.

## S588 (run ork-20261125-222934-1f3d, 2026-11-25)

- [ ] Open the doc comment of TestScrollSweep in internal/tui/scroll_sweep_test.go. Each of decisions detail, questions detail, waves item pane and settings should be either a table row or a named Left out entry with a reason and a follow-up slice ID. No pane should be missing from both.

## S600 (run ork-20261125-222936-fc21, 2026-11-25)

- [ ] Run ork in a terminal of 80x10 and open a Roadmap with several goals. Press home twice: the Goals show, and the dim line under them reads +N more lines, cursor: S90 (the first row's ID). Press enter: that item opens. Press esc, then down: the cue is gone.

## S596 (run ork-20261125-222937-c7ce, 2026-11-25)

- [ ] In the next reviewer round for a slice where two changed Go files depend on each other's new fields, open the reviewer's brief under the run's role files. The ## Base overlays section should show does not build: ... for each single file. It should end with an All changed Go files at once entry holding one go -C ... test -overlay .../overlay-all.json ./PKG ... command. Run it from your shell. It should build, and git -C WORKTREE status --porcelain should be unchanged.

## S576 (run ork-20261125-222939-8d33, 2026-11-25)

- [ ] [AFTER LAND] The next time ork spec draft ork ID --json fails with report shape: ..., the error ends with [report kept in .../draft/spec-draft-failed-2.md], and that file holds what the role wrote, so you can see why no block was found.

## S589 (run ork-20261125-224829-7bd0, 2026-11-26)

- [ ] In the next real reviewer round of a Go project, open the reviewer's brief under the run's role files. You should see a ## Go module imports section listing every require path of the worktree's go.mod, for ork charm.land/bubbletea/v2, charm.land/bubbles/v2 and charm.land/lipgloss/v2. In a round of a project with no go.mod, the brief should have no such section.

## S598 (run ork-20261125-232714-f9a7, 2026-11-26)

- [ ] Start a reviewer round on a self-host daemon that ork daemon status reports as behind main, on a slice that changes a Go file. Open the reviewer brief under the run's role files. The ## Base overlays section should open with the "older than this slice's code" line, and the file entries should follow. After ork daemon restart --rebuild, the next round's section should not show that line.

## S592 (run ork-20261126-004139-f69b, 2026-11-26)

- [ ] In the next reviewer round for a slice that changes a Go file and a non-Go file, open the reviewer's brief under the run's role files. The ## Base overlays section should end with a paragraph saying other changed files are read with git -C WORKTREE show MERGEBASE:PATH and that git grep is not available. It should name no changed file, and the entry lines for the Go files should look as before.

## S584 (run ork-20261126-002418-89aa, 2026-11-26)

- [ ] On the next slice whose diff is only _test.go files under internal/tui, open the ui role's brief in the run directory. It should hold ## No renders expected and no "Produce the required renders" Done-when item. The ui report should say that no renders were expected.
