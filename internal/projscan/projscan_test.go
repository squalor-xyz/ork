// SPDX-License-Identifier: GPL-3.0-or-later

package projscan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func touch(t *testing.T, parts ...string) {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scan(root string) ([]Candidate, error) {
	r, err := Scan(root)
	return r.Candidates, err
}

func names(cs []Candidate) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func TestScanListsTaggedProjectsAndNotPlainFolders(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "tracked", ".slicer")
	touch(t, root, "decided", ".adjudicate.json")
	mkdir(t, root, "repo", ".git")
	touch(t, root, "worktree-repo", ".git") // a linked worktree has a .git file
	touch(t, root, "folder", "notes.txt")
	got, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"decided", "repo", "tracked", "worktree-repo"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("names %v, want %v", names(got), want)
	}
	tags := map[string]string{}
	for _, c := range got {
		tags[c.Name] = c.Tag()
		if c.Path != filepath.Join(root, c.Name) {
			t.Errorf("path %q", c.Path)
		}
	}
	want := map[string]string{"tracked": "slicer", "decided": "adj", "repo": "git", "worktree-repo": "git"}
	if !reflect.DeepEqual(tags, want) {
		t.Fatalf("tags %v, want %v", tags, want)
	}
}

func TestScanTagNamesEveryTrackedMarker(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "both", ".slicer")
	touch(t, root, "both", ".adjudicate.json")
	mkdir(t, root, "both", ".git")
	got, err := scan(root)
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	if got[0].Tag() != "slicer+adj" || !got[0].Slicer || !got[0].Adj || !got[0].Git {
		t.Fatalf("%+v tag %q", got[0], got[0].Tag())
	}
}

func TestScanWalksTwoLevelsAndStopsAtAHit(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "one", "two", "three", ".git") // depth 3: too deep
	mkdir(t, root, "group", "member", ".git")     // depth 2: found
	mkdir(t, root, "hit", ".slicer")
	mkdir(t, root, "hit", "inner", ".slicer") // below a hit: not listed
	mkdir(t, root, "other", ".git")
	mkdir(t, root, "other", "sub", ".slicer") // below a hit: not listed
	got, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"member", "hit", "other"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("names %v, want %v", names(got), want)
	}
	if got[0].Path != filepath.Join(root, "group", "member") {
		t.Fatalf("path %q", got[1].Path)
	}
}

func TestScanSkipsWorktreesNodeModulesAndHiddenFolders(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, ".worktrees", "s1", ".slicer")
	mkdir(t, root, "app", ".worktrees", "s2", ".git")
	mkdir(t, root, "node_modules", "pkg", ".git")
	mkdir(t, root, "app", "node_modules", "pkg", ".git")
	mkdir(t, root, ".hidden", "p", ".git")
	mkdir(t, root, "visible", ".cache", ".git")
	mkdir(t, root, "app", ".git")
	got, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"app"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("names %v, want %v", names(got), want)
	}
	// A folder below a plain folder is skipped by the same rule.
	root2 := t.TempDir()
	mkdir(t, root2, "plain", "node_modules", ".git")
	mkdir(t, root2, "plain", ".worktrees", ".git")
	mkdir(t, root2, "plain", ".dot", ".git")
	if got, err := scan(root2); err != nil || len(got) != 0 {
		t.Fatalf("%v %v", names(got), err)
	}
}

func TestScanDoesNotFollowSymlinks(t *testing.T) {
	elsewhere := t.TempDir()
	mkdir(t, elsewhere, "proj", ".git")
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(elsewhere, "proj"), filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	if got, err := scan(root); err != nil || len(got) != 0 {
		t.Fatalf("%v %v", names(got), err)
	}
}

