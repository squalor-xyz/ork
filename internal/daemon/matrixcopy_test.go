// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// ---- the planner (pure) ----

const (
	copySourceRule = "A reviewer never wrote code in the slice it reviews."
	noAdapterOne   = `agent "Acme Agent" has no ork adapter: a plan skips this row`
)

// planCopy is planMatrixCopy for a source project "src" and a target project of
// the given name, neither with settings.
func planCopy(project string, source, target *slicercli.Matrix) (copyPlan, error) {
	return planMatrixCopy(registry.Project{Name: "src"}, source, registry.Project{Name: project}, target)
}

func ruleErr(t *testing.T, err error, code string) *ipc.Error {
	t.Helper()
	e := ipc.ErrorOf(err)
	if err == nil || e.Code != code {
		t.Fatalf("error %v, want code %s", err, code)
	}
	return e
}

func TestMatrixCopyPlanTakesTheSourceRowsAndRulesWithoutIDs(t *testing.T) {
	when := json.RawMessage(`{"size":["M"],"effort":[2]}`)
	source := &slicercli.Matrix{
		Rows: []slicercli.Row{
			{ID: "r1", Role: "implementer", When: when, Agent: "Codex", Vendor: "OpenAI", Model: "gpt-x", Reasoning: "high", Note: "first"},
			{ID: "r2", Role: "reviewer", Agent: "Claude Code", Vendor: "Claude", Model: "opus", Reasoning: "medium"},
			{ID: "r3", Role: "reviewer", Agent: "Claude Code", Vendor: "Claude", Model: "sonnet", Reasoning: "low", Note: "source spec check"},
		},
		Rules: []slicercli.Rule{
			{ID: "u1", Text: copySourceRule},
			{ID: "u2", Text: "The spec check before a wave runs the Spec check row (r3)."},
		},
	}
	targetWhen := json.RawMessage(`{"size":["S"]}`)
	target := &slicercli.Matrix{
		Rows: []slicercli.Row{
			{ID: "r1", Role: "implementer", Agent: "Claude Code", Vendor: "Claude", Model: "haiku", Reasoning: "xhigh"},
			{ID: "r2", Role: "reviewer", Agent: "Claude Code", Vendor: "Claude", Model: "haiku", Reasoning: "xhigh"},
			{ID: "r9", Role: "reviewer", When: targetWhen, Agent: "Codex", Vendor: "OpenAI", Model: "gpt-t", Reasoning: "high", Note: "target spec check"},
		},
		Rules: []slicercli.Rule{
			{ID: "u1", Text: "Old rule one."},
			{ID: "u6", Text: "The spec check runs the Spec check row (r9) here."},
		},
	}
	plan, err := planCopy("adjudicate", source, target)
	if err != nil {
		t.Fatal(err)
	}
	want := copyReplace{
		Rows: []copyRowJSON{
			{Role: "implementer", When: when, Agent: "Codex", Vendor: "OpenAI", Model: "gpt-x", Reasoning: "high", Note: "first"},
			{Role: "reviewer", Agent: "Claude Code", Vendor: "Claude", Model: "opus", Reasoning: "medium"},
			{ID: "r9", Role: "reviewer", When: targetWhen, Agent: "Codex", Vendor: "OpenAI", Model: "gpt-t", Reasoning: "high", Note: "target spec check"},
		},
		Rules: []copyRuleJSON{
			{ID: "u6", Text: "The spec check runs the Spec check row (r9) here."},
			{Text: copySourceRule},
		},
	}
	if got := plan.set().Replace; !reflect.DeepEqual(got, want) {
		t.Fatalf("replace\n got %+v\nwant %+v", got, want)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("warnings %v", plan.Warnings)
	}
}

func TestMatrixCopyPlanKeepsTheTargetsUIPassRowAndRule(t *testing.T) {
	source := &slicercli.Matrix{
		Rows: []slicercli.Row{
			cRow("r1", "implementer", "Codex", "gpt-x"),
			cRow("r4", "reviewer", "Claude Code", "source-ui"),
			cRow("r3", "reviewer", "Claude Code", "source-spec"),
			cRow("r5", "gate", "Claude Code", "opus"),
		},
		Rules: []slicercli.Rule{
			{ID: "u5", Text: "A slice that touches the UI runs the UI pass row (r4)."},
			{ID: "u2", Text: "The spec check runs the Spec check row (r3)."},
			{ID: "u1", Text: copySourceRule},
		},
	}
	target := &slicercli.Matrix{
		Rows: []slicercli.Row{
			cRow("r1", "implementer", "Claude Code", "old"),
			cRow("r12", "reviewer", "Codex", "target-ui"),
			cRow("r9", "reviewer", "Codex", "target-spec"),
		},
		Rules: []slicercli.Rule{
			{ID: "u7", Text: "The UI pass row (r12) reads internal/tui."},
			{ID: "u6", Text: "The spec check runs the Spec check row (r9) here."},
		},
	}
	plan, err := planCopy("p", source, target)
	if err != nil {
		t.Fatal(err)
	}
	set := plan.set().Replace
	var ids, models []string
	for _, r := range set.Rows {
		ids = append(ids, r.ID)
		models = append(models, r.Model)
	}
	// The source's UI row sits at r4's place and the spec row at r3's: the
	// target's own rows, by id. Neither source row is copied.
	if want := []string{"", "r12", "r9", ""}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("row ids %v, want %v", ids, want)
	}
	if want := []string{"gpt-x", "target-ui", "target-spec", "opus"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("row models %v, want %v", models, want)
	}
	wantRules := []copyRuleJSON{
		{ID: "u7", Text: "The UI pass row (r12) reads internal/tui."},
		{ID: "u6", Text: "The spec check runs the Spec check row (r9) here."},
		{Text: copySourceRule},
	}
	if !reflect.DeepEqual(set.Rules, wantRules) {
		t.Fatalf("rules\n got %+v\nwant %+v", set.Rules, wantRules)
	}
}

func TestMatrixCopyPlanTargetWithoutASpecCheckRowIsRefused(t *testing.T) {
	source := &slicercli.Matrix{
		Rows:  []slicercli.Row{cRow("r3", "reviewer", "Claude Code", "x")},
		Rules: []slicercli.Rule{{ID: "u2", Text: "The spec check runs the Spec check row (r3)."}},
	}
	target := &slicercli.Matrix{
		Rows:  []slicercli.Row{cRow("r1", "reviewer", "Claude Code", "x")},
		Rules: []slicercli.Rule{{ID: "u1", Text: "No row is named here."}},
	}
	plan, err := planCopy("pitfight", source, target)
	e := ruleErr(t, err, CodeNoSpecCheckRow)
	if strings.Contains(e.Message, "pitfight") || !strings.Contains(e.Message, "slicer relay-roles rule-add") {
		t.Fatalf("message %q should omit the redundant project name and retain the fix", e.Message)
	}
	if len(plan.rows) != 0 || len(plan.rules) != 0 {
		t.Fatalf("a refused plan holds rows or rules: %+v", plan)
	}
}

