// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// The spec check (adj 164, roles.spec-check; docs/design.md, Wave lifecycle).
// The clients ask for it before they start a wave or a single slice. It is a
// read-only job of the daemon, not a run: no run record, no worktree. The role works in
// the project checkout with the tree fingerprint of a read-only role.
const (
	MethodSpecCheck  = "spec.check"  // proposed wire; params SpecCheckParams, result SpecCheckResult
	MethodSpecAnswer = "spec.answer" // proposed wire; params SpecAnswerParams, result SpecAnswerResult
)

// CodeSpecQuestions is the error code when wave.start or run.start refuses
// an item because its spec check left questions open.
const CodeSpecQuestions = "spec_questions"

// SpecCheckParams names the project and the items to check in one pass.
type SpecCheckParams struct {
	Project string   `json:"project"`
	Items   []string `json:"items"`
}

// SpecItemResult is the check's answer for one item against its current
// spec: ready, or the questions that are still open.
//
// NoSpec says the item has no slice spec to check (S283, *proposed*): the row
// is not Ready, has no questions and no role ran for it. The caller keeps the
// item out of its wave and names the fix, slicer promote.
type SpecItemResult struct {
	Item      string                `json:"item"`
	Ready     bool                  `json:"ready"`
	NoSpec    bool                  `json:"no_spec,omitempty"`
	Questions []engine.SpecQuestion `json:"questions,omitempty"`
}

// SpecCheckResult has one entry for each requested item, in order.
type SpecCheckResult struct {
	Items       []SpecItemResult `json:"items"`
	OwnerNeeded string           `json:"owner_needed,omitempty"`
}

// SpecAnswerParams carries the owner's answers to one item's open questions,
// one for each question in the order the check gave them. Adj files the
// questions that have a decision key in adj, as By.
type SpecAnswerParams struct {
	Project string   `json:"project"`
	Item    string   `json:"item"`
	Answers []string `json:"answers"`
	Adj     bool     `json:"adj,omitempty"`
	By      string   `json:"by,omitempty"`
}

// SpecAnswerResult says the item is ready once its answers are in the spec.
// Notices say what went to adj, or why not.
type SpecAnswerResult struct {
	Item    string   `json:"item"`
	Ready   bool     `json:"ready"`
	Notices []string `json:"notices,omitempty"`
}

// specEntry is what the daemon remembers of a check: the verdict, and the
// fingerprint of the spec it was made against. A verdict counts only while
// the spec still has that fingerprint. Role verdicts live in memory; owner
// answers are also saved beside the check until run start or a spec change.
type specEntry struct {
	fingerprint string
	ready       bool
	questions   []engine.SpecQuestion
	ownerNeeded string
	decisions   string // the paragraph recorded for the answers; the item's run carries it
}

func specKey(project, item string) string { return project + "\x00" + item }

// specCheckRows is the rows the spec check runs on, in order (S258,
// *proposed*): the project's spec_check_rows when it sets any, else every row
// that a rule names as "Spec check row (rNN)", in rule order (owner decision
// 2026-11-07): the first runs and each next one is its fallback. Empty means
// neither names one.
func specCheckRows(proj registry.Project, rules []slicercli.Rule) []string {
	if len(proj.SpecCheckRows) > 0 {
		return proj.SpecCheckRows
	}
	return slicercli.SpecCheckRowIDs(rules)
}

// specCheckRowSet is every matrix row that is a spec check row: the rows of
// specCheckRows and every row a rule names. None of them belongs to a run's
// reviewer or gate chain, and roles.matrix labels each one.
func specCheckRowSet(proj registry.Project, rules []slicercli.Rule) []string {
	set := slices.Clone(specCheckRows(proj, rules))
	for _, id := range slicercli.SpecCheckRowIDs(rules) {
		if !slices.Contains(set, id) {
			set = append(set, id)
		}
	}
	return set
}

// chainWarning tells the owner what naming a row costs: a spec check row is
// never a reviewer or gate row.
const chainWarning = "a spec check row leaves every run's reviewer and gate chains."

