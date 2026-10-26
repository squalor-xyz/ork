// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// uiSliceJSON is the harness slice with a Files section.
func uiSliceJSON(files string) string {
	body, _ := json.Marshal(files)
	return `{"id":"S04","title":"Engine","status":"started","slice":{"id":"S04","title":"Engine","size":"M","sections":[` +
		`{"heading":"Why","body":"Because."},{"heading":"Files","body":` + string(body) + `},` +
		`{"heading":"Git","body":"One commit: \"` + gitTitle + `\"."}]}}`
}

// uiHarness is a harness whose slice names the given Files section and whose
// lineup holds a UI pass row.
func uiHarness(t *testing.T, files string) *harness {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "slicer", Args: []string{"show", item, "--json", "--lean", "--root", W}, Stdout: uiSliceJSON(files)}}
		}
	})
	h.cfg.Lineup[RoleUI] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}}
	return h
}

// noSections has the Owner needed section but none of the sections (A) to
// (D): a shape error of the findings, not of the shared shape.
const noSections = "Looks fine.\n\n## Owner needed\n\nnone\n"

const tuiFiles = "- `internal/tui/model.go:40`: the pane.\n- `internal/tui/testdata/golden/launch.txt`"

// sortedRoles lists the role requests so far; the reviewer and the UI pass of
// a round run in parallel, so they are put in a fixed order.
func sortedRoles(h *harness) []string {
	got := h.runner.roles()
	if len(got) > 2 && got[1] > got[2] && strings.HasPrefix(got[1], "ui") {
		got[1], got[2] = got[2], got[1]
	}
	return got
}

func TestUIPassRunsWithReviewerOnUISlice(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	// Each of the two roles waits until the other has started. Run one after
	// the other, neither would ever see its peer and both would time out.
	var mu sync.Mutex
	started := map[string]chan struct{}{RoleReviewer: make(chan struct{}), RoleUI: make(chan struct{})}
	waited := map[string]bool{}
	h.runner.script = func(req Request, _ int) (Result, error) {
		peer := map[string]string{RoleReviewer: RoleUI, RoleUI: RoleReviewer}[req.Role]
		if peer == "" {
			return Result{}, nil
		}
		close(started[req.Role])
		select {
		case <-started[peer]:
			mu.Lock()
			waited[req.Role] = true
			mu.Unlock()
		case <-time.After(2 * time.Second):
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	mu.Lock()
	defer mu.Unlock()
	if !waited[RoleReviewer] || !waited[RoleUI] {
		t.Fatalf("the reviewer and the UI pass did not run at the same time: saw peer %v, roles %v", waited, h.runner.roles())
	}
	want := []string{"implementer-1", "reviewer-1", "ui-1", "gate-1"}
	if got := sortedRoles(h); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles %v, want %v", got, want)
	}
	for _, req := range h.runner.requests() {
		if req.Role != RoleUI {
			continue
		}
		if !req.ReadOnly || len(req.Allowlist) != 0 {
			t.Errorf("the UI pass is not read-only: %+v", req)
		}
		if req.Vendor != "claude" || req.Model != "opus" || req.Effort != "medium" {
			t.Errorf("the UI pass ran %s %s %s, want the lineup's ui row", req.Vendor, req.Model, req.Effort)
		}
		for _, text := range []string{h.W, "ui-1.md", "tmp/ui-1", "80x24"} {
			if !strings.Contains(req.Brief, text) {
				t.Errorf("UI pass brief missing %q", text)
			}
		}
		if strings.Contains(req.Brief, "{{") {
			t.Error("UI pass prompt has an unfilled placeholder")
		}
	}
	var sawUI bool
	for _, r := range rec.Roles {
		if r.Role == RoleUI {
			sawUI = true
			if r.Exit != string(ExitOK) || r.Report != "ui-1.md" {
				t.Errorf("UI role record %+v", r)
			}
		}
	}
	if !sawUI {
		t.Fatalf("no ui role record: %+v", rec.Roles)
	}
}

// The ledger has no ui role yet (slicer s392), so the UI pass is a reviewer
// step whose task says ui.
func TestUIPassLedgerStepIsReviewerWithUITask(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	var tasks []string
	for _, s := range h.steps() {
		if s.Phase == "start" && s.Task != "" && s.Role == "reviewer" {
			tasks = append(tasks, s.Task)
		}
		if s.Role == "ui" {
			t.Errorf("ledger step uses role ui, which slicer does not know: %+v", s)
		}
	}
	sort.Strings(tasks)
	if want := []string{"S04 reviewer", "S04 ui"}; !reflect.DeepEqual(tasks, want) {
		t.Fatalf("reviewer-role steps %v, want %v", tasks, want)
	}
}

func TestUIPassSkippedOnEngineSlice(t *testing.T) {
	h := uiHarness(t, "- `internal/engine/states.go:229` (review round)\n- `docs/design.md:393-425`\n- `internal/roles/roles.go`")
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	want := []string{"implementer-1", "reviewer-1", "gate-1"}
	if got := h.runner.roles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles %v, want %v", got, want)
	}
}

