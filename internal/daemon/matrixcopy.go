// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// MatrixCopyParams copies the role matrix of the registered project From to the
// projects in To, or to every other registered project when All is set (S261,
// *proposed*). Exactly one of To and All is given. A dry run writes nothing.
//
// ExpectSteps binds a write to the preview the owner confirmed (S548, owner
// decision matrix.copy-bind-preview): it maps a project's name to the steps its
// preview showed. The daemon dry-runs each such project again and, when the
// steps differ, refuses that project with CodeStalePreview and writes nothing
// to it; the other projects go on. A project without an entry is not bound. A
// dry run ignores ExpectSteps.
//
// ExpectRows binds a write to the rows and rules the preview showed (S558,
// owner decision matrix.copy-bind-content): it maps a project's name to the
// Digest its preview carried. The steps alone miss a source row whose content
// or copied rule text changed after the preview, since the steps can stay the same. The daemon
// plans each such project again and, when the digest differs, refuses that
// project with CodeStalePreview and writes nothing to it; the other projects go
// on. A project without an entry is not bound. A dry run ignores ExpectRows.
type MatrixCopyParams struct {
	From        string                      `json:"from"`
	To          []string                    `json:"to,omitempty"`
	All         bool                        `json:"all,omitempty"`
	DryRun      bool                        `json:"dry_run,omitempty"`
	ExpectSteps map[string][]MatrixCopyStep `json:"expect_steps,omitempty"`
	ExpectRows  map[string]string           `json:"expect_rows,omitempty"`
}

// CodeStalePreview is the code of a project the write refused because its steps
// are no longer the ones the owner confirmed (S548).
const CodeStalePreview = "stale_preview"

// CodeAddIDMismatch refuses a project whose dry runs assigned different row ids.
const CodeAddIDMismatch = "add_id_mismatch"

// What a project's copy came to.
const (
	CopyWouldChange = "would change"
	CopyUnchanged   = "unchanged"
	CopyChanged     = "changed"
	CopySkipped     = "skipped"
	CopyFailed      = "failed"
)

