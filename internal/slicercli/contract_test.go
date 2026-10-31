// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/clitest"
)

func TestRelayContractJSONConformance(t *testing.T) {
	raw, err := os.ReadFile(recorded("relay-v10.json"))
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"", "finding_kinds", "gate", "roles", "outcomes", "role_order", "gate_rule", "stop_rules", "vendor_limit", "ledger", "finish_after_pass", "run_report", "worktree_scope", "commit_scope", "conformance/finding_kinds"} {
		t.Run(missing, func(t *testing.T) {
			var body map[string]json.RawMessage
			json.Unmarshal(raw, &body)
			if _, ok := body[missing]; ok {
				delete(body, missing)
			} else if missing != "" {
				var conf []struct {
					Key  string `json:"key"`
					Text string `json:"text"`
				}
				json.Unmarshal(body["conformance"], &conf)
				for i, c := range conf {
					if c.Key == strings.TrimPrefix(missing, "conformance/") {
						conf = append(conf[:i], conf[i+1:]...)
						break
					}
				}
				body["conformance"], _ = json.Marshal(conf)
			}
			data, _ := json.Marshal(body)
			c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", root}, Stdout: string(data)})
			got, err := c.RelayContract(context.Background(), root)
			if missing == "" {
				if err != nil || got.Contract != "relay v10" || got.LedgerRole("ui") != "ui" || len(got.Gate.Criteria) != 5 {
					t.Fatalf("%+v: %v", got, err)
				}
			} else if err == nil {
				t.Fatalf("accepted missing %s", missing)
			}
		})
	}
}

// The recorded refusal of a slicer that serves v11 on a project pinned at v10
// (slicer 1.17.0, exit 2, error object on stdout).
func TestContractCheckPassesRootAndReportsPinMismatch(t *testing.T) {
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", root}, File: recorded("error-contract-mismatch.json"), Exit: 2})
	_, err := c.Contract(context.Background(), root)
	var tool *clirun.ToolError
	if !errors.As(err, &tool) || tool.Code != "contract_mismatch" || !strings.Contains(err.Error(), "pins relay v10") || !strings.Contains(err.Error(), "serves relay v11") {
		t.Fatalf("%v", err)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 || calls[0].Args[len(calls[0].Args)-1] != root {
		t.Fatalf("calls: %+v", calls)
	}
}

// ReadContract decodes without validating, so a caller can read the pin of a
// contract that Validate refuses.
func TestReadContractKeepsThePinOfARefusedContract(t *testing.T) {
	raw, err := os.ReadFile(recorded("relay-v11-pinned.json"))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(raw), `"pinned": 11`, `"pinned": 9`, 1)
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", root}, Stdout: body})
	got, err := c.ReadContract(context.Background(), root)
	if err != nil || got.Pinned == nil || *got.Pinned != 9 {
		t.Fatalf("%+v, %v", got, err)
	}
	if err := got.Validate(); err == nil {
		t.Fatal("Validate accepted pin 9 on relay v11")
	}
}

