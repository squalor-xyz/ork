// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// splitFiles are the paths of the Files section of the split tests' slice.
var splitFiles = []string{"internal/a/a.go", "internal/a/a_test.go", "internal/b/b.go", "internal/c/c.go", "internal/d/d.go"}

// splitSlice answers `slicer show` with a slice of the given size whose Files
// section names splitFiles.
func splitSlice(size string, paths ...string) func(*harnessOpts) {
	if len(paths) == 0 {
		paths = splitFiles
	}
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = strconv.Quote(p)
	}
	return func(o *harnessOpts) {
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"show", item}, Prefix: true, Stdout: `{"id":"S04","title":"Engine","status":"started","paths":[` + strings.Join(quoted, ",") +
			`],"slice":{"id":"S04","title":"Engine","size":"` + size + `","sections":[{"heading":"Why","body":"Because."},{"heading":"Git","body":"One commit: \"` + gitTitle + `\"."}]}}`})
	}
}

// leadReport is the lead's first-round report: the implementer's shape and the
// Parts section.
func leadReport(parts ...string) string {
	body := "none"
	if len(parts) > 0 {
		body = "- " + strings.Join(parts, "\n- ")
	}
	return "Diff stat.\n\n## Parts\n\n" + body + "\n\nTests: 12 passed, 0 failed\n\n## Owner needed\n\nnone\n"
}

// splitRun records when each part started and ended, to tell parts that ran at
// the same time from parts that ran one after another.
type splitRun struct {
	mu            sync.Mutex
	active, most  int
	events        []string
	hold          time.Duration
	partReport    func(id string) string
	leadReport    string
	fixReport     string // the lead's report in a fix round; empty means the default report
	partStartedAt map[string]time.Time
}

func (s *splitRun) enter(id string) {
	s.mu.Lock()
	s.active++
	s.most = max(s.most, s.active)
	s.events = append(s.events, "start "+id)
	s.mu.Unlock()
	time.Sleep(s.hold)
	s.mu.Lock()
	s.active--
	s.events = append(s.events, "end "+id)
	s.mu.Unlock()
}

func (s *splitRun) index(event string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Index(s.events, event)
}

// splitHarness is a harness whose lead returns the given parts and whose parts
// report a clean round. The lineup has the part chain beside the lead's.
func splitHarness(t *testing.T, size string, parts []string, opts ...func(*harnessOpts)) (*harness, *splitRun) {
	return splitHarnessStatus(t, size, parts, nil, opts...)
}

// splitHarnessStatus is splitHarness with the replies of `git status
// --porcelain=v1 -z` of the worktree, which ork reads before and after each
// batch of parts to see what the batch changed. With none, the tree stays clean.
func splitHarnessStatus(t *testing.T, size string, parts []string, status []string, opts ...func(*harnessOpts)) (*harness, *splitRun) {
	t.Helper()
	if len(status) == 0 {
		status = []string{""}
	}
	opts = append(opts, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			var seq []clitest.Reply
			for _, r := range status {
				seq = append(seq, clitest.Reply{Stdout: r})
			}
			return []clitest.Route{{Tool: "git", Args: []string{"-C", W, "status", "--porcelain=v1", "-z", "--untracked-files=all"}, Seq: seq}}
		}
	}, splitSlice(size))
	h := newHarness(t, opts...)
	h.cfg.Split = true
	h.cfg.Lineup[RolePart] = []runrec.LineupEntry{{Vendor: "claude", Model: "haiku", Effort: "medium"}}
	run := &splitRun{hold: 150 * time.Millisecond, leadReport: leadReport(parts...), partReport: func(string) string { return implReport }}
	h.runner.script = func(req Request, _ int) (Result, error) {
		switch {
		case req.Role == RoleImplementer && strings.Contains(req.Brief, "This is the first round"):
			return Result{Exit: ExitOK, Session: "sess-lead", Report: run.leadReport}, nil
		case req.Role == RoleImplementer && strings.Contains(req.Brief, "This is fix round") && run.fixReport != "":
			return Result{Exit: ExitOK, Session: "sess-lead", Report: run.fixReport}, nil
		case req.Role == RolePart:
			run.enter(req.Part)
			return Result{Exit: ExitOK, Session: "sess-part-" + req.Part, Report: run.partReport(req.Part)}, nil
		}
		return Result{}, nil
	}
	return h, run
}

func requestsOf(h *harness, role string) []Request {
	var out []Request
	for _, r := range h.runner.requests() {
		if r.Role == role {
			out = append(out, r)
		}
	}
	return out
}

