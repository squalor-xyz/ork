// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"errors"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"reflect"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/source"
)

var errNoMoreNotices = errors.New("no more notices in the script")

// fakeLive is the daemon's side of the TUI in a test. It logs each refresh
// into log when log is set, and answers Next from a script: each project name
// in turn, then an error. It never touches a socket.
type fakeLive struct {
	addPaths  []string
	mu        sync.Mutex
	log       *[]string     // refresh calls, when set
	focusGate chan struct{} // when set, Focus waits until it is closed
	focusErr  error
	notices   []string
	focused   [][]string
	nexts     int
}

func (l *fakeLive) Refresh(_ context.Context, project string) error {
	if l.log != nil {
		*l.log = append(*l.log, "refresh "+project)
	}
	return nil
}

func (l *fakeLive) Focus(_ context.Context, projects []string) error {
	if l.focusGate != nil {
		<-l.focusGate
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.focused = append(l.focused, append([]string(nil), projects...))
	return l.focusErr
}

func (l *fakeLive) Next(_ context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nexts++
	if len(l.notices) == 0 {
		return "", errNoMoreNotices
	}
	n := l.notices[0]
	l.notices = l.notices[1:]
	return n, nil
}

func (l *fakeLive) focusedCalls() [][]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.focused
}

func (l *fakeLive) nextCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.nexts
}

// loggingSource logs each snapshot call into log, then answers from inner.
type loggingSource struct {
	inner source.Source
	log   *[]string
}

func (s loggingSource) Snapshot(ctx context.Context, p registry.Project) source.Snapshot {
	*s.log = append(*s.log, "snapshot "+p.Name)
	return s.inner.Snapshot(ctx, p)
}

// deliver feeds msg to the model, then every message its commands make, until
// none are left. Ticks are dropped: a test drives the schedule itself.
func (h *harness) deliver(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	for _, out := range exec(cmd) {
		if _, ok := out.(tickMsg); ok {
			continue
		}
		h.deliver(out)
	}
}

// The r key asks the daemon to read the project now, and only then takes the
// snapshot, so the rows show what was read just now.
func TestRefreshKeyAsksTheDaemonToReadFirst(t *testing.T) {
	h := newHarness(t, proj("alpha", false))
	var log []string
	h.m.cfg.Source = loggingSource{inner: h.src, log: &log}
	h.m.cfg.Live = &fakeLive{log: &log}
	h.start()
	log = nil
	h.send(press("r"))
	if want := []string{"refresh alpha", "snapshot alpha"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("calls %v, want %v", log, want)
	}
}

// Switching projects tells the daemon which project the owner sees. The call
// is a command, so Update returns at once even while the daemon is slow.
func TestSwitchingProjectsFocusesTheNewOneWithoutBlockingUpdate(t *testing.T) {
	h := newHarness(t, proj("alpha", false), proj("beta", false))
	gate := make(chan struct{})
	live := &fakeLive{focusGate: gate}
	h.m.cfg.Live = live
	h.m.switchTo(0)

	cmdCh := make(chan tea.Cmd, 1)
	go func() {
		_, cmd := h.m.Update(press("right"))
		cmdCh <- cmd
	}()
	var cmd tea.Cmd
	select {
	case cmd = <-cmdCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Update blocked on the daemon")
	}
	if cmd == nil {
		t.Fatal("switching project returned no command to tell the daemon")
	}
	done := make(chan []tea.Msg, 1)
	go func() { done <- exec(cmd) }()
	close(gate)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the focus command did not finish")
	}
	if got := live.focusedCalls(); !reflect.DeepEqual(got, [][]string{{"beta"}}) {
		t.Fatalf("focus calls %v, want [[beta]]", got)
	}
}

// The first focus starts the notice feed. A notice reads its project, and the
// feed listens again until a Next fails.
func TestNoticesReadTheirProjectAndTheFeedListensAgain(t *testing.T) {
	h := newHarness(t, proj("alpha", false), proj("beta", false))
	var log []string
	h.m.cfg.Source = loggingSource{inner: h.src, log: &log}
	live := &fakeLive{notices: []string{"beta"}}
	h.m.cfg.Live = live

	h.deliver(focusMsg{})

	if n := live.nextCalls(); n != 2 {
		t.Fatalf("Next called %d times, want 2: one notice, then the failure that stops the feed", n)
	}
	if want := []string{"snapshot beta"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("snapshots %v, want %v", log, want)
	}
}

// A failed focus does not start the feed, and the next focus that works does.
func TestAFailedFocusDoesNotStartTheFeedUntilAFocusWorks(t *testing.T) {
	h := newHarness(t, proj("alpha", false))
	live := &fakeLive{notices: []string{"alpha"}}
	h.m.cfg.Live = live

	h.deliver(focusMsg{err: errors.New("no daemon answers")})
	if n := live.nextCalls(); n != 0 {
		t.Fatalf("Next called %d times after a failed focus, want 0", n)
	}

	h.deliver(focusMsg{})
	if n := live.nextCalls(); n == 0 {
		t.Fatal("the feed did not start after a focus that worked")
	}
}

// Start focuses the first project, the one the owner sees first.
func TestInitFocusesTheFirstProject(t *testing.T) {
	h := newHarness(t, proj("alpha", false), proj("beta", false))
	live := &fakeLive{}
	h.m.cfg.Live = live

	exec(h.m.Init())

	if got := live.focusedCalls(); !reflect.DeepEqual(got, [][]string{{"alpha", "beta"}}) {
		t.Fatalf("focus calls %v, want [[alpha beta]]", got)
	}
}

func (*fakeLive) Approve(context.Context, string) error { return errors.New("unexpected approve") }
func (*fakeLive) Stop(context.Context, string) error    { return errors.New("unexpected stop") }
func (*fakeLive) Retry(context.Context, string) error   { return errors.New("unexpected retry") }

func (l *fakeLive) Answer(context.Context, string, string) error { return nil }

func (l *fakeLive) ItemShow(context.Context, string, string) (*slicercli.Detail, error) {
	return nil, nil
}

func (*fakeLive) Plan(context.Context, daemon.RunParams) (*daemon.RunPlan, error) {
	return nil, errors.New("unexpected plan")
}
func (*fakeLive) Start(context.Context, daemon.RunParams) (runrec.Record, error) {
	return runrec.Record{}, errors.New("unexpected start")
}

func (*fakeLive) WaveSuggest(context.Context, string) (*slicercli.ParallelPlan, error) {
	return &slicercli.ParallelPlan{}, nil
}
func (*fakeLive) WaveOverlap(context.Context, string, []string) (*slicercli.Overlap, error) {
	return &slicercli.Overlap{}, nil
}
func (*fakeLive) WaveStart(context.Context, daemon.WaveStartParams) (*daemon.WaveResult, error) {
	return nil, errors.New("unexpected wave start")
}

func (f *fakeLive) Cleanup(context.Context, string) error { return nil }

func (l *fakeLive) ProjectsAdd(_ context.Context, path string) (registry.Project, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.addPaths = append(l.addPaths, path)
	return registry.Project{}, nil
}

func (l *fakeLive) AnswerAction(ctx context.Context, a daemon.RunAction) (string, error) {
	return "", l.Answer(ctx, a.Run, a.Text)
}
