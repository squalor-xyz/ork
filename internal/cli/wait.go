// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// RunEventStates is the state list that ork wait takes in --until, and that
// the notify command of S311 shares (S257, *proposed*; adj 202,
// cli.run-event-states). A question is a pause on an owner question, and
// landed is a committed run that ork landed. It has no failed: a run record and
// a run.show reply do not say whether a ledger run ended with outcome fail
// (adj 247, cli.wait-failed-state). S311 can add it later.
var RunEventStates = []string{"question", "awaiting-approval", "paused", "stopped", "landed"}

const defaultWaitPoll = 30 * time.Second

// wait blocks until one of the runs it follows changes state and prints the
// new state (S257, *proposed*; several runs and --stage, S403, *proposed*). It
// subscribes, reads run.show of each followed run at once, and reads them again
// on each notice and every WaitPoll, because the daemon sends a notice for a
// cached part and for a record change that a waiter must learn of, and not for
// every record change. It only reads.
func (a *App) wait(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	untilFlag := fs.String("until", "", "stop when a run enters one of these states (comma separated)")
	timeout := fs.Duration("timeout", 0, "give up after this long; 0 waits for ever")
	project := fs.String("project", "", "the project whose unfinished runs to follow when RUN is omitted")
	stage := fs.Bool("stage", false, "also stop when a role round of a followed run ends")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("wait: %v", err)
	}
	w := &waiter{app: a, o: o, project: *project, stage: *stage, poll: a.WaitPoll, parent: ctx}
	if w.poll <= 0 {
		w.poll = defaultWaitPoll
	}
	for _, id := range pos {
		if slices.ContainsFunc(w.runs, func(f *followed) bool { return f.id == id }) {
			return o.usage("wait: run %s is named twice", id)
		}
		w.runs = append(w.runs, &followed{id: id})
	}
	w.several = len(w.runs) > 1
	untilGiven := false
	fs.Visit(func(f *flag.Flag) { untilGiven = untilGiven || f.Name == "until" })
	if untilGiven {
		if w.until, err = parseUntil(*untilFlag); err != nil {
			return o.usage("wait: %v", err)
		}
	}
	if *timeout < 0 {
		return o.usage("wait: --timeout %v is negative", *timeout)
	}
	w.timeout = *timeout
	waitCtx := ctx
	if *timeout > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	for {
		code, retry := w.connection(waitCtx)
		if !retry {
			return code
		}
		if w.retried {
			return o.fail(ExitFailure, codeDaemon, fmt.Sprintf("the daemon connection dropped again before a read: %v", w.dropped))
		}
		w.retried = true
	}
}

// parseUntil reads the --until list. Every error names the valid states.
func parseUntil(list string) ([]string, error) {
	valid := "valid states: " + strings.Join(RunEventStates, ", ")
	var states []string
	for _, s := range strings.Split(list, ",") {
		s = strings.TrimSpace(s)
		switch {
		case s == "":
			return nil, fmt.Errorf("--until %q has an empty state; %s", list, valid)
		case !slices.Contains(RunEventStates, s):
			return nil, fmt.Errorf("--until has unknown state %q; %s", s, valid)
		}
		states = append(states, s)
	}
	return states, nil
}

// waiter is one ork wait: what it waits for and what it has seen.
type waiter struct {
	app     *App
	o       *output
	project string
	runs    []*followed // the runs followed, in the order given; empty until resolved from the project
	several bool        // more than one run is followed: print one line per run
	stage   bool        // a role round that ends is a stop
	until   []string
	poll    time.Duration
	timeout time.Duration
	parent  context.Context // the caller's context, without the --timeout deadline

	retried bool  // the connection dropped and a retry is under way
	dropped error // why the connection dropped
}

// followed is one run of a wait and what the wait has seen of it.
type followed struct {
	id          string
	base        string            // the state name of the first read
	hasBase     bool              // base is set
	ended       int               // the role rounds with an end at the last read
	last        *daemon.ShowReply // the newest read
	gone        bool              // final, or unknown, and matching nothing: no longer read
	goneReason  string            // the state name it was dropped in, or "unknown"
	verdict     verdict           // why the wait stops on it, once it does
	stageReason string            // "ROLE round N ended EXIT" for a stage end
	slotWait    bool              // the status call says the run waits for a slot (adj 613)
}

// verdict is what a read says of a followed run.
type verdict int

const (
	verdictNone     verdict = iota // nothing to report; read again
	verdictQuestion                // paused on an owner question
	verdictChanged                 // entered an --until state, or changed state
	verdictStage                   // a role round ended (--stage)
	verdictFinal                   // final and matched nothing: nothing more to do
)

// name is the shared-list name of the run's state: a committed run that ork
// landed reads landed.
func waitName(r daemon.ShowReply) string {
	if r.State == runrec.StateCommitted && r.Landed {
		return "landed"
	}
	return r.State
}

