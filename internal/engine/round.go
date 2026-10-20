// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// round is one role round the state machine asks for.
type round struct {
	role     string
	n        int    // the round number, for file names
	state    string // the state the round belongs to; a pause re-enters it
	task     string // the ledger step's label
	finish   bool   // the implementer's finish step
	sections []BriefSection
	// resumeFrom lists the roles whose latest session this round resumes. A
	// pause on a question or a shape error also lets the round resume its own
	// role's session.
	resumeFrom []string
}

// outcome is what a finished round gives its state function. ok is false when
// the round already stopped or paused the slice.
type outcome struct {
	ok       bool
	text     string
	report   *Report
	findings string // the reviewer's findings
	file     string // the report file name in the run directory
}

// latestSession returns the newest session id among the roles.
func latestSession(rec runrec.Record, from []string) string {
	for i := len(rec.Roles) - 1; i >= 0; i-- {
		r := rec.Roles[i]
		if r.Session != "" && contains(from, r.Role) {
			return r.Session
		}
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// denialNames returns the tool name of each denial, or nil when there are none.
func denialNames(denials []runrec.DeniedCommand) []string {
	var names []string
	for _, d := range denials {
		names = append(names, d.Tool)
	}
	return names
}

// denialTexts joins the text of each denial for a stop reason (DeniedCommand.Text).
func denialTexts(denials []runrec.DeniedCommand) string {
	texts := make([]string, 0, len(denials))
	for _, d := range denials {
		texts = append(texts, d.Text())
	}
	return strings.Join(texts, ", ")
}

// git runs `git -C dir ARGS...` as a read.
func (e *Engine) gitRead(ctx context.Context, dir string, argv ...string) (string, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: e.d.gitBin(), Args: append([]string{"-C", dir}, argv...), Timeout: 30 * time.Second})
	return string(res.Stdout), err
}

// gitWrite runs `git -C dir ARGS...` as a write: logged in writes[].
func (e *Engine) gitWrite(ctx context.Context, dir string, argv ...string) (clirun.Result, error) {
	return e.wr.Run(ctx, runrec.Cmd{
		Tool: "git", Bin: e.d.gitBin(), Root: dir, Argv: argv,
		Args: append([]string{"-C", dir}, argv...), Timeout: 2 * time.Minute,
	})
}

