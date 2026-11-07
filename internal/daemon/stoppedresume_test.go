// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// S296 (*proposed*): resuming a stopped run, and ork run on its item.

const (
	stoppedDiff  = "diff --git a/a.go b/a.go\n-old\n+new\n"
	stoppedNames = ""
)

// treeOf is the fingerprint an implementer round records for the worktree the
// fake git shows (engine.treeContent).
func treeOf(diff, names string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(diff+"\x00"+names)))
}

// stoppedRig is a daemon with one stopped run, r1, of item S1, whose worktree
// is a real directory that the fake git accepts.
type stoppedRig struct {
	d      *Daemon
	store  *runrec.Store
	p      registry.Project
	root   string
	W      string
	mu     sync.Mutex
	checks []string // "command" of each check the engine ran
}

func (r *stoppedRig) ranChecks() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.checks...)
}

// stoppedDaemon builds the rig. change edits the stopped record before it is
// stored; managed puts it in the run manager as a daemon that has not
// restarted holds it. diff is what the worktree shows now.
func stoppedDaemon(t *testing.T, runner roundRunner, managed bool, diff string, change func(*runrec.Record), extra ...clitest.Route) *stoppedRig {
	t.Helper()
	return stoppedDaemonIn(t, t.TempDir(), runner, managed, diff, change, extra...)
}

// stoppedDaemonIn is stoppedDaemon in a root the test names, so an extra
// route can name the worktree: root/p/.worktrees/S1.
func stoppedDaemonIn(t *testing.T, root string, runner roundRunner, managed bool, diff string, change func(*runrec.Record), extra ...clitest.Route) *stoppedRig {
	t.Helper()
	P := filepath.Join(root, "p")
	W := filepath.Join(P, ".worktrees", "S1")
	if err := os.MkdirAll(W, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) []string { return append([]string{"-C"}, args...) }
	routes := []clitest.Route{
		{Tool: "git", Args: git(W, "rev-parse", "--show-toplevel"), Stdout: W + "\n"},
		{Tool: "git", Args: git(W, "symbolic-ref", "--short", "HEAD"), Stdout: "S1\n"},
		{Tool: "git", Args: git(W, "rev-parse", "--git-common-dir"), Stdout: filepath.Join(P, ".git") + "\n"},
		{Tool: "git", Args: git(P, "rev-parse", "--git-common-dir"), Stdout: filepath.Join(P, ".git") + "\n"},
		{Tool: "git", Args: git(W, "diff", "HEAD", "--binary", "--no-ext-diff"), Stdout: diff},
		{Tool: "git", Args: git(W, "ls-files", "--others", "--exclude-standard", "-z"), Stdout: stoppedNames},
	}
	d, store, p := runDaemonIn(t, root, runner, append(extra, routes...)...)
	rig := &stoppedRig{d: d, store: store, p: p, root: root, W: W}
	d.runs.deps.Check = func(_ context.Context, _, command string, _ time.Duration) (engine.CheckOutput, error) {
		rig.mu.Lock()
		rig.checks = append(rig.checks, command)
		rig.mu.Unlock()
		return engine.CheckOutput{OK: true, Output: "resume checks\n"}, nil
	}
	lineup := map[string][]runrec.LineupEntry{}
	for _, role := range []string{engine.RoleImplementer, engine.RoleReviewer, engine.RoleGate} {
		lineup[role] = []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet"}}
	}
	at := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	rec := runrec.Record{
		ID: "r1", Project: p.Name, Item: "S1", Worktree: W, Branch: "S1", State: runrec.StateStopped, StoppedFrom: runrec.StateImplement,
		Reason: "stopped by the owner: fix the check", Round: 3, Lineup: lineup, RelayRun: "relay-old",
		Roles: []runrec.RoleRecord{{Role: engine.RoleImplementer, Round: 3, Exit: "ok", StartedAt: at, EndedAt: at, TreeSHA256: treeOf(stoppedDiff, stoppedNames)}},
	}
	if change != nil {
		change(&rec)
	}
	r, err := store.Create(rec)
	if err != nil {
		t.Fatal(err)
	}
	if managed {
		d.runs.runs["r1"] = &managedRun{engine: engine.Load(d.runDeps(p), p, r)}
	}
	return rig
}

