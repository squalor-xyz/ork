// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"reflect"
	"slices"
	"testing"
)

const localSystemPath = "/run/tmp/local-system.md"

func TestCloudClaudeReadOnlyRunsOnTheAllowlist(t *testing.T) {
	got, err := (Spec{Bin: "claude", Prompt: "review", ReadOnly: true, Allowlist: []string{"Read", "Bash(go test*)"}}).Args()
	want := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--allowedTools", "Read,Bash(go test*)", "--permission-prompts", "none", "review"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("cloud read-only argv = %q, err = %v; want %q", got, err, want)
	}
}

func TestCloudClaudeReadOnlyNeedsAnAllowlist(t *testing.T) {
	if _, err := (Spec{Bin: "claude", Prompt: "review", ReadOnly: true}).Args(); err == nil {
		t.Fatal("cloud read-only role accepted an empty allowlist")
	}
}

func TestArgsLocalReadOnlyUsesAllowlist(t *testing.T) {
	for _, vendor := range []string{"claude-local", "claude"} {
		t.Run(vendor, func(t *testing.T) {
			got, err := (Spec{Bin: vendor, Prompt: "review", ReadOnly: true, SystemPromptFile: localSystemPath, Allowlist: []string{"Read", "Grep", "Glob"}}).Args()
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
			if vendor == "claude-local" {
				want = append(want, "--system-prompt-file", localSystemPath, "--tools", "Read,Grep,Glob,Bash", "--disable-slash-commands", "--allowedTools", "Read,Grep,Glob", "--permission-prompts", "none")
				want = append(want, "--permission-mode", "manual", "--settings", HookSettings(""))
			} else {
				want = append(want, "--allowedTools", "Read,Grep,Glob", "--permission-prompts", "none")
			}
			want = append(want, "review")
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("read-only argv: got %q; want %q", got, want)
			}
		})
	}
}

// A local write role gets ork's own system prompt and the six tools (S364,
// adj 351 and 352, *proposed*); the S360 deny list is redundant with --tools.
func TestArgsLocalWriteRoleUsesOwnPromptAndSixTools(t *testing.T) {
	got, err := (Spec{Bin: "claude-local", Prompt: "implement", SystemPromptFile: localSystemPath, Allowlist: []string{"Read", "Edit"}}).Args()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--system-prompt-file", localSystemPath, "--tools", "Read,Grep,Glob,Edit,Write,Bash", "--disable-slash-commands",
		"--allowedTools", "Read,Edit", "--permission-prompts", "none", "--permission-mode", "manual", "--settings", HookSettings(""), "implement"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("local implementer argv: got %q; want %q", got, want)
	}
}

// A read-only role loses Edit and Write unless it holds the progress-note
// grant (AddDir), which is an Edit rule the hook applies to Edit and Write.
func TestArgsLocalReadOnlyToolsKeepEditWriteOnlyForProgressNote(t *testing.T) {
	for _, tc := range []struct{ addDir, want string }{
		{"", "Read,Grep,Glob,Bash"},
		{"/run/progress", "Read,Grep,Glob,Edit,Write,Bash"},
	} {
		got, err := (Spec{Bin: "claude-local", Prompt: "review", ReadOnly: true, AddDir: tc.addDir, SystemPromptFile: localSystemPath, Allowlist: []string{"Read"}}).Args()
		if err != nil {
			t.Fatal(err)
		}
		i := slices.Index(got, "--tools")
		if i < 0 || got[i+1] != tc.want {
			t.Fatalf("AddDir %q: --tools in %q, want %q", tc.addDir, got, tc.want)
		}
	}
}

