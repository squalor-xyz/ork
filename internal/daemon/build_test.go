// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import "testing"

func TestStaleBuild(t *testing.T) {
	for _, tc := range []struct {
		own, theirs string
		want        bool
	}{
		{"a", "a", false}, {"a", "b", true}, {"", "dev", false}, {"dev", "", true}, {"unknown", "unknown", true}, {"a", "unknown", true}, {"", "", true},
	} {
		if got := StaleBuild(tc.own, tc.theirs); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}
