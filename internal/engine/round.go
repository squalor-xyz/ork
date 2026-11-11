// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

const heartbeatInterval = 10 * time.Minute

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
// the round stopped, paused, or scheduled an automatic retry.
type outcome struct {
	ok       bool
	text     string
	report   *Report
	findings string // the reviewer's fix findings
	triage   *Triaged
	file     string // the report file name in the run directory
}

// latestSession returns the newest session id among the roles.
func latestSession(rec runrec.Record, from []string, vendor, model string) string {
	for i := len(rec.Roles) - 1; i >= 0; i-- {
		r := rec.Roles[i]
		if r.Session != "" && contains(from, r.Role) && r.Vendor == vendor && r.Model == model {
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

// treeContent fingerprints tracked changes (including staged changes and
// binary files) and untracked names. Read-only role checks keep treeStatus.
func (e *Engine) treeContent(ctx context.Context, worktree string) (string, error) {
	diff, err := e.gitRead(ctx, worktree, "diff", "HEAD", "--binary", "--no-ext-diff")
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	names, err := e.gitRead(ctx, worktree, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", fmt.Errorf("git ls-files: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(diff+"\x00"+names))), nil
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

// nextShapePauses is the count of the shape pause that a round of role at round
// is about to make, given the context the round started from: one more than the
// shape pause or automatic retry it reruns, else 1 (S298, *proposed*). A round
// that ends clean clears the context, a round of another role or round does not
// read it, so only consecutive shape pauses of one role and round add up.
func nextShapePauses(pend *runrec.Pending, role string, round int) int {
	if pend != nil && pend.Kind == runrec.PendingShape && pend.Role == role && pend.Round == round {
		return pend.ShapePauses + 1
	}
	return 1
}

// shapeCapped reports whether a pause is a shape pause that repeated: a plain
// retry is refused there (S298, owner decision 2026-11-05).
func shapeCapped(p *runrec.Pending) bool {
	return p != nil && p.Kind == runrec.PendingShape && p.ShapePauses >= runrec.ShapePauseCap
}

// cappedShapeReason is the reason of a capped shape pause: the plain error, the
// refused report and the ways past.
func cappedShapeReason(plain, role, report, runID string, pauses int) string {
	return fmt.Sprintf("%s; the %s's report %s was refused %d times in a row; read it with ork show %s; ork retry %s --accept-shape keeps an owner finding that has no options as it is, ork retry %s --again runs the %s once more, ork stop %s stops the run",
		plain, role, report, pauses, runID, runID, runID, role, runID)
}

// cappedRetryRefusal is the one line that refuses a plain retry at the cap.
func cappedRetryRefusal(id string, pauses int) string {
	return fmt.Sprintf("run %s paused on report shape %d times in a row; read the report with ork show %s, then ork retry %s --accept-shape, ork retry %s --again (one more rerun) or ork stop %s",
		id, pauses, id, id, id, id)
}

// shapeFindingText cuts the offending findings to what a pause keeps.
func shapeFindingText(findings []string) string {
	text := strings.Join(findings, "\n\n")
	if len(text) > runrec.MaxShapeFindingBytes {
		text = strings.ToValidUTF8(text[:runrec.MaxShapeFindingBytes], "") + "\n(cut)"
	}
	return text
}

// pendingSection is what a resumed round tells its role about why it runs
// again. The owner's note, when a retry carried one, follows (S256, *proposed*).
func pendingSection(p *runrec.Pending) []BriefSection {
	if p == nil {
		return nil
	}
	sections := pendingKindSection(p)
	if note := strings.TrimSpace(p.Note); note != "" {
		sections = append(sections, BriefSection{Title: "Owner note", Body: note})
	}
	return sections
}

// reportAsksOwner reports whether a report asks the owner: its Owner needed
// section is not none, or a line opens an [owner] finding. It reads the text
// as it is, so it also answers for a report that failed its shape checks.
func reportAsksOwner(text string) bool {
	if body, _ := section(text, ownerHeading); !isNone(body) {
		return true
	}
	for _, line := range strings.Split(text, "\n") {
		if m := kindTag.FindStringSubmatch(strings.TrimSuffix(line, "\r")); m != nil && strings.HasPrefix(strings.ToLower(m[3]), KindOwner) {
			return true
		}
	}
	return false
}

// shapeRetryBrief is the brief of a shape retry. asked is true when the
// refused report asked the owner: the role then keeps its question (S287,
// *proposed*).
func shapeRetryBrief(reason string, asked bool) string {
	body := "ork checked your last report and refused it: " + reason + "\n\nWrite the report again so that it has the shape the prompt asks for, and exit."
	if asked {
		body += "\n\nKeep your owner question as it was; fix only the shape."
	}
	return body
}

func pendingKindSection(p *runrec.Pending) []BriefSection {
	if p.Answer != "" {
		return []BriefSection{{Title: "Owner answer", Body: fmt.Sprintf("Your report asked the owner:\n\n%s\n\nThe owner answered:\n\n%s", p.Question, p.Answer)}}
	}
	switch p.Kind {
	case runrec.PendingQuestion:
		return []BriefSection{{Title: "Owner answer", Body: fmt.Sprintf("Your report asked the owner:\n\n%s\n\nThe owner answered:\n\n%s", p.Question, p.Answer)}}
	case runrec.PendingShape:
		// The plain error: a capped pause's reason names commands for the owner.
		reason := p.ShapeError
		if reason == "" {
			reason = p.Reason
		}
		body := shapeRetryBrief(reason, p.Asked)
		if p.ShapeFinding != "" {
			// The role may not resume its session, and then sees only this.
			fence := fenceFor(p.ShapeFinding)
			body += "\n\nThe lines that failed the check, as your report wrote them:\n\n" + fence + "\n" + p.ShapeFinding + "\n" + fence
		}
		return []BriefSection{{Title: "Report shape", Body: body}}
	case runrec.PendingLost:
		return []BriefSection{{Title: "Earlier process lost", Body: "The process that ran this role before was lost (" + p.Reason + "). The worktree may hold its partial work. Look at it, then continue from it."}}
	}
	return nil
}

// overBudget compares recorded role usage with the run's limits. Only role
// run time counts; time between roles or spent paused does not.
func (e *Engine) overBudget(rec runrec.Record) (string, bool) {
	var tokens int
	var elapsed time.Duration
	for _, role := range rec.Roles {
		tokens += role.Tokens
		if rec.Budgets.Minutes > 0 {
			end := role.EndedAt
			if role.Running() {
				end = e.d.now()
			}
			elapsed += end.Sub(role.StartedAt)
		}
	}
	if rec.Budgets.Tokens > 0 && tokens >= rec.Budgets.Tokens {
		return fmt.Sprintf("token budget %d reached (%d used)", rec.Budgets.Tokens, tokens), true
	}
	if rec.Budgets.Minutes > 0 && elapsed >= time.Duration(rec.Budgets.Minutes)*time.Minute {
		return fmt.Sprintf("time budget %dm reached (%s role run time used)", rec.Budgets.Minutes, elapsed.Round(time.Second)), true
	}
	return "", false
}

// runRound runs one role round: brief, ledger step, process, stop rules,
// report checks. When it returns ok false the slice stopped, paused, or has
// an automatic retry ready (or err says why not).
func (e *Engine) runRound(ctx context.Context, r round, pend *runrec.Pending) (outcome, error) {
	rec := e.run.Snapshot()
	// A sibling can own the single pending slot. The latest role record keeps
	// this role's automatic retry durable until its next attempt ends.
	if pend == nil {
		for i := len(rec.Roles) - 1; i >= 0; i-- {
			old := rec.Roles[i]
			if old.Role == r.role && old.Round == r.n {
				if old.ShapeRetry != nil {
					p := *old.ShapeRetry
					pend = &p
				}
				break
			}
		}
	}
	// The owner kept the refused report as it is (S298, *proposed*): no role
	// runs, so no budget, slot or lineup row is needed.
	if pend != nil && pend.AcceptShape && pend.Kind == runrec.PendingShape && pend.Role == r.role && pend.Round == r.n {
		return e.acceptShape(ctx, r)
	}
	// A retry of a budget pause grants this round. Its pending context is
	// cleared when the round ends, so the next round checks the limits again.
	if pend == nil || pend.Kind != runrec.PendingBudget {
		if why, over := e.overBudget(rec); over {
			return outcome{}, e.pause(runrec.Pending{Kind: runrec.PendingBudget, State: r.state, Role: r.role, Round: r.n, Reason: why})
		}
	}
	entries := rec.Lineup[r.role]
	if r.role == RoleGate && (len(entries) == 0 || slices.Equal(entries, rec.Lineup[RoleReviewer])) {
		entries = rec.Lineup[RoleReviewer]
	}
	entryIndex := currentEntry(rec, r.role)
	if entryIndex >= len(entries) {
		return outcome{}, e.stop(ctx, fallbackReasons(rec, r.role))
	}
	// The vendor's slot (S169). A vendor that is out is not tried while a later
	// row is on another vendor; the role's records then match that later row
	// going forward. The row is picked before the wait and again after a wait:
	// the vendor may have gone out meanwhile (the holder of the slot hit a
	// limit, or the owner marked it), and then the round takes the other row
	// and frees the slot it got. The wait is before the brief, the ledger step
	// and the role record, so it is neither role run time nor a silent role.
	// The slot is freed when the vendor process ends; a cancelled wait leaves
	// no record.
	first := entryIndex
	var entry runrec.LineupEntry
	var release func()
	waitStep := rec.OrchestratorStep
	if rec.RelayRun == "" || rec.Commit != nil {
		waitStep = ""
	}
	stopWait := e.heartbeat(ctx, rec, waitStep)
	for {
		entryIndex = e.skipVendorsOut(entries, first)
		entry = entries[entryIndex]
		rel, waited, err := e.acquireRoleSlot(ctx, entry.Vendor, func() { e.vendorWait.add(entry.Vendor, 1) })
		if waited {
			e.vendorWait.add(entry.Vendor, -1)
		}
		if err != nil {
			stopWait()
			return outcome{}, err
		}
		if waited && e.skipVendorsOut(entries, first) != entryIndex {
			rel()
			continue
		}
		release = rel
		break
	}
	stopWait()
	defer release()
	// A connection attempt stays on its row. Its history bounds the resume
	// to one per row and round, including when a report later needs a retry.
	connectionRetried := false
	for i := len(rec.Roles) - 1; i >= max(rec.ChainFrom, 0); i-- {
		old := rec.Roles[i]
		if old.Role != r.role {
			continue
		}
		if old.Round != r.n || fallbackExit(ExitClass(old.Exit)) || old.Vendor != entry.Vendor || old.Model != entry.Model || old.Effort != entry.Effort {
			break
		}
		if old.Exit == string(ExitConnection) {
			connectionRetried = true
		}
	}
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
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return fail("temp directory", err)
	}
	if r.role == RoleUI {
		// A rerun starts with no renders: an earlier attempt's files are not
		// this attempt's views (S121, *proposed*).
		if err := os.RemoveAll(filepath.Join(tempDir, rendersDir)); err != nil {
			return fail("old renders", err)
		}
	}
	if err := os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail("old report", err)
	}

	allowed := r.resumeFrom
	if connectionRetried {
		allowed = append([]string{r.role}, allowed...)
	}
	if pend != nil && (pend.Kind == runrec.PendingQuestion || pend.Kind == runrec.PendingShape) {
		allowed = append([]string{r.role}, allowed...)
	}
	session := latestSession(rec, allowed, entry.Vendor, entry.Model)

	sliceJSON, err := e.d.Slicer.ShowRaw(ctx, rec.Worktree, rec.Item)
	if err != nil {
		return fail("slicer show", err)
	}
	in := BriefInput{
		Role: r.role, SliceID: rec.Item, Worktree: rec.Worktree, TempDir: tempDir, ReportPath: reportPath,
		SliceJSON: string(sliceJSON), EnvLines: e.proj.EnvironmentLines,
		CheckCommand: e.proj.CheckCommand,
		Allow:        perm.Allow,
		UIDoc:        e.proj.UIDoc,
		Vendor:       entry.Vendor,
		UIPassSkills: e.proj.UIPassSkills,
		Finish:       r.finish,
		Sections:     append(pendingSection(pend), r.sections...),
	}
	if r.role == RoleGate {
		implementerPerm, err := ResolvePermission(e.proj, RoleImplementer)
		if err != nil {
			return fail("implementer permissions", err)
		}
		in.Allow = implementerPerm.Allow
		body := "No round check result is available. Do not claim tests green without evidence."
		if len(rec.Checks) > 0 {
			check := rec.Checks[len(rec.Checks)-1]
			log, err := ReadReport(e.run.Dir(), check.Log)
			if err != nil {
				return fail("gate check evidence", err)
			}
			status := "nonzero (failed; see log for exit code or timeout)"
			if check.OK {
				status = "0"
			}
			fence := fenceFor(log)
			body = fmt.Sprintf("Round: %d\nCommand: %s\nCheck log: %s\nExit status: %s\n\n%s\n%s\n%s", check.Round, check.Command, check.Log, status, fence, log, fence)
		}
		in.Sections = append(in.Sections, BriefSection{Title: "Ork round check result", Body: body})
	}
	if entryIndex > 0 {
		if section := fallbackSection(rec, r.role, entries[:entryIndex]); section.Body != "" {
			in.Sections = append(in.Sections, section)
		}
	}
	for _, q := range rec.Questions {
		if q.Answer != "" && q.Role == r.role && (pend == nil || q.Text != pend.Question) {
			in.Sections = append(in.Sections, BriefSection{Title: "Owner answer", Body: q.Text + "\n\nOwner answered: " + q.Answer})
		}
	}
	if rec.Decisions != "" {
		// The worktree's spec lacks the owner's spec check answers (S116).
		in.Sections = append([]BriefSection{{Title: "Owner decisions", Body: rec.Decisions}}, in.Sections...)
	}
	if session == "" {
		in.Earlier = e.earlierReports(rec)
	}
	in.ReadOnly = perm.ReadOnly
	in.Resumed = session != ""
	// One git call serves the resumed section and the read-only fingerprint.
	var before string
	if perm.ReadOnly || in.Resumed {
		if before, err = e.treeStatus(ctx, rec.Worktree); err != nil {
			if perm.ReadOnly {
				return fail("read-only fingerprint", err)
			}
			return fail("resumed session tree", err)
		}
		in.Tree = before
	}
	brief, err := BuildBrief(in)
	if err != nil {
		return fail("brief", err)
	}

	stepID := e.stepStart(ctx, rec, r, entry)
	stop := e.heartbeat(ctx, rec, stepID)
	defer stop()
	idx := -1
	err = e.run.Update(func(rr *runrec.Record) {
		// Only the attempt that runs as the automatic retry carries its context;
		// the refused attempt stays as history without the label.
		retry := automaticShapeRetry(pend)
		if retry != nil {
			for i := range rr.Roles {
				if rr.Roles[i].Role == r.role && rr.Roles[i].Round == r.n {
					rr.Roles[i].ShapeRetry = nil
				}
			}
		}
		rr.Roles = append(rr.Roles, runrec.RoleRecord{
			Role: r.role, Round: r.n, Vendor: entry.Vendor, Model: entry.Model, Effort: entry.Effort,
			ShapeRetry: retry,
			Step:       stepID, StartedAt: e.d.now(),
		})
		idx = len(rr.Roles) - 1
	})
	if err != nil {
		return outcome{}, err
	}

	onEvent, endActivity := e.startActivity(r.role)
	defer endActivity()
	onLive := e.liveHook(r.role)
	// The watchdog (S182) ends runCtx when the role has been silent for too
	// long; the adapters kill the role's process group when their context ends.
	runCtx, kill := context.WithCancel(ctx)
	defer kill()
	watch, stopWatch, hung := e.watchRole(runCtx, kill, entry.Vendor)
	defer stopWatch()
	endWork := e.beginWork(fmt.Sprintf("%s round %d", r.role, r.n), watch)
	defer endWork()
	res, runErr := e.d.Runner.Run(runCtx, Request{
		Role: r.role, Round: r.n, Vendor: entry.Vendor, Model: entry.Model, Effort: entry.Effort,
		Brief: brief, Worktree: rec.Worktree, TempDir: tempDir, ReportPath: reportPath,
		Allowlist: perm.Allow, ReadOnly: perm.ReadOnly, Resume: session,
		OnEvent: func() { watch.touch(); onEvent() },
		OnLive:  func(l Live) { watch.touch(); onLive(l) },
		OnStart: func(pgid int) { e.run.Update(func(rr *runrec.Record) { rr.Roles[idx].PGID = pgid }) },
	})
	stopWatch()
	if err := ctx.Err(); err != nil {
		release()
		if e.run.Snapshot().State == runrec.StateStopped {
			updateErr := e.run.Update(func(rr *runrec.Record) {
				role := &rr.Roles[idx]
				role.EndedAt = e.d.now()
				role.Exit = "cancelled"
				role.Session = res.Session
				role.Tokens = res.Tokens
				role.CostUSD = res.CostUSD
			})
			return outcome{}, updateErr
		}
		return outcome{}, err // the role stays running in the record; a restart marks it lost
	}
	// A runner without an error finished by itself; its result is real even
	// when it returned in the tick the watchdog fired.
	if hung() && runErr != nil && !IsLocalVendor(entry.Vendor) {
		release()
		limit, _ := e.d.hungLimit()
		return e.endHungRound(ctx, rec, r, idx, stepID, res, limit, stop)
	}
	if hung() && runErr != nil && IsLocalVendor(entry.Vendor) {
		res.Reason = "local role exceeded " + formatLimit(LocalRoleAfter)
		runErr = fmt.Errorf("%s: %w", res.Reason, runErr)
	}
	class := res.Exit
	switch {
	case runErr != nil:
		class = ExitError
	case class == "":
		class = ExitError
		runErr = errors.New("the runner gave no exit class")
	}

	// A rate limit or out of credits ends the vendor's turn for a while: the
	// next rounds, of this run and of others, skip its rows (S169). An error
	// exit does not, and neither does a hung role (it returned above). The mark
	// comes before the slot is freed, so that a round that waits for the slot
	// finds the vendor out when it gets it. The vendor process has ended, so
	// the slot is freed here.
	if class == ExitRateLimited || class == ExitOutOfCredits {
		reason := strings.TrimSpace(res.Reason)
		if reason == "" {
			reason = string(class)
		}
		e.d.Vendors.MarkOut(entry.Vendor, reason, e.d.now())
	}
	release()

	var changed bool
	var statusErr error
	if perm.ReadOnly {
		var after string
		if after, statusErr = e.treeStatus(ctx, rec.Worktree); statusErr == nil {
			changed = after != before
		}
	}

	connectionFailure := class == ExitConnection
	// Once the resume is spent, use error only when another row exists.
	// A last-row connection stays retryable after the owner resumes it.
	if connectionFailure && (connectionRetried || res.Session == "") && nextEntry(entries, entryIndex, ExitError) < len(entries) {
		class = ExitError
	}
	resumeConnection := connectionFailure && !connectionRetried && res.Session != "" && len(res.Denials) == 0 && !changed && statusErr == nil

	text, file := "", ""
	if class == ExitOK {
		text, file = e.readReport(reportPath, reportName, res.Report)
	}
	fallback := fallbackExit(class)
	fallingBack := fallback && len(res.Denials) == 0 && !changed && statusErr == nil && nextEntry(entries, entryIndex, class) < len(entries)
	err = e.run.Update(func(rr *runrec.Record) {
		// A sibling round of the same state may have paused the slice already;
		// its pause context is not this round's to clear.
		if !fallingBack && rr.State == r.state && (rr.Pending == nil || rr.Pending.Role == "" || rr.Pending.Role == r.role) {
			rr.Pending = nil
		}
		role := &rr.Roles[idx]
		role.EndedAt = e.d.now()
		role.Exit = string(class)
		role.Session = res.Session
		role.Tokens = res.Tokens
		role.CostUSD = res.CostUSD
		role.Denials = denialNames(res.Denials)
		role.DeniedCommands = res.Denials
		role.Report = file
		if role.ShapeRetry != nil {
			role.ShapeRetry.AutomaticShapeRetry = false
		}
	})
	if err != nil {
		return outcome{}, err
	}
	endActivity()

	if resumeConnection {
		stop()
		kill()
		endWork()
		e.stepFail(ctx, rec, stepID, class, res.Reason, runErr)
		if e.d.Sleep != nil {
			if err := e.d.Sleep(ctx, 30*time.Second); err != nil {
				return outcome{}, err
			}
		} else {
			timer := time.NewTimer(30 * time.Second)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return outcome{}, ctx.Err()
			case <-timer.C:
			}
		}
		return e.runRound(ctx, r, pend)
	}

	if fallingBack {
		stop()
		e.stepFail(ctx, rec, stepID, class, res.Reason, runErr)
		return outcome{}, nil
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
		if fallback {
			stopReason += "; " + fallbackReasons(e.run.Snapshot(), r.role)
		}
	case changed:
		stopReason = fmt.Sprintf("%s round %d changed the tree, and it is a read-only role", r.role, r.n)
	}
	if stopReason != "" {
		stop()
		e.stepFail(ctx, rec, stepID, class, res.Reason, runErr)
		if connectionFailure && len(res.Denials) == 0 && !changed && statusErr == nil {
			reason := strings.Join(strings.Fields(fmt.Sprintf("%s round %d (%s): connection error: %s", r.role, r.n, entry.Vendor, res.Reason)), " ")
			return outcome{}, e.pauseError(r, reason)
		}
		return outcome{}, e.stop(ctx, stopReason)
	}
	stop()
	// From relay v11 a gate round closes with its verdict, which is known only
	// once the report is read (slicer s408), and only when the engine accepts
	// the round: a round that pauses (a shape problem, an owner question, a
	// blocker finding, an error) closes without one, and its rerun is a new
	// step that carries the verdict (S204, *proposed*). Every other round, and
	// every v10 round, closes here. closeStep closes the step once: the
	// deferred call closes a held gate step on every exit that did not close
	// it with the verdict.
	closed := false
	closeStep := func(verdict string) {
		if closed {
			return
		}
		closed = true
		e.stepEnd(ctx, rec, stepID, "done", verdict)
	}
	if r.role == RoleGate && rec.Contract >= 11 {
		defer closeStep("")
	} else {
		closeStep("")
	}
	if statusErr != nil {
		return fail("read-only fingerprint", statusErr)
	}

	// The report.
	rep, tri, findings, bad, err := judgeReport(r, text, false)
	if err != nil {
		return outcome{}, err
	}
	// A report with a shape error has no verdict to record.
	verdict := ""
	if rep != nil {
		verdict = strings.ToLower(rep.Verdict)
	}
	if bad != nil {
		plain := bad.Error()
		p := runrec.Pending{
			Kind: runrec.PendingShape, State: r.state, Role: r.role, Round: r.n,
			Reason: plain, ShapeError: plain, ShapeFinding: shapeFindingText(bad.Findings),
			Asked: reportAsksOwner(text),
		}
		// A report that also asks the owner is retried too (S287, *proposed*):
		// the retry brief tells the role to keep its question. Only the one
		// automatic retry per role and round runs.
		if !(pend != nil && pend.Kind == runrec.PendingShape && pend.Role == r.role && pend.Round == r.n) {
			p.AutomaticShapeRetry = true
			return outcome{}, e.run.Update(func(rr *runrec.Record) {
				rr.Roles[idx].ShapeRetry = &p
				if rr.State == r.state && (rr.Pending == nil || rr.Pending.Role == r.role) {
					rr.Pending = &p
				}
			})
		}
		// A pause: count it, and at the cap name the refused report and the
		// ways past (S298, *proposed*).
		p.ShapePauses = nextShapePauses(pend, r.role, r.n)
		if shapeCapped(&p) {
			p.Reason = cappedShapeReason(plain, r.role, file, rec.ID, p.ShapePauses)
		}
		return outcome{}, e.pauseShape(p)
	}
	out, err := e.routeReport(r, rep, tri, findings, text, file)
	if err != nil || !out.ok {
		return out, err
	}
	closeStep(verdict) // a no-op for a round that closed above
	return out, nil
}