// MatrixCopyStep is one step of slicer's result. Fields contains the changed
// row values from ork's set, compared with the current target, for set steps.
type MatrixCopyStep struct {
	Op      string            `json:"op"`
	ID      string            `json:"id"`
	Changed bool              `json:"changed"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// MatrixCopyProject is the outcome for one target. Error is slicer's or ork's
// code and message as they came; a skipped project has one too, which says
// what to run. Digest is the SHA-256 of the rows and rules planned for the project,
// which a preview that would change the project carries, for ExpectRows (S558).
type MatrixCopyProject struct {
	Project  string           `json:"project"`
	Status   string           `json:"status"`
	Steps    []MatrixCopyStep `json:"steps,omitempty"`
	Digest   string           `json:"digest,omitempty"`
	Warnings []string         `json:"warnings,omitempty"`
	Error    *ipc.Error       `json:"error,omitempty"`
}

// MatrixCopyResult is one outcome per target, in request order. Partial is true
// when a write changed some projects and failed others.
type MatrixCopyResult struct {
	From     string              `json:"from"`
	Projects []MatrixCopyProject `json:"projects"`
	Partial  bool                `json:"partial"`
}

// copySet is the change set sent to `slicer relay-roles apply`.
type copySet struct {
	Expect  string      `json:"expect,omitempty"`
	Replace copyReplace `json:"replace"`
}

type copyReplace struct {
	Rows  []copyRowJSON  `json:"rows"`
	Rules []copyRuleJSON `json:"rules"`
}

// copyRowJSON is a row of the replacement matrix. An ID keeps a row of the
// target; a row without one is added.
type copyRowJSON struct {
	ID        string          `json:"id,omitempty"`
	Role      string          `json:"role"`
	When      json.RawMessage `json:"when,omitempty"`
	Agent     string          `json:"agent"`
	Vendor    string          `json:"vendor"`
	Model     string          `json:"model"`
	Reasoning string          `json:"reasoning"`
	Note      string          `json:"note"`
}

type copyRuleJSON struct {
	ID   string `json:"id,omitempty"`
	Text string `json:"text"`
}

// plannedRow is a row of the copy. id is the target's id for a kept or matched
// row and empty for a row that slicer adds; from is the source row's id, empty
// for a kept target row that has no source row.
type plannedRow struct {
	id   string
	from string
	row  slicercli.Row
}

// plannedRule is a rule of the copy: kept (the target's own), or copied from the
// source rule from.
type plannedRule struct {
	id   string
	text string
	from string
}

// copyPlan is the whole-matrix change that makes a target's matrix the
// source's, except for the parts the target keeps.
type copyPlan struct {
	rows  []plannedRow
	rules []plannedRule
	// Warnings are what the owner should know before the copy: a kept rule that
	// names a row the copy removes, and an agent ork has no adapter for.
	Warnings []string

	free    []slicercli.Rule  // target rules the copy does not keep, for matching a copied rule's text
	rowIDs  map[string]string // source row id to the target's id, for the rows whose id is known
	srcRows map[string]bool   // ids of the source's rows
	done    bool              // the copied rules' texts are final
}

var rowToken = regexp.MustCompile(`\br\d+\b`)

// ruleNamesCheckRow says whether a rule names a row of the spec check or the UI
// pass: a "Spec check row (rNN)" or "UI pass row (rNN)", or a Local trial row
// of either (S304). Such a rule belongs to the project that has it and is never
// copied.
func ruleNamesCheckRow(r slicercli.Rule) bool {
	one := []slicercli.Rule{r}
	return len(slicercli.SpecCheckRowIDs(one)) > 0 || len(slicercli.UIPassRowIDs(one)) > 0 ||
		len(slicercli.LocalSpecCheckRowIDs(one)) > 0 || len(slicercli.LocalUIPassRowIDs(one)) > 0
}

// checkRowSets are the rows of a project's spec check and UI pass, in the order
// ork reads them: the rows the project's own settings list (spec_check_rows and
// ui_pass_rows), then the rows its rules name, then its Local trial rows. The UI
// set leaves out a row that is also a spec check row.
func checkRowSets(proj registry.Project, rules []slicercli.Rule) (spec, ui []string) {
	spec = specCheckRowSet(proj, rules)
	ui = slices.DeleteFunc(slices.Clone(uiPassRowSet(proj, rules)), func(id string) bool { return slices.Contains(spec, id) })
	return spec, ui
}

// planMatrixCopy builds the change that replaces target's matrix with source's
// (S261, owner decisions matrix.copy-scope, matrix.copy-keep and, 2026-11-24,
// matrix.copy-settings-rows). sourceProject and project are the registered
// projects the matrices belong to, for the rows their settings name:
//   - Rows are the source's, in order, without ids, so slicer adds them. The
//     target keeps its spec check and UI pass rows by id (the rows its rules
//     name, the rows its spec_check_rows and ui_pass_rows settings list and its
//     Local trial rows), at the places of the source's own such rows, which are
//     not copied. A source row the target already has, field for field, is kept
//     by id as well, so that a second copy changes nothing.
//   - Rules are the target's rules that name those rows first (slicer moves no
//     rule), then the source's other rules, again keeping by id a target rule of
//     the same text. The source's rules that name its own such rows are not
//     copied.
//
// A target without a Spec check rule is refused: ork reads that row through the
// rule (CodeNoSpecCheckRow). A copied rule that names a source row the copy does
// not copy is refused (CodeCopyConflict). The texts of copied rules name the
// source's row ids until rewrite gives the target's.
func planMatrixCopy(sourceProject registry.Project, source *slicercli.Matrix, project registry.Project, target *slicercli.Matrix) (copyPlan, error) {
	if len(slicercli.SpecCheckRowIDs(target.Rules)) == 0 {
		return copyPlan{}, &ipc.Error{Code: CodeNoSpecCheckRow, Message: "no rule names a Spec check row (rNN), and the copy keeps each project's own: add one with slicer relay-roles rule-add, then copy again"}
	}
	specIDs, uiIDs := checkRowSets(project, target.Rules)
	targetRow := map[string]slicercli.Row{}
	for _, r := range target.Rows {
		targetRow[r.ID] = r
	}
	keepRow := func(ids []string) []plannedRow {
		var rows []plannedRow
		for _, id := range ids {
			if r, ok := targetRow[id]; ok {
				rows = append(rows, plannedRow{id: id, row: r})
			}
		}
		return rows
	}
	specQueue, uiQueue := keepRow(specIDs), keepRow(uiIDs)
	kept := map[string]bool{}
	for _, r := range slices.Concat(specQueue, uiQueue) {
		kept[r.id] = true
	}

	srcSpec, srcUI := checkRowSets(sourceProject, source.Rules)
	skipped := func(id string) bool { return slices.Contains(srcSpec, id) || slices.Contains(srcUI, id) }

	p := copyPlan{rowIDs: map[string]string{}, srcRows: map[string]bool{}}
	pop := func(queue *[]plannedRow) {
		if len(*queue) > 0 {
			p.rows = append(p.rows, (*queue)[0])
			*queue = (*queue)[1:]
		}
	}
	used := map[string]bool{}
	for _, s := range source.Rows {
		p.srcRows[s.ID] = true
		switch {
		case slices.Contains(srcSpec, s.ID):
			pop(&specQueue)
			continue
		case slices.Contains(srcUI, s.ID):
			pop(&uiQueue)
			continue
		}
		row := plannedRow{from: s.ID, row: s}
		for _, t := range target.Rows {
			if !kept[t.ID] && !used[t.ID] && sameRow(s, t) {
				used[t.ID] = true
				row.id = t.ID
				p.rowIDs[s.ID] = t.ID
				break
			}
		}
		p.rows = append(p.rows, row)
	}
	p.rows = append(p.rows, specQueue...)
	p.rows = append(p.rows, uiQueue...)

	for _, r := range target.Rules {
		if ruleNamesCheckRow(r) {
			p.rules = append(p.rules, plannedRule{id: r.ID, text: r.Text})
		} else {
			p.free = append(p.free, r)
		}
	}
	keptRules := len(p.rules)
	for _, r := range source.Rules {
		if ruleNamesCheckRow(r) {
			continue
		}
		for _, token := range rowToken.FindAllString(r.Text, -1) {
			if skipped(token) {
				return copyPlan{}, &ipc.Error{Code: CodeCopyConflict, Message: fmt.Sprintf(
					"rule %s names %s, a row the copy does not copy: %s keeps its own spec check and UI pass rows", r.ID, token, project.Name)}
			}
		}
		p.rules = append(p.rules, plannedRule{text: r.Text, from: r.ID})
	}

	retained := map[string]bool{}
	for _, r := range p.rows {
		if r.id != "" {
			retained[r.id] = true
		}
	}
	for _, r := range p.rules[:keptRules] {
		var named []string
		for _, token := range rowToken.FindAllString(r.text, -1) {
			if _, ok := targetRow[token]; ok && !retained[token] && !slices.Contains(named, token) {
				named = append(named, token)
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s names %s, which the copy removes", r.id, token))
			}
		}
	}
	var agents []string
	for _, r := range p.rows {
		if r.id == "" && matrixAdapters[strings.ToLower(r.row.Agent)] == "" && !slices.Contains(agents, r.row.Agent) {
			agents = append(agents, r.row.Agent)
			p.Warnings = append(p.Warnings, fmt.Sprintf("agent %q has no ork adapter: a plan skips this row", r.row.Agent))
		}
	}
	if !p.needsIDs() {
		if err := p.rewrite(nil); err != nil {
			return copyPlan{}, err
		}
	}
	return p, nil
}

// sameRow is true when two rows say the same thing, whatever their ids and
// however slicer spaced the JSON of their conditions.
func sameRow(a, b slicercli.Row) bool {
	return a.Role == b.Role && a.Agent == b.Agent && a.Vendor == b.Vendor && a.Model == b.Model &&
		a.Reasoning == b.Reasoning && a.Note == b.Note && sameJSON(a.When, b.When)
}

func sameJSON(a, b json.RawMessage) bool {
	a, b = bytes.TrimSpace(a), bytes.TrimSpace(b)
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(x, y)
}

// needsIDs is true while a copied rule names a row that slicer has yet to add:
// the rule can be written only once its id is known.
func (p *copyPlan) needsIDs() bool {
	for _, r := range p.rules {
		if r.from == "" {
			continue
		}
		for _, token := range rowToken.FindAllString(r.text, -1) {
			if p.srcRows[token] && p.rowIDs[token] == "" {
				return true
			}
		}
	}
	return false
}

// pending is true until the copied rules' texts are final.
func (p *copyPlan) pending() bool { return !p.done && p.needsIDs() }

// newRows is the source ids of the rows that slicer adds, in the order given.
func (p *copyPlan) newRows() []string {
	var ids []string
	for _, r := range p.rows {
		if r.id == "" && r.from != "" {
			ids = append(ids, r.from)
		}
	}
	return ids
}

// rewrite gives each copied rule the target's row ids: added maps a source row
// id to the id that slicer gave the row it added. Only a whole token `rNN` that
// is a copied source row's id is rewritten, in one pass. A copied rule of the
// same text as a target rule keeps that rule's id.
func (p *copyPlan) rewrite(added map[string]string) error {
	ids := map[string]string{}
	for from, id := range p.rowIDs {
		ids[from] = id
	}
	for from, id := range added {
		ids[from] = id
	}
	var failed error
	used := map[string]bool{}
	for i := range p.rules {
		r := &p.rules[i]
		if r.from == "" {
			continue
		}
		r.text = rowToken.ReplaceAllStringFunc(r.text, func(token string) string {
			if !p.srcRows[token] {
				return token
			}
			id, ok := ids[token]
			if !ok {
				failed = fmt.Errorf("slicer gave no id for the row copied from %s", token)
			}
			return id
		})
		r.id = ""
		for _, t := range p.free {
			if !used[t.ID] && t.Text == r.text {
				used[t.ID] = true
				r.id = t.ID
				break
			}
		}
	}
	if failed != nil {
		return failed
	}
	p.done = true
	return nil
}

// set is the change set for slicer, without expect. Until rewrite runs, the
// copied rules are the source's text, to be added without ids.
func (p *copyPlan) set() copySet {
	out := copySet{Replace: copyReplace{Rows: []copyRowJSON{}, Rules: []copyRuleJSON{}}}
	for _, r := range p.rows {
		out.Replace.Rows = append(out.Replace.Rows, copyRowJSON{
			ID: r.id, Role: r.row.Role, When: r.row.When, Agent: r.row.Agent, Vendor: r.row.Vendor,
			Model: r.row.Model, Reasoning: r.row.Reasoning, Note: r.row.Note,
		})
	}
	for _, r := range p.rules {
		id := r.id
		if !p.done && r.from != "" {
			id = ""
		}
		out.Replace.Rules = append(out.Replace.Rules, copyRuleJSON{ID: id, Text: r.text})
	}
	return out
}

// copyApplied is what the copy reads of slicer's apply result.
type copyApplied struct {
	Changed        bool             `json:"changed"`
	RevisionBefore string           `json:"revision_before"`
	Steps          []MatrixCopyStep `json:"steps"`
}

// copyApply sends set to `slicer relay-roles apply` for the project at root. A
// slicer without the command is slicer_too_old, which no project can fix.
func copyApply(ctx context.Context, slicer slicercli.Client, root string, set copySet, dry bool) (copyApplied, error) {
	body, err := json.Marshal(set)
	if err != nil {
		return copyApplied{}, err
	}
	raw, err := slicer.RelayRolesApply(ctx, runrec.Writer{}, root, body, dry)
	if err != nil {
		var exit *clirun.ExitError
		if errors.As(err, &exit) && exit.Tool == nil && exit.Result.ExitCode == 2 &&
			strings.Contains(string(exit.Result.Stderr), "invalid choice: 'apply'") {
			return copyApplied{}, &ipc.Error{Code: "slicer_too_old", Message: "slicer relay-roles apply is not available in this slicer; upgrade slicer (ork doctor --upgrade)"}
		}
		return copyApplied{}, err
	}
	var out copyApplied
	if err := json.Unmarshal(raw, &out); err != nil {
		return copyApplied{}, err
	}
	return out, nil
}

// copyJob is a target whose check passed and that a write will change.
type copyJob struct {
	project registry.Project
	set     copySet
	out     *MatrixCopyProject
	current *slicercli.Matrix
}

// matrixCopy copies the matrix of p.From to each target through slicer's atomic
// `relay-roles apply` replace (S261, *proposed*, owner decisions
// matrix.copy-source, matrix.copy-scope, matrix.copy-keep, matrix.copy-confirm,
// matrix.copy-all). Every target is checked with a dry run first; a failed
// check anywhere, or a dry run, writes nothing. A write passes the dry run's
// revision as expect and goes on after a refusal, so one project's failure
// leaves the others copied. The writes are left uncommitted, with no run
// record. An unreadable source, a bad request or an old slicer is the call's
// error; every other refusal is its project's.
func (d *Daemon) matrixCopy(ctx context.Context, p MatrixCopyParams) (*MatrixCopyResult, error) {
	if p.From == "" {
		return nil, badParams(errors.New("matrix.copy needs from"))
	}
	if p.All == (len(p.To) > 0) {
		return nil, badParams(errors.New("matrix.copy needs exactly one of to and all"))
	}
	from, err := d.registered(p.From)
	if err != nil {
		return nil, err
	}
	var targets []registry.Project
	if p.All {
		f, err := d.loadRegistry()
		if err != nil {
			return nil, err
		}
		for _, project := range f.Projects {
			if project.Name != from.Name {
				targets = append(targets, project)
			}
		}
	} else {
		for _, name := range p.To {
			if name == from.Name {
				return nil, badParams(fmt.Errorf("matrix.copy cannot copy %s to itself", name))
			}
			project, err := d.registered(name)
			if err != nil {
				return nil, err
			}
			if !slices.ContainsFunc(targets, func(t registry.Project) bool { return t.Name == name }) {
				targets = append(targets, project)
			}
		}
	}

	slicer := d.runDeps(from).Slicer
	source, err := slicer.RelayMatrix(ctx, from.Path)
	if err != nil {
		return nil, err
	}
	result := &MatrixCopyResult{From: from.Name, Projects: make([]MatrixCopyProject, len(targets))}
	var jobs []copyJob
	failed, refused := false, false
	for i, target := range targets {
		out := &result.Projects[i]
		out.Project = target.Name
		var bind copyBinding
		if !p.DryRun {
			bind.steps, bind.stepsBound = p.ExpectSteps[target.Name]
			bind.digest, bind.digestBound = p.ExpectRows[target.Name]
		}
		job, err := d.checkCopy(ctx, slicer, from, source, target, out, bind)
		if err != nil {
			var wire *ipc.Error
			if errors.As(err, &wire) && wire.Code == "slicer_too_old" {
				return nil, err
			}
			out.Status, out.Error = CopyFailed, ipc.ErrorOf(err)
			if wire != nil && wire.Code == CodeStalePreview {
				// Refused alone: the other projects still go on.
				refused = true
			} else {
				failed = true
			}
			continue
		}
		if job != nil {
			jobs = append(jobs, *job)
		}
	}
	if p.DryRun || failed {
		return result, nil
	}

	changed := false
	for _, job := range jobs {
		applied, err := copyApply(ctx, d.runDeps(job.project).Slicer, job.project.Path, job.set, false)
		if err != nil {
			*job.out = MatrixCopyProject{Project: job.project.Name, Status: CopyFailed, Error: ipc.ErrorOf(err)}
			failed = true
			continue
		}
		job.out.Status = CopyChanged
		if applied.Steps != nil {
			setStepFields(applied.Steps, job.set, job.current)
			job.out.Steps = applied.Steps
		}
		changed = true
	}
	result.Partial = changed && (failed || refused)
	return result, nil
}

// addedIDs is the ids of the rows that slicer's steps add, in order.
func addedIDs(steps []MatrixCopyStep) []string {
	var ids []string
	for _, step := range steps {
		if step.Op == "add" {
			ids = append(ids, step.ID)
		}
	}
	return ids
}

// copyBinding is what the owner confirmed for one target: the steps (S548) and
// the digest of the planned rows and rules (S558) its preview showed. Each part binds only
// when its Bound flag is set; the zero value binds nothing.
type copyBinding struct {
	steps       []MatrixCopyStep
	stepsBound  bool
	digest      string
	digestBound bool
}

// rowsDigest is the SHA-256 of the rows and rules the copy plans for a target, as
// "sha256:" and hex. It covers each row's id and every field the copy writes, so
// an edit to a source row changes it even when the steps stay the same. A row's
// conditions are hashed as slicer's JSON values, whatever their spacing.
// Rules include their target ids and final rewritten text.
func rowsDigest(rows []plannedRow, rules []plannedRule) string {
	h := sha256.New()
	enc := json.NewEncoder(h)
	for _, r := range rows {
		when := bytes.TrimSpace(r.row.When)
		var v any
		if json.Unmarshal(when, &v) == nil {
			if canon, err := json.Marshal(v); err == nil {
				when = canon
			}
		}
		_ = enc.Encode(copyRowJSON{
			ID: r.id, Role: r.row.Role, When: when, Agent: r.row.Agent, Vendor: r.row.Vendor,
			Model: r.row.Model, Reasoning: r.row.Reasoning, Note: r.row.Note,
		})
	}
	for _, r := range rules {
		_ = enc.Encode(copyRuleJSON{ID: r.id, Text: r.text})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// setStepFields attaches only the row values changed by a set step.
func setStepFields(steps []MatrixCopyStep, set copySet, current *slicercli.Matrix) {
	for i := range steps {
		step := &steps[i]
		if step.Op != "set" || !step.Changed {
			continue
		}
		for _, row := range set.Replace.Rows {
			if row.ID != step.ID {
				continue
			}
			for _, before := range current.Rows {
				if before.ID != row.ID {
					continue
				}
				step.Fields = nil
				add := func(key, value, old string) {
					if value != old {
						if step.Fields == nil {
							step.Fields = map[string]string{}
						}
						step.Fields[key] = value
					}
				}
				add("role", row.Role, before.Role)
				if !sameJSON(row.When, before.When) {
					var compact bytes.Buffer
					when := string(row.When)
					if json.Compact(&compact, row.When) == nil {
						when = compact.String()
					}
					add("when", when, string(before.When))
				}
				add("agent", row.Agent, before.Agent)
				add("vendor", row.Vendor, before.Vendor)
				add("model", row.Model, before.Model)
				add("reasoning", row.Reasoning, before.Reasoning)
				add("note", row.Note, before.Note)
			}
		}
	}
}

// checkCopy reads target's matrix, plans the copy and dry-runs it, filling out.
// It returns the job to write when the matrix would change. A refusal comes back
// as the error, for the caller to file under the project. When bind binds the
// steps, the dry run's steps must be the ones the owner confirmed; when it binds
// the digest, the planned rows' digest must be the confirmed one; else the error
// is CodeStalePreview (S548, S558).
func (d *Daemon) checkCopy(ctx context.Context, slicer slicercli.Client, from registry.Project, source *slicercli.Matrix, target registry.Project, out *MatrixCopyProject, bind copyBinding) (*copyJob, error) {
	matrix, err := slicer.RelayMatrix(ctx, target.Path)
	if err != nil {
		var tool *clirun.ToolError
		if errors.As(err, &tool) && tool.Code == "no_relay_roles" {
			out.Status = CopySkipped
			out.Error = &ipc.Error{Code: tool.Code, Message: fmt.Sprintf("no relay roles matrix; run ork projects setup %s relay-roles-init", target.Name)}
			return nil, nil
		}
		return nil, err
	}
	plan, err := planMatrixCopy(from, source, target, matrix)
	if err != nil {
		return nil, err
	}
	targetSlicer := d.runDeps(target).Slicer
	var adds []string // the ids the first dry run gave the added rows
	learned := plan.pending()
	if learned {
		// The first dry run, with the source's rule text, tells which ids slicer
		// gives the rows it adds.
		first, err := copyApply(ctx, targetSlicer, target.Path, plan.set(), true)
		if err != nil {
			return nil, err
		}
		adds = addedIDs(first.Steps)
		newRows := plan.newRows()
		if len(adds) != len(newRows) {
			return nil, fmt.Errorf("slicer added %d rows to %s for %d new rows", len(adds), target.Name, len(newRows))
		}
		added := map[string]string{}
		for i, from := range newRows {
			added[from] = adds[i]
		}
		if err := plan.rewrite(added); err != nil {
			return nil, err
		}
	}
	set := plan.set()
	dry, err := copyApply(ctx, targetSlicer, target.Path, set, true)
	if err != nil {
		return nil, err
	}
	if learned {
		// The rewritten rules name the ids of the first dry run; the write must give
		// the added rows those ids.
		if second := addedIDs(dry.Steps); !slices.Equal(adds, second) {
			return nil, &ipc.Error{Code: CodeAddIDMismatch, Message: fmt.Sprintf("slicer gave added rows ids %s in the first dry run and %s in the second; nothing was written to this project: preview again, then copy", strings.Join(adds, ", "), strings.Join(second, ", "))}
		}
	}
	setStepFields(dry.Steps, set, matrix)
	if bind.stepsBound && !slices.EqualFunc(dry.Steps, bind.steps, func(a, b MatrixCopyStep) bool {
		return a.Op == b.Op && a.ID == b.ID && a.Changed == b.Changed && maps.Equal(a.Fields, b.Fields)
	}) {
		return nil, &ipc.Error{Code: CodeStalePreview, Message: fmt.Sprintf(
			"the copy to %s would now make other steps than the preview showed: preview again, then copy", target.Name)}
	}
	digest := rowsDigest(plan.rows, plan.rules)
	if bind.digestBound && digest != bind.digest {
		return nil, &ipc.Error{Code: CodeStalePreview, Message: fmt.Sprintf(
			"the rows or rules the copy to %s would write differ from the ones the preview showed: preview again, then copy", target.Name)}
	}
	if !dry.Changed {
		out.Status = CopyUnchanged
		return nil, nil
	}
	out.Status, out.Steps, out.Warnings, out.Digest = CopyWouldChange, dry.Steps, plan.Warnings, digest
	set.Expect = dry.RevisionBefore
	return &copyJob{project: target, set: set, out: out, current: matrix}, nil
}
