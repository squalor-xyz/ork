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
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// excludeLine is the line setup adds to info/exclude so the worktrees never
// show as untracked files in the main checkout.
const excludeLine = ".worktrees/"

// setup creates the worktree and the ledger run. A failure here stops the
// slice: nothing was started that a retry could take over. Once both exist, it
// verifies the check command where the round check will run (S295,
// *proposed*); a command that cannot run pauses the run before round 1, at
// implement, so that a retry does not begin the ledger run again.
func (e *Engine) setup(ctx context.Context) error {
	rec := e.run.Snapshot()
	if err := e.ensureExclude(ctx); err != nil {
		return e.stop(ctx, "setup: "+err.Error())
	}
	if err := e.ensureWorktree(ctx, rec); err != nil {
		return e.stop(ctx, "setup: git worktree add: "+err.Error())
	}
	e.beginRelay(ctx, rec)
	if reason := e.checkCannotRun(ctx, rec); reason != "" {
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Reason: reason})
	}
	e.checkVerified = true
	return e.run.Update(func(r *runrec.Record) { r.State = runrec.StateImplement })
}

// checkCannotRun is the static check (VerifyCheck) of the commands the run
// will run, in the directory the round check uses (CheckDir). It returns the
// reason of a pause, or "" when they can run. It runs nothing. A directory it
// cannot name is left to the checks, which pause with that reason.
func (e *Engine) checkCannotRun(ctx context.Context, rec runrec.Record) string {
	if e.proj.NoVerifyCheck {
		return "" // the owner stored the check with --no-verify
	}
	dir, err := e.CheckDir(ctx, e.proj.Path, rec.Worktree)
	if err != nil {
		return ""
	}
	problems, flag := CheckCommandProblems(ctx, e.proj, dir, e.d.Verify)
	if len(problems) == 0 {
		return ""
	}
	return oneLine(fmt.Sprintf(CheckCannotRunPrefix+"%s: %s; to fix the check: ork stop %s, ork projects set %s %s CMD, then ork retry %s",
		dir, strings.Join(problems, "; "), rec.ID, ShellWord(e.proj.Name), flag, rec.ID))
}

// beginRelay begins the slicer ledger run of the engine's run and its
// orchestrator step. Recording is advisory: a failed relay-run call never
// stops the relay.
func (e *Engine) beginRelay(ctx context.Context, rec runrec.Record) {
	run, err := e.d.Slicer.RelayBegin(ctx, e.wr, rec.Worktree, rec.Worktree, rec.Item)
	if err != nil {
		return
	}
	e.run.Update(func(r *runrec.Record) { r.RelayRun = run.ID })
	step, err := e.d.Slicer.RelayStep(ctx, e.wr, rec.Worktree, run.ID, slicercli.StepSpec{
		Role: rec.LedgerRoles["orchestrator"], Agent: "ork", Phase: "start", Task: rec.Item + " orchestrator",
	})
	if err == nil {
		e.run.Update(func(r *runrec.Record) { r.OrchestratorStep = step.ID })
	}
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
		return BriefSection{Title: "Checks failed", Body: "The project checks are red. Fix the failures below. No review was spent on this diff.\n\n" + checkFailureBody(body, e.checkCommandFor(fb.File), e.checkAttempts(fb.File))}
	case "findings":
		if strings.HasPrefix(fb.File, "findings-") {
			return BriefSection{Title: "Review findings", Body: "The reports below hold findings: the reviewer's, the UI pass's, or both. Apply only the findings in them; owner and later findings go to the owner, not to you. The UI pass gives suggested design edits for the screens this slice changes. If a finding is wrong, say so in your report with your evidence.\n\n" + body}
		}
		return BriefSection{Title: "Review findings", Body: "The reviewer's report is below. Apply only the findings in it. If a finding is wrong, say so in your report with your evidence.\n\n" + body}
	case "owner":
		return BriefSection{Title: "Owner fix request", Body: "The owner read the approval card and asks for the change below. Make it and nothing else: the slice already passed the gate. Say in your report what you changed.\n\n" + body}
	case "land":
		return BriefSection{Title: "Landing blocked", Body: "The owner approved the slice and ork began to land it: it merged the base branch into the slice branch in your worktree, rendered and committed. The checks are red after that merge, and the output is below. " +
			"Fix them; the base branch moved, for example a golden view that another slice changed. " +
			"You do not commit: ork commits your change at the next approval. Do not commit, rebase, force, reset or abort, and do not change anything else. The gate runs again after this round, and the owner approves again. Say in your report what you fixed.\n\n" + body}
	case "architect":
		return BriefSection{Title: "Architect findings", Body: "The architect's whole report is below. Apply its change-now findings. If a finding is wrong, say so in your report with your evidence. Keep later work out of this slice.\n\n" + body}
	default:
		return BriefSection{Title: "Gate FAIL", Body: "The gate failed this diff. Its report is below. Fix what it lists.\n\n" + body}
	}
}

