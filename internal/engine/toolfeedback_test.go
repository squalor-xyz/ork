// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func nearFeedbackRun(t *testing.T, title string, listExit, noteExit int) (*harness, runrec.Record, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "target")
	rows, _ := json.Marshal([]slicercli.Item{{ID: "S419", Title: near419, Status: "open"}})
	h, e := toolFeedbackRun(t, root, func(o *harnessOpts) {
		fileRoutes("S900")(o)
		o.routes = append(o.routes,
			clitest.Route{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", root}, Stdout: string(rows), Exit: listExit},
			clitest.Route{Tool: "slicer", Args: []string{"note", "S900"}, Prefix: true, Stdout: `{}`, Exit: noteExit},
		)
	}, "tool feedback: 1. reviewer | tool: slicer | kind: friction | proposal: "+title+" | evidence: measured pair")
	return h, requireState(t, e, runrec.StateAwaitingApproval), root
}

func checkNearFeedback(t *testing.T, h *harness, rec runrec.Record, root string, wantNear bool) {
	t.Helper()
	wantList := []string{"list", "--json", "--lean", "--root", root}
	var found bool
	for _, call := range h.calls("slicer", "list") {
		found = found || reflect.DeepEqual(call, wantList)
	}
	if !found {
		t.Error("missing target slicer list call")
	}
	if len(rec.ToolFeedback) != 1 || rec.ToolFeedback[0].Filed != "S900" {
		t.Errorf("filing lost: %+v", rec.ToolFeedback)
	}
	raw, err := os.ReadFile(filepath.Join(h.store.RunDir(rec.ID), "run.json"))
	var disk struct {
		ToolFeedback []map[string]json.RawMessage `json:"tool_feedback"`
	}
	if err != nil || json.Unmarshal(raw, &disk) != nil || len(disk.ToolFeedback) != 1 {
		t.Fatalf("read persisted feedback: %s, %v", raw, err)
	}
	var gotNear []string
	if rawNear, ok := disk.ToolFeedback[0]["near"]; ok {
		if err := json.Unmarshal(rawNear, &gotNear); err != nil {
			t.Fatal(err)
		}
	}
	var want []string
	if wantNear {
		want = []string{"S419"}
	}
	if !reflect.DeepEqual(gotNear, want) {
		t.Errorf("persisted near = %v, want %v", gotNear, want)
	}
	var notes [][]string
	for _, call := range h.calls("slicer", "note") {
		if len(call) > 1 && call[1] == "S900" {
			notes = append(notes, call)
		}
	}
	if wantNear {
		wantNote := []string{"note", "S900", "--text=possible duplicate of S419", "--render", "--json", "--root", root}
		if !reflect.DeepEqual(notes, [][]string{wantNote}) {
			t.Errorf("notes = %q, want %q", notes, wantNote)
		}
	} else if len(notes) != 0 {
		t.Errorf("unexpected notes: %q", notes)
	}
}

func TestToolFeedbackNearDuplicateIsNotedOnTheNewSlice(t *testing.T) {
	h, rec, root := nearFeedbackRun(t, near438, 0, 0)
	checkNearFeedback(t, h, rec, root, true)
}

func TestToolFeedbackWithNoNearDuplicateAddsNoNote(t *testing.T) {
	h, rec, root := nearFeedbackRun(t, "capture dashboard screenshots", 0, 0)
	checkNearFeedback(t, h, rec, root, false)
}

func TestToolFeedbackListFailureStillFiles(t *testing.T) {
	h, rec, root := nearFeedbackRun(t, near438, 3, 0)
	checkNearFeedback(t, h, rec, root, false)
}

func TestToolFeedbackNearDuplicateNoteFailureStillFiles(t *testing.T) {
	h, rec, root := nearFeedbackRun(t, near438, 0, 3)
	checkNearFeedback(t, h, rec, root, true)
}

const (
	tf1 = "tool feedback: 1. reviewer | tool: slicer | kind: friction | proposal: show the Check line in --lean | evidence: ran slicer show S12 --lean twice"
	tf2 = "tool feedback: 2. reviewer | tool: ork | kind: deterministic-check | proposal: reject a report with no Tests line early | evidence: run-20261112 had a retry; the line was a | pipe in the text"
	tf3 = "tool feedback: 3. reviewer | tool: gofmt | kind: feature | proposal: print the diff | evidence: none"
	tf4 = "tool feedback: 4. reviewer | tool: slicer | kind: feature | proposal: a fourth | evidence: dropped"
)

func TestCheckReportParsesToolFeedback(t *testing.T) {
	text := review("nothing", "nothing", "nothing", "nothing") + "\n" + tf1 + "\n" + tf2 + "\n"
	rep, err := CheckReport(RoleReviewer, false, text)
	if err != nil {
		t.Fatalf("tool feedback after Owner needed is a shape error: %v", err)
	}
	want := []ToolFeedback{
		{Role: RoleReviewer, Tool: "slicer", Kind: "friction", Proposal: "show the Check line in --lean", Evidence: "ran slicer show S12 --lean twice"},
		{Role: RoleReviewer, Tool: "ork", Kind: "deterministic-check", Proposal: "reject a report with no Tests line early", Evidence: "run-20261112 had a retry; the line was a | pipe in the text"},
	}
	if !reflect.DeepEqual(rep.ToolFeedback, want) {
		t.Fatalf("tool feedback %+v, want %+v", rep.ToolFeedback, want)
	}
	if rep.Question != "" || rep.Ask != nil {
		t.Fatalf("tool feedback became an owner question: %q", rep.Question)
	}
}

// The role is the one that wrote the report, whatever name the line carries.
func TestCheckReportToolFeedbackRoleIsTheReportingRole(t *testing.T) {
	line := strings.Replace(tf1, "1. reviewer", "1. gate", 1)
	rep, err := CheckReport(RoleReviewer, false, review("nothing", "nothing", "nothing", "nothing")+line+"\n")
	if err != nil || len(rep.ToolFeedback) != 1 || rep.ToolFeedback[0].Role != RoleReviewer {
		t.Fatalf("%+v, %v", rep, err)
	}
}

