// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

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
		// The failing gate runs twice with the same 2 lines; the record keeps 3.
		if want := map[bool]int{false: 2, true: 3}[fail]; len(fbRec.ToolFeedback) != want {
			t.Errorf("fail=%v: recorded %+v", fail, fbRec.ToolFeedback)
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
