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

// wait blocks until a run changes state and prints the new state (S257,
// *proposed*). It subscribes, reads run.show at once, and reads it again on
// each notice and every WaitPoll, because the daemon sends a notice for a
// cached part and not for every record change. It only reads.
func (a *App) wait(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	untilFlag := fs.String("until", "", "stop when the run enters one of these states (comma separated)")
	timeout := fs.Duration("timeout", 0, "give up after this long; 0 waits for ever")
	project := fs.String("project", "", "the project whose unfinished run to follow when RUN is omitted")
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("wait: %v", err)
	}
	if len(pos) > 1 {
		return o.usage("wait takes [RUN] [--until STATE,...] [--timeout D] [--project P]")
	}
	w := &waiter{app: a, o: o, project: *project, poll: a.WaitPoll, parent: ctx}
	if w.poll <= 0 {
		w.poll = defaultWaitPoll
	}
	if len(pos) == 1 {
		w.run = pos[0]
	}
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
	run     string // empty until resolved from the project
	until   []string
	poll    time.Duration
	timeout time.Duration
	parent  context.Context // the caller's context, without the --timeout deadline

	base    string            // the state name of the first read
	hasBase bool              // base is set
	last    *daemon.ShowReply // the newest read
	retried bool              // the connection dropped and a retry is under way
	dropped error             // why the connection dropped
}

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
	notices.Add(1)
	go func() {
		defer notices.Done()
		for {
			if _, err := c.Next(noticeCtx); err != nil {
				dead <- err
				return
			}
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}()
	if w.run == "" {
		run, code, retry := w.resolve(ctx, c, projects)
		if run == "" {
			return code, retry
		}
		w.run = run
	}
	tick := time.NewTicker(w.poll)
	defer tick.Stop()
	for {
		reply, err := c.Show(ctx, w.run)
		if err != nil {
			if e := ipc.ErrorOf(err); ctx.Err() == nil && !transport(err) && e.Code == daemon.CodeNotFound {
				return o.fail(ExitWaitNothingActive, "nothing_active", fmt.Sprintf("no run %s: it is unknown to the daemon, so nothing is active", w.run)), false
			}
			return drop(err)
		}
		w.retried = false // a read worked on this connection
		if code, done := w.judge(reply); done {
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

// resolve picks the run a wait without RUN follows (adj 203, cli.wait-default-run;
// cli.wait-project-scope): the unfinished run of --project, or of the only
// registered project. Several runs, or several projects with no --project, is
// a usage error. It returns no run when the wait ended with code, or when the
// connection dropped (retry).
func (w *waiter) resolve(ctx context.Context, c *daemon.Client, projects []registry.Project) (run string, code int, retry bool) {
	o := w.o
	name := w.project
	if name == "" {
		switch len(projects) {
		case 0:
			return "", o.usage("wait needs RUN: no project is registered"), false
		case 1:
			name = projects[0].Name
		default:
			names := make([]string, len(projects))
			for i, p := range projects {
				names[i] = p.Name
			}
			return "", o.usage("wait needs RUN or --project: %d projects are registered (%s)", len(projects), strings.Join(names, ", ")), false
		}
	}
	var res daemon.StatusResult
	if err := c.Call(ctx, daemon.MethodStatus, daemon.StatusParams{Project: name}, &res); err != nil {
		code, retry := w.drop(ctx, err)
		return "", code, retry
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
	switch len(open) {
	case 0:
		return "", o.fail(ExitWaitNothingActive, "nothing_active", fmt.Sprintf("project %s has no unfinished run, so nothing is active", name)), false
	case 1:
		return open[0], 0, false
	}
	return "", o.usage("project %s has %d unfinished runs (%s); name one as RUN", name, len(open), strings.Join(open, ", ")), false
}

// judge decides whether a read ends the wait, and prints the result when it
// does. A pending question always ends it, because only the owner moves the
// run on. Then the --until list; with no list, a change of state from the
// first read. A final run that matched none of those has nothing more to do.
func (w *waiter) judge(r daemon.ShowReply) (code int, done bool) {
	name := waitName(r)
	first := !w.hasBase
	if first {
		w.base, w.hasBase = name, true
	}
	w.last = &r
	switch {
	case questionPending(r):
		return w.print(r, name, ExitWaitQuestion), true
	case len(w.until) > 0 && slices.Contains(w.until, name):
		return w.print(r, name, ExitWaitChanged), true
	case len(w.until) == 0 && !first && name != w.base:
		return w.print(r, name, ExitWaitChanged), true
	case runrec.IsFinal(r.State):
		return w.o.fail(ExitWaitNothingActive, "nothing_active", fmt.Sprintf("run %s is %s, so nothing is active", r.ID, name)), true
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
			switch {
			case w.last != nil:
				msg += fmt.Sprintf("; run %s is still %s", w.last.ID, waitName(*w.last))
			case w.run != "":
				msg += fmt.Sprintf("; run %s was not read", w.run)
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

// print writes the state the wait stopped on and returns code. The text is
// the run and its state, then for a pause its kind, the question and the
// decision key, and the next command, as ork show prints them.
func (w *waiter) print(r daemon.ShowReply, name string, code int) int {
	o := w.o
	if o.json {
		out := waitJSON{Run: r.ID, Item: r.Item, State: name, Reason: r.Reason, Next: r.Next}
		if r.Pending != nil {
			out.Pending = &waitPending{Kind: r.Pending.Kind}
		}
		if rc := o.printJSON(out); rc != ExitOK {
			return rc
		}
		return code
	}
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
	return code
}