func TestUIPassNeedsALineupRow(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	delete(h.cfg.Lineup, RoleUI)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	want := []string{"implementer-1", "reviewer-1", "gate-1"}
	if got := h.runner.roles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("roles %v, want %v", got, want)
	}
}

func TestUIPassFindingsReachFixRound(t *testing.T) {
	for name, tc := range map[string]struct {
		reviewer, ui string
		want, absent []string
	}{
		"ui only":       {cleanReview, findingsReview("ui-model.go:10 the key hint clips at 80 columns"), []string{"ui-model.go:10 the key hint clips at 80 columns", "### UI pass report (ui-1)"}, []string{"### Reviewer report"}},
		"both":          {findingsReview("states.go:10 reviewer finding"), findingsReview("ui-model.go:10 the key hint clips at 80 columns"), []string{"states.go:10 reviewer finding", "ui-model.go:10 the key hint clips at 80 columns", "### Reviewer report (reviewer-1)", "### UI pass report (ui-1)"}, nil},
		"reviewer only": {findingsReview("states.go:10 reviewer finding"), cleanReview, []string{"states.go:10 reviewer finding"}, []string{"ui-model.go:", "UI pass report"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := uiHarness(t, tuiFiles)
			h.runner.script = func(req Request, _ int) (Result, error) {
				switch req.Role {
				case RoleReviewer:
					return Result{Exit: ExitOK, Session: "review-session", Report: tc.reviewer}, nil
				case RoleUI:
					return Result{Exit: ExitOK, Session: "ui-session", Report: tc.ui}, nil
				}
				return Result{}, nil
			}
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			reqs := h.runner.requests()
			var fix *Request
			for i := range reqs {
				if reqs[i].Role == RoleImplementer && reqs[i].Round == 2 {
					fix = &reqs[i]
				}
			}
			if fix == nil {
				t.Fatalf("no fix round: roles %v", h.runner.roles())
			}
			for _, text := range tc.want {
				if !strings.Contains(fix.Brief, text) {
					t.Errorf("fix brief missing %q:\n%s", text, fix.Brief)
				}
			}
			for _, text := range tc.absent {
				if strings.Contains(fix.Brief, text) {
					t.Errorf("fix brief holds %q", text)
				}
			}
			if !strings.Contains(fix.Brief, "## Review findings") {
				t.Error("fix brief has no Review findings section")
			}
			// The review does not run again: the gate follows the fix round.
			if got := h.runner.roles(); got[len(got)-1] != "gate-3" && got[len(got)-1] != "gate-2" {
				t.Errorf("roles %v do not end in the gate", got)
			}
		})
	}
}

// A role that ended well is not run again when the other role paused the
// review: a retry runs only the role that paused.
func TestUIPassRetryRunsOnlyThePausedRole(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	var mu sync.Mutex
	uiRuns := 0
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role != RoleUI {
			return Result{}, nil
		}
		mu.Lock()
		defer mu.Unlock()
		uiRuns++
		if uiRuns == 1 {
			return Result{Exit: ExitOK, Report: noSections}, nil
		}
		return Result{Exit: ExitOK, Report: cleanReview}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape || rec.Pending.Role != RoleUI {
		t.Fatalf("pending %+v, want a shape pause of the ui role", rec.Pending)
	}
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	count := map[string]int{}
	for _, r := range h.runner.roles() {
		count[r]++
	}
	if count["reviewer-1"] != 1 || count["ui-1"] != 2 {
		t.Fatalf("roles %v: the reviewer must run once and the UI pass twice", h.runner.roles())
	}
}

