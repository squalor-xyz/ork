// SPDX-License-Identifier: GPL-3.0-or-later

package engine

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

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const olderBinaryNote = "Note: an ork binary older than this slice's code made this list, so it may offer commands that do not build."

func behindFunc(v bool) func(context.Context, registry.Project) bool {
	return func(context.Context, registry.Project) bool { return v }
}

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
	reviewers, gates := 0, 0
	for _, req := range h.runner.requests() {
		if req.Role == RoleGate {
			gates++
		}
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
		// A reviewer round shares one branch read between its sections, and a
		// gate round makes one for its Stale doc references section (S569).
		if reads := h.calls("git", args...); len(reads) != reviewers+gates {
			t.Errorf("git %v: %d reads for %d reviewer and %d gate rounds, want one each", args, len(reads), reviewers, gates)
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
	return fmt.Sprintf("go -C %s test -overlay %s -count=1 ./%s", ShellWord(W), ShellWord(filepath.Join(tmp, fmt.Sprintf("overlay-%d.json", n))), dir)
}

func baseOverlayVetCalls(h *harness) []clitest.Call {
	var calls []clitest.Call
	for _, call := range h.fake.CallsTo("go") {
		if len(call.Args) > 2 && call.Args[2] == "vet" {
			calls = append(calls, call)
		}
	}
	return calls
}

func TestBaseOverlayOtherFilesSkipTrackingPaths(t *testing.T) {
	tracking := []string{".slicer/index.json", ".slicer/config.json", ".slicer/slices/S04.json", "landed-changed/S04.md"}
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "internal/x/x.go\x00README.md\x00docs/landed-changed/notes.md\x00"+strings.Join(tracking, "\x00")+"\x00", "",
				baseOverlayShow(W, "internal/x/x.go", "package x\n"), baseOverlayShow(W, "README.md", "readme"), baseOverlayShow(W, "docs/landed-changed/notes.md", "notes"))
		}
	})
	req, body, entries := overlayVetResult(t, h)
	requireOtherFileLines(t, body, h.W, []string{"README.md", "docs/landed-changed/notes.md"})
	want := []string{"- `internal/x/x.go`: `" + baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x") + "`"}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("Go entries = %q, want %q", entries, want)
	}
	for _, p := range tracking {
		if strings.Contains(body, p) || len(h.calls("git", "-C", h.W, "show", "base:"+p)) != 0 {
			t.Errorf("tracking path read or shown: %s", p)
		}
	}
}

func TestBaseOverlayTrackingPathsDoNotCountTowardOtherFileLimit(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			changed := ".slicer/index.json\x00.slicer/slices/S04.json\x00landed-changed/S04.md\x00"
			var extra []clitest.Route
			for i := 25; i >= 1; i-- {
				p := fmt.Sprintf("docs/f%02d.md", i)
				changed += p + "\x00"
				extra = append(extra, baseOverlayShow(W, p, "doc"))
			}
			return baseOverlayRoutes(W, changed, "", extra...)
		}
	})
	_, body, _ := overlayVetResult(t, h)
	var want []string
	for i := 1; i <= 20; i++ {
		want = append(want, fmt.Sprintf("docs/f%02d.md", i))
	}
	requireOtherFileLines(t, body, h.W, want)
	if !strings.Contains(body, "5 more changed other files were left out") || strings.Contains(body, ".slicer/") || strings.Contains(body, "landed-changed/") {
		t.Errorf("wrong limit notice or tracking paths: %s", body)
	}
}

func TestBaseOverlayNoSectionForTrackingOnlyDiff(t *testing.T) {
	for _, untracked := range []bool{false, true} {
		t.Run(fmt.Sprint(untracked), func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.routesFor = func(P, W string) []clitest.Route {
					paths := ".slicer/index.json\x00landed-changed/S04.md\x00"
					if untracked {
						return baseOverlayRoutes(W, "", paths)
					}
					return baseOverlayRoutes(W, paths, "")
				}
			})
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			if body, found := baseOverlayBody(baseOverlayReviewer(t, h).Brief); found {
				t.Errorf("tracking-only diff has section: %s", body)
			}
			if calls := h.calls("git", "-C", h.W, "show"); len(calls) != 0 {
				t.Errorf("tracking-only git show calls: %v", calls)
			}
		})
	}
}

// importerJSON emits the same concatenated object stream as go list.
func importerJSON(pkg, field string, imports ...string) string {
	doc := map[string]any{"ImportPath": "m/" + pkg, "Module": map[string]string{"Path": "m"}}
	if pkg == "." {
		doc["ImportPath"] = "m"
	}
	if field != "" {
		doc[field] = imports
	}
	data, _ := json.Marshal(doc)
	return string(data) + "\n"
}

func importerHarness(t *testing.T, changed, listed string, listExit int, vet ...clitest.Reply) *harness {
	t.Helper()
	return newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			extra := []clitest.Route{
				{Tool: "go", Args: []string{"-C", W, "list", "-e", "-json", "./..."}, Stdout: listed, Exit: listExit},
				{Tool: "go", Args: []string{"-C", W, "vet"}, Prefix: true, Seq: vet},
			}
			for _, p := range strings.Split(strings.TrimSuffix(changed, "\x00"), "\x00") {
				extra = append(extra, baseOverlayShow(W, p, "package x\n"))
			}
			return baseOverlayRoutes(W, changed, "", extra...)
		}
	})
}

func importerLines(body string) []string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- Importer ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func importerLine(W, tmp, file string, n int, pkg string) string {
	return "- Importer `./" + pkg + "` of `" + file + "`: `" + baseOverlayCommand(W, tmp, n, pkg) + "`"
}

func TestBaseOverlayListsDirectImporterPackages(t *testing.T) {
	listed := importerJSON("internal/x", "Imports", "m/internal/x") +
		importerJSON("internal/tui", "TestImports", "m/internal/x", "m/internal/x") +
		importerJSON("internal/cli", "Imports", "m/internal/x") +
		importerJSON("internal/ext", "XTestImports", "m/internal/x") +
		importerJSON("internal/far", "Imports", "m/internal/cli") + importerJSON("internal/z", "")
	h := importerHarness(t, "internal/x/x.go\x00", listed, 0, clitest.Reply{})
	req, body, entries := overlayVetResult(t, h)
	var want []string
	for _, pkg := range []string{"internal/cli", "internal/ext", "internal/tui"} {
		want = append(want, importerLine(h.W, req.TMPDir, "internal/x/x.go", 1, pkg))
		calls := h.calls("go", "-C", h.W, "vet", "-overlay", filepath.Join(req.TMPDir, "overlay-1.json"), "./"+pkg)
		if len(calls) != 1 {
			t.Errorf("importer %s vet calls = %v", pkg, calls)
		}
	}
	if got := importerLines(body); !reflect.DeepEqual(got, want) {
		t.Errorf("importer lines = %q, want %q", got, want)
	}
	if len(entries) != 1 || strings.Index(body, want[0]) < strings.Index(body, entries[0]) {
		t.Errorf("Go entry count or importer order: %s", body)
	}
	if calls := h.calls("go", "-C", h.W, "list"); !reflect.DeepEqual(calls, [][]string{{"-C", h.W, "list", "-e", "-json", "./..."}}) {
		t.Errorf("list calls = %v", calls)
	}
}

func TestBaseOverlayImporterLinePerRevertedFile(t *testing.T) {
	listed := importerJSON("internal/x", "") + importerJSON("internal/y", "") + importerJSON("internal/cli", "Imports", "m/internal/x", "m/internal/y")
	h := importerHarness(t, "internal/y/b.go\x00internal/x/a.go\x00", listed, 0, clitest.Reply{})
	req, body, _ := overlayVetResult(t, h)
	want := []string{importerLine(h.W, req.TMPDir, "internal/x/a.go", 1, "internal/cli"), importerLine(h.W, req.TMPDir, "internal/y/b.go", 2, "internal/cli")}
	if got := importerLines(body); !reflect.DeepEqual(got, want) {
		t.Errorf("per-file importer lines = %q, want %q", got, want)
	}
	if calls := h.calls("go", "-C", h.W, "list"); len(calls) != 1 {
		t.Errorf("list calls = %v", calls)
	}
}

func TestBaseOverlayImporterThatDoesNotBuildIsReported(t *testing.T) {
	listed := importerJSON("internal/x", "") + importerJSON("internal/cli", "Imports", "m/internal/x") + importerJSON("internal/tui", "Imports", "m/internal/x")
	h := importerHarness(t, "internal/x/x.go\x00", listed, 0, clitest.Reply{}, clitest.Reply{Exit: 1, Stderr: "undefined helper"}, clitest.Reply{})
	req, body, _ := overlayVetResult(t, h)
	want := []string{"- Importer `./internal/cli` of `internal/x/x.go`: does not build: undefined helper.", importerLine(h.W, req.TMPDir, "internal/x/x.go", 1, "internal/tui")}
	if got := importerLines(body); !reflect.DeepEqual(got, want) {
		t.Errorf("importer build results = %q, want %q", got, want)
	}
}

func TestBaseOverlayImporterListFailureIsAdvisory(t *testing.T) {
	for _, mode := range []string{"exit", "json"} {
		t.Run(mode, func(t *testing.T) {
			exit, listed := 1, ""
			if mode == "json" {
				exit, listed = 0, "{invalid"
			}
			h := importerHarness(t, "internal/x/x.go\x00", listed, exit, clitest.Reply{})
			req, body, entries := overlayVetResult(t, h)
			if len(entries) != 1 || !strings.Contains(entries[0], baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x")) || len(importerLines(body)) != 0 || strings.Count(body, "Check incomplete: the packages that import the reverted files could not be listed") != 1 {
				t.Errorf("missing advisory list failure with own command: %s", body)
			}
		})
	}
}

func TestBaseOverlayNoImporterListWhenNoOverlayBuilds(t *testing.T) {
	for _, builds := range []bool{true, false} {
		t.Run(fmt.Sprint(builds), func(t *testing.T) {
			vet := clitest.Reply{}
			if !builds {
				vet = clitest.Reply{Exit: 1, Stderr: "undefined helper"}
			}
			h := importerHarness(t, "internal/x/x.go\x00", importerJSON("internal/x", "")+importerJSON("internal/cli", "Imports", "m/internal/x"), 0, vet)
			_, body, _ := overlayVetResult(t, h)
			want := 0
			if builds {
				want = 1
			}
			if len(importerLines(body)) != want || len(h.calls("go", "-C", h.W, "list")) != want {
				t.Errorf("builds %v: wrong importer lines or list calls: %s", builds, body)
			}
		})
	}
}

func TestBaseOverlayImporterListLimit(t *testing.T) {
	listed := importerJSON("internal/x", "") + importerJSON("internal/y", "")
	for i := 12; i >= 1; i-- {
		listed += importerJSON(fmt.Sprintf("internal/c%02d", i), "Imports", "m/internal/x", "m/internal/y")
	}
	h := importerHarness(t, "internal/y/b.go\x00internal/x/a.go\x00", listed, 0, clitest.Reply{})
	req, body, _ := overlayVetResult(t, h)
	var want []string
	for i := 1; i <= 12; i++ {
		want = append(want, importerLine(h.W, req.TMPDir, "internal/x/a.go", 1, fmt.Sprintf("internal/c%02d", i)))
	}
	for i := 1; i <= 8; i++ {
		want = append(want, importerLine(h.W, req.TMPDir, "internal/y/b.go", 2, fmt.Sprintf("internal/c%02d", i)))
	}
	if got := importerLines(body); !reflect.DeepEqual(got, want) || !strings.Contains(body, "4 more importer lines were left out (ork lists at most 20).") {
		t.Errorf("wrong capped importer list: %q; body: %s", got, body)
	}
}

func overlayVetHarness(t *testing.T, changed string, isNew bool, replies ...clitest.Reply) *harness {
	t.Helper()
	return newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			extra := []clitest.Route{{Tool: "go", Args: []string{"-C", W, "vet"}, Prefix: true, Seq: replies}}
			for _, p := range strings.Split(strings.TrimRight(changed, "\x00"), "\x00") {
				if isNew {
					extra = append(extra, baseOverlayNotInBase(W, p))
				} else {
					extra = append(extra, baseOverlayShow(W, p, "package x\n"))
				}
			}
			return baseOverlayRoutes(W, changed, "", extra...)
		}
	})
}

func overlayVetResult(t *testing.T, h *harness) (Request, string, []string) {
	t.Helper()
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	body, found := baseOverlayBody(req.Brief)
	if !found {
		t.Fatal("missing Base overlays section")
	}
	return req, body, baseOverlayEntries(body)
}

const overlayPassWant = "Passes in full with this file reverted: no test in the package fails for this change, so no test covers it."
const overlayTestOnlyWant = "Every changed Go file is a test file, so a base overlay cannot isolate the product change: the tests are part of the change. Read each file's base version with `git show` instead."

func overlayPassHarness(t *testing.T, changed string, isNew bool, vet clitest.Reply, tests ...clitest.Reply) *harness {
	t.Helper()
	return newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			extra := []clitest.Route{
				{Tool: "go", Args: []string{"-C", W, "vet"}, Prefix: true, Seq: []clitest.Reply{vet}},
				{Tool: "go", Args: []string{"-C", W, "test"}, Prefix: true, Seq: tests},
			}
			for _, p := range strings.Split(strings.TrimRight(changed, "\x00"), "\x00") {
				if isNew {
					extra = append(extra, baseOverlayNotInBase(W, p))
				} else {
					extra = append(extra, baseOverlayShow(W, p, "package x\n"))
				}
			}
			return baseOverlayRoutes(W, changed, "", extra...)
		}
	})
}

func TestBaseOverlayMarksPackagePassWithFileReverted(t *testing.T) {
	h := overlayPassHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", false, clitest.Reply{}, clitest.Reply{}, clitest.Reply{Exit: 1})
	req, _, entries := overlayVetResult(t, h)
	want := []string{
		"- `internal/x/a.go`: `" + baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x") + "`. " + overlayPassWant,
		"- `internal/x/b.go`: `" + baseOverlayCommand(h.W, req.TMPDir, 2, "internal/x") + "`",
	}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("entries = %q, want %q", entries, want)
	}
	var wantCalls []clitest.Call
	for i := 1; i <= 2; i++ {
		for _, verb := range []string{"vet", "test"} {
			args := []string{"-C", h.W, verb, "-overlay", filepath.Join(req.TMPDir, fmt.Sprintf("overlay-%d.json", i))}
			if verb == "test" {
				args = append(args, "-count=1")
			}
			wantCalls = append(wantCalls, clitest.Call{Tool: "go", Args: append(args, "./internal/x")})
		}
	}
	wantCalls = append(wantCalls, clitest.Call{Tool: "go", Args: []string{"-C", h.W, "list", "-e", "-json", "./..."}})
	if got := h.fake.CallsTo("go"); !reflect.DeepEqual(got, wantCalls) {
		t.Errorf("go calls = %v, want %v", got, wantCalls)
	}
}

