// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

type launchLive struct {
	fakeLive
	plans, starts     []daemon.RunParams
	plan              daemon.RunPlan
	planErr, startErr error
	checks            [][]string
	check             func([]string) (*daemon.SpecCheckResult, error)
	answers           []daemon.SpecAnswerParams
	answerErr         error
}

func (l *launchLive) Plan(_ context.Context, p daemon.RunParams) (*daemon.RunPlan, error) {
	l.plans = append(l.plans, p)
	plan := l.plan
	return &plan, l.planErr
}
func (l *launchLive) Start(_ context.Context, p daemon.RunParams) (runrec.Record, error) {
	l.starts = append(l.starts, p)
	return runrec.Record{ID: "new-run"}, l.startErr
}
func (l *launchLive) SpecCheck(_ context.Context, project string, items []string) (*daemon.SpecCheckResult, error) {
	l.checks = append(l.checks, append([]string{project}, items...))
	if l.check != nil {
		return l.check(items)
	}
	out := &daemon.SpecCheckResult{}
	for _, id := range items {
		out.Items = append(out.Items, daemon.SpecItemResult{Item: id, Ready: true})
	}
	return out, nil
}
func (l *launchLive) SpecAnswer(_ context.Context, p daemon.SpecAnswerParams) (*daemon.SpecAnswerResult, error) {
	l.answers = append(l.answers, p)
	return &daemon.SpecAnswerResult{Item: p.Item, Ready: true}, l.answerErr
}
func launchHarness(t *testing.T) (*harness, *launchLive) {
	t.Helper()
	h := roadmapHarness(t)
	l := &launchLive{plan: daemon.RunPlan{
		Project: registry.Project{Name: "ork"}, Item: "S90", Worktree: "/project/.worktrees/S90", Branch: "S90", CheckCommand: "go test ./...",
		Lineup: map[string][]runrec.LineupEntry{
			"implementer": {{Vendor: "Codex", Model: "gpt", Effort: "high"}},
			"reviewer":    {{Vendor: "Claude", Model: "opus", Effort: "medium"}},
			"gate":        {{Vendor: "Codex", Model: "gpt", Effort: "low"}},
		}, Permissions: map[string]engine.Permission{"reviewer": {ReadOnly: true}},
	}}
	h.m.cfg.Live = l
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return h, l
}

// launchPlanning opens the launch list and picks the first slice. The pick
// asks nothing and returns the run.plan command (adj 189).
func launchPlanning(t *testing.T, h *harness) tea.Cmd {
	t.Helper()
	h.m.Update(press("l"))
	_, cmd := h.m.Update(press("enter"))
	if cmd == nil {
		t.Fatalf("missing plan command:\n%s", h.view())
	}
	return cmd
}
func launchConfirmed(t *testing.T, h *harness) {
	t.Helper()
	h.deliver(launchPlanning(t, h)())
	// A plan with no check command asks nothing: the bar says it is refused.
	if v := h.view(); !strings.Contains(v, "ork run ork S90? [y/N] (Enter is No)") && !strings.Contains(v, "ork run ork S90: refused (n or esc closes)") {
		t.Fatalf("missing confirmation:\n%s", v)
	}
}
func TestLaunchListsOnlyReadySlices(t *testing.T) {
	h, l := launchHarness(t)
	h.m.Update(press("l"))
	v := h.view()
	if rowsWith(v, "S90", "Ninety", "M", "effort 2") != 1 {
		t.Fatalf("claimed ready slice absent:\n%s", v)
	}
	for _, id := range []string{"S02", "S03", "S04", "SECRET-CLAIM"} {
		if strings.Contains(v, id) {
			t.Errorf("unexpected %s:\n%s", id, v)
		}
	}
	if len(l.plans)+len(l.starts) != 0 {
		t.Fatal("list wrote")
	}
	h.m.Update(press("esc"))
	h.m.projects[0].snap.Deps.Err = errors.New("deps failed")
	h.m.Update(press("l"))
	if v = h.view(); !strings.Contains(v, "No ready slices") || strings.Contains(v, "S90") {
		t.Fatalf("deps error permits launch:\n%s", v)
	}
	if _, c := h.m.Update(press("enter")); c != nil || strings.Contains(h.view(), "pending: run.plan") {
		t.Fatal("empty list picked a slice")
	}
}
func TestLaunchPlanBeforeConfirm(t *testing.T) {
	h, l := launchHarness(t)
	c := launchPlanning(t, h)
	if len(l.plans) != 0 || len(l.starts) != 0 || !strings.Contains(h.view(), "pending: run.plan") {
		t.Fatal("Plan must run in command")
	}
	h.deliver(c())
	want := daemon.RunParams{Project: "ork", Item: "S90", WellSpecified: true, Split: false}
	if len(l.plans) != 1 || !reflect.DeepEqual(l.plans[0], want) || len(l.starts) != 0 {
		t.Fatalf("calls: %+v %+v", l.plans, l.starts)
	}
	for _, s := range []string{"ork S90", "worktree: /project/.worktrees/S90", "branch: S90", "check: go test ./...", "implementer: Codex gpt high", "reviewer: Claude opus medium", "gate: Codex gpt low", "permissions: read-only", "ork run ork S90? [y/N] (Enter is No)"} {
		if !strings.Contains(h.view(), s) {
			t.Errorf("missing %q:\n%s", s, h.view())
		}
	}
}

