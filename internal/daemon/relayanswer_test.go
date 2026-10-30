// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestDaemonRelayAnswerRefuses(t *testing.T) {
	for _, bad := range []string{"project", "ended", "stale", "run", "question", "answered", "option", "by", "linked", "both", "empty", "via"} {
		t.Run(bad, func(t *testing.T) {
			p := RelayAnswerParams{Adj: 12, Project: "p", Run: "r1", Question: "q1", Option: "Files", By: "Jon", Via: "tui"}
			q := &slicercli.LedgerQuestion{ID: "q1", Options: []runrec.Option{{Label: "Files"}}}
			r := slicercli.RelayRun{ID: "r1", State: "active", Steps: []slicercli.Step{{Question: q}}}
			switch bad {
			case "project":
				p.Project = "unknown"
			case "ended", "stale":
				r.State = bad
			case "run":
				p.Run = "missing"
			case "question":
				p.Question = "missing"
			case "answered":
				q.AnsweredAt = "yesterday"
			case "option":
				p.Option = "wrong"
			case "by":
				p.By = " "
			case "both":
				p.Text = "also"
			case "empty":
				p.Option = ""
			case "via":
				p.Via = "wrong"
			}
			out, _ := json.Marshal(map[string]any{"runs": []slicercli.RelayRun{r}})
			d, store, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"relay-run", "list"}, Prefix: true, Stdout: string(out)})
			if bad == "linked" {
				if _, err := store.Create(runrec.Record{ID: "engine1", Project: "p", RelayRun: "r1", State: "paused"}); err != nil {
					t.Fatal(err)
				}
			}
			raw, _ := json.Marshal(p)
			if _, err := d.handle(context.Background(), "relay.answer", raw); err == nil {
				t.Fatal("accepted invalid answer")
			}
			for _, call := range lastFake.CallsTo("slicer") {
				if len(call.Args) > 1 && call.Args[1] == "answer" {
					t.Fatal("wrote refused answer")
				}
			}
		})
	}
}

func TestDaemonRelayAnswerAlsoAnswersAKeyedOpenAdjRecord(t *testing.T) {
	for _, state := range []string{"open", "decided", "closed", "not found", "no-adj", "adj-error", "check-error"} {
		for _, text := range []bool{false, true} {
			t.Run(state+map[bool]string{false: "/option", true: "/text"}[text], func(t *testing.T) {
				check := "{}"
				if state == "open" || state == "adj-error" {
					check = `{"open":[{"id":12,"key":"storage.engine"}]}`
				} else if state == "decided" || state == "closed" {
					check = `{"` + state + `":[{"id":12,"key":"storage.engine"}]}`
				}
				checkRoute := clitest.Route{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: check}
				if state == "check-error" {
					checkRoute.Exit = 1
					checkRoute.Stderr = "check failed"
				}
				decide := clitest.Route{Tool: "adj", Args: []string{"decide"}, Prefix: true}
				if state == "adj-error" {
					decide.Exit = 1
					decide.Stderr = "disk full"
				}
				d, _, proj := runDaemon(t, nil,
					clitest.Route{Tool: "slicer", Args: []string{"relay-run", "list"}, Prefix: true, Stdout: `{"runs":[{"id":"r1","state":"active","steps":[{"question":{"id":"q1","key":"storage.engine","options":[{"label":"Files"}]}}]}]}`},
					clitest.Route{Tool: "slicer", Args: []string{"relay-run", "answer"}, Prefix: true},
					checkRoute, decide, clitest.Route{Tool: "adj", Args: []string{"show"}, Prefix: true, Stdout: `{"id":12,"status":"open","title":"Storage","options":[{"label":"Files"}]}`})
				proj.Adj = state != "no-adj"
				if err := registry.Save(d.registryPath(), &registry.File{Version: registry.Version, Projects: []registry.Project{proj}}); err != nil {
					t.Fatal(err)
				}
				d.runs.deps.Adj = adjcli.Client{Bin: lastFake.Bin("adj")}
				p := RelayAnswerParams{Adj: 12, Project: "p", Run: "r1", Question: "q1", Option: "Files", By: "Jon", Via: "tui"}
				if text {
					p.Option = ""
					p.Text = "custom"
				}
				raw, _ := json.Marshal(p)
				v, err := d.handle(context.Background(), "relay.answer", raw)
				if err != nil {
					t.Fatal(err)
				}
				msg := v.(*RelayAnswerResult).Message
				if !strings.Contains(msg, "ledger") {
					t.Fatal(msg)
				}
				if state == "open" && !strings.Contains(msg, "#12") {
					t.Fatal(msg)
				}
				if state == "adj-error" && !strings.Contains(msg, "disk full") {
					t.Fatal(msg)
				}
				if state == "decided" || state == "closed" || state == "not found" {
					if !strings.Contains(msg, state) {
						t.Fatal(msg)
					}
				}
				calls := lastFake.CallsTo("adj")
				writes := 0
				for _, c := range calls {
					if c.Args[0] == "decide" {
						writes++
						want := "Files"
						flag := "12"
						if text {
							want = "custom"
							flag = "--text"
						}
						if !strings.Contains(strings.Join(c.Args, " "), flag+" "+want) || !strings.Contains(strings.Join(c.Args, " "), "ork (tui)") {
							t.Fatalf("%v", c.Args)
						}
					}
				}
				if (writes == 1) != (state == "open" || state == "adj-error") {
					t.Fatalf("writes %d", writes)
				}
				ledgerAt, adjAt := -1, -1
				for i, c := range lastFake.Calls() {
					if c.Tool == "slicer" && len(c.Args) > 1 && c.Args[1] == "answer" {
						ledgerAt = i
					}
					if c.Tool == "adj" && c.Args[0] == "decide" {
						adjAt = i
					}
				}
				if ledgerAt < 0 || (adjAt >= 0 && adjAt < ledgerAt) {
					t.Fatal("ledger must be written before adj")
				}
				if d.cfg.Source.(*stubSource).count("runs p") != 1 {
					t.Fatal("ledger cache was not refreshed")
				}

			})
		}
	}
}