func TestBaseOverlayNoPassNoteWhenVetFails(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(fmt.Sprint(fails), func(t *testing.T) {
			vet := clitest.Reply{}
			if fails {
				vet = clitest.Reply{Exit: 1, Stderr: "undefined helper"}
			}
			h := overlayPassHarness(t, "internal/x/a.go\x00", false, vet, clitest.Reply{})
			req, _, entries := overlayVetResult(t, h)
			want := "- `internal/x/a.go`: `" + baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x") + "`. " + overlayPassWant
			if fails {
				want = "- `internal/x/a.go`: does not build: undefined helper. Trace its base version with `git -C " + ShellWord(h.W) + " show " + ShellWord("base:internal/x/a.go") + "` instead."
			}
			if !reflect.DeepEqual(entries, []string{want}) {
				t.Errorf("entries = %q, want %q", entries, want)
			}
			wantCalls := 1
			if fails {
				wantCalls = 0
			}
			if calls := h.calls("go", "-C", h.W, "test"); len(calls) != wantCalls {
				t.Errorf("test calls = %v, vet fails = %v", calls, fails)
			}
		})
	}
}

func TestBaseOverlayPassNoteNeedsAnExitZeroTestRun(t *testing.T) {
	for _, mode := range []string{"pass", "fail", "missing", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			reply := clitest.Reply{}
			if mode == "fail" {
				reply.Exit = 1
			}
			h := overlayPassHarness(t, "internal/x/a.go\x00", false, clitest.Reply{}, reply)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if mode == "missing" {
				h.deps.GoBin = filepath.Join(h.W, "missing-go")
			}
			if mode == "timeout" {
				// Vet succeeds, then the test process runs until its parent deadline.
				bin := filepath.Join(t.TempDir(), "go")
				if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$3\" = vet ]; then exit 0; fi\nwhile :; do :; done\n"), 0700); err != nil {
					t.Fatal(err)
				}
				h.deps.GoBin = bin
			}
			e := &Engine{d: h.deps, proj: h.proj}
			section := e.baseOverlaySection(ctx, runrec.Record{Worktree: h.W}, t.TempDir(), branchChangeResult{MergeBase: "base", Paths: []string{"internal/x/a.go"}})
			entries := baseOverlayEntries(section.Body)
			if len(entries) != 1 {
				t.Fatalf("entries = %q", entries)
			}
			wantSuffix := "`"
			if mode == "pass" {
				wantSuffix += ". " + overlayPassWant
			}
			if !strings.Contains(entries[0], " test -overlay ") || !strings.HasSuffix(entries[0], wantSuffix) {
				t.Errorf("%s entry = %q, want command ending %q", mode, entries[0], wantSuffix)
			}
			if mode == "timeout" && ctx.Err() != context.DeadlineExceeded {
				t.Error("test run did not reach its deadline")
			}
		})
	}
}

