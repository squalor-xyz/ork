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
	Project string                                     `json:"project"`
	Items   []string                                   `json:"items"`
	Lineups map[string]map[string][]runrec.LineupEntry `json:"lineups,omitempty"`
}

// SpecItemResult is the check's answer for one item against its current
// spec: ready, or the questions that are still open.
//
// NoSpec says the item has no slice spec to check (S283, *proposed*): the row
// is not Ready, has no questions and no role ran for it. The caller keeps the
// item out of its wave and names the fix, slicer promote.
type SpecItemResult struct {
	WaitsOn   []int                 `json:"waits_on,omitempty"`
	Notices   []string              `json:"notices,omitempty"`
	Item      string                `json:"item"`
	Ready     bool                  `json:"ready"`
	NoSpec    bool                  `json:"no_spec,omitempty"`
	Questions []engine.SpecQuestion `json:"questions,omitempty"`
	Carved    []string              `json:"carved,omitempty"` // the slivers this check filed under the item, in the order they run (S359, *proposed*)
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
	// Lineup is the owner's lineup overrides for the item, as in a spec check:
	// the answers can make a local implementer's item Ready, which carves it
	// (S359, *proposed*).
	Lineup map[string][]runrec.LineupEntry `json:"lineup,omitempty"`
}

// SpecAnswerResult says the item is ready once its answers are in the spec.
// Notices say what went to adj, or why not. Carved lists the slivers the
// answers' carve filed under the item (S359, *proposed*).
type SpecAnswerResult struct {
	Item    string   `json:"item"`
	Ready   bool     `json:"ready"`
	Notices []string `json:"notices,omitempty"`
	Carved  []string `json:"carved,omitempty"`
}

// specEntry is what the daemon remembers of a check: the verdict, and the
// fingerprint of the spec it was made against. A verdict counts only while
// the spec still has that fingerprint. Role verdicts live in memory; owner
// answers are also saved beside the check until run start or a spec change.
type specEntry struct {
	waiting     []engine.SpecQuestion
	waitsOn     []int
	notices     []string
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
	for _, id := range append(slicercli.SpecCheckRowIDs(rules), slicercli.LocalSpecCheckRowIDs(rules)...) {
		if !slices.Contains(set, id) {
			set = append(set, id)
		}
	}
	return set
}

// uiPassRows selects the ordered UI lineup; empty uses the rule defaults.
func uiPassRows(proj registry.Project, rules []slicercli.Rule) []string {
	if len(proj.UIPassRows) > 0 {
		return proj.UIPassRows
	}
	return slicercli.UIPassRowIDs(rules)
}

