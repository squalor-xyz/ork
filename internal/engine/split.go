// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// A split run (S61, *proposed*): a lead and parts in one worktree.
//
// The lead is the implementer's slot in the state machine (same chain, fix
// rounds and finish step) with the lead prompt. Its first round returns a
// parts list. ork checks the list, runs the parts in the slice worktree, then
// the one round check, one review, the architect (if the lineup has one) and
// one gate on the combined diff, as for any run. A fix round goes to the lead.

// maxParallelParts is how many parts run at the same time (matrix rule u7).
const maxParallelParts = 3

// partIDPattern is a part id: it names files, so it holds no path character.
var partIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

const partsHeading = "## Parts"

// parseParts reads the Parts section of the lead's first report: one line per
// part, `- ID: files PATH, PATH; tests NAME, NAME`. A section of `none` is no
// part. Every problem is named, so the lead can fix its list in one go.
func parseParts(text string) ([]runrec.Part, error) {
	body, ok := section(text, partsHeading)
	if !ok {
		return nil, fmt.Errorf("the lead's report has no %q section (write none under it when there are no parts)", partsHeading)
	}
	var parts []runrec.Part
	var problems []string
	seen := map[string]bool{}
	saidNone := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if !listMarker.MatchString(line) {
			// Only list lines are parts. A line that says none is the lead's
			// answer for no part; other text in the section is not read.
			saidNone = saidNone || isNone(line) && line != ""
			continue
		}
		line = strings.TrimSpace(listMarker.ReplaceAllString(line, ""))
		id, rest, ok := strings.Cut(line, ":")
		id, rest = strings.TrimSpace(id), strings.TrimSpace(rest)
		filesPart, testsPart, hasTests := strings.Cut(rest, "; tests")
		filesPart, isFiles := strings.CutPrefix(filesPart, "files ")
		if !ok || !isFiles || !partIDPattern.MatchString(id) {
			problems = append(problems, fmt.Sprintf("%q is not a part line (- ID: files PATH, PATH; tests NAME, NAME)", line))
			continue
		}
		if seen[id] {
			problems = append(problems, fmt.Sprintf("part %s is listed twice", id))
			continue
		}
		seen[id] = true
		part := runrec.Part{ID: id, Files: splitList(filesPart), Tests: splitList(testsPart)}
		if len(part.Files) == 0 {
			problems = append(problems, fmt.Sprintf("part %s lists no files", id))
		}
		if !hasTests || len(part.Tests) == 0 {
			problems = append(problems, fmt.Sprintf("part %s lists no tests", id))
		}
		for i, f := range part.Files {
			if !relativePath(f) {
				problems = append(problems, fmt.Sprintf("part %s: %q is not a path inside the worktree", id, f))
			}
			// Overlap and scope are decided on cleaned paths: internal/a/../b.go is internal/b.go.
			part.Files[i] = cleanPath(f)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 && len(problems) == 0 && !saidNone {
		problems = append(problems, fmt.Sprintf("the %q section lists no part and does not say none", partsHeading))
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return parts, nil
}

const handBackHeading = "## Hand back"

// parseHandBack reads the Hand back section of the lead's fix-round report: one
// entry per part, `- ID: the findings`, with indented lines after it as more of
// the same findings. A missing section, or one that says none, hands nothing
// back. An entry that names a part the run does not have, names a part twice or
// has no findings is an error that names every problem.
func parseHandBack(text string, parts []runrec.Part) (map[string]string, error) {
	body, ok := section(text, handBackHeading)
	if !ok {
		return nil, nil
	}
	known := map[string]bool{}
	for _, p := range parts {
		known[p.ID] = true
	}
	out := map[string]string{}
	var order, problems []string
	last := ""
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			// A blank line stays inside the findings; an indented line after it
			// goes on with them.
			if last != "" {
				out[last] += "\n"
			}
			continue
		}
		if !listMarker.MatchString(line) {
			if last != "" && strings.HasPrefix(raw, " ") {
				out[last] += "\n" + line
			}
			continue
		}
		line = strings.TrimSpace(listMarker.ReplaceAllString(line, ""))
		id, findings, ok := strings.Cut(line, ":")
		id = strings.TrimSpace(id)
		switch {
		case !ok || !partIDPattern.MatchString(id):
			problems = append(problems, fmt.Sprintf("%q is not a hand-back line (- ID: the findings)", line))
			last = ""
		case !known[id]:
			problems = append(problems, fmt.Sprintf("part %s is not a part of this run", id))
			last = ""
		case slices.Contains(order, id):
			problems = append(problems, fmt.Sprintf("part %s is handed findings twice", id))
			last = ""
		default:
			order = append(order, id)
			out[id] = strings.TrimSpace(findings)
			last = id
		}
	}
	for _, id := range order {
		out[id] = strings.TrimSpace(out[id])
		if out[id] == "" {
			problems = append(problems, fmt.Sprintf("part %s is handed no findings", id))
		}
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// pendingHandBack reports whether a part still owes the findings the lead
// handed back to it; the lead's fix round is then over and only the parts go on.
func pendingHandBack(rec runrec.Record) bool {
	for _, p := range rec.Parts {
		if !p.Done && p.HandBack != "" {
			return true
		}
	}
	return false
}

// handBackSection tells the lead of a fix round which parts own which files and
// how to hand a file's findings back to its part (S61, owner decision adj 385).
func handBackSection(parts []runrec.Part) BriefSection {
	var b strings.Builder
	b.WriteString("This is a split run. Apply the findings yourself by default, in any file. You may instead hand a file's findings back to the part that owns the file, for this round only: " +
		"add a `## Hand back` section to your report, before `## Owner needed`, with one line per part, `- ID: the findings, with file:line` (indent more lines under it). " +
		"ork then runs that part with those findings, after your round, in this worktree, and checks it as any part. Do not change a file you hand back. " +
		"Write `none` under `## Hand back` when you hand nothing back.\n\nParts and their files:\n")
	for _, p := range parts {
		fmt.Fprintf(&b, "- %s: %s\n", p.ID, strings.Join(p.Files, ", "))
	}
	return BriefSection{Title: "Hand back to a part", Body: b.String()}
}

// splitList reads a comma-separated list, dropping backticks and blanks.
func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(strings.Trim(strings.TrimSpace(item), "`"))
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// relativePath reports whether p is a path inside the worktree that tracking
// does not own.
func relativePath(p string) bool {
	clean := filepath.ToSlash(filepath.Clean(p))
	return !filepath.IsAbs(p) && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") &&
		clean != ".slicer" && !strings.HasPrefix(clean, ".slicer/") && clean != ".git" && !strings.HasPrefix(clean, ".git/")
}

// cleanPath is p as a slash path with its dot segments resolved and no trailing
// slash.
func cleanPath(p string) string { return filepath.ToSlash(filepath.Clean(p)) }

// filesOverlap reports whether two entries of parts' file lists name the same
// file, or one names a directory that holds the other.
func filesOverlap(a, b string) bool {
	a, b = cleanPath(a), cleanPath(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func partsOverlap(p, q runrec.Part) bool {
	for _, a := range p.Files {
		for _, b := range q.Files {
			if filesOverlap(a, b) {
				return true
			}
		}
	}
	return false
}

// partBatches groups parts, in order, into batches that run one after another;
// the parts of a batch run at the same time. A part joins the first batch that
// has room (at most maxParallelParts) and no part whose files overlap its own,
// so overlapping parts never share a batch (slicer ai.py, split: parallel only
// when the files are disjoint, otherwise one after another). It returns
// indexes into parts.
func partBatches(parts []runrec.Part) [][]int {
	var batches [][]int
next:
	for i, p := range parts {
		for b, batch := range batches {
			if len(batch) >= maxParallelParts {
				continue
			}
			fits := true
			for _, j := range batch {
				if partsOverlap(p, parts[j]) {
					fits = false
					break
				}
			}
			if fits {
				batches[b] = append(batch, i)
				continue next
			}
		}
		batches = append(batches, []int{i})
	}
	return batches
}

// planParts reads and checks the lead's parts list: its shape, and each part's
// files against the slice's Files section. A list that fails the check is an
// error that names every problem.
func (e *Engine) planParts(ctx context.Context, rec runrec.Record, leadReport string) ([]runrec.Part, error) {
	parts, err := parseParts(leadReport)
	if err != nil || len(parts) == 0 {
		return nil, err
	}
	raw, err := e.d.Slicer.ShowRaw(ctx, rec.Worktree, rec.Item)
	if err != nil {
		return nil, fmt.Errorf("slicer show: %w", err)
	}
	var detail slicercli.Detail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return nil, fmt.Errorf("slicer show: %w", err)
	}
	var problems []string
	for _, p := range parts {
		if outside := ScopeWarnings(p.Files, detail.Paths); len(outside) > 0 {
			problems = append(problems, fmt.Sprintf("part %s lists files outside the slice's Files section: %s", p.ID, strings.Join(outside, ", ")))
		}
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return parts, nil
}

// statusEntries reads `git status --porcelain=v1 -z` into path -> status code.
func statusEntries(out string) map[string]string {
	entries := map[string]string{}
	records := strings.Split(out, "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 || record[2] != ' ' || record[:2] == "!!" {
			continue
		}
		entries[record[3:]] = record[:2]
		if strings.ContainsAny(record[:2], "RC") && i+1 < len(records) {
			i++
			if records[i] != "" {
				entries[records[i]] = record[:2]
			}
		}
	}
	return entries
}

// pathStates fingerprints each path git reports as changed: its status code and
// the hash of its content. Two readings differ at a path that something changed
// between them, even a path that was already changed before.
func (e *Engine) pathStates(ctx context.Context, worktree string) (map[string]string, error) {
	out, err := e.gitRead(ctx, worktree, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	states := map[string]string{}
	for path, code := range statusEntries(out) {
		sum := ""
		if b, err := os.ReadFile(filepath.Join(worktree, path)); err == nil {
			sum = fmt.Sprintf("%x", sha256.Sum256(b))
		}
		states[path] = code + ":" + sum
	}
	return states, nil
}

// touchedSince lists the paths whose state differs between two readings.
func touchedSince(before, after map[string]string) []string {
	var out []string
	for path, state := range after {
		if before[path] != state {
			out = append(out, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			out = append(out, path)
		}
	}
	slices.Sort(out)
	return out
}

// leadStep is the ledger step of the lead's round in the current round: the
// parent of each part's step.
func leadStep(rec runrec.Record) string {
	for i := len(rec.Roles) - 1; i >= 0; i-- {
		r := rec.Roles[i]
		if r.Role == RoleImplementer && r.Part == "" && r.Round == rec.Round && r.Step != "" {
			return r.Step
		}
	}
	return ""
}

// partSections is the part's brief: what it owns, what it must not touch and,
// on a second try, why the first failed.
func partSections(part runrec.Part, all []runrec.Part, why string) []BriefSection {
	var others []string
	for _, p := range all {
		if p.ID != part.ID {
			others = append(others, p.Files...)
		}
	}
	list := func(items []string) string {
		if len(items) == 0 {
			return "- none\n"
		}
		return "- " + strings.Join(items, "\n- ") + "\n"
	}
	body := "Part: " + part.ID + "\n\n" +
		"Files you change (only these; each may also be a directory whose files you change):\n" + list(part.Files) + "\n" +
		"Tests you write and run (only these, not the full suite):\n" + list(part.Tests) + "\n" +
		"Files you must not touch: every file that is not on your list, the other parts' files included:\n" + list(others) + "\n" +
		"The lead owns the slice and the tracking. Run no slicer command that writes. Other parts change their files in this worktree at the same time, so a test outside your part may fail for a reason that is not yours; say so in your report. The lead's report is under Earlier reports."
	sections := []BriefSection{{Title: "Your part", Body: body}}
	if part.HandBack != "" {
		sections = append(sections, BriefSection{Title: "Findings handed back by the lead", Body: "The lead handed these findings of the review of the combined diff back to your part:\n\n" + part.HandBack + "\n\nFix only that, in your own files, and rerun your own tests."})
	}
	if why != "" {
		sections = append(sections, BriefSection{Title: "Your last round failed", Body: "ork's check of your part failed: " + why + ". Fix only that, in your own files, and rerun your own tests."})
	}
	return sections
}

// runParts runs the parts of the lead's list that are not done, batch by
// batch: the parts of a batch run at the same time in the slice worktree, the
// batches one after another. A part that fails its own check or changes a file
// outside its list fails the round and goes back to that part once; a part that
// fails again pauses the run. It returns true when every part is done, false
// when the run stopped or paused (the state function then returns).
func (e *Engine) runParts(ctx context.Context, pend *runrec.Pending) (bool, error) {
	tries := map[string]int{}
	why := map[string]string{}
	for {
		rec := e.run.Snapshot()
		var todo []runrec.Part
		for _, p := range rec.Parts {
			if !p.Done {
				todo = append(todo, p)
			}
		}
		if len(todo) == 0 {
			return true, nil
		}
		var batch []runrec.Part
		for _, i := range partBatches(todo)[0] {
			batch = append(batch, todo[i])
		}
		failed, ok, err := e.runBatch(ctx, rec, batch, why, pend)
		pend = nil
		if err != nil || !ok {
			return false, err
		}
		for _, p := range batch {
			tries[p.ID]++
			reason, bad := failed[p.ID]
			if !bad {
				continue
			}
			if tries[p.ID] >= 2 {
				return false, e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Role: RolePart, Part: p.ID, Round: rec.Round,
					Reason: fmt.Sprintf("part %s failed its check twice: %s", p.ID, reason)})
			}
			why[p.ID] = reason
		}
	}
}

// runBatch runs one batch of parts at the same time and checks each part. It
// returns the reason for each part that failed its check, and false when a part
// stopped or paused the run.
func (e *Engine) runBatch(ctx context.Context, rec runrec.Record, batch []runrec.Part, why map[string]string, pend *runrec.Pending) (failed map[string]string, ok bool, err error) {
	before, err := e.pathStates(ctx, rec.Worktree)
	if err != nil {
		return nil, false, e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Role: RolePart, Round: rec.Round, Reason: "worktree status: " + err.Error()})
	}
	parent := leadStep(rec)
	// The parts of a batch run at the same time and none waits for another. The
	// first pause is kept; a stop overrides a pause (as the review's rounds do).
	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	outs := make([]outcome, len(batch))
	errs := make([]error, len(batch))
	var wg sync.WaitGroup
	for i, p := range batch {
		var p2 *runrec.Pending
		if pend != nil && pend.Role == RolePart && pend.Part == p.ID {
			p2 = pend
		}
		r := partRound(rec, p, why[p.ID], nil, parent)
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i], errs[i] = e.runRound(batchCtx, r, p2)
			if e.run.Snapshot().State == runrec.StateStopped {
				cancel()
			}
		}()
	}
	wg.Wait()
	stopped := e.run.Snapshot().State == runrec.StateStopped
	for i := range batch {
		if errs[i] != nil && !(stopped && ctx.Err() == nil && errors.Is(errs[i], context.Canceled)) {
			return nil, false, errs[i]
		}
	}
	if stopped {
		return nil, false, ctx.Err()
	}
	paused := false
	for i := range batch {
		paused = paused || !outs[i].ok
	}

	// The parts' own check: no failed test in the report, and no file left
	// changed outside the batch's lists. A part whose round ended clean is judged
	// even when a sibling paused, so a retry does not run it again.
	failed = map[string]string{}
	after, err := e.pathStates(ctx, rec.Worktree)
	if err != nil {
		return nil, false, e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Role: RolePart, Round: rec.Round, Reason: "worktree status: " + err.Error()})
	}
	var listed []string
	for _, p := range batch {
		listed = append(listed, p.Files...)
	}
	outside := dirtyOutside(before, after, listed)
	var clean []runrec.Part // parts whose round ended and whose own tests passed
	var markErr error
	mark := func(id string, done bool) {
		if markErr == nil {
			markErr = e.markPart(id, done)
		}
	}
	for i, p := range batch {
		switch {
		case !outs[i].ok:
		case outs[i].report != nil && outs[i].report.Failed > 0:
			failed[p.ID] = fmt.Sprintf("its own tests failed: your report counts %d failed", outs[i].report.Failed)
			mark(p.ID, false)
		default:
			clean = append(clean, p)
		}
	}
	switch {
	case len(outside) == 0:
		for _, p := range clean {
			mark(p.ID, true)
		}
	case paused:
		// The tree is mid-edit and a path outside every list cannot be put to a
		// part now: the clean parts are judged again after the pause.
	case len(batch) == 1:
		failed[batch[0].ID] = strayReason(outside)
		mark(batch[0].ID, false)
	default:
		// Parts that share a tree cannot be told apart by one reading. Run the
		// clean parts one after another; the part whose own run changes a path
		// outside its list fails, the others stay done.
		for _, p := range clean {
			why, ok, err := e.soloPart(ctx, rec, p, parent, outside)
			if err != nil || !ok {
				return failed, false, err
			}
			if why != "" {
				failed[p.ID] = why
			}
		}
	}
	if markErr != nil {
		return failed, false, markErr
	}
	return failed, !paused, nil
}