// A plan made without the UI or spec-check rules shows its note under the
// plan lines in the confirm, before the question (S159). The note is a
// warning: drawn in the Warn style (owner decision 2026-10-27).
func TestLaunchConfirmShowsThePlanNote(t *testing.T) {
	const note = "planning without UI or spec-check rules: exit 2"
	for _, present := range []bool{false, true} {
		h, l := launchHarness(t)
		h.m.cfg.Theme = NewTheme(false)
		if present {
			l.plan.Note = note
		}
		launchConfirmed(t, h)
		view := h.view()
		if present && !strings.Contains(h.m.View().Content, h.m.cfg.Theme.Warn.Render("note: "+note)) {
			t.Errorf("the note is not drawn in the Warn style:\n%q", h.m.View().Content)
		}
		if got := strings.Contains(view, "note: "+note); got != present {
			t.Fatalf("present=%v:\n%s", present, view)
		}
		if present && !(strings.Index(view, "gate: Codex gpt low") < strings.Index(view, "note: ") && strings.Index(view, "note: ") < strings.Index(view, "ork run ork S90?")) {
			t.Errorf("the note is not between the plan and the question:\n%s", view)
		}
	}
}
func TestLaunchYesStartsWithExpectedPlan(t *testing.T) {
	for _, k := range []string{"y", "Y", "enter", "n", "N", "esc"} {
		t.Run(k, func(t *testing.T) {
			h, l := launchHarness(t)
			launchConfirmed(t, h)
			_, c := h.m.Update(press(k))
			if len(l.starts) != 0 {
				t.Fatal("Start called in Update")
			}
			if k != "y" && k != "Y" {
				if c != nil || !strings.Contains(h.view(), "Cancelled") {
					t.Fatal("must cancel")
				}
				return
			}
			if c == nil {
				t.Fatal("missing Start")
			}
			for _, key := range []string{"y", "l", "enter"} {
				if _, extra := h.m.Update(press(key)); extra != nil {
					t.Fatalf("second command from %s while Start pending", key)
				}
			}
			_, refresh := h.m.Update(c())
			if len(l.starts) != 1 || !reflect.DeepEqual(l.starts[0].Expected, &l.plan) || !l.starts[0].WellSpecified || l.starts[0].Split || l.starts[0].Project != "ork" || l.starts[0].Item != "S90" || l.starts[0].Lineup != nil || l.starts[0].Budgets != nil {
				t.Fatalf("start: %+v", l.starts)
			}
			if refresh == nil || !strings.Contains(h.view(), "ork run ork S90: done") {
				t.Fatal("success must show and refresh")
			}
		})
	}
}
func TestLaunchErrorVerbatim(t *testing.T) {
	for _, stage := range []string{"plan", "start"} {
		t.Run(stage, func(t *testing.T) {
			h, l := launchHarness(t)
			e := &ipc.Error{Code: "launch_failed", Message: "Exact ERROR: keep punctuation."}
			if stage == "plan" {
				l.planErr = e
			} else {
				l.startErr = e
			}
			c := launchPlanning(t, h)
			h.deliver(c())
			if stage == "start" {
				_, c = h.m.Update(press("y"))
				if c == nil {
					t.Fatal("missing Start")
				}
				h.deliver(c())
			}
			if !strings.Contains(h.view(), "launch_failed: Exact ERROR: keep punctuation.") {
				t.Fatalf("error changed:\n%s", h.view())
			}
			if _, c = h.m.Update(press("y")); c != nil {
				t.Fatal("error retried")
			}
		})
	}
}
func TestLaunchEscapeAndLateResults(t *testing.T) {
	for _, stage := range []string{"list", "pending", "confirm"} {
		t.Run(stage, func(t *testing.T) {
			h, l := launchHarness(t)
			var late tea.Msg
			if stage == "list" {
				h.m.Update(press("l"))
			} else {
				c := launchPlanning(t, h)
				late = c()
				if stage == "confirm" {
					h.m.Update(late)
					late = nil
				}
			}
			h.m.Update(press("esc"))
			if late != nil {
				h.m.Update(late)
			}
			if len(l.starts) != 0 || (stage != "pending" && stage != "confirm" && len(l.plans) != 0) || strings.Contains(h.view(), "[y/N]") || !strings.Contains(h.view(), "Cancelled") {
				t.Fatalf("cancel: plans %d starts %d\n%s", len(l.plans), len(l.starts), h.view())
			}
		})
	}
	for _, drop := range []string{"project", "view", "reopen"} {
		t.Run(drop, func(t *testing.T) {
			h, l := launchHarness(t)
			c := launchPlanning(t, h)
			late := c()
			if drop == "project" {
				h.m.switchTo(1)
			} else if drop == "view" {
				h.m.moveView(1)
			} else {
				h.m.Update(press("esc"))
				launchPlanning(t, h)
			}
			h.m.Update(late)
			if h.m.confirm != nil || len(l.starts) != 0 {
				t.Fatal("stale plan accepted")
			}
		})
	}
	for _, drop := range []string{"esc", "project", "view"} {
		t.Run("queued-"+drop, func(t *testing.T) {
			h, l := launchHarness(t)
			c := launchPlanning(t, h)
			if drop == "esc" {
				h.m.Update(press("esc"))
			} else if drop == "project" {
				h.m.switchTo(1)
			} else {
				h.m.moveView(1)
			}
			if msg := c(); msg != nil {
				h.m.Update(msg)
			}
			if len(l.plans)+len(l.starts) != 0 || h.m.confirm != nil {
				t.Fatal("dropped command called old project")
			}
		})
	}
}
func TestLaunchScopeAndHelp(t *testing.T) {
	for _, where := range []string{"overview", "needs-you", "detail", "offline"} {
		t.Run(where, func(t *testing.T) {
			h, l := launchHarness(t)
			switch where {
			case "overview":
				h.m.view = 0
			case "needs-you":
				h.m.switchTo(-1)
			case "detail":
				h.m.Update(press("enter"))
			case "offline":
				h.m.cfg.Live = nil
			}
			h.m.Update(press("l"))
			if strings.Contains(h.view(), "Ready slices") || len(l.plans)+len(l.starts) != 0 {
				t.Fatal("launch outside scope")
			}
			if where == "offline" && !strings.Contains(h.view(), "No daemon connection; action unavailable") {
				t.Fatal("missing offline error")
			}
		})
	}
	h, _ := launchHarness(t)
	h.m.Update(press("?"))
	if !strings.Contains(h.view(), "launch a ready slice (Roadmap)") {
		t.Fatal("launch missing from help")
	}
}
func TestLaunchReadyListNavigation(t *testing.T) {
	h, _ := launchHarness(t)
	s := h.m.projects[0].snap
	second := s.Items.Value[0]
	second.ID = "S91"
	second.ShortTitle = "Next ready"
	s.Items.Value = append(s.Items.Value, second)
	s.Deps.Value = append(s.Deps.Value, second)
	h.m.projects[0].snap = s
	h.m.Update(press("l"))
	if strings.Index(h.view(), "S90") >= strings.Index(h.view(), "S91") {
		t.Fatal("list order changed")
	}
	for _, keys := range [][]string{{"down"}, {"j"}, {"down", "up", "down"}, {"down", "k", "j"}} {
		h.m.Update(press("esc"))
		h.m.Update(press("l"))
		for _, k := range keys {
			h.m.Update(press(k))
		}
		_, c := h.m.Update(press("enter"))
		if c == nil {
			t.Fatal("missing Plan")
		}
		h.deliver(c())
		if !strings.Contains(h.view(), "ork run ork S91?") {
			t.Fatalf("wrong selection for %v", keys)
		}
	}
}

func TestLaunchPendingStartCannotRepeatAfterClose(t *testing.T) {
	h, l := launchHarness(t)
	launchConfirmed(t, h)
	_, c := h.m.Update(press("y"))
	if c == nil {
		t.Fatal("no Start command")
	}
	result := c()
	h.m.Update(press("esc"))
	h.m.Update(press("l"))
	if !strings.Contains(h.view(), "ork run ork S90 is pending") {
		t.Fatal("pending Start can be repeated")
	}
	h.m.Update(result)
	if len(l.starts) != 1 || h.m.confirm != nil {
		t.Fatal("late Start reopened launch")
	}
	h.m.Update(press("l"))
	if !strings.Contains(h.view(), "Ready slices") {
		t.Fatal("completed Start still blocks launch")
	}
}