const (
	partOne   = "P1: files internal/a/a.go, internal/a/a_test.go; tests TestA"
	partTwo   = "P2: files internal/b/b.go; tests TestB"
	partThree = "P3: files internal/c/c.go; tests TestC"
	partAlsoA = "P3: files internal/a/a.go; tests TestAgain"
)

func TestSplitPartsShareTheSliceWorktree(t *testing.T) {
	h, _ := splitHarness(t, "M", []string{partOne, partTwo})
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if !rec.Split || len(rec.Parts) != 2 || rec.Parts[0].ID != "P1" || rec.Parts[1].ID != "P2" || !rec.Parts[0].Done || !rec.Parts[1].Done {
		t.Fatalf("the run record keeps no done parts: split %v, parts %+v", rec.Split, rec.Parts)
	}
	lead := requestsOf(h, RoleImplementer)
	parts := requestsOf(h, RolePart)
	if len(lead) == 0 || len(parts) != 2 {
		t.Fatalf("roles %v, want the lead and two parts", h.runner.roles())
	}
	for _, r := range append(append([]Request(nil), lead...), parts...) {
		if r.Worktree != h.W {
			t.Errorf("%s %s runs in %q, want the slice worktree %q", r.Role, r.Part, r.Worktree, h.W)
		}
	}
	if n := len(h.calls("git", "-C", h.P, "worktree", "add")); n != 1 {
		t.Errorf("%d worktrees were added, want the one slice worktree", n)
	}
	if !strings.Contains(lead[0].Brief, "Role: lead.") {
		t.Errorf("the lead's brief is not the lead prompt:\n%.200s", lead[0].Brief)
	}
	ids := []string{parts[0].Part, parts[1].Part}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"P1", "P2"}) {
		t.Fatalf("part ids %v", ids)
	}
	for _, p := range parts {
		for _, want := range []string{"Role: part.", "## Your part", "Part: " + p.Part, "must not touch"} {
			if !strings.Contains(p.Brief, want) {
				t.Errorf("part %s brief lacks %q", p.Part, want)
			}
		}
	}
	// Each part is a ledger step under the lead's step.
	var leadStep string
	var partParents []string
	for i, s := range h.steps() {
		switch {
		case s.Task == item+" lead" && s.Role == "implementer":
			leadStep = strconv.Itoa(i + 1)
		case strings.HasPrefix(s.Task, item+" part "):
			if s.Role != "implementer" {
				t.Errorf("part step %q has ledger role %q, want implementer", s.Task, s.Role)
			}
			partParents = append(partParents, s.Parent)
		}
	}
	if leadStep == "" || len(partParents) != 2 || partParents[0] != leadStep || partParents[1] != leadStep {
		t.Fatalf("part steps have parents %v, want the lead's step %q", partParents, leadStep)
	}
}

func TestPartsRunInParallelOnlyWhenFilesAreDisjoint(t *testing.T) {
	t.Run("disjoint parts run at the same time", func(t *testing.T) {
		h, run := splitHarness(t, "M", []string{partOne, partTwo, partThree})
		requireState(t, h.start(), runrec.StateAwaitingApproval)
		if run.most != 3 {
			t.Fatalf("at most %d parts ran at once, want 3 (events %v)", run.most, run.events)
		}
	})
	t.Run("at most three at once", func(t *testing.T) {
		four := "P4: files internal/d/d.go; tests TestD"
		h, run := splitHarness(t, "M", []string{partOne, partTwo, partThree, four})
		requireState(t, h.start(), runrec.StateAwaitingApproval)
		if run.most != 3 {
			t.Fatalf("at most %d parts ran at once, want 3 (events %v)", run.most, run.events)
		}
		if run.index("start P4") < min(run.index("end P1"), run.index("end P2"), run.index("end P3")) {
			t.Fatalf("the fourth part started before a batch of three ended: %v", run.events)
		}
	})
	t.Run("overlapping parts run one after another", func(t *testing.T) {
		// P3 changes internal/a/a.go like P1: P3 waits for P1; P2 is disjoint from both.
		h, run := splitHarness(t, "M", []string{partOne, partTwo, partAlsoA})
		requireState(t, h.start(), runrec.StateAwaitingApproval)
		if run.most != 2 {
			t.Fatalf("at most %d parts ran at once, want 2 (P1 with P2, P3 after P1): %v", run.most, run.events)
		}
		if run.index("start P3") < run.index("end P1") {
			t.Fatalf("P3 overlaps P1 and started before it ended: %v", run.events)
		}
	})
	t.Run("a directory overlaps the files under it", func(t *testing.T) {
		h, run := splitHarness(t, "M", []string{"P1: files internal/a; tests TestA", partTwo, "P3: files internal/a/a_test.go; tests TestB"}, splitSlice("M", "internal/"))
		requireState(t, h.start(), runrec.StateAwaitingApproval)
		if run.index("start P3") < run.index("end P1") {
			t.Fatalf("P3 lies under P1's directory and started before it ended: %v", run.events)
		}
	})
}

