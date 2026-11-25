// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const (
	ucsHelperSrc = "package x\n\nfunc Helper() int { return 1 }\n"
	ucsRunSrc    = "package x\n\nfunc Run() int { return Helper() }\n"
	ucsTestSrc   = "package x\n\nimport \"testing\"\n\nfunc TestHelper(t *testing.T) { Helper() }\n"
)

func ucsFind(base, tree map[string]string, changed ...string) ([]untestedFinding, []string) {
	return untestedCallSites(base, tree, changed)
}

func ucsRequireNone(t *testing.T, findings []untestedFinding, unparsed []string) {
	t.Helper()
	if len(findings) != 0 || len(unparsed) != 0 {
		t.Fatalf("findings = %+v, unparsed = %v; want none", findings, unparsed)
	}
}

func TestUntestedCallSitesFlagsDirectOnlyFunction(t *testing.T) {
	tree := map[string]string{"x.go": ucsHelperSrc, "y.go": ucsRunSrc, "x_test.go": ucsTestSrc}
	findings, unparsed := ucsFind(map[string]string{}, tree, "x.go")
	if len(unparsed) != 0 {
		t.Fatalf("unparsed = %v", unparsed)
	}
	want := []untestedFinding{{
		Key: "Helper", File: "x.go",
		Sites: []untestedSite{{File: "y.go", Func: "Run"}},
		Tests: []string{"x_test.go"},
	}}
	if !reflect.DeepEqual(findings, want) {
		t.Fatalf("findings = %+v, want %+v", findings, want)
	}
}

func TestUntestedCallSitesReachedByTest(t *testing.T) {
	tree := map[string]string{"x.go": ucsHelperSrc, "y.go": ucsRunSrc,
		"x_test.go": ucsTestSrc + "\nfunc TestRun(t *testing.T) { Run() }\n"}
	findings, unparsed := ucsFind(map[string]string{}, tree, "x.go")
	ucsRequireNone(t, findings, unparsed)
}

func TestUntestedCallSitesReachedTransitively(t *testing.T) {
	tree := map[string]string{"x.go": ucsHelperSrc,
		"y.go":      ucsRunSrc + "\nfunc Outer() int { return Run() }\n",
		"x_test.go": ucsTestSrc + "\nfunc TestRun(t *testing.T) { Outer() }\n"}
	findings, unparsed := ucsFind(map[string]string{}, tree, "x.go")
	ucsRequireNone(t, findings, unparsed)
}

func TestUntestedCallSitesNotNewInBase(t *testing.T) {
	tree := map[string]string{"x.go": ucsHelperSrc, "y.go": ucsRunSrc, "x_test.go": ucsTestSrc}
	findings, unparsed := ucsFind(map[string]string{"x.go": ucsHelperSrc}, tree, "x.go")
	ucsRequireNone(t, findings, unparsed)
}

func TestUntestedCallSitesNeedsDirectTestCall(t *testing.T) {
	tree := map[string]string{"x.go": ucsHelperSrc, "y.go": ucsRunSrc,
		"x_test.go": "package x\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n"}
	findings, unparsed := ucsFind(map[string]string{}, tree, "x.go")
	ucsRequireNone(t, findings, unparsed)
}

func TestUntestedCallSitesNeedsProductCallSite(t *testing.T) {
	tree := map[string]string{"x.go": ucsHelperSrc, "x_test.go": ucsTestSrc}
	findings, unparsed := ucsFind(map[string]string{}, tree, "x.go")
	ucsRequireNone(t, findings, unparsed)
}

