// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

type landingSource struct {
	stubSource
	git string
}

func (s *landingSource) Fetch(ctx context.Context, p registry.Project, part source.Part) (any, error) {
	if part != source.Head {
		return s.stubSource.Fetch(ctx, p, part)
	}
	r, e := clirun.Run(ctx, clirun.Call{Bin: s.git, Args: []string{"-C", p.Path, "rev-parse", "HEAD"}})
	return strings.TrimSpace(string(r.Stdout)), e
}
func landingDaemon(t *testing.T, exit int, branchError string) (*Daemon, *runrec.Store, registry.Project, *clitest.Fake) {
	t.Helper()
	old, store, p := runDaemon(t, nil)
	fake := clitest.Install(t,
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "rev-parse", "HEAD"}, Seq: []clitest.Reply{{Stdout: "h1"}, {Stdout: "h1"}, {Stdout: "h2"}, {Stdout: "h2"}}},
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "merge-base", "--is-ancestor", "s63", "HEAD"}, Exit: exit},
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "worktree", "remove", p.Path + "/.worktrees/s63"}},
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "branch", "-d", "s63"}, Exit: map[bool]int{true: 1}[branchError != ""], Stderr: branchError})
	cfg := old.cfg
	deps := *cfg.Engine
	deps.GitBin = fake.Bin("git")
	cfg.Engine = &deps
	cfg.Source = &landingSource{git: fake.Bin("git")}
	d := New(cfg)
	t.Cleanup(d.cache.Close)
	t.Cleanup(d.closeRuns)
	return d, store, p, fake
}
func committedRun(t *testing.T, store *runrec.Store, p registry.Project, id, state string) {
	t.Helper()
	_, e := store.Create(runrec.Record{ID: id, Project: p.Name, Item: "S63", State: state, Branch: "s63", Worktree: p.Path + "/.worktrees/s63"})
	if e != nil {
		t.Fatal(e)
	}
}
func refreshHead(t *testing.T, d *Daemon, p registry.Project) {
	t.Helper()
	if e := d.cache.Refresh(context.Background(), p, source.Head); e != nil {
		t.Fatal(e)
	}
}
func mergeCalls(f *clitest.Fake) int {
	n := 0
	for _, c := range f.CallsTo("git") {
		if len(c.Args) > 2 && c.Args[2] == "merge-base" {
			n++
		}
	}
	return n
}
func TestMergeCheckRunsOnlyWhenHeadChanged(t *testing.T) {
	d, s, p, f := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	for i, want := range []int{1, 1, 2, 2} {
		refreshHead(t, d, p)
		if n := mergeCalls(f); n != want {
			t.Fatalf("refresh %d merge checks=%d, want %d", i, n, want)
		}
	}
}
func TestLandedIsTrueOnlyForAMergedBranch(t *testing.T) {
	for _, exit := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(exit), func(t *testing.T) {
			d, s, p, _ := landingDaemon(t, exit, "")
			committedRun(t, s, p, "run", "committed")
			refreshHead(t, d, p)
			r := d.engineRuns(p.Name)[0]
			if exit == 2 {
				if r.Merged != nil {
					t.Fatal("git error must leave merge state unknown")
				}
				return
			}
			if r.Merged == nil || *r.Merged != (exit == 0) {
				t.Fatalf("merged=%v, exit=%d", r.Merged, exit)
			}
		})
	}
}
func TestMergedBranchIsOfferedNotRemoved(t *testing.T) {
	d, s, p, f := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	refreshHead(t, d, p)
	r := d.engineRuns(p.Name)[0]
	if r.Merged == nil || !*r.Merged {
		t.Fatal("merged cleanup offer missing")
	}
	for _, c := range f.CallsTo("git") {
		if c.Args[2] != "rev-parse" && c.Args[2] != "merge-base" {
			t.Fatalf("unconfirmed write: %v", c)
		}
	}
	if _, e := s.Open("run"); e != nil {
		t.Fatal(e)
	}
}
func cleanupRequest(d *Daemon) error {
	_, e := d.handle(context.Background(), "run.cleanup", json.RawMessage(`{"run":"run"}`))
	return e
}
func TestCleanupRemovesWorktreeThenBranch(t *testing.T) {
	d, s, p, f := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	refreshHead(t, d, p)
	before := len(f.CallsTo("git"))
	if e := cleanupRequest(d); e != nil {
		t.Fatal(e)
	}
	got := f.CallsTo("git")[before:]
	want := []clitest.Call{{Tool: "git", Args: []string{"-C", p.Path, "worktree", "remove", p.Path + "/.worktrees/s63"}}, {Tool: "git", Args: []string{"-C", p.Path, "branch", "-d", "s63"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup calls=%v, want %v", got, want)
	}
}
func TestCleanupRefusesUnmergedOrActiveRuns(t *testing.T) {
	for _, state := range []string{"committed", "implement", "stopped"} {
		t.Run(state, func(t *testing.T) {
			d, s, p, f := landingDaemon(t, 1, "")
			committedRun(t, s, p, "run", state)
			refreshHead(t, d, p)
			before := len(f.CallsTo("git"))
			e := cleanupRequest(d)
			wire, ok := e.(*ipc.Error)
			if !ok || wire.Code != CodeBadParams {
				t.Fatalf("error=%v, want bad_params", e)
			}
			if len(f.CallsTo("git")) != before {
				t.Fatal("refused cleanup ran git")
			}
		})
	}
}
func TestCleanupSurfacesGitErrorVerbatim(t *testing.T) {
	const message = "error: branch is checked out elsewhere\n"
	d, s, p, _ := landingDaemon(t, 0, message)
	committedRun(t, s, p, "run", "committed")
	refreshHead(t, d, p)
	if e := cleanupRequest(d); e == nil || e.Error() != message {
		t.Fatalf("error=%q, want %q", e, message)
	}
	if _, e := s.Open("run"); e != nil {
		t.Fatal("failed cleanup lost run record", e)
	}
}
func TestPruneNeverRemovesUnlandedOrActiveRuns(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 1, "")
	committedRun(t, s, p, "active", "implement")
	for i := 0; i < 102; i++ {
		committedRun(t, s, p, fmt.Sprintf("run%03d", i), "committed")
	}
	refreshHead(t, d, p)
	all, e := s.List()
	if e != nil || len(all) != 103 {
		t.Fatalf("unlanded/active runs pruned: count=%d err=%v", len(all), e)
	}
}
func TestPruneAfterMergeCheckKeepsLatestHundred(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	for i := 0; i < 102; i++ {
		committedRun(t, s, p, fmt.Sprintf("run%03d", i), "committed")
	}
	refreshHead(t, d, p)
	all, e := s.List()
	if e != nil || len(all) != 100 {
		t.Fatalf("finished runs=%d err=%v, want 100", len(all), e)
	}
	if all[0].Snapshot().ID != "run002" {
		t.Fatal("did not prune oldest runs")
	}
}

func TestRefreshReadsOnlyHeadPerProject(t *testing.T) {
	d, s, p, f := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	refreshHead(t, d, p)
	before := len(f.CallsTo("git"))
	if _, err := d.refresh(context.Background(), RefreshParams{Project: p.Name}); err != nil {
		t.Fatal(err)
	}
	got := f.CallsTo("git")[before:]
	want := []clitest.Call{{Tool: "git", Args: []string{"-C", p.Path, "rev-parse", "HEAD"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unchanged refresh git calls=%v, want %v", got, want)
	}
}
func TestPruneAfterRecoveryKeepsUnknownBranches(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	for i := 0; i < 102; i++ {
		committedRun(t, s, p, fmt.Sprintf("run%03d", i), "committed")
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	all, err := s.List()
	if err != nil || len(all) != 102 {
		t.Fatalf("unknown branches after recovery=%d err=%v", len(all), err)
	}
}
func TestMergeDetectionScopesCommittedRunsToProject(t *testing.T) {
	d, s, p, f := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	committedRun(t, s, p, "active", "implement")
	other := registry.Project{Name: "other", Path: p.Path + "/other"}
	committedRun(t, s, other, "other", "committed")
	refreshHead(t, d, p)
	if mergeCalls(f) != 1 {
		t.Fatalf("merge checks=%d, want one committed run in this project", mergeCalls(f))
	}
	r := d.engineRuns(p.Name)[0]
	if r.Worktree != p.Path+"/.worktrees/s63" {
		t.Fatalf("worktree missing from display: %+v", r)
	}
	if d.engineRuns(other.Name)[0].Merged != nil {
		t.Fatal("other project merge state changed")
	}
}
func TestCleanupStopsAfterWorktreeError(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	refreshHead(t, d, p)
	const message = "fatal: worktree contains modified files\n"
	fake := clitest.Install(t, clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "worktree", "remove", p.Path + "/.worktrees/s63"}, Exit: 128, Stderr: message})
	d.runs.deps.GitBin = fake.Bin("git")
	if err := cleanupRequest(d); err == nil || err.Error() != message {
		t.Fatalf("error=%q, want %q", err, message)
	}
	if len(fake.CallsTo("git")) != 1 {
		t.Fatalf("cleanup continued after worktree error: %v", fake.CallsTo("git"))
	}
	if _, err := s.Open("run"); err != nil {
		t.Fatal(err)
	}
}

// blockedGitMain runs only in a fake git helper process, before clitest.Main.
func blockedGitMain() {
	if filepath.Base(os.Args[0]) != "blocked-git" {
		return
	}
	dir := os.Getenv("ORK_BLOCKED_GIT")
	if err := os.WriteFile(filepath.Join(dir, "started"), []byte("started"), 0600); err != nil {
		os.Exit(2)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
			os.Exit(0)
		}
		time.Sleep(5 * time.Millisecond)
	}
	os.Exit(2)
}
func blockedGit(t *testing.T, d *Daemon) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "blocked-git")
	if err := os.Symlink(exe, bin); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORK_BLOCKED_GIT", dir)
	d.runs.deps.GitBin = bin
	release := func() {
		if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(release)
	return dir, release
}
func TestLandingGitDoesNotBlockSnapshots(t *testing.T) {
	for _, operation := range []string{"detect", "cleanup"} {
		t.Run(operation, func(t *testing.T) {
			d, s, p, _ := landingDaemon(t, 0, "")
			committedRun(t, s, p, "run", "committed")
			if operation == "cleanup" {
				refreshHead(t, d, p)
			}
			dir, release := blockedGit(t, d)
			done := make(chan error, 1)
			go func() {
				if operation == "cleanup" {
					done <- cleanupRequest(d)
				} else {
					d.checkMerged(context.Background(), p, "changed")
					done <- nil
				}
			}()
			defer func() {
				release()
				if err := <-done; err != nil {
					t.Error(err)
				}
			}()
			waitFor(t, "blocked git started", func() bool { _, err := os.Stat(filepath.Join(dir, "started")); return err == nil })
			snapshots := make(chan struct{})
			go func() { d.engineRuns(p.Name); close(snapshots) }()
			select {
			case <-snapshots:
			case <-time.After(300 * time.Millisecond):
				release()
				<-snapshots
				t.Error("snapshot blocked behind git")
			}
		})
	}
}
func TestConcurrentCleanupIsRefused(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	refreshHead(t, d, p)
	dir, release := blockedGit(t, d)
	first := make(chan error, 1)
	go func() { first <- cleanupRequest(d) }()
	defer func() {
		release()
		if err := <-first; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "cleanup started", func() bool { _, err := os.Stat(filepath.Join(dir, "started")); return err == nil })
	second := make(chan error, 1)
	go func() { second <- cleanupRequest(d) }()
	select {
	case err := <-second:
		if err == nil {
			t.Error("concurrent cleanup was accepted")
		}
	case <-time.After(300 * time.Millisecond):
		release()
		<-second
		t.Error("concurrent cleanup waited for git instead of being refused")
	}
}
func TestLandingRefusesOptionLikeArguments(t *testing.T) {
	for _, field := range []string{"branch", "worktree"} {
		t.Run(field, func(t *testing.T) {
			d, s, p, f := landingDaemon(t, 0, "")
			committedRun(t, s, p, "run", "committed")
			run, err := s.Open("run")
			if err != nil {
				t.Fatal(err)
			}
			if err := run.Update(func(r *runrec.Record) {
				if field == "branch" {
					r.Branch = "--help"
				} else {
					r.Worktree = "--force"
				}
			}); err != nil {
				t.Fatal(err)
			}
			merged, err := d.landed(context.Background(), p, run.Snapshot())
			if err != nil || merged != nil || len(f.CallsTo("git")) != 0 {
				t.Errorf("unsafe detection: merged=%v err=%v calls=%v", merged, err, f.CallsTo("git"))
			}
			yes := true
			d.runs.merged["run"] = &yes
			before := len(f.CallsTo("git"))
			err = cleanupRequest(d)
			wire, ok := err.(*ipc.Error)
			if !ok || wire.Code != CodeBadParams {
				t.Errorf("cleanup error=%v, want bad_params", err)
			}
			if len(f.CallsTo("git")) != before {
				t.Error("unsafe cleanup ran git")
			}
		})
	}
}
func TestCleanupRefusesUnknownMergeState(t *testing.T) {
	d, s, p, f := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	err := cleanupRequest(d)
	wire, ok := err.(*ipc.Error)
	if !ok || wire.Code != CodeBadParams || len(f.CallsTo("git")) != 0 {
		t.Fatalf("unknown cleanup: %v calls=%v", err, f.CallsTo("git"))
	}
}
func TestPruneMergedRunsKeepsActiveRun(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	committedRun(t, s, p, "active", "implement")
	yes := true
	d.runs.merged["active"] = &yes
	for i := 0; i < 102; i++ {
		committedRun(t, s, p, fmt.Sprintf("run%03d", i), "committed")
	}
	refreshHead(t, d, p)
	if _, err := s.Open("active"); err != nil {
		t.Fatalf("active run removed: %v", err)
	}
	all, err := s.List()
	if err != nil || len(all) != 101 {
		t.Fatalf("retained=%d err=%v, want active plus 100", len(all), err)
	}
}

