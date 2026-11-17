VERDICT: n/a (this is a review round, not gate mode)

# S407 reviewer report (round 1)

Scope reviewed: `git -C <worktree> diff` covering `internal/engine/brief.go`, `internal/engine/brief_test.go`, `internal/engine/slicechecks_test.go`, `docs/design.md` and `CHANGELOG.md`. The `.slicer/*` changes come from slicer commands; I did not read them.

Checks I ran:
- `go -C .../S407 test ./internal/engine -count=1 -run 'AcceptanceCriteria|WithoutCriteria|CriteriaWithout|SliceChecks|SpecCheck|GateBrief' -v`: every listed test passed, including the 4 new ones.
- `go -C .../S407 test ./... -count=1`: exit 0, and no package printed anything other than `ok`.
- `go -C .../S407 vet ./...`: clean.
- `gofmt -l <worktree>`: printed nothing.
- `slicer check --root .../S407`: exit 0.
- `adj lint --root .../S407`: exit 0.
- `adj check "checks.acceptance-criteria" --root .../S407`: decided (448, skip-criteria).
- Revert test: I copied the tree, without `.git` and `.slicer`, into my temp dir. In the copy I restored `brief.go:417` to `checklistEntries(sliceChecklistSections(raw)["Check"])` and ran the new tests:
  - `TestCheckSectionsSkipAcceptanceCriteria` failed (`brief_test.go:487: spec-check brief lists a backticked criterion as a Check command`).
  - `TestSliceChecksSkipAcceptanceCriteria` failed (`slicechecks_test.go:247`, paused on `exec: "friction": executable file not found in $PATH`).
  - `TestSliceChecksCriteriaWithoutCommandsLine` failed (`slicechecks_test.go:255`, same pause).
  - I deleted the copy afterwards.
- The worktree has no files beyond those already modified (`git status --short`).

## (A) Correctness

nothing

Traced and CONFIRMED:
- `withoutAcceptanceCriteria` (`internal/engine/brief.go:434-450`) starts skipping at a trimmed `Acceptance criteria:` line and stops at a trimmed `Commands:` line. It drops both marker lines. A body without the start marker comes back unchanged except for a lone `Commands:` line.
- Dropping a lone `Commands:` line cannot change any command: it has no backtick span, and `checklistEntries` (`brief.go:602-621`) already treated it as unindented prose that ends the current entry.
- The filter is applied inside `sliceCheckCommands` (`brief.go:417`), which feeds both callers:
  - `checkCommandSections` (`brief.go:379`), the brief's classification of Check commands.
  - `sliceChecks` (`brief.go:460`), which `runSliceChecks` uses (`states.go:1056`).
- So the slice's lead ("never runs them and never classifies them as Check commands") is met at both places.

## (B) Test gaps

nothing

- All 4 tests named under Failing tests exist:
  - `TestSliceChecksSkipAcceptanceCriteria` (`slicechecks_test.go:246`)
  - `TestSliceChecksWithoutCriteriaUnchanged` (`slicechecks_test.go:250`)
  - `TestSliceChecksCriteriaWithoutCommandsLine` (`slicechecks_test.go:254`)
  - `TestCheckSectionsSkipAcceptanceCriteria` (`brief_test.go:474`)
- The 3 red tests fail for the stated reason when the product change is reverted (CONFIRMED by the revert test above).
- The guard test `TestSliceChecksWithoutCriteriaUnchanged` passes both before and after the change. That is expected for a guard.
- The helper `requireCheckCommands` filters on `CheckRecord.Slice`, checks the exact command order and requires every record to be OK.
- For the mutation where the skip starts true, I rely on the implementer's mutation table (report item 5); I did not re-run it.

## (C) Direction

- Fits goal C18 ("Deterministic before model") and decided adj 448 (`checks.acceptance-criteria`, skip-criteria).
- Not in this slice: the slice lists no such items. The diff touches only the files under Files.
- The check fingerprint (`states.go:1061`) still hashes the whole Check text. That is correct: an edit to a criterion still invalidates cached check results, so it errs toward running the checks again.

- [later] Implementer Done-when still lists acceptance criteria as "Run or report blocked:" items
  `brief.go:293-311` builds the implementer's Done-when list from `checklistEntries(sections["Check"])` without the new filter. Each criterion bullet therefore becomes "Run or report blocked: A slice whose Acceptance criteria ...", which asks the implementer to run prose. The implementer said this was intentional (report item 2), and the Files section does not name this code, so it is out of scope here. A follow-up could label criteria as "Verify:" items instead.
  Importance: 1
  Urgency: 1
  Effort: 1

Every tracking step the implementer needed succeeded, so there is no deviation for the orchestrator. The implementer's one denial was the command form `go -C ... diff`; they used `git -C ... diff` instead, which needs no follow-up.

## (D) Doc accuracy

nothing

- `docs/design.md:1562-1565` matches the code: skipping runs up to a `Commands:` line or the end of the section, the entries are not listed in a brief, and a Check without the marker reads as before. It is marked *Proposed (S407, ...)* like the paragraphs around it.
- The `CHANGELOG.md` bullet sits under `## Unreleased`, matches the behavior and follows the existing `(*proposed*)` style.
- The Files anchor was re-pointed to `docs/design.md:1551-1562`, and the S317 paragraph is at `docs/design.md:1550`. That is close enough.

## Owner needed

none