type blockingStartLive struct {
	*launchLive
	started   chan context.Context
	release   chan struct{}
	cancelled bool
}

func (l *blockingStartLive) Start(ctx context.Context, p daemon.RunParams) (runrec.Record, error) {
	l.starts = append(l.starts, p)
	l.started <- ctx
	select {
	case <-ctx.Done():
	case <-l.release:
	}
	l.cancelled = ctx.Err() != nil
	if l.cancelled {
		return runrec.Record{}, ctx.Err()
	}
	return runrec.Record{ID: "new-run"}, l.startErr
}

// The cleanup joins the command even when an assertion stops the test early.
func launchBlockedStart(t *testing.T, h *harness, l *launchLive) (*blockingStartLive, context.Context, func() tea.Msg) {
	t.Helper()
	b := &blockingStartLive{launchLive: l, started: make(chan context.Context, 1), release: make(chan struct{})}
	h.m.cfg.Live = b
	launchConfirmed(t, h)
	_, cmd := h.m.Update(press("y"))
	if cmd == nil {
		t.Fatal("no Start command")
	}
	result, finished := make(chan tea.Msg, 1), make(chan struct{})
	go func() {
		defer close(finished)
		result <- cmd()
	}()
	var once sync.Once
	release := func() { once.Do(func() { close(b.release) }) }
	t.Cleanup(func() { release(); <-finished })
	ctx := <-b.started
	return b, ctx, func() tea.Msg { release(); return <-result }
}

func TestLaunchSentStartSurvivesNavigation(t *testing.T) {
	for _, key := range []string{"esc", "]", "[", "2", "tab", "shift+tab"} {
		t.Run(key, func(t *testing.T) {
			h, l := launchHarness(t)
			b, ctx, reply := launchBlockedStart(t, h, l)
			h.m.Update(press(key))
			if ctx.Err() != nil {
				t.Errorf("sent Start context cancelled by %s: %v", key, ctx.Err())
			}
			if h.m.actionStatus != "ork run ork S90: pending" {
				t.Errorf("pending status lost after %s: %q", key, h.m.actionStatus)
			}
			// Return to the item while the daemon still has not replied.
			h.m.switchTo(0)
			h.m.view = roadmapView
			h.m.Update(press("l"))
			if h.m.launch != nil || !strings.Contains(h.view(), "ork run ork S90 is pending") {
				t.Errorf("sent Start can be repeated before its reply:\n%s", h.view())
			}
			h.m.Update(reply())
			if b.cancelled || len(b.starts) != 1 {
				t.Errorf("Start cancelled=%t calls=%d", b.cancelled, len(b.starts))
			}
		})
	}
}

func TestLaunchStartReplyAfterLeaving(t *testing.T) {
	for _, key := range []string{"esc", "]", "tab"} {
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failed-%t", key, failed), func(t *testing.T) {
				h, l := launchHarness(t)
				if failed {
					l.startErr = &ipc.Error{Code: "start_failed", Message: "Exact ERROR: keep punctuation."}
				}
				_, _, reply := launchBlockedStart(t, h, l)
				h.m.Update(press(key))
				var log []string
				l.log = &log
				h.m.cfg.Source = loggingSource{inner: h.src, log: &log}
				_, refresh := h.m.Update(reply())
				want := "ork run ork S90: done"
				if failed {
					want = "ork run ork S90: start_failed: Exact ERROR: keep punctuation."
				}
				if h.m.actionStatus != want || !strings.Contains(h.view(), want) {
					t.Errorf("reply status = %q, want %q\n%s", h.m.actionStatus, want, h.view())
				}
				if len(h.m.launchFlights) != 0 || h.m.launch != nil {
					t.Error("reply left a flight or restored the launch")
				}
				if failed {
					if refresh != nil {
						t.Error("failed Start refreshed")
					}
					h.m.Update(press("y"))
					if len(l.starts) != 1 {
						t.Error("failed Start retried")
					}
					return
				}
				for _, msg := range exec(refresh) {
					h.m.Update(msg)
				}
				if want := []string{"refresh ork", "snapshot ork"}; !reflect.DeepEqual(log, want) {
					t.Errorf("refresh calls = %v, want %v", log, want)
				}
			})
		}
	}
}

func TestLaunchStartReplyMatchesFlightRequest(t *testing.T) {
	h, _ := launchHarness(t)
	launchConfirmed(t, h)
	_, first := h.m.Update(press("y"))
	oldReply := first()
	h.send(oldReply)
	launchConfirmed(t, h)
	_, second := h.m.Update(press("y"))
	current := h.m.launch
	_, refresh := h.m.Update(oldReply)
	if refresh != nil || h.m.launch != current || len(h.m.launchFlights) != 1 || h.m.actionStatus != "ork run ork S90: pending" {
		t.Error("stale reply changed the current Start flight")
	}
	h.m.Update(second())
	if len(h.m.launchFlights) != 0 || h.m.actionStatus != "ork run ork S90: done" {
		t.Error("current Start reply was dropped")
	}
}

func TestLaunchStartReplyPreservesOtherLaunch(t *testing.T) {
	h, _ := launchHarness(t)
	launchConfirmed(t, h)
	_, start := h.m.Update(press("y"))
	reply := start()
	h.m.switchTo(1)
	h.m.Update(press("l"))
	other := h.m.launch
	_, refresh := h.m.Update(reply)
	if other == nil || h.m.launch != other || refresh == nil || h.m.actionStatus != "ork run ork S90: done" {
		t.Fatal("Start reply lost its result or dropped the other project's launch")
	}
}

func TestLaunchQueuedStartDropped(t *testing.T) {
	for _, key := range []string{"esc", "]", "tab"} {
		t.Run(key, func(t *testing.T) {
			h, l := launchHarness(t)
			launchConfirmed(t, h)
			_, c := h.m.Update(press("y"))
			h.m.Update(press(key))
			h.deliver(c())
			if len(l.starts) != 0 || len(h.m.launchFlights) != 0 {
				t.Fatal("dropped queued Start called the daemon or left a flight")
			}
		})
	}
}

func TestLaunchPendingNavigationDropsPlan(t *testing.T) {
	for _, k := range []string{"tab", "shift+tab", "]", "[", "2"} {
		t.Run(k, func(t *testing.T) {
			h, _ := launchHarness(t)
			c := launchPlanning(t, h)
			late := c()
			h.m.Update(press(k))
			h.m.Update(late)
			if h.m.confirm != nil || h.m.launch != nil {
				t.Fatal("navigation kept launch")
			}
		})
	}
}

