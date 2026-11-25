// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"reflect"
	"testing"

	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestWhyContainsTitle(t *testing.T) {
	for _, tc := range []struct {
		name, why, title string
		want             bool
	}{
		{"boundary", "alpha beta gamma extra context", "alpha beta gamma delta epsilon", true},
		{"below", "alpha beta extra context", "alpha beta gamma delta epsilon", false},
		{"normalization", "ALPHA, beta-beta gamma! unrelated prose", "the alpha beta gamma delta epsilon", true},
		{"empty title", "alpha beta", "the and a", false},
		{"empty why", "", "alpha beta", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := whyContainsTitle(tc.why, tc.title); got != tc.want {
				t.Fatalf("whyContainsTitle = %v, want %v", got, tc.want)
			}
		})
	}
}

// Historical proposals read through slicer show; S418's original wording is
// preserved in its Why after its title changed to the decided implementation.
const (
	near419 = `Treat a "prints nothing" Check line that is a no-match rg (exit 1) as passing, or read the output and not the exit code`
	near438 = `judge a Check line that says "prints nothing" by its output, and treat rg exit 1 (no match) as pass`
	near426 = `give the UI pass a way to capture CLI output, such as a module copy in the temp directory or a render hook, because hand-traced renders can drift from real output`
	near429 = `give the UI pass a way to capture real CLI output, such as a module copy in its temp directory, so renders are not traced by hand`
	near418 = `Say in the reviewer prompt that the harness may start the reviewer in plan mode and that the report then goes in the final message`
	near436 = `say in the reviewer brief whether the harness may start the role in plan mode, since plan-mode instructions conflict with the report-as-final-message rule`
	near423 = `say in the brief whether the reviewer may write the report path, instead of giving both "write your report to PATH" and "you cannot write it"`
)

func TestNearDuplicateTitlesFindsTheMeasuredPairs(t *testing.T) {
	items := []slicercli.Item{{ID: "S419", Title: near419, Status: "open"}, {ID: "S426", Title: near426, Status: "open"}, {ID: "S418", Title: near418, Status: "open"}}
	for _, tc := range []struct{ title, want string }{{near438, "S419"}, {near429, "S426"}, {near436, "S418"}} {
		t.Run(tc.want, func(t *testing.T) {
			if got := NearDuplicateTitles(tc.title, "NEW", items, 0.5); !reflect.DeepEqual(got, []string{tc.want}) {
				t.Fatalf("near = %v, want [%s]", got, tc.want)
			}
		})
	}
}

func TestNearDuplicateTitlesIgnoresOtherFamilies(t *testing.T) {
	for _, tc := range []struct{ name, id, title, status string }{
		{"other family", "S423", near423, "open"},
		{"self", "NEW", near436, "open"},
		{"done", "S418", near436, "done"},
		{"retired", "S418", near436, "retired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NearDuplicateTitles(near436, "NEW", []slicercli.Item{{ID: tc.id, Title: tc.title, Status: tc.status}}, 0.5); len(got) != 0 {
				t.Fatalf("near = %v, want none", got)
			}
		})
	}
}

func TestNearDuplicateTitlesIsCaseAndPunctuationBlind(t *testing.T) {
	if got := NearDuplicateTitles("Treat a `prints nothing` line", "NEW", []slicercli.Item{{ID: "S419", Title: "treat a prints-nothing line", Status: "open"}}, 1); !reflect.DeepEqual(got, []string{"S419"}) {
		t.Fatalf("near = %v, want [S419]", got)
	}
}