// When both roles give a report that breaks its shape, the review keeps the
// first pause and a retry runs both again.
func TestUIPassBothPauseKeepsOnePauseAndRerunsBoth(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	var mu sync.Mutex
	runs := map[string]int{}
	refused := map[string]bool{} // the role whose rerun brief carries a shape pause
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role != RoleReviewer && req.Role != RoleUI {
			return Result{}, nil
		}
		mu.Lock()
		defer mu.Unlock()
		runs[req.Role]++
		if strings.Contains(req.Brief, "ork checked your last report and refused it") {
			refused[req.Role] = true
		}
		if runs[req.Role] == 1 {
			return Result{Exit: ExitOK, Report: noSections}, nil
		}
		return Result{Exit: ExitOK, Report: cleanReview}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape {
		t.Fatalf("pending %+v", rec.Pending)
	}
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateAwaitingApproval)
	if runs[RoleReviewer] != 2 || runs[RoleUI] != 2 {
		t.Fatalf("runs %v, want each role twice", runs)
	}
	// The pause names one role; the other reruns without its pause context.
	if len(refused) != 1 || !refused[rec.Pending.Role] {
		t.Fatalf("shape pause reached the briefs of %v, want only %s", refused, rec.Pending.Role)
	}
}

// A UI pass that fails stops the slice, as a failed reviewer does, and a
// reviewer that ended well is not lost from the record.
func TestUIPassFailureStopsTheSlice(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleUI {
			return Result{Exit: ExitAuth}, nil
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateStopped)
	if !strings.Contains(rec.Reason, "ui round 1 exited auth") {
		t.Fatalf("reason %q", rec.Reason)
	}
	for _, r := range h.runner.roles() {
		if strings.HasPrefix(r, "gate") {
			t.Fatalf("the gate ran after a stopped review: %v", h.runner.roles())
		}
	}
}

func TestUIPassFallsBackThroughItsOwnChain(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	h.cfg.Lineup[RoleUI] = []runrec.LineupEntry{{Vendor: "codex", Model: "gpt", Effort: "high"}, {Vendor: "claude", Model: "opus", Effort: "medium"}}
	h.runner.exits = map[string]ExitClass{"codex": ExitRateLimited}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	var uis []string
	for _, r := range rec.Roles {
		if r.Role == RoleUI {
			uis = append(uis, r.Vendor+":"+r.Exit)
		}
	}
	if want := []string{"codex:rate_limited", "claude:ok"}; !reflect.DeepEqual(uis, want) {
		t.Fatalf("ui attempts %v, want %v", uis, want)
	}
	reviews := 0
	for _, r := range h.runner.roles() {
		if r == "reviewer-1" {
			reviews++
		}
	}
	if reviews != 1 {
		t.Fatalf("the reviewer ran %d times, want 1: %v", reviews, h.runner.roles())
	}
}

func TestSliceTouchesUI(t *testing.T) {
	for name, tc := range map[string]struct {
		files string
		want  bool
	}{
		"tui file":          {"- `internal/tui/model.go`", true},
		"tui with anchor":   {"- `internal/tui/model.go:40-60` (view)", true},
		"tui directory":     {"- `internal/tui`", true},
		"tui golden":        {"- `internal/tui/testdata/golden/x.txt`", true},
		"dashboard doc":     {"- `docs/dashboard.md:12`", true},
		"cli output":        {"- `internal/cli/run.go`", true},
		"adj ui key":        {"- the key `ui.pane-scroll`", true},
		"engine":            {"- `internal/engine/states.go`", false},
		"design doc":        {"- `docs/design.md`", false},
		"lookalike prefix":  {"- `internal/tui2/x.go`, `internal/client/x.go`", false},
		"other doc":         {"- `docs/state.md`", false},
		"tui outside tick":  {"- internal/tui/model.go without backticks", false},
		"non ui adj key":    {"- the key `roles.ui-pass`", false},
		"ui word in a path": {"- `internal/engine/ui.go`", false},
	} {
		t.Run(name, func(t *testing.T) {
			got := sliceTouchesUI([]slicercli.Section{{Heading: "Why", Body: "`internal/tui/model.go`"}, {Heading: "Files", Body: tc.files}})
			if got != tc.want {
				t.Fatalf("sliceTouchesUI(%q) = %v, want %v", tc.files, got, tc.want)
			}
		})
	}
	// Only the Files section counts.
	if sliceTouchesUI([]slicercli.Section{{Heading: "Why", Body: "`internal/tui/model.go`"}}) {
		t.Error("a path outside the Files section made the slice a UI slice")
	}
}

// A slice with no Files section, or one the engine cannot read, is not a UI
// slice; the reviewer still runs.
func TestUIPassSkippedWithoutFilesSection(t *testing.T) {
	h := newHarness(t)
	h.cfg.Lineup[RoleUI] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	if got, want := h.runner.roles(), []string{"implementer-1", "reviewer-1", "gate-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles %v, want %v", got, want)
	}
}