func TestPartRoundRunsNoSlicerWriteCommand(t *testing.T) {
	h, _ := splitHarness(t, "M", []string{partOne, partTwo})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	parts := requestsOf(h, RolePart)
	if len(parts) != 2 {
		t.Fatalf("roles %v", h.runner.roles())
	}
	for _, p := range parts {
		if len(p.Allowlist) == 0 || p.ReadOnly {
			t.Fatalf("part %s has no allowlist or is read-only: %+v", p.Part, p)
		}
		for _, entry := range p.Allowlist {
			if !strings.HasPrefix(entry, "Bash(slicer ") {
				continue
			}
			ok := false
			for _, verb := range []string{"show", "goals", "list", "deps", "find"} {
				ok = ok || strings.HasPrefix(entry, "Bash(slicer "+verb)
			}
			if !ok {
				t.Errorf("part %s may run %q, a slicer command that can write", p.Part, entry)
			}
		}
		if !slices.Contains(p.Allowlist, "Edit") || !slices.Contains(p.Allowlist, "Bash(go test*)") {
			t.Errorf("part %s lost the implementer's other entries: %v", p.Part, p.Allowlist)
		}
		if !strings.Contains(p.Brief, "Run no slicer command that writes") {
			t.Errorf("part %s brief does not forbid a slicer write", p.Part)
		}
	}
	// The lead keeps `slicer *`: it runs start and the tracking steps.
	if lead := requestsOf(h, RoleImplementer)[0]; !slices.Contains(lead.Allowlist, "Bash(slicer *)") {
		t.Errorf("the lead lost `Bash(slicer *)`: %v", lead.Allowlist)
	}
	// ork's own slicer writes before the finish step are the ledger's steps; a
	// part round adds no start, note, edit, handoff or done.
	for _, c := range h.fake.CallsTo("slicer") {
		if len(c.Args) > 0 && slices.Contains([]string{"start", "note", "done", "handoff", "edit"}, c.Args[0]) {
			t.Errorf("a slicer write ran before the finish step: %v", c.Args)
		}
	}
}

func TestSplitReviewSeesTheCombinedDiffOnce(t *testing.T) {
	h, _ := splitHarness(t, "M", []string{partOne, partTwo})
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	roles := h.runner.roles()
	var reviews, gates int
	for _, r := range roles {
		switch {
		case strings.HasPrefix(r, "reviewer-"):
			reviews++
		case strings.HasPrefix(r, "gate-"):
			gates++
		}
	}
	if reviews != 1 || gates != 1 {
		t.Fatalf("roles %v: %d reviews and %d gates, want one of each for the whole slice", roles, reviews, gates)
	}
	// The reviewer comes after the last part and reads every part's report.
	last := -1
	for i, r := range roles {
		if strings.HasPrefix(r, "part-") {
			last = i
		}
	}
	reviewer := requestsOf(h, RoleReviewer)[0]
	if first := slices.IndexFunc(roles, func(r string) bool { return strings.HasPrefix(r, "reviewer-") }); first < last {
		t.Fatalf("the reviewer ran before the last part ended: %v", roles)
	}
	for _, name := range []string{"part-P1-1", "part-P2-1"} {
		if !strings.Contains(reviewer.Brief, "### "+name) {
			t.Errorf("the reviewer's brief lacks the report %s", name)
		}
	}
	// One round check on the combined tree, as in a run with no split.
	plain := newHarness(t)
	plainRec := plain.start().Snapshot()
	if len(rec.Checks) != len(plainRec.Checks) {
		t.Fatalf("%d checks ran, want %d as in a run with one implementer: %+v", len(rec.Checks), len(plainRec.Checks), rec.Checks)
	}
}