func TestRelayContractDecodesNumericPin(t *testing.T) {
	raw, err := os.ReadFile(recorded("relay-v10.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []string{"10", "9"} {
		body := strings.Replace(string(raw), `"pinned": null`, `"pinned": `+pin, 1)
		c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", root}, Stdout: body})
		got, err := c.RelayContract(context.Background(), root)
		if pin == "10" {
			if err != nil || got.Pinned == nil || *got.Pinned != 10 {
				t.Fatalf("%+v, %v", got, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "pinned relay v9") {
			t.Fatalf("%v", err)
		}
	}
}

// editContract decodes a recorded contract file, applies edit to its generic
// form and returns the typed result.
func editContract(t *testing.T, file string, edit func(map[string]any)) RelayContract {
	t.Helper()
	raw, err := os.ReadFile(recorded(file))
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	edit(generic)
	data, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	var out RelayContract
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return out
}

// dropEntry removes the entries of a generic list whose field equals value.
func dropEntry(generic map[string]any, list, field, value string) {
	var kept []any
	for _, e := range generic[list].([]any) {
		if e.(map[string]any)[field] != value {
			kept = append(kept, e)
		}
	}
	generic[list] = kept
}

// v11 renames the conformance key vendor_limit to role_fallback, adds
// blocker_wait and puts the gate in roles[] (slicer s424).
func TestRelayContractV11Validate(t *testing.T) {
	if got := editContract(t, "relay-v11.json", func(map[string]any) {}); got.Validate() != nil {
		t.Fatalf("recorded v11 JSON refused: %v", got.Validate())
	}
	for name, edit := range map[string]func(map[string]any){
		"no gate in roles[]": func(g map[string]any) { dropEntry(g, "roles", "role", "gate") },
		"no role_fallback":   func(g map[string]any) { dropEntry(g, "conformance", "key", "role_fallback") },
		"no blocker_wait":    func(g map[string]any) { dropEntry(g, "conformance", "key", "blocker_wait") },
		"v10 key vendor_limit": func(g map[string]any) {
			dropEntry(g, "conformance", "key", "role_fallback")
			g["conformance"] = append(g["conformance"].([]any), map[string]any{"key": "vendor_limit", "text": "x"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := editContract(t, "relay-v11.json", edit).Validate()
			if err == nil {
				t.Fatal("accepted")
			}
		})
	}
	// stop[] keys are decoded, not required (owner decision 2026-10-30).
	if err := editContract(t, "relay-v11.json", func(g map[string]any) { g["stop"] = []any{"plain"} }).Validate(); err != nil {
		t.Fatalf("v11 with plain stop[] strings refused: %v", err)
	}
}

// Recorded v10 JSON keeps validating, with plain-string stop[] and vendor_limit.
func TestRelayContractV10StillValidates(t *testing.T) {
	got := editContract(t, "relay-v10.json", func(map[string]any) {})
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(got.Stop) != 3 || got.Stop[0].Key != "" || got.Stop[0].Text != "the same slice fails its gate three times" {
		t.Fatalf("v10 stop[]: %+v", got.Stop)
	}
	if err := editContract(t, "relay-v10.json", func(g map[string]any) { dropEntry(g, "conformance", "key", "vendor_limit") }).Validate(); err == nil {
		t.Fatal("v10 without vendor_limit accepted")
	}
}

func TestRelayContractV9IsRefused(t *testing.T) {
	got := editContract(t, "relay-v10.json", func(g map[string]any) { g["contract"] = "relay v9" })
	err := got.Validate()
	if err == nil || !strings.Contains(err.Error(), "v10, v11") {
		t.Fatalf("v9: %v", err)
	}
}

func TestRelayContractV11DecodesStopKeys(t *testing.T) {
	got := editContract(t, "relay-v11.json", func(map[string]any) {})
	var keys []string
	for _, s := range got.Stop {
		if strings.TrimSpace(s.Text) == "" {
			t.Errorf("stop %q has no text", s.Key)
		}
		keys = append(keys, s.Key)
	}
	if want := []string{"gate_fail_3", "spec_wrong", "permission_denied", "no_eligible_row"}; !slices.Equal(keys, want) {
		t.Fatalf("stop keys %v, want %v", keys, want)
	}
}

func TestLedgerRoleGate(t *testing.T) {
	v11 := editContract(t, "relay-v11.json", func(map[string]any) {})
	if got := v11.LedgerRole("gate"); got != "gate" {
		t.Fatalf("v11 gate = %q", got)
	}
	// The v11 contract's own ledger_role is read, not assumed.
	renamed := editContract(t, "relay-v11.json", func(g map[string]any) {
		for _, r := range g["roles"].([]any) {
			if m := r.(map[string]any); m["role"] == "gate" {
				m["ledger_role"] = "judge"
			}
		}
	})
	if got := renamed.LedgerRole("gate"); got != "judge" {
		t.Fatalf("renamed gate = %q, want the contract's ledger_role", got)
	}
	v10 := editContract(t, "relay-v10.json", func(map[string]any) {})
	if got := v10.LedgerRole("gate"); got != "gate" {
		t.Fatalf("v10 gate = %q", got)
	}
}

func TestRelayRunsWaitDecodesQuestionObject(t *testing.T) {
	c, _ := client(t, route("relay-run-wait-v10.json", "relay-run", "list", "--json", "--lean", "--active", "--root", root))
	runs, err := c.RelayRuns(context.Background(), root, true)
	if err != nil || len(runs) != 1 {
		t.Fatalf("%+v: %v", runs, err)
	}
	run := runs[0]
	if run.Counts.Waiting != 1 || run.Counts.Running != 0 || len(run.Steps) != 1 {
		t.Fatalf("%+v", run)
	}
	step := run.Steps[0]
	if step.Phase != "wait" || step.WaitingOn != "owner" || step.Question == nil || step.Question.ID != "q1" || len(step.Question.Options) != 2 || !step.Question.Blocker || step.Question.Recommend != "four" {
		t.Fatalf("%+v", step)
	}
}
