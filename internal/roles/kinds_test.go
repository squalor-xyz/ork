// SPDX-License-Identifier: GPL-3.0-or-later

package roles

import (
	"strings"
	"testing"
)

// The reviewer, the UI pass and the gate tag each finding with its kind
// (adj 163, review.finding-kinds); ork sorts the report by the tags.
func TestReportingRolesTagFindingKinds(t *testing.T) {
	ps := prompts(t)
	for _, name := range []string{"reviewer", "ui", "gate"} {
		p, ok := ps[name]
		if !ok {
			t.Fatalf("no %s prompt", name)
		}
		if !strings.Contains(p, "## Finding kinds") {
			t.Errorf("%s has no Finding kinds section", name)
			continue
		}
		for _, want := range []string{"`[fix]`", "`[owner]`", "`[owner blocker]`", "`[later]`", "A finding without a tag counts as `[fix]`"} {
			if !has(p, want) {
				t.Errorf("%s: Finding kinds lacks %s", name, want)
			}
		}
	}
	for _, name := range []string{"implementer", "architect"} {
		if strings.Contains(ps[name], "## Finding kinds") {
			t.Errorf("%s does not tag findings; its report has no kinds", name)
		}
	}
}