func TestSplitArchitectRunsOnlyOnSizeL(t *testing.T) {
	// The matrix gives the architect to size L only (adj 33, adj 151): the plan's
	// lineup has an architect row for a size L slice and none below. The engine
	// reads no size; with a row it runs the architect once for the whole slice,
	// after the review and before the gate, however many parts there were.
	for _, size := range []string{"M", "L"} {
		t.Run(size, func(t *testing.T) {
			h, _ := splitHarness(t, size, []string{partOne, partTwo})
			if size == "L" {
				h.cfg.Lineup[RoleArchitect] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "high"}}
				script := h.runner.script
				h.runner.script = func(req Request, n int) (Result, error) {
					if req.Role == RoleArchitect {
						return Result{Exit: ExitOK, Session: "sess-architect", Report: architectReport}, nil
					}
					return script(req, n)
				}
			}
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			if n := len(requestsOf(h, RolePart)); n != 2 {
				t.Fatalf("%d parts ran, want the two the lead listed: %v", n, h.runner.roles())
			}
			var architects []string
			for _, r := range h.runner.roles() {
				if strings.HasPrefix(r, "architect-") {
					architects = append(architects, r)
				}
			}
			if size == "M" && len(architects) != 0 {
				t.Fatalf("a size M split ran the architect: %v", h.runner.roles())
			}
			if size == "L" {
				if len(architects) != 1 {
					t.Fatalf("a size L split ran the architect %d times: %v", len(architects), h.runner.roles())
				}
				roles := h.runner.roles()
				arch := slices.Index(roles, architects[0])
				rev := slices.IndexFunc(roles, func(r string) bool { return strings.HasPrefix(r, "reviewer-") })
				gate := slices.IndexFunc(roles, func(r string) bool { return strings.HasPrefix(r, "gate-") })
				lastPart := -1
				for i, r := range roles {
					if strings.HasPrefix(r, "part-") {
						lastPart = i
					}
				}
				if !(lastPart < rev && rev < arch && arch < gate) {
					t.Fatalf("the architect is not after the parts and the review and before the gate: %v", roles)
				}
			}
		})
	}
}

// A part that reports failed tests, or that changes a file outside its list,
// fails the round and goes back to that part once.
func TestPartThatFailsItsCheckGoesBackToThatPartOnce(t *testing.T) {
	failed := "Diff stat.\n\nTests: 3 passed, 1 failed\n\n## Owner needed\n\nnone\n"
	t.Run("fixed on the second try", func(t *testing.T) {
		h, run := splitHarness(t, "M", []string{partOne, partTwo})
		var mu sync.Mutex
		tries := map[string]int{}
		run.partReport = func(id string) string {
			mu.Lock()
			defer mu.Unlock()
			tries[id]++
			if id == "P2" && tries[id] == 1 {
				return failed
			}
			return implReport
		}
		rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
		if tries["P1"] != 1 || tries["P2"] != 2 {
			t.Fatalf("tries %v, want P1 once and P2 twice", tries)
		}
		second := requestsOf(h, RolePart)[2]
		if second.Part != "P2" || !strings.Contains(second.Brief, "1 failed") || !strings.Contains(second.Brief, "Fix only") {
			t.Fatalf("the second P2 round is not a fix of the failed check: %+v", second)
		}
		if !rec.Parts[1].Done || rec.Parts[1].Attempts != 2 {
			t.Fatalf("parts %+v", rec.Parts)
		}
	})
	t.Run("fails twice", func(t *testing.T) {
		h, run := splitHarness(t, "M", []string{partOne})
		run.partReport = func(string) string { return failed }
		rec := requireState(t, h.start(), runrec.StatePaused)
		if n := len(requestsOf(h, RolePart)); n != 2 {
			t.Fatalf("part ran %d times, want the first try and one more", n)
		}
		if !strings.Contains(rec.Reason, "P1") || len(requestsOf(h, RoleReviewer)) != 0 {
			t.Fatalf("reason %q, roles %v", rec.Reason, h.runner.roles())
		}
	})
}

func TestPartThatChangesAFileOutsideItsListFailsTheRound(t *testing.T) {
	// git status of the worktree before the batch, then after it. The part
	// changed its own file and internal/c/c.go, which is not on its list; its
	// second try changed only its own file.
	h, _ := splitHarnessStatus(t, "M", []string{partOne}, []string{"", " M internal/a/a.go\x00 M internal/c/c.go\x00", "", " M internal/a/a.go\x00"})
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	parts := requestsOf(h, RolePart)
	if len(parts) != 2 {
		t.Fatalf("part ran %d times, want the first try and one fix: %v", len(parts), h.runner.roles())
	}
	if !strings.Contains(parts[1].Brief, "internal/c/c.go") || !strings.Contains(parts[1].Brief, "outside your list") {
		t.Fatalf("the fix round does not name the file outside the list:\n%s", parts[1].Brief)
	}
	if !rec.Parts[0].Done {
		t.Fatalf("parts %+v", rec.Parts)
	}
}

