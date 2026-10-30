// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
)

func recordDeps(t *testing.T, status string) (Deps, *clitest.Fake) {
	t.Helper()
	f := clitest.Install(t, clitest.Route{Tool: "adj", Args: []string{"show"}, Prefix: true, Stdout: fmt.Sprintf(`{"id":12,"title":"Which?","status":%q,"options":[{"label":"a","summary":"A"},{"label":"b","summary":"B"}]}`, status)}, clitest.Route{Tool: "adj", Args: []string{"decide"}, Prefix: true})
	return Deps{Adj: adjcli.Client{Bin: f.Bin("adj")}, Now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }}, f
}
func TestAnswerRecordPicksAnOptionOrTypesText(t *testing.T) {
	for _, via := range []string{"cli", "tui"} {
		for _, option := range []bool{true, false} {
			t.Run(fmt.Sprint(via, option), func(t *testing.T) {
				d, f := recordDeps(t, "open")
				c := AnswerRecordConfig{Project: registry.Project{Path: t.TempDir()}, ID: 12, By: "Jon", Via: via}
				want := []string{"decide", "12"}
				if option {
					c.Option = "a"
					want = append(want, "a")
				} else {
					c.Text = "typed"
					want = append(want, "--text", "typed")
				}
				want = append(want, "--why", "Answered in ork ("+via+") on 2026-10-30: Which?", "--by", "Jon", "--root", c.Project.Path)
				msg, err := AnswerRecord(context.Background(), d, c)
				if err != nil || msg != "decided in adj as #12" {
					t.Fatalf("%s %v", msg, err)
				}
				calls := f.CallsTo("adj")
				if len(calls) != 2 || fmt.Sprint(calls[1].Args) != fmt.Sprint(want) {
					t.Fatalf("calls %+v want %q", calls, want)
				}
			})
		}
	}
}
func TestAnswerRecordRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		change       func(*AnswerRecordConfig)
	}{
		{"via", "open", func(c *AnswerRecordConfig) { c.Via = "other" }}, {"empty-via", "open", func(c *AnswerRecordConfig) { c.Via = "" }}, {"by", "open", func(c *AnswerRecordConfig) { c.By = " " }}, {"both", "open", func(c *AnswerRecordConfig) { c.Text = "text" }}, {"neither", "open", func(c *AnswerRecordConfig) { c.Option = "" }}, {"label", "open", func(c *AnswerRecordConfig) { c.Option = "A" }}, {"decided", "decided", func(c *AnswerRecordConfig) {}}, {"closed", "rejected", func(c *AnswerRecordConfig) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, f := recordDeps(t, tc.status)
			c := AnswerRecordConfig{Project: registry.Project{Path: t.TempDir()}, ID: 12, Option: "a", By: "Jon", Via: "cli"}
			tc.change(&c)
			_, err := AnswerRecord(context.Background(), d, c)
			if err == nil {
				t.Fatal("accepted")
			}
			if tc.status != "open" && !strings.Contains(err.Error(), tc.status) {
				t.Fatal(err)
			}
			if (tc.name == "via" || tc.name == "empty-via") && len(f.Calls()) != 0 {
				t.Fatal("read adj before refusing invalid via")
			}
			for _, call := range f.Calls() {
				if call.Args[0] == "decide" {
					t.Fatal("decided")
				}
			}
		})
	}
}
func TestAskRecordChecksTheKeyBeforeAsking(t *testing.T) {
	for _, state := range []string{"decided", "open", "closed", "not found", "no key"} {
		t.Run(state, func(t *testing.T) {
			out := `{}`
			if state != "not found" && state != "no key" {
				out = fmt.Sprintf(`{%q:[{"id":9,"key":"a.b"}]}`, state)
			}
			f := clitest.Install(t, clitest.Route{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: out}, clitest.Route{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "12\n"})
			c := AskRecordConfig{Project: registry.Project{Path: t.TempDir()}, Spec: adjcli.AskSpec{Title: "Title", Key: "a.b"}}
			if state == "no key" {
				c.Spec.Key = ""
			}
			id, err := AskRecord(context.Background(), Deps{Adj: adjcli.Client{Bin: f.Bin("adj")}}, c)
			calls := f.Calls()
			if state == "not found" || state == "no key" {
				if err != nil || id != 12 {
					t.Fatalf("%d %v", id, err)
				}
				want := 2
				if state == "no key" {
					want = 1
				}
				if len(calls) != want || calls[len(calls)-1].Args[0] != "ask" {
					t.Fatalf("calls %+v", calls)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "#9") || !strings.Contains(err.Error(), state) || len(calls) != 1 {
					t.Fatalf("%v calls %+v", err, calls)
				}
			}
		})
	}
}
func TestAskRecordRefusals(t *testing.T) {
	for _, change := range []func(*adjcli.AskSpec){func(s *adjcli.AskSpec) { s.Title = " " }, func(s *adjcli.AskSpec) { s.Options = make([]adjcli.AskOption, 1) }, func(s *adjcli.AskSpec) { s.Options = make([]adjcli.AskOption, 5) }, func(s *adjcli.AskSpec) { s.Recommend = "missing" }, func(s *adjcli.AskSpec) { s.Reasoning = "why" }} {
		f := clitest.Install(t)
		s := adjcli.AskSpec{Title: "Title"}
		change(&s)
		_, err := AskRecord(context.Background(), Deps{Adj: adjcli.Client{Bin: f.Bin("adj")}}, AskRecordConfig{Project: registry.Project{Path: t.TempDir()}, Spec: s})
		if err == nil || len(f.Calls()) != 0 {
			t.Fatalf("%v calls %+v", err, f.Calls())
		}
	}
}

func TestAskRecordRefusesLeadingDashTitle(t *testing.T) {
	for _, title := range []string{"- title", "-h", "-"} {
		t.Run(title, func(t *testing.T) {
			f := clitest.Install(t)
			_, err := AskRecord(context.Background(), Deps{Adj: adjcli.Client{Bin: f.Bin("adj")}}, AskRecordConfig{Project: registry.Project{Path: t.TempDir()}, Spec: adjcli.AskSpec{Title: title, Key: "a.b"}})
			if err == nil || !strings.Contains(err.Error(), "title must not start with '-'") {
				t.Fatalf("error %v", err)
			}
			if len(f.Calls()) != 0 {
				t.Fatalf("called adj: %+v", f.Calls())
			}
		})
	}
}
