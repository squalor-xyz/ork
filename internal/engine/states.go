// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// excludeLine is the line setup adds to info/exclude so the worktrees never
// show as untracked files in the main checkout.
const excludeLine = ".worktrees/"

// setup creates the worktree and the ledger run. A failure here stops the
// slice: nothing was started that a retry could take over.
func (e *Engine) setup(ctx context.Context) error {
	rec := e.run.Snapshot()
	if err := e.ensureExclude(ctx); err != nil {
		return e.stop(ctx, "setup: "+err.Error())
	}
	if err := e.ensureWorktree(ctx, rec); err != nil {
		return e.stop(ctx, "setup: git worktree add: "+err.Error())
	}
	// Recording is advisory: a failed relay-run call never stops the relay.
	if run, err := e.d.Slicer.RelayBegin(ctx, e.wr, rec.Worktree, rec.Worktree, rec.Item); err == nil {
		e.run.Update(func(r *runrec.Record) { r.RelayRun = run.ID })
		step, err := e.d.Slicer.RelayStep(ctx, e.wr, rec.Worktree, run.ID, slicercli.StepSpec{
			Role: "orchestrator", Agent: "ork", Phase: "start", Task: rec.Item + " orchestrator",
		})
		if err == nil {
			e.run.Update(func(r *runrec.Record) { r.OrchestratorStep = step.ID })
		}
	}
	return e.run.Update(func(r *runrec.Record) { r.State = runrec.StateImplement })
}

// ensureExclude adds .worktrees/ to the main checkout's info/exclude when it
// is missing. ork edits the file itself, so the edit is recorded in writes[]
// as a git write named "exclude".
func (e *Engine) ensureExclude(ctx context.Context) error {
	out, err := e.gitRead(ctx, e.proj.Path, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return fmt.Errorf("git rev-parse --git-path info/exclude: %w", err)
	}
	path := strings.TrimSpace(out)
	if path == "" {
		return errors.New("git gave no path for info/exclude")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.proj.Path, path)
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch strings.TrimSpace(line) {
		case ".worktrees/", "/.worktrees/", ".worktrees", "/.worktrees":
			return nil
		}
	}
	start := e.d.now()
	add := excludeLine + "\n"
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		add = "\n" + add
	}
	werr := os.MkdirAll(filepath.Dir(path), 0o755)
	if werr == nil {
		werr = appendFile(path, add)
	}
	w := runrec.Write{At: start, Tool: "git", Root: e.proj.Path, Argv: []string{"exclude", excludeLine}, MS: e.d.now().Sub(start).Milliseconds()}
	if werr != nil {
		w.Exit, w.Error = 1, werr.Error()
	}
	e.run.AddWrite(w)
	return werr
}

