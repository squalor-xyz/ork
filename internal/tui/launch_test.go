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
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

type launchLive struct {
	fakeLive
	plans, starts     []daemon.RunParams
	plan              daemon.RunPlan
	planErr, startErr error
}

func (l *launchLive) Plan(_ context.Context, p daemon.RunParams) (*daemon.RunPlan, error) {
	l.plans = append(l.plans, p)
	return &l.plan, l.planErr
}
func (l *launchLive) Start(_ context.Context, p daemon.RunParams) (runrec.Record, error) {
	l.starts = append(l.starts, p)
	return runrec.Record{ID: "new-run"}, l.startErr
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
func launchPick(t *testing.T, h *harness) {
	t.Helper()
	h.m.Update(press("l"))
	h.m.Update(press("enter"))
	if !strings.Contains(h.view(), "Well specified? [y/n]:") {
		t.Fatalf("missing first prompt:\n%s", h.view())
	}
}
func launchAnswer(h *harness, answer string) tea.Cmd {
	h.m.Update(tea.PasteMsg{Content: answer})
	_, cmd := h.m.Update(press("enter"))
	return cmd
}
func launchPlanning(t *testing.T, h *harness) tea.Cmd {
	t.Helper()
	launchPick(t, h)
	if cmd := launchAnswer(h, "YeS"); cmd != nil {
		t.Fatal("command before both answers")
	}
	cmd := launchAnswer(h, "nO")
	if cmd == nil {
		t.Fatal("missing plan command")
	}
	return cmd
}
func launchConfirmed(t *testing.T, h *harness) {
	t.Helper()
	h.m.Update(launchPlanning(t, h)())
	if !strings.Contains(h.view(), "ork run ork S90? [y/N] (Enter is No)") {
		t.Fatalf("missing confirmation:\n%s", h.view())
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
	h.m.Update(press("enter"))
	if strings.Contains(h.view(), "Well specified?") {
		t.Fatal("empty list picked a slice")
	}
}
func TestLaunchAsksWellSpecifiedAndSplit(t *testing.T) {
	for _, a := range []string{"y", "YES", "YeS", "n", "NO", "nO"} {
		t.Run(a, func(t *testing.T) {
			h, l := launchHarness(t)
			launchPick(t, h)
			for _, bad := range []string{"", "maybe"} {
				if c := launchAnswer(h, bad); c != nil {
					t.Fatal("invalid input issued command")
				}
				if !strings.Contains(h.view(), "Well specified? requires y or n") {
					t.Fatal("missing input error")
				}
				h.m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
			}
			if c := launchAnswer(h, a); c != nil || len(l.plans) != 0 || !strings.Contains(h.view(), "Split? [y/n]:") {
				t.Fatal("must ask split before Plan")
			}
			if c := launchAnswer(h, ""); c != nil || !strings.Contains(h.view(), "Split? requires y or n") {
				t.Fatal("split has no default")
			}
			c := launchAnswer(h, a)
			if c == nil {
				t.Fatal("accepted answer gave no Plan")
			}
			h.m.Update(c())
			want := strings.HasPrefix(strings.ToLower(a), "y")
			if len(l.plans) != 1 || l.plans[0].WellSpecified != want || l.plans[0].Split != want {
				t.Fatalf("answers: %+v", l.plans)
			}
		})
	}
}
func TestLaunchPlanBeforeConfirm(t *testing.T) {
	h, l := launchHarness(t)
	c := launchPlanning(t, h)
	if len(l.plans) != 0 || len(l.starts) != 0 || !strings.Contains(h.view(), "pending: run.plan") {
		t.Fatal("Plan must run in command")
	}
	h.m.Update(c())
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
			h.m.Update(c())
			if stage == "start" {
				_, c = h.m.Update(press("y"))
				if c == nil {
					t.Fatal("missing Start")
				}
				h.m.Update(c())
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
	for _, stage := range []string{"list", "well", "split", "pending", "confirm"} {
		t.Run(stage, func(t *testing.T) {
			h, l := launchHarness(t)
			var late tea.Msg
			if stage == "list" {
				h.m.Update(press("l"))
			} else if stage == "well" {
				launchPick(t, h)
			} else if stage == "split" {
				launchPick(t, h)
				launchAnswer(h, "y")
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
func TestLaunchPromptOwnsKeysAndPaste(t *testing.T) {
	h, l := launchHarness(t)
	launchPick(t, h)
	for _, k := range []string{"n", "y", "q", "s", "r", "?"} {
		if _, c := h.m.Update(press(k)); c != nil {
			t.Fatalf("prompt key %s issued command", k)
		}
	}
	if !strings.Contains(h.view(), "nyqsr?") || len(l.plans) != 0 || h.m.showHelp || h.m.sidebarHidden {
		t.Fatalf("prompt lost keys:\n%s", h.view())
	}
	h.m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	launchAnswer(h, "yes")
	h.m.Update(tea.PasteMsg{Content: "no"})
	if len(l.plans) != 0 {
		t.Fatal("paste submitted")
	}
	_, c := h.m.Update(press("ctrl+c"))
	if c == nil {
		t.Fatal("ctrl+c did not quit")
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
		h.m.Update(press("enter"))
		launchAnswer(h, "y")
		c := launchAnswer(h, "n")
		if c == nil {
			t.Fatal("missing Plan")
		}
		h.m.Update(c())
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
			h.m.Update(c())
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
			h, _ := launchHarness(t)
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
	if !strings.Contains(h.view(), "No ready slices") || strings.Contains(h.view(), "Well specified?") || len(l.plans) != 0 {
		t.Fatalf("stale readiness selected a slice:\n%s", h.view())
	}
}

func TestLaunchStartsWithBothAnswers(t *testing.T) {
	for _, well := range []bool{false, true} {
		for _, split := range []bool{false, true} {
			t.Run(fmt.Sprintf("well-%t/split-%t", well, split), func(t *testing.T) {
				h, l := launchHarness(t)
				launchPick(t, h)
				answers := map[bool]string{false: "no", true: "yes"}
				launchAnswer(h, answers[well])
				c := launchAnswer(h, answers[split])
				if c == nil {
					t.Fatal("no Plan")
				}
				h.m.Update(c())
				_, c = h.m.Update(press("y"))
				if c == nil {
					t.Fatal("no Start")
				}
				h.m.Update(c())
				if len(l.plans) != 1 || len(l.starts) != 1 || l.starts[0].WellSpecified != well || l.starts[0].Split != split || !reflect.DeepEqual(l.starts[0].Expected, &l.plan) {
					t.Fatalf("Plan and Start: %+v %+v", l.plans, l.starts)
				}
			})
		}
	}
}