func TestBaseOverlayPassNoteOnNewFile(t *testing.T) {
	h := overlayPassHarness(t, "internal/x/new.go\x00", true, clitest.Reply{}, clitest.Reply{})
	req, _, entries := overlayVetResult(t, h)
	want := "- `internal/x/new.go` (new in this slice; the overlay hides it): `" + baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x") + "`. " + overlayPassWant
	if !reflect.DeepEqual(entries, []string{want}) {
		t.Errorf("entries = %q, want %q", entries, want)
	}
}

func TestBaseOverlayTestOnlyChangeSaysOverlayCannotIsolate(t *testing.T) {
	h := overlayPassHarness(t, "internal/x/x_test.go\x00README.md\x00", false, clitest.Reply{})
	_, body, _ := overlayVetResult(t, h)
	if strings.Count(body, overlayTestOnlyWant) != 1 || !strings.Contains(body, overlayTestOnlyWant+"\n\nThe overlays cover non-test Go files only") {
		t.Errorf("missing single test-only paragraph before closing guidance: %s", body)
	}
	if calls := h.fake.CallsTo("go"); len(calls) != 0 {
		t.Errorf("unexpected Go calls: %v", calls)
	}
}

func TestBaseOverlayNoTestOnlyNoteWithProductFile(t *testing.T) {
	for _, changed := range []string{"internal/x/a_test.go\x00", "internal/x/a.go\x00internal/x/a_test.go\x00", "README.md\x00"} {
		t.Run(changed, func(t *testing.T) {
			h := overlayPassHarness(t, changed, false, clitest.Reply{}, clitest.Reply{Exit: 1})
			_, body, _ := overlayVetResult(t, h)
			want := changed == "internal/x/a_test.go\x00"
			if got := strings.Contains(body, overlayTestOnlyWant); got != want {
				t.Errorf("test-only note = %v, want %v: %s", got, want, body)
			}
		})
	}
}

func TestBaseOverlayVetsEachOverlayFirst(t *testing.T) {
	h := overlayVetHarness(t, "internal/x/b.go\x00internal/x/a.go\x00", false, clitest.Reply{}, clitest.Reply{})
	req, _, entries := overlayVetResult(t, h)
	calls := baseOverlayVetCalls(h)
	if len(calls) != 2 {
		t.Fatalf("go calls = %v, want exactly two vet calls", calls)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %q, want two", entries)
	}
	for i, p := range []string{"a.go", "b.go"} {
		want := []string{"-C", h.W, "vet", "-overlay", filepath.Join(req.TMPDir, fmt.Sprintf("overlay-%d.json", i+1)), "./internal/x"}
		if !reflect.DeepEqual(calls[i].Args, want) {
			t.Errorf("vet args = %q, want %q", calls[i], want)
		}
		if !strings.Contains(entries[i], "internal/x/"+p) || !strings.Contains(entries[i], baseOverlayCommand(h.W, req.TMPDir, i+1, "internal/x")) {
			t.Errorf("sorted command entry = %q", entries[i])
		}
	}
}

func TestBaseOverlayNotesOlderBinary(t *testing.T) {
	h := overlayVetHarness(t, "internal/x/a.go\x00", false, clitest.Reply{})
	h.deps.Behind = behindFunc(true)
	req, body, entries := overlayVetResult(t, h)
	if !strings.HasPrefix(body, olderBinaryNote+"\n\n") {
		t.Errorf("body does not start with the note and a blank line: %q", body)
	}
	if strings.Count(body, "older than this slice's code") != 1 {
		t.Errorf("want the note once: %q", body)
	}
	if want := "- `internal/x/a.go`: `" + baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x") + "`"; len(entries) != 1 || entries[0] != want {
		t.Errorf("entries = %q, want [%q]", entries, want)
	}
}

