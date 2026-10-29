// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// RolesMatrixParams selects the project whose role matrix roles.matrix reads.
type RolesMatrixParams struct {
	Project string `json:"project"`
}

// MatrixRow is one matrix row with what ork does with it (Use).
type MatrixRow struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Agent string `json:"agent"`
	Use   string `json:"use"`
}

// RolesMatrixResult is slicer's matrix text and rules with ork's reading of
// each row.
type RolesMatrixResult struct {
	Markdown string           `json:"markdown"`
	Rows     []MatrixRow      `json:"rows"`
	Rules    []slicercli.Rule `json:"rules"`
}

// rolesMatrix reads the project's matrix through `slicer relay-roles show` and
// says, row by row, what ork does with it. It writes nothing and does not
// change how a plan reads the matrix.
func (d *Daemon) rolesMatrix(ctx context.Context, p RolesMatrixParams) (*RolesMatrixResult, error) {
	if p.Project == "" {
		return nil, badParams(errors.New("roles.matrix needs a project"))
	}
	project, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	m, err := d.runs.deps.Slicer.RelayMatrix(ctx, project.Path)
	if err != nil {
		return nil, err
	}
	uiRow, specRow := slicercli.UIPassRowID(m.Rules), slicercli.SpecCheckRowID(m.Rules)
	out := &RolesMatrixResult{Markdown: m.Markdown, Rows: []MatrixRow{}, Rules: m.Rules}
	if out.Rules == nil {
		out.Rules = []slicercli.Rule{}
	}
	for _, row := range m.Rows {
		out.Rows = append(out.Rows, MatrixRow{ID: row.ID, Role: row.Role, Agent: row.Agent, Use: matrixUse(row, uiRow, specRow)})
	}
	return out, nil
}

// matrixUse is what ork does with a row. The gate follows the reviewer's
// lineup (S83), ork is the orchestrator, and a row maps to an engine adapter
// by its agent (adj 155). The UI pass row and the Spec check row are
// reviewer rows that a rule names; they use the same adapters.
func matrixUse(row slicercli.Row, uiRow, specRow string) string {
	switch row.Role {
	case engine.RoleGate:
		return "not used: the gate runs the reviewer's lineup"
	case "orchestrator":
		return "not run by ork"
	case engine.RoleImplementer, engine.RoleReviewer, engine.RoleArchitect:
	default:
		return "not used: ork has no role " + row.Role
	}
	use := "skipped: no adapter for " + row.Agent
	if adapter := matrixAdapters[strings.ToLower(row.Agent)]; adapter != "" {
		use = "used as " + adapter
	}
	switch {
	case uiRow != "" && row.ID == uiRow:
		return "UI pass row, " + use
	case specRow != "" && row.ID == specRow:
		return "Spec check row, " + use
	}
	return use
}

// FormatMatrix is the text of the result: slicer's matrix text, a blank line,
// then ork's section with one line per row. The CLI and the TUI both show it.
func FormatMatrix(r *RolesMatrixResult) []string {
	var lines []string
	if text := strings.TrimRight(r.Markdown, "\n"); text != "" {
		lines = append(strings.Split(text, "\n"), "")
	}
	lines = append(lines, "## ork")
	for _, row := range r.Rows {
		lines = append(lines, "- "+row.ID+" "+row.Role+": "+row.Use)
	}
	return lines
}

// FormatMatrixOrkFirst is the same lines with ork's section above slicer's
// text, the order of the TUI pane: the section, a blank line, then the text.
func FormatMatrixOrkFirst(r *RolesMatrixResult) []string {
	lines := []string{"## ork"}
	for _, row := range r.Rows {
		lines = append(lines, "- "+row.ID+" "+row.Role+": "+row.Use)
	}
	if text := strings.TrimRight(r.Markdown, "\n"); text != "" {
		lines = append(lines, "")
		lines = append(lines, strings.Split(text, "\n")...)
	}
	return lines
}
