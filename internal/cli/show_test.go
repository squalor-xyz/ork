// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

var showLongReason = "report shape: " + strings.Repeat("the finding has no Options line; ", 20) + "end"

// showDouble answers run.show with show and run.report from texts, by name.
func showDouble(show daemon.ShowReply, texts map[string]string) *callLog {
	return &callLog{reply: func(method string, params any) (string, error) {
		a := params.(daemon.RunAction)
		switch method {
		case daemon.MethodRunShow:
			if a.Run != show.ID {
				return "", &ipc.Error{Code: daemon.CodeNotFound, Message: "no run " + a.Run}
			}
			b, _ := json.Marshal(show)
			return string(b), nil
		case daemon.MethodRunReport:
			text, ok := texts[a.Name]
			if !ok || a.Run != show.ID {
				return "", &ipc.Error{Code: daemon.CodeNotFound, Message: "no report " + a.Name + " for run " + a.Run}
			}
			b, _ := json.Marshal(daemon.ReportReply{Name: a.Name, Text: text})
			return string(b), nil
		}
		return "", &ipc.Error{Code: ipc.CodeInternal, Message: "unexpected " + method}
	}}
}

func pausedShow() daemon.ShowReply {
	return daemon.ShowReply{
		ID: "r1", Item: "S9", State: "paused", Reason: showLongReason, Next: "ork retry r1",
		Pending: &daemon.ShowPending{Kind: "shape", State: "review", Role: "reviewer", Round: 3, Reason: showLongReason},
		Roles: []daemon.ShowRole{
			{Role: "implementer", Round: 1, Attempt: 1, Exit: "ok", Report: "implementer-1.md", Denied: 5, DeniedFirst: []string{"Bash: a", "Bash: b", "Edit: c"}},
			{Role: "reviewer", Round: 3, Attempt: 3, Exit: "ok", Report: "reviewer-3-attempt-3.md"},
		},
		Checks:       []daemon.ShowCheck{{Round: 2, Command: "go test ./...", OK: false, Seconds: 7, Log: "checks-2.log"}},
		Findings:     []daemon.ShowFinding{{N: 1, Kind: "owner", Title: "Rename the flag?", Text: "FINDING TEXT LINE 1\nFINDING TEXT LINE 2", Answer: "yes"}, {N: 2, Kind: "later", Title: "Later work", Text: "LATER TEXT", Filed: "S99"}},
		LatestReport: "reviewer-3-attempt-3.md",
	}
}

func TestShowPrintsTheSummaryAndThePendingReport(t *testing.T) {
	log := showDouble(pausedShow(), map[string]string{"reviewer-3-attempt-3.md": "PENDING REPORT BODY\nline two"})
	code, out, errs := runCLI(&App{Call: log.call}, "show", "r1")
	if code != ExitOK || errs != "" {
		t.Fatalf("code %d, stderr %q", code, errs)
	}
	if len(log.calls) != 2 || log.calls[0] != `run.show {"run":"r1"}` || log.calls[1] != `run.report {"name":"reviewer-3-attempt-3.md","run":"r1"}` {
		t.Fatalf("calls %q", log.calls)
	}
	lines := strings.Split(out, "\n")
	if lines[0] != "r1 S9 paused" {
		t.Fatalf("header %q", lines[0])
	}
	for _, want := range []string{
		"shape pause, reviewer round 3",
		"  " + showLongReason, // the whole reason on its own line, no "; next:" glued on
		"next: ork retry r1",
		"\n  implementer round 1 attempt 1  exit ok  report implementer-1.md\n    denied 5: Bash: a, Bash: b, Edit: c\n",
		"reviewer round 3 attempt 3  exit ok  report reviewer-3-attempt-3.md",
		"round 2  red  7s  go test ./...  log checks-2.log",
		"1 owner  Rename the flag?",
		"FINDING TEXT LINE 2",
		"answer: yes",
		"2 later  Later work",
		"filed as S99",
		"read a report or log: ork show r1 NAME",
		"-- reviewer-3-attempt-3.md --\nPENDING REPORT BODY\nline two",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "; next:") || strings.Contains(out, "Bash: c") {
		t.Errorf("output has a status-style suffix or a fourth denial:\n%s", out)
	}
	// The next command comes again after the report, so a long report does not scroll it away.
	if !strings.HasSuffix(out, "-- reviewer-3-attempt-3.md --\nPENDING REPORT BODY\nline two\nnext: ork retry r1\n") || strings.Count(out, "next: ork retry r1") != 2 {
		t.Errorf("the next command does not follow the pending report:\n%s", out)
	}
	for _, line := range lines {
		if len(line) > 80 && !strings.HasPrefix(line, "  "+showLongReason[:20]) {
			t.Errorf("line of %d columns: %q", len(line), line)
		}
	}

	// A run that is not paused prints the summary and names the reports only.
	running := pausedShow()
	running.State, running.Pending, running.Reason, running.Next = "review", nil, "", ""
	log = showDouble(running, map[string]string{"reviewer-3-attempt-3.md": "PENDING REPORT BODY"})
	code, out, _ = runCLI(&App{Call: log.call}, "show", "r1")
	if code != ExitOK || len(log.calls) != 1 || strings.Contains(out, "PENDING REPORT BODY") || strings.Contains(out, "-- ") || !strings.Contains(out, "r1 S9 review\n") || !strings.Contains(out, "report reviewer-3-attempt-3.md") || strings.Contains(out, "next:") {
		t.Fatalf("not paused: code %d, calls %q:\n%s", code, log.calls, out)
	}
}

