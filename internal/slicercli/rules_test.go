// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"context"
	"reflect"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
)

func TestRelayRules(t *testing.T) {
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "show", "--json", "--root", root},
		Stdout: `{"relay_roles":"# Relay roles","rows":[{"id":"r1"}],"rules":[{"id":"u1","text":"A reviewer never wrote code."},{"id":"u10","text":"A slice that touches the UI also runs the UI pass row (r14) in parallel."}]}`})
	rules, err := c.RelayRules(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{{ID: "u1", Text: "A reviewer never wrote code."}, {ID: "u10", Text: "A slice that touches the UI also runs the UI pass row (r14) in parallel."}}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules %+v, want %+v", rules, want)
	}
	if got := fake.CallsTo("slicer")[0].Args; !reflect.DeepEqual(got, []string{"relay-roles", "show", "--json", "--root", root}) {
		t.Fatalf("args %v", got)
	}
}

func TestUIPassRowID(t *testing.T) {
	for name, tc := range map[string]struct {
		rules []Rule
		want  string
	}{
		"u10 today":      {[]Rule{{ID: "u9", Text: "no"}, {ID: "u10", Text: "A slice that touches the UI also runs the UI pass row (r14) in parallel with the reviewer."}}, "r14"},
		"another number": {[]Rule{{ID: "u11", Text: "The UI pass row (r203) runs beside the reviewer."}}, "r203"},
		"no rule":        {[]Rule{{ID: "u1", Text: "A reviewer never wrote code."}}, ""},
		"row not named":  {[]Rule{{ID: "u10", Text: "The UI pass row runs beside the reviewer."}}, ""},
		"other row":      {[]Rule{{ID: "u10", Text: "The gate row (r6) is separate. The reviewer row (r5)."}}, ""},
		"none":           {nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := UIPassRowID(tc.rules); got != tc.want {
				t.Fatalf("UIPassRowID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSpecCheckRowID(t *testing.T) {
	for name, tc := range map[string]struct {
		rules []Rule
		want  string
	}{
		"u11 today":     {[]Rule{{ID: "u10", Text: "The UI pass row (r14) runs beside the reviewer."}, {ID: "u11", Text: "The spec check before a wave runs the Spec check row (r16): one read-only pass."}}, "r16"},
		"another row":   {[]Rule{{ID: "u12", Text: "Spec check row (r203) runs first."}}, "r203"},
		"no rule":       {[]Rule{{ID: "u1", Text: "A reviewer never wrote code."}}, ""},
		"row not named": {[]Rule{{ID: "u11", Text: "The Spec check row runs before a wave."}}, ""},
		"ui rule only":  {[]Rule{{ID: "u10", Text: "The UI pass row (r14) runs beside the reviewer."}}, ""},
		"none":          {nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := SpecCheckRowID(tc.rules); got != tc.want {
				t.Fatalf("SpecCheckRowID = %q, want %q", got, tc.want)
			}
		})
	}
}
