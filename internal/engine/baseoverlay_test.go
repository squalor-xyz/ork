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
	return fmt.Sprintf("go -C %s test -overlay %s ./%s", ShellWord(W), ShellWord(filepath.Join(tmp, fmt.Sprintf("overlay-%d.json", n))), dir)
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

func TestBaseOverlayVetsEachOverlayFirst(t *testing.T) {
	h := overlayVetHarness(t, "internal/x/b.go\x00internal/x/a.go\x00", false, clitest.Reply{}, clitest.Reply{})
	req, _, entries := overlayVetResult(t, h)
	calls := h.fake.CallsTo("go")
	if len(calls) != 2 {
		t.Fatalf("go calls = %v, want exactly two vet calls", calls)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %q, want two", entries)
	}
	for i, p := range []string{"a.go", "b.go"} {
		want := []string{"-C", h.W, "vet", "-overlay", filepath.Join(req.TMPDir, fmt.Sprintf("overlay-%d.json", i+1)), "./internal/x"}
		if !reflect.DeepEqual(calls[i].Args, want) {
			t.Errorf("vet args = %q, want %q", calls[i].Args, want)
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
			return baseOverlayRoutes(W, "internal/x/x_test.go\x00README.md\x00", "")
		}
	})
	h.deps.Behind = behindFunc(true)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := baseOverlayReviewer(t, h)
	if body, found := baseOverlayBody(req.Brief); found {
		t.Errorf("reviewer brief holds a Base overlays section:\n%s", body)
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
	calls := h.fake.CallsTo("go")
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
	command := "go -C " + ShellWord(h.W) + " test -overlay " + ShellWord(overlay) + " ./internal/x ./internal/y"
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
			if got := len(h.fake.CallsTo("go")); got != wantCalls {
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
			if got := len(h.fake.CallsTo("go")); got != wantCalls {
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
	wantCommand := "go -C " + ShellWord(h.W) + " test -overlay " + ShellWord(filepath.Join(req.TMPDir, "overlay-all.json")) + " ./internal/x"
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

func TestBaseOverlayNamesGitShowForNonGoFiles(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return baseOverlayRoutes(W, "internal/x/x.go\x00internal/x/x_test.go\x00internal/roles/prompts/reviewer.md\x00", "",
				baseOverlayShow(W, "internal/x/x.go", "package x\n"))
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
	command := "git -C " + ShellWord(h.W) + " show " + ShellWord("base") + ":PATH"
	for _, want := range []string{command, "not overlaid", "`git grep` is not available"} {
		if !strings.Contains(body, want) {
			t.Errorf("section lacks %q:\n%s", want, body)
		}
	}
	for _, gone := range []string{"reviewer.md", "x_test.go"} {
		if strings.Contains(body, gone) {
			t.Errorf("section names %s:\n%s", gone, body)
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
