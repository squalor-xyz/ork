// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
)

type fakeSpecPermissionLive struct {
	Live
	project, item, answer string
	question              *runrec.OwnerQuestion
}

func (l *fakeSpecPermissionLive) SpecPermission(_ context.Context, p daemon.SpecPermissionParams) (*daemon.SpecPermissionResult, error) {
	if p.Project != l.project || len(p.Items) != 1 || p.Items[0] != l.item {
		return nil, fmt.Errorf("wrong sitting scope: %+v", p)
	}
	if p.Text != "" {
		if p.ID != 7 || !strings.HasPrefix(strings.ToLower(p.Text), l.answer) {
			return nil, fmt.Errorf("wrong permission answer: %+v", p)
		}
		l.question = nil
	}
	if l.question == nil {
		return &daemon.SpecPermissionResult{}, nil
	}
	return &daemon.SpecPermissionResult{ID: 7, Question: l.question}, nil
}

func TestSpecPermissionSitting(t *testing.T) {
	for _, flow := range []string{"roadmap", "waves"} {
		for _, answer := range []string{"allow", "deny", "escape"} {
			t.Run(flow+"/"+answer, func(t *testing.T) {
				h, live := launchHarness(t)
				q := &runrec.OwnerQuestion{Title: "Allow Bash: make test?", Options: []runrec.Option{{Label: "Allow", Summary: "this call"}, {Label: "Deny", Summary: "this call"}}, Recommend: 2, Reason: "not allowlisted"}
				expected := answer
				if answer == "escape" {
					expected = "deny"
				}
				bridge := &fakeSpecPermissionLive{Live: live, project: "ork", item: "S90", answer: expected, question: q}
				h.m.cfg.Live = bridge
				var state *specState
				if flow == "roadmap" {
					msg := launchPlanning(t, h)()
					h.m.Update(msg) // leave the SpecCheck command in flight
					state = &h.m.launch.specState
				} else {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					h.m.view = wavesView
					h.m.waves = &wavesState{project: "ork", ctx: ctx}
					h.m.startSpecCheck([]daemon.RunParams{{Project: "ork", Item: "S90"}})
					state = &h.m.waves.specState
				}
				h.deliver(tickMsg{})
				if view := h.view(); !strings.Contains(view, q.Title) || !strings.Contains(view, "1. Allow") || !strings.Contains(view, "2. Deny") {
					t.Fatalf("live spec permission not shown:\n%s", view)
				}
				if view := h.view(); !strings.Contains(view, "deny call") {
					t.Fatalf("permission escape hint is not accurate:\n%s", view)
				}
				if view := h.view(); strings.Contains(view, "free text") || !strings.Contains(view, "1-2 pick") {
					t.Errorf("spec permission hints offer unsupported free text:\n%s", view)
				}
				h.deliver(press("t"))
				if view := h.view(); strings.Contains(view, "Answer:") || !strings.Contains(view, "1. Allow") {
					t.Fatalf("spec permission question entered a free-text field:\n%s", view)
				}
				stale := specPermissionMsg{state: state, epoch: state.permissionEpoch, result: &daemon.SpecPermissionResult{ID: 7, Question: q}}
				key := "1"
				if answer == "deny" {
					key = "2"
				}
				if answer == "escape" {
					key = "esc"
				}
				h.deliver(press(key))
				if view := h.view(); strings.Contains(view, q.Title) || !strings.Contains(view, "spec check:") || h.m.confirm != nil {
					t.Fatalf("permission answer changed spec readiness or did not reply:\n%s", view)
				}
				h.deliver(stale)
				if strings.Contains(h.view(), q.Title) {
					t.Fatal("stale permission poll reopened an answered call")
				}
			})
		}
	}
}

func TestSpecPermissionCanceledSitting(t *testing.T) {
	h, live := launchHarness(t)
	q := &runrec.OwnerQuestion{Title: "Late permission question", Options: []runrec.Option{{Label: "Allow"}, {Label: "Deny"}}}
	h.m.cfg.Live = &fakeSpecPermissionLive{Live: live, project: "ork", item: "S90", answer: "allow", question: q}
	msg := launchPlanning(t, h)()
	h.m.Update(msg)
	state := &h.m.launch.specState
	late := specPermissionMsg{state: state, epoch: state.permissionEpoch, result: &daemon.SpecPermissionResult{ID: 7, Question: q}}
	h.m.launch.cancel()
	h.deliver(late)
	h.deliver(tickMsg{})
	if strings.Contains(h.view(), q.Title) {
		t.Fatal("canceled sitting reopened a permission question")
	}
}
