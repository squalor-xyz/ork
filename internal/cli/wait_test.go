// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/source"
)

// waitFake is a daemon double on a real socket. It answers run.show from a
// list of replies (the last one repeats), and can send a snapshot.changed
// notice before each reply, as the daemon does when something changed.
type waitFake struct {
	t        *testing.T
	notify   bool                // send a notice before each run.show reply
	notFound bool                // run.show says the run is unknown
	projects []string            // registered project names; default one, "p"
	runs     map[string][]string // per project: "ID:state" engine runs that status lists
	afterRun func(n int)         // after the n-th run.show reply was written (one-based)

	mu        sync.Mutex
	shows     []daemon.ShowReply
	showCalls int
	connects  atomic.Int32
	subscribe []string // the focus of each subscribe call, joined
}

func (f *waitFake) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.showCalls
}

func (f *waitFake) handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case daemon.MethodDaemonInfo:
		return daemon.DaemonInfoResult{BuildID: "current"}, nil
	case daemon.MethodProjectsList:
		names := f.projects
		if names == nil {
			names = []string{"p"}
		}
		var out daemon.ProjectsResult
		for _, n := range names {
			out.Projects = append(out.Projects, registry.Project{Name: n, Path: "/" + n})
		}
		return out, nil
	case daemon.MethodSubscribe:
		var p daemon.SubscribeParams
		json.Unmarshal(raw, &p)
		f.mu.Lock()
		f.subscribe = append(f.subscribe, strings.Join(p.Focus, ","))
		f.mu.Unlock()
		return nil, nil
	case daemon.MethodStatus:
		var p daemon.StatusParams
		json.Unmarshal(raw, &p)
		var out daemon.StatusResult
		ps := daemon.ProjectStatus{Name: p.Project}
		for _, r := range f.runs[p.Project] {
			id, state, _ := strings.Cut(r, ":")
			ps.EngineRuns = append(ps.EngineRuns, source.EngineRun{ID: id, State: state})
		}
		out.Projects = append(out.Projects, ps)
		return out, nil
	case daemon.MethodRunShow:
		if f.notFound {
			return nil, &ipc.Error{Code: daemon.CodeNotFound, Message: "no run"}
		}
		if f.notify {
			if c := ipc.ConnFrom(ctx); c != nil {
				c.Notify(daemon.MethodSnapshotChanged, daemon.SnapshotChanged{Project: "p", Parts: []string{"runs"}})
			}
		}
		f.mu.Lock()
		i := min(f.showCalls, len(f.shows)-1)
		f.showCalls++
		reply := f.shows[i]
		f.mu.Unlock()
		return reply, nil
	}
	return nil, &ipc.Error{Code: daemon.CodeUnknownMethod, Message: "unexpected " + method}
}

// serve starts the double on socket and returns what stops it and waits for it.
func (f *waitFake) serve(socket string) (stop func()) {
	f.t.Helper()
	l, err := ipc.Listen(socket)
	if err != nil {
		f.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s := &ipc.Server{
		Handler:   f.handle,
		OnConnect: func() { f.connects.Add(1) },
		AfterReply: func(method string) {
			if method != daemon.MethodRunShow || f.afterRun == nil {
				return
			}
			f.mu.Lock()
			n := f.showCalls
			f.mu.Unlock()
			f.afterRun(n)
		},
	}
	go func() { defer close(done); s.Serve(ctx, l) }()
	return func() { cancel(); <-done }
}

// waitSocket returns a socket path in a new short directory. It fails, and
// does not skip, when the temp directory is too long for a socket: a skipped
// wait test would pass the suite without testing anything.
func waitSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ow")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "w.sock")
	if len(socket) > ipc.MaxSocketPath {
		t.Fatalf("socket path %s is longer than %d bytes", socket, ipc.MaxSocketPath)
	}
	return socket
}

// waitNoDaemon is an App with no daemon behind its socket that fails the test
// if wait tries to start one.
func waitNoDaemon(t *testing.T) *App {
	return &App{
		Socket: waitSocket(t),
		Start:  func(context.Context) error { t.Error("wait started a daemon"); return nil },
	}
}

