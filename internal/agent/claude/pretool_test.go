// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// S370: the approval detail is never cut, keeps the newline that separates
// commands, and drops terminal controls.
func TestApprovalDetailIsWholeAndKeepsNewlines(t *testing.T) {
	long := strings.Repeat("x", 500)
	input, _ := json.Marshal(map[string]string{"command": "rm a\r\ncurl " + long + "\x1b[2J\x07"})
	got := (ToolCall{ToolName: "Bash", ToolInput: input}).ApprovalDetail()
	if want := "rm a\ncurl " + long + "[2J"; got != want {
		t.Errorf("detail %q, want %q", got, want)
	}
}

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

func TestEvaluateQuotedPipeIsNotAnOperator(t *testing.T) {
	root := t.TempDir()
	p := ApprovalPolicy{Worktree: root, Allowlist: []string{"Bash(go -C * test*)"}}
	for _, filter := range []string{"'TestA|TestB'", "'TestA;TestB&TestC>TestD'"} {
		got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote("go -C "+root+" test -run "+filter+" ./internal/cli") + `}`)})
		if got != (Approval{"allow", "role allowlist"}) {
			t.Errorf("filter %s = %+v, want allow with role allowlist", filter, got)
		}
	}
}

func TestEvaluateUnquotedPipeStillAsks(t *testing.T) {
	root := t.TempDir()
	p := ApprovalPolicy{Worktree: root, Allowlist: []string{"Bash(go -C * test*)"}}
	got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote("go -C "+root+" test ./... | tail -5") + `}`)})
	if got.Decision != "ask" {
		t.Fatalf("unquoted pipe = %+v, want ask", got)
	}
}

func TestEvaluateTildePathOutsideWorktree(t *testing.T) {
	p := ApprovalPolicy{Worktree: t.TempDir(), Allowlist: []string{"Bash(cat*)"}}
	got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"cat ~/.ssh/config"}`)})
	if got.Decision == "allow" {
		t.Fatalf("tilde path = %+v, must ask or deny", got)
	}
}

func TestEvaluateBracePathOutsideWorktree(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Dir(root), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	p := ApprovalPolicy{Worktree: root, Allowlist: []string{"Bash(ls*)"}}
	for _, path := range []string{"/home/{x,y}", root + "/{safe,escape}/file"} {
		got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote("ls "+path) + `}`)})
		if got.Decision == "allow" {
			t.Errorf("brace path %s = %+v, must ask or deny", path, got)
		}
	}
}

func TestEvaluateDoubleQuotedOperator(t *testing.T) {
	root := t.TempDir()
	p := ApprovalPolicy{Worktree: root, Allowlist: []string{"Bash(go -C * test*)"}}
	for _, tc := range []struct{ filter, want string }{
		{`"TestA|TestB"`, "allow"},
		{`"$(echo TestA)"`, "ask"},
		{"\"`echo TestA`\"", "ask"},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote("go -C "+root+" test -run "+tc.filter+" ./internal/cli") + `}`)})
			if got.Decision != tc.want {
				t.Fatalf("double quoted filter = %+v, want %s", got, tc.want)
			}
		})
	}
}

func TestEvaluateQuotedPathStaysWhole(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "escape space")
	if err := os.Symlink(filepath.Dir(root), link); err != nil {
		t.Fatal(err)
	}
	p := ApprovalPolicy{Worktree: root, Allowlist: []string{"Bash(ls*)"}}
	got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote("ls '"+link+"/file'") + `}`)})
	if got.Decision != "deny" {
		t.Fatalf("quoted symlink path = %+v, want deny", got)
	}
}

func TestEvaluateOutsidePathOverridesAsk(t *testing.T) {
	p := ApprovalPolicy{Worktree: t.TempDir(), Allowlist: []string{"Bash(ls*)", "Bash(cat*)"}}
	for _, tc := range []struct{ name, command, outside string }{
		{"brace", "ls {a,b} /etc/passwd", "/etc/passwd"},
		{"tilde", "cat ~/.ssh/config ../outside", "../outside"},
		{"incomplete quote", "cat /etc/passwd; # '", "/etc/passwd"},
		{"trailing backslash", `cat /etc/passwd\`, "/etc/passwd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote(tc.command) + `}`)})
			if got.Decision != "deny" {
				t.Errorf("decision = %q, want deny: %s", got.Decision, got.Reason)
			}
			if !strings.Contains(got.Reason, strconvQuote(tc.outside)) {
				t.Errorf("reason %q lacks outside token %q", got.Reason, tc.outside)
			}
		})
	}
}

func TestPretoolMissingRead(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name, path, want string
		allow            []string
	}{
		{"allowed", filepath.Join(root, "missing"), "allow", []string{"Read"}},
		{"uncovered", filepath.Join(root, "missing"), "ask", nil},
		{"outside", filepath.Join(root, "..", "missing"), "deny", []string{"Read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (ApprovalPolicy{Worktree: root, Allowlist: tc.allow}).Evaluate(ToolCall{ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":` + strconvQuote(tc.path) + `}`)})
			if got.Decision != tc.want {
				t.Fatalf("missing Read decision = %q, want %q: %s", got.Decision, tc.want, got.Reason)
			}
		})
	}
}