func (r *stoppedRig) setCheck(t *testing.T, command string) {
	t.Helper()
	got := request(t, r.d, MethodProjectsSet, `{"name":"p","check_command":"`+command+`"}`)
	if !strings.Contains(string(got), command) {
		t.Fatalf("projects.set: %s", got)
	}
}

func decodeRecord(t *testing.T, b json.RawMessage) runrec.Record {
	t.Helper()
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestRunRetryResumesAStoppedRun(t *testing.T) {
	// A stopped run the manager holds (no daemon restart since the stop), and a
	// grant of 4: the wire accepts both.
	rig := stoppedDaemon(t, retryRunner, true, stoppedDiff, nil)
	got := decodeRecord(t, request(t, rig.d, "run.retry", `{"run":"r1","rounds":4}`))
	if got.State != runrec.StateAwaitingApproval || got.Round != 4 || got.RoundLimit != 8 || got.StoppedFrom != "" || got.RelayRun != "relay-1" {
		t.Fatalf("resumed record: state %s round %d limit %d stopped_from %q relay %q", got.State, got.Round, got.RoundLimit, got.StoppedFrom, got.RelayRun)
	}
	if checks := rig.ranChecks(); len(checks) != 1 || checks[0] != "go test ./..." {
		t.Fatalf("checks after the resume: %v", checks)
	}
	waitIdle(t, rig.d, "r1")
	stored, err := rig.store.Open("r1")
	if err != nil || stored.Snapshot().State != runrec.StateAwaitingApproval {
		t.Fatalf("stored record: %v %v", stored.Snapshot().State, err)
	}
}

func TestRunRetryResumesAStoppedRunAfterARestart(t *testing.T) {
	// The manager does not hold the run: the daemon restarted, and recovery
	// skips a final run. The retry opens it from the store.
	rig := stoppedDaemon(t, retryRunner, false, stoppedDiff, nil)
	if rig.d.runs.runs["r1"] != nil {
		t.Fatal("the run is managed")
	}
	got := decodeRecord(t, request(t, rig.d, "run.retry", `{"run":"r1"}`))
	if got.State != runrec.StateAwaitingApproval || got.RoundLimit != 6 {
		t.Fatalf("resumed record: state %s limit %d", got.State, got.RoundLimit)
	}
	if rig.d.runs.runs["r1"] == nil {
		t.Error("the resumed run is not managed")
	}
}

func TestRunRetryResumeIsBusyThenUnfinished(t *testing.T) {
	g := &roleGate{started: make(chan struct{}, 8), release: make(chan struct{})}
	rig := stoppedDaemon(t, g.run, false, stoppedDiff, func(r *runrec.Record) { r.Roles = nil })
	t.Cleanup(g.open)
	// No implementer round has run, so the resume enters implement and the
	// role blocks on the gate: the run is busy.
	got := decodeRecord(t, request(t, rig.d, "run.retry", `{"run":"r1","no_wait":true}`))
	if got.State == runrec.StateStopped {
		t.Fatalf("a no-wait reply must come after the resume is applied: %s", got.State)
	}
	<-g.started
	if _, err := rig.d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`)); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("a second retry of the running run: %v", err)
	}
	// The record is unfinished again, so projects.set refuses the field the run reads.
	_, err := rig.d.handle(context.Background(), MethodProjectsSet, json.RawMessage(`{"name":"p","check_command":"make verify"}`))
	var wire *ipc.Error
	if err == nil || !errorsAs(err, &wire) || wire.Code != CodeProjectBusy || !strings.Contains(wire.Message, "r1") {
		t.Fatalf("projects.set while the resumed run is unfinished: %v", err)
	}
	g.open()
	waitState(t, rig.store, "r1", runrec.StateAwaitingApproval)
}

func errorsAs(err error, target **ipc.Error) bool {
	for err != nil {
		if e, ok := err.(*ipc.Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestRunRetryOfAStoppedRunUsesTheCurrentProject(t *testing.T) {
	for _, managed := range []bool{true, false} {
		t.Run(fmt.Sprintf("managed=%v", managed), func(t *testing.T) {
			rig := stoppedDaemon(t, retryRunner, managed, stoppedDiff, nil)
			// Fixing the check of a project whose only run is stopped is allowed.
			rig.setCheck(t, "make verify")
			request(t, rig.d, "run.retry", `{"run":"r1"}`)
			if checks := rig.ranChecks(); len(checks) != 1 || checks[0] != "make verify" {
				t.Fatalf("the resumed checks ran %v, want the fixed command", checks)
			}
			if rec, _ := rig.store.Open("r1"); rec.Snapshot().Checks[0].Command != "make verify" {
				t.Fatalf("checks[] row: %+v", rec.Snapshot().Checks)
			}
		})
	}
}

func TestProjectsSetIsNotBlockedByAStoppedRun(t *testing.T) {
	for _, managed := range []bool{true, false} {
		rig := stoppedDaemon(t, nil, managed, stoppedDiff, nil)
		rig.setCheck(t, "make verify")
		rig.setCheck(t, "make again")
	}
}

func TestRunRetryRefusesAStoppedRunItCannotResume(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*runrec.Record)
		want   string
	}{
		"stopped at setup":     {func(r *runrec.Record) { r.StoppedFrom = runrec.StateSetup }, "stopped during setup"},
		"stopped after commit": {func(r *runrec.Record) { r.Commit = &runrec.Commit{SHA: "abc"} }, "commit"},
		"committed":            {func(r *runrec.Record) { r.State = runrec.StateCommitted; r.StoppedFrom = "" }, "committed"},
	} {
		for _, managed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s managed=%v", name, managed), func(t *testing.T) {
				rig := stoppedDaemon(t, nil, managed, stoppedDiff, tc.change)
				_, err := rig.d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
				if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "not paused") || strings.Contains(err.Error(), "\n") {
					t.Fatalf("retry: %v", err)
				}
			})
		}
	}
	t.Run("worktree gone", func(t *testing.T) {
		rig := stoppedDaemon(t, nil, false, stoppedDiff, nil)
		if err := os.RemoveAll(rig.W); err != nil {
			t.Fatal(err)
		}
		_, err := rig.d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
		var wire *ipc.Error
		if err == nil || !errorsAs(err, &wire) || wire.Code != CodeRefused || !strings.Contains(err.Error(), "worktree") {
			t.Fatalf("retry: %v", err)
		}
	})
	t.Run("the engine's refusal is a refusal", func(t *testing.T) {
		rig := stoppedDaemon(t, nil, true, stoppedDiff, nil)
		_, err := rig.d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1","rounds":-3}`))
		var wire *ipc.Error
		if err == nil || !errorsAs(err, &wire) || wire.Code != CodeRefused || !strings.Contains(err.Error(), "rounds") {
			t.Fatalf("retry: %v", err)
		}
	})
}