func newWaitFake(t *testing.T, shows ...daemon.ShowReply) (*waitFake, *App) {
	t.Helper()
	f := &waitFake{t: t, shows: shows}
	socket := waitSocket(t)
	t.Cleanup(f.serve(socket))
	// A long poll means only a notice, or a stop condition, ends a sleep.
	return f, &App{Socket: socket, BuildID: "current", WaitPoll: time.Hour}
}

// runWait runs ork wait with a short deadline, so a wait that never returns
// fails the test at once.
func runWait(t *testing.T, a *App, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code = a.Run(ctx, append([]string{"wait"}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

func waitShow(state string) daemon.ShowReply {
	return daemon.ShowReply{ID: "r1", Item: "S9", State: state}
}

func waitPause(kind, key string) daemon.ShowReply {
	r := waitShow("paused")
	r.Reason = "the reason"
	r.Next = "ork retry r1"
	r.Pending = &daemon.ShowPending{Kind: kind, State: "review", Role: "reviewer", Round: 1, Reason: "the reason", Key: key}
	return r
}

const untilList = "awaiting-approval,paused,stopped,landed"

func TestWaitReturnsWhenTheRunEntersAnUntilState(t *testing.T) {
	f, app := newWaitFake(t, waitShow("implement"), waitShow("implement"), waitShow("awaiting-approval"))
	f.notify = true
	code, out, errs := runWait(t, app, "r1", "--until", untilList)
	if code != ExitOK || out != "r1 awaiting-approval\n" || errs != "" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	if n := f.calls(); n != 3 {
		t.Fatalf("run.show was read %d times, want 3: the two implement replies are not a stop", n)
	}
	if n := f.connects.Load(); n != 1 {
		t.Fatalf("%d connections, want one", n)
	}
}

func TestWaitIgnoresOutputTicks(t *testing.T) {
	shows := make([]daemon.ShowReply, 0, 12)
	for range 11 { // the first read and ten notices that change nothing
		shows = append(shows, waitShow("implement"))
	}
	shows = append(shows, waitPause("shape", ""))
	f, app := newWaitFake(t, shows...)
	f.notify = true
	code, out, errs := runWait(t, app, "r1")
	if code != ExitWaitChanged || !strings.HasPrefix(out, "r1 paused\n") || errs != "" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	if n := f.calls(); n != 12 {
		t.Fatalf("run.show was read %d times, want 12", n)
	}
}

func TestWaitQuestionPendingHasItsOwnExitCode(t *testing.T) {
	_, app := newWaitFake(t, waitPause("question", "area.topic"))
	// A question stops the wait even when --until does not name it.
	code, out, errs := runWait(t, app, "r1", "--until", "awaiting-approval")
	if code != ExitWaitQuestion || errs != "" {
		t.Fatalf("code %d, want %d; stdout %q, stderr %q", code, ExitWaitQuestion, out, errs)
	}
	for _, want := range []string{"r1 paused\n", "decision key: area.topic\n", "next: ork retry r1\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
}

func TestWaitStateChangedExitCode(t *testing.T) {
	f, app := newWaitFake(t, waitShow("implement"), waitShow("implement"), waitShow("review"))
	f.notify = true
	code, out, errs := runWait(t, app, "r1")
	if code != ExitWaitChanged || out != "r1 review\n" || errs != "" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	if n := f.calls(); n != 3 {
		t.Fatalf("run.show was read %d times, want 3", n)
	}
}

func TestWaitNothingActive(t *testing.T) {
	oneLine := func(t *testing.T, code int, out, errs string, want ...string) {
		t.Helper()
		if code != ExitWaitNothingActive || out != "" || strings.Count(strings.TrimSuffix(errs, "\n"), "\n") != 0 || errs == "" {
			t.Fatalf("code %d, want %d; stdout %q, stderr %q", code, ExitWaitNothingActive, out, errs)
		}
		for _, w := range want {
			if !strings.Contains(errs, w) {
				t.Errorf("message %q lacks %q", errs, w)
			}
		}
	}
	t.Run("a run that is final", func(t *testing.T) {
		_, app := newWaitFake(t, waitShow("committed"))
		code, out, errs := runWait(t, app, "r1")
		oneLine(t, code, out, errs, "r1", "committed")
	})
	t.Run("a run the daemon does not know", func(t *testing.T) {
		f, app := newWaitFake(t, waitShow("implement"))
		f.notFound = true
		code, out, errs := runWait(t, app, "r9")
		oneLine(t, code, out, errs, "r9")
	})
	t.Run("a final run that never entered the until list", func(t *testing.T) {
		f, app := newWaitFake(t, waitShow("implement"), waitShow("committed"))
		f.notify = true
		code, out, errs := runWait(t, app, "r1", "--until", "landed")
		oneLine(t, code, out, errs, "r1", "committed")
	})
	t.Run("no unfinished run in the project", func(t *testing.T) {
		f, app := newWaitFake(t, waitShow("implement"))
		f.runs = map[string][]string{"p": {"r1:committed", "r2:stopped"}}
		code, out, errs := runWait(t, app)
		oneLine(t, code, out, errs, "p")
		if n := f.calls(); n != 0 {
			t.Errorf("run.show was read %d times for a project with no unfinished run", n)
		}
	})
}

func TestWaitTimeout(t *testing.T) {
	f, app := newWaitFake(t, waitShow("implement"))
	start := time.Now()
	code, out, errs := runWait(t, app, "r1", "--timeout", "50ms")
	if code != ExitWaitTimeout || out != "" || !strings.Contains(errs, "r1") || !strings.Contains(errs, "implement") {
		t.Fatalf("code %d, want %d; stdout %q, stderr %q", code, ExitWaitTimeout, out, errs)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("took %v for a 50ms timeout", took)
	}
	if n := f.calls(); n != 1 {
		t.Fatalf("run.show was read %d times, want 1", n)
	}
}

func TestWaitJSONPrintsTheNewState(t *testing.T) {
	_, app := newWaitFake(t, waitPause("shape", ""))
	code, out, errs := runWait(t, app, "r1", "--until", "paused", "--json")
	if code != ExitOK || errs != "" {
		t.Fatalf("code %d, stderr %q, stdout %q", code, errs, out)
	}
	dec := json.NewDecoder(strings.NewReader(out))
	var got map[string]any
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if dec.More() {
		t.Fatalf("stdout holds more than the JSON object:\n%s", out)
	}
	pending, _ := got["pending"].(map[string]any)
	want := map[string]any{"run": "r1", "item": "S9", "state": "paused", "reason": "the reason", "next": "ork retry r1"}
	if len(got) != len(want)+1 || len(pending) != 1 || pending["kind"] != "shape" {
		t.Fatalf("fields %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestWaitJSONFailureIsAnErrorObject(t *testing.T) {
	_, app := newWaitFake(t, waitShow("implement"))
	code, out, _ := runWait(t, app, "r1", "--timeout", "30ms", "--json")
	var got struct {
		Error ipc.Error `json:"error"`
	}
	mustJSON(t, out, &got)
	if code != ExitWaitTimeout || got.Error.Code != "timeout" || !strings.Contains(got.Error.Message, "implement") {
		t.Fatalf("code %d, stdout %s", code, out)
	}
}

func TestWaitUntilRejectsAnUnknownState(t *testing.T) {
	for _, until := range []string{"bogus", "paused,bogus", "committed", "paused,,stopped", "", "paused,failed"} {
		t.Run(until, func(t *testing.T) {
			code, out, errs := runWait(t, waitNoDaemon(t), "r1", "--until", until)
			if code != ExitUsage || out != "" {
				t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
			}
			for _, state := range RunEventStates {
				if !strings.Contains(errs, state) {
					t.Errorf("stderr lacks the valid state %q:\n%s", state, errs)
				}
			}
		})
	}
}

// failed is not on the list for now (adj 247, cli.wait-failed-state): a run.show
// reply does not say whether a ledger run ended with outcome fail, and ork ends
// its own ledger runs only with pass or stopped. S311 can add it later. Until
// then it is an unknown state and the valid list does not offer it.
func TestWaitHasNoFailedState(t *testing.T) {
	if want := []string{"question", "awaiting-approval", "paused", "stopped", "landed"}; !slices.Equal(RunEventStates, want) {
		t.Fatalf("RunEventStates = %q, want %q", RunEventStates, want)
	}
	code, out, errs := runWait(t, waitNoDaemon(t), "r1", "--until", "paused,failed")
	if code != ExitUsage || out != "" || !strings.Contains(errs, `unknown state "failed"`) || strings.Contains(errs, "valid states: question, awaiting-approval, paused, stopped, landed, failed") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestWaitAlreadyInAnUntilStateReturnsAtOnce(t *testing.T) {
	f, app := newWaitFake(t, waitShow("awaiting-approval"))
	start := time.Now()
	code, out, _ := runWait(t, app, "r1", "--until", untilList)
	if code != ExitOK || out != "r1 awaiting-approval\n" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("took %v", took)
	}
	if n := f.calls(); n != 1 {
		t.Fatalf("run.show was read %d times, want 1", n)
	}
}

func TestWaitReadsAgainWhenNoNoticeComes(t *testing.T) {
	// The daemon may not send a notice for every change: the wait reads again
	// on its own poll.
	f, app := newWaitFake(t, waitShow("implement"), waitShow("awaiting-approval"))
	app.WaitPoll = 10 * time.Millisecond
	code, out, _ := runWait(t, app, "r1", "--until", untilList)
	if code != ExitOK || out != "r1 awaiting-approval\n" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	if n := f.calls(); n != 2 {
		t.Fatalf("run.show was read %d times, want 2", n)
	}
}

func TestWaitReconnectsAfterADaemonRestart(t *testing.T) {
	// Three daemons in a row on one socket. The wait reads the first, loses
	// it, reads the second after one retry, loses that too (a drop after a
	// good read is a new drop, with its own retry), and ends on the third.
	daemons := []*waitFake{
		{t: t, shows: []daemon.ShowReply{waitShow("implement")}},
		{t: t, shows: []daemon.ShowReply{waitShow("review")}},
		{t: t, shows: []daemon.ShowReply{waitShow("awaiting-approval")}},
	}
	socket := waitSocket(t)
	answered := make([]chan struct{}, len(daemons)-1) // daemon i has answered once
	up := make([]chan struct{}, len(daemons)-1)       // daemon i+1 is serving
	for i := range answered {
		ch, once := make(chan struct{}), new(sync.Once)
		answered[i], up[i] = ch, make(chan struct{})
		daemons[i].afterRun = func(int) { once.Do(func() { close(ch) }) }
	}
	var mu sync.Mutex
	stops := make([]func(), len(daemons))
	stops[0] = daemons[0].serve(socket)
	started := make(chan int, len(daemons)) // the wait asked for a daemon to be started
	var starts atomic.Int32
	quit := make(chan struct{})
	var controller sync.WaitGroup
	t.Cleanup(func() {
		close(quit)
		controller.Wait()
		for _, stop := range stops {
			if stop != nil {
				stop()
			}
		}
	})
	controller.Add(1)
	go func() {
		defer controller.Done()
		for i := range answered {
			select {
			case <-answered[i]:
			case <-quit:
				return
			}
			mu.Lock()
			stop := stops[i]
			mu.Unlock()
			stop() // the daemon ends and every connection with it
			// The next daemon stays down until the wait asks for it, so the
			// wait cannot find it up by chance.
			select {
			case <-started:
			case <-quit:
				return
			}
			serving := daemons[i+1].serve(socket)
			mu.Lock()
			stops[i+1] = serving
			mu.Unlock()
			close(up[i])
		}
	}()
	app := &App{Socket: socket, BuildID: "current", WaitPoll: time.Hour, StartWait: 3 * time.Second, PollEvery: time.Millisecond,
		Start: func(context.Context) error {
			n := int(starts.Add(1)) - 1
			started <- n
			<-up[n]
			return nil
		}}
	code, out, errs := runWait(t, app, "r1", "--until", untilList)
	if code != ExitOK || out != "r1 awaiting-approval\n" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	if starts.Load() != 2 {
		t.Errorf("the wait asked for %d daemon starts, want 2: one retry per drop", starts.Load())
	}
	for i, d := range daemons {
		if n := d.connects.Load(); n != 1 {
			t.Errorf("daemon %d had %d connections, want 1", i, n)
		}
	}
}

func TestWaitSubscribesToTheRegisteredProjects(t *testing.T) {
	f, app := newWaitFake(t, waitShow("awaiting-approval"))
	f.projects = []string{"alpha", "beta"}
	if code, _, errs := runWait(t, app, "r1", "--until", untilList); code != ExitOK {
		t.Fatalf("code %d, stderr %q", code, errs)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.subscribe) != 1 || f.subscribe[0] != "alpha,beta" {
		t.Fatalf("subscribed %q, want one call for alpha,beta", f.subscribe)
	}
}

func TestWaitLandedIsACommittedRunThatLanded(t *testing.T) {
	landed := waitShow("committed")
	landed.Landed = true
	t.Run("landed", func(t *testing.T) {
		_, app := newWaitFake(t, landed)
		code, out, _ := runWait(t, app, "r1", "--until", "landed")
		if code != ExitOK || out != "r1 landed\n" {
			t.Fatalf("code %d, stdout %q", code, out)
		}
	})
	t.Run("committed only", func(t *testing.T) {
		_, app := newWaitFake(t, waitShow("committed"))
		code, out, errs := runWait(t, app, "r1", "--until", "landed")
		if code != ExitWaitNothingActive || out != "" {
			t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
		}
	})
}

func TestWaitWithoutRunFollowsTheOnlyUnfinishedRun(t *testing.T) {
	f, app := newWaitFake(t, waitShow("awaiting-approval"))
	f.runs = map[string][]string{"p": {"r0:committed", "r1:implement", "r2:stopped"}}
	code, out, errs := runWait(t, app, "--until", untilList)
	if code != ExitOK || out != "r1 awaiting-approval\n" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestWaitWithoutRunNeedsOneRunAndOneProject(t *testing.T) {
	t.Run("two unfinished runs", func(t *testing.T) {
		f, app := newWaitFake(t, waitShow("implement"))
		f.runs = map[string][]string{"p": {"r1:implement", "r2:review"}}
		code, out, errs := runWait(t, app)
		if code != ExitUsage || out != "" || !strings.Contains(errs, "r1") || !strings.Contains(errs, "r2") {
			t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
		}
	})
	t.Run("two projects", func(t *testing.T) {
		f, app := newWaitFake(t, waitShow("implement"))
		f.projects = []string{"alpha", "beta"}
		code, out, errs := runWait(t, app)
		if code != ExitUsage || out != "" || !strings.Contains(errs, "--project") {
			t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
		}
	})
	t.Run("--project picks one", func(t *testing.T) {
		f, app := newWaitFake(t, waitShow("awaiting-approval"))
		f.projects = []string{"alpha", "beta"}
		f.runs = map[string][]string{"alpha": {"a1:committed"}, "beta": {"b1:review"}}
		code, out, errs := runWait(t, app, "--project", "beta", "--until", untilList)
		if code != ExitOK || out != "r1 awaiting-approval\n" {
			t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
		}
	})
}

func TestWaitTakesRunAndFlags(t *testing.T) {
	for _, args := range [][]string{{"r1", "r2"}, {"r1", "--timeout", "-1s"}, {"r1", "--timeout", "soon"}, {"--bogus"}} {
		code, out, errs := runWait(t, waitNoDaemon(t), args...)
		if code != ExitUsage || out != "" || errs == "" {
			t.Errorf("%q: code %d, stdout %q, stderr %q", args, code, out, errs)
		}
	}
}

func TestExitCodesAreDistinct(t *testing.T) {
	// Owner decision 2026-11-09 (cli.wait-exit-codes): 0 changed to an until
	// state, 3 question pending, 4 nothing active, 5 timeout.
	codes := map[string]int{"changed": ExitWaitChanged, "question": ExitWaitQuestion, "nothing active": ExitWaitNothingActive, "timeout": ExitWaitTimeout}
	want := map[string]int{"changed": 0, "question": 3, "nothing active": 4, "timeout": 5}
	seen := map[int]string{}
	for name, code := range codes {
		if code != want[name] {
			t.Errorf("%s exits %d, want %d", name, code, want[name])
		}
		if other, dup := seen[code]; dup {
			t.Errorf("%s and %s both exit %d", name, other, code)
		}
		seen[code] = name
		if name != "changed" && (code == ExitOK || code == ExitFailure || code == ExitUsage) {
			t.Errorf("%s exits %d, which another result already uses", name, code)
		}
	}
}