func TestGoldenLaunch(t *testing.T) {
	for _, tc := range []struct {
		name string
		mono bool
	}{
		{"launch-100x30-color.txt", false}, {"launch-100x30-mono.txt", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, l := launchHarness(t)
			l.plan.Lineup["architect"] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}}
			l.plan.Permissions["architect"] = engine.Permission{ReadOnly: true}
			l.plan.Note = "slicer relay-roles show failed; planning without UI or spec-check rules: exit 2"
			h.m.cfg.Theme = NewTheme(tc.mono)
			launchConfirmed(t, h)
			checkGolden(t, tc.name, h.m.View().Content)
		})
	}
}

func TestLaunchDaemonConnectFailure(t *testing.T) {
	failed := errors.New("exact connect failure")
	d := &Daemon{Connect: func(context.Context) (*daemon.Client, error) { return nil, failed }}
	if _, err := d.Plan(context.Background(), daemon.RunParams{}); err != failed {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := d.Start(context.Background(), daemon.RunParams{}); err != failed {
		t.Fatalf("Start: %v", err)
	}
}

func TestDaemonTypedLaunch(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			plan := daemon.RunPlan{Project: registry.Project{Name: "ork"}, Item: "S52", Worktree: "/ork/.worktrees/S52", Branch: "S52", CheckCommand: "go test ./..."}
			params := daemon.RunParams{Project: "ork", Item: "S52", WellSpecified: true, Split: false}
			d, _, _, _ := requestServer(t, func(_ context.Context, method string, raw json.RawMessage) (any, error) {
				var got daemon.RunParams
				if err := json.Unmarshal(raw, &got); err != nil {
					return nil, err
				}
				want := params
				if method == daemon.MethodRunStart {
					want.Expected = &plan
				}
				if (method != daemon.MethodRunPlan && method != daemon.MethodRunStart) || !reflect.DeepEqual(got, want) {
					return nil, fmt.Errorf("wrong request: %s %s", method, raw)
				}
				if fail {
					return nil, &ipc.Error{Code: "launch_failed", Message: "Exact wire error."}
				}
				if method == daemon.MethodRunPlan {
					return plan, nil
				}
				return runrec.Record{ID: "engine"}, nil
			})
			got, err := d.Plan(context.Background(), params)
			if !fail && (err != nil || !reflect.DeepEqual(got, &plan)) {
				t.Fatalf("Plan: %+v %v", got, err)
			}
			if fail {
				var e *ipc.Error
				if !errors.As(err, &e) || e.Code != "launch_failed" || e.Message != "Exact wire error." {
					t.Fatalf("Plan error: %v", err)
				}
			}
			params.Expected = &plan
			rec, err := d.Start(context.Background(), params)
			if !fail && (err != nil || rec.ID != "engine") {
				t.Fatalf("Start: %+v %v", rec, err)
			}
			if fail {
				var e *ipc.Error
				if !errors.As(err, &e) || e.Code != "launch_failed" || e.Message != "Exact wire error." {
					t.Fatalf("Start error: %v", err)
				}
			}
		})
	}
}

func TestLaunchListKeepsSizeAndEffort(t *testing.T) {
	h, _ := launchHarness(t)
	h.m.projects[0].snap.Items.Value[0].ShortTitle = strings.Repeat("Long title ", 20)
	h.m.Update(press("l"))
	if rowsWith(h.view(), "S90", "M", "effort 2") != 1 {
		t.Fatalf("long title hid fields:\n%s", h.view())
	}
}

func TestLaunchListUsesCurrentReadiness(t *testing.T) {
	h, l := launchHarness(t)
	h.m.Update(press("l"))
	h.m.projects[0].snap.Deps.Err = errors.New("deps failed after list opened")
	h.m.Update(press("enter"))
	if !strings.Contains(h.view(), "No ready slices") || strings.Contains(h.view(), "pending: run.plan") || len(l.plans) != 0 {
		t.Fatalf("stale readiness selected a slice:\n%s", h.view())
	}
}

func TestLaunchStartsWithTheDefaults(t *testing.T) {
	h, l := launchHarness(t)
	h.deliver(launchPlanning(t, h)())
	_, c := h.m.Update(press("y"))
	if c == nil {
		t.Fatal("no Start")
	}
	h.deliver(c())
	if len(l.plans) != 1 || len(l.starts) != 1 || !l.plans[0].WellSpecified || l.plans[0].Split || !l.starts[0].WellSpecified || l.starts[0].Split || !reflect.DeepEqual(l.starts[0].Expected, &l.plan) {
		t.Fatalf("Plan and Start: %+v %+v", l.plans, l.starts)
	}
}

func TestPlanTextShowsTheArchitectRow(t *testing.T) {
	for _, present := range []bool{false, true} {
		h, l := launchHarness(t)
		if present {
			l.plan.Lineup["architect"] = []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}}
			l.plan.Permissions["architect"] = engine.Permission{ReadOnly: true}
		}
		launchConfirmed(t, h)
		if strings.Contains(h.view(), "architect: codex sol high") != present {
			t.Fatalf("present=%v: %s", present, h.view())
		}
	}
}

func TestLaunchPlanShowsUIPassRow(t *testing.T) {
	for _, present := range []bool{false, true} {
		h, l := launchHarness(t)
		if present {
			l.plan.Lineup["ui"] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}}
			l.plan.Permissions["ui"] = engine.Permission{ReadOnly: true}
		}
		launchConfirmed(t, h)
		if strings.Contains(h.view(), "ui: claude opus medium") != present {
			t.Fatalf("present=%v: %s", present, h.view())
		}
	}
}

