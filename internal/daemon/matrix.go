// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// RolesMatrixParams selects the project whose role matrix roles.matrix reads.
type RolesMatrixParams struct {
	Project string `json:"project"`
}

// MatrixRow is one matrix row with what ork does with it (Use).
type MatrixRow struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Agent     string `json:"agent"`
	Vendor    string `json:"vendor,omitempty"`    // proposed (S173)
	Model     string `json:"model,omitempty"`     // proposed (S173)
	Reasoning string `json:"reasoning,omitempty"` // proposed (S173)
	Note      string `json:"note,omitempty"`      // proposed (S173)
	Use       string `json:"use"`
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
	uiRows, specRows := uiPassRowSet(project, m.Rules), specCheckRowSet(project, m.Rules)
	implPerm, permErr := engine.ResolvePermission(project, engine.RoleImplementer)
	implReadOnly := permErr != nil || implPerm.ReadOnly
	out := &RolesMatrixResult{Markdown: m.Markdown, Rows: []MatrixRow{}, Rules: m.Rules}
	if out.Rules == nil {
		out.Rules = []slicercli.Rule{}
	}
	for _, row := range m.Rows {
		out.Rows = append(out.Rows, MatrixRow{
			ID: row.ID, Role: row.Role, Agent: row.Agent,
			Vendor: row.Vendor, Model: row.Model, Reasoning: row.Reasoning, Note: row.Note,
			Use: matrixUse(row, uiRows, specRows, implReadOnly),
		})
	}
	return out, nil
}

// MatrixEditParams is one edit of the project's role matrix (S173,
// *proposed*). Op is add, set, remove, move or rule-edit. Fields is for add and
// set: a flag name to a text value, where an absent name is not passed and an
// empty value clears the field. Before or Last is for move, Text for rule-edit.
// Preview returns the command without running it.
type MatrixEditParams struct {
	Project string              `json:"project"`
	Op      string              `json:"op"`
	ID      string              `json:"id,omitempty"`
	Fields  slicercli.RowFields `json:"fields,omitempty"`
	Before  string              `json:"before,omitempty"`
	Last    bool                `json:"last,omitempty"`
	Text    string              `json:"text,omitempty"`
	Preview bool                `json:"preview,omitempty"`
}

// MatrixEditResult is the line the owner confirms (Command), a warning when
// the edit names an agent that ork has no adapter for, the new row's id after
// an add, and whether slicer ran the edit.
type MatrixEditResult struct {
	Command string `json:"command"`
	Warning string `json:"warning,omitempty"`
	ID      string `json:"id,omitempty"`
	Done    bool   `json:"done"`
}

// matrixEdit previews or runs one `slicer relay-roles` write in the registered
// project's main checkout, with --render and no run record. The change is left
// uncommitted for the owner, and slicer's refusal comes back as it came.
// Nothing refreshes the cache: roles.matrix reads slicer on each call and the
// matrix is not part of the snapshot.
func (d *Daemon) matrixEdit(ctx context.Context, p MatrixEditParams) (*MatrixEditResult, error) {
	if p.Project == "" {
		return nil, badParams(errors.New("matrix.edit needs a project"))
	}
	args, err := slicercli.RowArgs(p.Op, p.ID, p.Fields, p.Before, p.Last, p.Text)
	if err != nil {
		return nil, badParams(err)
	}
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	out := &MatrixEditResult{Command: matrixCommand(args)}
	if agent, ok := p.Fields["agent"]; ok && matrixAdapters[strings.ToLower(agent)] == "" {
		out.Warning = `agent "` + agent + `" has no ork adapter: a plan skips this row`
	}
	if p.Preview {
		return out, nil
	}
	slicer, w := d.runDeps(proj).Slicer, runrec.Writer{}
	switch p.Op {
	case "add":
		out.ID, err = slicer.RelayRowAdd(ctx, w, proj.Path, p.Fields)
	case "set":
		err = slicer.RelayRowSet(ctx, w, proj.Path, p.ID, p.Fields)
	case "remove":
		err = slicer.RelayRowRemove(ctx, w, proj.Path, p.ID)
	case "move":
		err = slicer.RelayRowMove(ctx, w, proj.Path, p.ID, p.Before, p.Last)
	case "rule-edit":
		err = slicer.RelayRuleEdit(ctx, w, proj.Path, p.ID, p.Text)
	}
	if err != nil {
		return nil, err
	}
	out.Done = true
	return out, nil
}

// matrixCommand is the line the owner confirms: `slicer` and the arguments of
// the call with --render, without --json or --root, quoted for a shell.
func matrixCommand(args []string) string {
	words := []string{"slicer"}
	for _, a := range args {
		words = append(words, quoteArg(a))
	}
	return strings.Join(append(words, "--render"), " ")
}

// quoteArg quotes an argument for a shell when it holds anything but letters,
// digits and _@%+=:,./-. Only the value of a --name=VALUE argument is quoted,
// so that the line reads --note='two words'; the shell reads the same word.
func quoteArg(a string) string {
	if name, value, ok := strings.Cut(a, "="); ok && strings.HasPrefix(name, "--") && !strings.ContainsFunc(name, func(r rune) bool { return r != '-' && (r < 'a' || r > 'z') }) {
		if value == "" {
			return a
		}
		return name + "=" + quoteWord(value)
	}
	return quoteWord(a)
}

func quoteWord(s string) string {
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r))
	}) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// matrixUse is what ork does with a row. The gate follows the reviewer's
// lineup (S83), ork is the orchestrator, and a row maps to an engine adapter
// by its agent (adj 155). The UI pass row and the Spec check row are
// reviewer rows that a rule names; they use the same adapters.
//
// implementerReadOnly is whether the project gives the implementer a read-only
// permission; a grok row of the implementer is skipped unless it does (S268,
// *proposed*).
func matrixUse(row slicercli.Row, uiRows, specRows []string, implementerReadOnly bool) string {
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
		if row.Role == engine.RoleImplementer && !implementerReadOnly && isGrokVendor(adapter) {
			use = "skipped: " + grokNoWrite
		} else if reason := launcherMissing(adapter); reason != "" {
			use = reason
		}
	}
	switch {
	case row.ID != "" && slices.Contains(uiRows, row.ID):
		return "UI pass row, " + use
	case slices.Contains(specRows, row.ID):
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
