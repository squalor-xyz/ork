// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

type briefGrantPair struct {
	name, sentence, role, grantSite string
	vendors                         []string
	input                           func(role, vendor string) BriefInput
}

// Add a row for each BuildBrief permission sentence whose runner grant applies
// only to some vendors. Cite the runner site that grants the permission.
var briefGrantPairs = []briefGrantPair{
	{
		name:     "Reviewer temp-write",
		sentence: "You may create and change files under your temp directory with Edit or Write",
		role:     RoleReviewer, grantSite: "internal/daemon/runner.go:57",
		vendors: []string{"claude", "claude-local"},
		input: func(role, vendor string) BriefInput {
			in := baseBrief()
			in.Role, in.Vendor, in.ReadOnly = role, vendor, true
			in.TMPDir = in.TempDir + "/process"
			in.ProgressFile = in.TempDir + "/progress.md"
			return in
		},
	},
	{
		name: "UI renders directory", sentence: "The UI renders directory",
		role: RoleUI, grantSite: "internal/daemon/runner.go:49-52",
		vendors: []string{"claude"},
		input: func(role, vendor string) BriefInput {
			in := baseBrief()
			in.Role, in.Vendor, in.ReadOnly = role, vendor, true
			return in
		},
	},
}

func briefGrantViolations(pair briefGrantPair, build func(BriefInput) (string, error)) []error {
	var violations []error
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck, RoleSpecDraft} {
		for _, vendor := range []string{"claude", "claude-local", "codex", "codex-local", "grok", "grok-local"} {
			brief, err := build(pair.input(role, vendor))
			if err != nil {
				violations = append(violations, fmt.Errorf("sentence %q role %s vendor %s grant %s: %w", pair.sentence, role, vendor, pair.grantSite, err))
				continue
			}
			want := role == pair.role && slices.Contains(pair.vendors, vendor)
			present := strings.Contains(brief, pair.sentence)
			if present != want {
				state := "missing"
				if present {
					state = "present"
				}
				violations = append(violations, fmt.Errorf("sentence %q is %s for role %s vendor %s; grant %s", pair.sentence, state, role, vendor, pair.grantSite))
			}
		}
	}
	return violations
}

func TestBriefGrantLintFlagsASentenceGatedOnRoleOnly(t *testing.T) {
	pair := briefGrantPairs[1]
	violations := briefGrantViolations(pair, func(in BriefInput) (string, error) {
		if in.Role == pair.role {
			return pair.sentence, nil
		}
		return "", nil
	})
	if len(violations) != 5 {
		t.Fatalf("got %d violations, want 5: %v", len(violations), violations)
	}
	for i, vendor := range []string{"claude-local", "codex", "codex-local", "grok", "grok-local"} {
		for _, detail := range []string{pair.sentence, pair.role, vendor, pair.grantSite, "present"} {
			if !strings.Contains(violations[i].Error(), detail) {
				t.Errorf("violation %q missing %q", violations[i], detail)
			}
		}
	}
}

func TestBriefGrantLintPassesAVendorGatedSentence(t *testing.T) {
	pair := briefGrantPairs[1]
	build := func(in BriefInput) (string, error) {
		if in.Role == pair.role && slices.Contains(pair.vendors, in.Vendor) {
			return pair.sentence, nil
		}
		return "", nil
	}
	if violations := briefGrantViolations(pair, build); len(violations) != 0 {
		t.Fatalf("vendor-gated sentence: %v", violations)
	}
	violations := briefGrantViolations(pair, func(BriefInput) (string, error) { return "", nil })
	if len(violations) != 1 {
		t.Fatalf("missing sentence: got %d violations, want 1: %v", len(violations), violations)
	}
	for _, detail := range []string{pair.sentence, pair.role, "claude", pair.grantSite, "missing"} {
		if !strings.Contains(violations[0].Error(), detail) {
			t.Errorf("violation %q missing %q", violations[0], detail)
		}
	}
}

func TestBriefRoleGatedSentencesMatchVendorGatedGrants(t *testing.T) {
	for _, pair := range briefGrantPairs {
		t.Run(pair.name, func(t *testing.T) {
			for _, err := range briefGrantViolations(pair, BuildBrief) {
				t.Error(err)
			}
		})
	}
}
