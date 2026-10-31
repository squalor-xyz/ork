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

// The UI pass has its own ledger role, beside the reviewer.
func TestUIPassLedgerStepUsesOwnRole(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	var tasks []string
	for _, s := range h.steps() {
		if s.Phase == "start" && s.Task != "" && (s.Role == "reviewer" || s.Role == "ui") {
			tasks = append(tasks, s.Task)
		}
		if s.Task == "S04 ui" && s.Role != "ui" {
			t.Errorf("UI task recorded as %s", s.Role)
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

// contextRunner lets the harness observe cancellation without a vendor process.
type contextRunner struct {
	run func(context.Context, Request) (Result, error)
}

func (r contextRunner) Run(ctx context.Context, req Request) (Result, error) { return r.run(ctx, req) }

func TestStopCancelsSiblingReviewRound(t *testing.T) {
	for _, stopping := range []string{RoleReviewer, RoleUI} {
		t.Run(stopping, func(t *testing.T) {
			h := uiHarness(t, tuiFiles)
			started := make(chan struct{})
			cancelled := make(chan struct{})
			h.deps.Runner = contextRunner{run: func(ctx context.Context, req Request) (Result, error) {
				if req.Role != RoleReviewer && req.Role != RoleUI {
					return defaultResult(req), nil
				}
				if req.Role == stopping {
					<-started
					return Result{Exit: ExitAuth}, nil
				}
				close(started)
				select {
				case <-ctx.Done():
					close(cancelled)
					return Result{}, ctx.Err()
				case <-time.After(2 * time.Second):
					return defaultResult(req), nil
				}
			}}
			e := h.start()
			requireState(t, e, runrec.StateStopped)
			select {
			case <-cancelled:
			default:
				t.Fatal("stopped round did not cancel sibling context")
			}
			for _, r := range e.Snapshot().Roles {
				if r.Running() {
					t.Fatalf("cancelled role still running: %+v", r)
				}
			}
		})
	}
}

func TestParallelRoundsKeepSeparateActivity(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	h.deps.Now = func() time.Time { return now }
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	reviewerStarted := make(chan struct{})
	uiStarted := make(chan struct{})
	h.runner.script = func(req Request, _ int) (Result, error) {
		switch req.Role {
		case RoleReviewer:
			req.OnEvent()
			req.OnLive(Live{Tokens: 3, Text: "reviewer"})
			close(reviewerStarted)
			<-uiStarted
			for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				rec := e.Snapshot()
				ended := false
				for _, r := range rec.Roles {
					if r.Role == RoleUI && !r.Running() {
						ended = true
					}
				}
				if ended {
					break
				}
			}
			got := e.Activity(now)
			if len(got) != 10 || got[9] != 1 || e.Live().Tokens != 3 || e.Live().Text != "reviewer" {
				t.Errorf("sibling completion erased reviewer activity: %v %+v", got, e.Live())
			}
		case RoleUI:
			<-reviewerStarted
			req.OnEvent()
			req.OnEvent()
			req.OnLive(Live{Tokens: 5, Text: "ui"})
			close(uiStarted)
			if got := e.Activity(now, RoleReviewer); len(got) != 10 || got[9] != 1 || e.Live(RoleReviewer).Tokens != 3 {
				t.Errorf("reviewer ring: %v %+v", got, e.Live(RoleReviewer))
			}
			if got := e.Activity(now, RoleUI); len(got) != 10 || got[9] != 2 || e.Live(RoleUI).Tokens != 5 {
				t.Errorf("UI ring: %v %+v", got, e.Live(RoleUI))
			}
			if got := e.Activity(now); len(got) != 10 || got[9] != 3 || e.Live().Tokens != 8 {
				t.Errorf("parallel activity lost: %v %+v", got, e.Live())
			}
		}
		return Result{}, nil
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLostPauseNamesEveryLostRole(t *testing.T) {
	h := uiHarness(t, tuiFiles)
	h.seed("parallel-lost", runrec.StateReview, runningRole(RoleReviewer, 111, "2"), runningRole(RoleUI, 222, "3"))
	h.proc.alive[111] = true
	rec := requireState(t, h.recover()["parallel-lost"], runrec.StatePaused)
	for _, name := range []string{"reviewer round 1 lost", "ui round 1 lost", "111", "killed", "gone"} {
		if !strings.Contains(rec.Reason, name) {
			t.Errorf("lost pause omits %q: %s", name, rec.Reason)
		}
	}
}

func TestSiblingOwnerQuestionKept(t *testing.T) {
	for _, first := range []string{RoleReviewer, RoleUI} {
		t.Run(first, func(t *testing.T) {
			h := uiHarness(t, tuiFiles)
			e, err := New(context.Background(), h.deps, h.cfg)
			if err != nil {
				t.Fatal(err)
			}
			h.runner.script = func(req Request, _ int) (Result, error) {
				if req.Role != RoleReviewer && req.Role != RoleUI {
					return Result{}, nil
				}
				if strings.Contains(req.Brief, "Owner answer") {
					if !strings.Contains(req.Brief, "answer-"+req.Role) || !strings.Contains(req.Brief, "question-"+req.Role) {
						t.Errorf("wrong answer brief: %s", req.Brief)
					}
					return Result{}, nil
				}
				if req.Role != first {
					for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
						if e.Snapshot().State == runrec.StatePaused {
							break
						}
					}
				}
				return Result{Exit: ExitOK, Session: "sess-" + req.Role, Report: strings.Replace(cleanReview, "none", "question-"+req.Role, 1)}, nil
			}
			if err := e.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			rec := requireState(t, e, runrec.StatePaused)
			if rec.Pending.Role != first || len(rec.Questions) != 2 || len(rec.QueuedQuestions) != 1 {
				t.Fatalf("questions: %+v pending: %+v", rec.Questions, rec.Pending)
			}
			// Reload before each answer to prove the queue and answer context are durable.
			load := func() {
				run, err := h.store.Open(rec.ID)
				if err != nil {
					t.Fatal(err)
				}
				e = Load(h.deps, h.proj, run)
			}
			load()
			if err := e.Answer(context.Background(), "answer-"+first); err != nil {
				t.Fatal(err)
			}
			rec = requireState(t, e, runrec.StatePaused)
			if rec.Pending.Role == first {
				t.Fatalf("sibling question was dropped: %+v", rec.Pending)
			}
			second := rec.Pending.Role
			if len(h.runner.requests()) != 3 {
				t.Fatal("roles reran before both questions were answered")
			}
			load()
			if err := e.Answer(context.Background(), "answer-"+second); err != nil {
				t.Fatal(err)
			}
			rec = requireState(t, e, runrec.StateAwaitingApproval)
			for _, q := range rec.Questions {
				if q.Answer != "answer-"+q.Role {
					t.Errorf("misassigned answer: %+v", q)
				}
			}
		})
	}
}

// A locked ledger end holds the stopping round open while its sibling exits
// and cancels reviewCtx. Its retry must keep the ledger operation's context.
func TestStopLedgerEndSurvivesSiblingCancellation(t *testing.T) {
	for _, stopping := range []string{RoleReviewer, RoleUI} {
		t.Run(stopping, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.routesFor = func(_, W string) []clitest.Route {
					return []clitest.Route{
						{Tool: "slicer", Args: []string{"show", item, "--json", "--lean", "--root", W}, Stdout: uiSliceJSON(tuiFiles)},
						{Tool: "slicer", Args: []string{"relay-run", "end", "run-1"}, Prefix: true, Seq: []clitest.Reply{
							{Exit: 3, Stderr: `{"error":{"code":"locked","message":"busy"}}`},
							{Stdout: `{"run":{"id":"run-1","state":"ended"}}`},
						}},
					}
				}
			})
			h.cfg.Lineup[RoleUI] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}}
			siblingCtx := make(chan context.Context, 1)
			siblingStarted := make(chan struct{})
			releaseSibling := make(chan struct{})
			h.deps.Runner = contextRunner{run: func(ctx context.Context, req Request) (Result, error) {
				if req.Role != RoleReviewer && req.Role != RoleUI {
					return defaultResult(req), nil
				}
				if req.Role == stopping {
					<-siblingStarted
					return Result{Exit: ExitAuth}, nil
				}
				siblingCtx <- ctx
				close(siblingStarted)
				<-releaseSibling
				return defaultResult(req), nil
			}}
			e, err := New(context.Background(), h.deps, h.cfg)
			if err != nil {
				t.Fatal(err)
			}
			e.wr.Sleep = func(ctx context.Context, _ time.Duration) error {
				reviewCtx := <-siblingCtx
				close(releaseSibling)
				select {
				case <-reviewCtx.Done():
				case <-time.After(2 * time.Second):
					t.Error("sibling did not cancel review context")
				}
				if err := ctx.Err(); err != nil {
					t.Errorf("stop ledger end context cancelled by sibling: %v", err)
				}
				return ctx.Err()
			}
			if err := e.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			rec := requireState(t, e, runrec.StateStopped)
			if ends := h.calls("slicer", "relay-run", "end"); len(ends) != 2 {
				t.Errorf("ledger end did not complete its retry: %v", ends)
			}
			var last runrec.Write
			for _, w := range rec.Writes {
				if len(w.Argv) > 1 && w.Argv[0] == "relay-run" && w.Argv[1] == "end" {
					last = w
				}
			}
			if last.Exit != 0 || last.Error != "" {
				t.Errorf("ledger end failed: %+v", last)
			}
		})
	}
}