func TestBaseOverlayNoNoteWhenBinaryCurrent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		behind func(context.Context, registry.Project) bool
	}{{"nil", nil}, {"false", behindFunc(false)}} {
		t.Run(tc.name, func(t *testing.T) {
			h := overlayVetHarness(t, "internal/x/a.go\x00", false, clitest.Reply{})
			h.deps.Behind = tc.behind
			_, body, _ := overlayVetResult(t, h)
			if strings.Contains(body, "older than this slice's code") {
				t.Errorf("note present with the binary current: %q", body)
			}
			if !strings.HasPrefix(body, "Each command runs one package's tests") {
				t.Errorf("first line is not the intro: %q", body)
			}
		})
	}
}

func TestBaseOverlayNoteWhenNothingBuilds(t *testing.T) {
	h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", false, clitest.Reply{Exit: 1, Stderr: "undefined helper"})
	h.deps.Behind = behindFunc(true)
	_, body, entries := overlayVetResult(t, h)
	if !strings.HasPrefix(body, olderBinaryNote+"\n\n- `internal/x/a.go`: does not build:") {
		t.Errorf("body does not start with the note then the first entry: %q", body)
	}
	// Two single-file failures, then the combined overlay's entry (it follows
	// when no single-file overlay builds), as in the no-intro test.
	if len(entries) != 2 || strings.Count(body, "does not build: undefined helper") != 3 {
		t.Errorf("want three build failures: %s", body)
	}
}

func TestBaseOverlayNoNoteWithoutGoFiles(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "internal/x/x_test.go\x00README.md\x00", "",
				baseOverlayShow(W, "internal/x/x_test.go", "package x\n"), baseOverlayShow(W, "README.md", "readme"))
		}
	})
	h.deps.Behind = behindFunc(true)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	if body, found := baseOverlayBody(req.Brief); !found || strings.Contains(body, "older than this slice's code") {
		t.Errorf("want a section without the older-binary note: found %v, body %q", found, body)
	}
	if strings.Contains(req.Brief, "older than this slice's code") {
		t.Errorf("the note stands alone in the brief")
	}
}

func TestBaseOverlayBuildFailureReplacesCommand(t *testing.T) {
	const diagnostic = "internal/x/b.go:12:5: not enough arguments in call to engine.LocalReadOnlyAllowlist"
	h := overlayVetHarness(t, "internal/x/b.go\x00internal/x/a.go\x00", false, clitest.Reply{}, clitest.Reply{Exit: 1, Stderr: "# pkg\n" + diagnostic})
	req, body, entries := overlayVetResult(t, h)
	if len(entries) != 2 {
		t.Fatalf("entries = %q, want two", entries)
	}
	want := "- `internal/x/b.go`: does not build: # pkg; " + diagnostic + ". Trace its base version with `git -C " + ShellWord(h.W) + " show " + ShellWord("base:internal/x/b.go") + "` instead."
	if entries[1] != want {
		t.Errorf("failed entry = %q, want %q", entries[1], want)
	}
	if !strings.Contains(entries[0], baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x")) {
		t.Errorf("first entry lost command: %s", entries[0])
	}
	for _, want := range []string{"only that file reverted", "-run NAME", "one test per command"} {
		if !strings.Contains(body, want) {
			t.Errorf("intro lacks %q: %s", want, body)
		}
	}
	// Even a rejected overlay remains available for inspection.
	baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-2.json"))
}

func TestBaseOverlayNoIntroWhenNothingBuilds(t *testing.T) {
	h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", false, clitest.Reply{Exit: 1, Stderr: "undefined helper"})
	_, body, entries := overlayVetResult(t, h)
	if len(entries) != 2 || strings.Count(body, "does not build: undefined helper") != 3 {
		t.Errorf("want three build failures: %s", body)
	}
	for _, gone := range []string{"go -C", "-run NAME", "Each command runs"} {
		if strings.Contains(body, gone) {
			t.Errorf("failed-only section contains %q: %s", gone, body)
		}
	}
}

func TestBaseOverlayBuildErrorIsOneShortLine(t *testing.T) {
	var lines []string
	var wantLines []string
	for i := range 10 {
		line := fmt.Sprintf("line%d|", i) + strings.Repeat("x", 310)
		lines = append(lines, line)
		if i < 3 {
			wantLines = append(wantLines, strings.ReplaceAll(line, "|", "/")[:200])
		}
	}
	h := overlayVetHarness(t, "internal/x/a.go\x00", false, clitest.Reply{Exit: 1, Stderr: "\n" + strings.Join(lines, "\n\n")})
	_, body, entries := overlayVetResult(t, h)
	want := "- `internal/x/a.go`: does not build: " + strings.Join(wantLines, "; ") + ". Trace its base version with `git -C " + ShellWord(h.W) + " show " + ShellWord("base:internal/x/a.go") + "` instead."
	if len(entries) != 1 || strings.TrimSpace(body) != want {
		t.Errorf("short one-line section = %q, want %q", body, want)
	}
}

func TestBaseOverlayNewFileBuildFailureKeepsNote(t *testing.T) {
	h := overlayVetHarness(t, "internal/x/new.go\x00", true, clitest.Reply{Exit: 1, Stderr: "undefined helper"})
	_, _, entries := overlayVetResult(t, h)
	want := "- `internal/x/new.go` (new in this slice; the overlay hides it): does not build: undefined helper. Trace its base version with `git -C " + ShellWord(h.W) + " show " + ShellWord("base:internal/x/new.go") + "` instead."
	if len(entries) != 1 || entries[0] != want {
		t.Errorf("new-file failure = %q, want %q", entries, want)
	}
}