// The target keeps every row its spec check and UI pass read, not only the ones
// its rules name (owner decision matrix.copy-settings-rows, 2026-11-24): the rows
// of its spec_check_rows and ui_pass_rows settings, and its Local trial rows,
// and the rules that name them.
func TestMatrixCopyPlanKeepsTheTargetsSettingsRowsAndLocalTrialRows(t *testing.T) {
	source := &slicercli.Matrix{
		Rows: []slicercli.Row{
			cRow("r1", "implementer", "Codex", "gpt-x"),
			cRow("r4", "reviewer", "Claude Code", "source-ui"),
			cRow("r3", "reviewer", "Claude Code", "source-spec"),
			cRow("r5", "gate", "Claude Code", "opus"),
		},
		Rules: []slicercli.Rule{
			{ID: "u5", Text: "A slice that touches the UI runs the UI pass row (r4)."},
			{ID: "u2", Text: "The spec check runs the Spec check row (r3)."},
			{ID: "u1", Text: copySourceRule},
		},
	}
	target := &slicercli.Matrix{
		Rows: []slicercli.Row{
			cRow("r1", "implementer", "Claude Code", "old"),
			cRow("r9", "reviewer", "Codex", "target-spec"),
			cRow("r14", "reviewer", "Codex", "target-ui"),
			cRow("r35", "reviewer", "Codex", "local-spec"),
			cRow("r36", "reviewer", "Codex", "local-ui"),
			cRow("r40", "reviewer", "Codex", "settings-spec"),
		},
		Rules: []slicercli.Rule{
			{ID: "u6", Text: "The spec check runs the Spec check row (r9) here."},
			{ID: "u7", Text: "The UI pass row (r14) reads internal/tui."},
			{ID: "u12", Text: "Local spec check row (r35) and Local UI pass row (r36) are trial rows."},
		},
	}
	project := registry.Project{Name: "p", SpecCheckRows: []string{"r40"}, UIPassRows: []string{"r36", "r14"}}
	plan, err := planMatrixCopy(registry.Project{Name: "src"}, source, project, target)
	if err != nil {
		t.Fatal(err)
	}
	set := plan.set().Replace
	var ids []string
	for _, r := range set.Rows {
		ids = append(ids, r.ID)
	}
	// r4's slot gets the first UI row (r36), r3's the first spec row (r40); the
	// rest follow the copied rows, spec rows first.
	if want := []string{"", "r36", "r40", "", "r9", "r35", "r14"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("row ids %v, want %v", ids, want)
	}
	wantRules := []copyRuleJSON{
		{ID: "u6", Text: "The spec check runs the Spec check row (r9) here."},
		{ID: "u7", Text: "The UI pass row (r14) reads internal/tui."},
		{ID: "u12", Text: "Local spec check row (r35) and Local UI pass row (r36) are trial rows."},
		{Text: copySourceRule},
	}
	if !reflect.DeepEqual(set.Rules, wantRules) {
		t.Fatalf("rules\n got %+v\nwant %+v", set.Rules, wantRules)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("warnings %v", plan.Warnings)
	}
}

// The source's own settings rows and Local trial rows and rules are not copied,
// as its rule-named rows are not; a copied rule that names one is refused.
func TestMatrixCopyPlanDoesNotCopyTheSourcesSettingsRowsOrLocalTrialRows(t *testing.T) {
	source := func(rule string) *slicercli.Matrix {
		m := &slicercli.Matrix{
			Rows: []slicercli.Row{
				cRow("r1", "implementer", "Codex", "gpt-x"),
				cRow("r7", "reviewer", "Claude Code", "source-settings-spec"),
				cRow("r8", "reviewer", "Claude Code", "source-local-ui"),
				cRow("r3", "reviewer", "Claude Code", "source-spec"),
			},
			Rules: []slicercli.Rule{
				{ID: "u2", Text: "The spec check runs the Spec check row (r3)."},
				{ID: "u11", Text: "Local UI pass row (r8) is a trial row."},
				{ID: "u1", Text: copySourceRule},
			},
		}
		if rule != "" {
			m.Rules = append(m.Rules, slicercli.Rule{ID: "u7", Text: rule})
		}
		return m
	}
	target := &slicercli.Matrix{
		Rows:  []slicercli.Row{cRow("r9", "reviewer", "Codex", "target-spec")},
		Rules: []slicercli.Rule{{ID: "u6", Text: "The spec check runs the Spec check row (r9)."}},
	}
	src := registry.Project{Name: "src", SpecCheckRows: []string{"r7"}}
	plan, err := planMatrixCopy(src, source(""), registry.Project{Name: "p"}, target)
	if err != nil {
		t.Fatal(err)
	}
	set := plan.set().Replace
	var models []string
	for _, r := range set.Rows {
		models = append(models, r.Model)
	}
	if want := []string{"gpt-x", "target-spec"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("row models %v, want %v (only r1 is copied)", models, want)
	}
	wantRules := []copyRuleJSON{{ID: "u6", Text: "The spec check runs the Spec check row (r9)."}, {Text: copySourceRule}}
	if !reflect.DeepEqual(set.Rules, wantRules) {
		t.Fatalf("rules %+v, want %+v (the source's Local rule is not copied)", set.Rules, wantRules)
	}
	for _, row := range []string{"r7", "r8"} {
		_, err := planMatrixCopy(src, source("Never use "+row+" for implementing."), registry.Project{Name: "p"}, target)
		if e := ruleErr(t, err, CodeCopyConflict); !strings.Contains(e.Message, "u7") || !strings.Contains(e.Message, row) {
			t.Fatalf("message %q does not name the rule and %s", e.Message, row)
		}
	}
}

// A source with no Spec check or UI pass row leaves the target's own rows at
// the end, in the target's order, and its rules first.
func TestMatrixCopyPlanPutsKeptRowsAtTheEndWhenTheSourceHasNone(t *testing.T) {
	source := &slicercli.Matrix{
		Rows:  []slicercli.Row{cRow("r1", "implementer", "Codex", "gpt-x"), cRow("r2", "gate", "Claude Code", "opus")},
		Rules: []slicercli.Rule{{ID: "u1", Text: copySourceRule}},
	}
	target := &slicercli.Matrix{
		Rows: []slicercli.Row{cRow("r12", "reviewer", "Codex", "target-ui"), cRow("r9", "reviewer", "Codex", "target-spec")},
		Rules: []slicercli.Rule{
			{ID: "u6", Text: "The spec check runs the Spec check row (r9)."},
			{ID: "u7", Text: "The UI pass row (r12) reads internal/tui."},
		},
	}
	plan, err := planCopy("p", source, target)
	if err != nil {
		t.Fatal(err)
	}
	set := plan.set().Replace
	var ids []string
	for _, r := range set.Rows {
		ids = append(ids, r.ID)
	}
	if want := []string{"", "", "r9", "r12"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("row ids %v, want %v (the spec row, then the UI row, after the copied rows)", ids, want)
	}
	wantRules := []copyRuleJSON{
		{ID: "u6", Text: "The spec check runs the Spec check row (r9)."},
		{ID: "u7", Text: "The UI pass row (r12) reads internal/tui."},
		{Text: copySourceRule},
	}
	if !reflect.DeepEqual(set.Rules, wantRules) {
		t.Fatalf("rules %+v, want %+v", set.Rules, wantRules)
	}
}

