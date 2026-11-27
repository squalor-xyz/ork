// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/roles"
)

func TestApprovalAllowsRemovingAFileInTheWorktree(t *testing.T) {
	for _, preset := range engine.PresetNames() {
		allow, _ := engine.ImplementerPreset(preset, "")
		root := t.TempDir()
		for _, command := range []string{"git rm cmd/ork/start_unix.go", "rm tests/x.py.tail", "git -C " + root + " rm f", "rm -f x", "rm -- -report"} {
			t.Run(preset+"/"+command, func(t *testing.T) {
				got := (ApprovalPolicy{Worktree: root, Allowlist: allow}).Evaluate(ToolCall{ToolName: "Bash", CWD: root, ToolInput: json.RawMessage(`{"command":` + strconvQuote(command) + `}`)})
				if got.Decision != "allow" {
					t.Fatalf("%s: %+v, want allow", command, got)
				}
			})
		}
	}
}

func TestApprovalDeniesRemovingOutsideTheWorktree(t *testing.T) {
	for _, command := range []string{"rm ../other/file", "rm /home/x/file", "git -C /elsewhere rm f"} {
		t.Run(command, func(t *testing.T) {
			got := (ApprovalPolicy{Worktree: t.TempDir(), Allowlist: []string{"Bash(rm *)", "Bash(git -C * rm *)"}}).Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote(command) + `}`)})
			if got.Decision != "deny" || !strings.Contains(got.Reason, "outside worktree") {
				t.Fatalf("%s: %+v, want deny outside worktree", command, got)
			}
		})
	}
}

// A cloud write role runs the hook for paths and recursive rm only (owner
// answer launch.cloud-hook-scope, 2026-11-19): no owner question, no Read cap,
// no grants beyond the role allowlist.
func TestApprovalPathsOnlyKeepsCloudBehaviour(t *testing.T) {
	root := t.TempDir()
	long := filepath.Join(root, "long.go")
	if err := os.WriteFile(long, []byte(strings.Repeat("line\n", 600)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	allow, _ := engine.ImplementerPreset("go", "")
	for _, tc := range []struct {
		name, tool, input, local, cloud string
	}{
		// A call the hook does not deny is left to the CLI's own allowlist
		// (owner answer launch.cloud-hook-passthrough): "defer".
		{"Read over the cap", "Read", `{"file_path":` + strconvQuote(long) + `}`, "deny", "defer"},
		{"uncovered command", "Bash", `{"command":"make test"}`, "ask", "defer"},
		{"tool outside the allowlist", "TodoWrite", `{"todos":[]}`, "ask", "defer"},
		{"incomplete quoting", "Bash", `{"command":"git status 'x"}`, "ask", "defer"},
		{"tilde expansion", "Bash", `{"command":"git status ~/x"}`, "ask", "defer"},
		{"cat of an own file", "Bash", `{"command":"cat ` + filepath.Join(root, "go.mod") + `"}`, "allow", "defer"},
		{"ordinary removal", "Bash", `{"command":"git rm a.go"}`, "allow", "defer"},
		{"ordinary plain rm", "Bash", `{"command":"rm a.go"}`, "allow", "defer"},
		// What only an ask reached before must still be denied for a removal.
		{"recursive rm", "Bash", `{"command":"rm -rf tests"}`, "ask", "deny"},
		{"recursive rm after a file", "Bash", `{"command":"rm x -r"}`, "ask", "deny"},
		{"rm with a tilde", "Bash", `{"command":"rm ~/x"}`, "ask", "deny"},
		{"rm with a variable", "Bash", `{"command":"rm $HOME/x"}`, "ask", "deny"},
		{"rm with incomplete quoting", "Bash", `{"command":"rm 'x"}`, "ask", "deny"},
		{"git mv with a variable", "Bash", `{"command":"git mv $HOME/a b"}`, "ask", "deny"},
		{"git rm with a substitution", "Bash", `{"command":"git rm $(echo a)"}`, "ask", "deny"},
		{"compound removal", "Bash", `{"command":"rm a && rm b"}`, "ask", "deny"},
		{"outside path", "Bash", `{"command":"rm ../other/file"}`, "deny", "deny"},
		{"outside Read", "Read", `{"file_path":"/etc/hosts"}`, "deny", "deny"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := ApprovalPolicy{Worktree: root, TempDir: root, Allowlist: allow, Role: "implementer"}
			call := ToolCall{ToolName: tc.tool, CWD: root, ToolInput: json.RawMessage(tc.input)}
			if got := policy.Evaluate(call); got.Decision != tc.local {
				t.Fatalf("local policy: %+v, want %s", got, tc.local)
			}
			policy.PathsOnly = true
			if got := policy.Evaluate(call); got.Decision != tc.cloud {
				t.Fatalf("paths-only policy: %+v, want %s", got, tc.cloud)
			}
		})
	}
}

func TestApprovalDeniesRecursiveRemove(t *testing.T) {
	for _, command := range []string{"rm -r dir", "rm -R dir", "rm -rf .", "rm -fr x", "rm --recursive x", "rm x -r", "rm '-fr' x", `rm -\r x`, "rm --recursive=never x"} {
		t.Run(command, func(t *testing.T) {
			got := (ApprovalPolicy{Worktree: t.TempDir(), Allowlist: []string{"Bash(rm *)"}}).Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote(command) + `}`)})
			if got.Decision == "allow" {
				t.Fatalf("%s: %+v, must not allow recursive removal", command, got)
			}
		})
	}
}

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