func appendFile(path, s string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(s); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// feedbackSection reads the file a fix round must act on.
func (e *Engine) feedbackSection(fb *runrec.Feedback) BriefSection {
	body := "(the file is gone)"
	if b, err := os.ReadFile(filepath.Join(e.run.Dir(), fb.File)); err == nil {
		body = string(b)
	}
	switch fb.Kind {
	case "checks":
		return BriefSection{Title: "Checks failed", Body: "The project checks are red. Fix the failures below. No review was spent on this diff.\n\n```\n" + tail(body, 30000) + "\n```"}
	case "findings":
		if strings.HasPrefix(fb.File, "findings-") {
			return BriefSection{Title: "Review findings", Body: "The reports below hold findings: the reviewer's, the UI pass's, or both. Apply only the findings in them. The UI pass gives suggested design edits for the screens this slice changes. If a finding is wrong, say so in your report with your evidence.\n\n" + body}
		}
		return BriefSection{Title: "Review findings", Body: "The reviewer's report is below. Apply only the findings in it. If a finding is wrong, say so in your report with your evidence.\n\n" + body}
	case "architect":
		return BriefSection{Title: "Architect findings", Body: "The architect's whole report is below. Apply its change-now findings. If a finding is wrong, say so in your report with your evidence. Keep later work out of this slice.\n\n" + body}
	default:
		return BriefSection{Title: "Gate FAIL", Body: "The gate failed this diff. Its report is below. Fix what it lists.\n\n" + body}
	}
}

// bumpRound starts the next implementer round with the feedback, or stops the
// slice at the round limit.
func (e *Engine) bumpRound(ctx context.Context, fb runrec.Feedback) error {
	rec := e.run.Snapshot()
	if rec.Round+1 > e.d.maxRounds() {
		return e.stop(ctx, fmt.Sprintf("the round limit of %d implementer rounds was reached", e.d.maxRounds()))
	}
	return e.run.Update(func(r *runrec.Record) {
		r.Round++
		r.Feedback = &fb
		r.State = runrec.StateImplement
	})
}

func (e *Engine) implement(ctx context.Context) error {
	pend := e.takePending()
	rec := e.run.Snapshot()
	r := round{role: RoleImplementer, n: rec.Round, state: runrec.StateImplement, task: rec.Item + " implementer", resumeFrom: []string{RoleImplementer}}
	if rec.Feedback == nil {
		r.sections = []BriefSection{{Title: "Round", Body: "This is the first round. The slice is not started: run `slicer start " + rec.Item + " --render --strict` first."}}
	} else {
		r.task = fmt.Sprintf("%s implementer fix round %d", rec.Item, rec.Round-1)
		r.sections = []BriefSection{
			{Title: "Round", Body: fmt.Sprintf("This is fix round %d. The slice is already started; do not start it again.", rec.Round-1)},
			e.feedbackSection(rec.Feedback),
		}
	}
	out, err := e.runRound(ctx, r, pend)
	if err != nil || !out.ok {
		return err
	}
	return e.run.Update(func(rr *runrec.Record) { rr.Feedback = nil; rr.State = runrec.StateChecks })
}

func (e *Engine) checks(ctx context.Context) error {
	rec := e.run.Snapshot()
	start := e.d.now()
	check := e.d.Check
	if check == nil {
		check = RunCheck
	}
	timeout := time.Duration(e.proj.CheckMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ok := true
	var log strings.Builder
	if cmd := e.proj.CheckCommand; cmd != "" {
		res, err := check(ctx, rec.Worktree, cmd, timeout)
		if err != nil {
			return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateChecks, Reason: "check command: " + err.Error()})
		}
		ok = res.OK
		fmt.Fprintf(&log, "$ %s\n%s\n", cmd, tail(res.Output, 30000))
	}
	sres, err := e.d.Slicer.Check(ctx, rec.Worktree)
	if err != nil {
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateChecks, Reason: "slicer check: " + err.Error()})
	}
	if !sres.OK {
		ok = false
	}
	fmt.Fprintf(&log, "\n$ slicer check --json\n%s\n", tail(sres.Output, 30000))

	logName := fmt.Sprintf("checks-%d.log", rec.Round)
	logPath := filepath.Join(e.run.Dir(), logName)
	err = os.WriteFile(logPath, []byte(log.String()), 0o600)
	if err == nil {
		err = os.Chmod(logPath, 0o600)
	}
	if err != nil {
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateChecks, Reason: "check log: " + err.Error()})
	}
	command := e.proj.CheckCommand
	if command == "" {
		command = "slicer check --json"
	}
	err = e.run.Update(func(r *runrec.Record) {
		r.Pending = nil
		r.Checks = append(r.Checks, runrec.CheckRecord{Round: rec.Round, Command: command, OK: ok, Seconds: int(e.d.now().Sub(start).Seconds()), Log: logName})
	})
	if err != nil {
		return err
	}
	if !ok {
		return e.bumpRound(ctx, runrec.Feedback{Kind: "checks", File: logName})
	}
	next := runrec.StateReview
	for _, r := range rec.Roles {
		if r.Role == RoleReviewer {
			next = e.beforeGate(rec) // accepted reviewer findings are fixed before the architect
		}
	}
	return e.run.Update(func(r *runrec.Record) { r.State = next })
}

// latestReport returns the newest report of a role, with its name.
func (e *Engine) latestReport(rec runrec.Record, role string) (name, text string) {
	for i := len(rec.Roles) - 1; i >= 0; i-- {
		if r := rec.Roles[i]; r.Role == role && r.Report != "" {
			if b, err := os.ReadFile(filepath.Join(e.run.Dir(), r.Report)); err == nil {
				return strings.TrimSuffix(r.Report, ".md"), string(b)
			}
		}
	}
	return "", ""
}

