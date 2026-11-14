// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPretoolReadLimits(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name             string
		lines            int
		extra            string
		decision, reason string
	}{
		{"501 whole", 501, "", "deny", "501 lines"},
		{"500 whole", 500, "", "allow", ""},
		{"600 limit", 2000, `,"limit":600`, "deny", "500"},
		{"bounded range", 2000, `,"offset":1000,"limit":200`, "allow", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(root, "file")
			if err := os.WriteFile(path, []byte(strings.Repeat("line\n", tc.lines)), 0600); err != nil {
				t.Fatal(err)
			}
			input := json.RawMessage(`{"file_path":` + strconvQuote(path) + tc.extra + `}`)
			got := (ApprovalPolicy{Worktree: root, Allowlist: []string{"Read"}}).Evaluate(ToolCall{ToolName: "Read", ToolInput: input})
			if got.Decision != tc.decision {
				t.Fatalf("decision %q, want %q: %s", got.Decision, tc.decision, got.Reason)
			}
			if tc.reason != "" && (!strings.Contains(got.Reason, tc.reason) || !strings.Contains(got.Reason, "500")) {
				t.Fatalf("reason %q lacks count/500 guidance", got.Reason)
			}
		})
	}
}

func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestPretoolPathsAndAllowlist(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "session.jsonl"), []byte("transcript\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(work, "link")); err != nil {
		t.Fatal(err)
	}
	p := ApprovalPolicy{Worktree: work, Scratch: filepath.Join(root, "scratch"), AddDir: filepath.Join(root, "progress"), ToolResults: filepath.Join(root, "session", "tool-results"), Allowlist: []string{"Read", "Grep", "Glob", "Edit(/" + filepath.ToSlash(filepath.Join(root, "progress", "progress.md")) + ")", "Bash(go test*)", "Bash(go vet ./... && go test ./...*)"}}
	for _, tc := range []struct{ name, tool, input, want, reason string }{
		{"read outside", "Read", `{"file_path":` + strconvQuote(filepath.Join(root, "outside")) + `}`, "deny", "outside"},
		{"bash outside", "Bash", `{"command":"go test /outside"}`, "deny", "/outside"},
		{"bash traversal", "Bash", `{"command":"go test ../outside"}`, "deny", "../outside"},
		{"symlink", "Read", `{"file_path":"link"}`, "deny", "link"},
		{"grep outside", "Grep", `{"path":"/outside"}`, "deny", "/outside"},
		{"glob outside", "Glob", `{"pattern":"/outside/*"}`, "deny", "/outside"},
		{"known", "Bash", `{"command":"go test ./..."}`, "allow", ""},
		{"unknown", "Bash", `{"command":"make test"}`, "ask", ""},
		{"compound unknown", "Bash", `{"command":"go test; make test"}`, "ask", ""},
		{"pipe", "Bash", `{"command":"go test | make test"}`, "ask", ""},
		{"substitution", "Bash", `{"command":"go test $(make test)"}`, "ask", ""},
		{"compound check", "Bash", `{"command":"go vet ./... && go test ./..."}`, "allow", ""},
		{"scratch", "Grep", `{"path":` + strconvQuote(p.Scratch) + `}`, "allow", ""},
		{"progress exact", "Write", `{"file_path":` + strconvQuote(filepath.Join(p.AddDir, "progress.md")) + `}`, "allow", ""},
		{"progress other", "Write", `{"file_path":` + strconvQuote(filepath.Join(p.AddDir, "other.md")) + `}`, "ask", ""},
		{"tool results", "Grep", `{"path":` + strconvQuote(p.ToolResults) + `}`, "allow", ""},
		{"transcript", "Read", `{"file_path":` + strconvQuote(filepath.Join(root, "session.jsonl")) + `}`, "deny", "session.jsonl"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Evaluate(ToolCall{ToolName: tc.tool, ToolInput: json.RawMessage(tc.input)})
			if got.Decision != tc.want || !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("got %+v; want %s with %q", got, tc.want, tc.reason)
			}
		})
	}
}

func TestArgsLocalApproval(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		name := "write"
		if readOnly {
			name = "read-only"
		}
		t.Run(name, func(t *testing.T) {
			args, err := (Spec{Bin: "claude-local", Prompt: "work", ReadOnly: readOnly, Allowlist: []string{"Read"}}).Args()
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "--permission-mode manual") || !strings.Contains(joined, "--settings") || !strings.Contains(joined, "PreToolUse") || !strings.Contains(joined, "ork hook pretool") || strings.Contains(joined, "--permission-mode plan") || strings.Contains(joined, "--permission-mode auto") {
				t.Fatalf("local approval flags missing: %q", args)
			}
		})
	}
}