func TestRunRetryOfAStoppedRunWaitsForNoOtherRunOfTheItem(t *testing.T) {
	rig := stoppedDaemon(t, nil, false, stoppedDiff, nil)
	if _, err := rig.store.Create(runrec.Record{ID: "r2", Project: "p", Item: "S1", Worktree: rig.W, State: runrec.StatePaused}); err != nil {
		t.Fatal(err)
	}
	_, err := rig.d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
	if err == nil || !strings.Contains(err.Error(), "r2") || !strings.Contains(err.Error(), "unfinished") {
		t.Fatalf("retry beside an unfinished run of the item: %v", err)
	}
	if rec, _ := rig.store.Open("r1"); rec.Snapshot().State != runrec.StateStopped {
		t.Fatalf("a refused retry changed the run: %s", rec.Snapshot().State)
	}
}

func TestStatusNamesTheResumeOfAStoppedRun(t *testing.T) {
	rig := stoppedDaemon(t, nil, false, stoppedDiff, nil)
	b := string(request(t, rig.d, "status", `{"project":"p"}`))
	if want := `stopped by the owner: fix the check; next: ork retry r1 [--rounds N] or ork dismiss r1`; !strings.Contains(b, want) {
		t.Fatalf("status lacks %q: %s", want, b)
	}
	show := string(request(t, rig.d, "run.show", `{"run":"r1"}`))
	if !strings.Contains(show, `"next":"ork retry r1 [--rounds N] or ork dismiss r1"`) {
		t.Fatalf("run.show lacks the next command: %s", show)
	}
	// A run that cannot resume names none.
	rig = stoppedDaemon(t, nil, false, stoppedDiff, func(r *runrec.Record) { r.StoppedFrom = runrec.StateSetup })
	if b := string(request(t, rig.d, "status", `{"project":"p"}`)); strings.Contains(b, "next: ork retry") {
		t.Fatalf("a setup-stopped run names a retry: %s", b)
	}
	if show := string(request(t, rig.d, "run.show", `{"run":"r1"}`)); strings.Contains(show, `"next"`) {
		t.Fatalf("run.show of a setup-stopped run names a retry: %s", show)
	}
}