func (e *Engine) review(ctx context.Context) error {
	pend := e.takePending()
	rec := e.run.Snapshot()
	rounds := []round{{role: RoleReviewer, n: rec.Round, state: runrec.StateReview, task: rec.Item + " reviewer",
		sections: []BriefSection{{Title: "Review", Body: "Review the slice's diff in the worktree. Use `git status` and `git diff`; you may not change the tree. The implementer's report is under Earlier reports."}}}}
	if _, ok := lineup(rec.Lineup, RoleUI); ok {
		ui, err := e.touchesUI(ctx, rec)
		if err != nil {
			return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateReview, Reason: "slicer show: " + err.Error()})
		}
		if ui {
			rounds = append(rounds, round{role: RoleUI, n: rec.Round, state: runrec.StateReview, task: rec.Item + " ui",
				sections: []BriefSection{{Title: "UI pass", Body: "The slice touches the UI. Check the screens the diff adds or changes, beside the reviewer, who checks the rest of the diff (adj 165). Use `git status` and `git diff`; you may not change the tree. The implementer's report is under Earlier reports."}}})
		}
	}

	// A role whose report already stands in this round is not run again after
	// a pause of the other role.
	var todo []round
	for _, r := range rounds {
		if !e.reviewDone(rec, pend, r.role) {
			todo = append(todo, r)
		}
	}
	// The reviewer and the UI pass run at the same time and neither waits for
	// the other. Each round ends its own way. The first pause is kept; a stop
	// overrides a pause (pause refuses when paused or stopped, stop refuses
	// only when already stopped).
	outs := make([]outcome, len(todo))
	errs := make([]error, len(todo))
	var wg sync.WaitGroup
	for i, r := range todo {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i], errs[i] = e.runRound(ctx, r, pendFor(pend, r.role))
		}()
	}
	wg.Wait()
	for i := range todo {
		if errs[i] != nil {
			return errs[i]
		}
	}
	for i := range todo {
		if !outs[i].ok {
			return nil
		}
	}

	rec = e.run.Snapshot()
	revFile, revText := e.roundReport(rec, RoleReviewer)
	revFindings, _ := Findings(revText)
	uiFile, uiText := e.roundReport(rec, RoleUI)
	uiFindings, _ := Findings(uiText)
	switch {
	case revFindings == "" && uiFindings == "":
		return e.run.Update(func(rr *runrec.Record) { rr.State = e.beforeGate(rec) })
	case uiFindings == "":
		return e.bumpRound(ctx, runrec.Feedback{Kind: "findings", File: revFile})
	}
	// The findings of both reports reach the implementer in one fix round.
	var b strings.Builder
	if revFindings != "" {
		fmt.Fprintf(&b, "### Reviewer report (%s)\n\n%s\n\n", strings.TrimSuffix(revFile, ".md"), strings.TrimSpace(revText))
	}
	fmt.Fprintf(&b, "### UI pass report (%s)\n\n%s\n", strings.TrimSuffix(uiFile, ".md"), strings.TrimSpace(uiText))
	name := fmt.Sprintf("findings-%d.md", rec.Round)
	if err := os.WriteFile(filepath.Join(e.run.Dir(), name), []byte(b.String()), 0o600); err != nil {
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateReview, Reason: "findings file: " + err.Error()})
	}
	return e.bumpRound(ctx, runrec.Feedback{Kind: "findings", File: name})
}

// pendFor is the pause context a round of the review state takes: its own, or
// a budget grant, which is for the whole review.
func pendFor(p *runrec.Pending, role string) *runrec.Pending {
	if p != nil && (p.Role == "" || p.Role == role || p.Kind == runrec.PendingBudget) {
		return p
	}
	return nil
}

// roundReport returns the file name and text of the role's newest report of
// the current round, empty when it has none.
func (e *Engine) roundReport(rec runrec.Record, role string) (name, text string) {
	for i := len(rec.Roles) - 1; i >= 0; i-- {
		r := rec.Roles[i]
		if r.Role != role || r.Round != rec.Round {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(e.run.Dir(), r.Report)); err == nil && r.Report != "" {
			return r.Report, string(b)
		}
		return "", ""
	}
	return "", ""
}