func baseOverlayAllEntry(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- All changed Go files at once") {
			return line
		}
	}
	return ""
}

func TestBaseOverlayAllTogetherWhenEverySingleFileFails(t *testing.T) {
	fail := clitest.Reply{Exit: 1, Stderr: "undefined helper"}
	h := overlayVetHarness(t, "internal/y/b.go\x00internal/x/a.go\x00", false, fail, fail, clitest.Reply{})
	req, body, entries := overlayVetResult(t, h)
	overlay := filepath.Join(req.TMPDir, "overlay-all.json")
	calls := baseOverlayVetCalls(h)
	if len(calls) != 3 {
		t.Fatalf("vet calls = %d, want three", len(calls))
	}
	wantArgs := []string{"-C", h.W, "vet", "-overlay", overlay, "./internal/x", "./internal/y"}
	if !reflect.DeepEqual(calls[2].Args, wantArgs) {
		t.Errorf("combined vet args = %q, want %q", calls[2].Args, wantArgs)
	}
	wantReplace := map[string]string{}
	for _, p := range []string{"internal/x/a.go", "internal/y/b.go"} {
		wantReplace[filepath.Join(h.W, p)] = filepath.Join(req.TMPDir, "base", p)
	}
	if got := baseOverlayReplace(t, overlay); !reflect.DeepEqual(got, wantReplace) {
		t.Errorf("combined Replace = %v, want %v", got, wantReplace)
	}
	if len(entries) != 2 {
		t.Fatalf("per-file entries = %q, want two", entries)
	}
	for _, entry := range entries {
		if !strings.Contains(entry, "does not build: undefined helper") || strings.Contains(entry, " test -overlay ") {
			t.Errorf("per-file failure = %s", entry)
		}
	}
	entry := baseOverlayAllEntry(body)
	command := "go -C " + ShellWord(h.W) + " test -overlay " + ShellWord(overlay) + " -count=1 ./internal/x ./internal/y"
	for _, want := range []string{"(2 files)", command, "no single-file overlay builds", "reverts every file listed above together", "-run NAME", "before the package list", "one test per command", "never join test names"} {
		if !strings.Contains(entry, want) {
			t.Errorf("combined entry lacks %q: %s", want, entry)
		}
	}
	if strings.Contains(body, "|") || strings.Contains(body, "only that file reverted") {
		t.Errorf("combined-only guidance contains pipe or single-file intro: %s", body)
	}
	if strings.TrimSpace(body) != strings.Join(append(entries, entry), "\n") {
		t.Errorf("combined entry is not last: %s", body)
	}
}

func TestBaseOverlayAllTogetherNotOfferedWhenOneFileBuilds(t *testing.T) {
	for _, builds := range []bool{false, true} {
		t.Run(fmt.Sprintf("builds=%v", builds), func(t *testing.T) {
			fail := clitest.Reply{Exit: 1, Stderr: "undefined helper"}
			second := fail
			if builds {
				second = clitest.Reply{}
			}
			h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", false, fail, second, clitest.Reply{})
			req, body, _ := overlayVetResult(t, h)
			wantCalls := 3
			if builds {
				wantCalls = 2
			}
			if got := len(baseOverlayVetCalls(h)); got != wantCalls {
				t.Errorf("vet calls = %d, want %d", got, wantCalls)
			}
			_, err := os.Stat(filepath.Join(req.TMPDir, "overlay-all.json"))
			if builds && !errors.Is(err, os.ErrNotExist) || !builds && err != nil {
				t.Errorf("combined overlay stat = %v, builds = %v", err, builds)
			}
			if got := baseOverlayAllEntry(body) != ""; got == builds {
				t.Errorf("combined entry present = %v, builds = %v", got, builds)
			}
		})
	}
}

func TestBaseOverlayAllTogetherNeedsTwoFiles(t *testing.T) {
	for _, two := range []bool{true, false} {
		t.Run(fmt.Sprintf("two=%v", two), func(t *testing.T) {
			changed := "internal/x/a.go\x00"
			wantCalls := 1
			if two {
				changed += "internal/x/b.go\x00"
				wantCalls = 3
			}
			h := overlayVetHarness(t, changed, false, clitest.Reply{Exit: 1, Stderr: "undefined helper"})
			req, body, _ := overlayVetResult(t, h)
			if got := len(baseOverlayVetCalls(h)); got != wantCalls {
				t.Errorf("vet calls = %d, want %d", got, wantCalls)
			}
			_, err := os.Stat(filepath.Join(req.TMPDir, "overlay-all.json"))
			if !two && !errors.Is(err, os.ErrNotExist) || two && err != nil {
				t.Errorf("combined overlay stat = %v, two = %v", err, two)
			}
			if got := baseOverlayAllEntry(body) != ""; got != two {
				t.Errorf("combined entry present = %v, two = %v", got, two)
			}
		})
	}
}

func TestBaseOverlayAllTogetherThatDoesNotBuildIsReported(t *testing.T) {
	diagnostic := strings.Repeat("x", 210) + "|tail\nsecond|line\nthird\nfourth"
	h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", false, clitest.Reply{Exit: 1, Stderr: diagnostic})
	req, body, _ := overlayVetResult(t, h)
	want := "- All changed Go files at once (2 files): does not build: " + strings.Repeat("x", 200) + "; second/line; third. Trace the base versions with `git show` instead."
	if got := baseOverlayAllEntry(body); got != want {
		t.Errorf("combined failure = %q, want %q", got, want)
	}
	baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-all.json"))
}

func TestBaseOverlayAllTogetherMapsNewFileToEmpty(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			fail := clitest.Reply{Exit: 1, Stderr: "undefined helper"}
			return baseOverlayRoutes(W, "internal/x/a.go\x00internal/x/new.go\x00", "",
				clitest.Route{Tool: "go", Args: []string{"-C", W, "vet"}, Prefix: true, Seq: []clitest.Reply{fail, fail, {}}},
				baseOverlayShow(W, "internal/x/a.go", "package x\n"), baseOverlayNotInBase(W, "internal/x/new.go"))
		}
	})
	req, body, _ := overlayVetResult(t, h)
	want := map[string]string{filepath.Join(h.W, "internal/x/a.go"): filepath.Join(req.TMPDir, "base/internal/x/a.go"), filepath.Join(h.W, "internal/x/new.go"): ""}
	if got := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-all.json")); !reflect.DeepEqual(got, want) {
		t.Errorf("mixed Replace = %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(req.TMPDir, "base/internal/x/new.go")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("new base copy stat = %v, want absent", err)
	}
	entry := baseOverlayAllEntry(body)
	wantCommand := "go -C " + ShellWord(h.W) + " test -overlay " + ShellWord(filepath.Join(req.TMPDir, "overlay-all.json")) + " -count=1 ./internal/x"
	if !strings.Contains(entry, "`"+wantCommand+"`") {
		t.Errorf("mixed entry lacks deduplicated command: %s", entry)
	}
}

