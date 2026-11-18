// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
)

const draftReportS1 = "## Section Why\nNew why for S1.\n\n" +
	"## Section Files\n- `internal/a.go`\n- `internal/b.go` (new)\n\n" +
	"## Section Failing tests\n- TestA: a asserts b.\n\n" +
	"## Section Implement\n1. Do a.\n\n" +
	"## Section Check\nunchanged\n\n" +
	"## Owner needed\nnone\n"

// draftRig is a spec rig whose spec-draft role answers draftReportS1 and whose
// project checkout holds internal/a.go.
func draftRig(t *testing.T, extra ...clitest.Route) *specRig {
	t.Helper()
	rig := newSpecRig(t, rulesJSON(u11), extra...)
	rig.draft = draftReportS1
	reg, err := registry.Load(rig.d.cfg.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(reg.Projects[0].Path, "internal")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package internal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return rig
}

func (r *specRig) specDraft(t *testing.T, item string) (*SpecDraftResult, error) {
	t.Helper()
	raw, _ := json.Marshal(SpecDraftParams{Project: "p", Item: item})
	v, err := r.d.handle(context.Background(), MethodSpecDraft, json.RawMessage(raw))
	if err != nil {
		return nil, err
	}
	return v.(*SpecDraftResult), nil
}

func (r *specRig) specDraftApply(item string, sections ...string) (*SpecDraftApplyResult, error) {
	raw, _ := json.Marshal(SpecDraftApplyParams{Project: "p", Item: item, Sections: sections})
	v, err := r.d.handle(context.Background(), MethodSpecDraftApply, json.RawMessage(raw))
	if err != nil {
		return nil, err
	}
	return v.(*SpecDraftApplyResult), nil
}

// edits are the arguments of each `slicer edit` the fake slicer saw.
func (r *specRig) edits() [][]string {
	var out [][]string
	for _, c := range r.fake.CallsTo("slicer") {
		if c.Args[0] == "edit" {
			out = append(out, c.Args)
		}
	}
	return out
}

func draftSection(res *SpecDraftResult, name string) SpecDraftSectionView {
	for _, s := range res.Sections {
		if s.Name == name {
			return s
		}
	}
	return SpecDraftSectionView{}
}

func TestSpecDraftWritesNothingBeforeApproval(t *testing.T) {
	rig := draftRig(t)
	res, err := rig.specDraft(t, "S1")
	if err != nil {
		t.Fatal(err)
	}
	if edits := rig.edits(); len(edits) != 0 {
		t.Fatalf("a draft made slicer edit calls: %v", edits)
	}
	reqs := rig.drafts
	if len(reqs) != 1 || !reqs[0].ReadOnly || len(reqs[0].Allowlist) != 0 || reqs[0].Role != engine.RoleSpecDraft {
		t.Fatalf("the draft role is read-only and runs once: %+v", reqs)
	}
	// The row is the spec check row (u11 names r16).
	if reqs[0].Vendor != "claude" || reqs[0].Model != "sonnet-spec" || reqs[0].Effort != "low" {
		t.Errorf("row: %+v", reqs[0])
	}
	if len(rig.requests()) != 0 {
		t.Error("a draft ran the spec-check role")
	}
	if got := []string{res.Sections[0].Name, res.Sections[4].Name}; !reflect.DeepEqual(got, []string{"Why", "Check"}) || len(res.Sections) != 5 {
		t.Fatalf("sections: %+v", res.Sections)
	}
	why := draftSection(res, "Why")
	if why.Current != "why S1" || why.Text != "New why for S1." || why.Unchanged || len(why.Problems) != 0 {
		t.Errorf("Why: %+v", why)
	}
	// The command is the one an approval runs, as the TUI shows it.
	if want := "slicer edit S1 --section Why --text='New why for S1.' --render"; why.Command != want {
		t.Errorf("command %q, want %q", why.Command, want)
	}
	if check := draftSection(res, "Check"); !check.Unchanged || check.Command != "" {
		t.Errorf("Check: %+v", check)
	}
	if res.Item != "S1" || res.OwnerNeeded != "" {
		t.Errorf("item/owner: %+v", res)
	}
	if rs, err := rig.store.List(); err != nil || len(rs) != 0 {
		t.Errorf("a draft is not a run: %v %v", rs, err)
	}
}

func TestSpecDraftApprovalRunsSlicerEditPerSection(t *testing.T) {
	rig := draftRig(t)
	if _, err := rig.specDraft(t, "S1"); err != nil {
		t.Fatal(err)
	}
	out, err := rig.specDraftApply("S1", "Why", "Implement")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Applied, []string{"Why", "Implement"}) {
		t.Errorf("applied %v", out.Applied)
	}
	reg, err := registry.Load(rig.d.cfg.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	project := reg.Projects[0].Path
	want := [][]string{
		{"edit", "S1", "--section", "Why", "--text=New why for S1.", "--render", "--json", "--lean", "--root", project},
		{"edit", "S1", "--section", "Implement", "--text=1. Do a.", "--render", "--json", "--lean", "--root", project},
	}
	if got := rig.edits(); !reflect.DeepEqual(got, want) {
		t.Fatalf("edits %v, want %v", got, want)
	}
	// The section the owner did not approve was never written.
	for _, e := range rig.edits() {
		if e[3] == "Failing tests" {
			t.Errorf("an unapproved section was edited: %v", e)
		}
	}
	// An applied section is not applied twice.
	if _, err := rig.specDraftApply("S1", "Why"); err == nil || ipc.ErrorOf(err).Code != CodeRefused {
		t.Errorf("a second approval of Why: %v", err)
	}
	if len(rig.edits()) != 2 {
		t.Errorf("a refused approval edited: %v", rig.edits())
	}
	// The third section can still be approved later, on its own.
	if _, err := rig.specDraftApply("S1", "Failing tests"); err != nil || len(rig.edits()) != 3 {
		t.Errorf("the third section: %v, %d edits", err, len(rig.edits()))
	}
}

// The CLI and the TUI approve one section for each call. With real slicer the
// first edit changes `slicer show`, so the second call passes only because the
// first one took the new fingerprint. Here `slicer show S1` answers in turn: the
// spec at the draft, before and after the first edit, and before and after the
// second.
func TestSpecDraftSecondApprovalAfterARealEdit(t *testing.T) {
	spec := func(why string) string {
		return `{"id":"S1","title":"T","slice":{"id":"S1","title":"T","sections":[{"heading":"Why","body":"` + why + `"}]}}`
	}
	seq := []clitest.Reply{{Stdout: spec("v0")}, {Stdout: spec("v0")}, {Stdout: spec("v1")}, {Stdout: spec("v1")}, {Stdout: spec("v2")}}
	rig := draftRig(t, clitest.Route{Tool: "slicer", Args: []string{"show", "S1"}, Prefix: true, Seq: seq})
	if _, err := rig.specDraft(t, "S1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.specDraftApply("S1", "Why"); err != nil {
		t.Fatalf("the first approval: %v", err)
	}
	if _, err := rig.specDraftApply("S1", "Failing tests"); err != nil {
		t.Fatalf("the second approval after a real edit: %v", err)
	}
	if edits := rig.edits(); len(edits) != 2 || edits[0][3] != "Why" || edits[1][3] != "Failing tests" {
		t.Fatalf("edits %v", edits)
	}
}

func TestSpecDraftRefusesAChangedSpec(t *testing.T) {
	rig := draftRig(t)
	if _, err := rig.specDraft(t, "S1"); err != nil {
		t.Fatal(err)
	}
	rig.setSpec(t, "S1", "why S1, changed after the draft")
	_, err := rig.specDraftApply("S1", "Why")
	if err == nil || ipc.ErrorOf(err).Code != CodeRefused || !strings.Contains(err.Error(), "spec changed since the draft") {
		t.Fatalf("approval after a spec change: %v", err)
	}
	if edits := rig.edits(); len(edits) != 0 {
		t.Fatalf("a refused approval edited: %v", edits)
	}

	// Nothing to approve before a draft, and nothing for a section the draft
	// left unchanged or that has a problem.
	rig = draftRig(t)
	if _, err := rig.specDraftApply("S1", "Why"); err == nil || ipc.ErrorOf(err).Code != CodeRefused {
		t.Errorf("approval with no draft: %v", err)
	}
	rig.draft = strings.Replace(draftReportS1, "- `internal/b.go` (new)", "- `internal/gone.go`", 1)
	res, err := rig.specDraft(t, "S1")
	if err != nil {
		t.Fatal(err)
	}
	if files := draftSection(res, "Files"); len(files.Problems) != 1 || !strings.Contains(files.Problems[0], "internal/gone.go") {
		t.Fatalf("Files problems: %+v", files)
	}
	for _, name := range []string{"Files", "Check", "Nope"} {
		if _, err := rig.specDraftApply("S1", name); err == nil || ipc.ErrorOf(err).Code != CodeRefused {
			t.Errorf("approval of %s: %v", name, err)
		}
	}
	if edits := rig.edits(); len(edits) != 0 {
		t.Fatalf("a refused approval edited: %v", edits)
	}
}

// An item with no slice spec has no section to replace; slicer promote makes the
// outline first. The draft is refused, and the role does not run.
func TestSpecDraftRefusesAnItemWithNoSliceSpec(t *testing.T) {
	rig := draftRig(t)
	if err := os.WriteFile(filepath.Join(rig.root, "S2.json"), []byte(`{"id":"S2","title":"T","slice":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := rig.specDraft(t, "S2")
	if err == nil || ipc.ErrorOf(err).Code != CodeRefused || !strings.Contains(err.Error(), "slicer promote S2") {
		t.Fatalf("a draft of an item with no spec: %v", err)
	}
	if len(rig.drafts) != 0 {
		t.Errorf("the role ran for an item with no spec: %d rounds", len(rig.drafts))
	}
}

func TestSpecDraftRoleIsReadOnly(t *testing.T) {
	// Script the git calls; HEAD stays unchanged. Each draft makes five: HEAD, the
	// status twice before the role, the status after it, and HEAD again. The first
	// draft is clean; in the second, the status after the role (the 4th call of
	// the draft, the 9th in all) shows a change.
	seq := make([]clitest.Reply, 15)
	seq[8] = clitest.Reply{Stdout: " M a.go\n"}
	rig := draftRig(t, clitest.Route{Tool: "git", Args: []string{"-C"}, Prefix: true, Seq: seq})
	if _, err := rig.specDraft(t, "S1"); err != nil {
		t.Fatalf("the clean draft: %v", err)
	}
	_, err := rig.specDraft(t, "S1")
	if err == nil || !strings.Contains(err.Error(), "changed the tree") {
		t.Fatalf("error %v", err)
	}
	// The stopped draft keeps no proposal, and the proposal of the draft before it
	// is gone too.
	if _, err := rig.specDraftApply("S1", "Why"); err == nil || ipc.ErrorOf(err).Code != CodeRefused {
		t.Errorf("approval after a failed draft: %v", err)
	}
	if edits := rig.edits(); len(edits) != 0 {
		t.Fatalf("edits after a failed draft: %v", edits)
	}
}
