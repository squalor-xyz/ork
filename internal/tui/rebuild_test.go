// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestLandedRebuildNotice(t *testing.T) {
	for _, tc := range []struct {
		name                string
		self, behind, build bool
	}{
		{"configured", true, true, true}, {"not-self", false, true, true}, {"current", true, false, true}, {"unset", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := actionHarness(t, "awaiting-approval", "")
			ps := h.m.byName["ork"]
			ps.snap.SelfHost, ps.snap.BehindMain = tc.self, tc.behind
			if tc.build {
				p := ps.reg
				json.Unmarshal([]byte(`{"build_command":"./build"}`), &p)
				ps.reg = p
				ps.snap.Project = p
			}
			h.m.cfg.Live.(*actionLive).src.set(ps.snap)
			// Main moves at landing: the model's cached build can still be current.
			ps.snap.BehindMain = false
			h.m.cfg.Live.(*actionLive).approved = runrec.Record{State: runrec.StateCommitted, LandedAt: time.Now()}
			h.deliver(press("a"))
			_, cmd := h.m.Update(press("y"))
			if cmd == nil {
				t.Fatal("no approval command")
			}
			h.deliver(cmd())
			got := h.view()
			want := "landed S22"
			if tc.self && tc.behind && tc.build {
				want += "; ork daemon restart --rebuild"
			}
			if !strings.Contains(got, want) || (!(tc.self && tc.behind && tc.build) && strings.Contains(got, "--rebuild")) {
				t.Fatalf("landed line lacks %q:\n%s", want, got)
			}
		})
	}
}