func TestCheckReportDropsAFourthAndMalformedToolFeedback(t *testing.T) {
	base := review("nothing", "nothing", "nothing", "nothing")
	t.Run("a fourth line", func(t *testing.T) {
		rep, err := CheckReport(RoleReviewer, false, base+tf1+"\n"+tf2+"\n"+tf3+"\n"+tf4+"\n")
		if err != nil || len(rep.ToolFeedback) != 3 || rep.ToolFeedback[2].Tool != "gofmt" {
			t.Fatalf("%+v, %v", rep, err)
		}
	})
	for name, bad := range map[string]string{
		"unknown kind":     "tool feedback: 1. reviewer | tool: slicer | kind: complaint | proposal: p | evidence: e",
		"missing evidence": "tool feedback: 1. reviewer | tool: slicer | kind: friction | proposal: p",
		"fields reordered": "tool feedback: 1. reviewer | kind: friction | tool: slicer | proposal: p | evidence: e",
		"no number":        "tool feedback: reviewer | tool: slicer | kind: friction | proposal: p | evidence: e",
		"blank proposal":   "tool feedback: 1. reviewer | tool: slicer | kind: friction | proposal:    | evidence: e",
		"blank tool":       "tool feedback: 1. reviewer | tool:    | kind: friction | proposal: p | evidence: e",
		"free text":        "tool feedback: the tool is slow",
		"none":             "tool feedback: none",
	} {
		t.Run(name, func(t *testing.T) {
			// The malformed line comes first and between well-formed ones: it is
			// dropped and costs no shape error and no slot.
			rep, err := CheckReport(RoleReviewer, false, base+bad+"\n"+tf1+"\n"+bad+"\n"+tf2+"\n")
			if err != nil {
				t.Fatalf("shape error: %v", err)
			}
			if len(rep.ToolFeedback) != 2 || rep.ToolFeedback[0].Tool != "slicer" || rep.ToolFeedback[1].Tool != "ork" {
				t.Fatalf("%+v", rep.ToolFeedback)
			}
		})
	}
}

// A role may dress the line as a list item or in backticks. It is read the same
// way, and a malformed one is dropped, so it never becomes an owner question.
func TestCheckReportReadsDecoratedToolFeedbackLines(t *testing.T) {
	base := review("nothing", "nothing", "nothing", "nothing")
	for name, dress := range map[string]func(string) string{
		"dash":            func(l string) string { return "- " + l },
		"star":            func(l string) string { return "* " + l },
		"plus":            func(l string) string { return "+ " + l },
		"number":          func(l string) string { return "1. " + l },
		"paren number":    func(l string) string { return "2) " + l },
		"backticks":       func(l string) string { return "`" + l + "`" },
		"dash, backticks": func(l string) string { return "- `" + l + "`" },
		"indented dash":   func(l string) string { return "  - " + l },
	} {
		t.Run(name, func(t *testing.T) {
			rep, err := CheckReport(RoleReviewer, false, base+dress(tf1)+"\n"+dress(tf2)+"\n")
			if err != nil {
				t.Fatalf("shape error: %v", err)
			}
			if rep.Question != "" || rep.Ask != nil {
				t.Fatalf("the line became an owner question: %q", rep.Question)
			}
			if len(rep.ToolFeedback) != 2 || rep.ToolFeedback[0].Tool != "slicer" || rep.ToolFeedback[1].Evidence != "run-20261112 had a retry; the line was a | pipe in the text" {
				t.Fatalf("%+v", rep.ToolFeedback)
			}
			bad := "tool feedback: 1. reviewer | tool: slicer | kind: complaint | proposal: p | evidence: e"
			rep, err = CheckReport(RoleReviewer, false, base+dress(bad)+"\n")
			if err != nil || rep.Question != "" || len(rep.ToolFeedback) != 0 {
				t.Fatalf("a malformed line: %+v, %v", rep, err)
			}
		})
	}
}

func TestCheckReportWithoutToolFeedbackPassesAsBefore(t *testing.T) {
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate} {
		text := review("nothing", "nothing", "nothing", "nothing")
		switch role {
		case RoleImplementer:
			text = implReport
		case RoleGate:
			text = gatePass
		}
		rep, err := CheckReport(role, false, text)
		if err != nil || len(rep.ToolFeedback) != 0 {
			t.Errorf("%s: %+v, %v", role, rep, err)
		}
	}
}

// Tool feedback is not a finding: triage reads the same report with or without
// it, and the gate's verdict and fix text do not change (relay v12).
func TestToolFeedbackNeverChangesAGatesVerdictOrFindings(t *testing.T) {
	for name, plain := range map[string]string{"pass": gatePass, "fail": gateFail} {
		t.Run(name, func(t *testing.T) {
			withFeedback := strings.Replace(plain, "- ", tf1+"\n- ", 1) + tf2 + "\n" + tf3 + "\n" + tf4 + "\n" + "tool feedback: garbled\n"
			a, errA := CheckReport(RoleGate, false, plain)
			b, errB := CheckReport(RoleGate, false, withFeedback)
			if errA != nil || errB != nil || a.Verdict != b.Verdict || a.Passed != b.Passed || a.Failed != b.Failed || b.Question != "" {
				t.Fatalf("verdict: %+v %v vs %+v %v", a, errA, b, errB)
			}
			if len(b.ToolFeedback) != 3 {
				t.Fatalf("feedback %+v", b.ToolFeedback)
			}
			ta, errA := TriageGate(plain)
			tb, errB := TriageGate(withFeedback)
			if errA != nil || errB != nil {
				t.Fatalf("%v %v", errA, errB)
			}
			if !reflect.DeepEqual(ta, tb) || strings.Contains(tb.Fix, "tool feedback") {
				t.Fatalf("triage differs:\n%+v\n%+v", ta, tb)
			}
		})
	}
}

