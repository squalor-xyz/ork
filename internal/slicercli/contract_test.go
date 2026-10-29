// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

func TestContractCheckPassesRootAndReportsPinMismatch(t *testing.T) {
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--root", root}, Stdout: `{"error":{"code":"contract_mismatch","message":"pinned relay v9; installed relay v10","command":"ai relay"}}`, Exit: 3})
	_, err := c.Contract(context.Background(), root)
	var tool *clirun.ToolError
	if !errors.As(err, &tool) || tool.Code != "contract_mismatch" || !strings.Contains(err.Error(), "pinned relay v9") {
		t.Fatalf("%v", err)
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

func TestV9ContractDoesNotRequireV10VendorLimitKey(t *testing.T) {
	raw, err := os.ReadFile(recorded("relay-v10.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract RelayContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	contract.Contract = "relay v9"
	for i, c := range contract.Conformance {
		if c.Key == "vendor_limit" {
			contract.Conformance = append(contract.Conformance[:i], contract.Conformance[i+1:]...)
			break
		}
	}
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
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
