// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"os"
	"strings"
	"testing"
)

func TestClaudeConnection(t *testing.T) {
	ev, err := ParseLine(readFixture(t, "connection", "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	got, reason := Classify(1, ev.Result)
	if got != Class("connection") || reason != "api_error connection_failed" {
		t.Fatalf("connection class/reason: %s / %q", got, reason)
	}
}

func TestConnectionFixturesSanitized(t *testing.T) {
	for _, path := range []string{"../../../testdata/vendors/claude/connection.stdout", "../../../testdata/vendors/grok/connection.stderr"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(data)), "credential") {
			t.Errorf("credential text in %s", path)
		}
	}
}