// judgeReport checks a role's report: the shared shape checks, then, for the
// reviewer, UI pass and gate, the findings. A report that breaks the shape comes
// back as bad, with every problem and the lines of each finding that failed.
// With lenient set (the owner accepted the report, S298, *proposed*) an owner
// finding whose options are not right is kept as a plain finding.
func judgeReport(r round, text string, lenient bool) (rep *Report, tri *Triaged, findings string, bad *ShapeError, err error) {
	rep, err = CheckReport(r.role, r.finish, text)
	var problems, shown []string
	var se *ShapeError
	if errors.As(err, &se) {
		problems = se.Problems
	} else if err != nil {
		return nil, nil, "", nil, err
	}
	if r.role == RoleReviewer || r.role == RoleUI || r.role == RoleGate {
		var ferr error
		if r.role == RoleGate {
			tri, ferr = triageGate(text, lenient)
		} else {
			tri, ferr = triage(text, lenient)
		}
		if ferr != nil {
			var fse *ShapeError
			if errors.As(ferr, &fse) {
				problems = append(problems, fse.Problems...)
				shown = fse.Findings
			}
		} else if r.role != RoleGate {
			findings = tri.Fix
		}
	}
	if len(problems) > 0 {
		return rep, tri, findings, &ShapeError{Problems: problems, Findings: shown}, nil
	}
	return rep, tri, findings, nil, nil
}

