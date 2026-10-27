// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
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
	// The run's worktree exists, as it does until a cleanup removes it.
	if err := os.MkdirAll(p.Path+"/.worktrees/s63", 0o755); err != nil {
		t.Fatal(err)
	}
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

// setGitBin and setMerged write the manager's state under its lock: the cache
// of a live daemon reads both from its own goroutines.
func setGitBin(d *Daemon, bin string) {
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	d.runs.deps.GitBin = bin
}
func setMerged(d *Daemon, id string, merged *bool) {
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	d.runs.merged[id] = merged
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
	setGitBin(d, fake.Bin("git"))
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
	setGitBin(d, bin)
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
			setMerged(d, "run", &yes)
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
	setMerged(d, "active", &yes)
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

func runRecord(t *testing.T, s *runrec.Store, id string) runrec.Record {
	t.Helper()
	r, err := s.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	return r.Snapshot()
}

// A successful cleanup marks the record cleaned: the wire says so, so Ready
// hides the row, and a second cleanup is refused with a reason and no git
// (S95). The run was not landed by ork (the S31 case: slicer land, then c), so
// LandedAt cannot be what marks it.
func TestCleanupMarksTheRunCleaned(t *testing.T) {
	d, s, p, f := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	refreshHead(t, d, p)
	if r := d.engineRuns(p.Name)[0]; r.Cleaned {
		t.Fatalf("cleaned before the cleanup: %+v", r)
	}
	if err := cleanupRequest(d); err != nil {
		t.Fatal(err)
	}
	if rec := runRecord(t, s, "run"); rec.CleanedAt.IsZero() || !rec.LandedAt.IsZero() || rec.State != runrec.StateCommitted {
		t.Errorf("cleaned at %v, landed at %v, state %s", rec.CleanedAt, rec.LandedAt, rec.State)
	}
	row := d.engineRuns(p.Name)[0]
	if !row.Cleaned {
		t.Errorf("the wire does not say cleaned: %+v", row)
	}
	if wire, _ := json.Marshal(row); !strings.Contains(string(wire), `"cleaned":true`) {
		t.Errorf("wire: %s", wire)
	}
	before := len(f.CallsTo("git"))
	err := cleanupRequest(d)
	wire, ok := err.(*ipc.Error)
	if !ok || wire.Code != CodeBadParams || !strings.Contains(wire.Message, "already cleaned") {
		t.Errorf("second cleanup: %v, want bad_params that says already cleaned", err)
	}
	if len(f.CallsTo("git")) != before {
		t.Errorf("the refused cleanup ran git: %v", f.CallsTo("git")[before:])
	}
}

// A run that ork landed with a clean cleanup is cleaned too: its branch is gone
// and there is nothing for the cleanup key to do.
func TestALandedRunWithACleanCleanupIsCleaned(t *testing.T) {
	d, s, p, f := landingDaemon(t, 0, "")
	landedRun(t, s, p, "run", "")
	if r := d.engineRuns(p.Name)[0]; !r.Cleaned || !r.Landed {
		t.Errorf("landed run: %+v", r)
	}
	if err := cleanupRequest(d); err == nil || len(f.CallsTo("git")) != 0 {
		t.Errorf("cleanup of a cleaned run: %v, git %v", err, f.CallsTo("git"))
	}
	// Its branch is gone, so the merge check must not read it again.
	refreshHead(t, d, p)
	if mergeCalls(f) != 0 {
		t.Errorf("merge checks=%d for a cleaned run", mergeCalls(f))
	}
	if r := d.engineRuns(p.Name)[0]; r.Merged != nil {
		t.Errorf("merged %v for a cleaned run", *r.Merged)
	}
}

// A cleanup that removed the worktree and then failed at branch -d leaves the
// worktree path gone. The retry must not run worktree remove again, which fails
// with "is not a working tree"; it runs branch -d.
func TestCleanupRetryAfterHalfDone(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "error: the branch is checked out\n")
	committedRun(t, s, p, "run", "committed")
	refreshHead(t, d, p)
	err := cleanupRequest(d)
	if err == nil || !strings.Contains(err.Error(), "checked out") {
		t.Fatalf("first cleanup: %v", err)
	}
	if rec := runRecord(t, s, "run"); !rec.CleanedAt.IsZero() {
		t.Fatal("a half-done cleanup marked the run cleaned")
	}
	// The fake git cannot remove a directory; do what git worktree remove did.
	if err := os.RemoveAll(p.Path + "/.worktrees/s63"); err != nil {
		t.Fatal(err)
	}
	// The retry fakes no worktree remove: a call to it exits 97 and fails.
	retry := clitest.Install(t, clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "branch", "-d", "s63"}})
	setGitBin(d, retry.Bin("git"))
	if err := cleanupRequest(d); err != nil {
		t.Fatalf("retry: %v", err)
	}
	want := []clitest.Call{{Tool: "git", Args: []string{"-C", p.Path, "branch", "-d", "s63"}}}
	if got := retry.CallsTo("git"); !reflect.DeepEqual(got, want) {
		t.Errorf("retry calls=%v, want %v", got, want)
	}
	if rec := runRecord(t, s, "run"); rec.CleanedAt.IsZero() {
		t.Error("the retry did not mark the run cleaned")
	}
}