func TestPretoolOwnFileReads(t *testing.T) {
	root := t.TempDir()
	progress := filepath.Join(root, "run", "tmp", "progress-reviewer.md")
	p := ApprovalPolicy{Worktree: filepath.Join(root, "work"), AddDir: filepath.Dir(progress), TempDir: filepath.Join(root, "run", "tmp", "reviewer-1"), ReportFile: filepath.Join(root, "run", "reviewer-1.md"), Allowlist: []string{"Read", "Edit(/" + filepath.ToSlash(progress) + ")"}}
	for _, command := range []string{"ls -la", "wc -l", "head", "tail", "cat", "stat"} {
		for _, path := range []string{p.TempDir, filepath.Join(p.TempDir, "child"), p.ReportFile, progress} {
			t.Run(command+"/"+filepath.Base(path), func(t *testing.T) {
				got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote(command+" "+strconvQuote(path)) + `}`)})
				if got != (Approval{"allow", "read-only on own files"}) {
					t.Fatalf("own-file read = %+v", got)
				}
			})
		}
	}
	for _, command := range []string{"rm " + progress, "cat " + progress + " > " + p.ReportFile, "ls " + p.Worktree, "ls " + filepath.Dir(progress), "cat " + progress + " " + p.Worktree, "head -c " + progress, "ls", "ls " + progress + " && cat " + progress, "ls " + progress + "*"} {
		t.Run(command, func(t *testing.T) {
			got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote(command) + `}`)})
			want := "ask"
			if command == "ls "+filepath.Dir(progress) || command == "ls "+progress+"*" {
				want = "deny"
			}
			if got.Decision != want {
				t.Fatalf("uncovered command = %+v, want %s", got, want)
			}
		})
	}
	for _, tool := range []string{"Write", "Edit"} {
		for _, path := range []string{progress, filepath.Join(p.AddDir, "other.md"), p.ReportFile, filepath.Join(p.TempDir, "file")} {
			t.Run(tool+"/"+filepath.Base(path), func(t *testing.T) {
				got := p.Evaluate(ToolCall{ToolName: tool, ToolInput: json.RawMessage(`{"file_path":` + strconvQuote(path) + `}`)})
				want := "ask"
				if path == progress {
					want = "allow"
				} else if path == filepath.Join(p.AddDir, "other.md") {
					want = "deny"
				}
				if got.Decision != want {
					t.Fatalf("write scope = %+v, want %s", got, want)
				}
			})
		}
	}
}

func TestPretoolOwnFileReadQuotedPath(t *testing.T) {
	root := t.TempDir()
	p := ApprovalPolicy{Worktree: filepath.Join(root, "work"), TempDir: filepath.Join(root, "temp space")}
	for _, quote := range []string{"'", `"`} {
		t.Run(quote, func(t *testing.T) {
			got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote("ls -la "+quote+p.TempDir+quote) + `}`)})
			if got != (Approval{"allow", "read-only on own files"}) {
				t.Fatalf("quoted own-file read = %+v", got)
			}
		})
	}
}

func TestPretoolOtherRunDirectoryDenied(t *testing.T) {
	root := t.TempDir()
	run := filepath.Join(root, "runs", "own-run")
	progress := filepath.Join(run, "tmp", "progress-reviewer.md")
	p := ApprovalPolicy{
		Worktree: filepath.Join(root, "work"), AddDir: filepath.Dir(progress),
		TempDir: filepath.Join(run, "tmp", "reviewer-1"), ReportFile: filepath.Join(run, "reviewer-1.md"),
		Allowlist: []string{"Bash(ls*)", "Edit(/" + filepath.ToSlash(progress) + ")"},
	}
	command := "ls -la " + filepath.Join(root, "runs", "other-run", "tmp")
	got := p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote(command) + `}`)})
	if got.Decision != "deny" {
		t.Fatalf("other-run directory = %+v, want deny", got)
	}
}

func TestPretoolSiblingTempsDenied(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "run", "tmp")
	secret := filepath.Join(shared, "implementer-1", "secret")
	if err := os.MkdirAll(filepath.Dir(secret), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("private\n"), 0600); err != nil {
		t.Fatal(err)
	}
	progress := filepath.Join(shared, "progress-reviewer.md")
	p := ApprovalPolicy{Worktree: filepath.Join(root, "work"), AddDir: shared,
		TempDir:   filepath.Join(shared, "reviewer-1"),
		Allowlist: []string{"Read", "Grep", "Glob", "Edit(/" + filepath.ToSlash(progress) + ")"}}
	for _, tc := range []struct{ tool, field, path string }{
		{"Read", "file_path", secret},
		{"Grep", "path", secret},
		{"Glob", "pattern", secret + "*"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			input, _ := json.Marshal(map[string]string{tc.field: tc.path})
			got := p.Evaluate(ToolCall{ToolName: tc.tool, ToolInput: input})
			if got.Decision != "deny" {
				t.Fatalf("sibling temp = %+v, want deny", got)
			}
		})
	}
}

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
		{"progress other", "Write", `{"file_path":` + strconvQuote(filepath.Join(p.AddDir, "other.md")) + `}`, "deny", ""},
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