// routeReport acts on a report that passed its checks: an owner question or a
// blocker finding pauses the slice, other findings are kept. The outcome is ok
// when the round goes on.
func (e *Engine) routeReport(r round, rep *Report, tri *Triaged, findings, text, file string) (outcome, error) {
	if rep.Question != "" {
		err := e.run.Update(func(rr *runrec.Record) {
			rr.Questions = append(rr.Questions, runrec.Question{Ask: rep.Ask, Role: r.role, Round: r.n, Text: rep.Question, Key: rep.Key, At: e.d.now()})
		})
		if err != nil {
			return outcome{}, err
		}
		return outcome{}, e.pause(runrec.Pending{
			Kind: runrec.PendingQuestion, State: r.state, Role: r.role, Round: r.n,
			Reason:   fmt.Sprintf("%s round %d needs the owner", r.role, r.n),
			Question: rep.Question, Ask: rep.Ask, Key: rep.Key,
		})
	}
	if tri != nil {
		if err := e.recordFindings(r, tri); err != nil {
			return outcome{}, err
		}
		// An owner finding blocks the gate only when its report marks it.
		if tri.Blocker() != nil {
			for _, f := range tri.Owner {
				if !f.Blocker {
					continue
				}
				for _, q := range e.run.Snapshot().Questions {
					if q.Answer != "" && q.Ask != nil && findingKey(KindOwner, q.Ask.Title) == findingKey(KindOwner, f.Title) {
						return outcome{}, e.pauseError(r, "answered blocker repeated: "+f.Title)
					}
				}
			}
			for _, f := range tri.Owner {
				if f.Blocker {
					if err := e.askBlocker(r, f); err != nil {
						return outcome{}, err
					}
				}
			}
			return outcome{}, nil
		}
	}
	return outcome{ok: true, text: text, report: rep, findings: findings, triage: tri, file: file}, nil
}

