// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"reflect"
	"testing"
)

func TestArgsLocalReadOnlyUsesAllowlist(t *testing.T) {
	for _, vendor := range []string{"claude-local", "claude"} {
		t.Run(vendor, func(t *testing.T) {
			got, err := (Spec{Bin: vendor, Prompt: "review", ReadOnly: true, Allowlist: []string{"Read", "Grep", "Glob"}}).Args()
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
			if vendor == "claude-local" {
				want = append(want, "--allowedTools", "Read,Grep,Glob", "--permission-prompts", "none")
			} else {
				want = append(want, "--permission-mode", "plan")
			}
			want = append(want, "review")
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("read-only argv: got %q; want %q", got, want)
			}
		})
	}
}