// noSpecCheckRow is the owner-facing error of a spec check with no row to run
// on (owner decisions 2026-11-07). The message has one line each for the
// problem, the fix command with a real candidate row, the other way out, the
// reviewer rows with an adapter (candidates, possibly none) and the chain
// warning. The "or" line is the rule hint when the project sets no rows, and
// the way back to the rule when it does and a rule names a row.
func noSpecCheckRow(proj registry.Project, problem string, ruleNamesRow bool, candidates []string) error {
	// A set row that has an adapter would have made a chain, so the first
	// candidate is never one the project already lists.
	pick := "rNN"
	if len(candidates) > 0 {
		pick = candidates[0]
	}
	fix := "ork projects set " + proj.Name + " --spec-check-rows " + pick
	lines := []string{problem}
	switch n := len(proj.SpecCheckRows); {
	case n == 0:
		lines = append(lines, "fix: "+fix, `or:  slicer relay-roles rule-add ... "Spec check row (rNN)"`)
	case n == 1:
		lines = append(lines, "fix: change it with "+fix)
	default:
		lines = append(lines, "fix: change them with "+fix)
	}
	if len(proj.SpecCheckRows) > 0 && ruleNamesRow {
		lines = append(lines, `or:  ork projects set `+proj.Name+` --spec-check-rows ""`)
	}
	if len(candidates) > 0 {
		lines = append(lines, "reviewer rows with an adapter: "+strings.Join(candidates, ", "))
	}
	lines = append(lines, chainWarning)
	return &ipc.Error{Code: CodeNoSpecCheckRow, Message: strings.Join(lines, "\n")}
}

const noRowProblem = "no Spec check row is set: spec_check_rows is empty and no rule names one."

// specChain finds the lineup of the spec check: one entry for each of the
// project's spec check rows in order (see specCheckRows), taken from the
// reviewer rows slicer recommends for the item. A row without an adapter is
// left out, so the next listed row is the fallback. When no row is left, the
// error has code no_spec_check_row.
func (d *Daemon) specChain(ctx context.Context, deps engine.Deps, proj registry.Project, item string) ([]runrec.LineupEntry, error) {
	rules, err := deps.Slicer.RelayRules(ctx, proj.Path)
	if err != nil {
		return nil, err
	}
	wanted := specCheckRows(proj, rules)
	rec, err := deps.Slicer.RecommendRoles(ctx, proj.Path, item, false, false)
	if err != nil {
		if len(wanted) == 0 {
			return nil, noSpecCheckRow(proj, noRowProblem, false, nil)
		}
		return nil, err
	}
	// The reviewer rows with an adapter, in the order slicer gives them.
	var usable []slicercli.Row
	var usableIDs []string
	for _, choice := range rec.Roles {
		if choice.Role != engine.RoleReviewer {
			continue
		}
		rows := choice.Alternatives
		if choice.Row != nil {
			rows = append([]slicercli.Row{*choice.Row}, rows...)
		}
		for _, r := range rows {
			if matrixAdapters[strings.ToLower(r.Agent)] != "" {
				usable = append(usable, r)
				if !slices.Contains(usableIDs, r.ID) {
					usableIDs = append(usableIDs, r.ID)
				}
			}
		}
	}
	if len(wanted) == 0 {
		return nil, noSpecCheckRow(proj, noRowProblem, false, usableIDs)
	}
	var marked []chainEntry
	for _, id := range wanted {
		for _, r := range usable {
			if r.ID == id {
				entry := runrec.LineupEntry{Vendor: matrixAdapters[strings.ToLower(r.Agent)], Model: r.Model, Effort: r.Reasoning}
				marked = appendChain(marked, chainEntry{entry, isLocalRow(r)})
			}
		}
	}
	if len(marked) == 0 {
		noun, verb := "row (%s) is", "rows (%s) are"
		if len(wanted) > 1 {
			noun = verb
		}
		problem := fmt.Sprintf("the spec check "+noun+" not among the reviewer rows with an adapter that slicer recommends for %s.", strings.Join(wanted, ", "), item)
		return nil, noSpecCheckRow(proj, problem, len(slicercli.SpecCheckRowIDs(rules)) > 0, usableIDs)
	}
	chain, err := d.resolveEntries(ctx, marked)
	if err != nil {
		return nil, fmt.Errorf("spec check lineup: %w", err)
	}
	return chain, nil
}