// The lead's list is checked against the slice's Files section before any part
// runs; a list that fails the check pauses the run and runs no part.
func TestLeadPartsListThatBreaksTheRulesPausesWithNoPartRun(t *testing.T) {
	for name, tc := range map[string]struct {
		parts []string
		want  string
	}{
		"a file outside the Files section": {[]string{"P1: files internal/z/z.go; tests TestZ"}, "internal/z/z.go"},
		"a repeated part id":               {[]string{partOne, "P1: files internal/b/b.go; tests TestB"}, "P1"},
		"a part with no tests":             {[]string{"P1: files internal/a/a.go; tests"}, "P1"},
		"a line that is no part":           {[]string{"do the work"}, "do the work"},
	} {
		t.Run(name, func(t *testing.T) {
			h, _ := splitHarness(t, "M", tc.parts)
			rec := requireState(t, h.start(), runrec.StatePaused)
			if !strings.Contains(rec.Reason, tc.want) || len(requestsOf(h, RolePart)) != 0 {
				t.Fatalf("reason %q; roles %v", rec.Reason, h.runner.roles())
			}
		})
	}
}

func TestLeadReportWithoutAPartsSectionPausesAndNoneRunsNoPart(t *testing.T) {
	h, run := splitHarness(t, "M", nil)
	run.leadReport = implReport
	rec := requireState(t, h.start(), runrec.StatePaused)
	if !strings.Contains(rec.Reason, "## Parts") || len(requestsOf(h, RolePart)) != 0 {
		t.Fatalf("reason %q; roles %v", rec.Reason, h.runner.roles())
	}
	h, _ = splitHarness(t, "M", nil)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	if len(requestsOf(h, RolePart)) != 0 {
		t.Fatalf("a lead that listed no part ran parts: %v", h.runner.roles())
	}
}

// The lead's fix-round report: the implementer's shape and a Hand back section.
func leadFixReport(handBack string) string {
	return "Diff stat.\n\nTests: 12 passed, 0 failed\n\n## Hand back\n\n" + handBack + "\n\n## Owner needed\n\nnone\n"
}

// handBackHarness is a split run whose reviewer gives one fix finding in round 1,
// so the lead gets a fix round (round 2) and answers it with the given report.
func handBackHarness(t *testing.T, fixReport string) (*harness, *splitRun) {
	t.Helper()
	h, run := splitHarness(t, "M", []string{partOne, partTwo})
	run.fixReport = fixReport
	script := h.runner.script
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleReviewer && req.Round == 1 {
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: findingsReview("internal/b/b.go:3 the nil case is wrong")}, nil
		}
		return script(req, n)
	}
	return h, run
}

func partRequestsInRound(h *harness, round int) []Request {
	var out []Request
	for _, r := range requestsOf(h, RolePart) {
		if r.Round == round {
			out = append(out, r)
		}
	}
	return out
}

// The owner's decision (adj 385): a fix round goes to the lead, who may hand a
// file's findings back to the part that owns the file for that round.
func TestLeadFixRoundHandsAFilesFindingsBackToTheOwningPart(t *testing.T) {
	h, _ := handBackHarness(t, leadFixReport("- P2: internal/b/b.go:3 the nil case is wrong; return the error."))
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	back := partRequestsInRound(h, 2)
	if len(back) != 1 || back[0].Part != "P2" {
		t.Fatalf("round 2 part requests %+v, want only P2 (roles %v)", back, h.runner.roles())
	}
	for _, want := range []string{"Findings handed back by the lead", "return the error", "Fix only that, in your own files", "Part: P2"} {
		if !strings.Contains(back[0].Brief, want) {
			t.Errorf("the handed-back part's brief lacks %q", want)
		}
	}
	if back[0].Worktree != h.W {
		t.Errorf("the part runs in %q, want the slice worktree", back[0].Worktree)
	}
	// The lead's fix-round brief names the parts and their files, and the way to hand back.
	var fix Request
	for _, r := range requestsOf(h, RoleImplementer) {
		if r.Round == 2 {
			fix = r
		}
	}
	for _, want := range []string{"## Hand back to a part", "P1", "P2", "internal/b/b.go", "`## Hand back`"} {
		if !strings.Contains(fix.Brief, want) {
			t.Errorf("the lead's fix-round brief lacks %q", want)
		}
	}
	if !rec.Parts[1].Done || rec.Parts[1].HandBack != "" || rec.Parts[0].HandBack != "" {
		t.Fatalf("parts %+v: the hand-back is not spent", rec.Parts)
	}
	// The part's step goes under the lead's fix-round step.
	var leadStep, partParent string
	for i, s := range h.steps() {
		switch {
		case s.Task == item+" lead fix round 1":
			leadStep = strconv.Itoa(i + 1)
		case s.Task == item+" part P2":
			partParent = s.Parent // the last one: the round 2 step
		}
	}
	if leadStep == "" || partParent != leadStep {
		t.Fatalf("the round 2 part step has parent %q, want the lead's fix step %q", partParent, leadStep)
	}
	// One review and gate still follow: the review of round 2 sees the whole tree.
	if n := len(requestsOf(h, RoleGate)); n != 1 {
		t.Fatalf("%d gates, want 1: %v", n, h.runner.roles())
	}
}

