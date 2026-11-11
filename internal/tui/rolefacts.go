// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

// roleFact is what an open Running row says about one active role: the facts its
// row lacks (S333, adj 283, ui.running-expand). The row already shows the item,
// the stage bar, last heard and the pipeline, so none of them is here.
type roleFact struct {
	role, entry string    // the role and its "vendor model effort"
	began       time.Time // when the role started; zero when unknown
	usage       string    // "tokens 12.4k  $-"; "" when the source reports no usage
	output      string    // the role's last output line; "" when it has said nothing
}

// engineRoleFacts is a line per role of the engine run that has not ended. The
// run's live tokens and its output tail belong to the last of them, as the run
// detail reads them (roleDetails). A run that is not live has no role working.
func engineRoleFacts(r source.EngineRun) []roleFact {
	if !boardLive(r.State) {
		return nil
	}
	active, recorded := -1, 0
	for i, role := range r.Roles {
		recorded += role.Tokens
		if role.EndedAt.IsZero() {
			active = i
		}
	}
	var out []roleFact
	for j, role := range r.Roles {
		if !role.EndedAt.IsZero() {
			continue
		}
		tokens, known := role.Tokens, role.Tokens != 0
		f := roleFact{role: role.Role, began: role.StartedAt, entry: entryText(runrec.LineupEntry{Vendor: role.Vendor, Model: role.Model, Effort: role.Effort})}
		if j == active {
			tokens = max(r.Tokens-recorded, 0)
			known = r.LiveUsageKnown || tokens != 0
			f.output = lastTailLine(r.Tail)
		}
		f.usage = usageText(tokens, known, 0, false, role.Vendor)
		out = append(out, f)
	}
	return out
}

// ledgerRoleFacts is a line per step of a relay started elsewhere that is
// running. The ledger holds no usage and no output, so the line has neither. A
// stale run has no role working; ork's own orchestrator step is not a role's work.
func ledgerRoleFacts(r slicercli.RelayRun) []roleFact {
	if r.State == "stale" || !boardLive(r.State) {
		return nil
	}
	var out []roleFact
	for _, s := range r.Steps {
		if s.Phase != "start" || isEngineStep(s) {
			continue
		}
		f := roleFact{role: s.Role, entry: entryText(runrec.LineupEntry{Vendor: s.Vendor, Model: s.Model, Effort: s.Reasoning})}
		if at, err := time.Parse(time.RFC3339Nano, s.BeganAt); err == nil {
			f.began = at
		}
		out = append(out, f)
	}
	return out
}

// usageText is "tokens N  $COST" as the run detail draws it: "-" for what is not
// known, and "local" in place of a cost for a local vendor.
func usageText(tokens int, tokensKnown bool, cost float64, costKnown bool, vendor string) string {
	c := "$-"
	if costKnown {
		c = fmt.Sprintf("$%.2f", cost)
	}
	if engine.IsLocalVendor(vendor) {
		c = "local"
	}
	t := "-"
	if tokensKnown {
		t = compactTokens(tokens)
	}
	return fmt.Sprintf("tokens %s  %s", t, c)
}

// expandedLines is the detail of an open Running run, set in under its row: one
// dim line per active role, and under it the role's last output line. Nothing
// the row shows is said again (S333, adj 283). A role that has only its name
// (a relay started elsewhere whose step has no vendor, model or start time) has
// no line, the row names it. A run with no line left, because no role works (a
// question, a pause, a stop), gets one line of the facts its row lacks, which
// is its question or its stop reason (owner 2026-11-11, ui.running-expand-idle).
func (m *Model) expandedLines(r ovRow, w int) []string {
	if r.live == nil {
		return nil
	}
	prefix := indent + indent
	var lines []string
	for _, f := range r.live.roles {
		cells := []string{f.role}
		if f.entry != "" {
			cells = append(cells, f.entry)
		}
		if !f.began.IsZero() {
			cells = append(cells, elapsed(m.cfg.Now().Sub(f.began)))
		}
		if f.usage != "" {
			cells = append(cells, f.usage)
		}
		if len(cells) > 1 {
			lines = append(lines, m.cfg.Theme.Dim.Render(clip(prefix+strings.Join(cells, "  "), w)))
		}
		if f.output != "" {
			lines = append(lines, m.cfg.Theme.Dim.Render(clip(prefix+"> "+f.output, w)))
		}
	}
	if len(lines) == 0 && len(r.detailCells) > 0 {
		lines = append(lines, m.cfg.Theme.Dim.Render(clip(prefix+strings.Join(r.detailCells, "  "), w)))
	}
	return lines
}

// staleLine is the Running list's trailing `stale: N` line: dim, one cursor row
// (S333, adj 284, ui.stale-ledger-access).
func (m *Model) staleLine(r ovRow, selected bool, w int) string {
	const lead = "   " // under the mark and the project name of the rows above
	text := clip(r.text, max(w-len(lead), 1))
	if selected {
		line := lead + text
		return m.cfg.Theme.TabActive.Render(line + strings.Repeat(" ", max(w-len([]rune(line)), 0)))
	}
	return lead + m.cfg.Theme.Dim.Render(text)
}