// S372: the implementer's Read cap is 150 lines, with a Grep-and-window
// denial. A read-only role keeps the 500-line cap and today's denial.
func TestEvaluateLocalReadCap(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte(strings.Repeat("line\n", 2000)), 0600); err != nil {
		t.Fatal(err)
	}
	eval := func(role, extra string) Approval {
		t.Helper()
		input := json.RawMessage(`{"file_path":` + strconvQuote(path) + extra + `}`)
		return (ApprovalPolicy{Worktree: root, Role: role, Allowlist: []string{"Read"}}).Evaluate(ToolCall{ToolName: "Read", ToolInput: input})
	}
	implOver := eval("implementer", `,"limit":151`)
	if implOver.Decision != "deny" || implOver.Reason != "Read limit must be 1 to 150; Grep for the symbol and Read 150 lines around it" {
		t.Fatalf("implementer over cap = %+v", implOver)
	}
	if got := eval("implementer", `,"limit":150`); got.Decision != "allow" {
		t.Fatalf("implementer at cap = %+v, want allow", got)
	}
	if got := eval("reviewer", `,"limit":500`); got.Decision != "allow" {
		t.Fatalf("read-only at cap = %+v, want allow", got)
	}
	roOver := eval("reviewer", `,"limit":501`)
	if roOver.Decision != "deny" || roOver.Reason != "Read limit must be 1 to 500; Grep first, then Read with offset and limit (at most 500 lines)" {
		t.Fatalf("read-only over cap = %+v", roOver)
	}
}

func TestEvaluateUnlimitedReadOfABigFile(t *testing.T) {
	root := t.TempDir()
	write := func(name string, n int) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(strings.Repeat("line\n", n)), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	eval := func(role, path string) Approval {
		t.Helper()
		input := json.RawMessage(`{"file_path":` + strconvQuote(path) + `}`)
		return (ApprovalPolicy{Worktree: root, Role: role, Allowlist: []string{"Read"}}).Evaluate(ToolCall{ToolName: "Read", ToolInput: input})
	}
	over := eval("implementer", write("over.go", 151))
	if over.Decision != "deny" || over.Reason != "this file has 151 lines; Grep for the symbol and Read 150 lines around it" {
		t.Fatalf("implementer unbounded = %+v", over)
	}
	if got := eval("implementer", write("at.go", 150)); got.Decision != "allow" {
		t.Fatalf("implementer 150-line file = %+v, want allow", got)
	}
	if got := eval("reviewer", write("mid.go", 151)); got.Decision != "allow" {
		t.Fatalf("read-only 151-line file = %+v, want allow", got)
	}
	ro := eval("reviewer", write("big.go", 501))
	if ro.Decision != "deny" || ro.Reason != "this file has 501 lines; Grep first, then Read with offset and limit (at most 500 lines)" {
		t.Fatalf("read-only unbounded = %+v", ro)
	}
}

func TestLocalSystemPromptNamesTheCap(t *testing.T) {
	p := roles.LocalSystemPrompt()
	for _, sentence := range []string{
		"The implementer may read at most 150 lines",
		"Grep for the symbol and Read 150 lines around it",
		"A read-only role may read at most 500 lines",
	} {
		if !strings.Contains(p, sentence) {
			t.Fatalf("local system prompt lacks %q", sentence)
		}
	}
}