func TestUntestedCallSitesMatchesMethodsAndLiterals(t *testing.T) {
	const helper = "package x\n\nfunc Helper(n int) {\n\tif n > 0 {\n\t\tHelper(n - 1)\n\t}\n}\n"
	t.Run("selector call", func(t *testing.T) {
		tree := map[string]string{"x.go": helper,
			"y.go":      "package x\n\nfunc Run() { r.Helper(1) }\n",
			"x_test.go": ucsTestSrc}
		findings, _ := ucsFind(map[string]string{}, tree, "x.go")
		want := []untestedSite{{File: "y.go", Func: "Run"}}
		if len(findings) != 1 || !reflect.DeepEqual(findings[0].Sites, want) {
			t.Fatalf("findings = %+v, want one with sites %v", findings, want)
		}
	})
	t.Run("call in a func literal counts for the enclosing function", func(t *testing.T) {
		tree := map[string]string{"x.go": helper,
			"y.go":      "package x\n\nfunc Run() {\n\tf := func() { Helper(1) }\n\tf()\n}\n",
			"x_test.go": ucsTestSrc}
		findings, _ := ucsFind(map[string]string{}, tree, "x.go")
		want := []untestedSite{{File: "y.go", Func: "Run"}}
		if len(findings) != 1 || !reflect.DeepEqual(findings[0].Sites, want) {
			t.Fatalf("findings = %+v, want one with sites %v", findings, want)
		}
	})
	t.Run("recursion is not a call site", func(t *testing.T) {
		tree := map[string]string{"x.go": helper, "x_test.go": ucsTestSrc}
		findings, unparsed := ucsFind(map[string]string{}, tree, "x.go")
		ucsRequireNone(t, findings, unparsed)
	})
	t.Run("a call outside any function is ignored", func(t *testing.T) {
		tree := map[string]string{"x.go": ucsHelperSrc,
			"y.go":      "package x\n\nvar v = Helper()\n",
			"x_test.go": ucsTestSrc}
		findings, unparsed := ucsFind(map[string]string{}, tree, "x.go")
		ucsRequireNone(t, findings, unparsed)
	})
}

func TestUntestedCallSitesIgnoresTestFileFunctions(t *testing.T) {
	tree := map[string]string{
		"x_test.go": "package x\n\nimport \"testing\"\n\nfunc newThing() {}\n\nfunc TestA(t *testing.T) { newThing() }\n",
		"y.go":      "package x\n\nfunc Run() { newThing() }\n",
	}
	findings, unparsed := ucsFind(map[string]string{}, tree, "x_test.go")
	ucsRequireNone(t, findings, unparsed)
}

func TestUntestedCallSitesNewKeyIsReceiverAware(t *testing.T) {
	base := map[string]string{"x.go": "package x\n\nfunc (t T) Helper() {}\n"}
	tree := map[string]string{
		"x.go":      "package x\n\nfunc (t T) Helper() {}\n\nfunc (u *U) Helper() {}\n",
		"y.go":      "package x\n\nfunc Run() { v.Helper() }\n",
		"x_test.go": "package x\n\nimport \"testing\"\n\nfunc TestHelper(t *testing.T) { v.Helper() }\n",
	}
	findings, unparsed := ucsFind(base, tree, "x.go")
	if len(unparsed) != 0 || len(findings) != 1 || findings[0].Key != "U.Helper" {
		t.Fatalf("findings = %+v, unparsed = %v; want only U.Helper", findings, unparsed)
	}
}

func TestUntestedCallSitesReportsUnparseableFiles(t *testing.T) {
	tree := map[string]string{"x.go": "package x\n\nfunc (\n", "x_test.go": ucsTestSrc}
	findings, unparsed := ucsFind(map[string]string{}, tree, "x.go")
	if len(findings) != 0 || !reflect.DeepEqual(unparsed, []string{"x.go"}) {
		t.Fatalf("findings = %+v, unparsed = %v; want x.go unparsed", findings, unparsed)
	}
}

func TestUntestedCallSitesSortsFindings(t *testing.T) {
	tree := map[string]string{
		"b.go":      "package x\n\nfunc Zed() {}\n\nfunc Alpha() {}\n",
		"a.go":      "package x\n\nfunc Mid() {}\n",
		"y.go":      "package x\n\nfunc Run() { Zed(); Alpha(); Mid() }\n",
		"x_test.go": "package x\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { Zed(); Alpha(); Mid() }\n",
	}
	findings, _ := ucsFind(map[string]string{}, tree, "b.go", "a.go")
	var got []string
	for _, f := range findings {
		got = append(got, f.File+":"+f.Key)
	}
	if want := []string{"a.go:Mid", "b.go:Alpha", "b.go:Zed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// ucsReviewerHarness writes files under the worktree and routes the diff.
func ucsReviewerHarness(t *testing.T, files map[string]string, changed string, extra func(W string) []clitest.Route) *harness {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			var more []clitest.Route
			if extra != nil {
				more = extra(W)
			}
			return append(baseOverlayRoutes(W, changed, "", more...), ucsWorktree(P, W)...)
		}
	})
	ucsWrite(t, h, files)
	return h
}

