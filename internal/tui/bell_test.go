// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func bellWant(t *testing.T, h *harness, want int) {
	t.Helper()
	if h.bells != want {
		t.Fatalf("bells = %d, want %d", h.bells, want)
	}
}

func bellRead(h *harness, s source.Snapshot) {
	h.src.set(s)
	refreshProject(h, s.Project.Name)
}

func bellEngine(p registry.Project, state string) source.Snapshot {
	s := snap(p, nil, nil)
	s.EngineRuns = []source.EngineRun{{ID: "engine-1", State: state}}
	return s
}

func bellLedger(s source.Snapshot, state string) source.Snapshot {
	s.BoardRuns.Value = []slicercli.RelayRun{{ID: "run-1", State: state, Outcome: "fail"}}
	return s
}

func TestBellRingsWhenAnEngineRunEnds(t *testing.T) {
	for _, state := range []string{"committed", "stopped"} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t, four[0])
			bellRead(h, bellEngine(four[0], "implement"))
			bellWant(t, h, 0)
			bellRead(h, bellEngine(four[0], state))
			bellWant(t, h, 1)
			bellRead(h, bellEngine(four[0], state))
			bellWant(t, h, 1)
		})
	}
}

func TestBellRingsWhenALedgerOnlyRunEnds(t *testing.T) {
	h := newHarness(t, four[0])
	bellRead(h, bellLedger(snap(four[0], nil, nil), "active"))
	bellWant(t, h, 0)
	bellRead(h, bellLedger(snap(four[0], nil, nil), "ended"))
	bellWant(t, h, 1)
	bellRead(h, bellLedger(snap(four[0], nil, nil), "ended"))
	bellWant(t, h, 1)
}

func TestBellRingsOnceForALinkedEngineAndLedgerRun(t *testing.T) {
	for _, staggered := range []bool{false, true} {
		t.Run(map[bool]string{false: "together", true: "two-readings"}[staggered], func(t *testing.T) {
			h := newHarness(t, four[0])
			s := bellLedger(bellEngine(four[0], "implement"), "active")
			s.EngineRuns[0].RelayRun = "run-1"
			bellRead(h, s)
			s.EngineRuns[0].State = "committed"
			if !staggered {
				s.BoardRuns.Value[0].State = "ended"
			}
			bellRead(h, s)
			bellWant(t, h, 1)
			s.BoardRuns.Value[0].State = "ended"
			bellRead(h, s)
			bellWant(t, h, 1)
		})
	}
}

func TestBellDoesNotRingForARunAlreadyEndedAtStart(t *testing.T) {
	h := newHarness(t, four[0])
	s := bellLedger(bellEngine(four[0], "committed"), "ended")
	bellRead(h, s)
	bellWant(t, h, 0)
	bellRead(h, s)
	bellWant(t, h, 0)
}

func TestBellEndedRunsBaselineAgainAfterSetup(t *testing.T) {
	h := newHarness(t, four[0])
	bellRead(h, snap(four[0], nil, nil))
	bellRead(h, noSlicer(four[0]))
	s := bellLedger(bellEngine(four[0], "committed"), "ended")
	bellRead(h, s)
	bellWant(t, h, 0)
	bellRead(h, s)
	bellWant(t, h, 0)
	// A new end after setup still rings.
	s.EngineRuns = append(s.EngineRuns, source.EngineRun{ID: "new", State: "stopped"})
	bellRead(h, s)
	bellWant(t, h, 1)
}

func TestBellFailedBoardReadKeepsTheEndedRunsAndRingsNothing(t *testing.T) {
	h := newHarness(t, four[0])
	s := bellLedger(snap(four[0], nil, nil), "ended")
	bellRead(h, s)
	failed := snap(four[0], nil, nil)
	failed.BoardRuns.Err = errors.New("board unavailable")
	failed.BoardRuns.Value = []slicercli.RelayRun{{ID: "bad", State: "ended"}}
	bellRead(h, failed)
	bellWant(t, h, 0)
	bellRead(h, s)
	bellWant(t, h, 0)
	// An id supplied by the failed read must still be new on recovery.
	s.BoardRuns.Value = failed.BoardRuns.Value
	bellRead(h, s)
	bellWant(t, h, 1)
}

