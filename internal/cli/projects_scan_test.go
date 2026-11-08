// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/projscan"
	"github.com/squalor-xyz/ork/internal/registry"
)

// S302 (*proposed*): ork projects add --scan DIR.

const legend = "[ ] plain git repos are not added; use ork projects add PATH"

// scanTree makes DIR/code with a slicer project, an adj project, a plain git
// repo and a plain folder, and returns the folder.
func scanTree(t *testing.T, w *world) string {
	t.Helper()
	root := filepath.Join(w.dir, "code")
	for _, d := range []string{"a/.slicer", "b", "c/.git", "plain/notes"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "b", ".adjudicate.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func registered(t *testing.T, w *world) []string {
	t.Helper()
	f, err := registry.Load(w.regPath)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range f.Projects {
		out = append(out, p.Name)
	}
	return out
}

// lines are the output lines with their runs of spaces squeezed.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		out = append(out, strings.Join(strings.Fields(l), " "))
	}
	return out
}

func TestScanListsTheCandidatesAndAddsTheTrackedOnesWithYes(t *testing.T) {
	w := newWorld(t, stubSource{})
	root := scanTree(t, w)
	code, stdout, stderr := run(t, w.app, "projects", "add", "--scan", root, "--yes")
	want := []string{
		"found 3 projects under " + root + ":",
		"[x] a slicer " + root + "/a",
		"[x] b adj " + root + "/b",
		"[ ] c git " + root + "/c",
		legend,
		"added a " + root + "/a",
		"added b " + root + "/b (adj)",
	}
	if code != 0 || stderr != "" || fmt.Sprint(lines(stdout)) != fmt.Sprint(want) {
		t.Fatalf("exit %d\nstdout: %q\nstderr: %q\nwant %q", code, lines(stdout), stderr, want)
	}
	if got := registered(t, w); fmt.Sprint(got) != "[a b]" {
		t.Fatalf("registered %v", got)
	}
	f, _ := registry.Load(w.regPath)
	if f.Projects[0].Path != root+"/a" || f.Projects[1].Adj != true {
		t.Fatalf("%+v", f.Projects)
	}
}

func TestScanWithoutYesAsksOnStandardInput(t *testing.T) {
	for _, tc := range []struct {
		answer string
		added  bool
	}{{"y\n", true}, {"YES\n", true}, {"n\n", false}, {"\n", false}, {"", false}} {
		t.Run(strings.TrimSpace(tc.answer)+"/"+fmt.Sprint(tc.added), func(t *testing.T) {
			w := newWorld(t, stubSource{})
			root := scanTree(t, w)
			w.app.Input = strings.NewReader(tc.answer)
			code, stdout, stderr := run(t, w.app, "projects", "add", "--scan", root)
			if !strings.Contains(stdout, "Add the 2 marked projects? [y/N]: ") {
				t.Fatalf("no prompt: %q", stdout)
			}
			got := registered(t, w)
			if tc.added {
				if code != 0 || fmt.Sprint(got) != "[a b]" || !strings.Contains(stdout, "added a ") {
					t.Fatalf("exit %d %v\n%s\n%s", code, got, stdout, stderr)
				}
			} else if code != 1 || len(got) != 0 || stderr != "ork: cancelled\n" || strings.Contains(stdout, "added a ") {
				t.Fatalf("exit %d %v\n%s\n%s", code, got, stdout, stderr)
			}
		})
	}
}