func TestUIPassReportIsKept(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if _, err := os.Stat(filepath.Join(h.store.RunDir(rec.ID), "ui-1.md")); err != nil {
		t.Fatal(err)
	}
	if text := h.readReport(rec, "ui-1.md"); !strings.Contains(text, "## Owner needed") {
		t.Fatalf("report %q", text)
	}
}

// The role that ends after the other paused the review does not clear the
// pause: the owner can still retry.
func TestUIPassLateRoleKeepsTheOthersPause(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		switch req.Role {
		case RoleUI:
			return Result{Exit: ExitOK, Report: "A report with no sections.\n"}, nil
		case RoleReviewer:
			for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				if e.Snapshot().State == runrec.StatePaused {
					break
				}
			}
		}
		return Result{}, nil
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Role != RoleUI || rec.Pending.Kind != runrec.PendingShape {
		t.Fatalf("pending %+v, want the ui role's shape pause", rec.Pending)
	}
}

// Two failed roles stop the slice once: the first reason stays and the ledger
// run ends once.
func TestUIPassBothFailStopsOnce(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleReviewer || req.Role == RoleUI {
			return Result{Exit: ExitAuth}, nil
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateStopped)
	if !strings.Contains(rec.Reason, "round 1 exited auth") || strings.Count(rec.Reason, "exited auth") != 1 {
		t.Fatalf("reason %q, want one stop reason", rec.Reason)
	}
	if ends := h.calls("slicer", "relay-run", "end"); len(ends) != 1 {
		t.Fatalf("ledger run ended %d times: %v", len(ends), ends)
	}
}

// The gate resumes the reviewer's session, which never saw the UI pass: its
// brief carries the UI report.
func TestUIPassReportReachesTheGate(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	h.runner.script = func(req Request, _ int) (Result, error) {
		if req.Role == RoleUI {
			return Result{Exit: ExitOK, Report: findingsReview("ui-model.go:10 the key hint clips at 80 columns")}, nil
		}
		return Result{}, nil
	}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	reqs := h.runner.requests()
	gate := reqs[len(reqs)-1]
	if gate.Role != RoleGate || !strings.Contains(gate.Brief, "## UI pass report (ui-1)") || !strings.Contains(gate.Brief, "ui-model.go:10 the key hint clips at 80 columns") {
		t.Fatalf("gate brief lacks the UI report: %s", gate.Brief)
	}
}

func TestUIRoleIsReadOnly(t *testing.T) {
	h := newHarness(t)
	perm, err := ResolvePermission(h.proj, RoleUI)
	if err != nil || !perm.ReadOnly || len(perm.Allow) != 0 {
		t.Fatalf("permission %+v, %v", perm, err)
	}
	h.proj.Permissions = map[string]registry.Permission{RoleUI: {Allow: []string{"Edit"}}}
	if _, err := ResolvePermission(h.proj, RoleUI); err == nil {
		t.Fatal("a registry allowlist for the read-only ui role was accepted")
	}
}

// A pause from one role does not undo the stop of the other: the slice stays
// stopped.
func TestUIPassStopIsNotUndoneByAPause(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		switch req.Role {
		case RoleUI:
			return Result{Exit: ExitAuth}, nil
		case RoleReviewer:
			for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				if e.Snapshot().State == runrec.StateStopped {
					break
				}
			}
			return Result{Exit: ExitOK, Report: noSections}, nil
		}
		return Result{}, nil
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateStopped)
	if rec.Pending != nil || !strings.Contains(rec.Reason, "ui round 1 exited auth") {
		t.Fatalf("reason %q, pending %+v", rec.Reason, rec.Pending)
	}
}

// A stop overrides a pause: the reviewer paused the review, then the UI pass
// failed, and the slice ends stopped with no pause context.
func TestUIPassStopOverridesAPause(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.runner.script = func(req Request, _ int) (Result, error) {
		switch req.Role {
		case RoleReviewer:
			return Result{Exit: ExitOK, Report: noSections}, nil
		case RoleUI:
			for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				if e.Snapshot().State == runrec.StatePaused {
					break
				}
			}
			return Result{Exit: ExitAuth}, nil
		}
		return Result{}, nil
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateStopped)
	if rec.Pending != nil || !strings.Contains(rec.Reason, "ui round 1 exited auth") {
		t.Fatalf("reason %q, pending %+v", rec.Reason, rec.Pending)
	}
}