func (e *Engine) roundLimit(rec runrec.Record) int {
	return max(e.d.maxRounds(), rec.RoundLimit)
}

// noProgress requires adjacent checks and implementer content fingerprints.
// Missing fingerprints from older run records cannot prove no progress.
func noProgress(rec runrec.Record) bool {
	var trees [2]string
	var logs [2]string
	for i := len(rec.Roles) - 1; i >= 0; i-- {
		r := rec.Roles[i]
		d := rec.Round - r.Round
		if r.Role == RoleImplementer && d >= 0 && d < 2 && trees[d] == "" {
			trees[d] = r.TreeSHA256
		}
	}
	for i := len(rec.Checks) - 1; i >= 0; i-- {
		c := rec.Checks[i]
		d := rec.Round - c.Round
		if d >= 0 && d < 2 && logs[d] == "" && c.Log == fmt.Sprintf("checks-%d.log", c.Round) {
			logs[d] = c.LogSHA256
		}
	}
	return trees[0] != "" && trees[0] == trees[1] && logs[0] != "" && logs[0] == logs[1]
}

// bumpRound preserves feedback at a pause, before the next round starts.
func (e *Engine) bumpRound(ctx context.Context, fb runrec.Feedback, failure ...string) error {
	rec := e.run.Snapshot()
	stalled := fb.Kind == "checks" && noProgress(rec)
	if stalled || rec.Round >= e.roundLimit(rec) {
		log, _ := os.ReadFile(filepath.Join(e.run.Dir(), fb.File))
		lines := strings.Split(strings.TrimSpace(string(log)), "\n")
		last := strings.TrimSpace(lines[len(lines)-1])
		if len(failure) > 0 {
			last = failure[0]
		}
		step := fb.Kind
		switch fb.Kind {
		case "findings":
			step = "review"
		case "fail":
			step = "gate"
		}
		why := fmt.Sprintf("round %d ended red: %s failed (%s)", rec.Round, step, last)
		if stalled {
			why += fmt.Sprintf("; no change in the worktree and the same check output as round %d", rec.Round-1)
		}
		why += "; grant more rounds (2 by default), change the check (stop the run, set it, start a new run), or stop"
		if err := e.run.Update(func(r *runrec.Record) { r.Feedback = &fb }); err != nil {
			return err
		}
		return e.pause(runrec.Pending{Kind: runrec.PendingRounds, State: runrec.StateImplement, Role: RoleImplementer, Round: rec.Round + 1, Reason: why})
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
	// S295 (*proposed*): before round 1 the check command is verified, unless
	// setup just did. A retry of the setup pause, or a resume of the stopped run
	// (S296) with the project's fixed check, comes here and not through setup.
	if !e.checkVerified && rec.Feedback == nil && !hasImplementerRound(rec) {
		if reason := e.checkCannotRun(ctx, rec); reason != "" {
			return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Reason: reason})
		}
	}
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
	fingerprint, err := e.treeContent(ctx, rec.Worktree)
	if err != nil {
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Reason: "worktree fingerprint: " + err.Error()})
	}
	if err := e.run.Update(func(rr *runrec.Record) {
		for i := len(rr.Roles) - 1; i >= 0; i-- {
			if rr.Roles[i].Role == RoleImplementer && rr.Roles[i].Round == rec.Round {
				rr.Roles[i].TreeSHA256 = fingerprint
				break
			}
		}
	}); err != nil {
		return err
	}
	return e.run.Update(func(rr *runrec.Record) { rr.Feedback = nil; rr.State = runrec.StateChecks })
}

