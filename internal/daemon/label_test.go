// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import "testing"

// StripTaskLabel is the one label cut for the Running row (S145) and for
// ork status (S150).
func TestStripTaskLabel(t *testing.T) {
	for _, tc := range []struct {
		items []string
		task  string
		role  string
		want  string
	}{
		{[]string{"S52"}, "s52 implementer", "implementer", ""},
		{[]string{"S52"}, "s52/implementer", "implementer", ""},
		{[]string{"S52"}, "s52 implementer fix round 1", "implementer", "fix round 1"},
		{[]string{"S52"}, "s52/implementer/ fix round 1", "implementer", "fix round 1"},
		{[]string{"S52"}, "S52-Implementer: finish", "implementer", "finish"},
		{[]string{"S52", "S53"}, "s52,s53 implementer fix round 1", "implementer", "fix round 1"},
		{[]string{"S52"}, "s52 reviewer", "implementer", "s52 reviewer"},
		{[]string{"S52"}, "s52 implementerx", "implementer", "s52 implementerx"},
		{[]string{"S52"}, "Implement S52 in the worktree", "implementer", "Implement S52 in the worktree"},
	} {
		if got := StripTaskLabel(tc.task, tc.items, tc.role); got != tc.want {
			t.Errorf("StripTaskLabel(%q, %v, %q) = %q, want %q", tc.task, tc.items, tc.role, got, tc.want)
		}
	}
}

// The label pattern is compiled once for a given id and role, not on every
// render (S150).
func TestLabelPatternIsCompiledOnce(t *testing.T) {
	a := labelPattern([]string{"S52", "implementer"})
	b := labelPattern([]string{"S52", "implementer"})
	if a != b {
		t.Fatal("the same words compiled a second pattern")
	}
	if c := labelPattern([]string{"S53", "implementer"}); c == a {
		t.Fatal("different words share a pattern")
	}
}