func TestMatrixCopyRewritesRowIDsInCopiedRules(t *testing.T) {
	source := &slicercli.Matrix{
		Rows: []slicercli.Row{
			cRow("r8", "implementer", "Claude Code", "haiku"),
			cRow("r4", "implementer", "Claude Code", "haiku-b"),
			cRow("r3", "implementer", "Claude Code", "sonnet"),
			cRow("r30", "implementer", "Claude Code", "sonnet-b"),
			cRow("r20", "reviewer", "Claude Code", "source-spec"),
		},
		Rules: []slicercli.Rule{
			{ID: "u3", Text: "Haiku rows are for slivers (r8) and parts (r4); r3 is the stand-in. Leave r30x, rr8 and r99 alone."},
			{ID: "u9", Text: "The spec check runs the Spec check row (r20)."},
		},
	}
	target := &slicercli.Matrix{
		Rows:  []slicercli.Row{cRow("r9", "reviewer", "Codex", "target-spec")},
		Rules: []slicercli.Rule{{ID: "u6", Text: "The spec check runs the Spec check row (r9)."}},
	}
	plan, err := planCopy("p", source, target)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.pending() {
		t.Fatal("a rule names rows that slicer has yet to add; the plan must say it waits for their ids")
	}
	if want := []string{"r8", "r4", "r3", "r30"}; !reflect.DeepEqual(plan.newRows(), want) {
		t.Fatalf("new rows %v, want %v", plan.newRows(), want)
	}
	if err := plan.rewrite(map[string]string{"r8": "r31", "r4": "r32", "r3": "r33", "r30": "r34"}); err != nil {
		t.Fatal(err)
	}
	want := "Haiku rows are for slivers (r31) and parts (r32); r33 is the stand-in. Leave r30x, rr8 and r99 alone."
	rules := plan.set().Replace.Rules
	if len(rules) != 2 || rules[1].Text != want {
		t.Fatalf("rules %+v, want the second to read %q", rules, want)
	}
	if plan.pending() {
		t.Fatal("the plan still waits for ids after the rewrite")
	}
}

func TestMatrixCopyRefusesACopiedRuleNamingARowThatIsNotCopied(t *testing.T) {
	for _, tc := range []struct{ name, rule, row string }{
		{"UI pass row", "A slice that touches the UI also runs r14 as its own role.", "r14"},
		{"spec check row", "Never use r3 for implementing.", "r3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &slicercli.Matrix{
				Rows: []slicercli.Row{
					cRow("r1", "implementer", "Claude Code", "sonnet"),
					cRow("r14", "reviewer", "Claude Code", "source-ui"),
					cRow("r3", "reviewer", "Claude Code", "source-spec"),
				},
				Rules: []slicercli.Rule{
					{ID: "u10", Text: "The UI pass row (r14) reads internal/tui."},
					{ID: "u11", Text: "The spec check runs the Spec check row (r3)."},
					{ID: "u7", Text: tc.rule},
				},
			}
			target := &slicercli.Matrix{
				Rows:  []slicercli.Row{cRow("r9", "reviewer", "Codex", "target-spec")},
				Rules: []slicercli.Rule{{ID: "u6", Text: "The spec check runs the Spec check row (r9)."}},
			}
			_, err := planCopy("p", source, target)
			e := ruleErr(t, err, CodeCopyConflict)
			if !strings.Contains(e.Message, "u7") || !strings.Contains(e.Message, tc.row) {
				t.Fatalf("message %q does not name the rule and the row", e.Message)
			}
		})
	}
}

func TestMatrixCopyWarnsWhenAKeptRuleNamesARemovedRow(t *testing.T) {
	source := &slicercli.Matrix{
		Rows: []slicercli.Row{
			cRow("r1", "implementer", "Claude Code", "same"),
			cRow("r3", "reviewer", "Claude Code", "source-spec"),
		},
		Rules: []slicercli.Rule{{ID: "u2", Text: "The spec check runs the Spec check row (r3)."}},
	}
	target := &slicercli.Matrix{
		Rows: []slicercli.Row{
			cRow("r4", "implementer", "Codex", "removed"),
			cRow("r5", "implementer", "Claude Code", "same"), // identical to the source's r1: kept
			cRow("r9", "reviewer", "Codex", "target-spec"),
		},
		Rules: []slicercli.Rule{{ID: "u6", Text: "The spec check runs the Spec check row (r9); r4 and r5 are fallbacks."}},
	}
	plan, err := planCopy("p", source, target)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"u6 names r4, which the copy removes"}; !reflect.DeepEqual(plan.Warnings, want) {
		t.Fatalf("warnings %q, want %q", plan.Warnings, want)
	}
}

func TestMatrixCopyWarnsForRowsWithoutAnAdapter(t *testing.T) {
	source := &slicercli.Matrix{
		Rows: []slicercli.Row{
			cRow("r1", "reviewer", "Acme Agent", "a"),
			cRow("r2", "reviewer", "Acme Agent", "b"),
			cRow("r4", "reviewer", "Grok", "c"),
			cRow("r5", "reviewer", "Claude Code", "d"),
			cRow("r6", "gate", "Other Agent", "same"),
			cRow("r3", "reviewer", "Claude Code", "source-spec"),
		},
		Rules: []slicercli.Rule{{ID: "u2", Text: "The spec check runs the Spec check row (r3)."}},
	}
	target := &slicercli.Matrix{
		Rows: []slicercli.Row{
			cRow("r7", "gate", "Other Agent", "same"), // identical: not added, no warning
			cRow("r9", "reviewer", "Codex", "target-spec"),
		},
		Rules: []slicercli.Rule{{ID: "u6", Text: "The spec check runs the Spec check row (r9)."}},
	}
	plan, err := planCopy("p", source, target)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{noAdapterOne}; !reflect.DeepEqual(plan.Warnings, want) {
		t.Fatalf("warnings %q, want %q (one per agent, none for Grok, which has an adapter)", plan.Warnings, want)
	}
}

// A target row that is the same as a source row is kept by id, and so is a
// target rule with the copied text. Without it a second copy would remove and
// add every row, and slicer could never say "unchanged".
func TestMatrixCopyPlanKeepsIdenticalTargetRowsAndRulesByID(t *testing.T) {
	source := &slicercli.Matrix{
		Rows: []slicercli.Row{
			{ID: "r1", Role: "implementer", When: json.RawMessage(`{"size": ["M"]}`), Agent: "Codex", Vendor: "OpenAI", Model: "gpt-x", Reasoning: "high"},
			cRow("r2", "reviewer", "Claude Code", "opus"),
			cRow("r3", "reviewer", "Claude Code", "source-spec"),
		},
		Rules: []slicercli.Rule{
			{ID: "u1", Text: copySourceRule},
			{ID: "u2", Text: "The spec check runs the Spec check row (r3)."},
			{ID: "u3", Text: "Prefer r1 for anything."},
		},
	}
	target := &slicercli.Matrix{
		Rows: []slicercli.Row{
			cRow("r9", "reviewer", "Codex", "target-spec"),
			{ID: "r5", Role: "implementer", When: json.RawMessage(`{"size":["M"]}`), Agent: "Codex", Vendor: "OpenAI", Model: "gpt-x", Reasoning: "high"},
			cRow("r6", "reviewer", "Claude Code", "other"),
		},
		Rules: []slicercli.Rule{
			{ID: "u6", Text: "The spec check runs the Spec check row (r9)."},
			{ID: "u7", Text: copySourceRule},
			{ID: "u8", Text: "Prefer r5 for anything."},
		},
	}
	plan, err := planCopy("p", source, target)
	if err != nil {
		t.Fatal(err)
	}
	// r1 is matched (target r5), so the rule that names it is already final;
	// r2 is new but no rule names it.
	if plan.pending() {
		t.Fatal("no copied rule names a row that slicer has yet to add")
	}
	set := plan.set().Replace
	var ids []string
	for _, r := range set.Rows {
		ids = append(ids, r.ID)
	}
	if want := []string{"r5", "", "r9"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("row ids %v, want %v", ids, want)
	}
	wantRules := []copyRuleJSON{
		{ID: "u6", Text: "The spec check runs the Spec check row (r9)."},
		{ID: "u7", Text: copySourceRule},
		{ID: "u8", Text: "Prefer r5 for anything."},
	}
	if !reflect.DeepEqual(set.Rules, wantRules) {
		t.Fatalf("rules\n got %+v\nwant %+v", set.Rules, wantRules)
	}
	if want := []string{"r2"}; !reflect.DeepEqual(plan.newRows(), want) {
		t.Fatalf("new rows %v, want %v", plan.newRows(), want)
	}
}