func openTestLaunchLineup(t *testing.T, h *harness) {
	t.Helper()
	h.m.Update(press("e"))
	if !strings.Contains(h.view(), "Edit lineup: S90") {
		t.Fatalf("editor not opened:\n%s", h.view())
	}
}
func TestLaunchConfirmEKeyOpensLineupEditor(t *testing.T) {
	h, l := launchHarness(t)
	launchConfirmed(t, h)
	old := h.m.confirm
	openTestLaunchLineup(t, h)
	if !strings.Contains(h.view(), "Codex gpt high") || h.m.confirm != nil {
		t.Fatal("editor not seeded or confirmation still open")
	}
	h.m.Update(press("esc"))
	if h.m.confirm == nil || !reflect.DeepEqual(h.m.confirm, old) || len(l.plans) != 1 {
		t.Fatal("discard changed confirmation or planned")
	}
}
func editTestLaunchLineup(t *testing.T, h *harness) tea.Cmd {
	t.Helper()
	openTestLaunchLineup(t, h)
	changeImplementer(h)
	for range len(lineupRoles) + 1 {
		h.m.Update(press("down"))
	}
	replaceLineupField(h, "20")
	_, cmd := h.m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("save did not replan")
	}
	return cmd
}
func TestLaunchLineupSaveReplansWithOverrides(t *testing.T) {
	h, l := launchHarness(t)
	launchConfirmed(t, h)
	cmd := editTestLaunchLineup(t, h)
	if len(l.plans) != 1 || !strings.Contains(h.view(), "pending: run.plan") {
		t.Fatal("missing pending or synchronous Plan")
	}
	msg := cmd()
	if len(l.plans) != 2 {
		t.Fatalf("plans %d", len(l.plans))
	}
	p := l.plans[1]
	if p.Lineup["implementer"][0].Model != "custom-model" || p.Budgets == nil || p.Budgets.Minutes != 20 {
		t.Fatalf("params: %+v", p)
	}
	h.m.Update(msg)
	if h.m.confirm == nil || !strings.Contains(h.view(), "lineup edited") {
		t.Fatal("new confirmation missing")
	}
}
func TestLaunchLineupReplanErrorKeepsOldPlan(t *testing.T) {
	for _, priorEdit := range []bool{false, true} {
		t.Run(fmt.Sprint(priorEdit), func(t *testing.T) {
			h, l := launchHarness(t)
			launchConfirmed(t, h)
			if priorEdit {
				cmd := editTestLaunchLineup(t, h)
				l.plan.Lineup = map[string][]runrec.LineupEntry{"implementer": {{Vendor: "Codex", Model: "custom-model", Effort: "high"}}}
				l.plan.Budgets = registry.Budgets{Minutes: 20}
				h.m.Update(cmd())
			}
			old := *h.m.confirm
			l.planErr = &ipc.Error{Code: "bad_params", Message: "Exact override failure."}
			openTestLaunchLineup(t, h)
			changeImplementer(h)
			for range len(lineupRoles) + 1 {
				h.m.Update(press("down"))
			}
			replaceLineupField(h, "30")
			_, cmd := h.m.Update(press("enter"))
			if cmd == nil {
				t.Fatal("missing replan")
			}
			h.m.Update(cmd())
			if !strings.Contains(h.view(), "bad_params: Exact override failure.") || !reflect.DeepEqual(h.m.confirm, &old) {
				t.Fatalf("old confirmation/error lost:\n%s", h.view())
			}
			_, start := h.m.Update(press("y"))
			if start == nil {
				t.Fatal("old plan cannot start")
			}
			start()
			if len(l.starts) != 1 || l.starts[0].Expected != old.plan || !reflect.DeepEqual(l.starts[0].Lineup, old.lineup) || !reflect.DeepEqual(l.starts[0].Budgets, old.budgets) {
				t.Fatalf("start: %+v", l.starts)
			}
		})
	}
}
func TestLaunchStartCarriesOverridesAndEditedPlan(t *testing.T) {
	h, l := launchHarness(t)
	launchConfirmed(t, h)
	cmd := editTestLaunchLineup(t, h)
	edited := l.plan
	edited.Branch = "edited"
	edited.Lineup = map[string][]runrec.LineupEntry{"implementer": {{Vendor: "Codex", Model: "custom-model", Effort: "high"}}}
	edited.Budgets = registry.Budgets{Minutes: 20}
	l.plan = edited
	h.m.Update(cmd())
	_, start := h.m.Update(press("y"))
	if start == nil {
		t.Fatal("no Start")
	}
	start()
	if len(l.starts) != 1 {
		t.Fatal("no start call")
	}
	p := l.starts[0]
	if p.Lineup["implementer"][0].Model != "custom-model" || p.Budgets == nil || p.Budgets.Minutes != 20 || !reflect.DeepEqual(p.Expected, &edited) {
		t.Fatalf("start: %+v", p)
	}
}
func TestLaunchUnchangedEditorDoesNotPlan(t *testing.T) {
	h, l := launchHarness(t)
	launchConfirmed(t, h)
	openTestLaunchLineup(t, h)
	_, cmd := h.m.Update(press("enter"))
	if cmd != nil || len(l.plans) != 1 || h.m.confirm == nil {
		t.Fatal("unchanged save planned or lost confirmation")
	}
}
func TestLaunchLineupGolden(t *testing.T) {
	for _, mono := range []bool{false, true} {
		h, _ := launchHarness(t)
		h.m.cfg.Theme = NewTheme(mono)
		launchConfirmed(t, h)
		openTestLaunchLineup(t, h)
		theme := "color"
		if mono {
			theme = "mono"
		}
		h.at(100, 30)
		checkGolden(t, "launch-lineup-100x30-"+theme+".txt", h.m.View().Content)
	}
}

func TestLaunchBudgetOnlyEditShowsEffectiveBudgets(t *testing.T) {
	for _, value := range []string{"20", "0"} {
		t.Run(value, func(t *testing.T) {
			h, l := launchHarness(t)
			l.plan.Budgets = registry.Budgets{Minutes: 45, Tokens: 9000}
			launchConfirmed(t, h)
			if strings.Contains(h.view(), "budgets:") {
				t.Fatal("unoverridden budget shown")
			}
			openTestLaunchLineup(t, h)
			for range len(lineupRoles) + 1 {
				h.m.Update(press("down"))
			}
			replaceLineupField(h, value)
			_, cmd := h.m.Update(press("enter"))
			if cmd == nil {
				t.Fatal("missing budget replan")
			}
			if value == "20" {
				l.plan.Budgets.Minutes = 20
			}
			h.m.Update(cmd())
			p := l.plans[len(l.plans)-1]
			if p.Lineup != nil || p.Budgets == nil {
				t.Fatalf("not a budget-only edit: %+v", p)
			}
			row := fmt.Sprintf("budgets: %d min, 9000 tokens", l.plan.Budgets.Minutes)
			if !strings.Contains(h.view(), row) {
				t.Fatalf("effective budget missing: %s", h.view())
			}
		})
	}
}

type replanContextLive struct {
	*launchLive
	planContext context.Context
}

func (l *replanContextLive) Plan(ctx context.Context, p daemon.RunParams) (*daemon.RunPlan, error) {
	l.planContext = ctx
	return l.launchLive.Plan(ctx, p)
}

