// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// showReasonText is a pause reason of 600 characters with no line end.
var showReasonText = "report shape: " + strings.Repeat("the reviewer's [owner] finding has no Options line; ", 12)

func showShow(t *testing.T, d *Daemon, run string) ShowReply {
	t.Helper()
	var got ShowReply
	b, _ := json.Marshal(RunAction{Run: run})
	if err := json.Unmarshal(request(t, d, MethodRunShow, string(b)), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRunShowReturnsThePauseInFull(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	if len(showReasonText) < 600 {
		t.Fatalf("test reason is %d characters", len(showReasonText))
	}
	ask := &runrec.OwnerQuestion{Title: "Which?", Options: []runrec.Option{{Label: "a", Summary: "first"}, {Label: "b", Summary: "second"}}, Recommend: 2, Reason: "cheaper"}
	_, err := store.Create(runrec.Record{
		ID: "r1", Project: p.Name, Item: "S1", State: runrec.StatePaused, Reason: "old reason",
		Pending: &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateReview, Role: "reviewer", Round: 2, Reason: showReasonText, Ask: ask, Key: "area.topic", Note: "owner note"},
		Roles: []runrec.RoleRecord{
			{Role: "implementer", Round: 1, Exit: "ok", Report: "implementer-1.md"},
			{Role: "reviewer", Round: 2, Exit: "ok", Report: "reviewer-2.md"},
		},
		Checks: []runrec.CheckRecord{{Round: 1, Command: "go test ./...", OK: true, Seconds: 3, Log: "checks-1.log", LogSHA256: "deadbeefcafe", Deferred: []string{"S5: stale"}}},
		Findings: []runrec.Finding{
			{Kind: runrec.FindingOwner, Role: "reviewer", Round: 1, Title: "Rename?", Text: "FULL FINDING TEXT\nsecond line", Ask: ask, Answer: "yes"},
			{Kind: runrec.FindingLater, Role: "reviewer", Round: 1, Title: "Later work", Text: "LATER TEXT", Filed: "S99"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := showShow(t, d, "r1")
	if got.ID != "r1" || got.Item != "S1" || got.State != runrec.StatePaused {
		t.Fatalf("header %+v", got)
	}
	if got.Pending == nil || got.Pending.Kind != "shape" || got.Pending.Role != "reviewer" || got.Pending.Round != 2 || got.Pending.State != runrec.StateReview || got.Pending.Key != "area.topic" || got.Pending.Ask == nil {
		t.Fatalf("pending %+v", got.Pending)
	}
	if got.Pending.Reason != showReasonText || got.Reason != showReasonText || strings.Contains(got.Pending.Reason, "; next:") || strings.Contains(got.Reason, "; next:") {
		t.Fatalf("reason %q / %q, want the whole stored reason with no next suffix", got.Reason, got.Pending.Reason)
	}
	if got.Next != "ork retry r1" {
		t.Fatalf("next %q", got.Next)
	}
	if len(got.Roles) != 2 || got.Roles[1].Role != "reviewer" || got.Roles[1].Report != "reviewer-2.md" || got.Roles[0].Report != "implementer-1.md" {
		t.Fatalf("roles %+v", got.Roles)
	}
	if len(got.Checks) != 1 || got.Checks[0].Log != "checks-1.log" || got.Checks[0].Command != "go test ./..." || !got.Checks[0].OK {
		t.Fatalf("checks %+v", got.Checks)
	}
	if len(got.Findings) != 2 || got.Findings[0].N != 1 || got.Findings[0].Text != "FULL FINDING TEXT\nsecond line" || got.Findings[0].Answer != "yes" || got.Findings[0].Ask == nil || got.Findings[1].N != 2 || got.Findings[1].Filed != "S99" || got.Findings[1].Text != "LATER TEXT" {
		t.Fatalf("findings %+v", got.Findings)
	}
	raw := string(request(t, d, MethodRunShow, `{"run":"r1"}`))
	if strings.Contains(raw, "deadbeefcafe") || strings.Contains(raw, "log_sha256") || !strings.Contains(raw, `"deferred":["S5: stale"]`) {
		t.Fatalf("the checks rows carry record internals, or lose their deferred lines: %s", raw)
	}
	if strings.Contains(raw, "owner note") || strings.Contains(raw, `"note"`) {
		t.Fatalf("the reply carries the owner's retry note: %s", raw)
	}
	if MethodRunShow != "run.show" {
		t.Fatalf("method %q", MethodRunShow)
	}
}

// ork wait tells a landed run from a committed one by this field (S257).
func TestRunShowSaysWhenARunLanded(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	for _, rec := range []runrec.Record{
		{ID: "landed", State: runrec.StateCommitted, LandedAt: at},
		{ID: "committed", State: runrec.StateCommitted},
		{ID: "stopped", State: runrec.StateStopped, LandedAt: at}, // a landing that was undone is not landed
	} {
		rec.Project, rec.Item = p.Name, "S1"
		if _, err := store.Create(rec); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[string]bool{"landed": true, "committed": false, "stopped": false} {
		if got := showShow(t, d, id); got.Landed != want {
			t.Errorf("run %s: landed %v, want %v", id, got.Landed, want)
		}
	}
	if raw := string(request(t, d, MethodRunShow, `{"run":"committed"}`)); strings.Contains(raw, `"landed"`) {
		t.Errorf("a run that did not land carries a landed field: %s", raw)
	}
}

func TestRunShowNamesTheNextCommandOfAQuestionAndOfRounds(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	for id, pend := range map[string]runrec.Pending{
		"q1": {Kind: runrec.PendingQuestion, State: runrec.StateReview, Role: "reviewer", Round: 1, Reason: "r", Question: "Which one?\nQuestion: Which?"},
		"n1": {Kind: runrec.PendingRounds, State: runrec.StateImplement, Role: "implementer", Round: 3, Reason: "3 red rounds"},
	} {
		pend := pend
		if _, err := store.Create(runrec.Record{ID: id, Project: p.Name, Item: "S1", State: runrec.StatePaused, Pending: &pend}); err != nil {
			t.Fatal(err)
		}
	}
	if got := showShow(t, d, "q1"); got.Next != "ork answer q1 TEXT" || got.Pending.Question != "Which one?\nQuestion: Which?" {
		t.Fatalf("question run: next %q pending %+v", got.Next, got.Pending)
	}
	if got := showShow(t, d, "n1"); got.Next != "ork retry n1 [--rounds N] or ork stop n1" {
		t.Fatalf("rounds run: next %q", got.Next)
	}
}

func TestRunShowRolesCarryAttemptAndDenials(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	denied := []runrec.DeniedCommand{{Tool: "Bash", Command: "a"}, {Tool: "Bash", Command: "b"}, {Tool: "Edit", Command: "c"}, {Tool: "Bash", Command: "d"}, {Tool: "Bash", Command: "e"}}
	_, err := store.Create(runrec.Record{ID: "r1", Project: p.Name, Item: "S1", State: runrec.StateReview, Roles: []runrec.RoleRecord{
		{Role: "reviewer", Round: 1, Report: "reviewer-1.md", DeniedCommands: denied},
		{Role: "gate", Round: 1},
		{Role: "reviewer", Round: 1, Report: "reviewer-1-attempt-2.md"},
		{Role: "reviewer", Round: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := showShow(t, d, "r1")
	var attempts []int
	for _, r := range got.Roles {
		attempts = append(attempts, r.Attempt)
	}
	if len(got.Roles) != 4 || attempts[0] != 1 || attempts[1] != 1 || attempts[2] != 2 || attempts[3] != 1 {
		t.Fatalf("attempts %v", attempts)
	}
	if got.Roles[0].Denied != 5 || strings.Join(got.Roles[0].DeniedFirst, "|") != "Bash: a|Bash: b|Edit: c" || got.Roles[2].Denied != 0 || len(got.Roles[2].DeniedFirst) != 0 {
		t.Fatalf("denied %+v", got.Roles)
	}
}

func TestRunShowNamesTheLatestReportOfThePendingRole(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	roles := []runrec.RoleRecord{
		{Role: "reviewer", Round: 1, Report: "reviewer-1.md"},
		{Role: "reviewer", Round: 1, Report: "reviewer-1-attempt-2.md"},
		{Role: "reviewer", Round: 1, Report: "reviewer-1-attempt-3.md"},
		{Role: "gate", Round: 1, Report: "gate-1.md"},
	}
	_, err := store.Create(runrec.Record{ID: "p1", Project: p.Name, Item: "S1", State: runrec.StatePaused, Roles: roles,
		Pending: &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateReview, Role: "reviewer", Round: 1, Reason: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := showShow(t, d, "p1"); got.LatestReport != "reviewer-1-attempt-3.md" {
		t.Fatalf("pending run: latest %q", got.LatestReport)
	}
	_, err = store.Create(runrec.Record{ID: "n1", Project: p.Name, Item: "S1", State: runrec.StateGate, Roles: append(append([]runrec.RoleRecord{}, roles...), runrec.RoleRecord{Role: "implementer", Round: 2})})
	if err != nil {
		t.Fatal(err)
	}
	if got := showShow(t, d, "n1"); got.LatestReport != "gate-1.md" {
		t.Fatalf("run with no pending: latest %q, want the last role record that has a report", got.LatestReport)
	}
	_, err = store.Create(runrec.Record{ID: "e1", Project: p.Name, Item: "S1", State: runrec.StateSetup})
	if err != nil {
		t.Fatal(err)
	}
	if got := showShow(t, d, "e1"); got.LatestReport != "" {
		t.Fatalf("run with no report: latest %q", got.LatestReport)
	}
}

func TestRunShowIsNotFoundForAnUnknownOrBadRunID(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	if _, err := store.Create(runrec.Record{ID: "bad", Project: p.Name, Item: "S1", State: runrec.StatePaused}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.RunDir("bad"), "run.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"unknown", "../r1", ""} {
		_, err := d.handle(context.Background(), MethodRunShow, renderParams(run, ""))
		var wire *ipc.Error
		if !errors.As(err, &wire) || wire.Code != CodeNotFound {
			t.Errorf("run %q: %v", run, err)
		}
	}
	_, err := d.handle(context.Background(), MethodRunShow, renderParams("bad", ""))
	wire := ipc.ErrorOf(err)
	if wire == nil || wire.Code != ipc.CodeInternal || !strings.HasPrefix(wire.Message, "the record of run bad is unreadable: ") || strings.Contains(wire.Message, store.RunDir("bad")) {
		t.Fatalf("an unreadable record gave %v", err)
	}
}

func TestRunShowDoesNotWaitForTheRun(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	if _, err := store.Create(runrec.Record{ID: "r1", Project: p.Name, Item: "S1", State: runrec.StateImplement}); err != nil {
		t.Fatal(err)
	}
	d.runs.runs["r1"] = &managedRun{busy: true}
	lock := d.runRecordLock("r1")
	lock.Lock()
	defer lock.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := d.handle(context.Background(), MethodRunShow, renderParams("r1", ""))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run.show waited for the busy run's record lock")
	}
}

func TestRunShowServesNoFileAndWritesNothing(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	if _, err := store.Create(runrec.Record{ID: "r1", Project: p.Name, Item: "S1", State: runrec.StatePaused, Roles: []runrec.RoleRecord{{Role: "gate", Round: 1, Report: "gate-1.md"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.RunDir("r1"), "gate-1.md"), []byte("REPORT BODY SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(store.RunDir("r1"), "run.json"))
	raw := string(request(t, d, MethodRunShow, `{"run":"r1","name":"gate-1.md"}`))
	if strings.Contains(raw, "REPORT BODY SECRET") {
		t.Fatalf("run.show carries report text: %s", raw)
	}
	after, _ := os.ReadFile(filepath.Join(store.RunDir("r1"), "run.json"))
	if string(before) != string(after) {
		t.Fatal("run.show changed the record")
	}
}

// A pending left in the record of a run that is not paused is not a pause:
// ork status shows a pause only for a paused run, and so does run.show.
func TestRunShowShowsNoPauseForARunThatIsNotPaused(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "r1", Project: p.Name, Item: "S1", State: runrec.StateReview, Reason: "",
		Pending: &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateReview, Role: "reviewer", Round: 1, Reason: "stale pause"},
		Roles:   []runrec.RoleRecord{{Role: "reviewer", Round: 1, Report: "reviewer-1.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := showShow(t, d, "r1"); got.Pending != nil || got.Next != "" || got.Reason != "" {
		t.Fatalf("reply %+v, want no pause", got)
	}
}

// The Go client reaches run.show and a check log through run.report over the
// daemon's socket (wire test).
func TestClientShowAndCheckLogOverTheSocket(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0)
	e.start(t)
	store := &runrec.Store{Dir: filepath.Join(e.paths.Dir, "runs")}
	_, err := store.Create(runrec.Record{ID: "r1", Project: "p", Item: "S1", State: runrec.StatePaused,
		Pending: &runrec.Pending{Kind: runrec.PendingLost, State: runrec.StateImplement, Role: "implementer", Round: 1, Reason: "the role is gone"},
		Checks:  []runrec.CheckRecord{{Round: 1, Command: "go test", Log: "checks-1.log"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.RunDir("r1"), "checks-1.log"), []byte("LOG OVER THE WIRE"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := e.dial(t)
	got, err := c.Show(context.Background(), "r1")
	if err != nil || got.ID != "r1" || got.Pending == nil || got.Pending.Reason != "the role is gone" || got.Next != "ork retry r1" || len(got.Checks) != 1 || got.Checks[0].Log != "checks-1.log" {
		t.Fatalf("show %+v, %v", got, err)
	}
	if text, err := c.Report(context.Background(), "r1", "checks-1.log"); err != nil || text != "LOG OVER THE WIRE" {
		t.Fatalf("log %q, %v", text, err)
	}
	if _, err := c.Show(context.Background(), "nope"); ipc.ErrorOf(err).Code != CodeNotFound {
		t.Fatalf("unknown run: %v", err)
	}
}

// ork status prints "next: ork retry RUN" for every paused run, also one whose
// record has no pending; run.show names the same command (S297).
func TestRunShowNamesTheNextCommandOfAPausedRunWithNoPending(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	if _, err := store.Create(runrec.Record{ID: "r1", Project: p.Name, Item: "S1", State: runrec.StatePaused, Reason: "stopped by ork"}); err != nil {
		t.Fatal(err)
	}
	got := showShow(t, d, "r1")
	if got.Pending != nil || got.Next != "ork retry r1" || got.Reason != "stopped by ork" {
		t.Fatalf("reply %+v", got)
	}
}