func TestCleanupRetryOfLandedRunAfterHalfDone(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "error: the branch is checked out\n")
	landedRun(t, s, p, "run", "landed; cleanup failed at git branch -d s63: checked out")
	refreshHead(t, d, p)
	if err := os.RemoveAll(p.Path + "/.worktrees/s63"); err != nil {
		t.Fatal(err)
	}
	retry := clitest.Install(t, clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "branch", "-d", "s63"}})
	setGitBin(d, retry.Bin("git"))
	if err := cleanupRequest(d); err != nil {
		t.Fatalf("retry of landed cleanup: %v", err)
	}
	want := []clitest.Call{{Tool: "git", Args: []string{"-C", p.Path, "branch", "-d", "s63"}}}
	if got := retry.CallsTo("git"); !reflect.DeepEqual(got, want) {
		t.Errorf("retry calls=%v, want %v", got, want)
	}
	if rec := runRecord(t, s, "run"); !rec.Cleaned() || rec.Reason != "" || rec.LandedAt.IsZero() {
		t.Errorf("landed cleanup did not finish: %+v", rec)
	}
	if row := d.engineRuns(p.Name)[0]; !row.Cleaned || row.CleanupFailed != "" {
		t.Errorf("landed cleanup is still offered: %+v", row)
	}
}

// While a merge check runs, the previous merged value stays on the wire and
// the cleanup key still works for a run whose value is known.
func TestMergedValueSurvivesAPass(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	refreshHead(t, d, p)
	if r := d.engineRuns(p.Name)[0]; r.Merged == nil || !*r.Merged {
		t.Fatalf("merged before the pass: %+v", r.Merged)
	}
	dir, release := blockedGit(t, d)
	pass := make(chan struct{})
	go func() { d.checkMerged(context.Background(), p, "changed"); close(pass) }()
	waitFor(t, "the merge check started", func() bool { _, err := os.Stat(filepath.Join(dir, "started")); return err == nil })
	if r := d.engineRuns(p.Name)[0]; r.Merged == nil || !*r.Merged {
		t.Errorf("the pass cleared the merged value: %v", r.Merged)
	}
	cleanup := make(chan error, 1)
	go func() { cleanup <- cleanupRequest(d) }()
	var early error
	select {
	case early = <-cleanup:
		if early == nil {
			early = errors.New("cleanup finished before git was released")
		}
		t.Errorf("cleanup during a pass returned at once: %v (a known value must not be refused)", early)
	case <-time.After(300 * time.Millisecond):
		// It is waiting on git, as an accepted cleanup does.
	}
	release()
	if early == nil {
		if err := <-cleanup; err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}
	<-pass
}