func TestToolFeedbackIsNotAReviewFinding(t *testing.T) {
	plain := findingsReview("engine.go:10 drops the error.")
	// The line sits in section (D) and after the Owner needed section.
	withFeedback := strings.Replace(plain, "\n## Owner needed", "\n"+tf1+"\n\n## Owner needed", 1) + tf2 + "\n"
	a, errA := Triage(plain)
	b, errB := Triage(withFeedback)
	if errA != nil || errB != nil || !reflect.DeepEqual(a, b) || strings.Contains(b.Fix, "tool feedback") {
		t.Fatalf("%+v %v vs %+v %v", a, errA, b, errB)
	}
	fa, _ := Findings(plain)
	fb, _ := Findings(withFeedback)
	if fa != fb {
		t.Fatalf("findings differ: %q vs %q", fa, fb)
	}
}

func TestEveryRoleMayEndItsReportWithToolFeedback(t *testing.T) {
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate, RoleArchitect, RoleUI} {
		var text string
		switch role {
		case RoleImplementer:
			text = implReport
		case RoleGate:
			text = gatePass
		case RoleArchitect:
			text = "Shape is fine.\n" + ownerNone
		default:
			text = cleanReview
		}
		rep, err := CheckReport(role, false, text+"\n"+tf1+"\n")
		if err != nil || len(rep.ToolFeedback) != 1 || rep.ToolFeedback[0].Role != role {
			t.Errorf("%s: %+v, %v", role, rep, err)
		}
	}
	rep, err := CheckReport(RoleImplementer, true, finishReport+tf1+"\n")
	if err != nil || len(rep.ToolFeedback) != 1 {
		t.Errorf("finish step: %+v, %v", rep, err)
	}
}

// Roles report 5 tool feedback lines in all; the record keeps the first 3, in
// report order, each with its role. The run goes on as before.
func TestRunKeepsAtMostThreeToolFeedbackEntries(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		switch req.Role {
		case RoleImplementer:
			return Result{Exit: ExitOK, Session: "sess-impl", Report: implReport + strings.Replace(tf1, "reviewer", "implementer", 1) + "\n" + strings.Replace(tf2, "reviewer", "implementer", 1) + "\n"}, nil
		case RoleReviewer:
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: cleanReview + tf3 + "\n" + tf4 + "\n"}, nil
		case RoleGate:
			return Result{Exit: ExitOK, Session: "sess-gate", Report: gatePass + "tool feedback: 1. gate | tool: slicer | kind: feature | proposal: fifth | evidence: e\n"}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	var got []string
	for _, f := range rec.ToolFeedback {
		got = append(got, f.Role+"/"+f.Tool+"/"+f.Kind)
	}
	if want := []string{"implementer/slicer/friction", "implementer/ork/deterministic-check", "reviewer/gofmt/feature"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded tool feedback %v, want %v", got, want)
	}
	if rec.ToolFeedback[0].Proposal != "show the Check line in --lean" || rec.ToolFeedback[0].Evidence == "" {
		t.Fatalf("entry %+v", rec.ToolFeedback[0])
	}
	if len(rec.Findings) != 0 || len(rec.Questions) != 0 {
		t.Fatalf("tool feedback became findings %+v or questions %+v", rec.Findings, rec.Questions)
	}
}

// A gate that passes with tool feedback is still a pass: the ledger step closes
// with the verdict and nothing about the feedback reaches slicer.
func TestAGatesToolFeedbackDoesNotChangeTheRun(t *testing.T) {
	run := func(fail bool, feedback string) (runrec.Record, string) {
		h := newHarness(t)
		h.runner.script = func(req Request, n int) (Result, error) {
			if req.Role == RoleGate {
				report := gatePass
				if fail && req.Round == 1 {
					report = gateFail
				}
				return Result{Exit: ExitOK, Session: "sess-gate", Report: report + feedback}, nil
			}
			return Result{}, nil
		}
		e := h.start()
		var verdicts []string
		for _, c := range h.calls("slicer", "relay-run", "step") {
			s := join(c)
			if i := strings.Index(s, "--verdict "); i >= 0 {
				verdicts = append(verdicts, strings.Fields(s[i:])[1])
			}
			if strings.Contains(s, "feedback") {
				t.Errorf("tool feedback reached slicer: %s", s)
			}
		}
		return e.Snapshot(), strings.Join(verdicts, ",") + " " + strings.Join(h.runner.roles(), ",")
	}
	for _, fail := range []bool{false, true} {
		plainRec, plain := run(fail, "")
		fbRec, with := run(fail, tf1+"\n"+tf2+"\n")
		if plain != with || plainRec.State != fbRec.State || plainRec.Round != fbRec.Round || len(plainRec.Findings) != len(fbRec.Findings) {
			t.Errorf("fail=%v: run differs\n plain %s %s/%d\n with  %s %s/%d", fail, plain, plainRec.State, plainRec.Round, with, fbRec.State, fbRec.Round)
		}
		// The failing gate runs twice with the same 2 lines; the repeat has the
		// same keys (S387, adj 414), so the record keeps 2 either way.
		if len(fbRec.ToolFeedback) != 2 {
			t.Errorf("fail=%v: recorded %d entries, want 2: %+v", fail, len(fbRec.ToolFeedback), fbRec.ToolFeedback)
		}
	}
}