func TestScanOfAProjectListsOnlyItself(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, ".slicer")
	mkdir(t, root, "child", ".git")
	got, err := scan(root)
	if err != nil || len(got) != 1 || got[0].Path != root || got[0].Name != filepath.Base(root) || !got[0].Slicer {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestScanErrorsOnAMissingFolderAndAFile(t *testing.T) {
	root := t.TempDir()
	if _, err := scan(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing folder scanned")
	}
	touch(t, root, "file")
	if _, err := scan(filepath.Join(root, "file")); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("a file scanned: %v", err)
	}
}

func TestTrackedProjectsStartTickedAndPlainReposDoNot(t *testing.T) {
	rows := Classify([]Candidate{
		{Path: "/w/a", Name: "a", Slicer: true},
		{Path: "/w/b", Name: "b", Adj: true, Git: true},
		{Path: "/w/c", Name: "c", Git: true},
	}, nil)
	got := []bool{}
	for _, r := range rows {
		got = append(got, r.Preselected())
	}
	if want := []bool{true, true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("preselected %v, want %v", got, want)
	}
	if !rows[0].Tracked() || rows[2].Tracked() {
		t.Fatal("Tracked")
	}
}

func TestClassifyBlocksARegisteredPathAndANameClash(t *testing.T) {
	registered := []registry.Project{
		{Name: "app", Path: "/w/app"},
		{Name: "tool", Path: "/elsewhere/other"},
	}
	rows := Classify([]Candidate{
		{Path: "/w/app/", Name: "app", Slicer: true},  // same path, trailing slash
		{Path: "/w/tool", Name: "tool", Slicer: true}, // another path, same name
		{Path: "/w/free", Name: "free", Slicer: true},
	}, registered)
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	if !rows[0].Registered || !rows[0].Blocked() || rows[0].Preselected() || !strings.Contains(rows[0].Reason, "app") {
		t.Errorf("registered row: %+v", rows[0])
	}
	if rows[1].Registered || !rows[1].Blocked() || rows[1].Preselected() || !strings.Contains(rows[1].Reason, `"tool"`) || !strings.Contains(rows[1].Reason, "/elsewhere/other") {
		t.Errorf("clash row: %+v", rows[1])
	}
	if rows[2].Blocked() || !rows[2].Preselected() || rows[2].Reason != "" {
		t.Errorf("free row: %+v", rows[2])
	}
}

func TestAddAllNamesEachOutcomeAndKeepsGoingAfterAFailure(t *testing.T) {
	cands := []Candidate{
		{Path: "/w/a", Name: "a"},
		{Path: "/w/b", Name: "b"},
		{Path: "/w/c", Name: "c"},
	}
	var seen []Candidate
	boom := errors.New("boom")
	out := AddAll(context.Background(), cands, func(_ context.Context, c Candidate) (registry.Project, error) {
		seen = append(seen, c)
		if c.Name == "b" {
			return registry.Project{}, boom
		}
		return registry.Project{Name: c.Name, Path: c.Path}, nil
	})
	if !reflect.DeepEqual(seen, cands) {
		t.Fatalf("add called with %v", seen)
	}
	if len(out) != 3 {
		t.Fatal(out)
	}
	for i, o := range out {
		if o.Name != cands[i].Name || o.Path != cands[i].Path {
			t.Errorf("outcome %d is for %q", i, o.Name)
		}
	}
	if out[0].Err != nil || out[0].Project.Name != "a" || !errors.Is(out[1].Err, boom) || out[2].Err != nil || out[2].Project.Name != "c" {
		t.Fatalf("outcomes %+v", out)
	}
}

func TestAddAllStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	out := AddAll(ctx, []Candidate{{Name: "a"}, {Name: "b"}}, func(context.Context, Candidate) (registry.Project, error) {
		calls++
		cancel()
		return registry.Project{Name: "a"}, nil
	})
	if calls != 1 || len(out) != 2 || out[0].Err != nil || !errors.Is(out[1].Err, context.Canceled) {
		t.Fatalf("calls %d, outcomes %+v", calls, out)
	}
}

