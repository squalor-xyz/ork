// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestReportDecisionKey(t *testing.T) {
	for _, tc := range []struct {
		body, key string
		bad       bool
	}{
		{"Which?\nDecision key: storage.engine", "storage.engine", false},
		{"Decision key: storage.engine\nWhich?", "storage.engine", false},
		{"Which?", "", false},
		{"none", "", false},
		{"Which?\nDecision key: Storage.engine", "", true},
		{"Which?\nDecision key: storage", "", true},
		{"Which?\nDecision key: storage.engine.extra", "storage.engine.extra", false},
		{"Which?\nDecision key: storage.engine\nDecision key: storage.other", "", true},
	} {
		t.Run(tc.body, func(t *testing.T) {
			rep, err := CheckReport(RoleReviewer, false, "## Owner needed\n"+tc.body)
			if (err != nil) != tc.bad {
				t.Fatalf("report = %+v, %v", rep, err)
			}
			if !tc.bad && rep.Key != tc.key {
				t.Fatalf("key = %q, want %q", rep.Key, tc.key)
			}
		})
	}
}

func TestQuestionTitle(t *testing.T) {
	if got := questionTitle("Decision key: area.topic\n\n" + strings.Repeat("é", 105) + "\nNext"); got != strings.Repeat("é", 100) {
		t.Fatalf("title: %q", got)
	}
}

func TestMalformedDecisionKeyPausesForShape(t *testing.T) {
	h := newHarness(t)
	h.runner.script = func(Request, int) (Result, error) {
		return Result{Exit: ExitOK, Report: withQuestion(implReport) + "\nDecision key: BAD"}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingShape || !strings.Contains(rec.Pending.Reason, "Decision key") || len(rec.Questions) != 0 {
		t.Fatalf("record: %+v", rec)
	}
}

func TestQuestionTitleMarkdown(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"- Which library?\nDecision key: a.b", "Which library?"},
		{"  * Which library?", "Which library?"},
		{"### Which library?", "Which library?"},
		{"> - Which library?", "Which library?"},
		{"- * # >  \nDecision key: a.b\nNext question", "Next question"},
		{"- * # >  \nDecision key: a.b", "Owner question"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			if got := questionTitle(tc.body); got != tc.want {
				t.Fatalf("title = %q; want %q", got, tc.want)
			}
		})
	}
}
