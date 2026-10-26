// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

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
	h.deliver(press("enter"))
	if v := h.view(); !strings.Contains(v, "Run engine") {
		t.Fatalf("Enter must open the pane:\n%s", v)
	}
	return h
}

func TestPaneOffersLaterFindingsAsSlices(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne, laterTwo, ownerNote)
	v := h.view()
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
	v := h.view()
	if !strings.Contains(v, "slicer add 'Add a --json flag to ork status' --discovered-from S22?") || !strings.Contains(v, "[y/N] (Enter is No)") {
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
	if v := h.view(); !strings.Contains(v, "'Retire the old cache'") {
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
	if v := h.view(); cmd != nil || !strings.Contains(v, "--discovered-from S22") || !strings.Contains(v, "is pending") || h.m.confirm != nil {
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
	v := h.view()
	if !strings.Contains(v, "slicer add: locked") || !strings.Contains(v, "f file slice") {
		t.Errorf("failure and offer:\n%s", v)
	}
}

func TestPaneFileNeedsADaemonThatCanFile(t *testing.T) {
	h := findingsHarness(t, "awaiting-approval", laterOne)
	h.m.cfg.Live = &actionLive{src: h.src} // no FileLater
	h.deliver(press("f"))
	if v := h.view(); h.m.confirm != nil || !strings.Contains(v, "cannot file slices") {
		t.Errorf("a connection without FileLater:\n%s", v)
	}
	h.m.cfg.Live = nil
	h.deliver(press("f"))
	if v := h.view(); h.m.confirm != nil || !strings.Contains(v, "No daemon connection") {
		t.Errorf("no connection:\n%s", v)
	}

	// The connection changed between the question and the answer.
	h.m.cfg.Live = &fileLive{actionLive: actionLive{src: h.src}}
	h.deliver(press("f"))
	h.m.cfg.Live = &actionLive{src: h.src}
	h.deliver(press("y"))
	if v := h.view(); !strings.Contains(v, "cannot file slices") {
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
			v := h.view()
			if strings.Contains(v, "f file slice") || strings.Contains(v, "slicer add") || len(live.calls) != 0 {
				t.Errorf("offered with nothing to file:\n%s", v)
			}
		})
	}
}

func TestPaneFileKeyWorksWhileTheRunIsWorking(t *testing.T) {
	h := findingsHarness(t, "implement", laterOne)
	if v := h.view(); !strings.Contains(v, "f file slice") || !strings.Contains(v, "x stop") {
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
	v := h.view()
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
	v := h.view()
	if !strings.Contains(v, "Filed as slices: S99") || !strings.Contains(v, `1 later finding; next to file: "Retire the old cache"`) || !strings.Contains(v, "f file slice; esc close pane") {
		t.Errorf("stopped pane:\n%s", v)
	}
	h.deliver(press("f"))
	if v := h.view(); !strings.Contains(v, "'Retire the old cache'") || strings.Contains(v, "Done already") {
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

func TestPaneFileHintSitsRightBeforeEsc(t *testing.T) {
	for _, tc := range []struct{ state, pending, want string }{
		{"awaiting-approval", "", "a approve; x stop; f file slice; esc close pane"},
		{"implement", "", "x stop; f file slice; esc close pane"},
		{"paused", "shape", "t retry; f file slice; esc close pane"},
		{"paused", "budget", "t retry: one more role round; f file slice; esc close pane"},
		{"stopped", "", "f file slice; esc close pane"},
	} {
		t.Run(tc.state+"/"+tc.pending, func(t *testing.T) {
			h := findingsHarness(t, tc.state, laterOne)
			h.m.byName["ork"].snap.EngineRuns[0].Pending = tc.pending
			if v := h.view(); !strings.Contains(v, tc.want) {
				t.Errorf("key line lacks %q:\n%s", tc.want, v)
			}
		})
	}
}