// specCheck answers for each item from the check it remembers, and runs one
// read-only pass of the spec-check role over the items whose spec has no
// check yet or changed since it. Checks run one at a time, so a second
// caller finds the first one's answers.
func (d *Daemon) specCheck(ctx context.Context, p SpecCheckParams) (*SpecCheckResult, error) {
	if p.Project == "" || len(p.Items) == 0 {
		return nil, badParams(errors.New("spec check needs a project and items"))
	}
	seen := map[string]bool{}
	var items []string
	for _, item := range p.Items {
		if item == "" {
			return nil, badParams(errors.New("spec check item is empty"))
		}
		if !seen[item] {
			seen[item] = true
			items = append(items, item)
		}
	}
	d.runs.specMu.Lock()
	defer d.runs.specMu.Unlock()
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	if err := d.blockedItems(ctx, proj, items); err != nil {
		return nil, err
	}
	deps := d.runDeps(proj)
	var stale []string
	noSpec := map[string]bool{}
	for _, item := range items {
		raw, err := deps.Slicer.ShowRaw(ctx, proj.Path, item)
		if err != nil {
			return nil, err
		}
		fp, err := engine.SpecFingerprint(raw)
		if errors.Is(err, engine.ErrNoSpec) {
			// A row, not an error (adj 245): the client drops the item and keeps
			// the rest. No role runs for it and nothing is remembered.
			noSpec[item] = true
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", item, err)
		}
		if err := d.loadSpecAnswers(proj.Name, item, fp); err != nil {
			return nil, err
		}
		d.runs.mu.Lock()
		e, ok := d.runs.specs[specKey(proj.Name, item)]
		d.runs.mu.Unlock()
		if !ok || e.fingerprint != fp {
			stale = append(stale, item)
		}
	}
	if len(stale) > 0 {
		if err := d.runSpecCheck(ctx, deps, proj, stale); err != nil {
			return nil, err
		}
	}
	out := &SpecCheckResult{Items: []SpecItemResult{}}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	ownerTexts := map[string]bool{}
	for _, item := range items {
		if noSpec[item] {
			out.Items = append(out.Items, SpecItemResult{Item: item, NoSpec: true})
			continue
		}
		e := d.runs.specs[specKey(proj.Name, item)]
		out.Items = append(out.Items, SpecItemResult{Item: item, Ready: e.ready, Questions: e.questions})
		if e.ownerNeeded != "" && !ownerTexts[e.ownerNeeded] {
			if out.OwnerNeeded != "" {
				out.OwnerNeeded += "\n\n"
			}
			out.OwnerNeeded += e.ownerNeeded
			ownerTexts[e.ownerNeeded] = true
		}
	}
	return out, nil
}

// runSpecCheck runs the role over items and remembers each verdict. The job
// counts as active work, so the daemon does not stop or restart under it.
func (d *Daemon) runSpecCheck(ctx context.Context, deps engine.Deps, proj registry.Project, items []string) error {
	chain, err := d.specChain(ctx, deps, proj, items[0])
	if err != nil {
		return err
	}
	d.runs.mu.Lock()
	if d.runs.closed {
		d.runs.mu.Unlock()
		return errors.New("run manager is closed")
	}
	// The job runs under the manager's context too, so closing the manager (a
	// forced daemon stop) cancels it without waiting for the role.
	jobCtx, cancel := context.WithCancel(d.runs.ctx)
	d.runs.specSeq++
	id := d.runs.specSeq
	d.runs.specCancels[id] = cancel
	d.runs.specJobs++
	d.runs.wg.Add(1)
	d.runs.mu.Unlock()
	stop := context.AfterFunc(ctx, cancel)
	defer func() {
		stop()
		cancel()
		d.runs.mu.Lock()
		delete(d.runs.specCancels, id)
		d.runs.specJobs--
		d.runs.mu.Unlock()
		d.runs.wg.Done()
	}()
	ctx = jobCtx
	d.runs.specStateMu.Lock()
	if _, err := d.registered(proj.Name); err != nil {
		d.runs.specStateMu.Unlock()
		return err
	}
	for _, item := range items {
		if err := writeSpecJSON(filepath.Join(d.specDir(proj.Name, item), "check.json"), specCheckIdentity{Project: proj.Name, Item: item}); err != nil {
			d.runs.specStateMu.Unlock()
			return err
		}
	}
	d.runs.specStateMu.Unlock()
	dir := d.specDir(proj.Name, items[0])
	res, err := engine.RunSpecCheck(ctx, deps, engine.SpecCheckConfig{Project: proj, Items: items, Lineup: chain, Dir: dir})
	if err != nil {
		var retried *engine.SpecCheckRetryError
		if errors.As(err, &retried) {
			return &ipc.Error{Code: "spec_check_retry", Message: err.Error()}
		}
		return err
	}
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
	if _, err := d.registered(proj.Name); err != nil {
		return err
	}
	report, err := os.ReadFile(filepath.Join(dir, "spec-check.md"))
	if err != nil {
		return err
	}
	for _, item := range items[1:] {
		if err := os.WriteFile(filepath.Join(d.specDir(proj.Name, item), "spec-check.md"), report, 0600); err != nil {
			return err
		}
	}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	for _, v := range res.Verdicts {
		d.runs.specs[specKey(proj.Name, v.Item)] = specEntry{fingerprint: res.Specs[v.Item], ready: v.Ready, questions: v.Questions, ownerNeeded: res.OwnerNeeded}
	}
	return nil
}