// S371, *proposed*: the read log line comes from the call alone.
func TestReadLogEntry(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "short.go"), []byte(strings.Repeat("line\n", 42)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, tool, input, want string
	}{
		{"range", "Read", `{"file_path":"a.go","offset":10,"limit":20}`, "a.go:10-29"},
		{"limit only", "Read", `{"file_path":"a.go","limit":500}`, "a.go:1-500"},
		{"absolute path in the worktree", "Read", `{"file_path":` + strconvQuote(filepath.Join(root, "x", "a.go")) + `,"limit":5}`, "x/a.go:1-5"},
		{"whole file", "Read", `{"file_path":"short.go"}`, "short.go:1-42"},
		{"whole file from an offset", "Read", `{"file_path":"short.go","offset":30}`, "short.go:30-42"},
		{"file that does not exist", "Read", `{"file_path":"new.go"}`, "new.go"},
		{"path outside the worktree", "Read", `{"file_path":"/elsewhere/a.go","limit":3}`, "/elsewhere/a.go:1-3"},
		{"grep with a path", "Grep", `{"pattern":"func Foo","path":"internal"}`, "func Foo in internal"},
		{"grep without a path", "Grep", `{"pattern":"TODO"}`, "TODO in ."},
		{"grep pattern with a newline", "Grep", `{"pattern":"a\nb","path":"."}`, "a b in ."},
		{"other tool", "Glob", `{"pattern":"*.go"}`, ""},
		{"bad input", "Read", `{`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (ApprovalPolicy{Worktree: root}).ReadLogEntry(ToolCall{ToolName: tc.tool, ToolInput: json.RawMessage(tc.input)})
			if got != tc.want {
				t.Fatalf("entry %q, want %q", got, tc.want)
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

// S526: a read-only role holds the pinned go -C DIR test* entry, not a go -C *
// wildcard. The quoted -run pattern a brief shows is allowed; the unquoted one asks.
func TestEvaluateQuotedRunPatternAgainstThePinnedGoCEntry(t *testing.T) {
	root := t.TempDir()
	p := ApprovalPolicy{Worktree: root, Allowlist: []string{"Bash(go -C " + root + " test*)"}}
	eval := func(command string) Approval {
		return p.Evaluate(ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":` + strconvQuote(command) + `}`)})
	}
	if got := eval("go -C " + root + " test -run 'A|B' ./internal/cli"); got != (Approval{"allow", "role allowlist"}) {
		t.Errorf("quoted pattern = %+v, want allow with role allowlist", got)
	}
	if got := eval("go -C " + root + " test -run A|B ./internal/cli"); got.Decision != "ask" {
		t.Errorf("unquoted pattern = %+v, want ask", got)
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

// A reviewer's temp-dir grant (S510) is an Edit(//DIR/**) rule. The hook allows
// Edit and Write under that directory and nowhere else: a path outside every
// permitted directory is denied, and a permitted directory without the grant
// still asks.
func TestPretoolTempDirGrantAllowsWritesUnderItOnly(t *testing.T) {
	root := t.TempDir()
	tmp := filepath.Join(root, "t", "run", "reviewer-1")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(tmp, "escape")); err != nil {
		t.Fatal(err)
	}
	// A link inside the grant that leads to another permitted directory (the
	// worktree): the path passes the boundary check but is not under the grant.
	if err := os.MkdirAll(filepath.Join(root, "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "work"), filepath.Join(tmp, "to-worktree")); err != nil {
		t.Fatal(err)
	}
	grant := "Edit(/" + filepath.ToSlash(tmp) + "/**)"
	p := ApprovalPolicy{Worktree: filepath.Join(root, "work"), TempDir: filepath.Join(root, "runs", "run", "tmp", "reviewer-1"), TMPDir: tmp, Allowlist: []string{"Read", grant, "Write(/" + filepath.ToSlash(tmp) + "/**)"}}
	for _, tool := range []string{"Write", "Edit"} {
		for _, tc := range []struct{ name, path, want string }{
			{"file under", filepath.Join(tmp, "probe_test.go"), "allow"},
			{"new nested file", filepath.Join(tmp, "a", "b", "probe_test.go"), "allow"},
			{"outside", filepath.Join(root, "elsewhere", "probe_test.go"), "deny"},
			{"sibling prefix", tmp + "-other/probe_test.go", "deny"},
			{"parent", filepath.Join(filepath.Dir(tmp), "probe_test.go"), "deny"},
			{"dotdot", filepath.Join(tmp, "..", "probe_test.go"), "deny"},
			{"symlink escape", filepath.Join(tmp, "escape", "probe_test.go"), "deny"},
			{"link to worktree", filepath.Join(tmp, "to-worktree", "probe_test.go"), "ask"},
			{"worktree", filepath.Join(p.Worktree, "probe_test.go"), "ask"},
			{"assets dir", filepath.Join(p.TempDir, "probe_test.go"), "ask"},
		} {
			t.Run(tool+"/"+tc.name, func(t *testing.T) {
				got := p.Evaluate(ToolCall{ToolName: tool, ToolInput: json.RawMessage(`{"file_path":` + strconvQuote(tc.path) + `}`)})
				if got.Decision != tc.want {
					t.Fatalf("%s %s = %+v, want %s", tool, tc.path, got, tc.want)
				}
			})
		}
	}
	// Without the grant the same Write only asks.
	p.Allowlist = []string{"Read"}
	if got := p.Evaluate(ToolCall{ToolName: "Write", ToolInput: json.RawMessage(`{"file_path":` + strconvQuote(filepath.Join(tmp, "probe_test.go")) + `}`)}); got.Decision != "ask" {
		t.Fatalf("Write without the grant = %+v, want ask", got)
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