func (e *Engine) checks(ctx context.Context) error {
	rec := e.run.Snapshot()
	run, paused, err := e.runChecks(ctx, rec, runrec.StateChecks, "checks", e.roundCommand())
	if paused || err != nil {
		return err
	}
	if !run.ok {
		return e.bumpRound(ctx, runrec.Feedback{Kind: "checks", File: run.log}, run.failure)
	}
	if e.needsFullCheck() {
		// S285 (*proposed*): a green round check is not enough for review or the
		// gate. The full command runs once, under its own log name, so noProgress
		// still reads only the round checks' logs.
		run, paused, err = e.runChecks(ctx, rec, runrec.StateChecks, "checks-full", e.proj.CheckCommand)
		if paused || err != nil {
			return err
		}
		if !run.ok {
			return e.bumpRound(ctx, runrec.Feedback{Kind: "checks", File: run.log}, run.failure)
		}
	}
	next := runrec.StateReview
	for _, r := range rec.Roles {
		if r.Role == RoleReviewer {
			next = e.beforeGate(rec) // accepted reviewer findings are fixed before the architect
		}
	}
	return e.run.Update(func(r *runrec.Record) { r.State = next })
}

// checkRun is the outcome of one run of the project's checks.
type checkRun struct {
	ok       bool
	attempts int    // 2 when the project command was run again (S239)
	log      string // the log's file name in the run directory
	failure  string // last meaningful output line from the first failed check
}

// checkAttempts is how many times the project command ran for the check log
// of that name: 2 when it was run again (S239), 0 when the record has no such
// check or the command ran once.
func (e *Engine) checkAttempts(log string) int {
	for _, c := range e.run.Snapshot().Checks {
		if c.Log == log {
			return c.Attempts
		}
	}
	return 0
}

// checkFailureLine skips blank lines and standalone JSON closing punctuation.
func checkFailureLine(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		switch line {
		case "", "}", "]", "},", "],":
			continue
		}
		return line
	}
	return "no failure output"
}

// itemID is the shape slicer allows for an item ID (slicer/src/slicer/ids.py ID_RE).
var itemID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// problemItem is the item a `slicer check` problem names: the text before the
// first ": ", cut at " / " when a section follows ("ITEM: message" or
// "ITEM / SECTION: message"). It is "" for a problem with no item and for text
// that is not shaped like an item ID.
func problemItem(problem string) string {
	head, _, ok := strings.Cut(problem, ": ")
	if !ok {
		return ""
	}
	head, _, _ = strings.Cut(head, " / ")
	head = strings.TrimSpace(head)
	if !itemID.MatchString(head) {
		return ""
	}
	return head
}

// outsideProblems returns the slicer check problems a round check defers to
// the landing (adj 178, checks.outside-problems; S279), and the base branch.
// It defers only when every problem names an item other than the run's own,
// nothing else is red in the report, and the branch is behind its base branch,
// so that the landing merges the base in and re-checks. A problem that is not
// a string, a problem with no item, and any git error mean no deferral: the
// round is red. Git is read only when the report could defer. ork writes no
// git here.
func (e *Engine) outsideProblems(ctx context.Context, rec runrec.Record, sres *slicercli.CheckResult) (base string, problems []string) {
	if len(sres.Problems) == 0 || len(sres.StaleRender) > 0 || len(sres.StaleSync) > 0 || len(sres.OrphanRender) > 0 {
		return "", nil
	}
	for _, raw := range sres.Problems {
		// A problem that is not a string stays empty here and has no item.
		var p string
		_ = json.Unmarshal(raw, &p)
		item := problemItem(p)
		if item == "" || strings.EqualFold(item, rec.Item) {
			return "", nil
		}
		problems = append(problems, p)
	}
	out, err := e.gitRead(ctx, e.proj.Path, "symbolic-ref", "--short", "HEAD")
	base = strings.TrimSpace(out)
	if err != nil || base == "" {
		return "", nil
	}
	has, err := e.isAncestor(ctx, rec.Worktree, base, "HEAD")
	if err != nil || has {
		return "", nil
	}
	return base, problems
}

// roundCommand is the command of the checks after implementer rounds: the
// project's round check command when it names one, else its check command
// (S285, *proposed*). The check before review or the gate and the landing
// re-check run the check command.
func (e *Engine) roundCommand() string { return roundCommandOf(e.proj) }