func questionPending(r daemon.ShowReply) bool {
	return r.State == runrec.StatePaused && r.Pending != nil && r.Pending.Kind == runrec.PendingQuestion
}

// transport reports whether err is the connection failing, as opposed to the
// daemon answering with an error.
func transport(err error) bool {
	var reply *ipc.Error
	return !errors.As(err, &reply)
}

// connection serves one daemon connection until the wait ends. It returns
// retry when the connection dropped and nothing was printed.
func (w *waiter) connection(ctx context.Context) (code int, retry bool) {
	o := w.o
	c, err := w.app.connect(ctx)
	if err != nil {
		return w.ended(ctx, func() int { return o.fail(ExitFailure, codeDaemon, err.Error()) }), false
	}
	wake := make(chan struct{}, 1)
	dead := make(chan error, 1)
	noticeCtx, stopNotices := context.WithCancel(ctx)
	var notices sync.WaitGroup
	defer func() {
		stopNotices()
		c.Close()
		notices.Wait()
	}()
	drop := func(err error) (int, bool) { return w.drop(ctx, err) }
	if _, err := w.app.CheckDaemonBuild(ctx, c, o.stderr); err != nil {
		return drop(err)
	}
	projects, err := c.Projects(ctx)
	if err != nil {
		return drop(err)
	}
	names := make([]string, len(projects))
	for i, p := range projects {
		names[i] = p.Name
	}
	if len(names) > 0 {
		if err := c.Subscribe(ctx, names); err != nil {
			return drop(err)
		}
	}
	if len(w.runs) == 0 {
		ids, code, retry := w.resolve(ctx, c, projects)
		if len(ids) == 0 {
			return code, retry
		}
		for _, id := range ids {
			w.runs = append(w.runs, &followed{id: id})
		}
		w.several = len(w.runs) > 1
	}
	// A notice that names runs, none of them followed, changes nothing the wait
	// reads. A notice that names none (a cached part of a project) wakes it, as
	// does any notice from a daemon that does not name runs yet (adj 451).
	ids := make([]string, len(w.runs))
	for i, f := range w.runs {
		ids[i] = f.id
	}
	notices.Add(1)
	go func() {
		defer notices.Done()
		for {
			n, err := c.Next(noticeCtx)
			if err != nil {
				dead <- err
				return
			}
			if len(n.Runs) > 0 && !slices.ContainsFunc(n.Runs, func(id string) bool { return slices.Contains(ids, id) }) {
				continue
			}
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}()
	tick := time.NewTicker(w.poll)
	defer tick.Stop()
	for {
		verdicts := make([]verdict, len(w.runs))
		for i, f := range w.runs {
			if f.gone {
				continue
			}
			reply, err := c.Show(ctx, f.id)
			if err != nil {
				if e := ipc.ErrorOf(err); ctx.Err() == nil && !transport(err) && e.Code == daemon.CodeNotFound {
					if !w.several {
						return o.fail(ExitWaitNothingActive, "nothing_active", fmt.Sprintf("no run %s: it is unknown to the daemon, so nothing is active", f.id)), false
					}
					f.goneReason = "unknown"
					verdicts[i] = verdictFinal
					continue
				}
				return drop(err)
			}
			w.retried = false // a read worked on this connection
			f.slotWait = false
			if questionPending(reply) {
				f.slotWait, err = w.slotWaiting(ctx, c, f.id)
				if err != nil {
					return drop(err)
				}
			}
			verdicts[i] = w.judge(f, reply)
		}
		if code, done := w.finish(verdicts); done {
			return code, false
		}
		select {
		case <-wake:
		case <-tick.C:
		case err := <-dead:
			return drop(err)
		case <-ctx.Done():
			return w.ended(ctx, nil), false
		}
	}
}

// drop handles the error of a call: a lost connection is retried, and the
// context ending is the timeout or an interrupt.
func (w *waiter) drop(ctx context.Context, err error) (code int, retry bool) {
	if ctx.Err() == nil && transport(err) {
		w.dropped = err
		return 0, true
	}
	return w.ended(ctx, func() int { return w.o.failErr(err) }), false
}

// resolve picks the runs a wait without RUN follows (adj 449, cli.wait-project,
// which supersedes adj 203; cli.wait-project-scope): every unfinished run of
// --project, or of the only registered project, as of this read. A run started
// later is not followed. Several projects with no --project is a usage error.
// It returns no run when the wait ended with code, or when the connection
// dropped (retry).
func (w *waiter) resolve(ctx context.Context, c *daemon.Client, projects []registry.Project) (runs []string, code int, retry bool) {
	o := w.o
	name := w.project
	if name == "" {
		switch len(projects) {
		case 0:
			return nil, o.usage("wait needs RUN: no project is registered"), false
		case 1:
			name = projects[0].Name
		default:
			names := make([]string, len(projects))
			for i, p := range projects {
				names[i] = p.Name
			}
			return nil, o.usage("wait needs RUN or --project: %d projects are registered (%s)", len(projects), strings.Join(names, ", ")), false
		}
	}
	var res daemon.StatusResult
	if err := c.Call(ctx, daemon.MethodStatus, daemon.StatusParams{Project: name}, &res); err != nil {
		code, retry := w.drop(ctx, err)
		return nil, code, retry
	}
	var open []string
	for _, p := range res.Projects {
		if p.Name != name {
			continue
		}
		for _, r := range p.EngineRuns {
			if !runrec.IsFinal(r.State) {
				open = append(open, r.ID)
			}
		}
	}
	if len(open) == 0 {
		return nil, o.fail(ExitWaitNothingActive, "nothing_active", fmt.Sprintf("project %s has no unfinished run, so nothing is active", name)), false
	}
	return open, 0, false
}

// slotWaiting reads the status hint for a run whose question may be answered.
func (w *waiter) slotWaiting(ctx context.Context, c *daemon.Client, id string) (bool, error) {
	var res daemon.StatusResult
	if err := c.Call(ctx, daemon.MethodStatus, daemon.StatusParams{Project: w.project}, &res); err != nil {
		return false, err
	}
	for _, p := range res.Projects {
		for _, r := range p.EngineRuns {
			if r.ID == id && r.ResumeWaiting {
				return true, nil
			}
		}
	}
	return false, nil
}

// judge says what a read of a followed run means for the wait. A pending
// question ends it unless status says the run only waits for a slot. Then the
// --until list; with no list, a change of state from the first read. With
// --stage, a role round that ended since the last read. A final run that
// matched none of those has nothing more to do.
func (w *waiter) judge(f *followed, r daemon.ShowReply) verdict {
	name := waitName(r)
	first := !f.hasBase
	if first {
		f.base, f.hasBase = name, true
	}
	f.last = &r
	ended, latest := endedRounds(r)
	grew := ended > f.ended
	f.ended = ended
	switch {
	case f.slotWait && questionPending(r):
		return verdictNone
	case questionPending(r):
		return verdictQuestion
	case len(w.until) > 0 && slices.Contains(w.until, name):
		return verdictChanged
	case len(w.until) == 0 && !first && name != f.base:
		return verdictChanged
	case w.stage && !first && grew:
		f.stageReason = latest
		return verdictStage
	case runrec.IsFinal(r.State):
		f.goneReason = name
		return verdictFinal
	}
	return verdictNone
}

// endedRounds counts the role rounds of a read that have an end, and describes
// the last of them as "ROLE round N ended EXIT".
func endedRounds(r daemon.ShowReply) (n int, latest string) {
	for _, role := range r.Roles {
		if role.EndedAt.IsZero() {
			continue
		}
		n++
		latest = fmt.Sprintf("%s round %d ended", role.Role, role.Round)
		if role.Exit != "" {
			latest += " " + role.Exit
		}
	}
	return n, latest
}

// finish decides whether the reads of one round end the wait, and prints the
// result when they do. Every followed run that is done is printed, in the order
// given. A run that is final and matched nothing is dropped; the wait ends with
// nothing active when none is left.
func (w *waiter) finish(verdicts []verdict) (code int, done bool) {
	var stops []*followed
	var question, changed bool
	for i, v := range verdicts {
		switch v {
		case verdictQuestion:
			question = true
		case verdictChanged:
			changed = true
		case verdictStage:
		default:
			continue
		}
		w.runs[i].verdict = v
		stops = append(stops, w.runs[i])
	}
	if len(stops) > 0 {
		// A question outranks a state change, and a state change a stage end.
		switch {
		case question:
			code = ExitWaitQuestion
		case changed:
			code = ExitWaitChanged
		default:
			code = ExitWaitStage
		}
		return w.print(stops, code), true
	}
	var dropped []string
	for i, v := range verdicts {
		if v == verdictFinal {
			w.runs[i].gone = true
			dropped = append(dropped, w.runs[i].id)
		}
	}
	if !w.several {
		if len(dropped) > 0 {
			f := w.runs[0]
			return w.o.fail(ExitWaitNothingActive, "nothing_active", fmt.Sprintf("run %s is %s, so nothing is active", f.id, f.goneReason)), true
		}
		return 0, false
	}
	var left, gone []string
	for _, f := range w.runs {
		if f.gone {
			gone = append(gone, f.id+" "+f.goneReason)
		} else {
			left = append(left, f.id)
		}
	}
	if len(left) == 0 {
		return w.o.fail(ExitWaitNothingActive, "nothing_active", "nothing is active: "+strings.Join(gone, ", ")), true
	}
	for _, f := range w.runs {
		if f.gone && slices.Contains(dropped, f.id) {
			fmt.Fprintf(w.o.stderr, "%s is %s, so nothing is active; still waiting on %s\n", f.id, f.goneReason, strings.Join(left, ", "))
		}
	}
	return 0, false
}

// ended turns the context ending into the timeout code, or into a failure
// when something other than --timeout ended it. fallback, when not nil, makes
// the failure for a context that did not end.
func (w *waiter) ended(ctx context.Context, fallback func() int) int {
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) && w.timeout > 0 && w.parent.Err() == nil {
			msg := fmt.Sprintf("timed out after %v", w.timeout)
			for _, f := range w.runs {
				if f.last != nil {
					msg += fmt.Sprintf("; run %s is still %s", f.id, waitName(*f.last))
				} else {
					msg += fmt.Sprintf("; run %s was not read", f.id)
				}
			}
			return w.o.fail(ExitWaitTimeout, "timeout", msg)
		}
		return w.o.fail(ExitFailure, "interrupted", err.Error())
	}
	if fallback != nil {
		return fallback()
	}
	return w.o.fail(ExitFailure, ipc.CodeInternal, "wait ended without a result")
}