func TestLaunchPendingReplanEscRestoresConfirmation(t *testing.T) {
	for _, priorEdit := range []bool{false, true} {
		t.Run(fmt.Sprint(priorEdit), func(t *testing.T) {
			h, l := launchHarness(t)
			launchConfirmed(t, h)
			if priorEdit {
				cmd := editTestLaunchLineup(t, h)
				l.plan.Lineup = map[string][]runrec.LineupEntry{"implementer": {{Vendor: "Codex", Model: "custom-model", Effort: "high"}}}
				l.plan.Budgets = registry.Budgets{Minutes: 20}
				h.m.Update(cmd())
			}
			old := *h.m.confirm
			live := &replanContextLive{launchLive: l}
			h.m.cfg.Live = live
			openTestLaunchLineup(t, h)
			for range len(lineupRoles) + 1 {
				h.m.Update(press("down"))
			}
			replaceLineupField(h, "30")
			_, cmd := h.m.Update(press("enter"))
			if cmd == nil {
				t.Fatal("no replan command")
			}
			if !strings.Contains(hintLine(h.view()), "esc back") {
				t.Errorf("pending replan hint: %s", hintLine(h.view()))
			}
			late := cmd()
			h.m.Update(press("esc"))
			if h.m.launch == nil || !reflect.DeepEqual(h.m.confirm, &old) {
				t.Fatal("Esc lost previous confirmation")
			}
			if live.planContext.Err() != context.Canceled {
				t.Fatal("replan context not cancelled")
			}
			h.m.Update(late)
			if !reflect.DeepEqual(h.m.confirm, &old) || h.m.confirm.plan != old.plan {
				t.Fatal("late reply changed confirmation")
			}
			_, start := h.m.Update(press("y"))
			if start == nil {
				t.Fatal("old plan cannot start")
			}
			start()
			if len(l.starts) != 1 || !reflect.DeepEqual(l.starts[0], daemon.RunParams{Project: "ork", Item: "S90", WellSpecified: true, Expected: old.plan, Lineup: old.lineup, Budgets: old.budgets}) {
				t.Fatalf("old params lost: %+v", l.starts)
			}
		})
	}
}

func TestLaunchCancelledReplanLateReplyIgnored(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			h, l := launchHarness(t)
			launchConfirmed(t, h)
			cmd := editTestLaunchLineup(t, h)
			if fail {
				l.planErr = errors.New("cancelled replan error")
			}
			late := cmd()
			l.planErr = nil
			h.m.Update(press("esc"))
			if h.m.confirm == nil {
				t.Fatal("Esc dropped launch")
			}
			cmd2 := editTestLaunchLineup(t, h)
			request := h.m.launch.request
			h.m.Update(late)
			if h.m.launch == nil || h.m.launch.stage != launchPlanPending || h.m.launch.request != request || h.m.confirm != nil || strings.Contains(h.view(), "cancelled replan error") {
				t.Fatal("cancelled reply affected newer pending replan")
			}
			h.m.Update(cmd2())
			if h.m.confirm == nil {
				t.Fatal("new replan lost")
			}
		})
	}
}

func TestLaunchFirstPlanEscStillCancels(t *testing.T) {
	h, _ := launchHarness(t)
	cmd := launchPlanning(t, h)
	if !strings.Contains(hintLine(h.view()), "esc cancel") {
		t.Fatal("first plan hint changed")
	}
	h.m.Update(press("esc"))
	h.m.Update(cmd())
	if h.m.launch != nil || h.m.confirm != nil || !strings.Contains(h.view(), "Cancelled") {
		t.Fatal("first plan Esc did not cancel")
	}
}

func launchQuestions(l *launchLive) {
	l.check = func([]string) (*daemon.SpecCheckResult, error) {
		return &daemon.SpecCheckResult{OwnerNeeded: "Confirm the rollout window.", Items: []daemon.SpecItemResult{{Item: "S90", Questions: twoQuestions()}}}, nil
	}
}
func TestLaunchRunsTheSpecCheckBeforeTheConfirmation(t *testing.T) {
	h, l := launchHarness(t)
	_, check := h.m.Update(launchPlanning(t, h)())
	if check == nil || h.m.confirm != nil || !strings.Contains(h.view(), "pending: spec.check") {
		t.Fatalf("no pending check before confirmation:\n%s", h.view())
	}
	if m := h.m; m.actionStatus != "" || strings.Count(h.view(), "pending: spec.check") != 1 {
		t.Fatalf("pending check must appear in the launch body only; status %q:\n%s", m.actionStatus, h.view())
	}
	h.deliver(press("y"))
	h.deliver(press("s"))
	h.deliver(press("l"))
	if len(l.checks) != 0 || len(l.starts) != 0 {
		t.Fatal("check ran synchronously or launch skipped it")
	}
	h.deliver(check())
	if !reflect.DeepEqual(l.checks, [][]string{{"ork", "S90"}}) || h.m.confirm == nil {
		t.Fatalf("checks %v confirm %v", l.checks, h.m.confirm)
	}
}
func TestLaunchSittingAnswersRecordThenConfirm(t *testing.T) {
	h, l := launchHarness(t)
	launchQuestions(l)
	t.Setenv("ADJ_DECIDED_BY", "jon")
	h.m.byName["ork"].reg.Adj = true
	h.deliver(launchPlanning(t, h)())
	if !strings.Contains(h.view(), "[S90 1/2]") || h.m.confirm != nil {
		t.Fatalf("no sitting:\n%s", h.view())
	}
	h.deliver(press("2"))
	if len(l.answers) != 0 || !strings.Contains(h.view(), "[S90 2/2]") {
		t.Fatal("partial answer recorded or question not advanced")
	}
	h.deliver(press("t"))
	h.deliver(tea.PasteMsg{Content: "call it --widen"})
	_, cmd := h.m.Update(press("enter"))
	if h.m.confirm != nil {
		t.Fatal("confirmed before recording")
	}
	if cmd != nil {
		h.deliver(cmd())
	}
	want := daemon.SpecAnswerParams{Project: "ork", Item: "S90", Answers: []string{"memory: rebuilt at each start", "call it --widen"}, Adj: true, By: "jon"}
	if !reflect.DeepEqual(l.answers, []daemon.SpecAnswerParams{want}) || h.m.confirm == nil || len(l.starts) != 0 {
		t.Fatalf("answers %v confirm %v starts %v", l.answers, h.m.confirm, l.starts)
	}
	h.deliver(press("y"))
	if len(l.starts) != 1 {
		t.Fatal("ready launch did not start")
	}
}
func TestLaunchSittingSkipClosesTheLaunchWithAStatus(t *testing.T) {
	for _, key := range []string{"s", "esc"} {
		t.Run(key, func(t *testing.T) {
			h, l := launchHarness(t)
			launchQuestions(l)
			h.deliver(launchPlanning(t, h)())
			if !strings.Contains(h.view(), "[S90 1/2]") {
				t.Fatal("no sitting")
			}
			h.deliver(press("1"))
			h.deliver(press(key))
			h.deliver(press("y"))
			if h.m.launch != nil || h.m.confirm != nil || len(l.starts) != 0 || len(l.answers) != 0 || h.m.actionStatus != "S90 waits on 2 spec questions; l starts the launch again" {
				t.Fatalf("launch %v confirm %v status %q", h.m.launch, h.m.confirm, h.m.actionStatus)
			}
			h.deliver(launchPlanning(t, h)())
			if len(l.checks) != 2 {
				t.Fatal("retry did not check again")
			}
		})
	}
}

