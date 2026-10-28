// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// fileLive is a daemon that files later findings: it keeps the calls and marks
// the finding filed in the snapshot the way the daemon's next read would.
type fileLive struct {
	actionLive
	calls []string
	err   error
}

func (l *fileLive) FileLater(_ context.Context, run string, n int) (string, error) {
	l.calls = append(l.calls, fmt.Sprintf("%s#%d", run, n))
	if l.err != nil {
		return "", l.err
	}
	s := l.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
	s.EngineRuns[0].Findings = append([]source.EngineFinding(nil), s.EngineRuns[0].Findings...)
	s.EngineRuns[0].Findings[n-1].Filed = "S130"
	l.src.set(s)
	return "filed as S130", nil
}

var (
	laterOne  = source.EngineFinding{N: 1, Kind: runrec.FindingLater, Title: "Add a --json flag to ork status"}
	laterTwo  = source.EngineFinding{N: 2, Kind: runrec.FindingLater, Title: "Retire the old cache"}
	ownerNote = source.EngineFinding{N: 3, Kind: runrec.FindingOwner, Title: "Should the cache be shared?", Ask: &runrec.OwnerQuestion{
		Title:   "Should the cache be shared?",
		Options: []runrec.Option{{Label: "shared", Summary: "one cache"}, {Label: "per-project", Summary: "one each"}}, Recommend: 2, Reason: "isolation"}}
)

func findingsHarness(t *testing.T, state string, fs ...source.EngineFinding) *harness {
	t.Helper()
	h := actionHarness(t, state, "")
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
	s.EngineRuns[0].Findings = fs
	h.src.set(s)
	h.m.byName["ork"].snap.EngineRuns[0].Findings = fs
	h.m.cfg.Live = &fileLive{actionLive: actionLive{src: h.src}}
	if state == "committed" {
		h.deliver(press("G"))
	}
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "Run engine") {
		t.Fatalf("Enter must open the pane:\n%s", v)
	}
	return h
}

func TestPaneOffersLaterFindingsAsSlices(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne, laterTwo, ownerNote)
	v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), "")
	for _, want := range []string{"Add a --json flag to ork status", "f file slice", "a approve", "Should the cache be shared?"} {
		if !strings.Contains(v, want) {
			t.Errorf("pane lacks %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, `2 later findings; next to file: "Add a --json flag to ork status"`) || strings.Contains(v, "f files") {
		t.Errorf("pane does not count the later findings, or names the key twice:\n%s", v)
	}
	if !strings.Contains(v, "Owner question, not blocking: Should the cache be shared?") || !strings.Contains(v, "Recommended: per-project.") {
		t.Errorf("pane lacks the owner question line:\n%s", v)
	}
}

func TestPaneFileKeyAsksThenFilesTheFirstLaterFinding(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne, laterTwo)
	live := h.m.cfg.Live.(*fileLive)
	h.deliver(press("f"))
	v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), "")
	if !strings.Contains(v, "slicer add 'Add a --json flag to ork status' --discovered-from S22") || !strings.Contains(v, "[y/N] (Enter is No)") {
		t.Fatalf("f must show the exact command and ask:\n%s", v)
	}
	if len(live.calls) != 0 {
		t.Fatalf("nothing is filed before the owner confirms: %v", live.calls)
	}
	h.deliver(press("y"))
	if len(live.calls) != 1 || live.calls[0] != "engine#1" {
		t.Fatalf("calls %v", live.calls)
	}
	v = h.view()
	if !strings.Contains(v, "filed as S130") {
		t.Errorf("the owner is told the new slice:\n%s", v)
	}
	// The next press offers the second finding.
	h.deliver(press("f"))
	if v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), ""); !strings.Contains(v, "'Retire the old cache'") {
		t.Errorf("the filed finding is not offered again:\n%s", v)
	}
	h.deliver(press("y"))
	if len(live.calls) != 2 || live.calls[1] != "engine#2" {
		t.Errorf("the second confirmation files the second finding: %v", live.calls)
	}
}

func TestPaneFileWhileOneIsPendingIsRefused(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne, laterTwo)
	h.m.Update(press("f"))
	h.m.Update(press("y")) // hold the returned command: the call stays in flight
	_, cmd := h.m.Update(press("f"))
	if v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), ""); cmd != nil || !strings.Contains(v, "--discovered-from S22") || !strings.Contains(v, "is pending") || h.m.confirm != nil {
		t.Fatalf("a second filing was not refused while the first is in flight:\n%s", v)
	}
}