// waitJSON is what ork wait --json prints for a state it stopped on.
type waitJSON struct {
	Run     string       `json:"run"`
	Item    string       `json:"item"`
	State   string       `json:"state"`
	Reason  string       `json:"reason,omitempty"`
	Next    string       `json:"next,omitempty"`
	Pending *waitPending `json:"pending,omitempty"`
}

type waitPending struct {
	Kind string `json:"kind"`
}

// stageState is the state of a stage end in the output: the run itself has not
// changed state (adj 450; cli.wait-stage-json).
const stageState = "stage"

// print writes the runs the wait stopped on, in the order given, and returns
// code. One run prints a block of text, or one JSON object; several runs print
// one line each, or one JSON array. A stage end is always one line, and a JSON
// object with state "stage" and the round as its reason.
func (w *waiter) print(stops []*followed, code int) int {
	o := w.o
	if o.json {
		objs := make([]waitJSON, len(stops))
		for i, f := range stops {
			r := *f.last
			objs[i] = waitJSON{Run: r.ID, Item: r.Item, State: waitName(r), Reason: r.Reason, Next: r.Next}
			if f.verdict == verdictStage {
				objs[i] = waitJSON{Run: r.ID, Item: r.Item, State: stageState, Reason: f.stageReason}
			} else if r.Pending != nil {
				objs[i].Pending = &waitPending{Kind: r.Pending.Kind}
			}
		}
		var out any = objs
		if !w.several {
			out = objs[0]
		}
		if rc := o.printJSON(out); rc != ExitOK {
			return rc
		}
		return code
	}
	for _, f := range stops {
		switch r := *f.last; {
		case f.verdict == verdictStage:
			fmt.Fprintf(o.stdout, "%s %s %s %s\n", r.ID, r.Item, stageState, f.stageReason)
		case w.several:
			w.line(r)
		default:
			w.block(r, waitName(r))
		}
	}
	return code
}