func landedRun(t *testing.T, store *runrec.Store, p registry.Project, id, reason string) {
	t.Helper()
	_, e := store.Create(runrec.Record{ID: id, Project: p.Name, Item: "S63", State: runrec.StateCommitted, Branch: "s63", Worktree: p.Path + "/.worktrees/s63",
		LandedAt: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC), Reason: reason})
	if e != nil {
		t.Fatal(e)
	}
}

// A run that ork landed says so on the wire; one whose cleanup failed carries
// the reason, and a committed run that ork did not land says neither (S119).
func TestLandedRunsAreMarkedOnTheWire(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	committedRun(t, s, p, "plain", "committed")
	landedRun(t, s, p, "clean", "")
	landedRun(t, s, p, "dirty", "landed; cleanup failed at git worktree remove X: locked")
	got := map[string]source.EngineRun{}
	for _, r := range d.engineRuns(p.Name) {
		got[r.ID] = r
	}
	if r := got["plain"]; r.Landed || r.CleanupFailed != "" {
		t.Errorf("plain committed run: %+v", r)
	}
	if r := got["clean"]; !r.Landed || r.CleanupFailed != "" {
		t.Errorf("landed run: %+v", r)
	}
	if r := got["dirty"]; !r.Landed || r.CleanupFailed != "landed; cleanup failed at git worktree remove X: locked" {
		t.Errorf("landed run with a failed cleanup: %+v", r)
	}
	wire, _ := json.Marshal(got["dirty"])
	if !strings.Contains(string(wire), `"landed":true`) || !strings.Contains(string(wire), `"cleanup_failed":`) {
		t.Errorf("wire: %s", wire)
	}
}

// The cleanup key that removes what a failed landing cleanup left behind
// also clears the reason, so the row leaves Ready to land.
func TestCleanupClearsTheReasonOfALandedRun(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	landedRun(t, s, p, "run", "landed; cleanup failed at git worktree remove X: locked")
	refreshHead(t, d, p)
	if e := cleanupRequest(d); e != nil {
		t.Fatal(e)
	}
	r, e := s.Open("run")
	if e != nil {
		t.Fatal(e)
	}
	if rec := r.Snapshot(); rec.Reason != "" || rec.LandedAt.IsZero() {
		t.Errorf("reason %q, landed at %v", rec.Reason, rec.LandedAt)
	}
	for _, row := range d.engineRuns(p.Name) {
		if row.CleanupFailed != "" {
			t.Errorf("the wire still says the cleanup failed: %+v", row)
		}
	}
}
