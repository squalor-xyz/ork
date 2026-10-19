// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestApproveCommitsWithTheSlicesGitTitleThenRunsTheFinishStep(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	before := len(h.runner.requests())
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateCommitted)

	if len(rec.Approvals) != 1 || rec.Approvals[0].What != "slice" || rec.Approvals[0].At.IsZero() {
		t.Errorf("approvals: %+v", rec.Approvals)
	}
	if rec.Commit == nil || rec.Commit.SHA != "0123abc" || rec.Commit.Title != gitTitle {
		t.Errorf("commit: %+v", rec.Commit)
	}
	// Git: add everything, then commit with the title and nothing else.
	var gitWrites []string
	for _, c := range h.fake.CallsTo("git") {
		if len(c.Args) > 3 && c.Args[0] == "-C" && c.Args[1] == h.W && (c.Args[2] == "add" || c.Args[2] == "commit") {
			gitWrites = append(gitWrites, join(c.Args[2:]))
		}
	}
	if want := []string{"add -A", "commit -m " + gitTitle}; strings.Join(gitWrites, "|") != strings.Join(want, "|") {
		t.Errorf("git writes %q, want %q", gitWrites, want)
	}
	for _, c := range h.fake.CallsTo("git") {
		for _, a := range c.Args {
			if strings.Contains(strings.ToLower(a), "co-authored") || strings.Contains(a, "Generated") || a == "--trailer" || a == "--author" {
				t.Errorf("attribution in %v", c.Args)
			}
		}
	}

	// The finish step is a round of the implementer after the commit.
	reqs := h.runner.requests()
	if len(reqs) != before+1 {
		t.Fatalf("%d requests after approve, want %d", len(reqs), before+1)
	}
	fin := reqs[before]
	if fin.Role != RoleImplementer || fin.Round != 2 || fin.Resume != "sess-implementer" || fin.ReadOnly {
		t.Errorf("finish request: %+v", fin)
	}
	section := finishSection(t, fin.Brief)
	for _, want := range []string{
		"`slicer note S04 --text \"Describe the verified outcome\" --render`",
		"`slicer done S04 --note \"Describe the verified outcome\" --render --check`",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("finish section lacks %q", want)
		}
	}
	if strings.Contains(section, "slicer handoff") {
		t.Errorf("finish section mentions handoff although implement_finish is done")
	}
	steps := h.steps()
	if st := steps[len(steps)-2]; st.Task != "S04 implementer finish step" || st.Parent != "1" || st.Role != "implementer" {
		t.Errorf("finish ledger step: %+v", st)
	}
	end := h.calls("slicer", "relay-run", "end")
	if len(end) != 1 || join(end[0]) != "relay-run end run-1 --outcome pass --close-steps done --json --root "+h.W {
		t.Errorf("relay-run end: %v", end)
	}
	// Order: commit first, then the finish step, then the end of the run.
	var order []string
	for _, c := range h.fake.Calls() {
		switch {
		case c.Tool == "git" && len(c.Args) > 3 && c.Args[2] == "commit":
			order = append(order, "commit")
		case c.Tool == "slicer" && len(c.Args) > 2 && c.Args[0] == "relay-run" && c.Args[1] == "end":
			order = append(order, "end")
		case c.Tool == "slicer" && len(c.Args) > 2 && c.Args[0] == "config":
			order = append(order, "config")
		}
	}
	if strings.Join(order, ",") != "commit,config,end" {
		t.Errorf("order %v", order)
	}
}

// finishSection is the part of the brief that ork wrote for the finish step;
// the implementer prompt above it names both finish steps.
func finishSection(t *testing.T, brief string) string {
	t.Helper()
	i := strings.Index(brief, "## Finish step")
	if i < 0 {
		t.Fatalf("no Finish step section in the brief")
	}
	return brief[i:]
}