// No later brief carries a role's tool feedback: not the fix round after a
// reviewer's untagged or tagged finding, not the fix round after a failed gate,
// not the earlier reports a later role reads, and not the note on the item.
func TestToolFeedbackIsNotSentBackToARole(t *testing.T) {
	const marker = "show the Check line in --lean" // the proposal of tf1
	for name, tc := range map[string]struct {
		role   string
		report string
		want   string
	}{
		"untagged reviewer finding": {RoleReviewer, findingsReview("engine.go:10 drops the error.") + tf1 + "\n", "engine.go:10 drops the error."},
		"tagged reviewer finding":   {RoleReviewer, review(fixFinding, "nothing", "nothing", "nothing") + tf1 + "\n", "engine.go:42 drops the error."},
		// The line sits near the top so that a summary of the report would hold it.
		"failed gate": {RoleGate, strings.Replace(gateFail, "\n\n", "\n"+tf1+"\n\n", 1), "engine.go:10 missing nil check"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.runner.script = func(req Request, n int) (Result, error) {
				if req.Role == tc.role && req.Round == 1 {
					return Result{Exit: ExitOK, Session: "sess-" + req.Role, Report: tc.report}, nil
				}
				return Result{}, nil
			}
			h.start()
			var fix int
			for _, r := range h.runner.requests() {
				if r.Role == RoleImplementer && r.Round > 1 {
					fix++
					if !strings.Contains(r.Brief, tc.want) || strings.Contains(r.Brief, marker) {
						t.Errorf("fix brief:\n%s", r.Brief)
					}
				}
			}
			if fix == 0 {
				t.Fatal("no fix round ran")
			}
			for _, c := range h.calls("slicer", "note") {
				if strings.Contains(join(c), marker) {
					t.Errorf("tool feedback reached a slicer note: %s", join(c))
				}
			}
		})
	}
}

func TestEarlierReportsDoNotCarryToolFeedback(t *testing.T) {
	const marker = "show the Check line in --lean"
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && req.Round == 1 {
			return Result{Exit: ExitOK, Session: "sess-impl", Report: implReport + strings.Replace(tf1, "reviewer", "implementer", 1) + "\n"}, nil
		}
		return Result{}, nil
	}
	h.start()
	var saw bool
	for _, r := range h.runner.requests() {
		if r.Role == RoleReviewer {
			saw = true
			if !strings.Contains(r.Brief, "Earlier reports") || !strings.Contains(r.Brief, "Tests: 12 passed") || strings.Contains(r.Brief, marker) {
				t.Errorf("reviewer brief:\n%s", r.Brief)
			}
		}
	}
	if !saw {
		t.Fatal("no reviewer round")
	}
}

// A spec check has no run to record tool feedback in, but its lines are no
// part of a slice's verdict or of the Owner needed text ork shows.
func TestSpecCheckIgnoresToolFeedbackLines(t *testing.T) {
	line := strings.Replace(tf1, "reviewer", "spec-check", 1)
	report := "## Slice S374\nReady\n" + line + "\n\n## Slice S375\nReady\n\n## Owner needed\n\nnone\n" + line + "\n"
	verdicts, err := ParseSpecCheck(report, []string{"S374", "S375"})
	if err != nil {
		t.Fatalf("error %v", err)
	}
	if len(verdicts) != 2 || !verdicts[0].Ready || !verdicts[1].Ready {
		t.Fatalf("verdicts %+v", verdicts)
	}
	owner, _ := section(report, ownerHeading)
	if got := SpecOwnerText(owner); got != "" {
		t.Fatalf("owner text %q", got)
	}
}

// The Owner needed text a spec check shows leaves the tool feedback out; the
// saved report keeps the line.
func TestSpecCheckOwnerTextLeavesOutToolFeedback(t *testing.T) {
	line := strings.Replace(tf1, "reviewer", "spec-check", 1)
	h := specHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		report := strings.Replace(specReport, "\nnone\n", "\nConfirm the rollout window with the owner.\n"+line+"\n", 1)
		return Result{Exit: ExitOK, Report: report}, nil
	}
	cfg := specConfig(h, t)
	res, err := RunSpecCheck(context.Background(), h.deps, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.OwnerNeeded != "Confirm the rollout window with the owner." {
		t.Fatalf("owner text %q", res.OwnerNeeded)
	}
	saved, err := os.ReadFile(filepath.Join(cfg.Dir, "spec-check.md"))
	if err != nil || !strings.Contains(string(saved), line) {
		t.Fatalf("saved report %q, %v", saved, err)
	}
}

const specTFLine = "tool feedback: 1. spec-check | tool: ork | kind: friction | proposal: Name the item in the check outline | evidence: had to read the brief twice"