func TestLeadFixRoundThatHandsNothingBackRunsNoPart(t *testing.T) {
	for name, report := range map[string]string{
		"none":       leadFixReport("none"),
		"no section": implReport,
	} {
		t.Run(name, func(t *testing.T) {
			h, _ := handBackHarness(t, report)
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			if n := len(partRequestsInRound(h, 2)); n != 0 {
				t.Fatalf("%d parts ran in the fix round, want none: %v", n, h.runner.roles())
			}
		})
	}
}

func TestLeadHandBackThatNamesAnUnknownPartPausesAndRunsNoPart(t *testing.T) {
	h, _ := handBackHarness(t, leadFixReport("- P9: internal/z/z.go:1 wrong"))
	rec := requireState(t, h.start(), runrec.StatePaused)
	if !strings.Contains(rec.Reason, "P9") || len(partRequestsInRound(h, 2)) != 0 {
		t.Fatalf("reason %q; roles %v", rec.Reason, h.runner.roles())
	}
}

// A handed-back part is checked as any part: a failed test goes back to it once.
func TestHandedBackPartThatFailsItsCheckGoesBackOnce(t *testing.T) {
	failed := "Diff stat.\n\nTests: 3 passed, 1 failed\n\n## Owner needed\n\nnone\n"
	h, run := handBackHarness(t, leadFixReport("- P2: internal/b/b.go:3 the nil case is wrong"))
	var mu sync.Mutex
	tries := map[string]int{}
	run.partReport = func(id string) string {
		mu.Lock()
		defer mu.Unlock()
		tries[id]++
		if id == "P2" && tries[id] == 2 { // the first try of the hand-back
			return failed
		}
		return implReport
	}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	if got := len(partRequestsInRound(h, 2)); got != 2 || tries["P2"] != 3 {
		t.Fatalf("round 2 part runs %d, P2 tries %d, want 2 and 3", got, tries["P2"])
	}
}