func strayReason(outside []string) string {
	return "you changed files outside your list: " + strings.Join(outside, ", ") + "; change only your own files and put those changes back"
}

// soloPart runs one part of a batch alone, with the stray paths named, and
// judges it: failed tests, or a path outside its own list that its run left
// changed. It returns the reason when the part fails, and false when the run
// stopped or paused.
func (e *Engine) soloPart(ctx context.Context, rec runrec.Record, p runrec.Part, parent string, stray []string) (why string, ok bool, err error) {
	before, err := e.pathStates(ctx, rec.Worktree)
	if err != nil {
		return "", false, e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Role: RolePart, Part: p.ID, Round: rec.Round, Reason: "worktree status: " + err.Error()})
	}
	extra := []BriefSection{{Title: "Files outside every part's list", Body: "These files are changed in the worktree and are on no part's list: " + strings.Join(stray, ", ") +
		". If you changed one of them, put it back. Change only your own files."}}
	out, err := e.runRound(ctx, partRound(rec, p, "", extra, parent), nil)
	if err != nil || !out.ok {
		return "", false, err
	}
	after, err := e.pathStates(ctx, rec.Worktree)
	if err != nil {
		return "", false, e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Role: RolePart, Part: p.ID, Round: rec.Round, Reason: "worktree status: " + err.Error()})
	}
	switch outside := dirtyOutside(before, after, p.Files); {
	case out.report != nil && out.report.Failed > 0:
		why = fmt.Sprintf("its own tests failed: your report counts %d failed", out.report.Failed)
	case len(outside) > 0:
		why = strayReason(outside)
	}
	return why, true, e.markPart(p.ID, why == "")
}

// partRound is the round of one part: its brief sections, and its ledger step
// under the lead's.
func partRound(rec runrec.Record, p runrec.Part, why string, extra []BriefSection, parent string) round {
	return round{role: RolePart, part: p.ID, n: rec.Round, state: runrec.StateImplement, task: rec.Item + " part " + p.ID, parent: parent,
		sections: append(partSections(p, rec.Parts, why), extra...)}
}

// markPart counts a round of the part and sets whether it is done. A part that
// is done has spent any findings the lead handed back to it.
func (e *Engine) markPart(id string, done bool) error {
	return e.run.Update(func(rr *runrec.Record) {
		for j := range rr.Parts {
			if rr.Parts[j].ID == id {
				rr.Parts[j].Attempts++
				rr.Parts[j].Done = done
				if done {
					rr.Parts[j].HandBack = ""
				}
			}
		}
	})
}

// dirtyOutside lists the paths that changed between two readings, are still
// changed after, and lie outside every entry of listed. A path that a round put
// back is not one of them.
func dirtyOutside(before, after map[string]string, listed []string) []string {
	var present []string
	for _, path := range touchedSince(before, after) {
		if _, ok := after[path]; ok {
			present = append(present, path)
		}
	}
	return ScopeWarnings(present, listed)
}