func roundCommandOf(p registry.Project) string {
	if p.RoundCheckCommand != "" {
		return p.RoundCheckCommand
	}
	return p.CheckCommand
}

// needsFullCheck says a green round check is followed by a check with the
// check command: the project has both commands and they differ.
func (e *Engine) needsFullCheck() bool {
	return e.proj.CheckCommand != "" && e.roundCommand() != e.proj.CheckCommand
}

// checkCommandFor is the command the check that wrote the log ran, as
// recorded; the project's check command when the record has no such check.
func (e *Engine) checkCommandFor(log string) string {
	for _, c := range e.run.Snapshot().Checks {
		if c.Log == log && c.Command != "" && c.Command != slicerCheckCommand {
			return c.Command
		}
	}
	return e.proj.CheckCommand
}

// slicerCheckCommand is what a check row names when the project has no command.
const slicerCheckCommand = "slicer check --json"

// runChecks runs the command cmd (when it is not empty) and `slicer check` in
// the worktree, writes their output to PREFIX-ROUND.log in the run directory
// and records the result in checks[]. A tool that cannot run pauses the slice
// at state, and paused is true; the caller returns err then.
func (e *Engine) runChecks(ctx context.Context, rec runrec.Record, state, prefix, cmd string) (run checkRun, paused bool, err error) {
	start := e.d.now()
	defer e.beginWork(prefix, nil)()
	check := e.d.Check
	if check == nil {
		check = RunCheck
	}
	timeout := time.Duration(e.proj.CheckMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	pauseWith := func(reason string) (checkRun, bool, error) {
		return checkRun{}, true, e.pause(runrec.Pending{Kind: runrec.PendingError, State: state, Reason: reason})
	}
	ok := true
	failure := ""
	attempts, flaky := 0, false
	var log strings.Builder
	if cmd != "" {
		// S293: the command runs in the project's own folder of the worktree.
		// A project at the repository root has the worktree root; a project in
		// a subfolder (a monorepo) says so in the log, once per run.
		dir, err := e.CheckDir(ctx, e.proj.Path, rec.Worktree)
		if err != nil {
			return pauseWith("check directory: " + err.Error())
		}
		where := ""
		if rel, err := filepath.Rel(rec.Worktree, dir); err == nil && rel != "." {
			where = "(in " + filepath.ToSlash(rel) + ")\n"
		}
		res, err := check(ctx, dir, cmd, timeout)
		if err != nil {
			return pauseWith("check command: " + err.Error())
		}
		fmt.Fprintf(&log, "$ %s\n%s%s\n", cmd, where, checkLogOutput(res.Output))
		// S239: an exit failure is run once more before a round is spent on
		// it, because load or a flaky test can fail a run that is fine. A
		// timeout is not run again, and `slicer check` never is.
		if !res.OK && !res.TimedOut && ctx.Err() == nil {
			again, err := check(ctx, dir, cmd, timeout)
			if err != nil {
				return pauseWith("check command: " + err.Error())
			}
			attempts, flaky = 2, again.OK
			fmt.Fprintf(&log, "\n$ %s%s\n%s%s\n", cmd, rerunMarker, where, checkLogOutput(again.Output))
			res = again
		}
		ok = res.OK
		if !res.OK {
			failure = checkFailureLine(res.Output)
		}
	}
	sres, err := e.d.Slicer.Check(ctx, rec.Worktree)
	if err != nil {
		return pauseWith("slicer check: " + err.Error())
	}
	var base string
	var deferred []string
	if !sres.OK {
		// Only a round check defers: the landing re-check, which runs after
		// the merge, decides these problems.
		if state == runrec.StateChecks {
			base, deferred = e.outsideProblems(ctx, rec, sres)
		}
		if len(deferred) == 0 {
			if ok {
				failure = checkFailureLine(sres.Output)
			}
			ok = false
		}
	}
	fmt.Fprintf(&log, "\n$ slicer check --json\n%s\n", tail(sres.Output, 30000))
	for _, p := range deferred {
		fmt.Fprintf(&log, "deferred to land (branch behind %s): %s\n", base, p)
	}

	logName := fmt.Sprintf("%s-%d.log", prefix, rec.Round)
	logPath := filepath.Join(e.run.Dir(), logName)
	err = os.WriteFile(logPath, []byte(log.String()), 0o600)
	if err == nil {
		err = os.Chmod(logPath, 0o600)
	}
	if err != nil {
		return pauseWith("check log: " + err.Error())
	}
	command := cmd
	if command == "" {
		command = slicerCheckCommand
	}
	err = e.run.Update(func(r *runrec.Record) {
		r.Pending = nil
		r.Checks = append(r.Checks, runrec.CheckRecord{Round: rec.Round, Command: command, OK: ok, Seconds: int(e.d.now().Sub(start).Seconds()), Log: logName, LogSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(log.String()))), Deferred: deferred, Attempts: attempts, Flaky: flaky})
	})
	return checkRun{ok: ok, attempts: attempts, log: logName, failure: failure}, false, err
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
	if section := deferredSection(rec.Checks, false); section != nil {
		rounds[0].sections = append(rounds[0].sections, *section)
	}
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
	// overrides a pause. A sibling owner question is queued behind the first.
	reviewCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	outs := make([]outcome, len(todo))
	errs := make([]error, len(todo))
	var wg sync.WaitGroup
	for i, r := range todo {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := pendFor(pend, r.role)
			if p == nil {
				p = answeredQuestion(rec, r.role)
			}
			outs[i], errs[i] = e.runRound(reviewCtx, r, p)
			if e.run.Snapshot().State == runrec.StateStopped {
				cancel()
			}
		}()
	}
	wg.Wait()
	stopped := e.run.Snapshot().State == runrec.StateStopped
	for i := range todo {
		if errs[i] != nil && !(stopped && ctx.Err() == nil && errors.Is(errs[i], context.Canceled)) {
			return errs[i]
		}
	}
	if stopped {
		return ctx.Err()
	}
	for i := range todo {
		if !outs[i].ok {
			return nil
		}
	}

	// Only the fix findings go to the fix round (adj 163). Owner and later
	// findings were kept, or asked, when each report was read.
	rec = e.run.Snapshot()
	revFile, revText := e.roundReport(rec, RoleReviewer)
	// Every report here was accepted by its round: its own shape, or the owner's
	// choice to keep owner findings that have no options (S298, *proposed*).
	rev := triaged(triage(revText, true))
	uiFile, uiText := e.roundReport(rec, RoleUI)
	ui := triaged(triage(uiText, true))
	switch {
	case rev.Fix == "" && ui.Fix == "":
		return e.run.Update(func(rr *runrec.Record) { rr.State = e.beforeGate(rec) })
	case ui.Fix == "" && !rev.Tagged:
		return e.bumpRound(ctx, runrec.Feedback{Kind: "findings", File: revFile})
	}
	// The findings of both reports reach the implementer in one fix round. A
	// report without tags goes in whole; one with tags, as its fix findings.
	var b strings.Builder
	if rev.Fix != "" {
		fmt.Fprintf(&b, "### Reviewer report (%s)\n\n%s\n\n", strings.TrimSuffix(revFile, ".md"), strings.TrimSpace(fixBody(rev, revText)))
	}
	if ui.Fix != "" {
		fmt.Fprintf(&b, "### UI pass report (%s)\n\n%s\n", strings.TrimSuffix(uiFile, ".md"), strings.TrimSpace(fixBody(ui, uiText)))
	}
	name := fmt.Sprintf("findings-%d.md", rec.Round)
	if err := os.WriteFile(filepath.Join(e.run.Dir(), name), []byte(b.String()), 0o600); err != nil {
		return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateReview, Reason: "findings file: " + err.Error()})
	}
	return e.bumpRound(ctx, runrec.Feedback{Kind: "findings", File: name})
}