// uiPassRowSet includes trial rows even when the project does not select them.
func uiPassRowSet(proj registry.Project, rules []slicercli.Rule) []string {
	set := slices.Clone(uiPassRows(proj, rules))
	for _, id := range append(slicercli.UIPassRowIDs(rules), slicercli.LocalUIPassRowIDs(rules)...) {
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
// error has code no_spec_check_row. A row whose vendor is out is left out too
// (S379, *proposed*); when that leaves no row, the error has code refused and
// names the vendors and when each one ends.
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
	var outVendors []VendorOut // the vendors that left a row out, in row order (S379)
	for _, id := range wanted {
		for _, r := range usable {
			if r.ID == id {
				entry := runrec.LineupEntry{Vendor: matrixAdapters[strings.ToLower(r.Agent)], Model: r.Model, Effort: r.Reasoning}
				// A row on a vendor that is out is skipped, as run admission skips it.
				if out, isOut := deps.Vendors.Lookup(entry.Vendor); isOut {
					if !slices.ContainsFunc(outVendors, func(v VendorOut) bool { return v.Vendor == out.Vendor }) {
						outVendors = append(outVendors, VendorOut{Vendor: out.Vendor, Reason: out.Reason, Since: out.Since.Local(), Until: out.Until.Local()})
					}
					continue
				}
				marked = appendChain(marked, chainEntry{entry, isLocalRow(r)})
			}
		}
	}
	if len(marked) == 0 && len(outVendors) > 0 {
		lines := []string{"every spec check row is on a vendor that is out, so the check cannot run:"}
		for _, v := range outVendors {
			lines = append(lines, v.SentenceAt(d.cfg.Clock.Now()))
		}
		lines = append(lines, "end one with: ork vendor back VENDOR")
		return nil, &ipc.Error{Code: CodeRefused, Message: strings.Join(lines, "\n")}
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
	var stale, localItems []string
	noSpec := map[string]bool{}
	// A local implementer on a slice above size S takes slivers, which this
	// check carves once the item is Ready (S359, *proposed*; adj 396). The
	// saved carve is what an earlier filing that failed part way left to finish.
	carve := map[string][]engine.CarvedSliver{}
	var carveItems []string
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
		// A cached cloud check has no pack for a newly selected local
		// implementer. Plan with the same overrides as run admission.
		plan, planErr := d.planRun(ctx, RunParams{Project: proj.Name, Item: item, Lineup: p.Lineups[item]})
		local := planErr == nil && engine.IsLocalRun(plan.Lineup)
		if planErr == nil && plan.Carve {
			carveItems = append(carveItems, item)
			carve[item] = plan.carved
		}
		missingPack := false
		if local {
			_, err := os.Stat(filepath.Join(d.specDir(proj.Name, item), "orientation.md"))
			missingPack = errors.Is(err, os.ErrNotExist)
			if err != nil && !missingPack {
				return nil, fmt.Errorf("stat orientation %s: %w", item, err)
			}
		}
		if !ok || e.fingerprint != fp || missingPack {
			stale = append(stale, item)
			if local {
				localItems = append(localItems, item)
			}
		}
	}
	if len(stale) > 0 {
		if err := d.runSpecCheck(ctx, deps, proj, stale, localItems); err != nil {
			return nil, err
		}
	}
	for _, item := range items {
		if noSpec[item] {
			continue
		}
		if err := d.reuseSpecAnswers(ctx, deps, proj, item); err != nil {
			return nil, err
		}
	}
	carved := map[string][]string{}
	for _, item := range carveItems {
		d.runs.mu.Lock()
		ready := d.runs.specs[specKey(proj.Name, item)].ready
		d.runs.mu.Unlock()
		if !ready {
			continue
		}
		ids, err := d.carveSlivers(ctx, deps, proj, item, carve[item])
		if err != nil {
			return nil, err
		}
		carved[item] = ids
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
		out.Items = append(out.Items, SpecItemResult{Item: item, Ready: e.ready, Questions: e.questions, WaitsOn: e.waitsOn, Notices: e.notices, Carved: carved[item]})
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

// reuseSpecAnswers runs while specMu serializes checks and owner answers.
// Open adj records are read again on the next check without rerunning a role.
func (d *Daemon) reuseSpecAnswers(ctx context.Context, deps engine.Deps, proj registry.Project, item string) error {
	d.runs.mu.Lock()
	e := d.runs.specs[specKey(proj.Name, item)]
	d.runs.mu.Unlock()
	if e.ready || len(e.questions)+len(e.waiting) == 0 {
		return nil
	}
	questions := append(slices.Clone(e.questions), e.waiting...)
	reuse, err := engine.ReuseSpecAnswers(ctx, deps, proj, item, questions)
	if err != nil {
		return err
	}
	e.questions, e.waiting, e.waitsOn, e.notices = reuse.Questions, reuse.Waiting, reuse.WaitsOn, reuse.Notices
	e.ready = len(e.questions)+len(e.waiting) == 0
	if reuse.Decisions != "" {
		if e.decisions != "" {
			e.decisions += "\n\n"
		}
		e.decisions += reuse.Decisions
		raw, err := deps.Slicer.ShowRaw(ctx, proj.Path, item)
		if err != nil {
			return err
		}
		e.fingerprint, err = engine.SpecFingerprint(raw)
		if err != nil {
			return err
		}
	}
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
	if _, err := d.registered(proj.Name); err != nil {
		return err
	}
	if e.ready {
		err := writeSpecJSON(filepath.Join(d.specDir(proj.Name, item), "answers.json"), storedSpecAnswers{Fingerprint: e.fingerprint, Decisions: e.decisions, OwnerNeeded: e.ownerNeeded})
		if err != nil {
			return err
		}
	}
	d.runs.mu.Lock()
	d.runs.specs[specKey(proj.Name, item)] = e
	d.runs.mu.Unlock()
	return nil
}

// runSpecCheck runs the role over items and remembers each verdict. The job
// counts as active work, so the daemon does not stop or restart under it.
func (d *Daemon) runSpecCheck(ctx context.Context, deps engine.Deps, proj registry.Project, items, localItems []string) error {
	chain, err := d.specChain(ctx, deps, proj, items[0])
	if err != nil {
		return err
	}
	ctx, end, err := d.beginSpecJob(ctx)
	if err != nil {
		return err
	}
	defer end()
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
		if err := os.Remove(filepath.Join(d.specDir(proj.Name, item), "orientation.md")); err != nil && !errors.Is(err, os.ErrNotExist) {
			d.runs.specStateMu.Unlock()
			return err
		}
	}
	d.runs.specStateMu.Unlock()
	dir := d.specDir(proj.Name, items[0])
	res, err := engine.RunSpecCheck(ctx, deps, engine.SpecCheckConfig{Project: proj, Items: items, LocalItems: localItems, Lineup: chain, Dir: dir})
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
	for item, pack := range res.Orientations {
		if err := os.WriteFile(filepath.Join(d.specDir(proj.Name, item), "orientation.md"), []byte(pack), 0600); err != nil {
			return fmt.Errorf("save orientation %s: %w", item, err)
		}
	}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	for _, v := range res.Verdicts {
		d.runs.specs[specKey(proj.Name, v.Item)] = specEntry{fingerprint: res.Specs[v.Item], ready: v.Ready, questions: v.Questions, ownerNeeded: res.OwnerNeeded}
	}
	return nil
}

// beginSpecJob counts a read-only role job as active work, so the daemon does
// not stop or restart under it. The job runs under the manager's context too,
// so closing the manager (a forced daemon stop) cancels it without waiting for
// the role. The caller runs the returned func when the job ends.
func (d *Daemon) beginSpecJob(ctx context.Context) (context.Context, func(), error) {
	d.runs.mu.Lock()
	if d.runs.closed {
		d.runs.mu.Unlock()
		return nil, nil, errors.New("run manager is closed")
	}
	jobCtx, cancel := context.WithCancel(d.runs.ctx)
	d.runs.specSeq++
	id := d.runs.specSeq
	d.runs.specCancels[id] = cancel
	d.runs.specJobs++
	d.runs.wg.Add(1)
	d.runs.mu.Unlock()
	stop := context.AfterFunc(ctx, cancel)
	return jobCtx, func() {
		stop()
		cancel()
		d.runs.mu.Lock()
		delete(d.runs.specCancels, id)
		d.runs.specJobs--
		d.runs.mu.Unlock()
		d.rebuildKick()
		d.runs.wg.Done()
	}, nil
}

// carveSlivers has the spec check row carve item into slivers and files each
// under it, with no owner confirmation (S359, *proposed*; adj 337, adj 396). It
// runs inside the spec check, once the item is Ready, and returns the filed
// ids, in the order the slivers run. The role is read-only and a carve that
// fails files nothing. A filing that fails part way leaves the slivers filed
// so far under item and the report saved in the item's spec directory: the
// next spec check sees that the report has more slivers than are filed
// (localSliverGate) and passes it here as saved, so the rest is filed from it
// with no new carve (slicer's add key returns the slivers already filed). The
// caller holds specMu through the filing: slicer's writes change the project
// checkout, which a spec check's read-only fingerprint watches.
func (d *Daemon) carveSlivers(ctx context.Context, deps engine.Deps, proj registry.Project, item string, saved []engine.CarvedSliver) ([]string, error) {
	var chain []runrec.LineupEntry
	var err error
	if len(saved) == 0 {
		if chain, err = d.specChain(ctx, deps, proj, item); err != nil {
			return nil, err
		}
	}
	ctx, end, err := d.beginSpecJob(ctx)
	if err != nil {
		return nil, err
	}
	defer end()
	dir := d.specDir(proj.Name, item)
	slivers := saved
	if len(slivers) == 0 {
		slivers, err = engine.RunCarve(ctx, deps, engine.CarveConfig{Project: proj, Item: item, Lineup: chain, Dir: dir})
		if err != nil {
			return nil, fmt.Errorf("carve %s: %w. The carve changes nothing; run it again", item, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o700); err != nil {
		return nil, fmt.Errorf("carve %s: temp directory: %w", item, err)
	}
	var ids []string
	for i, sl := range slivers {
		id, err := engine.FileSliver(ctx, deps, proj, item, i+1, sl, filepath.Join(dir, "tmp"))
		if err != nil {
			filed := "none"
			if len(ids) > 0 {
				filed = strings.Join(ids, ", ")
			}
			return nil, fmt.Errorf("carve %s: sliver %d of %d (%s) was not filed: %w. Filed so far: %s; they stay under %s. Run %s again to file the rest from the saved carve report", item, i+1, len(slivers), sl.Title, err, filed, item, item)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// savedCarve is the carve report that RunCarve saved in item's spec directory,
// or nil when there is none or it does not parse.
func (d *Daemon) savedCarve(project, item string) []engine.CarvedSliver {
	text, err := os.ReadFile(filepath.Join(d.specDir(project, item), "carve.md"))
	if err != nil {
		return nil
	}
	slivers, err := engine.ParseCarve(string(text))
	if err != nil {
		return nil
	}
	return slivers
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
	if len(e.waitsOn) > 0 {
		return nil, fmt.Errorf("%s %s", p.Item, specWaitsOn(e.waitsOn))
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
	if e.decisions != "" {
		decisions = e.decisions + "\n\n" + decisions
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
	// The answers made the item Ready: a local implementer's slivers are carved
	// now, as a check that finds the item Ready would (S359, *proposed*).
	var ids []string
	if plan, err := d.planRun(ctx, RunParams{Project: proj.Name, Item: p.Item, Lineup: p.Lineup}); err == nil && plan.Carve {
		if ids, err = d.carveSlivers(ctx, deps, proj, p.Item, plan.carved); err != nil {
			return nil, fmt.Errorf("answers recorded in %s: %w", p.Item, err)
		}
	}
	return &SpecAnswerResult{Item: p.Item, Ready: true, Notices: notices, Carved: ids}, nil
}

// specGate refuses an item whose check, against its current spec, left
// questions open. An item that was never checked, or whose spec changed since,
// passes: the clients run the check before they start a wave or a single slice.
func (d *Daemon) specGate(ctx context.Context, project, item string) error {
	d.runs.mu.Lock()
	e, ok := d.runs.specs[specKey(project, item)]
	d.runs.mu.Unlock()
	if !ok || e.ready || len(e.questions)+len(e.waiting) == 0 {
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
	if len(e.waitsOn) > 0 {
		return &ipc.Error{Code: CodeSpecQuestions, Message: item + " " + specWaitsOn(e.waitsOn)}
	}
	noun := "questions"
	if len(e.questions) == 1 {
		noun = "question"
	}
	return &ipc.Error{Code: CodeSpecQuestions, Message: fmt.Sprintf("%s has %d open spec %s; answer them first", item, len(e.questions), noun)}
}

func specWaitsOn(ids []int) string {
	var names []string
	for _, id := range ids {
		names = append(names, fmt.Sprintf("adj %d", id))
	}
	return "waits on " + strings.Join(names, ", ")
}