func TestBaseOverlayVetCannotRunKeepsCommand(t *testing.T) {
	// The control proves vet is wired before checking that an unavailable tool
	// is advisory. Only the GoBin path differs between the two cases.
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("unavailable=%v", unavailable), func(t *testing.T) {
			h := overlayVetHarness(t, "internal/x/a.go\x00", false, clitest.Reply{Exit: 1, Stderr: "undefined helper"})
			if unavailable {
				h.deps.GoBin = filepath.Join(h.W, "missing-go")
			}
			req, body, _ := overlayVetResult(t, h)
			if got := strings.Contains(body, baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x")); got != unavailable {
				t.Errorf("test command present = %v, want %v: %s", got, unavailable, body)
			}
			if got := strings.Contains(body, "does not build:"); got == unavailable {
				t.Errorf("build diagnostic present = %v, unavailable = %v: %s", got, unavailable, body)
			}
		})
	}
}

func TestBaseOverlaySectionForChangedGoFile(t *testing.T) {
	const source = "package x\n\nfunc Old() int { return 1 }\n"
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "internal/x/x.go\x00", "",
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

func TestBaseOverlayNamesGitShowForNonGoFiles(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "internal/x/x.go\x00internal/x/x_test.go\x00internal/roles/prompts/reviewer.md\x00README.md\x00", "",
				baseOverlayShow(W, "internal/x/x.go", "package x\n"),
				baseOverlayShow(W, "internal/x/x_test.go", "package x\n"),
				baseOverlayShow(W, "internal/roles/prompts/reviewer.md", "review"), baseOverlayShow(W, "README.md", "readme"))
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	body, found := baseOverlayBody(baseOverlayReviewer(t, h).Brief)
	if !found {
		t.Fatal("reviewer brief lacks the Base overlays section")
	}
	if entries := baseOverlayEntries(body); len(entries) != 1 || !strings.Contains(entries[0], "internal/x/x.go") {
		t.Errorf("entries = %q, want one for internal/x/x.go", entries)
	}
	requireOtherFileLines(t, body, h.W, []string{"README.md", "internal/roles/prompts/reviewer.md", "internal/x/x_test.go"})
	for _, want := range []string{"not overlaid", "`git grep` is not available"} {
		if !strings.Contains(body, want) {
			t.Errorf("section lacks %q:\n%s", want, body)
		}
	}
}

func TestBaseOverlayNoNonGoNoteForGoOnlyDiff(t *testing.T) {
	h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", false, clitest.Reply{}, clitest.Reply{})
	_, body, entries := overlayVetResult(t, h)
	if len(entries) != 2 {
		t.Fatalf("entries = %q, want two", entries)
	}
	for _, gone := range []string{"git grep", "base:PATH", "not overlaid"} {
		if strings.Contains(body, gone) {
			t.Errorf("Go-only section contains %q:\n%s", gone, body)
		}
	}
}

func TestBaseOverlayUnreadableDiffNamesEveryChangedFile(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			routes := baseOverlayRoutes(W, "internal/x/x.go\x00", "")
			routes[1].Exit = 1
			return routes
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	body, found := baseOverlayBody(baseOverlayReviewer(t, h).Brief)
	if !found {
		t.Fatal("reviewer brief lacks the Base overlays section")
	}
	for _, want := range []string{"incomplete", "git show", "each changed file"} {
		if !strings.Contains(body, want) {
			t.Errorf("section lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "changed Go file") {
		t.Errorf("section still limits the notice to Go files:\n%s", body)
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

func TestBaseOverlayPromptOnlyDiffGetsHint(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "internal/x/x_test.go\x00README.md\x00docs/a.go.md\x00", "",
				baseOverlayShow(W, "internal/x/x_test.go", "package x\n"),
				baseOverlayShow(W, "README.md", "readme"), baseOverlayShow(W, "docs/a.go.md", "doc"))
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	body, found := baseOverlayBody(req.Brief)
	if !found {
		t.Error("missing Base overlays section")
	}
	requireOtherFileLines(t, body, h.W, []string{"README.md", "docs/a.go.md", "internal/x/x_test.go"})
	if !strings.Contains(body, "not overlaid") || strings.Contains(body, "Each command runs") {
		t.Errorf("wrong prompt-only guidance: %q", body)
	}
	var paths []string
	for _, call := range h.calls("git", "-C", h.W, "show") {
		paths = append(paths, call[3])
	}
	if !reflect.DeepEqual(paths, []string{"base:README.md", "base:docs/a.go.md", "base:internal/x/x_test.go"}) {
		t.Errorf("git show paths = %q", paths)
	}
	if calls := h.fake.CallsTo("go"); len(calls) != 0 {
		t.Errorf("unexpected Go calls: %v", calls)
	}
	if _, err := os.Stat(filepath.Join(req.TMPDir, "base")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("base directory: %v, want it absent", err)
	}
	if overlays, _ := filepath.Glob(filepath.Join(req.TMPDir, "overlay-*.json")); len(overlays) != 0 {
		t.Errorf("overlay files written: %v", overlays)
	}
}

func otherFileLines(body string) []string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- Other file") {
			lines = append(lines, line)
		}
	}
	return lines
}

func requireOtherFileLines(t *testing.T, body, W string, paths []string) {
	t.Helper()
	var want []string
	for _, p := range paths {
		want = append(want, "- Other file `"+p+"`: `git -C "+ShellWord(W)+" show "+ShellWord("base:"+p)+"`")
	}
	if got := otherFileLines(body); !reflect.DeepEqual(got, want) {
		t.Errorf("other-file lines = %q, want %q", got, want)
	}
}

func TestBaseOverlayOtherFileNewInSlice(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "", "docs/new.md\x00", baseOverlayNotInBase(W, "docs/new.md"))
		}
	})
	_, body, _ := overlayVetResult(t, h)
	if got := otherFileLines(body); !reflect.DeepEqual(got, []string{"- Other file `docs/new.md`: new in this slice, no base version"}) {
		t.Errorf("new-file line = %q", got)
	}
	if strings.Contains(body, "base:docs/new.md") || strings.Contains(body, "incomplete") {
		t.Errorf("new file has a command or incomplete notice: %q", body)
	}
}

