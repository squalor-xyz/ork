// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/agent/claude"
)

// A deferred call prints no permission decision, so the CLI's own allowlist
// decides it; an unknown decision still fails closed.
func TestHookOutput(t *testing.T) {
	for _, tc := range []struct {
		in       claude.Approval
		decision string // "" when the output holds no permissionDecision
	}{
		{claude.Approval{Decision: "allow", Reason: "r"}, "allow"},
		{claude.Approval{Decision: "deny", Reason: "r"}, "deny"},
		{claude.Approval{Decision: claude.DecisionDefer, Reason: "r"}, ""},
		{claude.Approval{Decision: "ask"}, "deny"},
	} {
		t.Run(tc.in.Decision, func(t *testing.T) {
			var got struct {
				Hook *struct {
					Decision string `json:"permissionDecision"`
				} `json:"hookSpecificOutput"`
			}
			raw, err := json.Marshal(hookOutput(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if tc.decision == "" && !(got.Hook == nil || got.Hook.Decision == "") || tc.decision != "" && (got.Hook == nil || got.Hook.Decision != tc.decision) {
				t.Fatalf("hook output %s, want decision %q", raw, tc.decision)
			}
			if tc.decision == "" && strings.Contains(string(raw), "permissionDecision") {
				t.Fatalf("deferred call printed a decision: %s", raw)
			}
		})
	}
}

func TestHookPretool(t *testing.T) {
	var output bytes.Buffer
	code := hookPretool(context.Background(), nil, strings.NewReader(`{"tool_name":"Read","tool_input":{"file_path":"/outside"}}`), &output)
	var got struct {
		Hook struct {
			Event    string `json:"hookEventName"`
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if code != 0 || got.Hook.Event != "PreToolUse" || got.Hook.Decision != "deny" || got.Hook.Reason == "" {
		t.Fatalf("hook failed open: code %d, %s", code, output.String())
	}
	output.Reset()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	oldInput := os.Stdin
	input, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	os.Stdin = input
	defer func() { os.Stdin = oldInput }()
	code = run(context.Background(), []string{"hook", "pretool"}, &output, &bytes.Buffer{}, program{})
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("hook route did not emit JSON: %v", err)
	}
	if code != 0 || got.Hook.Decision != "deny" {
		t.Fatalf("hook route failed open: %d %s", code, output.String())
	}
}
