// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/source"
)

// vendorOutAt is when the vendors of these tests went out, before the golden
// clock (16:42:07).
var vendorOutAt = time.Date(2026, 10, 19, 16, 2, 0, 0, time.UTC)

// vendorModel is the golden Overview with a vendor that is out and a run of
// the first project that waits for the slot of that vendor (S169).
func vendorModel(theme Theme, w, h int, out []source.VendorOut, wait string) *Model {
	return vendorModelStarted(theme, w, h, out, wait, time.Date(2026, 10, 19, 16, 30, 0, 0, time.UTC))
}

// vendorModelStarted is vendorModel with the time the run began, which is when
// its Running row last heard from it (the golden clock is 16:42:07).
func vendorModelStarted(theme Theme, w, h int, out []source.VendorOut, wait string, started time.Time) *Model {
	m := goldenModel(theme, w, h)
	s := m.projects[0].snap
	s.VendorsOut = out
	s.EngineRuns = []source.EngineRun{{ID: "engine", Item: "S12", State: "implement", VendorWait: wait, Tail: "reading the spec\nrunning the tests", CreatedAt: started}}
	m.Update(snapshotMsg{name: s.Project.Name, snap: s})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func codexOut() []source.VendorOut {
	return []source.VendorOut{{Vendor: "codex", Reason: "You've hit your usage limit", Since: vendorOutAt, Until: vendorOutAt.Add(time.Hour)}}
}

// ownerOut is a mark the owner set: it has no end.
func ownerOut() []source.VendorOut {
	return []source.VendorOut{{Vendor: "codex", Reason: "marked out by the owner", Since: vendorOutAt}}
}

func TestStatusLineAndRunningRowShowVendorState(t *testing.T) {
	for _, mono := range []bool{true, false} {
		name := fmt.Sprintf("vendor-out-100x30-%s.txt", map[bool]string{true: "mono", false: "color"}[mono])
		t.Run(name, func(t *testing.T) {
			theme := NewTheme(mono)
			m := vendorModel(theme, 100, 30, codexOut(), "codex")
			view := m.View().Content
			checkGolden(t, name, view)

			// The status line names the vendor in the warning style.
			line := m.statusLine(100)
			if want := theme.Warn.Render("codex out until 17:02"); !strings.Contains(line, want) {
				t.Fatalf("status line lacks the warning field %q:\n%q", want, line)
			}
			plain := stripANSI(line)
			if strings.Index(plain, "codex out until 17:02") > strings.Index(plain, "4 projects") {
				t.Fatalf("the vendor field must come before the project count: %q", plain)
			}
			// The waiting run says so on line 2, instead of its output tail.
			body := stripANSI(strings.Join(m.overview(120).body, "\n"))
			if !strings.Contains(body, "waits for codex") || strings.Contains(body, "slot") {
				t.Fatalf("Running row lacks the wait:\n%s", body)
			}
			if strings.Contains(body, "running the tests") {
				t.Fatalf("the tail shows beside the wait:\n%s", body)
			}
		})
	}
}

func TestVendorFieldsOnlyWhenAVendorIsOutAndTheTailOtherwise(t *testing.T) {
	m := vendorModel(plainTheme(), 100, 30, nil, "")
	if line := stripANSI(m.statusLine(100)); strings.Contains(line, " out until ") {
		t.Fatalf("status line names a vendor that is not out: %q", line)
	}
	body := stripANSI(strings.Join(m.overview(120).body, "\n"))
	if strings.Contains(body, "waits for") || !strings.Contains(body, "running the tests") {
		t.Fatalf("a run that waits for no slot shows its tail:\n%s", body)
	}

	// Two vendors, one field each, in vendor order; the fields lead the line,
	// so a narrow line keeps them and drops the rest.
	both := []source.VendorOut{
		{Vendor: "codex", Since: vendorOutAt, Until: vendorOutAt.Add(time.Hour)},
		{Vendor: "claude", Since: vendorOutAt.Add(5 * time.Minute)}, // an owner mark
	}
	m = vendorModel(plainTheme(), 100, 30, both, "")
	line := stripANSI(m.statusLine(100))
	if i, j := strings.Index(line, "claude out until back"), strings.Index(line, "codex out until 17:02"); i < 0 || j < i {
		t.Fatalf("one field per vendor, in vendor order: %q", line)
	}
	if narrow := stripANSI(m.statusLine(50)); !strings.Contains(narrow, "claude out until back") || strings.Contains(narrow, "refreshed") {
		t.Fatalf("a narrow line keeps the vendor field and drops the rest: %q", narrow)
	}
	// The daemon notice stays in front of the vendor fields.
	m.cfg.BuildID, m.cfg.DaemonBuild = "new", "old"
	if line := stripANSI(m.statusLine(200)); !strings.HasPrefix(line, " daemon build differs: ork daemon restart | claude out until back") {
		t.Fatalf("the daemon notice must lead the line: %q", line)
	}
	m.cfg.BuildID, m.cfg.DaemonBuild = "", ""
	// The vendors are global: another project's snapshot says the same, and
	// the field appears once.
	other := m.projects[1].snap
	other.VendorsOut = both
	m.Update(snapshotMsg{name: other.Project.Name, snap: other})
	if line := stripANSI(m.statusLine(200)); strings.Count(line, "claude out until back") != 1 {
		t.Fatalf("the vendor field repeats per project: %q", line)
	}
}

// The wait text fits beside a long slice title at the narrow sizes (S169).
func TestWaitTextFitsBesideALongTitle(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{80, 24}, {100, 30}} {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			m := vendorModel(plainTheme(), sz.w, sz.h, codexOut(), "codex")
			view := stripANSI(m.View().Content)
			if !strings.Contains(view, "Roadmap") || !strings.Contains(view, "waits for codex") {
				t.Fatalf("the long title or the wait text is cut:\n%s", view)
			}
		})
	}
}

// While a round waits for a vendor slot the run has not gone quiet: the heard
// cell is dim and carries no cue (S169, owner decision 2026-10-29).
func TestHeardCellOfAWaitingRunIsDimWithNoCue(t *testing.T) {
	for _, tc := range []struct {
		name    string
		started time.Time
		cue     string
		heard   string
	}{
		{"amber", time.Date(2026, 10, 19, 16, 30, 0, 0, time.UTC), "! heard 12m", "heard 12m"},
		{"red", time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC), "!! heard 42m", "heard 42m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			theme := NewTheme(false)
			plain := vendorModelStarted(theme, 100, 30, codexOut(), "", tc.started)
			if body := stripANSI(strings.Join(plain.overview(120).body, "\n")); !strings.Contains(body, tc.cue) {
				t.Fatalf("without a wait the row carries the cue %q:\n%s", tc.cue, body)
			}
			m := vendorModelStarted(theme, 100, 30, codexOut(), "codex", tc.started)
			raw := strings.Join(m.overview(120).body, "\n")
			body := stripANSI(raw)
			if strings.Contains(body, "! heard") || strings.Contains(body, "!! heard") || !strings.Contains(body, tc.heard) {
				t.Fatalf("a waiting run shows %q with no cue:\n%s", tc.heard, body)
			}
			if !strings.Contains(raw, theme.Dim.Render(tc.heard)) {
				t.Fatalf("the heard cell must be dim: %q", raw)
			}
		})
	}
}
