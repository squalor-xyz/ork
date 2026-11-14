// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

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