// A part that fails twice pauses the run; a retry runs that part again and
// does not run the lead's fix round again.
func TestHandBackPausesOnASecondFailureAndRetryRunsOnlyThePart(t *testing.T) {
	failed := "Diff stat.\n\nTests: 3 passed, 1 failed\n\n## Owner needed\n\nnone\n"
	h, run := handBackHarness(t, leadFixReport("- P2: internal/b/b.go:3 the nil case is wrong"))
	var mu sync.Mutex
	tries := map[string]int{}
	broken := true
	run.partReport = func(id string) string {
		mu.Lock()
		defer mu.Unlock()
		tries[id]++
		if id == "P2" && tries[id] > 1 && broken {
			return failed
		}
		return implReport
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if !strings.Contains(rec.Reason, "P2") || rec.Parts[1].HandBack == "" || rec.Parts[1].Done {
		t.Fatalf("reason %q, parts %+v", rec.Reason, rec.Parts)
	}
	leadFixes := func() int {
		n := 0
		for _, r := range requestsOf(h, RoleImplementer) {
			if r.Round == 2 {
				n++
			}
		}
		return n
	}
	if leadFixes() != 1 {
		t.Fatalf("%d lead fix rounds before the retry, want 1", leadFixes())
	}
	mu.Lock()
	broken = false
	mu.Unlock()
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec = requireState(t, e, runrec.StateAwaitingApproval)
	if leadFixes() != 1 || !rec.Parts[1].Done || rec.Parts[1].HandBack != "" {
		t.Fatalf("lead fix rounds %d after the retry (want 1); parts %+v", leadFixes(), rec.Parts)
	}
}

func TestParseHandBack(t *testing.T) {
	parts := []runrec.Part{{ID: "P1"}, {ID: "P2"}}
	report := func(body string) string { return leadFixReport(body) }
	for name, tc := range map[string]struct {
		text string
		want map[string]string
		err  string
	}{
		"one part":          {report("- P2: internal/b/b.go:3 wrong"), map[string]string{"P2": "internal/b/b.go:3 wrong"}, ""},
		"two parts":         {report("- P1: first\n- P2: second"), map[string]string{"P1": "first", "P2": "second"}, ""},
		"a continuation":    {report("- P1: first line\n  second line"), map[string]string{"P1": "first line\nsecond line"}, ""},
		"a blank line":      {report("- P1: first\n\n  second"), map[string]string{"P1": "first\n\nsecond"}, ""},
		"none":              {report("none"), nil, ""},
		"no section":        {implReport, nil, ""},
		"unknown part":      {report("- P9: x"), nil, "P9"},
		"listed twice":      {report("- P1: a\n- P1: b"), nil, "P1"},
		"no text":           {report("- P1:"), nil, "P1"},
		"not a part line":   {report("- do the work"), nil, "do the work"},
		"prose is not read": {report("none\n\nsome prose"), nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseHandBack(tc.text, parts)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("error %v, want one naming %q", err, tc.err)
				}
				return
			}
			if err != nil || fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

const askReport = "Diff stat.\n\nTests: 1 passed, 0 failed\n\n## Owner needed\n\nShould the part return an error?\n"

// Review finding: a part that finished must stay finished when a sibling of its
// batch pauses, and a retry runs only the part that paused.
func TestFinishedPartStaysFinishedWhenASiblingPauses(t *testing.T) {
	h, run := splitHarness(t, "M", []string{partOne, partTwo})
	var mu sync.Mutex
	tries := map[string]int{}
	run.partReport = func(id string) string {
		mu.Lock()
		defer mu.Unlock()
		tries[id]++
		if id == "P2" && tries[id] == 1 {
			return askReport
		}
		return implReport
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if !rec.Parts[0].Done || rec.Parts[1].Done {
		t.Fatalf("parts %+v: P1 finished and P2 asked the owner", rec.Parts)
	}
	if err := e.Answer(context.Background(), "yes"); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	mu.Lock()
	defer mu.Unlock()
	if tries["P1"] != 1 || tries["P2"] != 2 {
		t.Fatalf("runs %v, want P1 once and P2 twice", tries)
	}
}

// Review finding: two parts share the role "part"; a shape pause belongs to the
// record of the part that made it.
func TestShapePauseOfAPartIsKeptOnThatPartsRecord(t *testing.T) {
	h, _ := splitHarness(t, "M", []string{partOne, partTwo})
	e := h.start()
	if err := e.run.Update(func(r *runrec.Record) {
		r.State, r.Pending = runrec.StateImplement, nil
		r.Roles = append(r.Roles, runrec.RoleRecord{Role: RolePart, Part: "P2", Round: 1}, runrec.RoleRecord{Role: RolePart, Part: "P1", Round: 1})
	}); err != nil {
		t.Fatal(err)
	}
	shape := func(part string) runrec.Pending {
		return runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateImplement, Role: RolePart, Part: part, Round: 1, ShapePauses: 1}
	}
	if err := e.pauseShape(shape("P1")); err != nil {
		t.Fatal(err)
	}
	if err := e.pauseShape(shape("P2")); err != nil {
		t.Fatal(err)
	}
	rec := e.Snapshot()
	kept := map[string]string{}
	for _, r := range rec.Roles[len(rec.Roles)-2:] {
		if r.ShapeRetry != nil {
			kept[r.Part] = r.ShapeRetry.Part
		}
	}
	if kept["P2"] != "P2" || kept["P1"] == "P2" {
		t.Fatalf("shape pauses by record (record part -> pause part): %v", kept)
	}
}

// The part with more shape pauses takes the pause slot and the other part's
// pause moves to that part's own record.
func TestShapePauseOfAPartWithMorePausesTakesTheSlot(t *testing.T) {
	h, _ := splitHarness(t, "M", []string{partOne, partTwo})
	e := h.start()
	if err := e.run.Update(func(r *runrec.Record) {
		r.State, r.Pending = runrec.StateImplement, nil
		r.Roles = append(r.Roles, runrec.RoleRecord{Role: RolePart, Part: "P1", Round: 1}, runrec.RoleRecord{Role: RolePart, Part: "P2", Round: 1})
	}); err != nil {
		t.Fatal(err)
	}
	shape := func(part string, pauses int) runrec.Pending {
		return runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateImplement, Role: RolePart, Part: part, Round: 1, ShapePauses: pauses}
	}
	if err := e.pauseShape(shape("P1", 1)); err != nil {
		t.Fatal(err)
	}
	if err := e.pauseShape(shape("P2", 2)); err != nil {
		t.Fatal(err)
	}
	rec := e.Snapshot()
	if rec.Pending == nil || rec.Pending.Part != "P2" {
		t.Fatalf("pending %+v, want P2's", rec.Pending)
	}
	p1 := rec.Roles[len(rec.Roles)-2]
	if p1.Part != "P1" || p1.ShapeRetry == nil || p1.ShapeRetry.Part != "P1" {
		t.Fatalf("P1's record %+v: its pause should be kept on it", p1)
	}
}

// Review finding: overlap is decided on cleaned paths.
func TestParsePartsCleansPaths(t *testing.T) {
	parts, err := parseParts(leadReport("P1: files internal/a/../b.go, internal/./c.go; tests TestA"))
	if err != nil || len(parts) != 1 || fmt.Sprint(parts[0].Files) != "[internal/b.go internal/c.go]" {
		t.Fatalf("parts %+v, err %v", parts, err)
	}
}

// Review finding: a stray file of a batch of several parts fails only the part
// that changed it, found by running the parts one after another.
func TestAStrayFileOfASharedBatchFailsOnlyThePartThatChangedIt(t *testing.T) {
	const (
		own   = " M internal/a/a.go\x00 M internal/b/b.go\x00"
		stray = "?? internal/c/c.go\x00"
		moved = "A  internal/c/c.go\x00"
	)
	// Calls: batch before, batch after (P1 made c.go); P1 alone before and after
	// (it changed c.go again); P2 alone before and after (it changed nothing);
	// P1 again before and after (it put c.go back).
	h, _ := splitHarnessStatus(t, "M", []string{partOne, partTwo}, []string{"", own + stray, own + stray, own + moved, own + moved, own + moved, own + moved, own})
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	counts := map[string]int{}
	var last Request
	for _, r := range requestsOf(h, RolePart) {
		counts[r.Part]++
		if r.Part == "P1" {
			last = r
		}
	}
	if counts["P1"] != 3 || counts["P2"] != 2 {
		t.Fatalf("part runs %v, want P1 three times (with, alone, again) and P2 twice (with, alone): %v", counts, h.runner.roles())
	}
	if !strings.Contains(last.Brief, "internal/c/c.go") || !rec.Parts[0].Done || !rec.Parts[1].Done {
		t.Fatalf("parts %+v; the last P1 brief:\n%s", rec.Parts, last.Brief)
	}
}

func TestSplitRunNeedsAPartChain(t *testing.T) {
	h := newHarness(t)
	h.cfg.Split = true
	if _, err := New(t.Context(), h.deps, h.cfg); err == nil || !strings.Contains(err.Error(), "no part") {
		t.Fatalf("a split run with no part chain: %v", err)
	}
}

func TestBatchesOfParts(t *testing.T) {
	part := func(id string, files ...string) runrec.Part { return runrec.Part{ID: id, Files: files} }
	for name, tc := range map[string]struct {
		parts []runrec.Part
		want  [][]string
	}{
		"disjoint":              {[]runrec.Part{part("A", "a"), part("B", "b"), part("C", "c")}, [][]string{{"A", "B", "C"}}},
		"capped at three":       {[]runrec.Part{part("A", "a"), part("B", "b"), part("C", "c"), part("D", "d")}, [][]string{{"A", "B", "C"}, {"D"}}},
		"overlap waits":         {[]runrec.Part{part("A", "a"), part("B", "a"), part("C", "c")}, [][]string{{"A", "C"}, {"B"}}},
		"a directory covers":    {[]runrec.Part{part("A", "x"), part("B", "x/y.go"), part("C", "z")}, [][]string{{"A", "C"}, {"B"}}},
		"a prefix is not a dir": {[]runrec.Part{part("A", "x"), part("B", "xy.go")}, [][]string{{"A", "B"}}},
		"one part":              {[]runrec.Part{part("A", "a")}, [][]string{{"A"}}},
		"a parent segment":      {[]runrec.Part{part("A", "internal/b.go"), part("B", "internal/a/../b.go")}, [][]string{{"A"}, {"B"}}},
		"a dot segment":         {[]runrec.Part{part("A", "internal/b.go"), part("B", "internal/./b.go")}, [][]string{{"A"}, {"B"}}},
	} {
		t.Run(name, func(t *testing.T) {
			var got [][]string
			for _, b := range partBatches(tc.parts) {
				var ids []string
				for _, i := range b {
					ids = append(ids, tc.parts[i].ID)
				}
				got = append(got, ids)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("batches %v, want %v", got, tc.want)
			}
		})
	}
}