// acceptShape is a round that the owner ended with ork retry --accept-shape
// (S298, *proposed*): the refused report is read again with the lenient rule
// for owner findings, and goes on as if the role had given it. No role runs. A
// v11 gate round opens its own ledger step, on the lineup row of the refused
// attempt, and closes it with the verdict once the engine accepts the round (the
// refused attempt's step closed without one, S204).
func (e *Engine) acceptShape(ctx context.Context, r round) (outcome, error) {
	rec := e.run.Snapshot()
	text, file, err := e.refusedReport(rec, r.role, r.n)
	if err != nil {
		return outcome{}, e.pauseError(r, "accept-shape: "+err.Error())
	}
	rep, tri, findings, bad, err := judgeReport(r, text, true)
	if err == nil && bad != nil {
		err = bad
	}
	if err != nil {
		return outcome{}, e.pauseError(r, "accept-shape: "+err.Error())
	}
	var entry runrec.LineupEntry
	for i := len(rec.Roles) - 1; i >= 0; i-- {
		if old := rec.Roles[i]; old.Role == r.role && old.Round == r.n {
			entry = runrec.LineupEntry{Vendor: old.Vendor, Model: old.Model, Effort: old.Effort}
			break
		}
	}
	// The verdict goes only with a round the engine accepts; every other exit
	// closes the step without one.
	verdict := ""
	if r.role == RoleGate && rec.Contract >= 11 {
		if stepID := e.stepStart(ctx, rec, r, entry); stepID != "" {
			defer func() { e.stepEnd(ctx, rec, stepID, "done", verdict) }()
		}
	}
	err = e.run.Update(func(rr *runrec.Record) {
		for i := len(rr.Roles) - 1; i >= 0; i-- {
			if rr.Roles[i].Role == r.role && rr.Roles[i].Round == r.n {
				rr.Roles[i].ShapeAccepted = true
				rr.Roles[i].ShapeRetry = nil // the counted context is spent
				break
			}
		}
		if rr.State == r.state && (rr.Pending == nil || rr.Pending.Role == "" || rr.Pending.Role == r.role) {
			rr.Pending = nil
		}
	})
	if err != nil {
		return outcome{}, err
	}
	out, err := e.routeReport(r, rep, tri, findings, text, file)
	if err == nil && out.ok {
		verdict = strings.ToLower(rep.Verdict)
	}
	return out, err
}

