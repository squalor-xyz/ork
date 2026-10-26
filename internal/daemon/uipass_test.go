// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// matrixRow is one row of a recommendation, as slicer prints it.
func matrixRow(id, model, effort string) string {
	return fmt.Sprintf(`{"id":%q,"agent":"Claude Code","vendor":"Claude","model":%q,"reasoning":%q}`, id, model, effort)
}

// uiPlan plans S1 with a matrix whose reviewer role lists rows, and whose
// rules are the given rule texts.
func uiPlan(t *testing.T, implementer, reviewer []string, rules ...string) RunPlan {
	t.Helper()
	choice := func(role string, rows []string) string {
		return fmt.Sprintf(`{"role":%q,"row":%s,"alternatives":[%s]}`, role, rows[0], strings.Join(rows[1:], ","))
	}
	recommend := `{"roles":[` + choice("implementer", implementer) + `,` + choice("reviewer", reviewer) + `,{"role":"architect","row":null,"alternatives":[],"selected_by":"unmatched"}]}`
	var rs []string
	for i, text := range rules {
		rs = append(rs, fmt.Sprintf(`{"id":"u%d","text":%q}`, i+1, text))
	}
	show := `{"rows":[],"rules":[` + strings.Join(rs, ",") + `]}`
	d, _, _ := runDaemon(t, nil,
		clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "show"}, Prefix: true, Stdout: show},
		clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true, Stdout: recommend})
	return launchPlan(t, d, `{"project":"p","item":"S1"}`)
}

const u10 = "A slice that touches the UI also runs the UI pass row (r14) in parallel with the reviewer, not instead of it."

func TestPlanUIPassRowIsNotAReviewerOrGateFallback(t *testing.T) {
	plan := uiPlan(t,
		[]string{matrixRow("r3", "sonnet", "high")},
		[]string{matrixRow("r5", "opus", "medium"), matrixRow("r14", "opus-ui", "low"), matrixRow("r20", "haiku", "high")},
		"A reviewer never wrote code.", u10)
	reviewer := []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}, {Vendor: "claude", Model: "haiku", Effort: "high"}}
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
		if !reflect.DeepEqual(plan.Lineup[role], reviewer) {
			t.Errorf("%s chain %+v, want %+v", role, plan.Lineup[role], reviewer)
		}
	}
	if want := []runrec.LineupEntry{{Vendor: "claude", Model: "opus-ui", Effort: "low"}}; !reflect.DeepEqual(plan.Lineup[engine.RoleUI], want) {
		t.Errorf("ui chain %+v, want %+v", plan.Lineup[engine.RoleUI], want)
	}
	if perm := plan.Permissions[engine.RoleUI]; !perm.ReadOnly || len(perm.Allow) != 0 {
		t.Errorf("ui permission %+v, want read-only", perm)
	}
}

// ork finds the row through the rule that names it, not through the row's
// number or its note.
func TestPlanUIPassRowFollowsTheRule(t *testing.T) {
	plan := uiPlan(t,
		[]string{matrixRow("r3", "sonnet", "high")},
		[]string{matrixRow("r5", "opus", "medium"), matrixRow("r14", "opus-b", "low"), matrixRow("r31", "opus-ui", "low")},
		strings.ReplaceAll(u10, "(r14)", "(r31)"))
	if want := []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}, {Vendor: "claude", Model: "opus-b", Effort: "low"}}; !reflect.DeepEqual(plan.Lineup[engine.RoleReviewer], want) {
		t.Errorf("reviewer chain %+v, want %+v", plan.Lineup[engine.RoleReviewer], want)
	}
	if want := []runrec.LineupEntry{{Vendor: "claude", Model: "opus-ui", Effort: "low"}}; !reflect.DeepEqual(plan.Lineup[engine.RoleUI], want) {
		t.Errorf("ui chain %+v, want %+v", plan.Lineup[engine.RoleUI], want)
	}
}

// With no rule that names a row, no row is the UI pass and the plan has no ui
// role.
func TestPlanWithoutAUIPassRuleHasNoUIRole(t *testing.T) {
	plan := uiPlan(t,
		[]string{matrixRow("r3", "sonnet", "high")},
		[]string{matrixRow("r5", "opus", "medium"), matrixRow("r14", "opus-ui", "low")},
		"A reviewer never wrote code.")
	if _, ok := plan.Lineup[engine.RoleUI]; ok {
		t.Errorf("plan has a ui role: %+v", plan.Lineup[engine.RoleUI])
	}
	if _, ok := plan.Permissions[engine.RoleUI]; ok {
		t.Error("plan has a ui permission")
	}
	if len(plan.Lineup[engine.RoleReviewer]) != 2 {
		t.Errorf("reviewer chain %+v", plan.Lineup[engine.RoleReviewer])
	}
}

// A rule that names a row the matrix does not offer leaves the plan without a
// ui role and takes nothing from the reviewer.
func TestPlanUIPassRuleNamingAMissingRow(t *testing.T) {
	plan := uiPlan(t,
		[]string{matrixRow("r3", "sonnet", "high")},
		[]string{matrixRow("r5", "opus", "medium"), matrixRow("r14", "opus-b", "low")},
		strings.ReplaceAll(u10, "(r14)", "(r99)"))
	if _, ok := plan.Lineup[engine.RoleUI]; ok {
		t.Errorf("plan has a ui role: %+v", plan.Lineup[engine.RoleUI])
	}
	if len(plan.Lineup[engine.RoleReviewer]) != 2 {
		t.Errorf("reviewer chain %+v", plan.Lineup[engine.RoleReviewer])
	}
}

// An entry identical to the one before it is dropped from every chain (S49's
// duplicate-row note); an identical entry that is not next to it stays.
func TestPlanDropsAnEntryIdenticalToThePreviousOne(t *testing.T) {
	plan := uiPlan(t,
		[]string{matrixRow("r3", "sonnet", "high"), matrixRow("r4", "sonnet", "high"), matrixRow("r9", "haiku", "high"), matrixRow("r10", "sonnet", "high")},
		[]string{matrixRow("r5", "opus", "medium"), matrixRow("r6", "opus", "medium")})
	sonnet, haiku := runrec.LineupEntry{Vendor: "claude", Model: "sonnet", Effort: "high"}, runrec.LineupEntry{Vendor: "claude", Model: "haiku", Effort: "high"}
	if want := []runrec.LineupEntry{sonnet, haiku, sonnet}; !reflect.DeepEqual(plan.Lineup[engine.RoleImplementer], want) {
		t.Errorf("implementer chain %+v, want %+v", plan.Lineup[engine.RoleImplementer], want)
	}
	opus := runrec.LineupEntry{Vendor: "claude", Model: "opus", Effort: "medium"}
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
		if want := []runrec.LineupEntry{opus}; !reflect.DeepEqual(plan.Lineup[role], want) {
			t.Errorf("%s chain %+v, want %+v", role, plan.Lineup[role], want)
		}
	}
}