func TestBellRingsOncePerSnapshot(t *testing.T) {
	h := newHarness(t, four[0])
	bellRead(h, bellEngine(four[0], "implement"))
	withOpen(h, 1)
	s := h.src.snaps[four[0].Name]
	s = bellLedger(s, "ended")
	s.EngineRuns = bellEngine(four[0], "committed").EngineRuns
	bellRead(h, s)
	bellWant(t, h, 1)
}

func TestBellStillRingsWhenCountRises(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(map[bool]string{false: "open", true: "held"}[held], func(t *testing.T) {
			h := newHarness(t, four[0])
			bellRead(h, snap(four[0], nil, nil))
			withOpen(h, 1)
			s := h.src.snaps[four[0].Name]
			if held {
				s.Open.Value = nil
				s.Items.Value = []slicercli.Item{item("S1", "held", "needs-owner")}
			}
			bellRead(h, s)
			bellWant(t, h, 1)
		})
	}
}

func TestBellOffForAProjectWithBellOff(t *testing.T) {
	off := false
	p := four[0]
	p.Bell = &off
	for _, event := range []string{"open", "held", "end"} {
		t.Run(event, func(t *testing.T) {
			h := newHarness(t, p, four[1])
			bellRead(h, snap(p, nil, nil))
			s := snap(p, nil, nil)
			switch event {
			case "open":
				s.Open.Value = append(s.Open.Value, openRec(1, "bell.test", "bell question"))
			case "held":
				s.Items.Value = []slicercli.Item{item("S1", "bell held", "needs-owner")}
			case "end":
				s.EngineRuns = []source.EngineRun{{ID: "engine-1", State: "stopped"}}
			}
			bellRead(h, s)
			bellWant(t, h, 0)
			if v := compactSpaces(h.view()); !strings.Contains(v, "slicer 1") {
				t.Fatalf("muted project badge did not update: %s", v)
			}
			bellRead(h, snap(four[1], nil, nil))
			bellRead(h, bellEngine(four[1], "stopped"))
			bellWant(t, h, 1)
		})
	}
}

func TestBellOnWhenTheFieldIsAbsentOrTrue(t *testing.T) {
	on := true
	for _, value := range []*bool{nil, &on} {
		t.Run(map[bool]string{true: "absent", false: "true"}[value == nil], func(t *testing.T) {
			p := four[0]
			p.Bell = value
			h := newHarness(t, p)
			bellRead(h, snap(p, nil, nil))
			bellRead(h, bellEngine(p, "stopped"))
			bellWant(t, h, 1)
		})
	}
}

func TestBellSkipsUnavailableEngineRuns(t *testing.T) {
	h := newHarness(t, four[0])
	bellRead(h, snap(four[0], nil, nil))
	s := bellEngine(four[0], "stopped")
	s.EngineRuns[0].Unavailable = true
	bellRead(h, s)
	bellWant(t, h, 0)
	s.EngineRuns[0].Unavailable = false
	bellRead(h, s)
	bellWant(t, h, 1)
}

func TestBellBoardBaselineWaitsForGoodRead(t *testing.T) {
	h := newHarness(t, four[0])
	s := bellLedger(snap(four[0], nil, nil), "ended")
	s.BoardRuns.Err = errors.New("board unavailable")
	bellRead(h, s)
	bellWant(t, h, 0)
	s.BoardRuns.Err = nil
	bellRead(h, s)
	bellWant(t, h, 0)
	s.BoardRuns.Value = append(s.BoardRuns.Value, slicercli.RelayRun{ID: "new", State: "ended"})
	bellRead(h, s)
	bellWant(t, h, 1)
}