func TestBaseOverlayOtherFileListLimit(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			changed := "internal/x/x.go\x00"
			extra := []clitest.Route{baseOverlayShow(W, "internal/x/x.go", "package x\n")}
			for i := 25; i >= 1; i-- {
				p := fmt.Sprintf("docs/f%02d.md", i)
				changed += p + "\x00"
				extra = append(extra, baseOverlayShow(W, p, "doc"))
			}
			return baseOverlayRoutes(W, changed, "", extra...)
		}
	})
	_, body, entries := overlayVetResult(t, h)
	var paths []string
	for i := 1; i <= 20; i++ {
		paths = append(paths, fmt.Sprintf("docs/f%02d.md", i))
	}
	requireOtherFileLines(t, body, h.W, paths)
	if !strings.Contains(body, "5 more changed other files were left out") || strings.Contains(body, "f21.md") {
		t.Errorf("wrong limit notice: %q", body)
	}
	if len(entries) != 1 || !strings.Contains(entries[0], "internal/x/x.go") {
		t.Errorf("Go entries changed: %q", entries)
	}
}

func TestBaseOverlayOtherFileUnreadable(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			route := baseOverlayShow(W, "README.md", "")
			route.Exit, route.Stderr = 128, "fatal: bad object base"
			return baseOverlayRoutes(W, "README.md\x00", "", route)
		}
	})
	_, body, _ := overlayVetResult(t, h)
	if !strings.Contains(body, "Check incomplete: the base version of `README.md` could not be read. Trace it yourself with `git show`") {
		t.Errorf("missing unreadable notice: %q", body)
	}
	if lines := otherFileLines(body); len(lines) != 0 {
		t.Errorf("unreadable file has command lines: %q", lines)
	}
}

func TestBaseOverlayRejectsEscapingOtherFiles(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "../evil.md\x00/abs/evil.md\x00", "")
		}
	})
	_, body, _ := overlayVetResult(t, h)
	if !strings.Contains(body, "incomplete") {
		t.Errorf("missing incomplete notice: %q", body)
	}
	if lines := otherFileLines(body); len(lines) != 0 {
		t.Errorf("rejected paths listed: %q", lines)
	}
	if calls := h.calls("git", "-C", h.W, "show"); len(calls) != 0 {
		t.Errorf("rejected paths read: %v", calls)
	}
}

func TestBaseOverlayNoSectionWithoutChangedFiles(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route { return baseOverlayRoutes(W, "", "") }
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	if body, found := baseOverlayBody(baseOverlayReviewer(t, h).Brief); found {
		t.Errorf("section for empty diff: %q", body)
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

// namedVetHarness is overlayVetHarness with vet replies that may name the
// worktree, which the harness picks.
func namedVetHarness(t *testing.T, changed string, isNew func(p string) bool, replies func(W string) []clitest.Reply) *harness {
	t.Helper()
	return newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			extra := []clitest.Route{{Tool: "go", Args: []string{"-C", W, "vet"}, Prefix: true, Seq: replies(W)}}
			for _, p := range strings.Split(strings.TrimRight(changed, "\x00"), "\x00") {
				if isNew(p) {
					extra = append(extra, baseOverlayNotInBase(W, p))
				} else {
					extra = append(extra, baseOverlayShow(W, p, "package x\n"))
				}
			}
			return baseOverlayRoutes(W, changed, "", extra...)
		}
	})
}

func namedVetReplies(replies ...clitest.Reply) func(string) []clitest.Reply {
	return func(string) []clitest.Reply { return replies }
}

func baseOverlayWantReplace(h *harness, tmp string, files ...string) map[string]string {
	want := map[string]string{}
	for _, p := range files {
		want[filepath.Join(h.W, p)] = filepath.Join(tmp, "base", p)
	}
	return want
}

func TestBaseOverlayRevertsANamedChangedFileToo(t *testing.T) {
	fail := clitest.Reply{Exit: 1, Stderr: "# pkg\ninternal/x/b.go:20:12: w.quoted undefined"}
	h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", false, fail, clitest.Reply{}, clitest.Reply{})
	req, body, entries := overlayVetResult(t, h)
	want := []string{
		"- `internal/x/a.go` (with `internal/x/b.go` reverted too): `" + baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x") + "`",
		"- `internal/x/b.go`: `" + baseOverlayCommand(h.W, req.TMPDir, 2, "internal/x") + "`",
	}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("entries = %q, want %q", entries, want)
	}
	if got, want := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-1.json")), baseOverlayWantReplace(h, req.TMPDir, "internal/x/a.go", "internal/x/b.go"); !reflect.DeepEqual(got, want) {
		t.Errorf("overlay-1 = %v, want %v", got, want)
	}
	if got, want := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-2.json")), baseOverlayWantReplace(h, req.TMPDir, "internal/x/b.go"); !reflect.DeepEqual(got, want) {
		t.Errorf("overlay-2 = %v, want %v", got, want)
	}
	calls := baseOverlayVetCalls(h)
	if len(calls) != 3 {
		t.Fatalf("vet calls = %v, want three", calls)
	}
	for i, n := range []int{1, 1, 2} {
		wantArgs := []string{"-C", h.W, "vet", "-overlay", filepath.Join(req.TMPDir, fmt.Sprintf("overlay-%d.json", n)), "./internal/x"}
		if !reflect.DeepEqual(calls[i].Args, wantArgs) {
			t.Errorf("vet call %d args = %q, want %q", i, calls[i].Args, wantArgs)
		}
	}
	if baseOverlayAllEntry(body) != "" {
		t.Errorf("unexpected all-files entry: %s", body)
	}
}

func TestBaseOverlayNamedFileClosureRepeatsUntilItBuilds(t *testing.T) {
	h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00internal/x/c.go\x00", false,
		clitest.Reply{Exit: 1, Stderr: "internal/x/b.go:1:1: undefined"},
		clitest.Reply{Exit: 1, Stderr: "internal/x/c.go:1:1: undefined"},
		clitest.Reply{}, clitest.Reply{}, clitest.Reply{})
	req, _, entries := overlayVetResult(t, h)
	if len(entries) != 3 {
		t.Fatalf("entries = %q, want three", entries)
	}
	want := "- `internal/x/a.go` (with `internal/x/b.go`, `internal/x/c.go` reverted too): `" + baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x") + "`"
	if entries[0] != want {
		t.Errorf("entry 1 = %q, want %q", entries[0], want)
	}
	if got, want := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-1.json")), baseOverlayWantReplace(h, req.TMPDir, "internal/x/a.go", "internal/x/b.go", "internal/x/c.go"); !reflect.DeepEqual(got, want) {
		t.Errorf("overlay-1 = %v, want %v", got, want)
	}
	if calls := baseOverlayVetCalls(h); len(calls) != 5 {
		t.Errorf("vet calls = %d, want five", len(calls))
	}
}