func TestPaneFileKeyCancels(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne)
	live := h.m.cfg.Live.(*fileLive)
	h.deliver(press("f"))
	h.deliver(press("n"))
	h.deliver(press("f"))
	h.deliver(press("enter"))
	if len(live.calls) != 0 {
		t.Errorf("a cancelled confirmation filed: %v", live.calls)
	}
}

func TestPaneFileFailureIsShownAndKeepsTheOffer(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne)
	live := h.m.cfg.Live.(*fileLive)
	live.err = fmt.Errorf("slicer add: locked")
	h.deliver(press("f"))
	h.deliver(press("y"))
	v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), "")
	if !strings.Contains(v, "slicer add: locked") || !strings.Contains(v, "f file slice") {
		t.Errorf("failure and offer:\n%s", v)
	}
}

func TestPaneFileNeedsADaemonThatCanFile(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne)
	h.m.cfg.Live = &actionLive{src: h.src} // no FileLater
	h.deliver(press("f"))
	if v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), ""); h.m.confirm != nil || !strings.Contains(v, "cannot file slices") {
		t.Errorf("a connection without FileLater:\n%s", v)
	}
	h.m.cfg.Live = nil
	h.deliver(press("f"))
	if v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), ""); h.m.confirm != nil || !strings.Contains(v, "No daemon connection") {
		t.Errorf("no connection:\n%s", v)
	}

	// The connection changed between the question and the answer.
	h.m.cfg.Live = &fileLive{actionLive: actionLive{src: h.src}}
	h.deliver(press("f"))
	h.m.cfg.Live = &actionLive{src: h.src}
	h.deliver(press("y"))
	if v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), ""); !strings.Contains(v, "cannot file slices") {
		t.Errorf("the confirmed filing reports the missing support:\n%s", v)
	}
}

func TestPaneWithoutLaterFindingsHasNoFileKey(t *testing.T) {
	for name, fs := range map[string][]source.EngineFinding{
		"none":       nil,
		"owner only": {ownerNote},
		"filed":      {{N: 1, Kind: runrec.FindingLater, Title: "Done already", Filed: "S99"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := findingsHarness(t, "awaiting-approval", fs...)
			live := h.m.cfg.Live.(*fileLive)
			h.deliver(press("f"))
			v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), "")
			if strings.Contains(v, "f file slice") || strings.Contains(v, "slicer add") || len(live.calls) != 0 {
				t.Errorf("offered with nothing to file:\n%s", v)
			}
		})
	}
}

func TestPaneFileKeyWorksWhileTheRunIsWorking(t *testing.T) {
	h := findingsHarness(t, "implement", laterOne)
	if v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), ""); !strings.Contains(v, "f file slice") || !strings.Contains(v, "x stop") {
		t.Fatalf("running pane:\n%s", v)
	}
	h.deliver(press("f"))
	h.deliver(press("y"))
	if live := h.m.cfg.Live.(*fileLive); len(live.calls) != 1 {
		t.Errorf("calls %v", live.calls)
	}
}

func TestPaneFileKeyIsTextInAFreeAnswer(t *testing.T) {
	h := findingsHarness(t, "paused", laterOne, ownerNote)
	h.m.byName["ork"].snap.EngineRuns[0].Pending = "question"
	h.deliver(press("f"))
	v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), "")
	if live := h.m.cfg.Live.(*fileLive); len(live.calls) != 0 || h.m.confirm != nil {
		t.Errorf("f in a question pane asked to file: %v", live.calls)
	}
	if h.m.pane.input.Value() != "f" {
		t.Errorf("f is text in the answer field: %q", h.m.pane.input.Value())
	}
	for _, not := range []string{"f file slice", "next to file", "Owner question, not blocking"} {
		if strings.Contains(v, not) {
			t.Errorf("a question pane shows %q:\n%s", not, v)
		}
	}
}

func TestPaneFiledFindingsAreListedNotOffered(t *testing.T) {
	h := findingsHarness(t, "stopped", source.EngineFinding{N: 1, Kind: runrec.FindingLater, Title: "Done already", Filed: "S99"}, laterTwo)
	v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), "")
	if !strings.Contains(v, "Filed as slices: S99") || !strings.Contains(v, `1 later finding; next to file: "Retire the old cache"`) || !strings.Contains(v, "f file slice") {
		t.Errorf("stopped pane:\n%s", v)
	}
	h.deliver(press("f"))
	if v := h.view() + "\n" + sgr.ReplaceAllString(h.m.hintLine(200), ""); !strings.Contains(v, "'Retire the old cache'") || strings.Contains(v, "Done already") {
		t.Errorf("the second finding is offered, not the filed one:\n%s", v)
	}
}