func cRow(id, role, agent, model string) slicercli.Row {
	return slicercli.Row{ID: id, Role: role, When: json.RawMessage(`{"size":[]}`), Agent: agent, Vendor: "V", Model: model, Reasoning: "high"}
}

// ---- the handler ----

// copyHarness is a daemon with the named projects registered at /p/NAME and a
// slicer wrapper that keeps the stdin of each `relay-roles apply` call, one
// JSON line per call, in call order.
type copyHarness struct {
	d     *Daemon
	stdin string
}

func newCopyHarness(t *testing.T, names []string, routes ...clitest.Route) *copyHarness {
	t.Helper()
	var projects []registry.Project
	for _, n := range names {
		projects = append(projects, registry.Project{Name: n, Path: "/p/" + n})
	}
	return newCopyHarnessFor(t, projects, routes...)
}

// newCopyHarnessFor is newCopyHarness for projects that carry settings; each is
// at /p/NAME.
func newCopyHarnessFor(t *testing.T, projects []registry.Project, routes ...clitest.Route) *copyHarness {
	t.Helper()
	dir := t.TempDir()
	reg := filepath.Join(dir, "projects.json")
	if err := registry.Save(reg, &registry.File{Version: registry.Version, Projects: projects}); err != nil {
		t.Fatal(err)
	}
	fake := clitest.Install(t, routes...)
	lastFake = fake
	bin := filepath.Join(dir, "slicer-capture")
	script := fmt.Sprintf("#!/bin/sh\ncase \" $* \" in\n*\" apply \"*) cat >> \"$0.stdin\"; printf '\\n' >> \"$0.stdin\" ;;\nesac\nexec %q \"$@\"\n", fake.Bin("slicer"))
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	deps := engine.Deps{Slicer: slicercli.Client{Bin: bin}}
	d := New(Config{Paths: PathsIn(filepath.Join(dir, "state")), RegistryPath: reg, Source: &stubSource{}, Engine: &deps})
	t.Cleanup(d.cache.Close)
	t.Cleanup(d.closeRuns)
	return &copyHarness{d: d, stdin: bin + ".stdin"}
}

// sets returns the change sets sent to `relay-roles apply`, in call order.
func (h *copyHarness) sets(t *testing.T) []copySent {
	t.Helper()
	data, err := os.ReadFile(h.stdin)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []copySent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var s copySent
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			t.Fatalf("stdin %q: %v", line, err)
		}
		out = append(out, s)
	}
	return out
}

type copySent struct {
	Expect  string `json:"expect"`
	Replace struct {
		Rows  []map[string]any `json:"rows"`
		Rules []copyRuleJSON   `json:"rules"`
	} `json:"replace"`
}

func (h *copyHarness) copy(t *testing.T, raw string) MatrixCopyResult {
	t.Helper()
	var got MatrixCopyResult
	if err := json.Unmarshal(request(t, h.d, "matrix.copy", raw), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func showBody(t *testing.T, revision string, rows []slicercli.Row, rules []slicercli.Rule) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"relay_roles": "x", "rows": rows, "rules": rules, "revision": revision})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func showRoute(name, body string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "show", "--json", "--root", "/p/" + name}, Stdout: body}
}

func missingMatrixRoute(name string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "show", "--json", "--root", "/p/" + name}, Exit: 2,
		Stdout: `{"error":{"code":"no_relay_roles","message":"/p/` + name + `/.slicer/relay-roles.json does not exist"}}`}
}

func dryArgs(name string) []string {
	return []string{"relay-roles", "apply", "--stdin", "--dry-run", "--json", "--root", "/p/" + name}
}

func writeArgs(name string) []string {
	return []string{"relay-roles", "apply", "--stdin", "--render", "--json", "--root", "/p/" + name}
}

// dryReply is a slicer apply result; each step is "OP ID".
func dryReply(revisionBefore string, changed bool, steps ...string) clitest.Reply {
	var parts []string
	for _, s := range steps {
		op, id, _ := strings.Cut(s, " ")
		parts = append(parts, fmt.Sprintf(`{"op":%q,"id":%q,"changed":true}`, op, id))
	}
	return clitest.Reply{Stdout: fmt.Sprintf(`{"dry_run":true,"changed":%t,"revision_before":%q,"revision":"after","steps":[%s],"rows":[],"rules":[]}`, changed, revisionBefore, strings.Join(parts, ","))}
}

func refusal(code, message string) clitest.Reply {
	return clitest.Reply{Exit: 2, Stdout: fmt.Sprintf(`{"error":{"code":%q,"message":%q}}`, code, message)}
}

func dryRoute(name string, replies ...clitest.Reply) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: dryArgs(name), Seq: replies}
}

func writeRoute(name string, replies ...clitest.Reply) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: writeArgs(name), Seq: replies}
}

var (
	srcRows = []slicercli.Row{
		cRow("r1", "implementer", "Codex", "gpt-x"),
		cRow("r2", "reviewer", "Claude Code", "opus"),
		cRow("r3", "reviewer", "Claude Code", "source-spec"),
	}
	srcRules = []slicercli.Rule{
		{ID: "u1", Text: copySourceRule},
		{ID: "u2", Text: "The spec check runs the Spec check row (r3)."},
	}
	tgtRows = []slicercli.Row{
		cRow("r1", "implementer", "Claude Code", "old"),
		cRow("r9", "reviewer", "Codex", "target-spec"),
	}
	tgtRules = []slicercli.Rule{{ID: "u6", Text: "The spec check runs the Spec check row (r9)."}}
)

// srcTarget routes the source's matrix and the matrices of targets.
func srcTarget(t *testing.T, targets ...string) []clitest.Route {
	t.Helper()
	routes := []clitest.Route{showRoute("src", showBody(t, "rev-src", srcRows, srcRules))}
	for _, n := range targets {
		routes = append(routes, showRoute(n, showBody(t, "rev-"+n, tgtRows, tgtRules)))
	}
	return routes
}

func callArgs(t *testing.T) [][]string {
	t.Helper()
	var out [][]string
	for _, c := range lastFake.CallsTo("slicer") {
		out = append(out, c.Args)
	}
	return out
}

// isWrite is a `relay-roles apply` call that saves; isDry one that does not.
func isWrite(args []string) bool {
	return len(args) > 1 && args[1] == "apply" && slices.Contains(args, "--render")
}

func isDry(args []string) bool {
	return len(args) > 1 && args[1] == "apply" && slices.Contains(args, "--dry-run")
}