func TestScanSkipsARegisteredPathAndANameClashAndSaysWhy(t *testing.T) {
	root := ""
	w := newWorld(t, stubSource{},
		registry.Project{Name: "b", Path: "/elsewhere/b"},
	)
	root = scanTree(t, w)
	// a is registered at its own path.
	if code, _, stderr := run(t, w.app, "projects", "add", filepath.Join(root, "a")); code != 0 {
		t.Fatal(stderr)
	}
	code, stdout, stderr := run(t, w.app, "projects", "add", "--scan", root, "--yes")
	got := lines(stdout)
	wantRows := []string{
		"[-] a slicer " + root + "/a already registered as a",
		`[-] b adj ` + root + `/b name "b" is registered for /elsewhere/b`,
		"[ ] c git " + root + "/c",
	}
	if code != 0 || stderr != "" || len(got) != 6 || fmt.Sprint(got[1:4]) != fmt.Sprint(wantRows) || got[4] != legend {
		t.Fatalf("exit %d\n%q\n%q", code, got, stderr)
	}
	if !strings.HasPrefix(got[5], "nothing to add") || len(strings.Split(stdout, "\n")[5]) >= 80 {
		t.Fatalf("nothing-to-add line: %q", got[5])
	}
	if reg := registered(t, w); fmt.Sprint(reg) != "[b a]" {
		t.Fatalf("registered %v", reg)
	}
}

// removeAfterStart makes the daemon start, then removes a folder the scan
// already listed, so the daemon refuses that row when it is added.
func removeAfterStart(w *world, dir string) {
	start := w.app.Start
	w.app.Start = func(ctx context.Context) error {
		err := start(ctx)
		os.RemoveAll(dir)
		return err
	}
}