// reviewDone reports whether the role already gave a good report in this
// round: it exited well, and its report has the shape and asks nothing. A
// role that the pause names runs again.
func (e *Engine) reviewDone(rec runrec.Record, pend *runrec.Pending, role string) bool {
	if pend != nil && pend.Kind != runrec.PendingBudget && (pend.Role == "" || pend.Role == role) {
		return false
	}
	for i := len(rec.Roles) - 1; i >= 0; i-- {
		r := rec.Roles[i]
		if r.Role != role || r.Round != rec.Round {
			continue
		}
		if r.Exit != string(ExitOK) || r.Report == "" {
			return false
		}
		b, err := os.ReadFile(filepath.Join(e.run.Dir(), r.Report))
		if err != nil {
			return false
		}
		rep, err := CheckReport(role, false, string(b))
		if err != nil || rep.Question != "" {
			return false
		}
		_, err = Findings(string(b))
		return err == nil
	}
	return false
}

// touchesUI reports whether the slice's Files section names a UI path.
func (e *Engine) touchesUI(ctx context.Context, rec runrec.Record) (bool, error) {
	raw, err := e.d.Slicer.ShowRaw(ctx, rec.Worktree, rec.Item)
	if err != nil {
		return false, err
	}
	var detail slicercli.Detail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return false, err
	}
	return detail.Slice != nil && sliceTouchesUI(detail.Slice.Sections), nil
}

var (
	backticked = regexp.MustCompile("`([^`\n]+)`")
	lineAnchor = regexp.MustCompile(`:\d+(-\d+)?$`)
	adjUIKey   = regexp.MustCompile(`^ui\.[a-z][a-z0-9-]*$`)
)

// sliceTouchesUI applies the UI test of AGENTS.md and matrix rule u10 to the
// slice's Files section: a path under internal/tui (its golden views are
// there), docs/dashboard.md, a path under internal/cli (its output), or a
// ui.* adj key. Only a path or key in backticks counts, as the Files section
// asks (AGENTS.md, Editing).
func sliceTouchesUI(sections []slicercli.Section) bool {
	for _, s := range sections {
		if !strings.EqualFold(s.Heading, "Files") {
			continue
		}
		for _, m := range backticked.FindAllStringSubmatch(s.Body, -1) {
			token := strings.TrimPrefix(lineAnchor.ReplaceAllString(strings.TrimSpace(m[1]), ""), "./")
			if adjUIKey.MatchString(token) || token == "docs/dashboard.md" {
				return true
			}
			for _, dir := range []string{"internal/tui", "internal/cli"} {
				if token == dir || strings.HasPrefix(token, dir+"/") {
					return true
				}
			}
		}
	}
	return false
}

// beforeGate schedules the optional architect once, after accepted reviewer
// findings and before the gate (adj 123). The lineup alone enables it (adj 151).
func (e *Engine) beforeGate(rec runrec.Record) string {
	if _, ok := lineup(rec.Lineup, RoleArchitect); ok {
		for _, r := range rec.Roles {
			if r.Role == RoleArchitect {
				return runrec.StateGate
			}
		}
		return runrec.StateArchitect
	}
	return runrec.StateGate
}

func (e *Engine) architect(ctx context.Context) error {
	pend := e.takePending()
	rec := e.run.Snapshot()
	r := round{role: RoleArchitect, n: rec.Round, state: runrec.StateArchitect, task: rec.Item + " architect",
		sections: []BriefSection{{Title: "Architect review", Body: "Review the slice's diff after accepted reviewer findings have been addressed and before the gate (adj 123). You are read-only. The implementer and reviewer reports are under Earlier reports. For each reviewer finding under Earlier reports, report whether it was fixed (CONFIRMED-FIXED, PARTIAL or NOT-FIXED) with evidence. Give change-now findings and later work separately."}}}
	out, err := e.runRound(ctx, r, pend)
	if err != nil || !out.ok {
		return err
	}
	// The free-form report has no findings parser. Every report goes back to
	// the implementer, including one with no blockers (adj 150).
	return e.bumpRound(ctx, runrec.Feedback{Kind: "architect", File: out.file})
}