func TestMatrixCopyDryRunWritesNothing(t *testing.T) {
	routes := append(srcTarget(t, "a"), dryRoute("a", dryReply("rev-a", true, "remove r1", "add r10", "add r11")))
	h := newCopyHarness(t, []string{"src", "a"}, routes...)
	got := h.copy(t, `{"from":"src","to":["a"],"dry_run":true}`)
	want := [][]string{
		{"relay-roles", "show", "--json", "--root", "/p/src"},
		{"relay-roles", "show", "--json", "--root", "/p/a"},
		dryArgs("a"),
	}
	if calls := callArgs(t); !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls\n got %v\nwant %v", calls, want)
	}
	if got.From != "src" || len(got.Projects) != 1 || got.Partial {
		t.Fatalf("result %+v", got)
	}
	p := got.Projects[0]
	wantSteps := []MatrixCopyStep{{Op: "remove", ID: "r1", Changed: true}, {Op: "add", ID: "r10", Changed: true}, {Op: "add", ID: "r11", Changed: true}}
	if p.Project != "a" || p.Status != CopyWouldChange || !reflect.DeepEqual(p.Steps, wantSteps) || p.Error != nil {
		t.Fatalf("project %+v", p)
	}
	sent := h.sets(t)
	if len(sent) != 1 || sent[0].Expect != "" || len(sent[0].Replace.Rows) != 3 {
		t.Fatalf("sent %+v", sent)
	}
}

func TestMatrixCopyAppliesWithTheDryRunsRevision(t *testing.T) {
	src := []slicercli.Rule{
		{ID: "u1", Text: copySourceRule},
		{ID: "u2", Text: "The spec check runs the Spec check row (r3)."},
		{ID: "u3", Text: "Use r2 for review."},
	}
	routes := []clitest.Route{
		showRoute("src", showBody(t, "rev-src", srcRows, src)),
		showRoute("a", showBody(t, "rev-a-show", tgtRows, tgtRules)),
		// The first dry run learns the new ids; the second checks the rewritten
		// set and gives the revision the write expects.
		dryRoute("a",
			dryReply("rev-0", true, "remove r1", "add r10", "add r11"),
			dryReply("rev-a", true, "remove r1", "add r10", "add r11"),
		),
		writeRoute("a", clitest.Reply{Stdout: `{"changed":true,"revision_before":"rev-a","revision":"rev-z","steps":[{"op":"remove","id":"r1","changed":true},{"op":"add","id":"r10","changed":true},{"op":"add","id":"r11","changed":true}]}`}),
	}
	h := newCopyHarness(t, []string{"src", "a"}, routes...)
	got := h.copy(t, `{"from":"src","to":["a"]}`)
	if len(got.Projects) != 1 || got.Projects[0].Status != CopyChanged || got.Partial {
		t.Fatalf("result %+v", got)
	}
	want := [][]string{
		{"relay-roles", "show", "--json", "--root", "/p/src"},
		{"relay-roles", "show", "--json", "--root", "/p/a"},
		dryArgs("a"), dryArgs("a"), writeArgs("a"),
	}
	if calls := callArgs(t); !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls\n got %v\nwant %v", calls, want)
	}
	sent := h.sets(t)
	if len(sent) != 3 {
		t.Fatalf("sent %d change sets", len(sent))
	}
	if sent[0].Replace.Rules[2].Text != "Use r2 for review." {
		t.Fatalf("the first dry run has rule text %q, want the source's", sent[0].Replace.Rules[2].Text)
	}
	if sent[1].Expect != "" || sent[1].Replace.Rules[2].Text != "Use r11 for review." {
		t.Fatalf("the second dry run %+v", sent[1])
	}
	if sent[2].Expect != "rev-a" || sent[2].Replace.Rules[2].Text != "Use r11 for review." {
		t.Fatalf("write expect %q, rule %q; want rev-a, the rewritten text", sent[2].Expect, sent[2].Replace.Rules[2].Text)
	}
	if steps := got.Projects[0].Steps; len(steps) != 3 || steps[2].ID != "r11" {
		t.Fatalf("steps %+v", steps)
	}
}

// pendingSrcRules makes the copy wait for the ids of the rows slicer adds: a
// copied rule names the source's r2.
var pendingSrcRules = []slicercli.Rule{
	{ID: "u1", Text: copySourceRule},
	{ID: "u2", Text: "The spec check runs the Spec check row (r3)."},
	{ID: "u3", Text: "Use r2 for review."},
}

func TestMatrixCopyAddIDsThatDifferBetweenTheDryRunsFailTheProject(t *testing.T) {
	routes := []clitest.Route{
		showRoute("src", showBody(t, "rev-src", srcRows, pendingSrcRules)),
		showRoute("a", showBody(t, "rev-a-show", tgtRows, tgtRules)),
		// The second dry run gives the second added row another id than the first.
		dryRoute("a",
			dryReply("rev-0", true, "remove r1", "add r10", "add r11"),
			dryReply("rev-a", true, "remove r1", "add r10", "add r12"),
		),
	}
	h := newCopyHarness(t, []string{"src", "a"}, routes...)
	got := h.copy(t, `{"from":"src","to":["a"]}`)
	if len(got.Projects) != 1 {
		t.Fatalf("result %+v", got)
	}
	p := got.Projects[0]
	if p.Error == nil || p.Error.Code != "add_id_mismatch" || p.Error.Message != "slicer gave added rows ids r10, r11 in the first dry run and r10, r12 in the second; nothing was written to this project: preview again, then copy" {
		t.Fatalf("want named add-id refusal and recovery guidance, got %+v", p.Error)
	}
	if p.Status != CopyFailed || p.Error == nil || !strings.Contains(p.Error.Message, "r11") || !strings.Contains(p.Error.Message, "r12") {
		t.Fatalf("project %+v, want failed with an error that names both ids", p)
	}
	for _, args := range callArgs(t) {
		if isWrite(args) {
			t.Fatalf("a write call after the add ids changed: %v", args)
		}
	}
}

func TestMatrixCopyFewerAddsThanNewRowsFailTheProject(t *testing.T) {
	routes := []clitest.Route{
		showRoute("src", showBody(t, "rev-src", srcRows, pendingSrcRules)),
		showRoute("a", showBody(t, "rev-a-show", tgtRows, tgtRules)),
		// Two rows are new, but slicer adds one.
		dryRoute("a", dryReply("rev-0", true, "remove r1", "add r10")),
	}
	h := newCopyHarness(t, []string{"src", "a"}, routes...)
	got := h.copy(t, `{"from":"src","to":["a"]}`)
	p := got.Projects[0]
	if p.Status != CopyFailed || p.Error == nil || p.Error.Message != "slicer added 1 rows to a for 2 new rows" {
		t.Fatalf("project %+v", p)
	}
	for _, args := range callArgs(t) {
		if isWrite(args) {
			t.Fatalf("a write call after the count mismatch: %v", args)
		}
	}
}