// A failed answer write leaves the item's questions open, but the owner did
// answer them: the status says the answers were not saved and what starts the
// launch again, not a count of questions (S190, S193 note).
func TestSpecStatusAfterAFailedAnswerWriteSaysTheAnswersWereNotSaved(t *testing.T) {
	h, l := launchHarness(t)
	launchQuestions(l)
	l.answerErr = &ipc.Error{Code: "internal", Message: "Exact ANSWER failure."}
	h.deliver(launchPlanning(t, h)())
	h.deliver(press("1"))
	h.deliver(press("1"))
	const want = "S90: internal: Exact ANSWER failure.; S90: answers not saved; l starts the launch again"
	if h.m.launch != nil || h.m.actionStatus != want {
		t.Fatalf("launch %v status %q, want %q", h.m.launch, h.m.actionStatus, want)
	}
	// A new sitting of the same launch flow starts clean: a skip reads the count.
	l.answerErr = nil
	h.deliver(launchPlanning(t, h)())
	h.deliver(press("1"))
	h.deliver(press("s"))
	if h.m.actionStatus != "S90 waits on 2 spec questions; l starts the launch again" {
		t.Fatalf("status after a skip: %q", h.m.actionStatus)
	}
}

// Replan with no plan and no error keeps the plan the owner was looking at
// (S190, S187 note).
func TestReplanWithNoPlanRestoresThePreviousPlan(t *testing.T) {
	h, _ := launchHarness(t)
	launchConfirmed(t, h)
	old := *h.m.confirm
	editTestLaunchLineup(t, h)
	if h.m.launch == nil || h.m.launch.stage != launchPlanPending || h.m.launch.prev == nil {
		t.Fatal("the replan is not pending")
	}
	h.deliver(launchMsg{request: h.m.launch.request})
	if h.m.launch == nil || h.m.confirm == nil || !reflect.DeepEqual(*h.m.confirm, old) || h.m.launch.prev != nil || h.m.actionStatus != "No run plan returned" {
		t.Fatalf("launch %v confirm %v status %q", h.m.launch != nil, h.m.confirm, h.m.actionStatus)
	}
	if !strings.Contains(h.view(), "ork run ork S90? [y/N] (Enter is No)") {
		t.Fatalf("the old confirmation is not back:\n%s", h.view())
	}
	// A first plan with no plan still closes the launch.
	h, _ = launchHarness(t)
	launchPlanning(t, h)
	h.deliver(launchMsg{request: h.m.launch.request})
	if h.m.launch != nil || h.m.actionStatus != "No run plan returned" {
		t.Fatalf("first plan: launch %v status %q", h.m.launch != nil, h.m.actionStatus)
	}
}
func TestLaunchSpecCheckErrorDropsTheLaunch(t *testing.T) {
	blocked := daemon.BlockedMessage([]string{"S90"}, []slicercli.ItemDeps{{ID: "S90", BlockedBy: []string{"S89"}}}, map[string]string{"S89": "Prior slice"})
	for _, tc := range []struct {
		err  *ipc.Error
		want string
	}{
		{err: &ipc.Error{Code: "internal", Message: "Exact CHECK failure."}, want: "internal: Exact CHECK failure."},
		{err: &ipc.Error{Code: "refused", Message: blocked}, want: blocked},
	} {
		e := tc.err
		t.Run(e.Code, func(t *testing.T) {
			h, l := launchHarness(t)
			l.check = func([]string) (*daemon.SpecCheckResult, error) { return nil, e }
			h.deliver(launchPlanning(t, h)())
			h.deliver(press("y"))
			h.deliver(press("s"))
			if h.m.launch != nil || h.m.confirm != nil || len(l.starts) != 0 || h.m.actionStatus != tc.want {
				t.Fatalf("launch %v status %q", h.m.launch, h.m.actionStatus)
			}
		})
	}
}
func TestLaunchReplanAfterEditDoesNotCheckAgain(t *testing.T) {
	h, l := launchHarness(t)
	launchConfirmed(t, h)
	if len(l.checks) != 1 {
		t.Fatalf("initial checks %v", l.checks)
	}
	h.deliver(editTestLaunchLineup(t, h)())
	if len(l.checks) != 1 || h.m.confirm == nil || len(l.plans) != 2 {
		t.Fatalf("checks %v confirm %v plans %v", l.checks, h.m.confirm, l.plans)
	}
}
func TestLaunchSittingGolden(t *testing.T) {
	for _, mono := range []bool{false, true} {
		t.Run(fmt.Sprint(mono), func(t *testing.T) {
			h, l := launchHarness(t)
			h.m.cfg.Theme = NewTheme(mono)
			launchQuestions(l)
			h.deliver(launchPlanning(t, h)())
			if !strings.Contains(h.view(), "[S90 1/2]") {
				t.Fatal("no launch sitting")
			}
			theme := "color"
			if mono {
				theme = "mono"
			}
			h.at(100, 30)
			checkGolden(t, "launch-sitting-100x30-"+theme+".txt", h.m.View().Content)
		})
	}
}

func TestLaunchSittingAnswerFailureBlocksLaunch(t *testing.T) {
	h, l := launchHarness(t)
	launchQuestions(l)
	l.answerErr = &ipc.Error{Code: "internal", Message: "Exact ANSWER failure."}
	h.deliver(launchPlanning(t, h)())
	h.deliver(press("1"))
	h.deliver(press("1"))
	if h.m.launch != nil || h.m.confirm != nil || len(l.starts) != 0 || !strings.Contains(h.m.actionStatus, "internal: Exact ANSWER failure.") {
		t.Fatalf("launch %v status %q", h.m.launch, h.m.actionStatus)
	}
}

func TestLaunchStaleSpecCheckAfterNavigation(t *testing.T) {
	for _, key := range []string{"esc", "]", "tab"} {
		t.Run(key, func(t *testing.T) {
			h, l := launchHarness(t)
			_, cmd := h.m.Update(launchPlanning(t, h)())
			if cmd == nil {
				t.Fatal("no check")
			}
			late := cmd()
			h.deliver(press(key))
			h.deliver(late)
			if h.m.launch != nil || h.m.confirm != nil || len(l.starts) != 0 {
				t.Fatal("stale check restored launch")
			}
		})
	}
	// A reply from the previous launch must not finish a newer launch's check.
	h, l := launchHarness(t)
	_, cmd := h.m.Update(launchPlanning(t, h)())
	late := cmd()
	h.deliver(press("esc"))
	_, next := h.m.Update(launchPlanning(t, h)())
	h.deliver(late)
	if h.m.confirm != nil || !strings.Contains(h.view(), "pending: spec.check") {
		t.Fatal("old request confirmed new launch")
	}
	h.deliver(next())
	if h.m.confirm == nil || len(l.checks) != 2 {
		t.Fatal("new check lost")
	}
}