func TestShowSaysWhenThePendingReportCannotBeRead(t *testing.T) {
	log := showDouble(pausedShow(), nil)
	code, out, _ := runCLI(&App{Call: log.call}, "show", "r1")
	if code != ExitOK || !strings.Contains(out, "-- reviewer-3-attempt-3.md -- (cannot be read: no report reviewer-3-attempt-3.md for run r1)") || !strings.Contains(out, showLongReason) || !strings.HasSuffix(out, "cannot be read: no report reviewer-3-attempt-3.md for run r1)\nnext: ork retry r1\n") {
		t.Fatalf("code %d:\n%s", code, out)
	}
}

func TestShowNamePrintsOneReportOrOneLog(t *testing.T) {
	texts := map[string]string{"reviewer-3-attempt-3.md": "REPORT TEXT\n", "checks-2.log": "LOG TEXT\nline 2"}
	for _, tc := range []struct{ name, want string }{
		{"reviewer-3-attempt-3.md", "REPORT TEXT\n"},
		{"checks-2.log", "LOG TEXT\nline 2\n"},
	} {
		log := showDouble(pausedShow(), texts)
		code, out, errs := runCLI(&App{Call: log.call}, "show", "r1", tc.name)
		if code != ExitOK || out != tc.want || errs != "" || len(log.calls) != 1 || !strings.HasPrefix(log.calls[0], "run.report ") {
			t.Errorf("%s: code %d, stdout %q, stderr %q, calls %q", tc.name, code, out, errs, log.calls)
		}
	}
	log := showDouble(pausedShow(), texts)
	code, out, errs := runCLI(&App{Call: log.call}, "show", "r1", "nope.md")
	want := "ork: no report or log nope.md for run r1; names: implementer-1.md, reviewer-3-attempt-3.md, checks-2.log\n"
	if code != ExitFailure || out != "" || errs != want {
		t.Fatalf("unknown name: code %d, stdout %q, stderr %q, want %q", code, out, errs, want)
	}
	// An unknown run keeps the daemon's message, with no list of names.
	code, _, errs = runCLI(&App{Call: log.call}, "show", "other", "nope.md")
	if code != ExitFailure || errs != "ork: no report nope.md for run other\n" {
		t.Fatalf("unknown run: code %d, stderr %q", code, errs)
	}
}

