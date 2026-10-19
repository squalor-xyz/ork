// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

type roundRunner func(context.Context, engine.Request) (engine.Result, error)

func (f roundRunner) Run(c context.Context, r engine.Request) (engine.Result, error) { return f(c, r) }

func runDaemon(t *testing.T, runner roundRunner) (*Daemon, *runrec.Store, registry.Project) {
	t.Helper()
	if runner == nil {
		runner = func(context.Context, engine.Request) (engine.Result, error) {
			return engine.Result{}, fmt.Errorf("unexpected role call")
		}
	}
	root := t.TempDir()
	p := registry.Project{Name: "p", Path: filepath.Join(root, "p"), CheckCommand: "go test ./..."}
	store := &runrec.Store{Dir: filepath.Join(root, "runs")}
	reg := filepath.Join(root, "projects.json")
	if err := registry.Save(reg, &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	fake := clitest.Install(t,
		clitest.Route{Tool: "slicer", Args: []string{"relay-roles"}, Prefix: true, Stdout: `{"roles":[{"role":"implementer","row":{"vendor":"Codex","model":"wrong"},"alternatives":[{"vendor":"Claude","model":"sonnet","reasoning":"high"},{"vendor":"Claude","model":"wrong"}]},{"role":"reviewer","row":{"vendor":"Claude","model":"opus","reasoning":"medium"}}]}`},
		clitest.Route{Tool: "slicer", Args: []string{"ai", "relay"}, Stdout: "Contract: relay v7\n"},
		clitest.Route{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: `{"slice":{"sections":[{"heading":"Git","body":"One commit: \"S1: test\"."}]}}`},
		clitest.Route{Tool: "slicer", Args: []string{"config"}, Prefix: true, Stdout: `{"value":"done"}`},
		clitest.Route{Tool: "slicer", Args: []string{"check"}, Prefix: true, Stdout: `{"ok":true}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "begin"}, Prefix: true, Stdout: `{"run":{"id":"relay-1"}}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "step"}, Prefix: true, Stdout: `{"step":{"id":"1"}}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "end"}, Prefix: true, Stdout: `{}`},
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "rev-parse", "--git-path", "info/exclude"}, Stdout: filepath.Join(root, "exclude")},
		clitest.Route{Tool: "git", Args: []string{"-C", filepath.Join(p.Path, ".worktrees", "S1"), "rev-parse", "HEAD"}, Stdout: "abc123\n"},
		clitest.Route{Tool: "git", Prefix: true},
	)
	deps := engine.Deps{Store: store, Slicer: slicercli.Client{Bin: fake.Bin("slicer")}, GitBin: fake.Bin("git"), Runner: runner,
		Check: func(context.Context, string, string, time.Duration) (engine.CheckOutput, error) {
			return engine.CheckOutput{OK: true}, nil
		}}
	d := New(Config{Paths: PathsIn(filepath.Join(root, "state")), RegistryPath: reg, Source: &stubSource{}, Engine: &deps})
	t.Cleanup(d.cache.Close)
	t.Cleanup(d.closeRuns)
	return d, store, p
}
func request(t *testing.T, d *Daemon, method, raw string) json.RawMessage {
	t.Helper()
	v, err := d.handle(context.Background(), method, json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func waitState(t *testing.T, store *runrec.Store, id, state string) runrec.Record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := store.Open(id)
		if err == nil && r.Snapshot().State == state {
			return r.Snapshot()
		}
		time.Sleep(time.Millisecond)
	}
	r, _ := store.Open(id)
	t.Fatalf("run did not reach %s: %+v", state, r.Snapshot())
	return runrec.Record{}
}
func TestRunPlanUsesFirstClaudeAndResolvedPermissions(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	b := request(t, d, "run.plan", `{"project":"p","item":"S1","well_specified":true,"split":false}`)
	for _, want := range []string{filepath.Join(p.Path, ".worktrees", "S1"), `"branch":"S1"`, `"check_command":"go test ./..."`, `"model":"sonnet"`, `"model":"opus"`, `Bash(go test ./...*)`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("plan missing %s: %s", want, b)
		}
	}
	if strings.Contains(string(b), "wrong") {
		t.Fatalf("wrong lineup: %s", b)
	}
	runs, err := store.List()
	if err != nil || len(runs) != 0 {
		t.Fatalf("plan created run: %v %v", runs, err)
	}
}
func TestRunStartAnswerApproveThroughDaemon(t *testing.T) {
	runner := roundRunner(func(_ context.Context, r engine.Request) (engine.Result, error) {
		report := "Tests: 1 passed, 0 failed\n\n## Owner needed\nnone\n"
		if r.Role == engine.RoleImplementer && !strings.Contains(r.Brief, "## Finish step") && !strings.Contains(r.Brief, "use stdlib") {
			report = "Tests: 1 passed, 0 failed\n\n## Owner needed\nWhich library?\n"
		}
		if r.Role == engine.RoleReviewer {
			report = "(A): nothing\n(B): nothing\n(D): nothing\n\n## Owner needed\nnone\n"
		}
		if r.Role == engine.RoleGate {
			report = "VERDICT: PASS\n" + report
		}
		return engine.Result{Exit: engine.ExitOK, Session: "session", Report: report}, nil
	})
	d, store, _ := runDaemon(t, runner)
	b := request(t, d, "run.start", `{"project":"p","item":"S1","well_specified":true,"split":false}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StatePaused)
	request(t, d, "run.answer", fmt.Sprintf(`{"run":%q,"text":"use stdlib"}`, rec.ID))
	waitState(t, store, rec.ID, runrec.StateAwaitingApproval)
	b = request(t, d, "run.approve", fmt.Sprintf(`{"run":%q}`, rec.ID))
	if !strings.Contains(string(b), `"state":"committed"`) || !strings.Contains(string(b), `"sha":"abc123"`) {
		t.Fatalf("approval did not commit: %s", b)
	}
}
func TestDaemonStopCancelsAnActiveRole(t *testing.T) {
	started := make(chan struct{})
	killed := make(chan struct{})
	d, store, _ := runDaemon(t, func(ctx context.Context, r engine.Request) (engine.Result, error) {
		close(started)
		<-ctx.Done()
		close(killed)
		return engine.Result{}, ctx.Err()
	})
	b := request(t, d, "run.start", `{"project":"p","item":"S1"}`)
	var rec runrec.Record
	json.Unmarshal(b, &rec)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("role did not start")
	}
	b = request(t, d, "run.stop", fmt.Sprintf(`{"run":%q}`, rec.ID))
	select {
	case <-killed:
	default:
		t.Fatal("stop returned before role was killed")
	}
	if !strings.Contains(string(b), `"state":"stopped"`) {
		t.Fatalf("stop: %s", b)
	}
	stopped := waitState(t, store, rec.ID, runrec.StateStopped)
	for _, role := range stopped.Roles {
		if role.Running() {
			t.Fatalf("stopped role is still running: %+v", role)
		}
	}
}
func TestStatusIncludesEngineWaitsAndNeedsYou(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	for _, s := range []string{runrec.StateAwaitingApproval, runrec.StatePaused, runrec.StateStopped, runrec.StateCommitted} {
		_, err := store.Create(runrec.Record{ID: s, Project: p.Name, State: s, Reason: "owner action"})
		if err != nil {
			t.Fatal(err)
		}
	}
	b := request(t, d, "status", `{"project":"p"}`)
	if !strings.Contains(string(b), `"needs_you":3`) || !strings.Contains(string(b), `"state":"awaiting-approval"`) || !strings.Contains(string(b), `"wait":"owner action"`) {
		t.Fatalf("status: %s", b)
	}
}
func TestActiveRunInhibitsDaemonIdleExit(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "active", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{}
	exited := false
	idle := &idleTimer{clock: clock, timeout: func() time.Duration { return time.Minute }, waveRunning: d.activeRuns, onIdle: func() { exited = true }}
	idle.start()
	defer idle.stop()
	clock.Advance(time.Minute)
	if exited {
		t.Fatal("daemon idle-exited during active run")
	}
}

