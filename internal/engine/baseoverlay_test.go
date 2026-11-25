// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// baseOverlayRoutes answers the three git reads that list the slice's changed
// paths against the merge base "base".
func baseOverlayRoutes(W, changed, untracked string, extra ...clitest.Route) []clitest.Route {
	return append([]clitest.Route{
		{Tool: "git", Args: []string{"-C", W, "merge-base", "main", "HEAD"}, Stdout: "base\n"},
		{Tool: "git", Args: []string{"-C", W, "diff", "--name-only", "-z", "--no-renames", "--no-relative", "base"}, Stdout: changed},
		{Tool: "git", Args: []string{"-C", W, "ls-files", "--others", "--exclude-standard", "-z", "--full-name"}, Stdout: untracked},
	}, extra...)
}

func TestBaseOverlayAndUntestedCallSitesShareOneBranchRead(t *testing.T) {
	h := ucsReviewerHarness(t, ucsReviewerFiles, "internal/x/x.go\x00", ucsXRoutes)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	reviewers := 0
	for _, req := range h.runner.requests() {
		if req.Role != RoleReviewer {
			continue
		}
		reviewers++
		base, hasBase := baseOverlayBody(req.Brief)
		ucs, hasUCS := ucsBody(req.Brief)
		if !hasBase || !strings.Contains(base, "overlay-1.json") || !hasUCS || !strings.Contains(ucs, "- `Helper`") {
			t.Errorf("reviewer sections missing: base = %q, call sites = %q", base, ucs)
		}
	}
	for _, args := range [][]string{
		{"-C", h.P, "symbolic-ref", "--short", "HEAD"},
		{"-C", h.W, "merge-base", "main", "HEAD"},
		{"-C", h.W, "diff", "--name-only"},
		{"-C", h.W, "ls-files", "--others", "--exclude-standard", "-z", "--full-name"},
	} {
		if reads := h.calls("git", args...); len(reads) != reviewers {
			t.Errorf("git %v: %d reads for %d reviewer rounds, want one each", args, len(reads), reviewers)
		}
	}
}

func baseOverlayShow(W, path, stdout string) clitest.Route {
	return clitest.Route{Tool: "git", Args: []string{"-C", W, "show", "base:" + path}, Stdout: stdout}
}

// baseOverlayNotInBase is the answer git gives for a path the base lacks.
func baseOverlayNotInBase(W, path string) clitest.Route {
	return clitest.Route{Tool: "git", Args: []string{"-C", W, "show", "base:" + path}, Exit: 128, Stderr: fmt.Sprintf("fatal: path '%s' does not exist in 'base'\n", path)}
}

// baseOverlayReviewer returns the reviewer's request of a finished run.
func baseOverlayReviewer(t *testing.T, h *harness) Request {
	t.Helper()
	for _, req := range h.runner.requests() {
		if req.Role == RoleReviewer {
			return req
		}
	}
	t.Fatalf("no reviewer ran: %v", h.runner.roles())
	return Request{}
}

// baseOverlayBody returns the body of the brief's Base overlays section.
func baseOverlayBody(brief string) (string, bool) {
	_, body, found := strings.Cut(brief, "## Base overlays\n\n")
	if !found {
		return "", false
	}
	body, _, _ = strings.Cut(body, "\n## ")
	return body, true
}