// triaged is t, or no findings when the report did not parse.
func triaged(t *Triaged, err error) *Triaged {
	if err != nil || t == nil {
		return &Triaged{}
	}
	return t
}

// fixBody is what a fix round reads of a report: the whole report when it
// carries no kind tags, else only its fix findings.
func fixBody(t *Triaged, text string) string {
	if t.Tagged {
		return t.Fix
	}
	return text
}

// answeredQuestion restores each parallel role's answer after queued questions
// have been answered, including across a daemon restart.
func answeredQuestion(rec runrec.Record, role string) *runrec.Pending {
	for i := len(rec.Questions) - 1; i >= 0; i-- {
		q := rec.Questions[i]
		if q.Role == role && q.Round == rec.Round {
			if q.Answer == "" {
				return nil
			}
			return &runrec.Pending{Kind: runrec.PendingQuestion, State: runrec.StateReview, Role: role, Round: q.Round, Question: q.Text, Ask: q.Ask, Key: q.Key, Answer: q.Answer}
		}
	}
	return nil
}

// pendFor is the pause context a round of the review state takes: its own, or
// a budget grant, which is for the whole review.
//
// A question that was asked before a retry carries that retry's context (S159).
// The role the question names gets its answer; any other role takes the
// retried pause if it names that role.
func pendFor(p *runrec.Pending, role string) *runrec.Pending {
	if p == nil {
		return nil
	}
	if p.Role == role {
		return p
	}
	if p.Retry != nil {
		if r := pendFor(p.Retry, role); r != nil {
			return r
		}
	}
	if p.Role == "" || p.Kind == runrec.PendingBudget {
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
	for p := pend; p != nil; p = p.Retry {
		if p.Kind != runrec.PendingBudget && (p.Role == "" || p.Role == role) {
			return false
		}
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
		// A blocker that no one asked yet means the round is not done.
		tri, err := triage(string(b), r.ShapeAccepted)
		return err == nil && tri.Blocker() == nil
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
	"A successful ork round check result is the tests-green evidence. You must accept it and must not run the suite. " +
	"Cite ork's result separately and give only the counts of tests you actually ran on a `Tests: N passed, M failed` line."

func (e *Engine) gate(ctx context.Context) error {
	pend := e.takePending()
	rec := e.run.Snapshot()
	r := round{role: RoleGate, n: rec.Round, state: runrec.StateGate, task: rec.Item + " gate",
		resumeFrom: []string{RoleGate, RoleReviewer},
		sections:   []BriefSection{{Title: "Gate", Body: gateCriteria}}}
	if len(rec.GateCriteria) > 0 {
		r.sections[0].Body += "\nThe criteria from slicer are:\n- " + strings.Join(rec.GateCriteria, "\n- ")
	}
	if name, text := e.latestReport(rec, RoleImplementer); text != "" {
		r.sections = append(r.sections, BriefSection{Title: "Implementer report (" + name + ")", Body: text})
	}
	if name, text := e.latestReport(rec, RoleArchitect); text != "" {
		r.sections = append(r.sections, BriefSection{Title: "Architect report (" + name + ")", Body: text})
	}
	if name, text := e.latestReport(rec, RoleUI); text != "" {
		r.sections = append(r.sections, BriefSection{Title: "UI pass report (" + name + ")", Body: text})
	}
	// An owner fix goes to every gate until one passes the change: a red check
	// or a gate FAIL sends the slice to a later round (S141).
	for _, fix := range rec.Fixes {
		if rec.Card == nil || fix.Round > rec.Card.Round {
			r.sections = append(r.sections, BriefSection{Title: "Owner fix request", Body: "The owner asked for this change after the last pass. Check that the new diff makes it, and nothing else.\n\n" + fix.Note})
		}
	}
	if section := deferredSection(rec.Checks, false); section != nil {
		r.sections = append(r.sections, *section)
	}
	if section := e.scopeSection(ctx, rec); section != nil {
		r.sections = append(r.sections, *section)
	}
	out, err := e.runRound(ctx, r, pend)
	if err != nil || !out.ok {
		return err
	}
	if out.report.Verdict == "PASS" {
		// An owner question answered in the ledger shows on the finding before
		// the card is built (S223, proposed).
		e.syncLedgerAnswers(ctx)
		card := e.buildCard(ctx, e.run.Snapshot(), out.report)
		// S331 (*proposed*; adj 281): a run that qualifies is approved here, with
		// no approve call. Its human checks never block: each open one is marked
		// later, and the landing adds them to the todo file.
		auto := e.autoLandReady(e.run.Snapshot(), card)
		if auto {
			for i := range card.HumanChecks {
				if !card.HumanChecks[i].Resolved() {
					card.HumanChecks[i].State = runrec.CheckLater
				}
			}
		}
		err := e.run.Update(func(rr *runrec.Record) { rr.Card = card; rr.State = runrec.StateAwaitingApproval; rr.Reason = "" })
		if err != nil {
			return err
		}
		if auto {
			if err := e.accept(ctx); err != nil {
				// The run waits for the owner's approve, with the reason on it.
				e.orchestratorPhase(ctx, "wait", "approval")
				return e.run.Update(func(rr *runrec.Record) { rr.Reason = "automatic approval failed: " + err.Error() })
			}
			return nil
		}
		e.orchestratorPhase(ctx, "wait", "approval")
		return nil
	}
	// FAIL: record it on the item, then back to the implementer.
	if err := e.run.Update(func(rr *runrec.Record) { rr.Fails++ }); err != nil {
		return err
	}
	rec = e.run.Snapshot()
	summary := out.text
	if out.triage != nil && out.triage.Tagged {
		summary = out.triage.Fix
	}
	e.d.Slicer.Note(ctx, e.wr, rec.Worktree, rec.Item, "Review FAIL: "+failSummary(summary))
	if rec.Fails >= 3 {
		return e.stop(ctx, fmt.Sprintf("the slice failed its gate %d times", rec.Fails))
	}
	// Only the gate's fix findings go back; its owner and later findings
	// were kept, or asked, when the report was read.
	file := out.file
	if out.triage != nil && out.triage.Tagged {
		file = fmt.Sprintf("gate-%d-fix.md", rec.Round)
		if err := os.WriteFile(filepath.Join(e.run.Dir(), file), []byte(out.triage.Fix), 0o600); err != nil {
			return e.pause(runrec.Pending{Kind: runrec.PendingError, State: runrec.StateGate, Reason: "gate fix file: " + err.Error()})
		}
	}
	return e.bumpRound(ctx, runrec.Feedback{Kind: "fail", File: file})
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
// slice's Git section, falling back to "ID: title", and no attribution
// (git.commit). The run stops here for stop_after commit; otherwise landing follows.
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
	title, err := sliceCommitTitle(detail)
	if err != nil {
		return pauseWith("the slice's Git section: " + err.Error())
	}
	// A landing that a fix round interrupted commits again after the next
	// approval; when the tree is already clean there is nothing new to commit.
	dirty := true
	if rec.Commit != nil {
		status, err := e.treeStatus(ctx, rec.Worktree)
		if err != nil {
			return pauseWith(err.Error())
		}
		dirty = strings.TrimSpace(status) != ""
	}
	if dirty {
		if _, err := e.gitWrite(ctx, rec.Worktree, "add", "-A"); err != nil {
			return pauseWith("git add: " + err.Error())
		}
		if _, err := e.gitWrite(ctx, rec.Worktree, "commit", "-m", title); err != nil {
			// finish already closed the ledger with PASS.
			return e.run.Update(func(r *runrec.Record) { markStopped(r, "git commit: "+err.Error()) })
		}
	}
	sha, err := e.gitRead(ctx, rec.Worktree, "rev-parse", "HEAD")
	if err != nil {
		return pauseWith("git rev-parse HEAD: " + err.Error())
	}
	return e.run.Update(func(r *runrec.Record) {
		r.Pending = nil
		r.Commit = &runrec.Commit{SHA: strings.TrimSpace(sha), Title: title, At: e.d.now()}
		r.State = runrec.StateLand
		if r.StopAfter == runrec.StopAfterCommit {
			r.State = runrec.StateCommitted
		}
	})
}

// finish runs the finish step the contract orders after PASS: the implementer,
// resumed, writes the closing note and runs `slicer done` or `slicer handoff`,
// per `slicer config implement_finish`. The run ends when it is done.
func (e *Engine) finish(ctx context.Context) error {
	pend := e.takePending()
	// An owner question answered in the ledger since the card is read before
	// the closing note and the finish step (S223, proposed).
	e.syncLedgerAnswers(ctx)
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
	if section := deferredSection(rec.Checks, true); section != nil {
		r.sections = append(r.sections, *section)
	}
	out, err := e.runRound(ctx, r, pend)
	if err != nil || !out.ok {
		return err
	}
	// A finish step that follows a blocked landing finds the ledger run ended.
	first := e.run.Snapshot().Commit == nil
	if err := e.run.Update(func(rr *runrec.Record) { rr.State = runrec.StateCommit; rr.Reason = "" }); err != nil {
		return err
	}
	if first {
		e.endRelay(ctx, "pass", "done")
	}
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