// refusedReport reads the newest report of the role in the round, the one the
// shape check refused.
func (e *Engine) refusedReport(rec runrec.Record, role string, round int) (text, file string, err error) {
	for i := len(rec.Roles) - 1; i >= 0; i-- {
		if old := rec.Roles[i]; old.Role == role && old.Round == round {
			file = old.Report
			break
		}
	}
	if file == "" {
		return "", "", errors.New("the refused report is not in the run record")
	}
	b, err := os.ReadFile(filepath.Join(e.run.Dir(), file))
	if err != nil {
		return "", "", fmt.Errorf("the refused report cannot be read: %w", err)
	}
	return string(b), file, nil
}

// acceptShapeCheck says whether the report the paused role gave can be
// accepted: only the missing options of an owner finding of a reviewer, UI
// pass or gate can. It returns a refusal otherwise.
func (e *Engine) acceptShapeCheck(rec runrec.Record) error {
	const only = "only an owner finding's missing options can be accepted"
	p := rec.Pending
	if p.Role != RoleReviewer && p.Role != RoleUI && p.Role != RoleGate {
		return refusef("%s; the %s's report has no findings", only, p.Role)
	}
	text, _, err := e.refusedReport(rec, p.Role, p.Round)
	if err != nil {
		return refusef("%s: %v", only, err)
	}
	_, _, _, bad, err := judgeReport(round{role: p.Role, n: p.Round}, text, true)
	if err != nil {
		return refusef("%s: %v", only, err)
	}
	if bad != nil {
		return refusef("%s; the report also has: %s", only, oneLine(strings.Join(bad.Problems, "; ")))
	}
	return nil
}