// baseOverlayEntries returns the entry lines of the section body.
func baseOverlayEntries(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- `") {
			out = append(out, line)
		}
	}
	return out
}

func baseOverlayReplace(t *testing.T, file string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Replace map[string]string }
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return doc.Replace
}

func baseOverlayCommand(W, tmp string, n int, dir string) string {
	return fmt.Sprintf("go -C %s test -overlay %s ./%s", ShellWord(W), ShellWord(filepath.Join(tmp, fmt.Sprintf("overlay-%d.json", n))), dir)
}

func TestBaseOverlaySectionForChangedGoFile(t *testing.T) {
	const source = "package x\n\nfunc Old() int { return 1 }\n"
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "internal/x/x.go\x00internal/x/x_test.go\x00README.md\x00", "",
				baseOverlayShow(W, "internal/x/x.go", source))
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	body, found := baseOverlayBody(req.Brief)
	if !found {
		t.Fatalf("reviewer brief lacks the Base overlays section")
	}
	entries := baseOverlayEntries(body)
	if len(entries) != 1 || !strings.Contains(entries[0], "internal/x/x.go") {
		t.Fatalf("entries %q, want one for internal/x/x.go", entries)
	}
	if want := baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x"); !strings.Contains(entries[0], want) {
		t.Errorf("entry lacks the command %q:\n%s", want, entries[0])
	}
	for _, gone := range []string{"x_test.go", "README"} {
		if strings.Contains(body, gone) {
			t.Errorf("section names %s:\n%s", gone, body)
		}
	}
	got, err := os.ReadFile(filepath.Join(req.TMPDir, "base", "internal", "x", "x.go"))
	if err != nil || string(got) != source {
		t.Errorf("base copy = %q, %v; want %q", got, err, source)
	}
	want := map[string]string{filepath.Join(h.W, "internal/x/x.go"): filepath.Join(req.TMPDir, "base", "internal/x/x.go")}
	if replace := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-1.json")); !reflect.DeepEqual(replace, want) {
		t.Errorf("overlay-1.json Replace = %v, want %v", replace, want)
	}
	if _, err := os.Stat(filepath.Join(req.TMPDir, "overlay-2.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("overlay-2.json: %v, want it absent", err)
	}
	for _, line := range []string{"only that file reverted", "-run NAME", "one test per command", "never join test names in one `-run` pattern"} {
		if !strings.Contains(body, line) {
			t.Errorf("section lacks %q:\n%s", line, body)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "-run") && strings.Contains(line, "|") {
			t.Errorf("section has a -run line with a pipe: %s", line)
		}
	}
	for _, entry := range entries {
		if strings.Contains(entry, "|") {
			t.Errorf("entry holds a pipe: %s", entry)
		}
	}
}

func TestBaseOverlayNewFileMapsToEmpty(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "", "internal/x/new.go\x00", baseOverlayNotInBase(W, "internal/x/new.go"))
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	body, found := baseOverlayBody(req.Brief)
	if !found {
		t.Fatalf("reviewer brief lacks the Base overlays section")
	}
	for _, want := range []string{"new in this slice", "hides", baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x")} {
		if !strings.Contains(body, want) {
			t.Errorf("section lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "incomplete") {
		t.Errorf("a new file is not an incomplete check:\n%s", body)
	}
	replace := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-1.json"))
	if value, ok := replace[filepath.Join(h.W, "internal/x/new.go")]; !ok || value != "" || len(replace) != 1 {
		t.Errorf("overlay-1.json Replace = %q, want the new file mapped to the empty string", replace)
	}
	if _, err := os.Stat(filepath.Join(req.TMPDir, "base")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a base copy was written for a new file: %v", err)
	}
}

func TestBaseOverlayNoSectionWithoutGoFiles(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "internal/x/x_test.go\x00README.md\x00docs/a.go.md\x00", "")
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	// The section is absent because ork read the changed paths and found no
	// non-test Go file, not because it never looked.
	if len(h.calls("git", "-C", h.W, "diff", "--name-only")) == 0 {
		t.Error("ork never read the changed paths for the reviewer")
	}
	if shown := h.calls("git", "-C", h.W, "show"); len(shown) != 0 {
		t.Errorf("git show calls for a diff with no non-test Go file: %v", shown)
	}
	// The reviewer prompt itself names the heading, so look for the section.
	if body, found := baseOverlayBody(req.Brief); found {
		t.Errorf("reviewer brief holds a Base overlays section:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(req.TMPDir, "base")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("base directory: %v, want it absent", err)
	}
	if overlays, _ := filepath.Glob(filepath.Join(req.TMPDir, "overlay-*.json")); len(overlays) != 0 {
		t.Errorf("overlay files written: %v", overlays)
	}
}

func TestBaseOverlayAdvisory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		routes func(W string) []clitest.Route
	}{
		{"changed paths unreadable", func(W string) []clitest.Route {
			routes := baseOverlayRoutes(W, "internal/x/x.go\x00", "")
			routes[1].Exit = 1
			return routes
		}},
		{"git show fails", func(W string) []clitest.Route {
			return baseOverlayRoutes(W, "internal/x/x.go\x00", "",
				clitest.Route{Tool: "git", Args: []string{"-C", W, "show", "base:internal/x/x.go"}, Exit: 128, Stderr: "fatal: bad object base\n"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.routesFor = func(P, W string) []clitest.Route { return tc.routes(W) }
			})
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			req := baseOverlayReviewer(t, h)
			body, found := baseOverlayBody(req.Brief)
			if !found {
				t.Fatalf("reviewer brief lacks the Base overlays section")
			}
			for _, want := range []string{"incomplete", "git show"} {
				if !strings.Contains(body, want) {
					t.Errorf("section lacks %q:\n%s", want, body)
				}
			}
			if overlays, _ := filepath.Glob(filepath.Join(req.TMPDir, "overlay-*.json")); len(overlays) != 0 {
				t.Errorf("overlay files written for an unreadable base: %v", overlays)
			}
		})
	}
}

// A path that is absolute or climbs out with .. is never read or written: the
// base copy goes under TMPDIR/base only.
func TestBaseOverlayRejectsEscapingPaths(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "../evil.go\x00/abs/evil.go\x00internal/../../evil2.go\x00", "")
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	body, found := baseOverlayBody(req.Brief)
	if !found || !strings.Contains(body, "incomplete") {
		t.Errorf("section = %q, found %v; want an incomplete notice", body, found)
	}
	if entries := baseOverlayEntries(body); len(entries) != 0 {
		t.Errorf("entries for rejected paths: %q", entries)
	}
	if shown := h.calls("git", "-C", h.W, "show"); len(shown) != 0 {
		t.Errorf("git show ran for a rejected path: %v", shown)
	}
	for _, p := range []string{filepath.Join(req.TMPDir, "..", "evil.go"), filepath.Join(req.TMPDir, "base"), filepath.Join(req.TMPDir, "overlay-1.json")} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: %v, want it absent", p, err)
		}
	}
}

func TestBaseOverlayListLimit(t *testing.T) {
	var changed strings.Builder
	var extra []clitest.Route
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			for i := 25; i >= 1; i-- {
				path := fmt.Sprintf("internal/p/f%02d.go", i)
				changed.WriteString(path + "\x00")
				extra = append(extra, baseOverlayShow(W, path, "package p\n"))
			}
			return baseOverlayRoutes(W, changed.String(), "", extra...)
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	body, found := baseOverlayBody(req.Brief)
	if !found {
		t.Fatalf("reviewer brief lacks the Base overlays section")
	}
	entries := baseOverlayEntries(body)
	if len(entries) != 20 {
		t.Fatalf("%d entries, want 20", len(entries))
	}
	if !strings.Contains(entries[0], "f01.go") || !strings.Contains(entries[19], "f20.go") {
		t.Errorf("entries are not the first 20 in order: %q ... %q", entries[0], entries[19])
	}
	if !strings.Contains(body, "5 more changed Go files were left out") {
		t.Errorf("section does not say 5 were left out:\n%s", body)
	}
	for n, want := range map[int]bool{20: true, 21: false} {
		_, err := os.Stat(filepath.Join(req.TMPDir, fmt.Sprintf("overlay-%d.json", n)))
		if (err == nil) != want {
			t.Errorf("overlay-%d.json exists = %v, want %v", n, err == nil, want)
		}
	}
}

func TestBaseOverlayOnlyForTheReviewer(t *testing.T) {
	h := architectHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "internal/x/x.go\x00", "", baseOverlayShow(W, "internal/x/x.go", "package x\n"))
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	seen := map[string]bool{}
	for _, req := range h.runner.requests() {
		seen[req.Role] = true
		_, has := baseOverlayBody(req.Brief)
		if want := req.Role == RoleReviewer; has != want {
			t.Errorf("%s brief holds a Base overlays section = %v, want %v", req.Role, has, want)
		}
	}
	for _, role := range []string{RoleReviewer, RoleArchitect, RoleGate} {
		if !seen[role] {
			t.Fatalf("role %s did not run: %v", role, h.runner.roles())
		}
	}
}