func TestStopClearsQueuedSiblingQuestion(t *testing.T) {
	h := newHarness(t)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{RoleReviewer, RoleUI} {
		if err := e.pause(runrec.Pending{Kind: runrec.PendingQuestion, State: runrec.StateReview, Role: role, Round: 1, Question: "question-" + role}); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.Snapshot().QueuedQuestions) != 1 {
		t.Fatal("sibling question was not queued")
	}
	if err := e.Stop(context.Background(), "stop with unanswered questions"); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateStopped)
	loaded, err := h.store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []runrec.Record{rec, loaded.Snapshot()} {
		if len(r.QueuedQuestions) != 0 || r.Pending != nil {
			t.Fatalf("stopped run kept questions: pending=%+v queue=%+v", r.Pending, r.QueuedQuestions)
		}
	}
}

// The engine hands the registry's ui_doc to the UI pass's brief and to no
// other role's (S136).
func TestUIPassBriefCarriesTheRegistryUIDoc(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.project = func(p *registry.Project) { p.UIDoc = "docs/ui.md" }
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "slicer", Args: []string{"show", item, "--json", "--lean", "--root", W}, Stdout: uiSliceJSON(tuiFiles)}}
		}
	})
	h.cfg.Lineup[RoleUI] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	seen := 0
	for _, req := range h.runner.requests() {
		has := strings.Contains(req.Brief, "The project's UI design doc is `docs/ui.md`.")
		if has != (req.Role == RoleUI) {
			t.Errorf("%s brief names the UI doc: %v", req.Role, has)
		}
		if req.Role == RoleUI {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("the UI pass ran %d times", seen)
	}
}

func TestUIPassBriefCarriesRegistrySkillsForItsVendor(t *testing.T) {
	for _, vendor := range []string{"claude", "codex"} {
		t.Run(vendor, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.project = func(p *registry.Project) { p.UIPassSkills = []string{"tui-design", "frontend-design"} }
				o.routesFor = func(P, W string) []clitest.Route {
					return []clitest.Route{{Tool: "slicer", Args: []string{"show", item, "--json", "--lean", "--root", W}, Stdout: uiSliceJSON(tuiFiles)}}
				}
			})
			h.cfg.Lineup[RoleUI] = []runrec.LineupEntry{{Vendor: vendor, Model: "test", Effort: "medium"}}
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			seen := 0
			for _, req := range h.runner.requests() {
				has := strings.Contains(req.Brief, "## Skills\n\nLoad these skills for the UI pass: tui-design, frontend-design. If one is not available, say so in your report and go on.\n\n## UI design doc")
				if has != (req.Role == RoleUI && vendor == "claude") {
					t.Errorf("%s skills line: %v", req.Role, has)
				}
				if req.Role == RoleUI {
					seen++
				}
			}
			if seen != 1 {
				t.Fatalf("UI pass ran %d times", seen)
			}
		})
	}
}