func TestShowJSON(t *testing.T) {
	log := showDouble(pausedShow(), map[string]string{"checks-2.log": "LOG TEXT"})
	code, out, _ := runCLI(&App{Call: log.call}, "show", "r1", "--json")
	var got daemon.ShowReply
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != ExitOK || got.Pending == nil || got.Pending.Reason != showLongReason || got.LatestReport != "reviewer-3-attempt-3.md" || len(got.Findings) != 2 {
		t.Fatalf("code %d, err %v, out %s", code, err, out)
	}
	if len(log.calls) != 1 {
		t.Fatalf("--json without NAME made calls %q, want run.show only", log.calls)
	}
	code, out, _ = runCLI(&App{Call: log.call}, "show", "r1", "checks-2.log", "--json")
	var rep daemon.ReportReply
	if err := json.Unmarshal([]byte(out), &rep); err != nil || code != ExitOK || rep.Name != "checks-2.log" || rep.Text != "LOG TEXT" {
		t.Fatalf("code %d, err %v, out %s", code, err, out)
	}
	code, out, _ = runCLI(&App{Call: log.call}, "show", "r1", "nope.md", "--json")
	var e struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal([]byte(out), &e); err != nil || code != ExitFailure || e.Error.Code != daemon.CodeNotFound || !strings.Contains(e.Error.Message, "nope.md") {
		t.Fatalf("code %d, err %v, out %s", code, err, out)
	}
}