func TestRecoveredRunCanBeStoppedAndActiveRunIsPaused(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "interrupted", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := waitState(t, store, "interrupted", runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingInterrupted {
		t.Fatalf("recovery: %+v", rec)
	}
	b := request(t, d, "run.stop", `{"run":"interrupted"}`)
	if !strings.Contains(string(b), `"state":"stopped"`) {
		t.Fatalf("recovered stop: %s", b)
	}
}

func TestTwoOpenTUIsShareCLIReads(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "alpha", Path: "/p/alpha", Adj: true})
	fake := clitest.Install(t,
		clitest.Route{Tool: "slicer", Args: []string{"list"}, Prefix: true, Stdout: `[{"id":"S1","title":"shared"}]`},
		clitest.Route{Tool: "slicer", Args: []string{"goals"}, Prefix: true, Stdout: `{}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run"}, Prefix: true, Stdout: `{"runs":[]}`},
		clitest.Route{Tool: "adj", Prefix: true, Stdout: `{"records":[]}`},
		clitest.Route{Tool: "git", Prefix: true, Stdout: "head\n"},
	)
	direct := source.NewDirect(slicercli.Client{Bin: fake.Bin("slicer")}, adjcli.Client{Bin: fake.Bin("adj")})
	direct.Git = fake.Bin("git")
	e.fetch = direct
	e.start(t)
	ctx := context.Background()
	f1, f2 := NewFeed(e.paths.Socket), NewFeed(e.paths.Socket)
	defer f1.Close()
	defer f2.Close()
	if err := f1.Focus(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	c1, c2 := e.dial(t), e.dial(t)
	s1 := c1.Snapshot(ctx, registry.Project{Name: "alpha"})
	if err := f2.Focus(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	s2 := c2.Snapshot(ctx, registry.Project{Name: "alpha"})
	if len(s1.Items.Value) != 1 || len(s2.Items.Value) != 1 || s1.Items.Value[0].ID != "S1" || s2.Items.Value[0].ID != "S1" {
		t.Fatalf("TUI snapshots: %+v %+v", s1.Items, s2.Items)
	}
	settle()
	for _, verb := range []string{"list", "goals", "relay-run"} {
		n := 0
		for _, call := range fake.CallsTo("slicer") {
			if call.Args[0] == verb {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("two open TUIs: %s invoked %d times, want one shared read", verb, n)
		}
	}
}

func TestChangedConfirmationPlanCannotStart(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	_, err := d.handle(context.Background(), "run.start", json.RawMessage(`{"project":"p","item":"S1","expected":{"item":"S1","branch":"stale"}}`))
	if err == nil || !strings.Contains(err.Error(), "plan changed") {
		t.Fatalf("changed plan: %v", err)
	}
}

func TestRecoveryIgnoresFinishedRunsFromRemovedProjects(t *testing.T) {
	d, store, _ := runDaemon(t, nil)
	for _, state := range []string{runrec.StateCommitted, runrec.StateStopped} {
		if _, err := store.Create(runrec.Record{ID: state, Project: "removed", State: state}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatalf("finished history blocks daemon startup: %v", err)
	}
}

func TestDaemonActiveRunHoldsOffIdle(t *testing.T) {
	started := make(chan struct{})
	d, _, _ := runDaemon(t, func(ctx context.Context, _ engine.Request) (engine.Result, error) {
		close(started)
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	e := newEnv(t)
	d.cfg.Paths = e.paths
	d.cfg.Clock = e.clock
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	})
	select {
	case <-d.Ready():
	case err := <-done:
		done <- err
		t.Fatalf("daemon startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not become ready")
	}
	b := request(t, d, "run.start", `{"project":"p","item":"S1"}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("role did not start")
	}
	e.clock.Advance(10 * time.Minute)
	select {
	case err := <-done:
		done <- err
		t.Fatalf("daemon idle-exited during active run: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	request(t, d, "run.stop", fmt.Sprintf(`{"run":%q}`, rec.ID))
	e.clock.Advance(10 * time.Minute)
	select {
	case err := <-done:
		done <- err
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("idle daemon did not exit after stopping its run")
	}
}

func TestBusyRunHoldsOffIdle(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	d.runs.runs["busy"] = &managedRun{busy: true}
	clock := &fakeClock{}
	exited := false
	idle := &idleTimer{clock: clock, timeout: func() time.Duration { return time.Minute }, waveRunning: d.activeRuns, onIdle: func() { exited = true }}
	idle.start()
	defer idle.stop()
	clock.Advance(time.Minute)
	if exited {
		t.Fatal("daemon idle-exited during busy job")
	}
}

type failedRecoveryProc struct{}

func (failedRecoveryProc) Alive(int) bool { return true }
func (failedRecoveryProc) Kill(int) error { return errors.New("kill refused") }

func captureRunErrors(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old; r.Close(); w.Close() }()
	fn()
	w.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRecoverySkipsBadRuns(t *testing.T) {
	for _, kind := range []string{"removed-project", "kill", "list"} {
		t.Run(kind, func(t *testing.T) {
			d, store, p := runDaemon(t, nil)
			bad := runrec.Record{ID: "bad", Project: p.Name, State: runrec.StateImplement}
			want := ""
			switch kind {
			case "removed-project":
				bad.Project = "removed"
				want = "not registered"
			case "kill":
				d.runs.deps.Proc = failedRecoveryProc{}
				bad.Roles = []runrec.RoleRecord{{Role: engine.RoleImplementer, PGID: 987654, StartedAt: time.Now()}}
				want = "kill refused"
			case "list":
				want = "run record"
			}
			if _, err := store.Create(bad); err != nil {
				t.Fatal(err)
			}
			if kind == "list" {
				if err := os.WriteFile(filepath.Join(store.RunDir("bad"), "run.json"), []byte("broken"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.Create(runrec.Record{ID: "good", Project: p.Name, State: runrec.StateImplement}); err != nil {
				t.Fatal(err)
			}
			log := captureRunErrors(t, func() {
				if err := d.recoverRuns(context.Background()); err != nil {
					t.Errorf("bad run blocks startup: %v", err)
				}
			})
			if !strings.Contains(log, want) {
				t.Errorf("recovery stderr %q lacks %q", log, want)
			}
			// Exercise the recovered run through the owner surface.
			b, err := d.handle(context.Background(), MethodRunStop, json.RawMessage(`{"run":"good"}`))
			if err != nil {
				t.Fatalf("healthy recovered run unavailable: %v", err)
			}
			if b.(runrec.Record).State != runrec.StateStopped {
				t.Fatalf("healthy stop: %+v", b)
			}
		})
	}
}

func TestStatusSurvivesRunListError(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	for _, id := range []string{"bad", "good"} {
		if _, err := store.Create(runrec.Record{ID: id, Project: p.Name, State: runrec.StateAwaitingApproval}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(store.RunDir("bad"), "run.json"), []byte("broken"), 0644); err != nil {
		t.Fatal(err)
	}
	log := captureRunErrors(t, func() {
		b, err := d.handle(context.Background(), "status", json.RawMessage(`{"project":"p"}`))
		if err != nil {
			t.Fatalf("run list error blocks status: %v", err)
		}
		raw, _ := json.Marshal(b)
		if !strings.Contains(string(raw), `"id":"good"`) {
			t.Errorf("healthy run missing from status: %s", raw)
		}
	})
	if !strings.Contains(log, "run record") {
		t.Errorf("status stderr missing list error: %q", log)
	}
}

func TestClosedRunManagerRefusesJobs(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	r, err := store.Create(runrec.Record{ID: "waiting", Project: p.Name, State: runrec.StateAwaitingApproval})
	if err != nil {
		t.Fatal(err)
	}
	d.runs.runs["waiting"] = &managedRun{engine: engine.Load(d.runDeps(p), p, r)}
	cancelled := make(chan struct{})
	release := make(chan struct{})
	d.runs.mu.Lock()
	busy := &managedRun{engine: d.runs.runs["waiting"].engine}
	d.runs.runs["busy"] = busy
	d.launchRun(busy, func(ctx context.Context) error {
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	})
	d.runs.mu.Unlock()
	closed := make(chan struct{})
	go func() { d.closeRuns(); close(closed) }()
	defer func() { close(release); <-closed }()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("close did not cancel active job")
	}
	if _, err := d.startRun(context.Background(), RunParams{Project: "p", Item: "S1"}); err == nil {
		t.Error("start accepted after close")
	}
	if _, err := d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: "waiting"}); err == nil {
		t.Error("owner job accepted after close")
	}
}

func TestCloseRefusesAStopWaitingForItsRole(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	run, err := store.Create(runrec.Record{ID: "waiting", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	r := &managedRun{engine: engine.Load(d.runDeps(p), p, run)}
	cancelled, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRole := func() { releaseOnce.Do(func() { close(release) }) }
	d.runs.mu.Lock()
	d.runs.runs["waiting"] = r
	d.launchRun(r, func(ctx context.Context) error {
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	})
	d.runs.mu.Unlock()
	result := make(chan error, 1)
	go func() {
		_, err := d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: "waiting"})
		result <- err
	}()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		releaseRole()
		t.Fatal("stop did not cancel role")
	}
	closed := make(chan struct{})
	go func() { d.closeRuns(); close(closed) }()
	defer func() { releaseRole(); <-closed }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		d.runs.mu.Lock()
		closing := d.runs.closed
		d.runs.mu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("close did not begin")
		}
		time.Sleep(time.Millisecond)
	}
	// Release the old job after shutdown has closed admission.
	releaseRole()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("waiting stop launched a new job during close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting stop did not return")
	}
}