func TestPaneFindingsGoldens(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprint(mono), func(t *testing.T) {
			h := findingsHarness(t, "awaiting-approval", laterOne, laterTwo, ownerNote)
			h.m.cfg.Theme = NewTheme(mono)
			checkGolden(t, fmt.Sprintf("pane-findings-mono-%t.txt", mono), h.m.View().Content)
		})
	}
}

// The live connection files through the daemon's run.file.
func TestDaemonFileLaterWire(t *testing.T) {
	var _ laterFiler = (*Daemon)(nil)
	d, _, _, _ := requestServer(t, func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var a daemon.RunAction
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		if method != "run.file" || a != (daemon.RunAction{Run: "engine", Finding: 2}) {
			return nil, fmt.Errorf("wrong request: %s %s", method, params)
		}
		return daemon.RunReply{Message: "filed as S130"}, nil
	})
	message, err := d.FileLater(context.Background(), "engine", 2)
	if err != nil || message != "filed as S130" {
		t.Fatalf("reply: %q, %v", message, err)
	}
}

func TestPaneFileHintUsesTheHintBar(t *testing.T) {
	for _, state := range []string{"awaiting-approval", "implement", "paused", "stopped"} {
		t.Run(state, func(t *testing.T) {
			h := findingsHarness(t, state, laterOne)
			hint := wideHint(h)
			if !strings.Contains(hint, "f file slice") || !strings.HasSuffix(hint, "esc close pane") {
				t.Errorf("hint: %q", hint)
			}
			if strings.Contains(strings.Join(h.m.paneLines(200, 40, ""), "\n"), "f file slice") {
				t.Error("pane repeats file keys")
			}
		})
	}
}

func TestReadyToLandPaneFilesLater(t *testing.T) {
	h := findingsHarness(t, "committed", laterOne)
	h.deliver(press("f"))
	if h.m.confirm == nil {
		t.Fatal("Ready-to-land pane cannot file")
	}
	if v := h.view() + "\n" + wideHint(h); !strings.Contains(v, "Why section") {
		t.Fatalf("confirmation lacks Why section: %s", v)
	}
	h.deliver(press("y"))
	if len(h.m.cfg.Live.(*fileLive).calls) != 1 {
		t.Fatal("committed finding was not filed")
	}
}

func TestFindingsFooterCapped(t *testing.T) {
	fs := []source.EngineFinding{laterOne}
	for n := 0; n < 10; n++ {
		f := ownerNote
		f.N = n + 2
		f.Title = fmt.Sprintf("Question %d?", n)
		fs = append(fs, f)
	}
	h := findingsHarness(t, "awaiting-approval", fs...)
	later, owner := h.m.findingLines(100)
	text := strings.Join(later, "\n")
	for _, lines := range owner {
		text += "\n" + strings.Join(lines, "\n")
	}
	if !strings.Contains(text, "+7 more") || len(owner) > 4 {
		t.Fatalf("uncapped footer: %s", text)
	}
}

func TestNeedsYouIncludesFindings(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne, ownerNote)
	if got := h.m.byName["ork"].needs(); got != 3 {
		t.Fatalf("project needs %d", got)
	}
	h.deliver(press("esc"))
	if v := h.view(); !strings.Contains(v, "Needs you 3") {
		t.Fatalf("aggregate ignores findings: %s", v)
	}
}

func TestPaneAnswersNonBlockingFinding(t *testing.T) {
	h := findingsHarness(t, "implement", ownerNote)
	h.deliver(press("o"))
	if !h.m.paneQuestion() || h.m.paneAsk() == nil {
		t.Fatal("o did not open structured finding question")
	}
	if h.m.byName["ork"].snap.EngineRuns[0].State != "implement" {
		t.Fatal("opening question paused run")
	}
	h.deliver(press("2"))
	if h.m.confirm == nil || h.m.confirm.finding != 3 {
		t.Fatalf("finding answer not targeted: %+v", h.m.confirm)
	}
}

// AnswerAction models the daemon's finding response without changing run state.
func (l *fileLive) AnswerAction(_ context.Context, a daemon.RunAction) (string, error) {
	if a.Finding == 0 {
		return "", fmt.Errorf("missing finding number")
	}
	l.calls = append(l.calls, fmt.Sprintf("answer %s#%d %s", a.Run, a.Finding, a.Text))
	if l.err != nil {
		return "", l.err
	}
	s := l.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns = append([]source.EngineRun(nil), s.EngineRuns...)
	s.EngineRuns[0].Findings = append([]source.EngineFinding(nil), s.EngineRuns[0].Findings...)
	for i := range s.EngineRuns[0].Findings {
		if s.EngineRuns[0].Findings[i].N == a.Finding {
			s.EngineRuns[0].Findings[i].Answer = a.Text
		}
	}
	l.src.set(s)
	return "answer recorded", nil
}

