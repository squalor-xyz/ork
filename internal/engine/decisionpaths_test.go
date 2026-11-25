// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestDecisionPathsReviewerBrief(t *testing.T) {
	for _, tc := range []struct {
		name, source, changed string
		failure               string
	}{
		{"spec answers missing", "spec", "", ""},
		{"spec answers changed", "spec", "landed-changed/S548.md\x00", ""},
		{"spec answers unrelated change", "spec", "landed-changed/S558.md\x00", ""},
		{"run answer missing", "answer", "", ""},
		{"cited number missing", "number", "", ""},
		{"cited key missing", "key", "", ""},
		{"slice owner paragraph", "paragraph", "", ""},
		{"multiple paths", "multiple", "", ""},
		{"diff unavailable", "spec", "", "diff"},
		{"adj unavailable", "number", "", "adj"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.project = func(p *registry.Project) { p.Adj = true }
				o.routesFor = func(P, W string) []clitest.Route {
					raw := sliceJSON()
					if tc.source == "number" {
						raw = strings.Replace(raw, "Because.", "Because (adj 12).", 1)
					}
					if tc.source == "key" {
						raw = strings.Replace(raw, "Because.", "Because [changelog.correct-entry].", 1)
					}
					if tc.source == "paragraph" {
						raw = strings.Replace(raw, "Because.", "Owner decisions (2026-11-25): Edit landed-changed/S548.md.", 1)
					}
					routes := []clitest.Route{
						{Tool: "slicer", Args: []string{"show", item, "--json", "--lean", "--root", W}, Stdout: raw},
						{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: `{"contract":"adj-json v1","version":1}`},
						{Tool: "adj", Args: []string{"show", "12", "--json", "--root", P}, Stdout: `{"id":12,"status":"decided","decision":"Edit landed-changed/S548.md"}`},
						{Tool: "adj", Args: []string{"check", "changelog.correct-entry", "--json", "--root", P}, Stdout: `{"decided":[{"id":12,"key":"changelog.correct-entry","decision":"Edit landed-changed/S548.md"}]}`},
						{Tool: "git", Args: []string{"-C", W, "merge-base", "main", "HEAD"}, Stdout: "base\n"},
						{Tool: "git", Args: []string{"-C", W, "diff", "--name-only", "-z", "--no-renames", "--no-relative", "base"}, Stdout: tc.changed},
						{Tool: "git", Args: []string{"-C", W, "ls-files", "--others", "--exclude-standard", "-z", "--full-name"}},
					}
					if tc.failure == "diff" {
						routes[5].Exit = 1
					}
					if tc.failure == "adj" {
						routes[2].Exit = 1
					}
					return routes
				}
			})
			h.deps.Adj = adjcli.Client{Bin: h.fake.Bin("adj")}
			if tc.source == "spec" {
				h.cfg.Decisions = "Edit `landed-changed/S548.md`, twice: landed-changed/S548.md."
			}
			if tc.source == "multiple" {
				h.cfg.Decisions = "Edit landed-changed/S549.md and landed-changed/S548.md and landed-changed/S549.md."
			}
			e, err := New(context.Background(), h.deps, h.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if tc.source == "answer" {
				if err := e.run.Update(func(r *runrec.Record) {
					r.Questions = []runrec.Question{{Role: RoleImplementer, Text: "Which entry?", Answer: "landed-changed/S548.md"}}
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := e.run.Snapshot().State; got != runrec.StateAwaitingApproval {
				t.Fatalf("advisory check blocked run: %s", got)
			}
			for _, req := range h.runner.requests() {
				_, body, found := strings.Cut(req.Brief, "## Decision changelog paths\n\n")
				if req.Role == RoleReviewer && tc.changed != "landed-changed/S548.md\x00" {
					if !found {
						t.Fatal("reviewer lacks flagged decision path")
					}
					body, _, _ = strings.Cut(body, "\n## ")
					want := "Flagged finding: owner decisions name paths absent from the branch diff against the base:\n\n- `landed-changed/S548.md`\n\nReview these omissions. This check is advisory and does not block the run.\n"
					if tc.source == "multiple" {
						want = strings.Replace(want, "- `landed-changed/S548.md`", "- `landed-changed/S548.md`\n- `landed-changed/S549.md`", 1)
					}
					if tc.failure == "diff" {
						want = "Check incomplete: branch diff against the base could not be read. Review the decision paths yourself; this does not block the run.\n"
					}
					if tc.failure == "adj" {
						want = "Check incomplete: adj 12 could not be read. Review the decision paths yourself; this does not block the run.\n"
					}
					if body != want {
						t.Fatalf("decision path section = %q, want %q", body, want)
					}
				} else if found {
					t.Fatalf("unexpected decision path section for %s (changed %q)", req.Role, tc.changed)
				}
			}
		})
	}
}