// Retention counts a cleaned run as done with: it is pruned beyond the keep
// limit although its branch is gone and its merge state is not known.
func TestPruneRemovesACleanedFinishedRunBeyondTheKeepLimit(t *testing.T) {
	d, s, p, f := landingDaemon(t, 1, "")
	for i := 0; i < 102; i++ {
		id := fmt.Sprintf("run%03d", i)
		committedRun(t, s, p, id, "committed")
		r, err := s.Open(id)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
		switch i {
		case 0, 50: // marked by the cleanup key
			err = r.Update(func(rec *runrec.Record) { rec.CleanedAt = at })
		case 1: // landed by ork with a clean cleanup
			err = r.Update(func(rec *runrec.Record) { rec.LandedAt = at })
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	refreshHead(t, d, p)
	all, err := s.List()
	if err != nil || len(all) != 100 {
		t.Fatalf("finished runs=%d err=%v, want 100", len(all), err)
	}
	if all[0].Snapshot().ID != "run002" {
		t.Errorf("oldest kept run %s, want run002", all[0].Snapshot().ID)
	}
	// Only the two cleaned runs went: the 100 unmerged runs stay.
	if _, err := s.Open("run050"); err != nil {
		t.Errorf("a cleaned run inside the keep limit was removed: %v", err)
	}
	if n := mergeCalls(f); n != 99 {
		t.Errorf("merge checks=%d, want 99 (cleaned runs are not checked)", n)
	}
}

// staleRun is a run from before S95: committed, its branch and worktree
// gone, never marked cleaned. sha is the commit ork made on the slice branch;
// "none" records no commit at all.
func staleRun(t *testing.T, store *runrec.Store, p registry.Project, sha string) {
	t.Helper()
	rec := runrec.Record{ID: "run", Project: p.Name, Item: "S63", State: runrec.StateCommitted, Branch: "s63", Worktree: p.Path + "/.worktrees/s63"}
	if sha != "none" {
		rec.Commit = &runrec.Commit{SHA: sha, Title: "S63: work"}
	}
	if _, err := store.Create(rec); err != nil {
		t.Fatal(err)
	}
}

// staleGit fakes git for the merge pass of a run whose branch may be gone:
// the branch merge check fails as git does for a missing branch (exit 128),
// rev-parse says whether the branch still exists, and the commit check says
// whether the commit is in HEAD.
func staleGit(t *testing.T, d *Daemon, p registry.Project, branchExit, shaExit int) *clitest.Fake {
	t.Helper()
	// The fake routes are process-wide: the HEAD read of the cache needs its own.
	f := clitest.Install(t,
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "rev-parse", "HEAD"}, Stdout: "h1"},
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "merge-base", "--is-ancestor", "s63", "HEAD"}, Exit: 128},
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "rev-parse", "--verify", "--quiet", "refs/heads/s63"}, Exit: branchExit},
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "merge-base", "--is-ancestor", "abc123", "HEAD"}, Exit: shaExit},
		// An empty commit must not be asked about; if it were, git would say yes.
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "merge-base", "--is-ancestor", "", "HEAD"}},
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "merge-base", "--is-ancestor", "--x", "HEAD"}})
	setGitBin(d, f.Bin("git"))
	return f
}

// removeWorktree does what the old cleanup did to the run's worktree.
func removeWorktree(t *testing.T, p registry.Project) {
	t.Helper()
	if err := os.RemoveAll(p.Path + "/.worktrees/s63"); err != nil {
		t.Fatal(err)
	}
}

// A committed run whose branch and worktree are gone and whose commit is in
// HEAD is cleaned up already: it leaves Ready, is not offered the cleanup key, and git writes
// nothing (S153, owner decision 2026-10-26).
func TestStaleRunWithGoneBranchAndMergedCommitIsCleaned(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 128, "")
	staleRun(t, s, p, "abc123")
	removeWorktree(t, p)
	f := staleGit(t, d, p, 1, 0)
	refreshHead(t, d, p)
	if rec := runRecord(t, s, "run"); rec.CleanedAt.IsZero() || !rec.Cleaned() || rec.State != runrec.StateCommitted {
		t.Errorf("the stale run is not marked cleaned: %+v", rec)
	}
	if row := d.engineRuns(p.Name)[0]; !row.Cleaned {
		t.Errorf("the wire does not say cleaned: %+v", row)
	}
	for _, c := range f.CallsTo("git") {
		if c.Args[2] != "merge-base" && c.Args[2] != "rev-parse" {
			t.Errorf("git wrote: %v", c)
		}
	}
	if err := cleanupRequest(d); err == nil || !strings.Contains(err.Error(), "already cleaned") {
		t.Errorf("cleanup of the stale run: %v, want already cleaned", err)
	}
}