func TestTheFinishStepFollowsImplementFinish(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routes = []clitest.Route{{Tool: "slicer", Args: []string{"config", "implement_finish", "--json", "--root"}, Prefix: true,
			Stdout: `{"key":"implement_finish","value":"handoff"}`}}
	})
	e := h.start()
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireState(t, e, runrec.StateCommitted)
	reqs := h.runner.requests()
	section := finishSection(t, reqs[len(reqs)-1].Brief)
	if !strings.Contains(section, "`slicer handoff S04 --render --check`") || strings.Contains(section, "`slicer done S04") {
		t.Errorf("handoff section:\n%s", section)
	}
	if !strings.Contains(section, "`slicer note S04 --text \"Describe the verified outcome\" --render`") {
		t.Errorf("the closing note comes before either finish step")
	}
}

func TestApproveIsOnlyForASliceAwaitingApproval(t *testing.T) {
	h := newHarness(t)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(context.Background()); err == nil {
		t.Error("Approve before the run was accepted")
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(context.Background()); err == nil {
		t.Error("a second Approve was accepted")
	}
	if n := len(h.calls("git", "-C", h.W, "commit")); n != 1 {
		t.Errorf("%d commits", n)
	}
}

func TestRejectStopsWithoutCommitting(t *testing.T) {
	h := newHarness(t)
	e := h.start()
	if err := e.Reject(context.Background(), "wrong approach"); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StateStopped)
	if !strings.Contains(rec.Reason, "wrong approach") || rec.Commit != nil || len(h.calls("git", "-C", h.W, "commit")) != 0 {
		t.Errorf("reason %q, commit %+v", rec.Reason, rec.Commit)
	}
	if end := h.calls("slicer", "relay-run", "end"); len(end) != 1 || !strings.Contains(join(end[0]), "--outcome stopped") {
		t.Errorf("end: %v", end)
	}
	if err := e.Reject(context.Background(), "again"); err == nil {
		t.Error("Reject on a stopped slice was accepted")
	}
}

func TestACommitFailurePausesAndRetryCommits(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{{Tool: "git", Args: []string{"-C", W, "commit"}, Prefix: true,
				Seq: []clitest.Reply{{Stderr: "fatal: unable to write\n", Exit: 1}, {}}}}
		}
	})
	e := h.start()
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingError || !strings.Contains(rec.Pending.Reason, "commit") {
		t.Fatalf("pending: %+v", rec.Pending)
	}
	if rec.Commit != nil || len(h.runner.requests()) != 3 {
		t.Fatalf("nothing may run after a failed commit: %+v", rec.Commit)
	}
	if err := e.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec = requireState(t, e, runrec.StateCommitted)
	if rec.Commit == nil || rec.Commit.SHA != "0123abc" {
		t.Fatalf("commit: %+v", rec.Commit)
	}
}

func TestParseGitTitle(t *testing.T) {
	for body, want := range map[string]string{
		`One commit: "S04: engine core for one slice".`: "S04: engine core for one slice",
		"One commit: \"S04: x\"\n":                      "S04: x",
		`Commit "S04: a" then stop.`:                    "S04: a",
		"S04: engine core\n":                            "S04: engine core",
		"\n  S04: engine core  \n\nMore text\n":         "S04: engine core",
	} {
		got, err := ParseGitTitle(body)
		if err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", body, got, err, want)
		}
	}
	for _, bad := range []string{"", "  \n", `One commit: "".`} {
		if got, err := ParseGitTitle(bad); err == nil {
			t.Errorf("%q gave %q", bad, got)
		}
	}
}

func TestApprovePausesWhenTheSliceHasNoGitTitle(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.routes = []clitest.Route{{Tool: "slicer", Args: []string{"show", item}, Prefix: true,
			Stdout: `{"id":"S04","slice":{"id":"S04","sections":[{"heading":"Why","body":"x"}]}}`}}
	})
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	if err := e.Approve(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending.Kind != runrec.PendingError || !strings.Contains(rec.Pending.Reason, "Git") || len(h.calls("git", "-C", h.W, "commit")) != 0 {
		t.Fatalf("pending: %+v", rec.Pending)
	}
}