// specAnswer writes the owner's answers into the item's spec and marks the
// item ready for the spec it now has. It refuses an item with no open
// questions, and a spec that changed since the check.
func (d *Daemon) specAnswer(ctx context.Context, p SpecAnswerParams) (*SpecAnswerResult, error) {
	if p.Project == "" || p.Item == "" {
		return nil, badParams(errors.New("spec answer needs a project and an item"))
	}
	d.runs.specMu.Lock()
	defer d.runs.specMu.Unlock()
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	d.runs.mu.Lock()
	e, ok := d.runs.specs[specKey(proj.Name, p.Item)]
	closed := d.runs.closed
	d.runs.mu.Unlock()
	if closed {
		return nil, errors.New("run manager is closed")
	}
	if !ok || e.ready || len(e.questions) == 0 {
		return nil, fmt.Errorf("%s has no open spec questions", p.Item)
	}
	deps := d.runDeps(proj)
	raw, err := deps.Slicer.ShowRaw(ctx, proj.Path, p.Item)
	if err != nil {
		return nil, err
	}
	if fp, err := engine.SpecFingerprint(raw); err != nil {
		return nil, err
	} else if fp != e.fingerprint {
		return nil, fmt.Errorf("spec changed since the check of %s; run the check again", p.Item)
	}
	// The owner is the caller's ADJ_DECIDED_BY, else the registry's owner; the
	// registry's file_answers turns filing off (S314, *proposed*).
	reg, err := d.loadRegistry()
	if err != nil {
		return nil, err
	}
	by := engine.ResolveOwner(p.By, reg.Owner)
	decisions, notices, err := engine.RecordSpecAnswers(ctx, deps, engine.SpecAnswerConfig{Project: proj, Item: p.Item, Questions: e.questions, Answers: p.Answers, Adj: p.Adj && proj.Adj && reg.FileAnswersOn(), By: by})
	if err != nil {
		return nil, err
	}
	if p.Adj && !proj.Adj {
		for _, q := range e.questions {
			if q.Key != "" {
				notices = append(notices, "no adj in this project; not filed in adj: "+q.Key)
			}
		}
	}
	raw, err = deps.Slicer.ShowRaw(ctx, proj.Path, p.Item)
	if err != nil {
		return nil, fmt.Errorf("answers recorded in %s, but its spec could not be read again: %w", p.Item, err)
	}
	fp, err := engine.SpecFingerprint(raw)
	if err != nil {
		return nil, err
	}
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
	if _, err := d.registered(proj.Name); err != nil {
		return nil, err
	}
	if err := writeSpecJSON(filepath.Join(d.specDir(proj.Name, p.Item), "answers.json"), storedSpecAnswers{Fingerprint: fp, Decisions: decisions, OwnerNeeded: e.ownerNeeded}); err != nil {
		return nil, fmt.Errorf("save owner answers %s: %w", filepath.Join(d.specDir(proj.Name, p.Item), "answers.json"), err)
	}
	d.runs.mu.Lock()
	d.runs.specs[specKey(proj.Name, p.Item)] = specEntry{fingerprint: fp, ready: true, decisions: decisions, ownerNeeded: e.ownerNeeded}
	d.runs.mu.Unlock()
	return &SpecAnswerResult{Item: p.Item, Ready: true, Notices: notices}, nil
}

// specGate refuses an item whose check, against its current spec, left
// questions open. An item that was never checked, or whose spec changed since,
// passes: the clients run the check before they start a wave or a single slice.
func (d *Daemon) specGate(ctx context.Context, project, item string) error {
	d.runs.mu.Lock()
	e, ok := d.runs.specs[specKey(project, item)]
	d.runs.mu.Unlock()
	if !ok || e.ready || len(e.questions) == 0 {
		return nil
	}
	proj, err := d.registered(project)
	if err != nil {
		return err
	}
	raw, err := d.runDeps(proj).Slicer.ShowRaw(ctx, proj.Path, item)
	if err != nil {
		return err
	}
	fp, err := engine.SpecFingerprint(raw)
	if err != nil || fp != e.fingerprint {
		return nil
	}
	noun := "questions"
	if len(e.questions) == 1 {
		noun = "question"
	}
	return &ipc.Error{Code: CodeSpecQuestions, Message: fmt.Sprintf("%s has %d open spec %s; answer them first", item, len(e.questions), noun)}
}