func TestScanOneFailingRowDoesNotStopTheOthers(t *testing.T) {
	w := newWorld(t, stubSource{})
	root := filepath.Join(w.dir, "code")
	for _, d := range []string{"one/.slicer", "two/.slicer", "three/.slicer"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	removeAfterStart(w, filepath.Join(root, "three"))
	code, stdout, stderr := run(t, w.app, "projects", "add", "--scan", root, "--yes")
	out := strings.Join(lines(stdout), "\n")
	for _, want := range []string{
		"added one " + root + "/one",
		"failed three: path " + `"` + root + `/three" does not exist`,
		"added two " + root + "/two",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if code != 1 || !strings.Contains(stderr, "1 of 3 projects were not added") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if reg := registered(t, w); fmt.Sprint(reg) != "[one two]" {
		t.Fatalf("registered %v", reg)
	}
}

func TestScanBlocksTheLaterOfTwoSameNamedFolders(t *testing.T) {
	w := newWorld(t, stubSource{})
	root := filepath.Join(w.dir, "code")
	for _, d := range []string{"one/app/.slicer", "two/app/.slicer", "three/zed/.slicer"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	code, stdout, stderr := run(t, w.app, "projects", "add", "--scan", root, "--yes")
	got := lines(stdout)
	wantRow := `[-] app slicer ` + root + `/two/app name "app" is also found at ` + root + `/one/app; add it with ork projects add PATH --name N`
	if code != 0 || stderr != "" || got[1] != "[x] app slicer "+root+"/one/app" || got[2] != "[x] zed slicer "+root+"/three/zed" || got[3] != wantRow {
		t.Fatalf("exit %d\n%q\n%q", code, got, stderr)
	}
	if reg := registered(t, w); fmt.Sprint(reg) != "[app zed]" {
		t.Fatalf("registered %v", reg)
	}
}

func TestScanJSONNamesEachOutcome(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	root := scanTree(t, w)
	if err := os.MkdirAll(filepath.Join(root, "other", "a", ".slicer"), 0o755); err != nil {
		t.Fatal(err)
	}
	removeAfterStart(w, filepath.Join(root, "b"))
	code, stdout, stderr := run(t, w.app, "projects", "add", "--scan", root, "--yes", "--json")
	var doc struct {
		Root    string
		Stopped bool
		Folders int
		Results []struct {
			Name, Path string
			Added      bool
			Project    *registry.Project
			Error      *struct{ Code, Message string }
		}
		Skipped []struct{ Name, Path, Reason string }
	}
	mustJSON(t, stdout, &doc)
	if code != 1 || stderr != "" || doc.Root != root || len(doc.Results) != 2 || doc.Stopped || doc.Folders < 5 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	r := doc.Results
	if !r[0].Added || r[0].Name != "a" || r[0].Project == nil || r[0].Project.Path != root+"/a" ||
		r[1].Added || r[1].Name != "b" || r[1].Project != nil || r[1].Error == nil || r[1].Error.Code != daemon.CodeInvalidPath {
		t.Fatalf("results: %s", stdout)
	}
	if len(doc.Skipped) != 2 || doc.Skipped[0].Name != "c" || doc.Skipped[1].Name != "a" || !strings.Contains(doc.Skipped[1].Reason, "also found at") {
		t.Fatalf("skipped: %s", stdout)
	}
}

func TestScanJSONAllAddedExitsZero(t *testing.T) {
	w := newWorld(t, stubSource{})
	root := scanTree(t, w)
	code, stdout, _ := run(t, w.app, "projects", "add", "--scan", root, "--yes", "--json")
	var doc struct{ Results []struct{ Added bool } }
	mustJSON(t, stdout, &doc)
	if code != 0 || len(doc.Results) != 2 || !doc.Results[0].Added || !doc.Results[1].Added {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestScanUsageErrorsStartNoDaemon(t *testing.T) {
	a := noDaemon(t)
	root := t.TempDir()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"projects", "add", "--scan", "relative/dir", "--yes"}, `path "relative/dir" is not absolute`},
		{[]string{"projects", "add", "--scan", root, "--json"}, "--scan with --json needs --yes"},
		{[]string{"projects", "add", "--scan", root, "/also/a/path"}, "--scan takes no PATH"},
		{[]string{"projects", "add", "--scan", root, "--name", "x"}, "--scan does not take --name"},
		{[]string{"projects", "add", "/some/path", "--yes"}, "--yes needs --scan"},
		{[]string{"projects", "add", "--yes"}, "--yes needs --scan"},
	} {
		code, out, errOut := run(t, a, tc.args...)
		if code != 2 || !strings.Contains(errOut+out, tc.want) || strings.Contains(out, "added") {
			t.Errorf("%v: exit %d\n%s\n%s", tc.args, code, out, errOut)
		}
	}
}

func TestScanOfAFolderWithNoProjectOrNoFolder(t *testing.T) {
	a := noDaemon(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plain", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, a, "projects", "add", "--scan", root, "--yes")
	if code != 1 || !strings.Contains(errOut, "no projects found under "+root) {
		t.Errorf("empty: exit %d %q", code, errOut)
	}
	missing := filepath.Join(root, "missing")
	code, _, errOut = run(t, a, "projects", "add", "--scan", missing, "--yes")
	if code != 1 || !strings.Contains(errOut, missing) || !strings.Contains(errOut, "does not exist") {
		t.Errorf("missing: exit %d %q", code, errOut)
	}
	code, out, _ := run(t, a, "projects", "add", "--scan", missing, "--yes", "--json")
	if code != 1 || !strings.Contains(out, daemon.CodeInvalidPath) {
		t.Errorf("missing json: exit %d %q", code, out)
	}
}

func TestScanOfAProjectFolderListsItself(t *testing.T) {
	w := newWorld(t, stubSource{})
	root := filepath.Join(w.dir, "solo")
	if err := os.MkdirAll(filepath.Join(root, ".slicer"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(t, w.app, "projects", "add", "--scan", root, "--yes")
	if code != 0 || !strings.Contains(strings.Join(lines(stdout), "\n"), "[x] solo slicer "+root) || fmt.Sprint(registered(t, w)) != "[solo]" {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
}

func TestScanLegendOnlyWhenAPlainRepoIsListed(t *testing.T) {
	w := newWorld(t, stubSource{})
	root := filepath.Join(w.dir, "solo")
	if err := os.MkdirAll(filepath.Join(root, ".slicer"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, stdout, _ := run(t, w.app, "projects", "add", "--scan", root, "--yes")
	if strings.Contains(stdout, legend) {
		t.Fatalf("legend without a plain repo:\n%s", stdout)
	}
}

// Names and paths print as one plain line each; JSON keeps the real name.
func TestScanPrintsHostileNamesAsPlainText(t *testing.T) {
	w := newWorld(t, stubSource{})
	root := filepath.Join(w.dir, "code")
	evil := "ev\x1b[31mil\nname\x1b]0;t\x07"
	if err := os.MkdirAll(filepath.Join(root, evil, ".slicer"), 0o755); err != nil {
		t.Skip(err)
	}
	code, stdout, stderr := run(t, w.app, "projects", "add", "--scan", root, "--yes")
	if code != 0 || strings.ContainsAny(stdout+stderr, "\x1b\x07") || strings.Count(stdout, "\n") != 3 || !strings.Contains(stdout, "evil name") {
		t.Fatalf("exit %d\n%q\n%q", code, stdout, stderr)
	}
}

func TestScanSaysWhenItStoppedAtALimit(t *testing.T) {
	w := newWorld(t, stubSource{})
	root := filepath.Join(w.dir, "code")
	if err := os.MkdirAll(filepath.Join(root, "a", ".slicer"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < projscan.MaxFolders+20; i++ {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("z%04d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	code, stdout, _ := run(t, w.app, "projects", "add", "--scan", root, "--yes")
	want := fmt.Sprintf("stopped after %d folders and 1 project; scan a narrower folder to see more", projscan.MaxFolders)
	if code != 0 || !strings.Contains(stdout, want) {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	_, stdout, _ = run(t, w.app, "projects", "add", "--scan", root, "--yes", "--json")
	var doc struct {
		Stopped bool
		Folders int
	}
	mustJSON(t, stdout, &doc)
	if !doc.Stopped || doc.Folders != projscan.MaxFolders {
		t.Fatalf("json: %s", stdout)
	}
	// With nothing found, the error says the scan was cut short.
	empty := filepath.Join(w.dir, "empty")
	for i := 0; i < projscan.MaxFolders+20; i++ {
		os.MkdirAll(filepath.Join(empty, fmt.Sprintf("z%04d", i)), 0o755)
	}
	code, _, stderr := run(t, w.app, "projects", "add", "--scan", empty, "--yes")
	if code != 1 || !strings.Contains(stderr, "no projects found under "+empty) || !strings.Contains(stderr, fmt.Sprintf("stopped after %d folders and 0 projects", projscan.MaxFolders)) {
		t.Fatalf("exit %d: %q", code, stderr)
	}
}

func TestResultLineIsOnePlainLine(t *testing.T) {
	failed := resultLine(scanResult{Name: "a\x1b[31mb", Error: &ipc.Error{Code: "x", Message: "refused\nsecond\x1b[2J line"}})
	added := resultLine(scanResult{Name: "n\nm", Path: "/p\x07q", Added: true, Project: &registry.Project{Adj: true}})
	if failed != "failed ab: refused second line" || added != "added n m /pq (adj)" {
		t.Fatalf("%q %q", failed, added)
	}
}

// A blocked row's reason names the first folder, so it is plain text too.
func TestScanPrintsAHostileReasonAsPlainText(t *testing.T) {
	w := newWorld(t, stubSource{})
	root := filepath.Join(w.dir, "code")
	for _, d := range []string{"a/x\x1b[31m\nx/.slicer", "b/x\x1b[31m\nx/.slicer"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Skip(err)
		}
	}
	_, stdout, stderr := run(t, w.app, "projects", "add", "--scan", root, "--yes")
	if strings.ContainsAny(stdout+stderr, "\x1b\x07") || strings.Count(stdout, "\n") != 4 || !strings.Contains(stdout, "also found at") {
		t.Fatalf("%q %q", stdout, stderr)
	}
}