func TestMatrixCopyWritesOnlyProjectsWhoseStepsMatchThePreview(t *testing.T) {
	changed := clitest.Reply{Stdout: `{"changed":true,"revision":"z","steps":[{"op":"add","id":"r10","changed":true}]}`}
	routes := append(srcTarget(t, "a", "b", "c", "d"),
		dryRoute("a", dryReply("rev-a", true, "add r10")),
		dryRoute("b", dryReply("rev-b", true, "add r10", "add r11")), // the preview showed one step
		dryRoute("c", dryReply("rev-c", true, "add r10")),
		dryRoute("d", dryReply("rev-d", false)), // the preview showed a change
		writeRoute("a", changed),
		writeRoute("c", changed),
	)
	h := newCopyHarness(t, []string{"src", "a", "b", "c", "d"}, routes...)
	step := `[{"op":"add","id":"r10","changed":true}]`
	got := h.copy(t, `{"from":"src","to":["a","b","c","d"],"expect_steps":{"a":`+step+`,"b":`+step+`,"c":`+step+`,"d":`+step+`}}`)
	var statuses []string
	for _, p := range got.Projects {
		statuses = append(statuses, p.Project+" "+p.Status)
	}
	if want := []string{"a changed", "b failed", "c changed", "d failed"}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses %v, want %v", statuses, want)
	}
	for _, i := range []int{1, 3} {
		if e := got.Projects[i].Error; e == nil || e.Code != CodeStalePreview {
			t.Fatalf("%s error %+v, want %s", got.Projects[i].Project, e, CodeStalePreview)
		}
	}
	if !got.Partial {
		t.Fatal("some projects changed and some were refused: partial")
	}
	var written []string
	for _, args := range callArgs(t) {
		if isWrite(args) {
			written = append(written, args[len(args)-1])
		}
	}
	if want := []string{"/p/a", "/p/c"}; !reflect.DeepEqual(written, want) {
		t.Fatalf("written %v, want %v: a refused project is not written", written, want)
	}
}

// copyThenWrite previews the copy to a and c, then writes it with the preview's
// steps for both and its digest for a only (c is not bound by content). The
// source's matrix is read once per call: srcShows are its replies, in order.
func copyThenWrite(t *testing.T, srcShows ...string) (preview, written MatrixCopyResult) {
	t.Helper()
	var seq []clitest.Reply
	for _, s := range srcShows {
		seq = append(seq, clitest.Reply{Stdout: s})
	}
	changed := clitest.Reply{Stdout: `{"changed":true,"revision":"z","steps":[{"op":"add","id":"r10","changed":true}]}`}
	routes := []clitest.Route{
		{Tool: "slicer", Args: []string{"relay-roles", "show", "--json", "--root", "/p/src"}, Seq: seq},
		showRoute("a", showBody(t, "rev-a", tgtRows, tgtRules)),
		showRoute("c", showBody(t, "rev-c", tgtRows, tgtRules)),
		dryRoute("a", dryReply("rev-a", true, "add r10")),
		dryRoute("c", dryReply("rev-c", true, "add r10")),
		writeRoute("a", changed),
		writeRoute("c", changed),
	}
	h := newCopyHarness(t, []string{"src", "a", "c"}, routes...)
	preview = h.copy(t, `{"from":"src","to":["a","c"],"dry_run":true}`)
	digest := preview.Projects[0].Digest
	if digest == "" {
		t.Fatalf("the preview of a carries no digest: %+v", preview.Projects[0])
	}
	step := `[{"op":"add","id":"r10","changed":true}]`
	written = h.copy(t, `{"from":"src","to":["a","c"],"expect_steps":{"a":`+step+`,"c":`+step+`},"expect_rows":{"a":"`+digest+`"}}`)
	return preview, written
}

// A source row edited between the preview and the write leaves the steps as they
// were, so only its content tells (S558).
func TestMatrixCopyRefusesAProjectWhoseSourceRowsChangedSinceThePreview(t *testing.T) {
	edited := slices.Clone(srcRows)
	edited[0].Model = "gpt-edited"
	_, got := copyThenWrite(t,
		showBody(t, "rev-src", srcRows, srcRules),
		showBody(t, "rev-src2", edited, srcRules))
	a := got.Projects[0]
	if a.Status != CopyFailed || a.Error == nil || a.Error.Code != CodeStalePreview {
		t.Fatalf("a %+v, want failed with %s", a, CodeStalePreview)
	}
	if c := got.Projects[1]; c.Status != CopyChanged {
		t.Fatalf("c %+v, want changed: a refused project does not stop the others", c)
	}
	if !got.Partial {
		t.Fatal("one project changed and one was refused: partial")
	}
	var written []string
	for _, args := range callArgs(t) {
		if isWrite(args) {
			written = append(written, args[len(args)-1])
		}
	}
	if want := []string{"/p/c"}; !reflect.DeepEqual(written, want) {
		t.Fatalf("written %v, want %v: a refused project is not written", written, want)
	}
}

func TestMatrixCopyWritesAProjectWhoseSourceRowsAreTheOnesPreviewed(t *testing.T) {
	_, got := copyThenWrite(t, showBody(t, "rev-src", srcRows, srcRules))
	for _, p := range got.Projects {
		if p.Status != CopyChanged || p.Error != nil {
			t.Fatalf("project %+v, want changed", p)
		}
	}
	if got.Partial {
		t.Fatal("every project was written: not partial")
	}
}

func TestMatrixCopyRefusesChangedCopiedRuleText(t *testing.T) {
	edited := slices.Clone(srcRules)
	edited[0].Text += " Changed rule text."
	_, got := copyThenWrite(t, showBody(t, "rev-src", srcRows, srcRules), showBody(t, "rev-src", srcRows, edited))
	a := got.Projects[0]
	if a.Status != CopyFailed || a.Error == nil || a.Error.Code != CodeStalePreview {
		t.Fatalf("changed copied rule must refuse bound project: %+v", a)
	}
	if got.Projects[1].Status != CopyChanged || !got.Partial {
		t.Fatalf("unbound project should still change: %+v", got)
	}
	for _, set := range lastFake.CallsTo("slicer") {
		if isWrite(set.Args) && set.Args[len(set.Args)-1] == "/p/a" {
			t.Fatal("refused project's write was invoked")
		}
	}
}

func TestMatrixCopySetFieldsDescribeOnlyChangedValues(t *testing.T) {
	base := copyRowJSON{ID: "r1", Role: "reviewer", When: json.RawMessage(`["L"]`), Agent: "Codex", Vendor: "openai", Model: "gpt-x", Reasoning: "high", Note: "old"}
	for _, field := range []string{"role", "when", "agent", "vendor", "model", "reasoning", "note"} {
		t.Run(field, func(t *testing.T) {
			body, _ := json.Marshal(base)
			var changed map[string]json.RawMessage
			_ = json.Unmarshal(body, &changed)
			value := `"new"`
			want := "new"
			if field == "when" {
				value, want = `["M"]`, `["M"]`
			}
			if field == "note" {
				value, want = `""`, ""
			}
			changed[field] = json.RawMessage(value)
			body, _ = json.Marshal(changed)
			var row copyRowJSON
			_ = json.Unmarshal(body, &row)
			var target slicercli.Row
			body, _ = json.Marshal(base)
			_ = json.Unmarshal(body, &target)
			target.When = json.RawMessage(`[ "L" ]`)
			steps := []MatrixCopyStep{{Op: "set", ID: "r1", Changed: true}}
			setStepFields(steps, copySet{Replace: copyReplace{Rows: []copyRowJSON{row}}}, &slicercli.Matrix{Rows: []slicercli.Row{target}})
			body, _ = json.Marshal(steps[0])
			var got struct {
				Fields map[string]string `json:"fields"`
			}
			_ = json.Unmarshal(body, &got)
			if !reflect.DeepEqual(got.Fields, map[string]string{field: want}) {
				t.Fatalf("changed %s: fields %v, want only %s=%q", field, got.Fields, field, want)
			}
		})
	}
}