// ---- ork run and ork wave start on an item with a stopped run ----
// Owner decision 2026-11-07 (S296 Q1, option b): resume when the worktree's
// tree hash equals the last implementer round's tree_sha256, otherwise start
// fresh and say so.

func TestRunStartResumesTheStoppedRunWhenTheTreeIsUnchanged(t *testing.T) {
	for _, managed := range []bool{true, false} {
		t.Run(fmt.Sprintf("managed=%v", managed), func(t *testing.T) {
			rig := stoppedDaemon(t, retryRunner, managed, stoppedDiff, nil)
			b := request(t, rig.d, "run.start", `{"project":"p","item":"S1"}`)
			var reply RunReply
			if err := json.Unmarshal(b, &reply); err != nil {
				t.Fatal(err)
			}
			if reply.ID != "r1" || reply.State == runrec.StateStopped {
				t.Fatalf("run.start answered %s in state %s, want the resumed run r1", reply.ID, reply.State)
			}
			for _, want := range []string{"resumed stopped run r1", "implementer round 3", "granted 2 more implementer rounds", "uses the stopped run's lineup, budgets and stop-after"} {
				if !strings.Contains(reply.Message, want) {
					t.Errorf("message %q lacks %q", reply.Message, want)
				}
			}
			waitIdle(t, rig.d, "r1")
			stored := waitState(t, rig.store, "r1", runrec.StateAwaitingApproval)
			if stored.RoundLimit != 6 || stored.Round != 4 {
				t.Errorf("round %d limit %d", stored.Round, stored.RoundLimit)
			}
			if all, _ := rig.store.List(); len(all) != 1 {
				t.Errorf("%d runs in the store, want only r1", len(all))
			}
			if checks := rig.ranChecks(); len(checks) != 1 {
				t.Errorf("checks: %v", checks)
			}
		})
	}
}

func TestRunStartStartsFreshAndSaysWhyWhenTheTreeChanged(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*runrec.Record)
		diff   string
		want   string
	}{
		"the worktree changed":     {nil, stoppedDiff + "+more\n", "changed since implementer round 3"},
		"no fingerprint was kept":  {func(r *runrec.Record) { r.Roles[0].TreeSHA256 = "" }, stoppedDiff, "no tree fingerprint"},
		"no implementer round ran": {func(r *runrec.Record) { r.Roles = nil }, stoppedDiff, "no implementer round ran to compare with"},
	} {
		t.Run(name, func(t *testing.T) {
			rig := stoppedDaemon(t, retryRunner, false, tc.diff, tc.change)
			b := request(t, rig.d, "run.start", `{"project":"p","item":"S1"}`)
			var reply RunReply
			if err := json.Unmarshal(b, &reply); err != nil {
				t.Fatal(err)
			}
			if reply.ID == "r1" || reply.ID == "" {
				t.Fatalf("run.start answered %q, want a new run", reply.ID)
			}
			for _, want := range []string{"started a new run at implementer round 1 in the same worktree; stopped run r1 not resumed: ", tc.want} {
				if !strings.Contains(reply.Message, want) {
					t.Errorf("message %q lacks %q", reply.Message, want)
				}
			}
			waitIdle(t, rig.d, reply.ID)
			if old, _ := rig.store.Open("r1"); old.Snapshot().State != runrec.StateStopped {
				t.Errorf("the stopped run changed: %s", old.Snapshot().State)
			}
		})
	}
}