const gateCriteria = "You are the gate. A small fix round is reviewed and gated in one pass: check the new diff against the earlier findings and decide. " +
	"The first line of your report is exactly `VERDICT: PASS` or `VERDICT: FAIL`. " +
	"The criteria are: earlier blockers resolved with a proving test, no new issue of medium severity or worse, tests green, and docs matching the code. " +
	"Run the project's check command yourself and give the counts on a `Tests: N passed, M failed` line."

func (e *Engine) gate(ctx context.Context) error {
	pend := e.takePending()
	rec := e.run.Snapshot()
	r := round{role: RoleGate, n: rec.Round, state: runrec.StateGate, task: rec.Item + " gate",
		resumeFrom: []string{RoleGate, RoleReviewer},
		sections:   []BriefSection{{Title: "Gate", Body: gateCriteria}}}
	if name, text := e.latestReport(rec, RoleImplementer); text != "" {
		r.sections = append(r.sections, BriefSection{Title: "Implementer report (" + name + ")", Body: text})
	}
	if name, text := e.latestReport(rec, RoleArchitect); text != "" {
		r.sections = append(r.sections, BriefSection{Title: "Architect report (" + name + ")", Body: text})
	}
	if name, text := e.latestReport(rec, RoleUI); text != "" {
		r.sections = append(r.sections, BriefSection{Title: "UI pass report (" + name + ")", Body: text})
	}
	out, err := e.runRound(ctx, r, pend)
	if err != nil || !out.ok {
		return err
	}
	if out.report.Verdict == "PASS" {
		return e.run.Update(func(rr *runrec.Record) { rr.State = runrec.StateAwaitingApproval; rr.Reason = "" })
	}
	// FAIL: record it on the item, then back to the implementer.
	if err := e.run.Update(func(rr *runrec.Record) { rr.Fails++ }); err != nil {
		return err
	}
	rec = e.run.Snapshot()
	e.d.Slicer.Note(ctx, e.wr, rec.Worktree, rec.Item, "Review FAIL: "+failSummary(out.text))
	if rec.Fails >= 3 {
		return e.stop(ctx, fmt.Sprintf("the slice failed its gate %d times", rec.Fails))
	}
	return e.bumpRound(ctx, runrec.Feedback{Kind: "fail", File: out.file})
}

// failSummary is a gate FAIL report as one line for a note: the text after the
// verdict line, up to the Owner needed section.
func failSummary(report string) string {
	_, body, _ := strings.Cut(report, "\n")
	if i := strings.Index(body, "\n## Owner needed"); i >= 0 {
		body = body[:i]
	}
	s := strings.Join(strings.Fields(body), " ")
	if len(s) > 600 {
		s = s[:600] + "..."
	}
	return s
}

// commit commits the approved slice in its worktree with the title of the
// slice's Git section and no attribution (git.commit).
func (e *Engine) commit(ctx context.Context) error {
	rec := e.run.Snapshot()
	pauseWith := func(reason string) error {
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateCommit, Reason: reason})
	}
	raw, err := e.d.Slicer.ShowRaw(ctx, rec.Worktree, rec.Item)
	if err != nil {
		return pauseWith("slicer show: " + err.Error())
	}
	var detail slicercli.Detail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return pauseWith("slicer show: " + err.Error())
	}
	var title string
	if detail.Slice != nil {
		for _, s := range detail.Slice.Sections {
			if strings.EqualFold(s.Heading, "Git") {
				if title, err = ParseGitTitle(s.Body); err != nil {
					return pauseWith("the slice's Git section: " + err.Error())
				}
			}
		}
	}
	if title == "" {
		return pauseWith("the slice has no Git section with a commit title")
	}
	if _, err := e.gitWrite(ctx, rec.Worktree, "add", "-A"); err != nil {
		return pauseWith("git add: " + err.Error())
	}
	if _, err := e.gitWrite(ctx, rec.Worktree, "commit", "-m", title); err != nil {
		// finish already closed the ledger with PASS.
		return e.run.Update(func(r *runrec.Record) {
			r.State = runrec.StateStopped
			r.Reason = "git commit: " + err.Error()
			r.Pending = nil
		})
	}
	sha, err := e.gitRead(ctx, rec.Worktree, "rev-parse", "HEAD")
	if err != nil {
		return pauseWith("git rev-parse HEAD: " + err.Error())
	}
	return e.run.Update(func(r *runrec.Record) {
		r.Pending = nil
		r.Commit = &runrec.Commit{SHA: strings.TrimSpace(sha), Title: title, At: e.d.now()}
		r.State = runrec.StateCommitted
	})
}