// specFilingCheck runs a spec check of items whose report ends with lines,
// with `slicer add` answering add. It returns the harness, the result and the
// check's directory.
func specFilingCheck(t *testing.T, add clitest.Route, items []string, report string, lines ...string) (*harness, *SpecCheckResult, string, error) {
	t.Helper()
	add.Tool, add.Args, add.Prefix = "slicer", []string{"add"}, true
	h := specHarnessRoutes(t, []clitest.Route{
		add,
		{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{"id":"S130"}`},
		{Tool: "slicer", Args: []string{"list"}, Prefix: true, Stdout: `[]`},
	}, nil)
	h.deps.Project = registeredProjects(h.proj)
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role != RoleSpecCheck {
			return Result{}, nil
		}
		return Result{Exit: ExitOK, Session: "sess", Report: report + strings.Join(lines, "\n") + "\n"}, nil
	}
	cfg := specConfig(h, t)
	cfg.Items = items
	res, err := RunSpecCheck(context.Background(), h.deps, cfg)
	return h, res, cfg.Dir, err
}

const (
	specOneReport = "## Slice S10\nReady\n\n## Owner needed\n\nnone\n"
	specAddNew    = `{"id":"S130","title":"x","status":"open","existing":false}`
)

// A spec check has no run record, so its tool feedback is filed and not kept
// (S389, *proposed*; adj 417): the verdicts are the ones the report gives
// without the line.
func TestSpecCheckFilesItsToolFeedback(t *testing.T) {
	_, base, _, err := specFilingCheck(t, clitest.Route{Stdout: specAddNew}, []string{"S10"}, specOneReport)
	if err != nil {
		t.Fatal(err)
	}
	h, res, dir, err := specFilingCheck(t, clitest.Route{Stdout: specAddNew}, []string{"S10"}, specOneReport, specTFLine)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Verdicts, base.Verdicts) || len(res.Verdicts) != 1 || !res.Verdicts[0].Ready {
		t.Fatalf("verdicts %+v, want %+v", res.Verdicts, base.Verdicts)
	}
	key := ToolFeedbackKey("ork", "friction", "Name the item in the check outline")
	want := []string{"add", "Name the item in the check outline", "--discovered-from", "S10", "--key", key, "--render", "--json", "--lean", "--root", h.P}
	adds := h.calls("slicer", "add")
	if len(adds) != 1 || !reflect.DeepEqual(adds[0], want) {
		t.Fatalf("slicer add calls %q, want %q", adds, want)
	}
	promotes := h.calls("slicer", "promote")
	if len(promotes) != 1 || promotes[0][1] != "S130" {
		t.Fatalf("slicer promote calls %q", promotes)
	}
	outline, err := os.ReadFile(promotes[0][3])
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(promotes[0][3]) != dir {
		t.Errorf("the outline is written in the check's directory, not %s", promotes[0][3])
	}
	for _, want := range []string{"### Why", "spec-check", "spec check of S10", "had to read the brief twice"} {
		if !strings.Contains(string(outline), want) {
			t.Errorf("outline lacks %q:\n%s", want, outline)
		}
	}
	if strings.Contains(string(outline), "ork run") {
		t.Errorf("a spec check has no run:\n%s", outline)
	}
}

// With more than one item the entry has no item to be discovered from, and the
// outline names all of them.
func TestSpecCheckToolFeedbackOfSeveralItemsHasNoDiscoveredFrom(t *testing.T) {
	h, _, _, err := specFilingCheck(t, clitest.Route{Stdout: specAddNew}, []string{"S10", "S11"}, specReport, specTFLine)
	if err != nil {
		t.Fatal(err)
	}
	adds := h.calls("slicer", "add")
	if len(adds) != 1 || slices.Contains(adds[0], "--discovered-from") {
		t.Fatalf("slicer add calls %q", adds)
	}
	promotes := h.calls("slicer", "promote")
	if len(promotes) != 1 {
		t.Fatalf("slicer promote calls %q", promotes)
	}
	if outline, _ := os.ReadFile(promotes[0][3]); !strings.Contains(string(outline), "spec check of S10, S11") {
		t.Errorf("outline:\n%s", outline)
	}
}

// The key is the same on every check, and an item the key already names that
// has its spec is not promoted again, so a retried check files nothing new.
func TestSpecCheckToolFeedbackRepeatFilesNothingNew(t *testing.T) {
	existing := clitest.Route{Stdout: `{"id":"S130","title":"x","status":"open","existing":true,"has_slice":true}`}
	var keys []string
	for range 2 {
		h, _, _, err := specFilingCheck(t, existing, []string{"S10"}, specOneReport, specTFLine)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(h.calls("slicer", "promote")); n != 0 {
			t.Errorf("promote ran %d times", n)
		}
		adds := h.calls("slicer", "add")
		if len(adds) != 1 {
			t.Fatalf("slicer add calls %q", adds)
		}
		keys = append(keys, flagValues(adds[0], "--key")...)
	}
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Errorf("keys %q", keys)
	}
}

// Tool feedback is advisory: a filing that fails leaves the verdicts as they
// were and is no error of the check.
func TestSpecCheckToolFeedbackFailureKeepsTheVerdict(t *testing.T) {
	_, base, _, err := specFilingCheck(t, clitest.Route{Stdout: specAddNew}, []string{"S10"}, specOneReport)
	if err != nil {
		t.Fatal(err)
	}
	h, res, _, err := specFilingCheck(t, clitest.Route{Exit: 3, Stderr: "slicer: broken"}, []string{"S10"}, specOneReport, specTFLine)
	if err != nil {
		t.Fatalf("error %v", err)
	}
	if n := len(h.calls("slicer", "add")); n != 1 {
		t.Fatalf("slicer add ran %d times, want 1", n)
	}
	if !reflect.DeepEqual(res.Verdicts, base.Verdicts) {
		t.Fatalf("verdicts %+v, want %+v", res.Verdicts, base.Verdicts)
	}
}

// registeredProjects answers Deps.Project from a list, by name without regard
// to case, as the daemon does.
func registeredProjects(ps ...registry.Project) func(string) (registry.Project, bool) {
	return func(name string) (registry.Project, bool) {
		for _, p := range ps {
			if strings.EqualFold(p.Name, name) {
				return p, true
			}
		}
		return registry.Project{}, false
	}
}

// The key is stable (S387, proposed; adj 414 feedback.dedupe): the tool and the
// proposal are normalized, the kind is part of it.
func TestToolFeedbackKeyIsStableAndNormalized(t *testing.T) {
	key := regexp.MustCompile(`^tool-feedback-slicer-[0-9a-f]{8}$`)
	a := ToolFeedbackKey("Slicer", "friction", "Add  a --json flag")
	b := ToolFeedbackKey("slicer", "friction", "add a --json flag")
	if !key.MatchString(a) || a != b {
		t.Fatalf("keys %q and %q, want one tool-feedback-slicer-XXXXXXXX", a, b)
	}
	if c := ToolFeedbackKey("slicer", "feature", "add a --json flag"); c == a || !key.MatchString(c) {
		t.Errorf("another kind gives another key: %q vs %q", c, a)
	}
	if c := ToolFeedbackKey("slicer", "friction", "add a --yaml flag"); c == a {
		t.Errorf("another proposal gives another key: %q", c)
	}
	if c := ToolFeedbackKey("my tool", "friction", "p"); !strings.HasPrefix(c, "tool-feedback-my-tool-") {
		t.Errorf("tool `my tool` gives %q, want slug my-tool", c)
	}
	for tool, want := range map[string]string{"Some_CLI v2": "some-cli-v2", "--x--": "x", "!!!": "tool", "Ünï": "n", "go-vet": "go-vet"} {
		if got := ToolFeedbackSlug(tool); got != want {
			t.Errorf("slug of %q = %q, want %q", tool, got, want)
		}
	}
}

// The target is the registered project the tool names, else the run's own
// project with the tool in the title (adj 412 feedback.target).
func TestToolFeedbackTargetIsTheRegisteredToolElseTheRunsProject(t *testing.T) {
	ork := registry.Project{Name: "ork", Path: "/p/ork"}
	slicer := registry.Project{Name: "slicer", Path: "/p/slicer"}
	lookup := registeredProjects(ork, slicer)
	fb := func(tool string) ToolFeedback {
		return ToolFeedback{Role: "reviewer", Tool: tool, Kind: "friction", Proposal: "PROPOSAL", Evidence: "e"}
	}
	for name, tc := range map[string]struct {
		tool, project, title, from string
		lookup                     func(string) (registry.Project, bool)
	}{
		"registered tool":          {"slicer", "slicer", "PROPOSAL", "", lookup},
		"registered, other case":   {"Slicer", "slicer", "PROPOSAL", "", lookup},
		"unregistered tool":        {"some-cli", "ork", "some-cli: PROPOSAL", "S04", lookup},
		"the run's own project":    {"ork", "ork", "PROPOSAL", "S04", lookup},
		"own project, other case":  {"ORK", "ork", "PROPOSAL", "S04", lookup},
		"no registry resolves any": {"slicer", "ork", "slicer: PROPOSAL", "S04", nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := ToolFeedbackTarget(ork, "S04", tc.lookup, fb(tc.tool))
			if got.Project.Name != tc.project || got.Title != tc.title || got.DiscoveredFrom != tc.from {
				t.Errorf("target %+v, want project %s, title %q, discovered-from %q", got, tc.project, tc.title, tc.from)
			}
		})
	}
}

// toolFeedbackRun runs a slice to its approval card with the reviewer's
// reports lines at the end of its first report, ork and slicer registered.
func toolFeedbackRun(t *testing.T, slicerPath string, opts func(*harnessOpts), lines ...string) (*harness, *Engine) {
	t.Helper()
	h := newHarness(t, opts)
	h.deps.Project = registeredProjects(h.proj, registry.Project{Name: "slicer", Path: slicerPath})
	reviewerSays(h, cleanReview+strings.Join(lines, "\n")+"\n")
	return h, h.start()
}

const (
	tfFriction = "tool feedback: 1. reviewer | tool: slicer | kind: friction | proposal: Print the run id on start | evidence: had to run ork status"
	tfFeature  = "tool feedback: 1. reviewer | tool: slicer | kind: feature | proposal: Print the run id on start | evidence: had to run ork status"
)

func TestToolFeedbackFrictionIsFiledInTheToolsProject(t *testing.T) {
	slicerPath := filepath.Join(t.TempDir(), "slicer")
	h, e := toolFeedbackRun(t, slicerPath, func(o *harnessOpts) {
		fileRoutes("S130")(o)
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", slicerPath}, Stdout: `[]`})
	}, tfFriction)
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	adds := h.calls("slicer", "add")
	key := ToolFeedbackKey("slicer", "friction", "Print the run id on start")
	want := []string{"add", "Print the run id on start", "--key", key, "--render", "--json", "--lean", "--root", slicerPath}
	if len(adds) != 1 || !reflect.DeepEqual(adds[0], want) {
		t.Fatalf("slicer add calls %q, want %q", adds, want)
	}
	promotes := h.calls("slicer", "promote")
	if len(promotes) != 1 || len(promotes[0]) != 9 || promotes[0][1] != "S130" || promotes[0][2] != "--file" || promotes[0][8] != slicerPath {
		t.Fatalf("slicer promote calls %q", promotes)
	}
	outline, err := os.ReadFile(promotes[0][3])
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(promotes[0][3]) != h.store.RunDir(rec.ID) {
		t.Errorf("the outline is written in the run directory, not %s", promotes[0][3])
	}
	for _, want := range []string{"## Print the run id on start", "### Why", "reviewer", rec.ID, "ork", item, "had to run ork status"} {
		if !strings.Contains(string(outline), want) {
			t.Errorf("outline lacks %q:\n%s", want, outline)
		}
	}
	if len(rec.ToolFeedback) != 1 {
		t.Fatalf("entries %+v", rec.ToolFeedback)
	}
	if f := rec.ToolFeedback[0]; f.Key != key || f.Filed != "S130" || f.FiledAt.IsZero() || f.FileError != "" {
		t.Errorf("entry %+v", f)
	}
	var logged int
	for _, w := range rec.Writes {
		if w.Tool == "slicer" && (w.Argv[0] == "add" || w.Argv[0] == "promote") && w.Root == slicerPath {
			logged++
		}
	}
	if logged != 2 {
		t.Errorf("writes[] holds %d filing writes in the tool's project, want 2: %+v", logged, rec.Writes)
	}
}

func TestToolFeedbackUnregisteredToolIsFiledInTheRunsProject(t *testing.T) {
	line := "tool feedback: 1. reviewer | tool: some-cli | kind: deterministic-check | proposal: Check the flag order | evidence: it failed twice"
	h, e := toolFeedbackRun(t, "/projects/slicer", fileRoutes("S131"), line)
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	adds := h.calls("slicer", "add")
	key := ToolFeedbackKey("some-cli", "deterministic-check", "Check the flag order")
	want := []string{"add", "some-cli: Check the flag order", "--discovered-from", item, "--key", key, "--render", "--json", "--lean", "--root", h.P}
	if len(adds) != 1 || !reflect.DeepEqual(adds[0], want) {
		t.Fatalf("slicer add calls %q, want %q", adds, want)
	}
	if n := len(h.calls("slicer", "promote")); n != 1 {
		t.Errorf("promote ran %d times", n)
	}
	if f := rec.ToolFeedback[0]; f.Filed != "S131" || f.Key != key {
		t.Errorf("entry %+v", f)
	}
}

// A feature entry is never filed as a slice by the run (S388: it is asked in
// adj, and filed only when the owner answers). Here its target has no adj, so
// the entry only says why nothing was asked.
func TestToolFeedbackFeatureIsNotFiledAsASlice(t *testing.T) {
	h, e := toolFeedbackRun(t, "/projects/slicer", fileRoutes("S130"), tfFeature)
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if n := len(h.calls("slicer", "add")) + len(h.calls("slicer", "promote")); n != 0 {
		t.Fatalf("a feature entry is not filed; %d filing calls", n)
	}
	if len(rec.ToolFeedback) != 1 {
		t.Fatalf("entries %+v", rec.ToolFeedback)
	}
	if f := rec.ToolFeedback[0]; f.Key != ToolFeedbackKey("slicer", "feature", "Print the run id on start") || f.Filed != "" || f.Asked != 0 {
		t.Errorf("entry %+v", f)
	}
	raw, err := os.ReadFile(filepath.Join(h.store.RunDir(rec.ID), "run.json"))
	var disk struct {
		ToolFeedback []struct {
			Key   string `json:"key"`
			Filed string `json:"filed"`
		} `json:"tool_feedback"`
	}
	if err != nil || json.Unmarshal(raw, &disk) != nil || len(disk.ToolFeedback) != 1 || !strings.HasPrefix(disk.ToolFeedback[0].Key, "tool-feedback-slicer-") || disk.ToolFeedback[0].Filed != "" {
		t.Errorf("run.json tool_feedback %+v (%v), want the entry with its key and no filed", disk.ToolFeedback, err)
	}
}

const tfOrkFeature = "tool feedback: 1. reviewer | tool: ork | kind: feature | proposal: Show the run id | evidence: had to run ork status"

// askedKey is the adj key of an ork feature entry: the filing key with its
// first "-" after "tool-feedback" as ".".
func askedKey(proposal string) string {
	return strings.Replace(ToolFeedbackKey("ork", "feature", proposal), "tool-feedback-", "tool-feedback.", 1)
}

// adjAskRun runs a slice of ork, which has adj, to its approval card with the
// reviewer's lines; adj check answers check, and adj ask answers 12.
func adjAskRun(t *testing.T, adj bool, check string, lines ...string) (*harness, *Engine) {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.project = func(p *registry.Project) { p.Adj = adj }
		o.routes = append(o.routes,
			clitest.Route{Tool: "adj", Args: []string{"contract", "--json"}, Prefix: true, Stdout: `{"contract":"adj-json v1","version":1,"pinned":null}`},
			clitest.Route{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: check},
			clitest.Route{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "12\n"},
		)
	})
	h.deps.Adj = adjcli.Client{Bin: h.fake.Bin("adj")}
	reviewerSays(h, cleanReview+strings.Join(lines, "\n")+"\n")
	return h, h.start()
}

// flagValues returns every value that follows flag in args.
func flagValues(args []string, flag string) []string {
	var out []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			out = append(out, args[i+1])
		}
	}
	return out
}

func TestToolFeedbackFeatureIsAskedInAdj(t *testing.T) {
	h, e := adjAskRun(t, true, `{}`, tfOrkFeature)
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	key := askedKey("Show the run id")
	if !regexp.MustCompile(`^tool-feedback\.ork-[0-9a-f]{8}$`).MatchString(key) {
		t.Fatalf("key %q", key)
	}
	var verbs []string
	for _, c := range h.fake.CallsTo("adj") {
		if c.Args[0] != "contract" {
			verbs = append(verbs, c.Args[0])
		}
	}
	if !reflect.DeepEqual(verbs, []string{"check", "ask"}) {
		t.Fatalf("adj calls %v, want check then ask", verbs)
	}
	check, ask := h.calls("adj", "check")[0], h.calls("adj", "ask")[0]
	if check[1] != key {
		t.Errorf("adj check %q, want key %s", check, key)
	}
	if ask[1] != "Tool feedback (feature, ork): Show the run id" || !reflect.DeepEqual(flagValues(ask, "--key"), []string{key}) {
		t.Errorf("adj ask %q", ask)
	}
	wantOptions := []string{"file:ork files it as a slice in ork", "drop:not acted on"}
	if got := flagValues(ask, "--option"); !reflect.DeepEqual(got, wantOptions) {
		t.Errorf("options %q, want %q", got, wantOptions)
	}
	if got := flagValues(ask, "--recommend"); !reflect.DeepEqual(got, []string{"file"}) {
		t.Errorf("recommend %q", got)
	}
	if got := flagValues(ask, "--reasoning"); !reflect.DeepEqual(got, []string{"A role proposed it; code cannot judge its cost."}) {
		t.Errorf("reasoning %q", got)
	}
	if got := flagValues(ask, "--ref"); !reflect.DeepEqual(got, []string{item}) {
		t.Errorf("refs %q, want the run's item", got)
	}
	ctx := flagValues(ask, "--context")
	if len(ctx) != 1 || !strings.HasPrefix(ctx[0], "Proposal: Show the run id\n") {
		t.Fatalf("context %q", ctx)
	}
	for _, want := range []string{"reviewer", rec.ID, "ork", item, "had to run ork status"} {
		if !strings.Contains(ctx[0], want) {
			t.Errorf("context lacks %q:\n%s", want, ctx[0])
		}
	}
	if n := len(h.calls("slicer", "add")) + len(h.calls("slicer", "promote")); n != 0 {
		t.Errorf("a feature entry is asked, not filed: %d filing calls", n)
	}
	if len(rec.ToolFeedback) != 1 {
		t.Fatalf("entries %+v", rec.ToolFeedback)
	}
	if f := rec.ToolFeedback[0]; f.Asked != 12 || f.Filed != "" || f.FileError != "" || f.Key != ToolFeedbackKey("ork", "feature", "Show the run id") {
		t.Errorf("entry %+v, want asked 12 and the filing key", f)
	}
}

func TestToolFeedbackAskedTwiceIsNotReasked(t *testing.T) {
	key := askedKey("Show the run id")
	h, e := adjAskRun(t, true, `{"open":[{"id":12,"key":"`+key+`"}]}`, tfOrkFeature)
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if n := len(h.calls("adj", "ask")); n != 0 {
		t.Fatalf("adj ask ran %d times for a key that is open", n)
	}
	if len(rec.ToolFeedback) != 1 {
		t.Fatalf("entries %+v", rec.ToolFeedback)
	}
	if f := rec.ToolFeedback[0]; f.Asked != 0 || !strings.Contains(f.FileError, "#12") {
		t.Errorf("entry %+v, want file_error naming #12", f)
	}
}

func TestToolFeedbackAskSkipsAProjectWithoutAdj(t *testing.T) {
	h, e := adjAskRun(t, false, `{}`, tfOrkFeature)
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if calls := h.fake.CallsTo("adj"); len(calls) != 0 {
		t.Fatalf("adj was called in a project without adj: %q", calls)
	}
	if f := rec.ToolFeedback[0]; f.Asked != 0 || !strings.Contains(f.FileError, "adj") {
		t.Errorf("entry %+v, want a file_error that says the project has no adj", f)
	}
}

// A filing that fails is advisory: the entry says why, and the run goes on as
// it does without the line.
func TestToolFeedbackFilingFailureDoesNotStopTheRun(t *testing.T) {
	failing := func(o *harnessOpts) {
		o.routes = append(o.routes, clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Exit: 3, Stderr: "slicer: broken"})
	}
	plainH := newHarness(t)
	reviewerSays(plainH, cleanReview)
	plain := requireState(t, plainH.start(), runrec.StateAwaitingApproval)

	h, e := toolFeedbackRun(t, "/projects/slicer", failing, tfFriction)
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	if rec.Pending != nil || rec.Round != plain.Round || !reflect.DeepEqual(h.runner.roles(), plainH.runner.roles()) {
		t.Errorf("the run differs: pending %+v, round %d (plain %d), roles %v (plain %v)", rec.Pending, rec.Round, plain.Round, h.runner.roles(), plainH.runner.roles())
	}
	if len(rec.ToolFeedback) != 1 {
		t.Fatalf("entries %+v", rec.ToolFeedback)
	}
	if f := rec.ToolFeedback[0]; f.FileError == "" || !strings.Contains(f.FileError, "slicer add") || f.Filed != "" || f.Key == "" {
		t.Errorf("entry %+v", f)
	}
	if n := len(h.calls("slicer", "promote")); n != 0 {
		t.Errorf("promote ran after a failed add")
	}
}

// The same proposal in a second run carries the same key; slicer returns the
// item it filed before, already specified, so nothing is promoted again.
func TestToolFeedbackRepeatAcrossRunsReturnsTheSameItem(t *testing.T) {
	const slicerPath = "/projects/slicer"
	h1, e1 := toolFeedbackRun(t, slicerPath, fileRoutes("S130"), tfFriction)
	requireState(t, e1, runrec.StateAwaitingApproval)
	again := func(o *harnessOpts) {
		o.routes = append(o.routes,
			clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"id":"S130","existing":true,"has_slice":true}`},
			clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{"id":"S130"}`},
			clitest.Route{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", slicerPath}, Stdout: `[{"id":"S419","title":"Print the run id on start","status":"open"}]`},
			clitest.Route{Tool: "slicer", Args: []string{"note", "S130"}, Prefix: true, Stdout: `{}`},
		)
	}
	h2, e2 := toolFeedbackRun(t, slicerPath, again, tfFriction)
	rec := requireState(t, e2, runrec.StateAwaitingApproval)
	first, second := h1.calls("slicer", "add"), h2.calls("slicer", "add")
	if len(first) != 1 || len(second) != 1 || !reflect.DeepEqual(first[0], second[0]) {
		t.Fatalf("the runs send different filings:\n %q\n %q", first, second)
	}
	if n := len(h2.calls("slicer", "promote")); n != 0 {
		t.Errorf("promote ran %d times for an item that has its spec", n)
	}
	if calls := h2.calls("slicer", "list"); len(calls) != 0 {
		t.Errorf("exact-key repeat listed near duplicates: %q", calls)
	}
	if calls := h2.calls("slicer", "note"); len(calls) != 0 {
		t.Errorf("exact-key repeat added a duplicate note: %q", calls)
	}
	if f := rec.ToolFeedback[0]; f.Filed != "S130" {
		t.Errorf("entry %+v", f)
	}
}

// A line a role repeats in one run is kept and filed once.
func TestToolFeedbackRepeatInOneRunIsFiledOnce(t *testing.T) {
	h := newHarness(t, fileRoutes("S130"))
	h.deps.Project = registeredProjects(h.proj, registry.Project{Name: "slicer", Path: "/projects/slicer"})
	h.runner.script = func(req Request, n int) (Result, error) {
		switch req.Role {
		case RoleImplementer:
			return Result{Exit: ExitOK, Session: "sess-impl", Report: implReport + strings.Replace(tfFriction, "reviewer", "implementer", 1) + "\n"}, nil
		case RoleReviewer:
			return Result{Exit: ExitOK, Session: "sess-reviewer", Report: cleanReview + tfFriction + "\n"}, nil
		}
		return Result{}, nil
	}
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	if len(rec.ToolFeedback) != 1 || rec.ToolFeedback[0].Role != "implementer" {
		t.Errorf("entries %+v", rec.ToolFeedback)
	}
	if n := len(h.calls("slicer", "add")); n != 1 {
		t.Errorf("slicer add ran %d times", n)
	}
}

// A check log is not a role report: a line in it that starts with "tool
// feedback:" stays in the fix round's brief.
func TestChecksFailedBriefKeepsTheLogsToolFeedbackLine(t *testing.T) {
	h := newHarness(t)
	red := CheckOutput{Output: "--- FAIL: TestX\nFAIL\ntool feedback: the log line stays\n"}
	h.checks.outputs = []CheckOutput{red, red, {OK: true, Output: "ok\n"}}
	h.start()
	var fix int
	for _, r := range h.runner.requests() {
		if r.Role == RoleImplementer && r.Round > 1 {
			fix++
			if !strings.Contains(r.Brief, "tool feedback: the log line stays") {
				t.Errorf("checks brief lost the log line:\n%s", r.Brief)
			}
		}
	}
	if fix == 0 {
		t.Fatal("no fix round ran")
	}
}