// ucsWorktree answers the checks ork makes of a worktree that already exists,
// as it does once the test has written files under it.
func ucsWorktree(P, W string) []clitest.Route {
	return []clitest.Route{
		{Tool: "git", Args: []string{"-C", W, "rev-parse", "--show-toplevel"}, Stdout: W + "\n"},
		{Tool: "git", Args: []string{"-C", W, "symbolic-ref", "--short", "HEAD"}, Stdout: item + "\n"},
		{Tool: "git", Args: []string{"-C", W, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
		{Tool: "git", Args: []string{"-C", P, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git") + "\n"},
	}
}

// ucsWrite writes real Go files under the harness's worktree.
func ucsWrite(t *testing.T, h *harness, files map[string]string) {
	t.Helper()
	for p, src := range files {
		full := filepath.Join(h.W, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ucsBody returns the body of the brief's Untested call sites section. The
// reviewer prompt may name the heading, so cut at the section's own blank line.
func ucsBody(brief string) (string, bool) {
	_, body, found := strings.Cut(brief, "## Untested call sites\n\n")
	if !found {
		return "", false
	}
	body, _, _ = strings.Cut(body, "\n## ")
	return body, true
}

func ucsEntries(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- `") {
			out = append(out, line)
		}
	}
	return out
}

var ucsReviewerFiles = map[string]string{
	"internal/x/x.go":      ucsHelperSrc,
	"internal/x/y.go":      ucsRunSrc,
	"internal/x/x_test.go": ucsTestSrc,
}

func ucsXRoutes(W string) []clitest.Route {
	return []clitest.Route{baseOverlayShow(W, "internal/x/x.go", "package x\n")}
}

func TestUntestedCallSitesReviewerBrief(t *testing.T) {
	h := ucsReviewerHarness(t, ucsReviewerFiles, "internal/x/x.go\x00internal/x/x_test.go\x00", ucsXRoutes)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	body, found := ucsBody(req.Brief)
	if !found {
		t.Fatalf("reviewer brief lacks the Untested call sites section")
	}
	entries := ucsEntries(body)
	want := "- `Helper` in `internal/x/x.go`: called from `internal/x/y.go` in `Run`; tests call it directly in `internal/x/x_test.go`."
	if len(entries) != 1 || entries[0] != want {
		t.Errorf("entries = %q, want [%q]", entries, want)
	}
	if !strings.Contains(body, "advisory") {
		t.Errorf("section does not say it is advisory:\n%s", body)
	}
	if strings.Contains(body, "Check incomplete") {
		t.Errorf("section reports an incomplete check:\n%s", body)
	}
}

func TestUntestedCallSitesOnlyForTheReviewer(t *testing.T) {
	h := architectHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return append(baseOverlayRoutes(W, "internal/x/x.go\x00internal/x/x_test.go\x00", "", ucsXRoutes(W)...), ucsWorktree(P, W)...)
		}
	})
	ucsWrite(t, h, ucsReviewerFiles)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	seen := map[string]bool{}
	for _, req := range h.runner.requests() {
		seen[req.Role] = true
		_, has := ucsBody(req.Brief)
		if want := req.Role == RoleReviewer; has != want {
			t.Errorf("%s brief holds an Untested call sites section = %v, want %v", req.Role, has, want)
		}
	}
	for _, role := range []string{RoleReviewer, RoleArchitect, RoleGate} {
		if !seen[role] {
			t.Fatalf("role %s did not run: %v", role, h.runner.roles())
		}
	}
}

func TestUntestedCallSitesNoSectionWhenClean(t *testing.T) {
	files := map[string]string{}
	for p, src := range ucsReviewerFiles {
		files[p] = src
	}
	files["internal/x/x_test.go"] += "\nfunc TestRun(t *testing.T) { Run() }\n"
	h := ucsReviewerHarness(t, files, "internal/x/x.go\x00internal/x/x_test.go\x00", ucsXRoutes)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	if body, found := ucsBody(req.Brief); found {
		t.Errorf("reviewer brief holds an Untested call sites section:\n%s", body)
	}
	// The section is absent because ork looked and found nothing.
	if len(h.calls("git", "-C", h.W, "show", "base:internal/x/x.go")) < 2 {
		t.Errorf("ork did not read the base version for this check: %v", h.calls("git", "-C", h.W, "show"))
	}
}

func TestUntestedCallSitesNoSectionWithoutGoFiles(t *testing.T) {
	h := ucsReviewerHarness(t, nil, "internal/x/x_test.go\x00README.md\x00", nil)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	if body, found := ucsBody(req.Brief); found {
		t.Errorf("reviewer brief holds an Untested call sites section:\n%s", body)
	}
}

func TestUntestedCallSitesAdvisory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  map[string]string
		routes func(W string) []clitest.Route
		diff   bool // fail the diff read
	}{
		{name: "changed paths unreadable", files: ucsReviewerFiles, routes: ucsXRoutes, diff: true},
		{name: "git show fails", files: ucsReviewerFiles, routes: func(W string) []clitest.Route {
			return []clitest.Route{{Tool: "git", Args: []string{"-C", W, "show", "base:internal/x/x.go"}, Exit: 128, Stderr: "fatal: bad object base\n"}}
		}},
		{name: "unparseable Go file", files: map[string]string{
			"internal/x/x.go":      "package x\n\nfunc (\n",
			"internal/x/x_test.go": ucsTestSrc,
		}, routes: ucsXRoutes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.routesFor = func(P, W string) []clitest.Route {
					routes := baseOverlayRoutes(W, "internal/x/x.go\x00internal/x/x_test.go\x00", "", tc.routes(W)...)
					if tc.diff {
						routes[1].Exit = 1
					}
					return append(routes, ucsWorktree(P, W)...)
				}
			})
			ucsWrite(t, h, tc.files)
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			req := baseOverlayReviewer(t, h)
			body, found := ucsBody(req.Brief)
			if !found {
				t.Fatalf("reviewer brief lacks the Untested call sites section")
			}
			for _, want := range []string{"Check incomplete", "does not block the run"} {
				if !strings.Contains(body, want) {
					t.Errorf("section lacks %q:\n%s", want, body)
				}
			}
		})
	}
}

func TestUntestedCallSitesListLimit(t *testing.T) {
	var helpers, calls, tests strings.Builder
	helpers.WriteString("package x\n\n")
	calls.WriteString("package x\n\nfunc Run() {\n")
	tests.WriteString("package x\n\nimport \"testing\"\n\nfunc TestAll(t *testing.T) {\n")
	for i := 25; i >= 1; i-- {
		fmt.Fprintf(&helpers, "func H%02d() {}\n", i)
		fmt.Fprintf(&calls, "\tH%02d()\n", i)
		fmt.Fprintf(&tests, "\tH%02d()\n", i)
	}
	calls.WriteString("}\n")
	tests.WriteString("}\n")
	files := map[string]string{
		"internal/x/x.go": helpers.String(), "internal/x/y.go": calls.String(), "internal/x/x_test.go": tests.String(),
	}
	h := ucsReviewerHarness(t, files, "internal/x/x.go\x00internal/x/x_test.go\x00", ucsXRoutes)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	body, found := ucsBody(req.Brief)
	if !found {
		t.Fatalf("reviewer brief lacks the Untested call sites section")
	}
	entries := ucsEntries(body)
	if len(entries) != 20 {
		t.Fatalf("%d entries, want 20", len(entries))
	}
	if !strings.HasPrefix(entries[0], "- `H01` ") || !strings.HasPrefix(entries[19], "- `H20` ") {
		t.Errorf("entries are not the first 20 in order: %q ... %q", entries[0], entries[19])
	}
	if !strings.Contains(body, "5 more functions were left out") {
		t.Errorf("section does not say 5 were left out:\n%s", body)
	}
}