// treeStatus is the worktree fingerprint of a read-only role: the output of
// `git status --porcelain` (role.readonly-fingerprint).
func (e *Engine) treeStatus(ctx context.Context, worktree string) (string, error) {
	out, err := e.gitRead(ctx, worktree, "status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	return out, nil
}

// earlierReports reads the reports of the rounds so far.
func (e *Engine) earlierReports(rec runrec.Record) []EarlierReport {
	var out []EarlierReport
	for _, r := range rec.Roles {
		if r.Report == "" {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(e.run.Dir(), r.Report)); err == nil {
			out = append(out, EarlierReport{Name: strings.TrimSuffix(r.Report, ".md"), Text: string(b)})
		}
	}
	return out
}

// pendingSection is what a resumed round tells its role about why it runs
// again.
func pendingSection(p *runrec.Pending) []BriefSection {
	if p == nil {
		return nil
	}
	if p.Answer != "" {
		return []BriefSection{{Title: "Owner answer", Body: fmt.Sprintf("Your report asked the owner:\n\n%s\n\nThe owner answered:\n\n%s", p.Question, p.Answer)}}
	}
	switch p.Kind {
	case runrec.PendingQuestion:
		return []BriefSection{{Title: "Owner answer", Body: fmt.Sprintf("Your report asked the owner:\n\n%s\n\nThe owner answered:\n\n%s", p.Question, p.Answer)}}
	case runrec.PendingShape:
		return []BriefSection{{Title: "Report shape", Body: "ork checked your last report and refused it: " + p.Reason + "\n\nWrite the report again so that it has the shape the prompt asks for, and exit."}}
	case runrec.PendingLost:
		return []BriefSection{{Title: "Earlier process lost", Body: "The process that ran this role before was lost (" + p.Reason + "). The worktree may hold its partial work. Look at it, then continue from it."}}
	}
	return nil
}

// runRound runs one role round: brief, ledger step, process, stop rules,
// report checks. When it returns ok false the slice is already stopped or
// paused (or err says why not).
func (e *Engine) runRound(ctx context.Context, r round, pend *runrec.Pending) (outcome, error) {
	rec := e.run.Snapshot()
	entry, _ := lineup(rec.Lineup, r.role)
	perm, err := ResolvePermission(e.proj, r.role)
	if err != nil {
		return outcome{}, e.pauseError(r, "permissions: "+err.Error())
	}
	fail := func(what string, err error) (outcome, error) {
		return outcome{}, e.pauseError(r, what+": "+err.Error())
	}

	reportName := fmt.Sprintf("%s-%d.md", r.role, r.n)
	attempt := 1
	for _, old := range rec.Roles {
		if old.Role == r.role && old.Round == r.n {
			attempt++
		}
	}
	if attempt > 1 {
		reportName = fmt.Sprintf("%s-%d-attempt-%d.md", r.role, r.n, attempt)
	}
	reportPath := filepath.Join(e.run.Dir(), reportName)
	tempDir := filepath.Join(e.run.Dir(), "tmp", fmt.Sprintf("%s-%d", r.role, r.n))
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return fail("temp directory", err)
	}
	if err := os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail("old report", err)
	}

	allowed := r.resumeFrom
	if pend != nil && (pend.Kind == runrec.PendingQuestion || pend.Kind == runrec.PendingShape) {
		allowed = append([]string{r.role}, allowed...)
	}
	session := latestSession(rec, allowed)

	sliceJSON, err := e.d.Slicer.ShowRaw(ctx, rec.Worktree, rec.Item)
	if err != nil {
		return fail("slicer show", err)
	}
	in := BriefInput{
		Role: r.role, SliceID: rec.Item, Worktree: rec.Worktree, TempDir: tempDir, ReportPath: reportPath,
		SliceJSON: string(sliceJSON), EnvLines: e.proj.EnvironmentLines,
		Sections: append(pendingSection(pend), r.sections...),
	}
	if session == "" {
		in.Earlier = e.earlierReports(rec)
	}
	brief, err := BuildBrief(in)
	if err != nil {
		return fail("brief", err)
	}

	var before string
	if perm.ReadOnly {
		if before, err = e.treeStatus(ctx, rec.Worktree); err != nil {
			return fail("read-only fingerprint", err)
		}
	}

	stepID := e.stepStart(ctx, rec, r, entry)
	idx := -1
	err = e.run.Update(func(rr *runrec.Record) {
		rr.Roles = append(rr.Roles, runrec.RoleRecord{
			Role: r.role, Round: r.n, Vendor: entry.Vendor, Model: entry.Model, Effort: entry.Effort,
			Step: stepID, StartedAt: e.d.now(),
		})
		idx = len(rr.Roles) - 1
	})
	if err != nil {
		return outcome{}, err
	}

	res, runErr := e.d.Runner.Run(ctx, Request{
		Role: r.role, Round: r.n, Vendor: entry.Vendor, Model: entry.Model, Effort: entry.Effort,
		Brief: brief, Worktree: rec.Worktree, ReportPath: reportPath,
		Allowlist: perm.Allow, ReadOnly: perm.ReadOnly, Resume: session,
		OnStart: func(pgid int) { e.run.Update(func(rr *runrec.Record) { rr.Roles[idx].PGID = pgid }) },
	})
	if err := ctx.Err(); err != nil {
		return outcome{}, err // the role stays running in the record; a restart marks it lost
	}
	class := res.Exit
	switch {
	case runErr != nil:
		class = ExitError
	case class == "":
		class = ExitError
		runErr = errors.New("the runner gave no exit class")
	}

	var changed bool
	var statusErr error
	if perm.ReadOnly {
		var after string
		if after, statusErr = e.treeStatus(ctx, rec.Worktree); statusErr == nil {
			changed = after != before
		}
	}

	text, file := "", ""
	if class == ExitOK {
		text, file = e.readReport(reportPath, reportName, res.Report)
	}
	err = e.run.Update(func(rr *runrec.Record) {
		rr.Pending = nil
		role := &rr.Roles[idx]
		role.EndedAt = e.d.now()
		role.Exit = string(class)
		role.Session = res.Session
		role.Tokens = res.Tokens
		role.CostUSD = res.CostUSD
		role.Denials = denialNames(res.Denials)
		role.DeniedCommands = res.Denials
		role.Report = file
	})
	if err != nil {
		return outcome{}, err
	}

	var stopReason string
	switch {
	case class != ExitOK:
		stopReason = fmt.Sprintf("%s round %d exited %s", r.role, r.n, class)
		if len(res.Denials) > 0 {
			stopReason += ": permission denied: " + denialTexts(res.Denials)
		}
		if runErr != nil {
			stopReason += ": " + runErr.Error()
		}
	case changed:
		stopReason = fmt.Sprintf("%s round %d changed the tree, and it is a read-only role", r.role, r.n)
	}
	if stopReason != "" {
		e.stepEnd(ctx, rec, stepID, "fail")
		return outcome{}, e.stop(ctx, stopReason)
	}
	e.stepEnd(ctx, rec, stepID, "done")
	if statusErr != nil {
		return fail("read-only fingerprint", statusErr)
	}

	// The report.
	rep, err := CheckReport(r.role, r.finish, text)
	var problems []string
	var se *ShapeError
	if errors.As(err, &se) {
		problems = se.Problems
	} else if err != nil {
		return outcome{}, err
	}
	var findings string
	if r.role == RoleReviewer {
		var ferr error
		if findings, ferr = Findings(text); ferr != nil {
			var fse *ShapeError
			if errors.As(ferr, &fse) {
				problems = append(problems, fse.Problems...)
			}
		}
	}
	if len(problems) > 0 {
		return outcome{}, e.pause(runrec.Pending{
			Kind: runrec.PendingShape, State: r.state, Role: r.role, Round: r.n,
			Reason: (&ShapeError{Problems: problems}).Error(),
		})
	}
	if rep.Question != "" {
		err := e.run.Update(func(rr *runrec.Record) {
			rr.Questions = append(rr.Questions, runrec.Question{Role: r.role, Round: r.n, Text: rep.Question, At: e.d.now()})
		})
		if err != nil {
			return outcome{}, err
		}
		return outcome{}, e.pause(runrec.Pending{
			Kind: runrec.PendingQuestion, State: r.state, Role: r.role, Round: r.n,
			Reason:   fmt.Sprintf("%s round %d needs the owner", r.role, r.n),
			Question: rep.Question,
		})
	}
	return outcome{ok: true, text: text, report: rep, findings: findings, file: file}, nil
}