// finish runs the finish step the contract orders after PASS: the implementer,
// resumed, writes the closing note and runs `slicer done` or `slicer handoff`,
// per `slicer config implement_finish`. The run ends when it is done.
func (e *Engine) finish(ctx context.Context) error {
	pend := e.takePending()
	rec := e.run.Snapshot()
	mode, err := e.d.Slicer.Config(ctx, rec.Worktree, "implement_finish")
	if err != nil {
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateFinish, Reason: "slicer config implement_finish: " + err.Error()})
	}
	var step string
	switch mode {
	case "done":
		step = fmt.Sprintf("`slicer done %s --note \"Describe the verified outcome\" --render --check`", rec.Item)
	case "handoff":
		step = fmt.Sprintf("`slicer handoff %s --render --check`", rec.Item)
	default:
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateFinish, Reason: fmt.Sprintf("implement_finish is %q; ork knows done and handoff", mode)})
	}
	body := fmt.Sprintf("The gate returned `VERDICT: PASS`, the owner approved, and the finish step must precede ork's commit (adj #81). The slice is final. "+
		"Run the closing note, then the finish step, and nothing else:\n\n"+
		"1. `slicer note %s --text \"Describe the verified outcome\" --render`\n"+
		"2. %s\n\n"+
		"Replace \"Describe the verified outcome\" with the outcome you verified. Do not commit. "+
		"Your report needs no Tests line; end it with `## Owner needed`.", rec.Item, step)
	r := round{role: RoleImplementer, n: rec.Round + 1, state: runrec.StateFinish, task: rec.Item + " implementer finish step", finish: true,
		resumeFrom: []string{RoleImplementer}, sections: []BriefSection{{Title: "Finish step", Body: body}}}
	out, err := e.runRound(ctx, r, pend)
	if err != nil || !out.ok {
		return err
	}
	if err := e.run.Update(func(rr *runrec.Record) { rr.State = runrec.StateCommit; rr.Reason = "" }); err != nil {
		return err
	}
	e.endRelay(ctx, "pass", "done")
	return nil
}

// ensureWorktree tolerates a setup interrupted after worktree creation. It
// verifies the checkout, branch and common repository before using it.
func (e *Engine) ensureWorktree(ctx context.Context, rec runrec.Record) error {
	if _, err := os.Stat(rec.Worktree); errors.Is(err, os.ErrNotExist) {
		_, err = e.gitWrite(ctx, e.proj.Path, "worktree", "add", rec.Worktree, "-b", rec.Branch)
		return err
	} else if err != nil {
		return err
	}
	top, err := e.gitRead(ctx, rec.Worktree, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	branch, err := e.gitRead(ctx, rec.Worktree, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return err
	}
	common, err := e.gitRead(ctx, rec.Worktree, "rev-parse", "--git-common-dir")
	if err != nil {
		return err
	}
	mainCommon, err := e.gitRead(ctx, e.proj.Path, "rev-parse", "--git-common-dir")
	if err != nil {
		return err
	}
	canonical := func(base, value string) string {
		value = strings.TrimSpace(value)
		if !filepath.IsAbs(value) {
			value = filepath.Join(base, value)
		}
		resolved, err := filepath.EvalSymlinks(value)
		if err == nil {
			return resolved
		}
		return filepath.Clean(value)
	}
	if canonical(rec.Worktree, top) != canonical(rec.Worktree, rec.Worktree) || strings.TrimSpace(branch) != rec.Branch || canonical(rec.Worktree, common) != canonical(e.proj.Path, mainCommon) {
		return errors.New("existing worktree does not match this run")
	}
	return nil
}