func TestRunStartNeverResumesADismissedOrUnresumableRun(t *testing.T) {
	for name, change := range map[string]func(*runrec.Record){
		"dismissed":        func(r *runrec.Record) { r.DismissedAt = time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC) },
		"stopped at setup": func(r *runrec.Record) { r.StoppedFrom = runrec.StateSetup },
		"committed":        func(r *runrec.Record) { r.State = runrec.StateCommitted; r.StoppedFrom = "" },
	} {
		t.Run(name, func(t *testing.T) {
			rig := stoppedDaemon(t, retryRunner, false, stoppedDiff, change)
			b := request(t, rig.d, "run.start", `{"project":"p","item":"S1"}`)
			var reply RunReply
			if err := json.Unmarshal(b, &reply); err != nil {
				t.Fatal(err)
			}
			if reply.ID == "r1" || reply.ID == "" || reply.Message != "" {
				t.Fatalf("run.start answered %q with message %q, want a new run and nothing to say", reply.ID, reply.Message)
			}
			waitIdle(t, rig.d, reply.ID)
		})
	}
}

func TestRunStartResumesOnlyTheNewestRunOfTheItem(t *testing.T) {
	rig := stoppedDaemon(t, retryRunner, false, stoppedDiff, nil)
	// A newer run of the item that landed: the stopped one is not the newest.
	if _, err := rig.store.Create(runrec.Record{ID: "r2", Project: "p", Item: "S1", Worktree: "elsewhere", State: runrec.StateCommitted}); err != nil {
		t.Fatal(err)
	}
	b := request(t, rig.d, "run.start", `{"project":"p","item":"S1"}`)
	var reply RunReply
	if err := json.Unmarshal(b, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.ID == "r1" || reply.ID == "" || reply.Message != "" {
		t.Fatalf("run.start answered %q with message %q", reply.ID, reply.Message)
	}
	waitIdle(t, rig.d, reply.ID)
}

func TestRunStartResumeKeepsTheOwnerRules(t *testing.T) {
	t.Run("another run of the item is unfinished", func(t *testing.T) {
		rig := stoppedDaemon(t, retryRunner, false, stoppedDiff, nil)
		if _, err := rig.store.Create(runrec.Record{ID: "r2", Project: "p", Item: "S1", Worktree: rig.W, State: runrec.StatePaused}); err != nil {
			t.Fatal(err)
		}
		_, err := rig.d.handle(context.Background(), "run.start", json.RawMessage(`{"project":"p","item":"S1"}`))
		if err == nil || !strings.Contains(err.Error(), "r2 already owns") {
			t.Fatalf("run.start: %v", err)
		}
	})
	t.Run("no free slot", func(t *testing.T) {
		rig := stoppedDaemon(t, retryRunner, false, stoppedDiff, nil)
		f, err := registry.Load(rig.d.registryPath())
		if err != nil {
			t.Fatal(err)
		}
		f.Projects[0].Cap = 1
		if err := registry.Save(rig.d.registryPath(), f); err != nil {
			t.Fatal(err)
		}
		rig.d.runs.runs["busy"] = &managedRun{busy: true, engine: engine.Load(rig.d.runDeps(rig.p), rig.p, mustCreate(t, rig.store, runrec.Record{ID: "busy", Project: "p", Item: "S9", Worktree: "w9", State: runrec.StateImplement}))}
		_, err = rig.d.handle(context.Background(), "run.start", json.RawMessage(`{"project":"p","item":"S1"}`))
		var wire *ipc.Error
		if err == nil || !errorsAs(err, &wire) || wire.Code != CodeRefused || !strings.Contains(err.Error(), "stopped run r1 has work in its worktree and no slot is free; resume it now with ork retry r1, or ork dismiss r1 to start a new run") {
			t.Fatalf("run.start with no slot: %v", err)
		}
		if rec, _ := rig.store.Open("r1"); rec.Snapshot().State != runrec.StateStopped {
			t.Fatalf("a refused start changed the run: %s", rec.Snapshot().State)
		}
	})
}

func mustCreate(t *testing.T, store *runrec.Store, rec runrec.Record) *runrec.Run {
	t.Helper()
	r, err := store.Create(rec)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWaveStartResumesAndSaysSo(t *testing.T) {
	rig := stoppedDaemon(t, retryRunner, false, stoppedDiff, nil)
	b := request(t, rig.d, "wave.start", `{"items":[{"project":"p","item":"S1"}]}`)
	var result WaveResult
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Error != nil || result.Items[0].Run == nil || result.Items[0].Run.ID != "r1" || !strings.Contains(result.Items[0].Message, "resumed stopped run r1") {
		t.Fatalf("wave result: %s", b)
	}
	waitIdle(t, rig.d, "r1")
}

func TestWaveStartStartsFreshAndSaysWhy(t *testing.T) {
	rig := stoppedDaemon(t, retryRunner, false, stoppedDiff+"+more\n", nil)
	b := request(t, rig.d, "wave.start", `{"items":[{"project":"p","item":"S1"}]}`)
	var result WaveResult
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Run == nil || result.Items[0].Run.ID == "r1" || !strings.Contains(result.Items[0].Message, "changed since implementer round 3") {
		t.Fatalf("wave result: %s", b)
	}
	waitIdle(t, rig.d, result.Items[0].Run.ID)
}

// The reply of run.start and the wave item carry the message as "message"; a
// reader of the bare record still decodes the reply (runrec.Record ignores it).
func TestRunReplyWireShape(t *testing.T) {
	b, err := json.Marshal(RunReply{Record: runrec.Record{ID: "r1", State: runrec.StateChecks}, Message: "resumed stopped run r1"})
	if err != nil {
		t.Fatal(err)
	}
	var plain runrec.Record
	if err := json.Unmarshal(b, &plain); err != nil || plain.ID != "r1" || !strings.Contains(string(b), `"message":"resumed stopped run r1"`) {
		t.Fatalf("%s: %v", b, err)
	}
	item, _ := json.Marshal(WaveItemResult{Item: "S1", Message: "m"})
	if !strings.Contains(string(item), `"message":"m"`) {
		t.Fatalf("wave item: %s", item)
	}
	item, _ = json.Marshal(WaveItemResult{Item: "S1"})
	if strings.Contains(string(item), "message") {
		t.Fatalf("an empty message is sent: %s", item)
	}
}

// ---- review round 1 ----

// A resume that the engine would refuse is never reported as a resume: the
// start checks what the resume checks first (a role still running, a worktree
// on another branch), and when the resume is refused all the same the start
// answers the refusal.
func TestRunStartNeverClaimsAResumeThatDidNotHappen(t *testing.T) {
	runStart := func(rig *stoppedRig) (RunReply, error) {
		b, err := rig.d.handle(context.Background(), "run.start", json.RawMessage(`{"project":"p","item":"S1"}`))
		if err != nil {
			return RunReply{}, err
		}
		return b.(RunReply), nil
	}
	for name, tc := range map[string]struct {
		change func(*runrec.Record)
		why    string
	}{
		"the tree matches but the branch differs": {func(r *runrec.Record) { r.Branch = "other" }, "does not match the run"},
		"the tree matches but a role is running": {func(r *runrec.Record) {
			r.Roles = append(r.Roles, runrec.RoleRecord{Role: engine.RoleReviewer, Round: 3, StartedAt: time.Date(2026, 11, 7, 9, 5, 0, 0, time.UTC)})
		}, "has a role still running"},
	} {
		t.Run(name, func(t *testing.T) {
			rig := stoppedDaemon(t, retryRunner, false, stoppedDiff, tc.change)
			reply, err := runStart(rig)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(reply.Message, "resumed") && !strings.Contains(reply.Message, "not resumed") || reply.ID == "r1" {
				t.Fatalf("a refused resume was reported: id %s message %q", reply.ID, reply.Message)
			}
			if !strings.Contains(reply.Message, "started a new run at implementer round 1 in the same worktree; stopped run r1 not resumed: ") || !strings.Contains(reply.Message, tc.why) || strings.Contains(reply.Message, "run r1 run r1") {
				t.Fatalf("message %q", reply.Message)
			}
			waitIdle(t, rig.d, reply.ID)
			if old, _ := rig.store.Open("r1"); old.Snapshot().State != runrec.StateStopped {
				t.Errorf("the stopped run changed: %s", old.Snapshot().State)
			}
		})
	}
	t.Run("the engine refuses the resume after the check passed", func(t *testing.T) {
		// The branch reads right for the start's check and wrong for the resume.
		root := t.TempDir()
		W := filepath.Join(root, "p", ".worktrees", "S1")
		rig := stoppedDaemonIn(t, root, retryRunner, false, stoppedDiff, nil, clitest.Route{
			Tool: "git", Args: []string{"-C", W, "symbolic-ref", "--short", "HEAD"},
			Seq: []clitest.Reply{{Stdout: "S1\n"}, {Stdout: "other\n"}},
		})
		_, err := runStart(rig)
		var wire *ipc.Error
		if err == nil || !errorsAs(err, &wire) || wire.Code != CodeRefused || !strings.Contains(err.Error(), "does not match the run") {
			t.Fatalf("run.start: %v", err)
		}
		if old, _ := rig.store.Open("r1"); old.Snapshot().State != runrec.StateStopped {
			t.Errorf("the stopped run changed: %s", old.Snapshot().State)
		}
	})
}

func TestWaveStartResumedRunJoinsTheNewWave(t *testing.T) {
	g := &roleGate{started: make(chan struct{}, 8), release: make(chan struct{})}
	rig := stoppedDaemon(t, g.run, false, stoppedDiff, func(r *runrec.Record) { r.Wave = "wave-old" })
	t.Cleanup(g.open)
	var result WaveResult
	if err := json.Unmarshal(request(t, rig.d, "wave.start", `{"items":[{"project":"p","item":"S1"}]}`), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Run == nil || result.Items[0].Run.ID != "r1" {
		t.Fatalf("wave result: %+v", result)
	}
	<-g.started // the resumed run is at its review round
	if got := result.Items[0].Run.Wave; got != result.Wave {
		t.Errorf("the reply's run is in wave %q, want %q", got, result.Wave)
	}
	if got := rig.d.runs.runs["r1"].engine.Snapshot().Wave; got != result.Wave {
		t.Errorf("the resumed run is in wave %q, want the new wave %q", got, result.Wave)
	}
	stop := request(t, rig.d, "wave.stop", `{"wave":"`+result.Wave+`"}`)
	var stopped WaveResult
	if err := json.Unmarshal(stop, &stopped); err != nil {
		t.Fatal(err)
	}
	if len(stopped.Items) != 1 || stopped.Items[0].Error != nil {
		t.Fatalf("wave.stop of the new wave: %s", stop)
	}
	waitState(t, rig.store, "r1", runrec.StateStopped)
	// The old wave no longer holds the run.
	if _, err := rig.d.handle(context.Background(), "wave.stop", json.RawMessage(`{"wave":"wave-old"}`)); err != nil {
		var wire *ipc.Error
		if !errorsAs(err, &wire) {
			t.Fatal(err)
		}
	}
}

func TestRunStartResumedRunGetsAWaveOfItsOwn(t *testing.T) {
	rig := stoppedDaemon(t, retryRunner, false, stoppedDiff, func(r *runrec.Record) { r.Wave = "wave-old" })
	b := request(t, rig.d, "run.start", `{"project":"p","item":"S1"}`)
	var reply RunReply
	if err := json.Unmarshal(b, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.ID != "r1" || !strings.HasPrefix(reply.Wave, "wave-") || reply.Wave == "wave-old" {
		t.Fatalf("run %s wave %q, want r1 in a new wave", reply.ID, reply.Wave)
	}
	waitIdle(t, rig.d, "r1")
	if got := waitState(t, rig.store, "r1", runrec.StateAwaitingApproval).Wave; got != reply.Wave {
		t.Errorf("stored wave %q, want %q", got, reply.Wave)
	}
}

// A finding action holds the run's record lock while it writes through its
// own handle; a start must not open the record beside it (lost update).
func TestRunStartDoesNotResumeWhileAnActionHoldsTheRecord(t *testing.T) {
	rig := stoppedDaemon(t, retryRunner, false, stoppedDiff, nil)
	lock := rig.d.runRecordLock("r1")
	lock.Lock()
	_, err := rig.d.handle(context.Background(), "run.start", json.RawMessage(`{"project":"p","item":"S1"}`))
	var wire *ipc.Error
	if err == nil || !errorsAs(err, &wire) || wire.Code != CodeRefused || !strings.Contains(err.Error(), "stopped run r1") || !strings.Contains(err.Error(), "another action") {
		lock.Unlock()
		t.Fatalf("run.start while the record is held: %v", err)
	}
	lock.Unlock()
	if all, _ := rig.store.List(); len(all) != 1 {
		t.Errorf("%d runs after a refused start, want 1", len(all))
	}
	// Released, the same start resumes the run.
	var reply RunReply
	if err := json.Unmarshal(request(t, rig.d, "run.start", `{"project":"p","item":"S1"}`), &reply); err != nil || reply.ID != "r1" {
		t.Fatalf("start after release: %+v %v", reply.ID, err)
	}
	waitIdle(t, rig.d, "r1")
}

// The tree is fingerprinted before the start takes the manager lock; under
// the lock the newest run and its UpdatedAt must still be the probed ones.
func TestAutoResumeRefusesWhenTheRunChangedSinceTheProbe(t *testing.T) {
	rig := stoppedDaemon(t, retryRunner, false, stoppedDiff, nil)
	ctx := context.Background()
	probe := rig.d.probeResume(ctx, rig.p, "S1", rig.W)
	if probe == nil || probe.id != "r1" || !probe.match || probe.canErr != nil {
		t.Fatalf("probe %+v", probe)
	}
	call := func(probe *resumeProbe) (string, *resumeLaunch, error) {
		f, err := rig.d.loadRegistry()
		if err != nil {
			t.Fatal(err)
		}
		rig.d.runs.mu.Lock()
		defer rig.d.runs.mu.Unlock()
		stored, _ := rig.d.runs.deps.Store.List()
		return rig.d.autoResumeLocked(ctx, f, rig.p, "S1", rig.W, "wave-x", stored, probe)
	}
	run, _ := rig.store.Open("r1")
	if err := run.Update(func(r *runrec.Record) { r.Reason = "changed by another action" }); err != nil {
		t.Fatal(err)
	}
	if _, resumed, err := call(probe); err == nil || resumed != nil || !strings.Contains(err.Error(), "changed while") {
		t.Fatalf("a stale probe: %v", err)
	}
	// No probe for a run that now exists is stale too.
	if _, resumed, err := call(nil); err == nil || resumed != nil || !strings.Contains(err.Error(), "changed while") {
		t.Fatalf("no probe: %v", err)
	}
	// A fresh probe is accepted.
	probe = rig.d.probeResume(ctx, rig.p, "S1", rig.W)
	if msg, resumed, err := call(probe); err != nil || resumed == nil || !strings.Contains(msg, "resumed stopped run r1") {
		t.Fatalf("a fresh probe: %q %v", msg, err)
	}
	waitIdle(t, rig.d, "r1")
}

func TestRunRetryAndStatusSkipAStoppedRunThatANewerRunSupersedes(t *testing.T) {
	rig := stoppedDaemon(t, nil, false, stoppedDiff, nil)
	time.Sleep(2 * time.Millisecond)
	if _, err := rig.store.Create(runrec.Record{ID: "r2", Project: "p", Item: "S1", Worktree: rig.W, Branch: "S1", State: runrec.StateStopped, StoppedFrom: runrec.StateReview, Reason: "stopped by the owner: later"}); err != nil {
		t.Fatal(err)
	}
	_, err := rig.d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
	if err == nil || !strings.Contains(err.Error(), "run r1 is superseded by run r2") || !strings.Contains(err.Error(), "same worktree") {
		t.Fatalf("retry of the older run: %v", err)
	}
	b := string(request(t, rig.d, "status", `{"project":"p"}`))
	if strings.Contains(b, "ork retry r1") || !strings.Contains(b, "ork retry r2 [--rounds N] or ork dismiss r2") {
		t.Fatalf("status names the superseded run or lacks the newer one: %s", b)
	}
	if show := string(request(t, rig.d, "run.show", `{"run":"r1"}`)); strings.Contains(show, `"next"`) {
		t.Fatalf("run.show names a retry of the superseded run: %s", show)
	}
	if show := string(request(t, rig.d, "run.show", `{"run":"r2"}`)); !strings.Contains(show, `"next":"ork retry r2 [--rounds N] or ork dismiss r2"`) {
		t.Fatalf("run.show of the newer run: %s", show)
	}
}