// Anything short of "the branch and worktree are gone and the commit is in
// HEAD" leaves the run as it was: one variable at a time.
func TestStaleRunIsNotCleanedUnlessAllHold(t *testing.T) {
	for _, tc := range []struct {
		name                string
		sha                 string
		branchExit, shaExit int
		keepWorktree        bool
	}{
		{"the branch still exists", "abc123", 0, 0, false},
		{"the worktree is still there", "abc123", 1, 0, true},
		{"the commit is not in HEAD", "abc123", 1, 1, false},
		{"git fails reading the commit", "abc123", 1, 2, false},
		{"git fails reading the branch", "abc123", 2, 0, false},
		{"no commit is recorded", "none", 1, 0, false},
		{"the commit has no sha", "", 1, 0, false},
		{"the commit looks like an option", "--x", 1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, s, p, _ := landingDaemon(t, 128, "")
			staleRun(t, s, p, tc.sha)
			if !tc.keepWorktree {
				removeWorktree(t, p)
			}
			f := staleGit(t, d, p, tc.branchExit, tc.shaExit)
			refreshHead(t, d, p)
			if mergeCalls(f) == 0 {
				t.Fatal("the merge pass did not run")
			}
			if rec := runRecord(t, s, "run"); !rec.CleanedAt.IsZero() || rec.Cleaned() {
				t.Errorf("marked cleaned: %+v", rec)
			}
			if row := d.engineRuns(p.Name)[0]; row.Cleaned || row.Merged != nil {
				t.Errorf("row %+v, want not cleaned and merge state unknown", row)
			}
		})
	}
}

// A cleaned stale run is pruned like any cleaned run beyond the keep limit.
func TestStaleCleanedRunIsPruned(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 128, "")
	staleRun(t, s, p, "abc123")
	removeWorktree(t, p)
	for i := 0; i < 100; i++ {
		committedRun(t, s, p, fmt.Sprintf("zlater%03d", i), "committed")
	}
	staleGit(t, d, p, 1, 0)
	refreshHead(t, d, p)
	if _, err := s.Open("run"); err == nil {
		t.Error("the stale cleaned run was not pruned")
	}
}

func TestWorktreeGoneIsProposedOnTheRow(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	row := d.engineRuns(p.Name)[0]
	if row.WorktreeGone {
		t.Errorf("worktree there, flag set: %+v", row)
	}
	if wire, _ := json.Marshal(row); strings.Contains(string(wire), "worktree_gone") {
		t.Errorf("wire: %s", wire)
	}
	if err := os.RemoveAll(p.Path + "/.worktrees/s63"); err != nil {
		t.Fatal(err)
	}
	row = d.engineRuns(p.Name)[0]
	if !row.WorktreeGone {
		t.Errorf("worktree gone, flag unset: %+v", row)
	}
	if wire, _ := json.Marshal(row); !strings.Contains(string(wire), `"worktree_gone":true`) {
		t.Errorf("wire: %s", wire)
	}
}

// Only a committed run that is not cleaned yet has a cleanup to adjust: a
// cleaned run, or one still running, never carries the flag.
func TestWorktreeGoneOnlyForUncleanedCommittedRuns(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	landedRun(t, s, p, "cleaned", "")
	committedRun(t, s, p, "running", "implement")
	if err := os.RemoveAll(p.Path + "/.worktrees/s63"); err != nil {
		t.Fatal(err)
	}
	for _, row := range d.engineRuns(p.Name) {
		if row.WorktreeGone {
			t.Errorf("flag set on %s (cleaned %v, state %s)", row.ID, row.Cleaned, row.State)
		}
	}
}

// A run without a worktree path has no worktree to be gone.
func TestWorktreeGoneNeedsAWorktreePath(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	if _, err := s.Create(runrec.Record{ID: "run", Project: p.Name, Item: "S63", State: runrec.StateCommitted, Branch: "s63"}); err != nil {
		t.Fatal(err)
	}
	if row := d.engineRuns(p.Name)[0]; row.WorktreeGone {
		t.Errorf("flag set without a worktree path: %+v", row)
	}
}

// A cleanup that finishes while a merge pass is in flight must not let the
// pass write a merged value for the cleaned run.
func TestAPassDoesNotWriteMergedForARunCleanedMeanwhile(t *testing.T) {
	d, s, p, _ := landingDaemon(t, 0, "")
	committedRun(t, s, p, "run", "committed")
	refreshHead(t, d, p)
	dir, release := blockedGit(t, d)
	pass := make(chan struct{})
	go func() { d.checkMerged(context.Background(), p, "changed"); close(pass) }()
	waitFor(t, "the merge check started", func() bool { _, err := os.Stat(filepath.Join(dir, "started")); return err == nil })
	if err := d.markCleaned("run"); err != nil {
		t.Fatal(err)
	}
	release()
	<-pass
	d.runs.mu.Lock()
	_, has := d.runs.merged["run"]
	d.runs.mu.Unlock()
	if has {
		t.Error("the pass wrote a merged value for a run cleaned meanwhile")
	}
}