// fallbackSection gives the next row the failed attempts of earlier rows of
// this role, including attempts with identical lineup entries.
func fallbackSection(rec runrec.Record, role string, earlier []runrec.LineupEntry) BriefSection {
	var history runrec.Record
	for _, record := range rec.Roles {
		if record.Role != role {
			continue
		}
		for _, entry := range earlier {
			if record.Vendor == entry.Vendor && record.Model == entry.Model && record.Effort == entry.Effort {
				history.Roles = append(history.Roles, record)
				break
			}
		}
	}
	reasons := fallbackReasons(history, role)
	if reasons == "" {
		return BriefSection{}
	}
	return BriefSection{
		Title: "Earlier lineup rows",
		Body:  reasons + ". The worktree may hold partial work. Check `git status` before continuing.",
	}
}

// fallbackReasons preserves the exit and row identity of each failed attempt.
func fallbackReasons(rec runrec.Record, role string) string {
	var reasons []string
	for _, r := range rec.Roles {
		if r.Role == role && (fallbackExit(ExitClass(r.Exit))) {
			reasons = append(reasons, fmt.Sprintf("%s round %d (%s %s %s) exited %s", role, r.Round, r.Vendor, r.Model, r.Effort, r.Exit))
		}
	}
	return strings.Join(reasons, "; ")
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
	err := os.WriteFile(path, []byte(fallback), 0o600)
	if err == nil {
		err = os.Chmod(path, 0o600)
	}
	if err != nil {
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
// call fails. Once the first finish step has ended the ledger run (the run is
// committed), a landing fix round records no steps: relay-run refuses them with
// run_ended, and the engine run record keeps the rounds (S153).
func (e *Engine) stepStart(ctx context.Context, rec runrec.Record, r round, entry runrec.LineupEntry) string {
	if rec.RelayRun == "" || rec.Commit != nil {
		return ""
	}
	role := rec.LedgerRoles[r.role]
	if role == "" {
		role = r.role
	} // records created before the contract snapshot
	step, err := e.d.Slicer.RelayStep(ctx, e.wr, rec.Worktree, rec.RelayRun, slicercli.StepSpec{
		Role: role, Agent: entry.Vendor, Vendor: entry.Vendor, Model: entry.Model, Reasoning: entry.Effort,
		Parent: rec.OrchestratorStep, Phase: "start", Task: r.task,
	})
	if err != nil {
		return ""
	}
	return step.ID
}

// stepEnd sets the phase of a ledger step when its round ends. verdict is
// "pass", "fail" or "": a v11 gate round closes with its verdict.
func (e *Engine) stepEnd(ctx context.Context, rec runrec.Record, id, phase, verdict string) {
	if id == "" || rec.RelayRun == "" {
		return
	}
	e.d.Slicer.RelayStep(ctx, e.wr, rec.Worktree, rec.RelayRun, slicercli.StepSpec{Step: id, Phase: phase, Verdict: verdict})
}

// heartbeat refreshes a running ledger step. Recording is advisory.
// stop cancels in-flight writes and waits before the step may end.
func (e *Engine) heartbeat(ctx context.Context, rec runrec.Record, id string) func() {
	if id == "" {
		return func() {}
	}
	every := e.heartbeatEvery
	if every == 0 {
		every = heartbeatInterval
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				e.d.Slicer.RelayStep(ctx, e.wr, rec.Worktree, rec.RelayRun, slicercli.StepSpec{Step: id, Phase: "start"})
			}
		}
	}()
	return func() { cancel(); <-done }
}

// automaticShapeRetry copies the active context so ending a role cannot change
// the brief's pending context or another role record.
func automaticShapeRetry(p *runrec.Pending) *runrec.Pending {
	if p == nil || !p.AutomaticShapeRetry {
		return nil
	}
	copy := *p
	return &copy
}