func TestShowQuestionPausePrintsTheOptions(t *testing.T) {
	ask := &runrec.OwnerQuestion{Title: "Which store?", Options: []runrec.Option{{Label: "sqlite", Summary: "one file"}, {Label: "json", Summary: "plain"}}, Recommend: 2, Reason: "no dependency"}
	show := daemon.ShowReply{
		ID: "r1", Item: "S9", State: "paused", Next: "ork answer r1 TEXT",
		Reason:  "the reviewer asks a question",
		Pending: &daemon.ShowPending{Kind: "question", State: "review", Role: "reviewer", Round: 1, Reason: "the reviewer asks a question", Question: "Some context line.", Ask: ask, Key: "storage.engine"},
	}
	log := showDouble(show, nil)
	code, out, _ := runCLI(&App{Call: log.call}, "show", "r1")
	for _, want := range []string{
		"question pause, reviewer round 1",
		"Some context line.",
		"Question: Which store?",
		"1. sqlite: one file",
		"2. json: plain",
		"Recommend: 2 - no dependency",
		"decision key: storage.engine",
		"next: ork answer r1 TEXT",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if code != ExitOK || len(log.calls) != 1 {
		t.Errorf("code %d, calls %q: a question pause has no latest report to read", code, log.calls)
	}

	// Question text that already holds the block is not printed twice.
	show.Pending.Question = "Some context line.\nQuestion: Which store?\nOptions:\n1. sqlite: one file\n2. json: plain\nRecommend: 2 - no dependency"
	log = showDouble(show, nil)
	_, out, _ = runCLI(&App{Call: log.call}, "show", "r1")
	if strings.Count(out, "Question: Which store?") != 1 || strings.Count(out, "1. sqlite: one file") != 1 {
		t.Errorf("the question block is printed twice:\n%s", out)
	}
}

func TestShowTakesRunAndOptionalName(t *testing.T) {
	for _, args := range [][]string{{"show"}, {"show", "r1", "a.md", "extra"}, {"show", "--json"}} {
		log := &callLog{}
		code, out, errs := runCLI(&App{Call: log.call}, args...)
		if code != ExitUsage || len(log.calls) != 0 || !strings.Contains(out+errs, "show takes RUN [NAME]") {
			t.Errorf("%q: code %d, stdout %q, stderr %q, calls %q", args, code, out, errs, log.calls)
		}
	}
	log := &callLog{}
	if code, _, _ := runCLI(&App{Call: log.call}, "show", "r1", "--bogus"); code != ExitUsage || len(log.calls) != 0 {
		t.Errorf("unknown flag: code %d, calls %q", code, log.calls)
	}
	code, out, _ := runCLI(&App{}, "help")
	if code != ExitOK || !strings.Contains(out, "ork show RUN [NAME]") {
		t.Errorf("ork help lists no show command:\n%s", out)
	}
}

// ork show over the daemon's socket: run.show and run.report carry the wire
// types, and the files come from the run directory (S297, wire test).
func TestShowOverTheDaemonSocket(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "p", Path: "/projects/p"})
	if code, _, stderr := run(t, w.app, "projects", "list"); code != 0 { // start the daemon before seeding
		t.Fatalf("start daemon: exit %d: %s", code, stderr)
	}
	store := &runrec.Store{Dir: filepath.Join(w.paths.Dir, "runs")}
	_, err := store.Create(runrec.Record{ID: "r1", Project: "p", Item: "S9", State: runrec.StatePaused,
		Pending: &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateReview, Role: "reviewer", Round: 1, Reason: showLongReason},
		Roles:   []runrec.RoleRecord{{Role: "reviewer", Round: 1, Exit: "ok", Report: "reviewer-1.md"}},
		Checks:  []runrec.CheckRecord{{Round: 1, Command: "go test", Log: "checks-1.log"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"reviewer-1.md": "THE REFUSED REPORT", "checks-1.log": "THE CHECK LOG", "other.log": "unlisted"} {
		if err := os.WriteFile(filepath.Join(store.RunDir("r1"), name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	code, out, stderr := run(t, w.app, "show", "r1")
	if code != 0 || !strings.Contains(out, showLongReason) || !strings.HasSuffix(out, "-- reviewer-1.md --\nTHE REFUSED REPORT\nnext: ork retry r1\n") || !strings.Contains(out, "log checks-1.log") {
		t.Fatalf("exit %d, stderr %q:\n%s", code, stderr, out)
	}
	if code, out, _ := run(t, w.app, "show", "r1", "checks-1.log"); code != 0 || out != "THE CHECK LOG\n" {
		t.Fatalf("log: exit %d, %q", code, out)
	}
	code, out, stderr = run(t, w.app, "show", "r1", "other.log")
	if code != 1 || out != "" || !strings.Contains(stderr, "no report or log other.log for run r1; names: reviewer-1.md, checks-1.log") {
		t.Fatalf("unlisted log: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	var reply daemon.ShowReply
	code, out, _ = run(t, w.app, "show", "r1", "--json")
	mustJSON(t, out, &reply)
	if code != 0 || reply.Pending == nil || reply.Pending.Reason != showLongReason || reply.LatestReport != "reviewer-1.md" || len(reply.Checks) != 1 {
		t.Fatalf("--json: exit %d: %s", code, out)
	}
	if code, _, stderr := run(t, w.app, "show", "nope"); code != 1 || !strings.Contains(stderr, "no run nope") {
		t.Fatalf("unknown run: exit %d: %q", code, stderr)
	}
}

// Free text from a run record (a reason, a finding, a denied command) can hold
// terminal controls; ork show draws none of them.
func TestShowDropsTerminalControls(t *testing.T) {
	show := pausedShow()
	show.Pending.Reason = "bad \x1b[31mred\x1b[0m reason\x1b]0;title\x07"
	show.Reason = show.Pending.Reason
	show.Findings[0].Text = "finding \x1b[2Jtext"
	show.Roles[0].DeniedFirst = []string{"Bash: \x1b[31mrm\x1b[0m"}
	show.Checks[0].Command = "go \x1b[31mtest\x1b[0m"
	show.Checks[0].Deferred = []string{"S5: \x1b[2Jstale"}
	show.Findings[0].Answer = "an \x1b[31manswer\x1b[0m"
	log := showDouble(show, map[string]string{"reviewer-3-attempt-3.md": "body"})
	code, out, _ := runCLI(&App{Call: log.call}, "show", "r1")
	if code != ExitOK || strings.ContainsRune(out, 0x1b) || strings.ContainsRune(out, 0x07) || !strings.Contains(out, "bad red reason") || !strings.Contains(out, "finding text") || !strings.Contains(out, "denied 5: Bash: rm") ||
		!strings.Contains(out, "go test") || !strings.Contains(out, "deferred: S5: stale") || !strings.Contains(out, "answer: an answer") {
		t.Fatalf("code %d:\n%q", code, out)
	}

	// A question pause: the question text, the title, the options and the reason of the recommendation.
	ask := &runrec.OwnerQuestion{Title: "T\x1b[31mitle", Options: []runrec.Option{{Label: "a\x1b[0m", Summary: "s\x1b]0;x\x07um"}, {Label: "b", Summary: "d"}}, Recommend: 1, Reason: "be\x1b[2Jcause"}
	q := daemon.ShowReply{ID: "r1", Item: "S9", State: "paused", Next: "ork answer r1 TEXT", Reason: "r",
		Pending: &daemon.ShowPending{Kind: "question", State: "review", Role: "reviewer", Round: 1, Reason: "r", Question: "ctx \x1b[31mline", Ask: ask, Key: "a.b\x1b[0m"}}
	code, out, _ = runCLI(&App{Call: showDouble(q, nil).call}, "show", "r1")
	if code != ExitOK || strings.ContainsRune(out, 0x1b) || strings.ContainsRune(out, 0x07) || !strings.Contains(out, "ctx line") || !strings.Contains(out, "Question: Title") || !strings.Contains(out, "1. a: sum") || !strings.Contains(out, "Recommend: 1 - because") {
		t.Fatalf("question pause: code %d:\n%q", code, out)
	}
}

func TestShowNameListedButUnreadableIsNotAnUnknownName(t *testing.T) {
	log := showDouble(pausedShow(), map[string]string{}) // every file is gone
	code, out, errs := runCLI(&App{Call: log.call}, "show", "r1", "checks-2.log")
	want := "ork: checks-2.log is listed for run r1 but cannot be read\n"
	if code != ExitFailure || out != "" || errs != want {
		t.Fatalf("code %d, stdout %q, stderr %q, want %q", code, out, errs, want)
	}
	if strings.Contains(errs, "names:") {
		t.Fatalf("a listed name got the names list: %q", errs)
	}
}

func TestShowSkipsEmptyQuestionAndZeroRecommend(t *testing.T) {
	show := daemon.ShowReply{ID: "r1", Item: "S9", State: "paused", Next: "ork answer r1 TEXT", Reason: "why",
		Pending: &daemon.ShowPending{Kind: "question", State: "review", Role: "reviewer", Round: 1, Reason: "why"}}
	_, out, _ := runCLI(&App{Call: showDouble(show, nil).call}, "show", "r1")
	if strings.Contains(out, "question:") || strings.Contains(out, "Recommend") {
		t.Errorf("a bare question heading:\n%s", out)
	}
	show.Pending.Ask = &runrec.OwnerQuestion{Title: "T", Options: []runrec.Option{{Label: "a", Summary: "b"}, {Label: "c", Summary: "d"}}}
	_, out, _ = runCLI(&App{Call: showDouble(show, nil).call}, "show", "r1")
	if !strings.Contains(out, "question:\n  Question: T") || !strings.Contains(out, "1. a: b") || strings.Contains(out, "Recommend") {
		t.Errorf("recommend 0 is printed, or the ask is not:\n%s", out)
	}
}

func TestShowIndentsContinuationLinesOfAnswersAndDeferredChecks(t *testing.T) {
	show := pausedShow()
	show.Findings[0].Answer = "yes\nbecause of the wire"
	show.Checks[0].Deferred = []string{"S5: stale\nsecond line"}
	_, out, _ := runCLI(&App{Call: showDouble(show, map[string]string{}).call}, "show", "r1")
	for _, want := range []string{"    answer: yes\n      because of the wire\n", "    deferred: S5: stale\n      second line\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestShowHelpEntryIsShortAndWithin80Columns(t *testing.T) {
	var entry []string
	for _, line := range strings.Split(usageText, "\n") {
		if strings.HasPrefix(line, "  ork show ") {
			entry = append(entry, line)
		} else if len(entry) > 0 && strings.HasPrefix(line, "      ") {
			entry = append(entry, line)
		} else if len(entry) > 0 {
			break
		}
	}
	if len(entry) < 2 || len(entry) > 3 {
		t.Fatalf("help entry has %d lines: %q", len(entry), entry)
	}
	for _, line := range entry {
		if len(line) > 80 {
			t.Errorf("help line of %d columns: %q", len(line), line)
		}
	}
}
