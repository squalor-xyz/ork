// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/roles"
)

// RelayContract is the machine-readable relay contract, read from slicer.
type RelayContract struct {
	Contract string `json:"contract"`
	Pinned   *int   `json:"pinned"`
	Changes  []struct {
		Version int    `json:"version"`
		Text    string `json:"text"`
	} `json:"changes"`
	Roles []struct {
		Role       string `json:"role"`
		LedgerRole string `json:"ledger_role"`
	} `json:"roles"`
	FindingKinds []struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	} `json:"finding_kinds"`
	Gate struct {
		Criteria []string `json:"criteria"`
		Verdicts []string `json:"verdicts"`
	} `json:"gate"`
	Conformance []struct {
		Key  string `json:"key"`
		Text string `json:"text"`
	} `json:"conformance"`
	Outcomes []string `json:"outcomes"`
}

// LedgerRole resolves ork's role to the ledger name supplied by the contract.
func (r RelayContract) LedgerRole(role string) string {
	key := role
	switch role {
	case "ui":
		key = "ui_pass"
	case "spec-check":
		key = "spec_check"
	}
	for _, v := range r.Roles {
		if v.Role == key {
			return v.LedgerRole
		}
	}
	// Gate is absent from v9/v10 roles[]; its ledger round keeps its own name.
	if role == "gate" {
		return "gate"
	}
	return ""
}

// Validate refuses contracts whose data cannot support ork's conformance.
func (r RelayContract) Validate() error {
	version, err := roles.CheckContractLine("Contract: " + r.Contract)
	if err != nil {
		return err
	}
	if r.Pinned != nil && *r.Pinned != version {
		return fmt.Errorf("contract_mismatch: pinned relay v%d; installed %s", *r.Pinned, r.Contract)
	}
	var kinds []string
	for _, k := range r.FindingKinds {
		kinds = append(kinds, k.Kind)
	}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []string{"fix", "later", "owner"}) {
		return fmt.Errorf("relay finding_kinds must be exactly fix, owner, later")
	}
	needed := []string{"role_order", "gate_rule", "finding_kinds", "stop_rules", "ledger", "finish_after_pass", "run_report", "worktree_scope", "commit_scope"}
	if version >= 10 {
		needed = append(needed, "vendor_limit")
	}
	for _, key := range needed {
		if !slices.ContainsFunc(r.Conformance, func(v struct {
			Key  string `json:"key"`
			Text string `json:"text"`
		}) bool {
			return v.Key == key && strings.TrimSpace(v.Text) != ""
		}) {
			return fmt.Errorf("relay conformance missing %s", key)
		}
	}
	if len(r.Gate.Criteria) == 0 || !slices.Contains(r.Gate.Verdicts, "VERDICT: PASS") || !slices.Contains(r.Gate.Verdicts, "VERDICT: FAIL") {
		return fmt.Errorf("relay gate criteria or verdicts missing")
	}
	for _, role := range []string{"orchestrator", "implementer", "reviewer", "architect", "ui", "spec-check"} {
		if r.LedgerRole(role) == "" {
			return fmt.Errorf("relay roles missing ledger_role for %s", role)
		}
	}
	for _, outcome := range []string{"pass", "fail", "stopped"} {
		if !slices.Contains(r.Outcomes, outcome) {
			return fmt.Errorf("relay outcomes missing %s", outcome)
		}
	}
	return nil
}

// RelayContract reads and validates `slicer ai relay --json --root ROOT`.
func (c Client) RelayContract(ctx context.Context, root string) (*RelayContract, error) {
	var out RelayContract
	if err := clirun.RunJSON(ctx, c.call("ai", "relay", "--json", "--root", root), &out); err != nil {
		return nil, err
	}
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return &out, nil
}