func TestPaneFindingAnswerSendsAndClearsOffer(t *testing.T) {
	h := findingsHarness(t, "implement", ownerNote)
	h.deliver(press("o"))
	h.deliver(press("2"))
	h.deliver(press("y"))
	live := h.m.cfg.Live.(*fileLive)
	if len(live.calls) != 1 || live.calls[0] != "answer engine#3 per-project: one each" {
		t.Fatalf("calls %v", live.calls)
	}
	if h.m.pane.finding != 0 || h.m.paneQuestion() {
		t.Fatal("answer form stayed open")
	}
	r := h.m.byName["ork"].snap.EngineRuns[0]
	if r.State != "implement" || r.FindingNeeds() != 0 {
		t.Fatalf("run after answer %+v", r)
	}
}

func TestPaneFindingAnswerFailureKeepsQuestion(t *testing.T) {
	h := findingsHarness(t, "implement", ownerNote)
	h.m.cfg.Live.(*fileLive).err = fmt.Errorf("adj failed")
	h.deliver(press("o"))
	h.deliver(press("2"))
	h.deliver(press("y"))
	if !h.m.paneQuestion() || !strings.Contains(h.view(), "adj failed") {
		t.Fatal("failed answer lost question or error")
	}
	h.deliver(press("esc"))
	if h.m.pane == nil || h.m.paneQuestion() {
		t.Fatal("escape did not return to run")
	}
}

func TestFindingPaneAnchorsAtSelectedRunRow(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne, ownerNote)
	ov := h.m.overview(100)
	if ov.paneAt != ov.sel {
		t.Fatalf("pane anchor %d, selected row %d", ov.paneAt, ov.sel)
	}
	h.m.cursor = 2
	ov = h.m.overview(100)
	if ov.paneAt != ov.sel {
		t.Fatalf("pane anchor %d, selected finding %d", ov.paneAt, ov.sel)
	}
}

func TestFindingRowsDoNotInflatePendingQuestions(t *testing.T) {
	h := findingsHarness(t, "paused", laterOne, ownerNote)
	h.m.byName["ork"].snap.EngineRuns[0].Pending = "question"
	text := strings.Join(h.m.overview(100).body, "\n")
	if !strings.Contains(text, "? owner questions 1") || strings.Contains(text, "? owner questions 3") {
		t.Fatalf("question count inflated: %s", text)
	}
}

func TestFindingHintsFitAndReturnToRun(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne, ownerNote)
	hint := hintLine(h.at(80, 24))
	for _, want := range []string{"o answer", "f file slice"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint lacks %q: %s", want, hint)
		}
	}
	h.deliver(press("o"))
	if hint := hintLine(h.view()); !strings.Contains(hint, "esc back to run") {
		t.Errorf("wrong escape hint: %s", hint)
	}
	h.deliver(press("2"))
	if cmd := h.m.confirm.command(); !strings.Contains(cmd, ownerNote.Title) || strings.Contains(cmd, "finding 3") {
		t.Errorf("confirmation lacks title: %s", cmd)
	}
}

func TestFindingsFooterDoesNotReplaceOneFindingWithCounter(t *testing.T) {
	var fs []source.EngineFinding
	for n := 0; n < 4; n++ {
		f := ownerNote
		f.N = n + 1
		f.Title = fmt.Sprintf("Owner %d", n)
		fs = append(fs, f)
	}
	h := findingsHarness(t, "awaiting-approval", fs...)
	_, lines := h.m.findingLines(100)
	if len(lines) != 4 || strings.Contains(strings.Join(lines[3], " "), "+1 more") {
		t.Fatalf("single finding replaced by counter: %v", lines)
	}
}

func TestFileConfirmationEndsWithQuestion(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne)
	h.deliver(press("f"))
	prompt := h.m.confirm.prompt()
	if !strings.HasSuffix(prompt, "? [y/N] (Enter is No)") || strings.Index(prompt, "Why section") > strings.Index(prompt, "[y/N]") {
		t.Fatalf("wrong confirmation order: %s", prompt)
	}
}