func TestScanSortsByPath(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "a", "x", ".git")
	mkdir(t, root, "a-b", ".git") // "a-b" sorts before "a/x", but a walk meets a/x first
	got, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a-b", "x"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("names %v, want %v", names(got), want)
	}
}

func TestScanStopsAtTheFolderLimitAndSaysSo(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		mkdir(t, root, n, ".git")
	}
	r, err := ScanLimit(root, 3, 100) // the root and two folders
	if err != nil || !r.Stopped || r.Folders != 3 || !reflect.DeepEqual(names(r.Candidates), []string{"a", "b"}) {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestScanStopsAtTheCandidateLimitAndSaysSo(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c", "d"} {
		mkdir(t, root, n, ".git")
	}
	r, err := ScanLimit(root, 1000, 2)
	if err != nil || !r.Stopped || !reflect.DeepEqual(names(r.Candidates), []string{"a", "b"}) {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestScanThatEndsExactlyAtALimitIsNotStopped(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "a", ".git")
	mkdir(t, root, "b", ".git")
	r, err := ScanLimit(root, 3, 2) // the root, a and b; two projects
	if err != nil || r.Stopped || r.Folders != 3 || len(r.Candidates) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestScanDefaultLimits(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < MaxFolders+100; i++ {
		mkdir(t, root, fmt.Sprintf("plain%04d", i))
	}
	r, err := Scan(root)
	if err != nil || !r.Stopped || r.Folders != MaxFolders || len(r.Candidates) != 0 {
		t.Fatalf("folders: stopped %v, folders %d, %v", r.Stopped, r.Folders, err)
	}
	root = t.TempDir()
	for i := 0; i < MaxCandidates+20; i++ {
		mkdir(t, root, fmt.Sprintf("p%04d", i), ".git")
	}
	r, err = Scan(root)
	if err != nil || !r.Stopped || len(r.Candidates) != MaxCandidates {
		t.Fatalf("candidates: stopped %v, %d, %v", r.Stopped, len(r.Candidates), err)
	}
}

func TestScanCountsTheFoldersItLookedAt(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "a", "x") // a is plain, so its child x is looked at too
	mkdir(t, root, "b", ".git")
	r, err := Scan(root)
	if err != nil || r.Stopped || r.Folders != 4 { // root, a, a/x, b
		t.Fatalf("%+v %v", r, err)
	}
}

func TestClassifyBlocksTheLaterOfTwoSameNamedCandidates(t *testing.T) {
	rows := Classify([]Candidate{
		{Path: "/w/one/app", Name: "app", Slicer: true},
		{Path: "/w/two/app", Name: "app", Slicer: true},
		{Path: "/w/three/app", Name: "app", Git: true},
		{Path: "/w/other", Name: "other", Slicer: true},
	}, nil)
	if rows[0].Blocked() || !rows[0].Preselected() {
		t.Errorf("first: %+v", rows[0])
	}
	const want = `name "app" is also found at /w/one/app; add it with ork projects add PATH --name N`
	for _, i := range []int{1, 2} {
		if !rows[i].Blocked() || rows[i].Registered || rows[i].Preselected() || rows[i].Reason != want {
			t.Errorf("row %d: %+v", i, rows[i])
		}
	}
	if rows[3].Blocked() {
		t.Errorf("unrelated row blocked: %+v", rows[3])
	}
}

// A row that cannot be added does not hold its name against a later row.
func TestClassifySameNameIgnoresBlockedEarlierRows(t *testing.T) {
	rows := Classify([]Candidate{
		{Path: "/w/one/app", Name: "app", Slicer: true},
		{Path: "/w/two/app", Name: "app", Slicer: true},
	}, []registry.Project{{Name: "first", Path: "/w/one/app"}})
	if !rows[0].Registered || rows[1].Blocked() {
		t.Fatalf("%+v", rows)
	}
}

func TestClassifyMatchesRegisteredPathsByCanonicalPath(t *testing.T) {
	root := t.TempDir()
	real := mkdir(t, root, "real", "app")
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "real"), link); err != nil {
		t.Skip(err)
	}
	for _, tc := range []struct{ cand, reg string }{
		{real, filepath.Join(link, "app")},
		{filepath.Join(link, "app"), real},
	} {
		rows := Classify([]Candidate{{Path: tc.cand, Name: "app", Slicer: true}},
			[]registry.Project{{Name: "x", Path: tc.reg}})
		if !rows[0].Registered || !strings.Contains(rows[0].Reason, "already registered as x") {
			t.Errorf("candidate %s, registered %s: %+v", tc.cand, tc.reg, rows[0])
		}
	}
	// A registered path that no longer exists still matches as written.
	rows := Classify([]Candidate{{Path: "/gone/app", Name: "app"}}, []registry.Project{{Name: "x", Path: "/gone/app/"}})
	if !rows[0].Registered {
		t.Errorf("missing path: %+v", rows[0])
	}
}