// readReport returns the role's report: the file at the report path, else the
// vendor's final result text (role.report-io). A report taken from the result
// is saved to the report path. file is the report's name in the run
// directory, empty when there is no report.
func (e *Engine) readReport(path, name, fallback string) (text, file string) {
	if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
		return string(b), name
	}
	if strings.TrimSpace(fallback) == "" {
		return "", ""
	}
	if err := os.WriteFile(path, []byte(fallback), 0o644); err != nil {
		return fallback, ""
	}
	return fallback, name
}

// pauseError pauses the slice on a failure the engine cannot go on from. The
// owner retries the state or stops the slice.
func (e *Engine) pauseError(r round, reason string) error {
	return e.pause(runrec.Pending{Kind: runrec.PendingError, State: r.state, Role: r.role, Round: r.n, Reason: reason})
}

// stepStart records a role round as a ledger step under the orchestrator step
// ork owns, and returns its id. Recording is advisory: it returns "" when the
// call fails.
func (e *Engine) stepStart(ctx context.Context, rec runrec.Record, r round, entry runrec.LineupEntry) string {
	if rec.RelayRun == "" {
		return ""
	}
	step, err := e.d.Slicer.RelayStep(ctx, e.wr, rec.Worktree, rec.RelayRun, slicercli.StepSpec{
		Role: r.role, Agent: entry.Vendor, Vendor: entry.Vendor, Model: entry.Model, Reasoning: entry.Effort,
		Parent: rec.OrchestratorStep, Phase: "start", Task: r.task,
	})
	if err != nil {
		return ""
	}
	return step.ID
}

// stepEnd sets the phase of a ledger step when its round ends.
func (e *Engine) stepEnd(ctx context.Context, rec runrec.Record, id, phase string) {
	if id == "" || rec.RelayRun == "" {
		return
	}
	e.d.Slicer.RelayStep(ctx, e.wr, rec.Worktree, rec.RelayRun, slicercli.StepSpec{Step: id, Phase: phase})
}