// line writes one run as RUN ITEM STATE, then " key: KEY" for a question and
// " next: COMMAND" when the run has one.
func (w *waiter) line(r daemon.ShowReply) {
	name := waitName(r)
	key := ""
	if questionPending(r) { // a question is its own state in a line, as in --until
		name = "question"
		if r.Pending.Key != "" {
			key = " key: " + runrec.PlainText(r.Pending.Key)
		}
	}
	s := fmt.Sprintf("%s %s %s%s", r.ID, r.Item, name, key)
	if r.Next != "" {
		s += " next: " + r.Next
	}
	fmt.Fprintln(w.o.stdout, s)
}

// block writes one run's state, then for a pause its kind, the question and the
// decision key, and the next command, as ork show prints them.
func (w *waiter) block(r daemon.ShowReply, name string) {
	o := w.o
	fmt.Fprintf(o.stdout, "%s %s\n", r.ID, name)
	if p := r.Pending; p != nil {
		head := p.Kind + " pause"
		if p.Role != "" {
			head += ", " + p.Role
			if p.Round > 0 {
				head += fmt.Sprintf(" round %d", p.Round)
			}
		}
		fmt.Fprintln(o.stdout, head)
		if p.Ask != nil {
			fmt.Fprintf(o.stdout, "question: %s\n", runrec.PlainText(p.Ask.Title))
		}
		if p.Key != "" {
			fmt.Fprintf(o.stdout, "decision key: %s\n", runrec.PlainText(p.Key))
		}
	}
	if r.Next != "" {
		fmt.Fprintf(o.stdout, "next: %s\n", r.Next)
	}
}