func TestLaunchMissingSpecCheckDoesNotConfirm(t *testing.T) {
	for _, res := range []*daemon.SpecCheckResult{nil, {}, {Items: []daemon.SpecItemResult{{Item: "different", Ready: true}}}} {
		t.Run(fmt.Sprint(res), func(t *testing.T) {
			h, l := launchHarness(t)
			l.check = func([]string) (*daemon.SpecCheckResult, error) { return res, nil }
			h.deliver(launchPlanning(t, h)())
			h.deliver(press("y"))
			if h.m.launch != nil || h.m.confirm != nil || len(l.starts) != 0 {
				t.Fatal("missing readiness allowed launch")
			}
		})
	}
}

func TestLaunchLateAnswerDoesNotConfirmAnotherLaunch(t *testing.T) {
	h, l := launchHarness(t)
	launchQuestions(l)
	h.deliver(launchPlanning(t, h)())
	h.deliver(press("1"))
	_, record := h.m.Update(press("1"))
	if record == nil {
		t.Fatal("no answer command")
	}
	// Navigation can drop a launch while an answer is being recorded.
	h.m.switchTo(1)
	h.m.switchTo(0)
	l.check = nil
	launchConfirmed(t, h)
	confirm := h.m.confirm
	for _, msg := range exec(record) {
		h.deliver(msg)
	}
	if h.m.confirm != confirm || len(l.answers) != 1 || len(l.starts) != 0 {
		t.Fatal("old answer affected new launch or was not recorded")
	}
}

// A plan's setup warnings show in the confirm after the plan rows and before
// the note, in the Warn style, wrapped to the width (S265, *proposed*). An
// empty check command reads "check: none".
func TestLaunchConfirmShowsSetupWarnings(t *testing.T) {
	const long = "the implementer has the built-in Go allowlist, but ork has no go.mod (fix: ork projects set ork --preset go|python, or --allow implementer=PATTERN)"
	for _, present := range []bool{false, true} {
		h, l := launchHarness(t)
		h.m.cfg.Theme = NewTheme(false)
		l.plan.Note = "a note"
		if present {
			l.plan.Warnings = []string{"short warning", long}
			l.plan.CheckCommand = ""
		}
		launchConfirmed(t, h)
		view := h.view()
		if got := strings.Contains(view, "check: none"); got != present {
			t.Fatalf("present=%v: check line:\n%s", present, view)
		}
		if !present {
			if strings.Contains(view, "warning:") {
				t.Fatalf("a plan without warnings shows one:\n%s", view)
			}
			continue
		}
		if !strings.Contains(h.m.View().Content, h.m.cfg.Theme.Warn.Render("warning: short warning")) {
			t.Errorf("the warning is not drawn in the Warn style:\n%q", h.m.View().Content)
		}
		if i := strings.Index(view, "warning: short warning"); !(strings.Index(view, "gate: Codex gpt low") < i && i < strings.Index(view, "note: a note") && strings.Index(view, "note: a note") < strings.Index(view, "ork run ork S90: refused")) {
			t.Errorf("warnings are not between the plan rows and the note:\n%s", view)
		}
		// The long warning wraps at the width: no line is wider than the screen.
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > 100 {
				t.Errorf("line of %d columns: %q", lipgloss.Width(line), line)
			}
		}
		if joined := strings.Join(strings.Fields(strings.ReplaceAll(view, "│", " ")), " "); !strings.Contains(joined, "warning: "+long) {
			t.Errorf("the long warning is cut:\n%s", view)
		}
		// At 80 columns no line runs off the edge either.
		for _, line := range strings.Split(h.view80(), "\n") {
			if lipgloss.Width(line) > 80 {
				t.Errorf("line of %d columns at 80: %q", lipgloss.Width(line), line)
			}
		}
	}
}

// A plan with no check command cannot start (adj 191): the confirm says why
// and how to fix it, once, and y starts nothing.
func TestLaunchConfirmRefusesAPlanWithNoCheckCommand(t *testing.T) {
	h, l := launchHarness(t)
	h.m.cfg.Theme = NewTheme(false)
	l.plan.CheckCommand = ""
	l.plan.Warnings = []string{engine.NoCheckMessage("ork"), "other warning"}
	launchConfirmed(t, h)
	view := h.view()
	for _, want := range []string{
		"check: none",
		"refused: ork has no check command",
		"fix: ork projects set ork --check CMD",
		"start anyway: ork run ork S90 --allow-no-check",
		"warning: other warning",
	} {
		if !strings.Contains(strings.Join(strings.Fields(view), " "), want) {
			t.Errorf("missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "warning: no check command") {
		t.Errorf("the refusal is said twice:\n%s", view)
	}
	if !strings.Contains(h.m.View().Content, h.m.cfg.Theme.Warn.Render("refused: ork has no check command, so only slicer check would run.")) {
		t.Errorf("the refusal is not drawn in the Warn style:\n%q", h.m.View().Content)
	}
	// The bar and the hint offer no start: no y, and the bar says it is refused.
	if !strings.Contains(view, "ork run ork S90: refused (n or esc closes)") || strings.Contains(view, "[y/N]") {
		t.Errorf("the bar offers a start it refuses:\n%s", view)
	}
	if strings.Contains(view, "y run") || !strings.Contains(view, "n cancel") {
		t.Errorf("the hint offers a start it refuses:\n%s", view)
	}
	_, cmd := h.m.Update(press("y"))
	if cmd != nil || len(l.starts) != 0 {
		t.Fatalf("y started a run: %+v", l.starts)
	}
	view = h.view()
	if !strings.Contains(view, "Not started: no check command") || !strings.Contains(view, "ork run ork S90: refused (n or esc closes)") || strings.Contains(view, "[y/N]") {
		t.Fatalf("y must leave the confirm open and say why:\n%s", view)
	}
	h.m.Update(press("n"))
	if !strings.Contains(h.view(), "Cancelled") || h.m.confirm != nil {
		t.Fatalf("n must cancel:\n%s", h.view())
	}
}

// The confirm of a plan with no check command and a setup warning, at the two
// sizes the golden views cover (S265, *proposed*).
func TestGoldenLaunchNoCheck(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		for _, mono := range []bool{false, true} {
			name := fmt.Sprintf("launch-nocheck-%dx%d-%s.txt", size[0], size[1], map[bool]string{false: "color", true: "mono"}[mono])
			t.Run(name, func(t *testing.T) {
				h, l := launchHarness(t)
				l.plan.CheckCommand = ""
				l.plan.Warnings = []string{engine.NoCheckMessage("ork"), "the implementer has the built-in Go allowlist, but ork has no go.mod (fix: ork projects set ork --preset go|python, or --allow implementer=PATTERN)"}
				h.m.cfg.Theme = NewTheme(mono)
				launchConfirmed(t, h)
				h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				checkGolden(t, name, h.m.View().Content)
			})
		}
	}
}