func TestArgsLocalUIPassKeepsSkill(t *testing.T) {
	got, err := (Spec{Bin: "claude-local", Prompt: "ui", ReadOnly: true, KeepSkills: true, SystemPromptFile: localSystemPath, Allowlist: []string{"Read"}}).Args()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--system-prompt-file", localSystemPath, "--tools", "Read,Grep,Glob,Bash,Skill",
		"--allowedTools", "Read", "--permission-prompts", "none", "--permission-mode", "manual", "--settings", HookSettings(""), "ui"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("local UI pass argv: got %q; want %q", got, want)
	}
}

func TestArgsLocalSystemPromptFileMustBeAbsolute(t *testing.T) {
	_, err := (Spec{Bin: "claude-local", Prompt: "implement", SystemPromptFile: "local-system.md", Allowlist: []string{"Read"}}).Args()
	if err == nil {
		t.Fatal("a relative system prompt file was accepted")
	}
}

// A cloud UI pass writes its renders to its run temp dir (S369, adj 372,
// *proposed*). Plan mode refuses every write, even under an allow rule
// (probe, claude 2.1, 2026-11-16), and accept-edits mode auto-approves shell
// file commands in the worktree (adj 371), so the UI pass has no permission
// mode flag: its allowlist (read-only tools plus Edit and Write under the
// directory) and --permission-prompts none deny every other call. A reviewer,
// with no write directory, gets only the read-only allowlist (adj 489).
func TestUIPassMayWriteOnlyItsTempDir(t *testing.T) {
	base := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	ui, err := (Spec{Bin: "claude", Dir: "/work/tree", WriteDir: "/run/tmp/ui-1", Prompt: "ui", ReadOnly: true, KeepSkills: true, Allowlist: []string{"Read", "Bash(go test*)"}}).Args()
	if err != nil {
		t.Fatal(err)
	}
	want := append(slices.Clone(base), "--add-dir", "/run/tmp/ui-1",
		"--allowedTools", "Read,Bash(go test*),Edit(//run/tmp/ui-1/**),Write(//run/tmp/ui-1/**)",
		"--permission-prompts", "none", "ui")
	if !reflect.DeepEqual(ui, want) {
		t.Fatalf("UI pass argv: got %q; want %q", ui, want)
	}
	reviewer, err := (Spec{Bin: "claude", Dir: "/work/tree", Prompt: "review", ReadOnly: true, Allowlist: []string{"Read", "Bash(go test*)"}}).Args()
	if err != nil {
		t.Fatal(err)
	}
	want = append(slices.Clone(base), "--allowedTools", "Read,Bash(go test*)", "--permission-prompts", "none", "review")
	if !reflect.DeepEqual(reviewer, want) {
		t.Fatalf("reviewer argv: got %q; want %q", reviewer, want)
	}
}

// The write directory must be absolute and clean: a relative path would turn
// the rule into a pattern about the wrong place.
func TestUIPassWriteDirNeedsAbsolutePaths(t *testing.T) {
	for _, tc := range []struct{ name, write string }{
		{"relative write dir", "tmp/ui-1"},
		{"unclean write dir", "/run/tmp/../tmp/ui-1"},
		{"comma in write dir", "/run/tmp,x/ui-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := (Spec{Bin: "claude", WriteDir: tc.write, Prompt: "ui", ReadOnly: true}).Args(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// Characters that mean something in a rule pattern are escaped in the path.
func TestUIPassWriteDirEscapesPatternCharacters(t *testing.T) {
	got, err := (Spec{Bin: "claude", WriteDir: "/run/a*b/ui-1", Prompt: "ui", ReadOnly: true}).Args()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(got, "--allowedTools")
	if i < 0 || got[i+1] != `Edit(//run/a\*b/ui-1/**),Write(//run/a\*b/ui-1/**)` {
		t.Fatalf("allow rules in %q", got)
	}
}

func TestArgsCloudRoleUnchanged(t *testing.T) {
	for _, keep := range []bool{false, true} {
		got, err := (Spec{Bin: "claude", Prompt: "implement", KeepSkills: keep, SystemPromptFile: localSystemPath, Allowlist: []string{"Read", "Edit"}}).Args()
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