func TestMatrixCopyWithoutExpectedStepsWritesWhateverTheDryRunSays(t *testing.T) {
	routes := append(srcTarget(t, "a"),
		dryRoute("a", dryReply("rev-a", true, "add r10", "add r11")),
		writeRoute("a", clitest.Reply{Stdout: `{"changed":true,"revision":"z","steps":[{"op":"add","id":"r10","changed":true}]}`}),
	)
	h := newCopyHarness(t, []string{"src", "a"}, routes...)
	got := h.copy(t, `{"from":"src","to":["a"]}`)
	if got.Projects[0].Status != CopyChanged || got.Partial {
		t.Fatalf("result %+v", got)
	}
}

func TestMatrixCopyADryRunIgnoresExpectedSteps(t *testing.T) {
	routes := append(srcTarget(t, "a"), dryRoute("a", dryReply("rev-a", true, "add r10", "add r11")))
	h := newCopyHarness(t, []string{"src", "a"}, routes...)
	got := h.copy(t, `{"from":"src","to":["a"],"dry_run":true,"expect_steps":{"a":[{"op":"add","id":"r10","changed":true}]}}`)
	if p := got.Projects[0]; p.Status != CopyWouldChange || len(p.Steps) != 2 {
		t.Fatalf("project %+v, want the preview of what would change", p)
	}
}

func TestMatrixCopyAFailedCheckStillWritesNothingWhenStepsAreExpected(t *testing.T) {
	// Only a stale preview is refused per project; a failed check anywhere still
	// writes nothing.
	step := `[{"op":"add","id":"r10","changed":true}]`
	routes := append(srcTarget(t, "a", "b"),
		dryRoute("a", dryReply("rev-a", true, "add r10")),
		dryRoute("b", refusal("bad_role_field", "row 2: reasoning must not be empty")),
	)
	h := newCopyHarness(t, []string{"src", "a", "b"}, routes...)
	got := h.copy(t, `{"from":"src","to":["a","b"],"expect_steps":{"a":`+step+`}}`)
	for _, args := range callArgs(t) {
		if isWrite(args) {
			t.Fatalf("a write call after a failed check: %v", args)
		}
	}
	if got.Projects[0].Status != CopyWouldChange || got.Projects[1].Status != CopyFailed {
		t.Fatalf("result %+v", got)
	}
}

func TestMatrixCopySameMatrixIsUnchanged(t *testing.T) {
	routes := append(srcTarget(t, "a"), dryRoute("a", dryReply("rev-a", false)))
	h := newCopyHarness(t, []string{"src", "a"}, routes...)
	got := h.copy(t, `{"from":"src","to":["a"]}`)
	if len(got.Projects) != 1 || got.Projects[0].Status != CopyUnchanged || got.Partial {
		t.Fatalf("result %+v", got)
	}
	for _, args := range callArgs(t) {
		if reflect.DeepEqual(args, writeArgs("a")) {
			t.Fatal("an unchanged matrix was written")
		}
	}
}

func TestMatrixCopyAllChecksEveryProjectBeforeAnyWrite(t *testing.T) {
	routes := append(srcTarget(t, "a", "b", "c"),
		dryRoute("a", dryReply("rev-a", true, "add r10")),
		dryRoute("b", refusal("bad_role_field", "row 2: reasoning must not be empty")),
		dryRoute("c", dryReply("rev-c", true, "add r10")),
	)
	h := newCopyHarness(t, []string{"src", "a", "b", "c"}, routes...)
	got := h.copy(t, `{"from":"src","all":true}`)
	for _, args := range callArgs(t) {
		if isWrite(args) {
			t.Fatalf("a write call after a failed check: %v", args)
		}
	}
	var statuses []string
	for _, p := range got.Projects {
		statuses = append(statuses, p.Project+" "+p.Status)
	}
	if want := []string{"a would change", "b failed", "c would change"}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses %v, want %v", statuses, want)
	}
	if e := got.Projects[1].Error; e == nil || e.Code != "bad_role_field" || e.Message != "row 2: reasoning must not be empty" {
		t.Fatalf("error %+v, want slicer's code and message", e)
	}
	if got.Partial {
		t.Fatal("nothing was written, so the result is not partial")
	}
}

func TestMatrixCopyAllReportsAWriteFailureAndGoesOn(t *testing.T) {
	changed := clitest.Reply{Stdout: `{"changed":true,"revision":"z","steps":[{"op":"add","id":"r10","changed":true}]}`}
	routes := append(srcTarget(t, "a", "b", "c"),
		dryRoute("a", dryReply("rev-a", true, "add r10")),
		dryRoute("b", dryReply("rev-b", true, "add r10")),
		dryRoute("c", dryReply("rev-c", true, "add r10")),
		writeRoute("a", changed),
		writeRoute("b", refusal("stale_revision", "the matrix changed since revision rev-b")),
		writeRoute("c", changed),
	)
	h := newCopyHarness(t, []string{"src", "a", "b", "c"}, routes...)
	got := h.copy(t, `{"from":"src","all":true}`)
	var statuses []string
	for _, p := range got.Projects {
		statuses = append(statuses, p.Project+" "+p.Status)
	}
	if want := []string{"a changed", "b failed", "c changed"}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses %v, want %v", statuses, want)
	}
	if e := got.Projects[1].Error; e == nil || e.Code != "stale_revision" {
		t.Fatalf("error %+v", e)
	}
	if !got.Partial {
		t.Fatal("some projects changed and one failed: partial")
	}
	// Every dry run came before the first write.
	lastDry, firstWrite := -1, -1
	for i, args := range callArgs(t) {
		if isDry(args) {
			lastDry = i
		}
		if isWrite(args) && firstWrite < 0 {
			firstWrite = i
		}
	}
	if lastDry < 0 || firstWrite < lastDry {
		t.Fatalf("the first write (call %d) came before the last dry run (call %d)", firstWrite, lastDry)
	}
	sent := h.sets(t)
	if len(sent) != 6 || sent[3].Expect != "rev-a" || sent[4].Expect != "rev-b" || sent[5].Expect != "rev-c" {
		t.Fatalf("writes did not each expect their own dry run's revision: %+v", sent)
	}
}

func TestMatrixCopyAllSkipsTheSourceAndProjectsWithoutAMatrix(t *testing.T) {
	changed := clitest.Reply{Stdout: `{"changed":true,"revision":"z","steps":[{"op":"add","id":"r10","changed":true}]}`}
	routes := append(srcTarget(t, "a", "c"), missingMatrixRoute("b"),
		dryRoute("a", dryReply("rev-a", true, "add r10")),
		dryRoute("c", dryReply("rev-c", true, "add r10")),
		writeRoute("a", changed),
		writeRoute("c", changed),
	)
	h := newCopyHarness(t, []string{"src", "a", "b", "c"}, routes...)
	got := h.copy(t, `{"from":"src","all":true}`)
	var statuses []string
	for _, p := range got.Projects {
		statuses = append(statuses, p.Project+" "+p.Status)
	}
	if want := []string{"a changed", "b skipped", "c changed"}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses %v, want %v (the source is not a target)", statuses, want)
	}
	if e := got.Projects[1].Error; e == nil || !strings.Contains(e.Message, "run ork projects setup b relay-roles-init") {
		t.Fatalf("skip note %+v", e)
	}
	if got.Partial {
		t.Fatal("a skipped project is not a failure")
	}
}

