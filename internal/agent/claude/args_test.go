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
				want = append(want, "--disallowedTools", localDisallowed, "--disable-slash-commands", "--allowedTools", "Read,Grep,Glob", "--permission-prompts", "none")
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

// localDisallowed is the tool list a local role never uses (S360, *proposed*).
const localDisallowed = "WebFetch,WebSearch,Task,Agent,NotebookEdit,Skill"

func TestArgsLocalWriteRoleTurnsOffUnusedTools(t *testing.T) {
	got, err := (Spec{Bin: "claude-local", Prompt: "implement", Allowlist: []string{"Read", "Edit"}}).Args()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--disallowedTools", localDisallowed, "--disable-slash-commands",
		"--allowedTools", "Read,Edit", "--permission-prompts", "none", "implement"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("local implementer argv: got %q; want %q", got, want)
	}
}

func TestArgsLocalUIPassKeepsSkill(t *testing.T) {
	got, err := (Spec{Bin: "claude-local", Prompt: "ui", ReadOnly: true, KeepSkills: true, Allowlist: []string{"Read"}}).Args()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--disallowedTools", "WebFetch,WebSearch,Task,Agent,NotebookEdit",
		"--allowedTools", "Read", "--permission-prompts", "none", "ui"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("local UI pass argv: got %q; want %q", got, want)
	}
}

func TestArgsCloudRoleUnchanged(t *testing.T) {
	for _, keep := range []bool{false, true} {
		got, err := (Spec{Bin: "claude", Prompt: "implement", KeepSkills: keep, Allowlist: []string{"Read", "Edit"}}).Args()
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
			"--allowedTools", "Read,Edit", "--permission-prompts", "none", "implement"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("cloud argv (KeepSkills %v): got %q; want %q", keep, got, want)
		}
	}
}
