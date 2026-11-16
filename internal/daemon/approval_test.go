// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func passRunner() roundRunner {
	return func(_ context.Context, r engine.Request) (engine.Result, error) {
		report := "Tests: 1 passed, 0 failed\n\n## Owner needed\nnone\n"
		switch r.Role {
		case engine.RoleReviewer:
			report = "(A): nothing\n(B): nothing\n(C): nothing\n(D): nothing\n\n## Owner needed\nnone\n"
		case engine.RoleGate:
			report = "VERDICT: PASS\n" + report
		}
		return engine.Result{Exit: engine.ExitOK, Session: "session", Report: report}, nil
	}
}

const twoHumanChecks = `{"slice":{"sections":[{"heading":"Check","body":"- go test ./...\n- [HUMAN CHECK] Read the card at 80x24.\n- [HUMAN CHECK] Press F and send a note."},{"heading":"Git","body":"One commit: \"S1: test\"."}]}}`

func TestRunStatusCarriesTheApprovalCardAndItsActions(t *testing.T) {
	d, store, p := runDaemon(t, passRunner(),
		clitest.Route{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: twoHumanChecks},
		clitest.Route{Tool: "slicer", Args: []string{"note"}, Prefix: true, Stdout: `{"id":"S1","added":true}`},
	)
	// The landing adds the checks marked later to the todo file in the project
	// folder (S331), so the folder exists although git is faked.
	if err := os.MkdirAll(p.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	b := request(t, d, "run.start", `{"project":"p","item":"S1","well_specified":true,"split":false}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StateAwaitingApproval)
	waitIdle(t, d, rec.ID)

	rows := d.engineRuns(p.Name)
	if len(rows) != 1 || rows[0].Card == nil {
		t.Fatalf("wire rows %+v", rows)
	}
	card := rows[0].Card
	if card.Verdict != "PASS" || card.Passed != 1 || card.Failed != 0 || len(card.HumanChecks) != 2 || card.HumanChecks[1].Text != "Press F and send a note." {
		t.Errorf("wire card %+v", card)
	}
	wire, _ := json.Marshal(rows[0])
	if !strings.Contains(string(wire), `"card":{`) || !strings.Contains(string(wire), `"human_checks"`) {
		t.Errorf("the card is not on the wire: %s", wire)
	}

	// Approval waits for the marks.
	if _, err := d.handle(context.Background(), "run.approve", json.RawMessage(fmt.Sprintf(`{"run":%q}`, rec.ID))); err == nil || !strings.Contains(err.Error(), "2 human checks") {
		t.Fatalf("approve with open checks: %v", err)
	}
	request(t, d, "run.check", fmt.Sprintf(`{"run":%q,"check":1,"state":"ticked"}`, rec.ID))
	request(t, d, "run.check", fmt.Sprintf(`{"run":%q,"check":2,"state":"later"}`, rec.ID))
	got := d.engineRuns(p.Name)[0].Card.HumanChecks
	if got[0].State != runrec.CheckTicked || got[1].State != runrec.CheckLater {
		t.Errorf("marks on the wire: %+v", got)
	}
	if _, err := d.handle(context.Background(), "run.check", json.RawMessage(fmt.Sprintf(`{"run":%q,"check":3,"state":"ticked"}`, rec.ID))); err == nil {
		t.Error("a mark on a check the slice does not have")
	}

	// A fix goes to a fix round and the run comes back to the card.
	out := request(t, d, "run.fix", fmt.Sprintf(`{"run":%q,"text":"Rename the flag."}`, rec.ID))
	if !strings.Contains(string(out), `"state":"awaiting-approval"`) {
		t.Errorf("fix reply %s", out)
	}
	after, _ := store.Open(rec.ID)
	if fixes := after.Snapshot().Fixes; len(fixes) != 1 || fixes[0].Note != "Rename the flag." {
		t.Errorf("stored fixes %+v", fixes)
	}
	if got := d.engineRuns(p.Name)[0].Card; got == nil || got.Round != 2 || got.HumanChecks[0].State != runrec.CheckTicked {
		t.Errorf("card after the fix %+v", got)
	}
	if _, err := d.handle(context.Background(), "run.fix", json.RawMessage(fmt.Sprintf(`{"run":%q,"text":"  "}`, rec.ID))); err == nil {
		t.Error("an empty fix note")
	}

	request(t, d, "run.approve", fmt.Sprintf(`{"run":%q}`, rec.ID))
	waitState(t, store, rec.ID, runrec.StateCommitted)
	waitIdle(t, d, rec.ID)
	if rows := d.engineRuns(p.Name); rows[0].Card != nil {
		t.Errorf("a committed run keeps no card on the wire: %+v", rows[0].Card)
	}
}

// An owner action the engine refuses changed nothing: the caller reads the
// refusal in the reply, and the run row shows no error marker (S141).
func TestRefusedOwnerActionShowsNoErrorMarker(t *testing.T) {
	d, store, p := runDaemon(t, passRunner(),
		clitest.Route{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: twoHumanChecks},
		clitest.Route{Tool: "slicer", Args: []string{"note"}, Prefix: true, Stdout: `{"id":"S1","added":true}`},
	)
	b := request(t, d, "run.start", `{"project":"p","item":"S1","well_specified":true,"split":false}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StateAwaitingApproval)
	waitIdle(t, d, rec.ID)

	for name, call := range map[string]string{
		"run.approve": fmt.Sprintf(`{"run":%q}`, rec.ID),
		"run.check":   fmt.Sprintf(`{"run":%q,"check":9,"state":"ticked"}`, rec.ID),
		"run.fix":     fmt.Sprintf(`{"run":%q,"text":" "}`, rec.ID),
	} {
		_, err := d.handle(context.Background(), name, json.RawMessage(call))
		if err == nil {
			t.Fatalf("%s was not refused", name)
		}
		// A refusal has its own wire code, so a client shows its text as it is.
		if wire := ipc.ErrorOf(err); wire.Code != CodeRefused || wire.Message == "" {
			t.Errorf("%s: wire error %+v, want code %q", name, wire, CodeRefused)
		}
		waitIdle(t, d, rec.ID)
		row := d.engineRuns(p.Name)[0]
		if got := wireRunError(t, row); got != "" {
			t.Errorf("%s: the run row carries the refusal as an error: %q", name, got)
		}
		if row.State != runrec.StateAwaitingApproval {
			t.Errorf("%s: state %s", name, row.State)
		}
	}
}

// An action that fails for another reason than a refusal keeps its error on the
// run row, as before.
func TestFailedOwnerActionKeepsTheErrorMarker(t *testing.T) {
	d, store, p := runDaemon(t, passRunner(),
		clitest.Route{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: twoHumanChecks},
		clitest.Route{Tool: "slicer", Args: []string{"note"}, Prefix: true, Stderr: "slicer: cannot write the note\n", Exit: 1},
	)
	b := request(t, d, "run.start", `{"project":"p","item":"S1","well_specified":true,"split":false}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StateAwaitingApproval)
	waitIdle(t, d, rec.ID)
	request(t, d, "run.check", fmt.Sprintf(`{"run":%q,"check":1,"state":"ticked"}`, rec.ID))
	request(t, d, "run.check", fmt.Sprintf(`{"run":%q,"check":2,"state":"later"}`, rec.ID))
	_, err := d.handle(context.Background(), "run.approve", json.RawMessage(fmt.Sprintf(`{"run":%q}`, rec.ID)))
	if err == nil {
		t.Fatal("approve with a failing note succeeded")
	}
	if wire := ipc.ErrorOf(err); wire.Code != ipc.CodeInternal {
		t.Errorf("a failure that is not a refusal has code %q", wire.Code)
	}
	waitIdle(t, d, rec.ID)
	if got := wireRunError(t, d.engineRuns(p.Name)[0]); !strings.Contains(got, "slicer note") && !strings.Contains(got, "human checks") {
		t.Errorf("the failure is not on the run row: %q", got)
	}
}

// S371, *proposed*: the approval point records each allowed Read and Grep of a
// local role in the role's read log, from the call alone.
func TestApprovalRecordsLocalReads(t *testing.T) {
	d, req := approvalRun(t)
	req.ReadLog = filepath.Join(t.TempDir(), "readlog-implementer-1.txt")
	token, finish := d.registerApproval(context.Background(), req, claude.ApprovalPolicy{Worktree: req.Worktree, Allowlist: []string{"Read", "Grep"}})
	defer finish()
	call := func(tool string, input map[string]any) claude.Approval {
		t.Helper()
		raw, _ := json.Marshal(input)
		a, err := approvalRequest(d, context.Background(), ApprovalParams{Session: token, Call: claude.ToolCall{ToolName: tool, ToolInput: raw}})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	abs := filepath.Join(req.Worktree, "internal", "a.go")
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Read", map[string]any{"file_path": abs, "offset": 1, "limit": 500}},
		{"Read", map[string]any{"file_path": "internal/a.go", "offset": 501, "limit": 100}},
		{"Read", map[string]any{"file_path": "b.go", "limit": 20}},
		{"Grep", map[string]any{"pattern": "func Foo", "path": "internal"}},
	} {
		if a := call(c.tool, c.input); a.Decision != "allow" {
			t.Fatalf("%s %v: %+v", c.tool, c.input, a)
		}
	}
	// A denied call read nothing, and only Read and Grep are recorded.
	if a := call("Read", map[string]any{"file_path": "/etc/passwd"}); a.Decision != "deny" {
		t.Fatalf("outside read: %+v", a)
	}
	if a := call("Read", map[string]any{"file_path": "c.go", "limit": 501}); a.Decision != "deny" {
		t.Fatalf("over-limit read: %+v", a)
	}
	got, err := engine.ReadReadLog(req.ReadLog)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"internal/a.go:1-500", "internal/a.go:501-600", "b.go:1-20", "func Foo in internal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read log = %q, want %q", got, want)
	}
}

// S370: the owner judges an allow from every token of the command, so the
// question keeps a short title and carries the whole command in Detail, on the
// run row (status) and on run.show.
func TestApprovalQuestionCarriesTheFullCommand(t *testing.T) {
	d, req := approvalRun(t)
	token, finish := d.registerApproval(context.Background(), req, claude.ApprovalPolicy{Worktree: req.Worktree, Allowlist: req.Allowlist})
	defer finish()
	command := "echo " + strings.Repeat("0123456789", 59) + "end00"
	if len(command) != 600 {
		t.Fatalf("test command is %d characters, want 600", len(command))
	}
	input, _ := json.Marshal(map[string]string{"command": command})
	reply := make(chan claude.Approval, 1)
	go func() {
		a, _ := approvalRequest(d, context.Background(), ApprovalParams{Session: token, Call: claude.ToolCall{ToolName: "Bash", ToolInput: input}})
		reply <- a
	}()
	row := waitPermission(t, d, req.RunID)
	if n := len([]rune(row.Question.Title)); n > 120 {
		t.Errorf("status title is %d characters, want at most 120: %q", n, row.Question.Title)
	}
	if !strings.HasPrefix(row.Question.Title, "Allow Bash: echo ") || !strings.Contains(row.Question.Title, "(call ") {
		t.Errorf("status title lost its tool and call number: %q", row.Question.Title)
	}
	if row.Question.Detail != command {
		t.Errorf("status detail is not the whole command: %q", row.Question.Detail)
	}
	show, err := d.runShow(RunAction{Run: req.RunID})
	if err != nil {
		t.Fatal(err)
	}
	if show.Pending == nil || show.Pending.Kind != "permission" || show.Pending.Ask == nil {
		t.Fatalf("run.show has no live permission question: %+v", show.Pending)
	}
	if got := show.Pending.Ask; got.Detail != command || len([]rune(got.Title)) > 120 {
		t.Errorf("run.show question: title %q, detail %q", got.Title, got.Detail)
	}
	finish() // the role ends: the waiting call is denied
	<-reply
}