func TestMatrixCopyOnAnOldSlicer(t *testing.T) {
	routes := append(srcTarget(t, "a"), clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "apply"}, Prefix: true, Exit: 2,
		Stderr: "argument command: invalid choice: 'apply' (choose from 'show', 'add')"})
	h := newCopyHarness(t, []string{"src", "a"}, routes...)
	_, err := h.d.handle(context.Background(), "matrix.copy", json.RawMessage(`{"from":"src","to":["a"]}`))
	e := ipc.ErrorOf(err)
	if e == nil || e.Code != "slicer_too_old" || e.Message != "slicer relay-roles apply is not available in this slicer; upgrade slicer (ork doctor --upgrade)" {
		t.Fatalf("error %+v", e)
	}
}

func TestMatrixCopyRefusesBadRequests(t *testing.T) {
	for _, tc := range []struct{ raw, code string }{
		{`{"to":["a"]}`, CodeBadParams},
		{`{"from":"src"}`, CodeBadParams},
		{`{"from":"src","to":[]}`, CodeBadParams},
		{`{"from":"src","to":["a"],"all":true}`, CodeBadParams},
		{`{"from":"src","to":["src"]}`, CodeBadParams},
		{`{"from":"nope","to":["a"]}`, CodeNotFound},
		{`{"from":"src","to":["nope"]}`, CodeNotFound},
		{`{"from":"src","to":["a","nope"]}`, CodeNotFound},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			h := newCopyHarness(t, []string{"src", "a"}, srcTarget(t, "a")...)
			_, err := h.d.handle(context.Background(), "matrix.copy", json.RawMessage(tc.raw))
			if e := ipc.ErrorOf(err); e == nil || e.Code != tc.code {
				t.Fatalf("error %v; want %s", err, tc.code)
			}
			if calls := lastFake.CallsTo("slicer"); len(calls) != 0 {
				t.Fatalf("a bad request called slicer: %+v", calls)
			}
		})
	}
}

// The registered target's own spec_check_rows reach the planner: the write keeps
// that row by id although no rule names it.
func TestMatrixCopyKeepsTheRegisteredTargetsSettingsRows(t *testing.T) {
	rows := []slicercli.Row{cRow("r1", "implementer", "Claude Code", "old"), cRow("r9", "reviewer", "Codex", "target-spec"), cRow("r50", "reviewer", "Codex", "settings-spec")}
	routes := []clitest.Route{
		showRoute("src", showBody(t, "rev-src", srcRows, srcRules)),
		showRoute("a", showBody(t, "rev-a", rows, tgtRules)),
		dryRoute("a", dryReply("rev-a", true, "add r60")),
	}
	h := newCopyHarnessFor(t, []registry.Project{
		{Name: "src", Path: "/p/src"},
		{Name: "a", Path: "/p/a", SpecCheckRows: []string{"r50"}},
	}, routes...)
	h.copy(t, `{"from":"src","to":["a"],"dry_run":true}`)
	sent := h.sets(t)
	if len(sent) != 1 {
		t.Fatalf("sent %d change sets", len(sent))
	}
	var ids []any
	for _, r := range sent[0].Replace.Rows {
		ids = append(ids, r["id"])
	}
	// r1 and r2 of the source are new; r50 sits at the source's spec row's place
	// and the rule's r9 follows.
	if want := []any{nil, nil, "r50", "r9"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("sent row ids %v, want %v", ids, want)
	}
}

func TestMatrixCopyARepeatedTargetIsCopiedOnce(t *testing.T) {
	routes := append(srcTarget(t, "a"), dryRoute("a", dryReply("rev-a", true, "add r10")))
	h := newCopyHarness(t, []string{"src", "a"}, routes...)
	got := h.copy(t, `{"from":"src","to":["a","a"],"dry_run":true}`)
	if len(got.Projects) != 1 || got.Projects[0].Project != "a" {
		t.Fatalf("result %+v", got)
	}
	if n := len(h.sets(t)); n != 1 {
		t.Fatalf("%d change sets sent for one target", n)
	}
}

// A refusal of the planner is that project's failure, and with it nothing is
// written anywhere.
func TestMatrixCopyAPlannerRefusalFailsOnlyThatProject(t *testing.T) {
	noSpec := showBody(t, "rev-b", tgtRows, []slicercli.Rule{{ID: "u1", Text: "Names no row."}})
	routes := []clitest.Route{
		showRoute("src", showBody(t, "rev-src", srcRows, srcRules)),
		showRoute("a", showBody(t, "rev-a", tgtRows, tgtRules)),
		showRoute("b", noSpec),
		dryRoute("a", dryReply("rev-a", true, "add r10")),
	}
	h := newCopyHarness(t, []string{"src", "a", "b"}, routes...)
	got := h.copy(t, `{"from":"src","all":true}`)
	if len(got.Projects) != 2 || got.Projects[0].Status != CopyWouldChange || got.Projects[1].Status != CopyFailed {
		t.Fatalf("result %+v", got)
	}
	if e := got.Projects[1].Error; e == nil || e.Code != CodeNoSpecCheckRow || strings.HasPrefix(e.Message, "b ") {
		t.Fatalf("error %+v", e)
	}
	for _, args := range callArgs(t) {
		if isWrite(args) {
			t.Fatalf("a write after a failed check: %v", args)
		}
	}
}

func TestMatrixCopyAnUnreadableSourceIsTheCallsError(t *testing.T) {
	h := newCopyHarness(t, []string{"src", "a"}, missingMatrixRoute("src"))
	_, err := h.d.handle(context.Background(), "matrix.copy", json.RawMessage(`{"from":"src","to":["a"]}`))
	if e := ipc.ErrorOf(err); e == nil || e.Code != "no_relay_roles" {
		t.Fatalf("error %v, want slicer's no_relay_roles", err)
	}
}

func TestMatrixCopyOverTheSocket(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "src", Path: "/p/src"}, registry.Project{Name: "a", Path: "/p/a"})
	routes := append(srcTarget(t, "a"), dryRoute("a", dryReply("rev-a", true, "add r10")))
	fake := clitest.Install(t, routes...)
	e.deps = &engine.Deps{Slicer: slicercli.Client{Bin: fake.Bin("slicer")}}
	e.start(t)
	c := e.dial(t)
	got, err := c.MatrixCopy(context.Background(), MatrixCopyParams{From: "src", To: []string{"a"}, DryRun: true})
	if err != nil || got.From != "src" || len(got.Projects) != 1 || got.Projects[0].Status != CopyWouldChange || len(got.Projects[0].Steps) != 1 {
		t.Fatalf("socket result %+v, %v", got, err)
	}
	var re *ipc.Error
	if _, err := c.MatrixCopy(context.Background(), MatrixCopyParams{From: "src", To: []string{"nope"}}); !errors.As(err, &re) || re.Code != CodeNotFound {
		t.Fatalf("unknown project: %v", err)
	}
	if _, err := c.MatrixCopy(context.Background(), MatrixCopyParams{From: "src"}); !errors.As(err, &re) || re.Code != CodeBadParams {
		t.Fatalf("bad request: %v", err)
	}
}
