// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
	"strings"
	"testing"
)

type permissionLive struct{ *actionLive }

func (l permissionLive) AnswerAction(ctx context.Context, a daemon.RunAction) (string, error) {
	if err := l.action(a.Run, "implement"); err != nil {
		return "", err
	}
	s := l.src.Snapshot(ctx, proj("ork", false))
	for i := range s.EngineRuns {
		s.EngineRuns[i].Pending = ""
	}
	l.src.set(s)
	return "", nil
}

func TestLivePermissionQuestion(t *testing.T) {
	h := actionHarness(t, "implement", "permission")
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns[0].Roles = []source.EngineRole{{Role: "implementer", Round: 1, StartedAt: t0}}
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.m.cfg.Live = permissionLive{h.m.cfg.Live.(*actionLive)}
	h.m.view = questionsView
	h.deliver(press("enter"))
	if h.m.pane == nil || !h.m.paneQuestion() || !strings.Contains(h.view(), "Answer:") {
		t.Fatalf("live permission has no answer surface:\n%s", h.view())
	}
	typeAnswer(h, "allow")
	h.deliver(press("enter"))
	if !strings.Contains(h.view(), "answered") {
		t.Fatalf("permission reply not sent:\n%s", h.view())
	}
	if strings.Contains(h.view(), "resumed") {
		t.Fatalf("permission answer falsely resumed the active role:\n%s", h.view())
	}
}

func TestPermissionQuestionOptionsOnly(t *testing.T) {
	h := actionHarness(t, "implement", "permission")
	s := h.src.Snapshot(context.Background(), proj("ork", false))
	s.EngineRuns[0].Question = &runrec.OwnerQuestion{Title: "Allow Bash: make test?", Options: []runrec.Option{{Label: "Allow", Summary: "this call"}, {Label: "Deny", Summary: "this call"}}, Recommend: 2, Reason: "not allowlisted"}
	h.src.set(s)
	h.m.apply(snapshotMsg{name: "ork", snap: s})
	h.m.view = questionsView
	h.deliver(press("enter"))
	if view := h.view(); strings.Contains(view, "free text") || !strings.Contains(view, "1-2 pick") {
		t.Errorf("permission hints offer unsupported free text:\n%s", view)
	}
	h.deliver(press("t"))
	if view := h.view(); strings.Contains(view, "Answer:") || !strings.Contains(view, "1. Allow") {
		t.Fatalf("permission question entered a free-text field:\n%s", view)
	}
}