func TestDaemonRelayAnswerLedgerFailureDoesNotAnswerAdj(t *testing.T) {
	d, _, _ := runDaemon(t, nil,
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "list"}, Prefix: true, Stdout: `{"runs":[{"id":"r1","state":"active","steps":[{"question":{"id":"q1","key":"storage.engine"}}]}]}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "answer"}, Prefix: true, Exit: 1, Stderr: "ledger write failed"})
	_, err := d.relayAnswer(context.Background(), RelayAnswerParams{Project: "p", Run: "r1", Question: "q1", Text: "custom", By: "Jon", Via: "tui"})
	if err == nil {
		t.Fatal("ledger failure was hidden")
	}
	if len(lastFake.CallsTo("adj")) > 0 {
		t.Fatal("adj called after ledger failure")
	}
	if d.cfg.Source.(*stubSource).count("runs p") != 0 {
		t.Fatal("refreshed failed ledger write")
	}
}

func TestRelayAnswerOnlyWritesConfirmedAdjID(t *testing.T) {
	for _, tc := range []struct {
		name, state      string
		confirmed, found int
		key              string
		usesAdj, writes  bool
		want             string
	}{
		{"unnamed", "open", 0, 12, "storage.engine", true, false, "adj skipped: confirm did not name #12"},
		{"matching", "open", 12, 12, "storage.engine", true, true, "decided in adj as #12"},
		{"different", "open", 12, 13, "storage.engine", true, false, "adj skipped: confirm did not name #13"},
		{"decided", "decided", 12, 12, "storage.engine", true, false, "adj #12 is decided"},
		{"closed", "closed", 12, 12, "storage.engine", true, false, "adj #12 is closed"},
		{"missing", "not found", 12, 0, "storage.engine", true, false, "not found"},
		{"no-key", "open", 0, 12, "", true, false, ""},
		{"no-adj", "open", 0, 12, "storage.engine", false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &slicercli.LedgerQuestion{ID: "q1", Key: tc.key}
			runs, _ := json.Marshal(map[string]any{"runs": []slicercli.RelayRun{{ID: "r1", State: "active", Steps: []slicercli.Step{{Question: q}}}}})
			check := "{}"
			if tc.found > 0 {
				b, _ := json.Marshal(map[string]any{tc.state: []adjcli.Record{{ID: tc.found, Key: "storage.engine"}}})
				check = string(b)
			}
			d, _, proj := runDaemon(t, nil,
				clitest.Route{Tool: "slicer", Args: []string{"relay-run", "list"}, Prefix: true, Stdout: string(runs)},
				clitest.Route{Tool: "slicer", Args: []string{"relay-run", "answer"}, Prefix: true},
				clitest.Route{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: check},
				clitest.Route{Tool: "adj", Args: []string{"show"}, Prefix: true, Stdout: `{"id":12,"status":"open","title":"Storage"}`},
				clitest.Route{Tool: "adj", Args: []string{"decide"}, Prefix: true})
			proj.Adj = tc.usesAdj
			if err := registry.Save(d.registryPath(), &registry.File{Version: registry.Version, Projects: []registry.Project{proj}}); err != nil {
				t.Fatal(err)
			}
			d.runs.deps.Adj = adjcli.Client{Bin: lastFake.Bin("adj")}
			raw, _ := json.Marshal(map[string]any{"project": "p", "run": "r1", "question": "q1", "text": "Files", "by": "Jon", "via": "tui", "adj": tc.confirmed})
			v, err := d.handle(context.Background(), "relay.answer", raw)
			if err != nil {
				t.Fatal(err)
			}
			msg := v.(*RelayAnswerResult).Message
			if tc.want == "" {
				if msg != "answered #q1 on r1 in the ledger" {
					t.Fatal(msg)
				}
			} else if !strings.Contains(msg, tc.want) {
				t.Fatal(msg)
			}
			writes := 0
			for _, c := range lastFake.CallsTo("adj") {
				if c.Args[0] == "decide" {
					writes++
				}
			}
			if (writes == 1) != tc.writes {
				t.Fatalf("writes=%d", writes)
			}
		})
	}
}
