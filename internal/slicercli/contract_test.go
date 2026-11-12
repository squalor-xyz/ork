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
	raw, err := os.ReadFile(recorded("relay-v12.json"))
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"", "finding_kinds", "gate", "roles", "outcomes", "role_order", "gate_rule", "stop_rules", "ledger", "finish_after_pass", "run_report", "worktree_scope", "commit_scope", "conformance/finding_kinds"} {
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
				if err != nil || got.Contract != "relay v12" || got.LedgerRole("ui") != "ui" || len(got.Gate.Criteria) != 5 {
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
	if !errors.As(err, &tool) || tool.Code != "contract_mismatch" || !strings.Contains(err.Error(), "pins relay v11") || !strings.Contains(err.Error(), "serves relay v12") {
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
	raw, err := os.ReadFile(recorded("relay-v12.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []string{"12", "9"} {
		body := strings.Replace(string(raw), `"pinned": null`, `"pinned": `+pin, 1)
		c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", root}, Stdout: body})
		got, err := c.RelayContract(context.Background(), root)
		if pin == "12" {
			if err != nil || got.Pinned == nil || *got.Pinned != 12 {
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
}

// Recorded v12 JSON validates and decodes tool_feedback (slicer 1.21.0, s453);
// the conformance keys and roles[] are v11's.
func TestRelayContractV12Validate(t *testing.T) {
	got := editContract(t, "relay-v12.json", func(map[string]any) {})
	if err := got.Validate(); err != nil {
		t.Fatalf("recorded v12 JSON refused: %v", err)
	}
	tf := got.ToolFeedback
	if tf == nil || tf.MaxEntries != 3 || !slices.Equal(tf.Kinds, []string{"friction", "feature", "deterministic-check"}) || !slices.Equal(tf.Fields, []string{"tool", "kind", "proposal", "evidence"}) {
		t.Fatalf("tool_feedback: %+v", tf)
	}
	if err := editContract(t, "relay-v12.json", func(g map[string]any) { delete(g, "tool_feedback") }).Validate(); err == nil || !strings.Contains(err.Error(), "tool_feedback") {
		t.Fatalf("v12 without tool_feedback: %v", err)
	}
	for name, edit := range map[string]func(map[string]any){
		"no gate in roles[]": func(g map[string]any) { dropEntry(g, "roles", "role", "gate") },
		"no role_fallback":   func(g map[string]any) { dropEntry(g, "conformance", "key", "role_fallback") },
	} {
		if err := editContract(t, "relay-v12.json", edit).Validate(); err == nil {
			t.Errorf("v12 %s accepted", name)
		}
	}
}

// v11 needs no tool_feedback key.
func TestRelayContractV11NeedsNoToolFeedback(t *testing.T) {
	got := editContract(t, "relay-v11.json", func(map[string]any) {})
	if got.ToolFeedback != nil || got.Validate() != nil {
		t.Fatalf("%+v: %v", got.ToolFeedback, got.Validate())
	}
}

// v10 and v13 are outside the accepted set {11, 12}.
func TestRelayContractV10AndV13AreRefused(t *testing.T) {
	for _, v := range []string{"relay v10", "relay v13"} {
		err := editContract(t, "relay-v12.json", func(g map[string]any) { g["contract"] = v }).Validate()
		if err == nil || !strings.Contains(err.Error(), "relay v11 or v12 and slicer prints "+strings.TrimPrefix(v, "relay ")) {
			t.Errorf("%s: %v", v, err)
		}
	}
}

func TestRelayContractV9IsRefused(t *testing.T) {
	got := editContract(t, "relay-v12.json", func(g map[string]any) { g["contract"] = "relay v9" })
	err := got.Validate()
	if err == nil || !strings.Contains(err.Error(), "v11 or v12") {
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
	// A contract without the gate entry has no ledger role for it (v10's
	// fallback to "gate" is gone).
	none := editContract(t, "relay-v12.json", func(g map[string]any) { dropEntry(g, "roles", "role", "gate") })
	if got := none.LedgerRole("gate"); got != "" {
		t.Fatalf("no gate entry: gate = %q", got)
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