// A limit hit inside a nested folder stops the whole scan: the parent loop does
// not look at the next folder or clear the flag.
func TestScanStoppedInANestedFolderStaysStopped(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		mkdir(t, root, "group", n, ".git")
	}
	mkdir(t, root, "z")
	r, err := ScanLimit(root, 1000, 2)
	if err != nil || !r.Stopped || !reflect.DeepEqual(names(r.Candidates), []string{"a", "b"}) {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Folders != 5 { // root, group, a, b, c; z is not looked at
		t.Fatalf("folders %d, want 5", r.Folders)
	}
	// The same layout with the folder limit.
	r, err = ScanLimit(root, 4, 1000)
	if err != nil || !r.Stopped || r.Folders != 4 || !reflect.DeepEqual(names(r.Candidates), []string{"a", "b"}) {
		t.Fatalf("folder limit: %+v %v", r, err)
	}
}

func TestResultNoteSaysWhereTheScanStopped(t *testing.T) {
	three := []Candidate{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	for _, tc := range []struct {
		r    Result
		want string
	}{
		{Result{Stopped: true, Folders: 500, Candidates: three}, "stopped after 500 folders and 3 projects"},
		{Result{Stopped: true, Folders: 12, Candidates: three[:1]}, "stopped after 12 folders and 1 project"},
		{Result{Stopped: true, Folders: 500}, "stopped after 500 folders and 0 projects"},
		{Result{Folders: 500, Candidates: three}, ""},
	} {
		if got := tc.r.Note(); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.r, got, tc.want)
		}
	}
}

// A plain git repo does not hold a name against a tracked project (it is not
// added from the command line); equal rows go by path.
func TestClassifyLetsATrackedRowClaimTheNameBeforeAPlainRepo(t *testing.T) {
	rows := Classify([]Candidate{
		{Path: "/w/a/app", Name: "app", Git: true},
		{Path: "/w/b/app", Name: "app", Slicer: true},
	}, nil)
	const want = `name "app" is also found at /w/b/app; add it with ork projects add PATH --name N`
	if !rows[0].Blocked() || rows[0].Reason != want || rows[1].Blocked() || !rows[1].Preselected() {
		t.Fatalf("%+v", rows)
	}
	// Two tracked rows: the earlier path wins, whatever the input order.
	rows = Classify([]Candidate{
		{Path: "/w/z/app", Name: "app", Adj: true},
		{Path: "/w/y/app", Name: "app", Slicer: true},
	}, nil)
	if rows[0].Reason != `name "app" is also found at /w/y/app; add it with ork projects add PATH --name N` || rows[1].Blocked() {
		t.Fatalf("%+v", rows)
	}
	// Two plain repos: the earlier path wins.
	rows = Classify([]Candidate{
		{Path: "/w/b/app", Name: "app", Git: true},
		{Path: "/w/a/app", Name: "app", Git: true},
	}, nil)
	if !rows[0].Blocked() || rows[1].Blocked() {
		t.Fatalf("%+v", rows)
	}
}