func TestSettledFindingsLeaveCountsButStayInPane(t *testing.T) {
	for _, state := range []string{"landed", "dismissed", "superseded"} {
		t.Run(state, func(t *testing.T) {
			h := findingsHarness(t, "stopped", laterOne, ownerNote)
			p := h.m.byName["ork"]
			switch state {
			case "landed":
				p.snap.EngineRuns[0].Landed = true
			case "dismissed":
				p.snap.EngineRuns[0].Dismissed = true
			case "superseded":
				old := p.snap.EngineRuns[0]
				p.snap.EngineRuns = append(p.snap.EngineRuns, source.EngineRun{ID: "new", Item: old.Item, State: "implement", CreatedAt: old.CreatedAt.Add(time.Second)})
			}
			rows, _ := h.m.rows()
			for _, r := range rows {
				if r.group == "Findings" {
					t.Fatal("settled finding still listed")
				}
			}
			later, owner := h.m.findingLines(100)
			if len(later) == 0 || len(owner) != 1 {
				t.Fatal("settled findings lost from run pane")
			}
		})
	}
}

func TestFileConfirmationExplainsProposedScores(t *testing.T) {
	f := laterOne
	f.Scores = &runrec.FindingScores{Importance: 3, Urgency: 2, Effort: 1}
	h := findingsHarness(t, "awaiting-approval", f)
	h.deliver(press("f"))
	if p := h.m.confirm.prompt(); !strings.Contains(p, "slicer set") || !strings.HasSuffix(p, "? [y/N] (Enter is No)") {
		t.Fatalf("missing scoring disclosure: %s", p)
	}
}

func TestFindingsHintsAndFooterGoldens(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		h := findingsHarness(t, "stopped", laterOne)
		fs := []source.EngineFinding{laterOne}
		for n := 0; n < 8; n++ {
			fs = append(fs, source.EngineFinding{N: n + 2, Kind: runrec.FindingOwner, Title: fmt.Sprintf("Owner %d?", n), Ask: ownerNote.Ask})
		}
		h.m.byName["ork"].snap.EngineRuns[0].Findings = fs
		view := h.at(sz.w, sz.h)
		if !strings.Contains(hintLine(view), "o answer") || !strings.Contains(hintLine(view), "f file slice") {
			t.Fatalf("%dx%d missing finding keys", sz.w, sz.h)
		}
		checkGolden(t, fmt.Sprintf("pane-finding-actions-%dx%d.txt", sz.w, sz.h), view)
		// The full-height pane makes the count visible at both requested sizes.
		pane := strings.Join(h.m.paneLines(sz.w, sz.h, ""), "\n")
		if !strings.Contains(pane, "+5 more owner findings") {
			t.Fatalf("%dx%d count hidden: %s", sz.w, sz.h, pane)
		}
		checkGolden(t, fmt.Sprintf("pane-finding-footer-%dx%d.txt", sz.w, sz.h), pane)
	}
}

func TestKeyedFindingWithoutAdjDoesNotPromiseDecision(t *testing.T) {
	f := ownerNote
	f.Key = "cache.mode"
	h := findingsHarness(t, "implement", f)
	h.m.byName["ork"].reg.Adj = false
	h.deliver(press("o"))
	if text := strings.Join(h.m.paneLines(100, 30, ""), "\n"); strings.Contains(text, "Answer also recorded in adj") || strings.Contains(text, "ADJ_DECIDED_BY unset") {
		t.Fatalf("no-adj question promises a decision: %s", text)
	}
}

func TestFindingPaneFallsBackToFirstMatchingRow(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne, ownerNote)
	first := h.m.overview(100).paneAt
	p := h.m.byName["ork"]
	p.snap.EngineRuns = append(p.snap.EngineRuns, source.EngineRun{ID: "other", Item: "S99", State: "implement"})
	h.m.cursor = len(h.m.cursorRows()) - 1
	if got := h.m.overview(100).paneAt; got != first {
		t.Fatalf("fallback anchor %d, first match %d", got, first)
	}
}

// The blank line under a reason needs room for the progress line after it; on
// a short pane the blank goes and the progress line stays (S134).
func TestShortPaneKeepsProgressLineBeforeSeparator(t *testing.T) {
	h := findingsHarness(t, "stopped", laterOne)
	fs := []source.EngineFinding{laterOne}
	for n := 0; n < 8; n++ {
		fs = append(fs, source.EngineFinding{N: n + 2, Kind: runrec.FindingOwner, Title: fmt.Sprintf("Owner %d?", n), Ask: ownerNote.Ask})
	}
	h.m.byName["ork"].snap.EngineRuns[0].Findings = fs
	if view := h.at(80, 24); !strings.Contains(view, "[......]") {
		t.Fatalf("the 80x24 pane lost its progress line:\n%s", view)
	}
}