func TestBaseOverlayNamedFileClosureAcceptsPackageRelativeAndAbsoluteNames(t *testing.T) {
	for _, tc := range []struct{ name, line string }{
		{"bare", "b.go:20:12: w.quoted undefined"},
		{"dot", "./b.go:20:12: w.quoted undefined"},
		{"dot-full", "./internal/x/b.go:20:12: w.quoted undefined"},
		{"indented", "vet: internal/x/b.go:20:12: w.quoted undefined"},
		{"absolute", "WORKTREE/internal/x/b.go:20:12: w.quoted undefined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := namedVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", func(string) bool { return false }, func(W string) []clitest.Reply {
				line := strings.ReplaceAll(tc.line, "WORKTREE", W)
				return []clitest.Reply{{Exit: 1, Stderr: "# pkg\ninternal/x/zzz.go:1:1: control\n" + line}, {}, {}}
			})
			req, _, entries := overlayVetResult(t, h)
			if len(entries) != 2 {
				t.Fatalf("entries = %q, want two", entries)
			}
			want := "- `internal/x/a.go` (with `internal/x/b.go` reverted too): `" + baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x") + "`"
			if entries[0] != want {
				t.Errorf("entry 1 = %q, want %q", entries[0], want)
			}
			if got, want := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-1.json")), baseOverlayWantReplace(h, req.TMPDir, "internal/x/a.go", "internal/x/b.go"); !reflect.DeepEqual(got, want) {
				t.Errorf("overlay-1 = %v, want %v", got, want)
			}
		})
	}
}

func TestBaseOverlayNamedFileClosureStopsWhenNoNewFileIsNamed(t *testing.T) {
	output := "# pkg\ninternal/x/a.go:1:1: self\ninternal/x/other.go:2:2: unchanged\ninternal/x/a_test.go:3:3: test"
	h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/a_test.go\x00", false, clitest.Reply{Exit: 1, Stderr: output})
	req, _, entries := overlayVetResult(t, h)
	if len(entries) != 1 {
		t.Fatalf("entries = %q, want one", entries)
	}
	want := "- `internal/x/a.go`: does not build: # pkg; internal/x/a.go:1:1: self; internal/x/other.go:2:2: unchanged. Trace its base version with `git -C " + ShellWord(h.W) + " show " + ShellWord("base:internal/x/a.go") + "` instead."
	if entries[0] != want {
		t.Errorf("entry = %q, want %q", entries[0], want)
	}
	if calls := baseOverlayVetCalls(h); len(calls) != 1 {
		t.Errorf("vet calls = %v, want one", calls)
	}
	if got, want := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-1.json")), baseOverlayWantReplace(h, req.TMPDir, "internal/x/a.go"); !reflect.DeepEqual(got, want) {
		t.Errorf("overlay-1 = %v, want %v", got, want)
	}
}

func TestBaseOverlayNamedFileClosureIgnoresASameNamedFileInAnotherDirectory(t *testing.T) {
	h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", false, clitest.Reply{Exit: 1, Stderr: "internal/y/b.go:1:1: other package\nfoo/b.go:2:2: other"})
	req, _, entries := overlayVetResult(t, h)
	if len(entries) != 2 || strings.Contains(entries[0], "reverted too") {
		t.Errorf("entries = %q, want a.go without a reverted-too note", entries)
	}
	if got, want := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-1.json")), baseOverlayWantReplace(h, req.TMPDir, "internal/x/a.go"); !reflect.DeepEqual(got, want) {
		t.Errorf("overlay-1 = %v, want %v", got, want)
	}
}

func TestBaseOverlayNamedFileClosureThatStillFailsReportsTheFinalError(t *testing.T) {
	h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", false,
		clitest.Reply{Exit: 1, Stderr: "internal/x/b.go:20:12: w.quoted undefined"},
		clitest.Reply{Exit: 1, Stderr: "undefined helper"})
	_, body, entries := overlayVetResult(t, h)
	want := "- `internal/x/a.go` (with `internal/x/b.go` reverted too): does not build: undefined helper. Trace its base version with `git -C " + ShellWord(h.W) + " show " + ShellWord("base:internal/x/a.go") + "` instead."
	if len(entries) != 2 || entries[0] != want {
		t.Errorf("entries = %q, want first %q", entries, want)
	}
	if strings.Contains(entries[0], "go -C "+ShellWord(h.W)+" test") {
		t.Errorf("a failing entry carries a test command: %s", entries[0])
	}
	if baseOverlayAllEntry(body) == "" {
		t.Errorf("the all-files fallback is missing: %s", body)
	}
	if calls := h.calls("go", "-C", h.W, "test"); len(calls) != 0 {
		t.Errorf("test calls = %v, want none", calls)
	}
}

func TestBaseOverlayNamedFileClosureReadsTheWholeVetOutput(t *testing.T) {
	output := "# pkg\nx.go:1:1: one\nx.go:2:2: two\nx.go:3:3: three\ninternal/x/b.go:20:12: w.quoted undefined"
	h := overlayVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", false,
		clitest.Reply{Exit: 1, Stderr: output}, clitest.Reply{Exit: 1, Stderr: output})
	req, _, entries := overlayVetResult(t, h)
	// The first three lines are all the entry shows; b.go is only in the fifth.
	want := "- `internal/x/a.go` (with `internal/x/b.go` reverted too): does not build: # pkg; x.go:1:1: one; x.go:2:2: two. Trace its base version with `git -C " + ShellWord(h.W) + " show " + ShellWord("base:internal/x/a.go") + "` instead."
	if len(entries) < 1 || entries[0] != want {
		t.Errorf("entries = %q, want first %q", entries, want)
	}
	if got, want := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-1.json")), baseOverlayWantReplace(h, req.TMPDir, "internal/x/a.go", "internal/x/b.go"); !reflect.DeepEqual(got, want) {
		t.Errorf("overlay-1 = %v, want %v", got, want)
	}
}

func TestBaseOverlayNamedFileClosureKeepsNewFileNote(t *testing.T) {
	// b.go has a base version here; a.go does not.
	h := namedVetHarness(t, "internal/x/a.go\x00internal/x/b.go\x00", func(p string) bool { return p == "internal/x/a.go" }, namedVetReplies(
		clitest.Reply{Exit: 1, Stderr: "internal/x/b.go:20:12: w.quoted undefined"}, clitest.Reply{}, clitest.Reply{}))
	req, _, entries := overlayVetResult(t, h)
	want := "- `internal/x/a.go` (new in this slice; the overlay hides it) (with `internal/x/b.go` reverted too): `" + baseOverlayCommand(h.W, req.TMPDir, 1, "internal/x") + "`"
	if len(entries) < 1 || entries[0] != want {
		t.Errorf("entries = %q, want first %q", entries, want)
	}
	wantReplace := map[string]string{
		filepath.Join(h.W, "internal/x/a.go"): "",
		filepath.Join(h.W, "internal/x/b.go"): filepath.Join(req.TMPDir, "base", "internal/x/b.go"),
	}
	if got := baseOverlayReplace(t, filepath.Join(req.TMPDir, "overlay-1.json")); !reflect.DeepEqual(got, wantReplace) {
		t.Errorf("overlay-1 = %v, want %v", got, wantReplace)
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
