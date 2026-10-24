// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/squalor-xyz/ork/internal/source"
)

// pipelinePhases ignores earlier-round results after a fix round starts.
func pipelinePhases(steps []runTreeStep) map[string]string {
	steps = laneSteps(steps)
	latestImpl := -1
	rounds := 0
	for i, s := range steps {
		if s.Role == "implementer" {
			latestImpl = i
			rounds++
		}
	}
	phases := map[string]string{}
	for i, s := range steps {
		if rounds > 1 && i < latestImpl {
			continue
		}
		phases[s.Role] = s.Phase
	}
	return phases
}

func stageBar(steps []runTreeStep, frame int, live bool) string {
	phases := pipelinePhases(steps)
	if !live {
		frame = 0
	}
	var b strings.Builder
	b.WriteByte('[')
	for _, role := range pipelineRoles(laneSteps(steps)) {
		cell := ".."
		switch phases[role] {
		case "done":
			cell = "##"
		case "fail":
			cell = "!!"
		case "start", "stale", "ended", "paused", "stopped", "committed":
			cell = []string{"~~", "~=", "==", "=~"}[frame%4]
		}
		b.WriteString(cell)
	}
	b.WriteByte(']')
	if n := roleCounts(steps)["implementer"]; n > 1 {
		fmt.Fprintf(&b, " r%d", n)
	}
	return b.String()
}

func sparkline(counts []int, ascii bool) string {
	glyphs := []rune("▁▂▃▄▅▆▇█")
	if ascii {
		glyphs = []rune("._-=+#")
	}
	peak := 0
	for _, n := range counts {
		peak = max(peak, n)
	}
	var b strings.Builder
	for _, n := range counts {
		level := 0
		if peak > 0 {
			level = max(n, 0) * (len(glyphs) - 1) / peak
		}
		if n > 0 {
			level = max(level, 1)
		}
		b.WriteRune(glyphs[level])
	}
	return b.String()
}

// frameVisible uses the same rows and pane targets as the Overview.
func (m *Model) frameVisible() bool {
	if m.showHelp {
		return false
	}
	if m.pane != nil {
		r, _, ok := m.paneRow()
		if ok && boardLive(r.state) && r.state != "stale" {
			state := m.paneBoardRun().State
			if boardLive(state) && state != "stale" {
				return true
			}
		}
	}
	if m.view != 0 {
		return false
	}
	_, rows := m.rows()
	for _, r := range rows {
		if r.engineID != "" && !r.dim {
			return true
		}
		if r.ledgerID != "" && !r.dim && boardLive(r.state) {
			return true
		}
	}
	return false
}

func (m *Model) armFrame() tea.Cmd {
	if m.frameArmed || !m.frameVisible() {
		return nil
	}
	m.frameArmed = true
	return m.cfg.FrameAfter(250 * time.Millisecond)
}

func (m *Model) paneActivity() []int {
	if m.pane == nil || m.pane.ledger {
		return nil
	}
	p := m.byName[m.pane.project]
	if p == nil {
		return nil
	}
	for _, r := range p.snap.EngineRuns {
		if r.ID == m.pane.run && r.Section(p.snap.EngineRuns) == source.SectionRunning {
			return r.Activity
		}
	}
	return nil
}
