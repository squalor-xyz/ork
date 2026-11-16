// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/agent/codex"
	"github.com/squalor-xyz/ork/internal/agent/grok"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/roles"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestClaudeRunnerCountsApprovalDenials(t *testing.T) {
	hook := runrec.DeniedCommand{Tool: "Read", Command: "long.go"}
	duplicate := claude.Denial{ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"long.go"}`)}
	other := claude.Denial{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"unlisted"}`)}
	for _, tc := range []struct {
		name   string
		hooks  []runrec.DeniedCommand
		vendor []claude.Denial
		count  int
		want   []runrec.DeniedCommand
	}{
		{"hook-only", []runrec.DeniedCommand{hook}, nil, 1, []runrec.DeniedCommand{hook}},
		{"duplicate", []runrec.DeniedCommand{hook}, []claude.Denial{duplicate}, 1, []runrec.DeniedCommand{hook}},
		{"mixed", []runrec.DeniedCommand{hook}, []claude.Denial{duplicate, other}, 1, []runrec.DeniedCommand{hook, {Tool: "Bash", Command: "unlisted"}}},
		{"vendor-only", nil, []claude.Denial{other}, 0, []runrec.DeniedCommand{{Tool: "Bash", Command: "unlisted"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := claudeRunner{
				approval: func(context.Context, engine.Request, []string, string) (string, func() []runrec.DeniedCommand, error) {
					return "hook", func() []runrec.DeniedCommand { return slices.Clone(tc.hooks) }, nil
				},
				round: func(context.Context, claude.Spec) (claude.Round, error) {
					return claude.Round{Denials: tc.vendor}, context.DeadlineExceeded
				},
			}
			got, err := r.Run(context.Background(), engine.Request{Vendor: "claude-local"})
			if !errors.Is(err, context.DeadlineExceeded) || got.ApprovalDenials != tc.count || !reflect.DeepEqual(got.Denials, tc.want) {
				t.Fatalf("approval count=%d denials=%+v err=%v; want count=%d denials=%+v", got.ApprovalDenials, got.Denials, err, tc.count, tc.want)
			}
		})
	}
}

// These launchers exercise the real dispatcher and adapters without a server.
func localLaunchers(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for vendor, output := range map[string]string{
		"claude-local": `{"type":"result","subtype":"success","is_error":false,"result":"local report","session_id":"local"}`,
		"codex-local":  "{\"type\":\"thread.started\",\"thread_id\":\"local\"}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"local report\"}}\n{\"type\":\"turn.completed\"}",
		"grok-local":   `{"type":"result","subtype":"success","is_error":false,"result":"local report","session_id":"local"}`,
	} {
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ORK_CAPTURE\"\nprintf '%s\\n' \"${ORK_CLOUD_ONLY-unset}\" > \"$ORK_ENV_CAPTURE\"\nprintf '%s\\n' '" + output + "'\n"
		if err := os.WriteFile(filepath.Join(dir, vendor), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

func TestLocalDispatcherUsesLaunchersAndRowFlags(t *testing.T) {
	for _, vendor := range []string{"claude-local", "codex-local", "grok-local"} {
		t.Run(vendor, func(t *testing.T) {
			d, _, p := runDaemon(t, nil)
			d.runs.deps.Runner = nil
			dir := localLaunchers(t)
			capture := filepath.Join(dir, "args")
			t.Setenv("ORK_CAPTURE", capture)
			t.Setenv("ORK_ENV_CAPTURE", filepath.Join(dir, "env"))
			model := "qwen3.8-27b"
			if vendor == "grok-local" {
				model = "latest"
			}
			got, err := d.runDeps(p).Runner.Run(context.Background(), engine.Request{Vendor: vendor, Brief: "brief", Worktree: dir, TempDir: dir, Model: model, Effort: "medium", Resume: "prior", ReadOnly: true})
			if err != nil || got.Report != "local report" {
				t.Fatalf("launcher result: %+v, %v", got, err)
			}
			b, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string][]string{
				"claude-local": {"-p", "--resume", "prior", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--model", model, "--effort", "medium", "--system-prompt-file", filepath.Join(dir, "local-system.md"), "--tools", "Read,Grep,Glob,Bash", "--disable-slash-commands", "--allowedTools", localReadOnlyTools, "--permission-prompts", "none", "brief"},
				"codex-local":  {"exec", "resume", "prior", "-m", model, "--json", "-c", "model_reasoning_effort=medium", "-c", `sandbox_mode="read-only"`, "brief"},
				"grok-local":   {"-p", "brief", "--output-format", "streaming-messages-json", "--include-partial-messages", "--reasoning-effort", "medium", "--resume", "prior", "--permission-mode", "plan", "--cwd", dir},
			}[vendor]
			if vendor == "claude-local" {
				// The command contains a fresh per-attempt session id. Check its
				// shape separately, then compare every other argument exactly.
				actual := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
				i := slices.Index(actual, "--settings")
				if i < 0 || i+1 >= len(actual) || !strings.Contains(actual[i+1], "hook pretool --socket") {
					t.Fatalf("local hook settings missing: %q", actual)
				}
				want = append(want[:len(want)-1], "--permission-mode", "manual", "--settings", actual[i+1], "brief")
			}
			if string(b) != strings.Join(want, "\n")+"\n" {
				t.Fatalf("launcher argv: %q; want %q", b, want)
			}
			if vendor == "claude-local" {
				// ork writes its own system prompt into the role's temp directory.
				text, err := os.ReadFile(filepath.Join(dir, "local-system.md"))
				if err != nil || string(text) != roles.LocalSystemPrompt() {
					t.Fatalf("local system prompt file: %q, %v", text, err)
				}
			}
		})
	}
}

const localReadOnlyTools = "Read,Grep,Glob,Bash(git diff*),Bash(git log*),Bash(git status*),Bash(git show*),Bash(git -C * diff*),Bash(git -C * log*),Bash(git -C * status*),Bash(git -C * show*),Bash(slicer show*),Bash(slicer goals*),Bash(slicer list*),Bash(slicer deps*),Bash(slicer find*),Bash(adj show*),Bash(adj check*),Bash(go test*),Bash(go vet*),Bash(go -C * test*),Bash(go -C * vet*),Bash(gofmt -l*)"

// Only a cloud UI pass gets a write directory, its run temp dir (S369).
func TestClaudeRunnerGivesOnlyTheCloudUIPassAWriteDir(t *testing.T) {
	for _, tc := range []struct {
		name, vendor, role, want string
	}{
		{"cloud UI pass", "claude", engine.RoleUI, "temp"},
		{"cloud reviewer", "claude", engine.RoleReviewer, ""},
		{"local UI pass", "claude-local", engine.RoleUI, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			temp := t.TempDir()
			if tc.want != "" {
				tc.want = temp
			}
			var got claude.Spec
			r := claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
				got = s
				return claude.Round{Class: claude.ClassOK}, nil
			}}
			_, err := r.Run(context.Background(), engine.Request{Vendor: tc.vendor, Role: tc.role, ReadOnly: true, Worktree: "/work/tree", TempDir: temp, Brief: "brief"})
			if err != nil {
				t.Fatal(err)
			}
			if got.WriteDir != tc.want {
				t.Fatalf("WriteDir = %q, want %q", got.WriteDir, tc.want)
			}
			if tc.want != "" && strings.Join(got.Allowlist, ",") != localReadOnlyTools+",Skill" {
				t.Fatalf("UI pass allowlist = %q", got.Allowlist)
			}
		})
	}
}

func TestClaudeContextReachesEngine(t *testing.T) {
	got := 0
	r := claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
		ev, err := claude.ParseLine([]byte(`{"type":"assistant","message":{"usage":{"input_tokens":1,"cache_read_input_tokens":70000}}}`))
		if err != nil {
			t.Fatal(err)
		}
		s.OnEvent(ev)
		return claude.Round{Class: claude.ClassOK}, nil
	}}
	_, err := r.Run(context.Background(), engine.Request{Vendor: "claude-local", Brief: "brief", OnContext: func(n int) { got = n }})
	if err != nil {
		t.Fatal(err)
	}
	if got != 70001 {
		t.Fatalf("engine context = %d, want 70001", got)
	}
}

func TestInterruptedClaudeLocalKeepsObservedInput(t *testing.T) {
	r := claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
		ev, err := claude.ParseLine([]byte(`{"type":"assistant","message":{"usage":{"input_tokens":1,"cache_read_input_tokens":70000}}}`))
		if err != nil {
			t.Fatal(err)
		}
		s.OnEvent(ev)
		return claude.Round{}, context.Canceled
	}}
	got, _ := r.Run(context.Background(), engine.Request{Vendor: "claude-local", Brief: "brief"})
	if got.Tokens != 70001 {
		t.Fatalf("interrupted usage = %d, want 70001", got.Tokens)
	}
}

func TestClaudeProgressPermissionIsOneAbsoluteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes[1]", "progress-run-reviewer.md")
	var args []string
	r := claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
		var err error
		args, err = s.Args()
		// Without a role temp directory the file still exists during the round.
		if _, statErr := os.Stat(s.SystemPromptFile); statErr != nil {
			t.Errorf("system prompt file during the round: %v", statErr)
		}
		return claude.Round{Class: claude.ClassOK}, err
	}}
	_, err := r.Run(context.Background(), engine.Request{Vendor: "claude-local", Brief: "brief", ReadOnly: true, ProgressFile: path, Allowlist: []string{"Write"}})
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.Index(args, "--tools"); i < 0 || args[i+1] != "Read,Grep,Glob,Edit,Write,Bash" {
		t.Fatalf("a read-only role with a progress note needs Edit and Write: %q", args)
	}
	escaped := strings.ReplaceAll(strings.ReplaceAll(path, "[", `\[`), "]", `\]`)
	want := localReadOnlyTools + ",Edit(/" + escaped + ")"
	i := slices.Index(args, "--allowedTools")
	if i < 0 || args[i+1] != want {
		t.Fatalf("scoped progress permissions: %q, want %q", args, want)
	}
	i = slices.Index(args, "--add-dir")
	if i < 0 || args[i+1] != filepath.Dir(path) {
		t.Fatalf("missing progress working directory: %q", args)
	}
}

func TestClaudeReadOnlyRoleArguments(t *testing.T) {
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate, engine.RoleArchitect, engine.RoleSpecCheck, engine.RoleUI} {
		for _, vendor := range []string{"claude-local", "claude"} {
			t.Run(role+"/"+vendor, func(t *testing.T) {
				perm, err := engine.ResolvePermission(registry.Project{}, role)
				if err != nil {
					t.Fatal(err)
				}
				temp := t.TempDir()
				r := claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
					got, err := s.Args()
					if err != nil {
						t.Fatal(err)
					}
					want := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
					if vendor == "claude-local" {
						tools, slash := "Read,Grep,Glob,Bash", []string{"--disable-slash-commands"}
						if role == engine.RoleUI {
							// A UI pass loads skills (brief.go), so it keeps Skill.
							tools, slash = "Read,Grep,Glob,Bash,Skill", nil
						}
						want = append(want, "--system-prompt-file", filepath.Join(temp, "local-system.md"), "--tools", tools)
						want = append(want, slash...)
						allowed := localReadOnlyTools
						if role == engine.RoleSpecCheck {
							allowed += ",Bash(adj find*)"
						}
						want = append(want, "--allowedTools", allowed, "--permission-prompts", "none")
						want = append(want, "--permission-mode", "manual", "--settings", claude.HookSettings(""))
					} else if role == engine.RoleUI {
						// A cloud UI pass has no plan mode: a read-only allowlist plus
						// a write rule for its temp dir, and no other call (S369).
						want = append(want, "--add-dir", temp,
							"--allowedTools", localReadOnlyTools+",Skill,Edit(/"+temp+"/**),Write(/"+temp+"/**)",
							"--permission-prompts", "none")
					} else {
						want = append(want, "--permission-mode", "plan")
					}
					want = append(want, "review")
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("role %s argv: got %q; want %q", role, got, want)
					}
					return claude.Round{Class: claude.ClassOK}, nil
				}}
				_, err = r.Run(context.Background(), engine.Request{Role: role, Vendor: vendor, Brief: "review", TempDir: temp, ReadOnly: perm.ReadOnly, Allowlist: []string{"Edit", "Write", "Bash(git *)"}})
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestLocalDispatcherDoesNotInheritCloudVendorEnv(t *testing.T) {
	for _, vendor := range []string{"claude-local", "codex-local", "grok-local"} {
		t.Run(vendor, func(t *testing.T) {
			d, _, p := runDaemon(t, nil)
			d.runs.deps.Runner = nil
			dir := localLaunchers(t)
			t.Setenv("ORK_CAPTURE", filepath.Join(dir, "args"))
			capture := filepath.Join(dir, "env")
			t.Setenv("ORK_ENV_CAPTURE", capture)
			t.Setenv("ORK_CLOUD_ONLY", "daemon")
			p.VendorEnv = map[string]map[string]string{strings.TrimSuffix(vendor, "-local"): {"ORK_CLOUD_ONLY": "cloud override"}}
			_, err := d.runDeps(p).Runner.Run(context.Background(), engine.Request{Vendor: vendor, Brief: "brief", Worktree: dir, ReadOnly: true})
			if err != nil {
				t.Fatalf("local run: %v", err)
			}
			b, err := os.ReadFile(capture)
			if err != nil || string(b) != "daemon\n" {
				t.Fatalf("local environment: %q, %v", b, err)
			}
		})
	}
}

func TestLocalDispatcherSavesFixturesUnderLocalIdentity(t *testing.T) {
	for _, vendor := range []string{"claude-local", "codex-local", "grok-local"} {
		t.Run(vendor, func(t *testing.T) {
			d, _, p := runDaemon(t, nil)
			d.runs.deps.Runner = nil
			dir := localLaunchers(t)
			if err := os.WriteFile(filepath.Join(dir, vendor), []byte("#!/bin/sh\nprintf 'unclassified local failure\\n'\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			_, _ = d.runDeps(p).Runner.Run(context.Background(), engine.Request{Vendor: vendor, Brief: "brief", Worktree: dir, ReadOnly: true})
			entries, err := os.ReadDir(filepath.Join(d.cfg.Paths.Dir, "fixtures", vendor))
			if err != nil || len(entries) == 0 {
				t.Fatalf("local fixture candidates missing: %v", err)
			}
		})
	}
}

func TestLocalVendorSupportAndOwnerMarks(t *testing.T) {
	for _, vendor := range []string{"claude-local", "codex-local", "grok-local"} {
		t.Run(vendor+"/support", func(t *testing.T) {
			if !supportsVendor(vendor) {
				t.Fatalf("unsupported local vendor %s", vendor)
			}
		})
		t.Run(vendor+"/mark", func(t *testing.T) {
			d, _, _ := runDaemon(t, nil)
			got, err := d.setVendor(SetVendorParams{Vendor: vendor, Out: true})
			if err != nil || len(got.VendorsOut) != 1 || got.VendorsOut[0].Vendor != vendor {
				t.Fatalf("local owner mark: %+v, %v", got, err)
			}
		})
	}
}

func TestClaudeRunnerForwardsRequestAndResult(t *testing.T) {
	started := 0
	req := engine.Request{Vendor: "claude", Model: "sonnet", Effort: "high", Brief: "brief", Worktree: "/work", ReportPath: "/report", Allowlist: []string{"Read", "Write"}, Resume: "old", OnStart: func(p int) { started = p }}
	r := claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
		if s.Dir != req.Worktree || s.Prompt != req.Brief || s.Model != req.Model || s.Effort != req.Effort || s.ReportFile != req.ReportPath || s.Resume != req.Resume || !reflect.DeepEqual(s.Allowlist, req.Allowlist) || s.ReadOnly != req.ReadOnly {
			t.Fatalf("spec %+v", s)
		}
		if s.OnStart == nil {
			t.Fatal("missing OnStart")
		}
		s.OnStart(123)
		return claude.Round{Class: claude.ClassOK, SessionID: "new", Report: "report", Usage: claude.Usage{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheCreationTokens: 4, CostUSD: 0.25}}, nil
	}}
	got, err := r.Run(context.Background(), req)
	want := engine.Result{Exit: engine.ExitOK, Session: "new", Report: "report", Tokens: 10, CostUSD: 0.25}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("result %+v, %v", got, err)
	}
	if started != 123 {
		t.Fatalf("PGID %d", started)
	}
}
func TestClaudeRunnerRecordsTheCommandOfEachDenial(t *testing.T) {
	r := claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
		return claude.Round{Class: claude.ClassDenied, Denials: []claude.Denial{
			{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"git push origin main"}`)},
			{ToolName: "Write", ToolInput: json.RawMessage(`{"file_path":"/x.txt","content":"hi"}`)},
			{ToolName: "Glob", ToolInput: json.RawMessage(`{"pattern":"*"}`)},
		}}, nil
	}}
	got, err := r.Run(context.Background(), engine.Request{Vendor: "claude"})
	want := []runrec.DeniedCommand{{Tool: "Bash", Command: "git push origin main"}, {Tool: "Write", Command: "/x.txt"}, {Tool: "Glob"}}
	if err != nil || !reflect.DeepEqual(got.Denials, want) {
		t.Fatalf("denials %+v (%v), want %+v", got.Denials, err, want)
	}
}

func TestClaudeRunnerCapsTheCommandItHandsToTheRecord(t *testing.T) {
	long := strings.Repeat("x", 300)
	raw, _ := json.Marshal(map[string]string{"command": long})
	r := claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
		return claude.Round{Class: claude.ClassDenied, Denials: []claude.Denial{{ToolName: "Bash", ToolInput: raw}}}, nil
	}}
	got, _ := r.Run(context.Background(), engine.Request{Vendor: "claude"})
	if len(got.Denials) != 1 || got.Denials[0].Command != strings.Repeat("x", 197)+"..." {
		t.Fatalf("denials %+v, want one command of 197 x and ...", got.Denials)
	}
}

func TestClaudeRunnerMapsDenialsAndReadOnly(t *testing.T) {
	for _, class := range []claude.Class{claude.ClassOK, claude.ClassDenied, claude.ClassUnclassified, claude.ClassRateLimited} {
		r := claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
			if !s.ReadOnly {
				t.Fatal("read-only lost")
			}
			return claude.Round{Class: class, Denials: []claude.Denial{{ToolName: "Write"}}}, nil
		}}
		got, err := r.Run(context.Background(), engine.Request{Vendor: "claude", ReadOnly: true})
		want := engine.ExitError
		if class == claude.ClassOK {
			want = engine.ExitOK
		}
		if class == claude.ClassUnclassified {
			want = engine.ExitUnclassified
		}
		if class == claude.ClassRateLimited {
			want = engine.ExitRateLimited
		}
		if err != nil || got.Exit != want || !reflect.DeepEqual(got.Denials, []runrec.DeniedCommand{{Tool: "Write"}}) {
			t.Fatalf("class %s: %+v %v", class, got, err)
		}
	}
}

func TestCodexRunnerReturnsTheSameResultShape(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s' "$ORK_CODEX_OUTPUT"
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ORK_CODEX_OUTPUT", `{"type":"thread.started","thread_id":"session"}
{"type":"item.completed","item":{"type":"agent_message","text":"report"}}
{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":5,"output_tokens":2}}`)
	started := 0
	got, err := (codexRunner{}).Run(context.Background(), engine.Request{Vendor: "codex", Brief: "brief", Worktree: dir, Model: "model", Effort: "high", Allowlist: []string{"ignored"}, OnStart: func(pid int) { started = pid }})
	want := engine.Result{Exit: engine.ExitOK, Session: "session", Report: "report", Tokens: 12}
	if err != nil || !reflect.DeepEqual(got, want) || started <= 0 {
		t.Fatalf("result %+v %v, started %d", got, err, started)
	}
}
func TestDaemonRunnerDispatchesByVendor(t *testing.T) {
	for _, vendor := range []string{"claude", "codex", "grok", "acme"} {
		called := ""
		r := vendorRunner{claude: roundRunner(func(context.Context, engine.Request) (engine.Result, error) {
			called = "claude"
			return engine.Result{Session: "claude"}, nil
		}), codex: roundRunner(func(context.Context, engine.Request) (engine.Result, error) {
			called = "codex"
			return engine.Result{Session: "codex"}, nil
		}), grok: roundRunner(func(context.Context, engine.Request) (engine.Result, error) {
			called = "grok"
			return engine.Result{Session: "grok"}, nil
		})}
		got, err := r.Run(context.Background(), engine.Request{Vendor: vendor})
		if vendor == "acme" {
			if err == nil || !strings.Contains(err.Error(), "unsupported vendor") || called != "" {
				t.Fatalf("%+v %v called %s", got, err, called)
			}
		} else if err != nil || called != vendor || got.Session != vendor {
			t.Fatalf("%+v %v called %s", got, err, called)
		}
	}
}
func TestSupportsVendorNamesTheThreeAdapters(t *testing.T) {
	for vendor, want := range map[string]bool{"claude": true, "codex": true, "grok": true, "acme": false, "Grok": false, "": false} {
		if supportsVendor(vendor) != want {
			t.Errorf("supportsVendor(%q) = %v", vendor, !want)
		}
	}
}
func TestRunPlanAcceptsGrokOverride(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	useModelLists(d, map[string][]string{"grok": {"grok-4.7"}})
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"reviewer":[{"vendor":"grok","model":"grok-*@latest","effort":"high"}]}}`)
	if got := plan.Lineup[engine.RoleReviewer]; len(got) != 1 || got[0].Vendor != "grok" || got[0].Model != "grok-4.7" {
		t.Fatalf("%+v", plan.Lineup)
	}
}
func TestGrokRunnerForwardsRequestAndClasses(t *testing.T) {
	for _, class := range []grok.Exit{grok.ExitOK, grok.ExitOutOfCredits, grok.ExitUnclassified, grok.ExitError} {
		started := 0
		req := engine.Request{Vendor: "grok", Worktree: "/work", TempDir: "/temp", Brief: "brief", Model: "latest", Effort: "high", Resume: "sid", ReportPath: "/report", ReadOnly: true, OnStart: func(pid int) { started = pid }}
		sentinel := errors.New("round error")
		r := grokRunner{fixtureDir: "/state/fixtures/grok", env: []string{"KEY=value"}, round: func(_ context.Context, s grok.Spec) (grok.Round, error) {
			if s.Dir != req.Worktree || s.Prompt != req.Brief || s.Model != req.Model || s.Effort != req.Effort || s.Resume != req.Resume || s.ReportFile != req.ReportPath || s.FixtureDir != "/state/fixtures/grok" || !s.ReadOnly || !reflect.DeepEqual(s.Env, []string{"KEY=value"}) {
				t.Fatalf("spec %+v", s)
			}
			s.OnStart(123)
			return grok.Round{Exit: class, Reason: "why", Session: "session", Report: "report", Tokens: 12}, sentinel
		}}
		got, err := r.Run(context.Background(), req)
		want := engine.Result{Exit: engine.ExitClass(class), Reason: "why", Session: "session", Report: "report", Tokens: 12}
		if !errors.Is(err, sentinel) || !reflect.DeepEqual(got, want) || started != 123 {
			t.Fatalf("%s: %+v %v started %d", class, got, err, started)
		}
	}
}

// A write role on grok or grok-local passes the request's allowlist and runs
// in the slice worktree (S305, adj 373).
func TestGrokRunnerForwardsAWriteRolesAllowlist(t *testing.T) {
	for _, vendor := range []string{"grok", "grok-local"} {
		t.Run(vendor, func(t *testing.T) {
			allow := engine.DefaultImplementerAllowlist("go test ./...")
			req := engine.Request{Role: engine.RoleImplementer, Vendor: vendor, Worktree: "/work", Brief: "brief", Model: "latest", Effort: "high", Allowlist: allow}
			r := grokRunner{round: func(_ context.Context, s grok.Spec) (grok.Round, error) {
				if s.ReadOnly || s.Dir != "/work" || !reflect.DeepEqual(s.Allowlist, allow) || s.Bin != vendor {
					t.Fatalf("spec %+v", s)
				}
				return grok.Round{Exit: grok.ExitOK}, nil
			}}
			if got, err := r.Run(context.Background(), req); err != nil || got.Exit != engine.ExitOK {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

// The command the fake binary sees: dontAsk with the mapped rules, in the
// worktree, for both vendors; a read-only role gets plan mode and no rule.
func TestGrokRunnerBuildsTheWriteCommand(t *testing.T) {
	for _, vendor := range []string{"grok", "grok-local"} {
		for _, readOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/readonly=%v", vendor, readOnly), func(t *testing.T) {
				dir := t.TempDir()
				argsFile := filepath.Join(dir, "args")
				script := "#!/bin/sh\npwd > \"$ORK_GROK_ARGS\"\nprintf '%s\\n' \"$@\" >> \"$ORK_GROK_ARGS\"\nprintf '%s' \"$ORK_GROK_OUTPUT\"\n"
				if err := os.WriteFile(filepath.Join(dir, vendor), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				work := t.TempDir()
				t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin"+string(os.PathListSeparator)+"/usr/bin")
				t.Setenv("ORK_GROK_ARGS", argsFile)
				t.Setenv("ORK_GROK_OUTPUT", `{"type":"result","subtype":"success","is_error":false,"result":"report","usage":{"input_tokens":1,"output_tokens":1}}`)
				req := engine.Request{Vendor: vendor, Brief: "brief", Worktree: work, Model: "latest", Effort: "high", ReadOnly: readOnly, Allowlist: []string{"Edit", "Write", "Bash(go test*)"}}
				if readOnly {
					req.Allowlist = nil
				}
				got, err := (grokRunner{fixtureDir: filepath.Join(dir, "fixtures")}).Run(context.Background(), req)
				if err != nil || got.Exit != engine.ExitOK {
					t.Fatalf("result %+v %v", got, err)
				}
				b, err := os.ReadFile(argsFile)
				if err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(strings.TrimSpace(string(b)), "\n")
				wantDir, _ := filepath.EvalSymlinks(work)
				if gotDir, _ := filepath.EvalSymlinks(lines[0]); gotDir != wantDir {
					t.Errorf("ran in %q, want %q", lines[0], wantDir)
				}
				args := strings.Join(lines[1:], " ")
				if readOnly {
					if !strings.Contains(args, "--permission-mode plan") || strings.Contains(args, "--allow") {
						t.Errorf("read-only args %q", args)
					}
					return
				}
				if want := "--permission-mode dontAsk --allow search_replace --allow write --allow Bash(go test*) --cwd " + work; !strings.Contains(args, want) {
					t.Errorf("args %q lack %q", args, want)
				}
			})
		}
	}
}

func TestGrokRunnerStreamsTextAndTouchesTheWatchdog(t *testing.T) {
	var live []engine.Live
	events := 0
	r := grokRunner{round: func(_ context.Context, s grok.Spec) (grok.Round, error) {
		for _, line := range []string{
			`{"type":"system","subtype":"init","session_id":"s"}`,
			`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}}`,
			`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}}`,
			`{"type":"result","subtype":"success","is_error":false,"result":"hello","usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":5}}`,
		} {
			ev, err := grok.ParseLine([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			s.OnEvent(ev)
		}
		return grok.Round{Exit: grok.ExitOK}, nil
	}}
	_, err := r.Run(context.Background(), engine.Request{Vendor: "grok", OnLive: func(l engine.Live) { live = append(live, l) }, OnEvent: func() { events++ }})
	var text strings.Builder
	tokens := 0
	for _, l := range live {
		text.WriteString(l.Text)
		if l.UsageKnown {
			tokens += l.Tokens
		}
	}
	if err != nil || events != 4 || text.String() != "hello" || tokens != 17 {
		t.Fatalf("events %d text %q tokens %d: %v", events, text.String(), tokens, err)
	}
}
func TestGrokRunnerRunsTheFakeBinaryEndToEnd(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s' "$ORK_GROK_OUTPUT"
`
	if err := os.WriteFile(filepath.Join(dir, "grok"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ORK_GROK_OUTPUT", `{"type":"system","subtype":"init","session_id":"session"}
{"type":"result","subtype":"success","is_error":false,"result":"report","usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":5}}`)
	got, err := (grokRunner{fixtureDir: filepath.Join(dir, "fixtures")}).Run(context.Background(), engine.Request{Vendor: "grok", Brief: "brief", Worktree: dir, Model: "latest", Effort: "high", ReadOnly: true})
	want := engine.Result{Exit: engine.ExitOK, Session: "session", Report: "report", Tokens: 17}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("result %+v %v", got, err)
	}
}
func TestRunPlanAcceptsCodexOverride(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"codex","model":"gpt","effort":"high"}]}}`)
	if plan.Lineup[engine.RoleImplementer][0].Vendor != "codex" {
		t.Fatalf("%+v", plan.Lineup)
	}
}

func TestCodexRunnerForwardsRequestAndClasses(t *testing.T) {
	for _, class := range []codex.Exit{codex.ExitOK, codex.ExitRateLimited, codex.ExitError} {
		started := 0
		req := engine.Request{Vendor: "codex", Worktree: "/work", Brief: "brief", Model: "model", Effort: "high", Resume: "thread", ReportPath: "/report", ReadOnly: true, OnStart: func(pid int) { started = pid }}
		sentinel := errors.New("round error")
		r := codexRunner{fixtureDir: "/state/fixtures/codex", env: []string{"KEY=value"}, round: func(_ context.Context, s codex.Spec) (codex.Round, error) {
			if s.Dir != req.Worktree || s.Prompt != req.Brief || s.Model != req.Model || s.Effort != req.Effort || s.Resume != req.Resume || s.ReportFile != req.ReportPath || s.FixtureDir != "/state/fixtures/codex" || !s.ReadOnly || !reflect.DeepEqual(s.Env, []string{"KEY=value"}) {
				t.Fatalf("spec %+v", s)
			}
			if s.OnStart == nil {
				t.Fatal("missing OnStart")
			}
			s.OnStart(123)
			return codex.Round{Exit: class, Session: "session", Report: "report", Tokens: 12}, sentinel
		}}
		got, err := r.Run(context.Background(), req)
		want := engine.Result{Exit: engine.ExitClass(class), Session: "session", Report: "report", Tokens: 12}
		if !errors.Is(err, sentinel) || !reflect.DeepEqual(got, want) || started != 123 {
			t.Fatalf("%+v %v started %d", got, err, started)
		}
	}
}
func TestRunDepsWiresBothVendorEnvironments(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	d.runs.deps.Runner = nil
	p.VendorEnv = map[string]map[string]string{"claude": {"ORK_VENDOR": "claude"}, "codex": {"ORK_VENDOR": "codex"}}
	r, ok := d.runDeps(p).Runner.(vendorRunner)
	if !ok {
		t.Fatal("missing dispatcher")
	}
	c, ok := r.claude.(claudeRunner)
	if !ok || !slices.Contains(c.env, "ORK_VENDOR=claude") {
		t.Fatalf("claude %+v", r.claude)
	}
	x, ok := r.codex.(codexRunner)
	if !ok || x.fixtureDir != filepath.Join(d.cfg.Paths.Dir, "fixtures", "codex") || !slices.Contains(x.env, "ORK_VENDOR=codex") || slices.Contains(x.env, "ORK_VENDOR=claude") {
		t.Fatalf("codex %+v", r.codex)
	}
}

func TestClaudeAndCodexRunnersMapUnclassifiedToTheUnclassifiedClass(t *testing.T) {
	for _, vendor := range []string{"claude", "codex"} {
		t.Run(vendor, func(t *testing.T) {
			var runner engine.Runner
			if vendor == "claude" {
				runner = claudeRunner{round: func(context.Context, claude.Spec) (claude.Round, error) {
					return claude.Round{Class: claude.ClassUnclassified}, nil
				}}
			} else {
				runner = codexRunner{round: func(context.Context, codex.Spec) (codex.Round, error) {
					return codex.Round{Exit: codex.ExitUnclassified}, nil
				}}
			}
			got, err := runner.Run(context.Background(), engine.Request{Vendor: vendor})
			if err != nil || got.Exit != engine.ExitUnclassified {
				t.Fatalf("exit = %q, want unclassified; err = %v", got.Exit, err)
			}
		})
	}
}

func TestRunnersForwardStreamEvents(t *testing.T) {
	for _, vendor := range []string{"claude", "codex"} {
		t.Run(vendor, func(t *testing.T) {
			events := 0
			req := engine.Request{Vendor: vendor, OnEvent: func() { events++ }}
			var r engine.Runner
			if vendor == "claude" {
				r = claudeRunner{round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
					if s.OnEvent == nil {
						t.Fatal("missing event hook")
					}
					s.OnEvent(claude.Event{})
					s.OnEvent(claude.Event{})
					return claude.Round{}, nil
				}}
			} else {
				r = codexRunner{round: func(_ context.Context, s codex.Spec) (codex.Round, error) {
					if s.OnEvent == nil {
						t.Fatal("missing event hook")
					}
					s.OnEvent(codex.Event{})
					s.OnEvent(codex.Event{})
					return codex.Round{}, nil
				}}
			}
			r.Run(context.Background(), req)
			if events != 2 {
				t.Fatalf("events %d", events)
			}
			req.OnEvent = nil
			r.Run(context.Background(), req)
		})
	}
}

func TestCodexRoundEnvSetsGOCACHEAndTMPDIR(t *testing.T) {
	t.Setenv("ORK_S87_ENV", "inherited")
	for _, tc := range []struct {
		name string
		env  []string
		temp string
	}{
		{"explicit", []string{"KEY=value"}, "/temp"},
		{"replace", []string{"KEY=value", "GOCACHE=/old", "TMPDIR=/old", "GOCACHE=/older", "TMPDIR=/older"}, "/temp"},
		{"inherit", nil, "/temp"},
		{"empty temp explicit", []string{"KEY=value", "GOCACHE=/old"}, ""},
		{"empty temp inherited", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]string(nil), tc.env...)
			base := tc.env
			if base == nil {
				base = os.Environ()
			}
			var want []string
			if tc.temp == "" {
				want = tc.env
			} else {
				for _, entry := range base {
					if !strings.HasPrefix(entry, "GOCACHE=") && !strings.HasPrefix(entry, "TMPDIR=") {
						want = append(want, entry)
					}
				}
				want = append(want, "GOCACHE="+filepath.Join(tc.temp, "gocache"), "TMPDIR="+tc.temp)
			}
			called := false
			runner := codexRunner{env: tc.env, round: func(_ context.Context, s codex.Spec) (codex.Round, error) {
				called = true
				if s.AddDir != tc.temp {
					t.Errorf("AddDir %q, want %q", s.AddDir, tc.temp)
				}
				if !reflect.DeepEqual(s.Env, want) {
					t.Errorf("environment %v, want inherited entries with GOCACHE and TMPDIR replaced: %v", s.Env, want)
				}
				if len(s.Env) > 0 {
					s.Env[0] = "MUTATED=value"
				}
				return codex.Round{Exit: codex.ExitOK}, nil
			}}
			_, err := runner.Run(context.Background(), engine.Request{Vendor: "codex", TempDir: tc.temp})
			if err != nil || !called {
				t.Fatalf("round called %v, err %v", called, err)
			}
			if tc.temp != "" && !reflect.DeepEqual(tc.env, before) {
				t.Error("base environment mutated")
			}
		})
	}
}

// S360 (*proposed*): a claude-local role gets a Bash output cap in its
// environment, replacing an inherited value, with or without a temp directory.
// For a cloud role ork does not set or replace the variable, so an inherited
// value stays.
func TestClaudeLocalEnvCapsBashOutput(t *testing.T) {
	base := []string{"KEY=value", "BASH_MAX_OUTPUT_LENGTH=150000"}
	for _, tc := range []struct {
		vendor, temp string
		want         []string
	}{
		{"claude-local", "/temp", []string{"KEY=value", "BASH_MAX_OUTPUT_LENGTH=10000", "TMPDIR=/temp"}},
		{"claude-local", "", []string{"KEY=value", "BASH_MAX_OUTPUT_LENGTH=10000"}},
		{"claude", "/temp", []string{"KEY=value", "BASH_MAX_OUTPUT_LENGTH=150000", "TMPDIR=/temp"}},
		{"claude", "", base},
	} {
		t.Run(tc.vendor+"/"+tc.temp, func(t *testing.T) {
			// A claude-local round writes its system prompt into the temp
			// directory, so "/temp" stands for a real directory.
			temp, want := tc.temp, slices.Clone(tc.want)
			if temp != "" {
				temp = t.TempDir()
				for i := range want {
					want[i] = strings.ReplaceAll(want[i], "/temp", temp)
				}
			}
			runner := claudeRunner{env: base, round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
				if !reflect.DeepEqual(s.Env, want) {
					t.Errorf("environment %v, want %v", s.Env, want)
				}
				return claude.Round{Class: claude.ClassOK}, nil
			}}
			if _, err := runner.Run(context.Background(), engine.Request{Vendor: tc.vendor, TempDir: temp, ReadOnly: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// S271: a Claude role's process sees TMPDIR as its own temp directory, so it
// needs no "env TMPDIR=DIR" prefix (which no allowlist pattern matches) and no
// mkdir. The owner's vendor_env TMPDIR does not win over the role's directory.
func TestClaudeRoundEnvSetsTMPDIR(t *testing.T) {
	t.Setenv("ORK_S271_ENV", "inherited")
	for _, tc := range []struct {
		name string
		env  []string
		temp string
	}{
		{"explicit", []string{"KEY=value"}, "/temp"},
		{"replace", []string{"KEY=value", "TMPDIR=/old", "TMPDIR=/older"}, "/temp"},
		{"inherit", nil, "/temp"},
		{"empty temp explicit", []string{"KEY=value", "TMPDIR=/old"}, ""},
		{"empty temp inherited", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]string(nil), tc.env...)
			base := tc.env
			if base == nil {
				base = os.Environ()
			}
			want := tc.env
			if tc.temp != "" {
				want = nil
				for _, entry := range base {
					if !strings.HasPrefix(entry, "TMPDIR=") {
						want = append(want, entry)
					}
				}
				want = append(want, "TMPDIR="+tc.temp)
			}
			called := false
			runner := claudeRunner{env: tc.env, round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
				called = true
				if !reflect.DeepEqual(s.Env, want) {
					t.Errorf("environment %v, want %v", s.Env, want)
				}
				for _, entry := range s.Env {
					if strings.HasPrefix(entry, "GOCACHE=") && !slices.Contains(base, entry) {
						t.Errorf("Claude role got %q: only TMPDIR is set", entry)
					}
				}
				if len(s.Env) > 0 {
					s.Env[0] = "MUTATED=value"
				}
				return claude.Round{Class: claude.ClassOK}, nil
			}}
			_, err := runner.Run(context.Background(), engine.Request{Vendor: "claude", TempDir: tc.temp})
			if err != nil || !called {
				t.Fatalf("round called %v, err %v", called, err)
			}
			if tc.temp != "" && !reflect.DeepEqual(tc.env, before) {
				t.Error("base environment mutated")
			}
		})
	}
}

// The dispatcher and runDeps carry the temp directory to the Claude process
// even when the project sets vendor_env TMPDIR for claude.
func TestRunDepsClaudeRoleGetsItsTempDirAsTMPDIR(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	d.runs.deps.Runner = nil
	p.VendorEnv = map[string]map[string]string{"claude": {"TMPDIR": "/owner"}}
	r, ok := d.runDeps(p).Runner.(vendorRunner)
	if !ok {
		t.Fatal("missing dispatcher")
	}
	c := r.claude.(claudeRunner)
	var got []string
	c.round = func(_ context.Context, s claude.Spec) (claude.Round, error) {
		got = s.Env
		return claude.Round{Class: claude.ClassOK}, nil
	}
	r.claude = c
	if _, err := r.Run(context.Background(), engine.Request{Vendor: "claude", TempDir: "/role/tmp"}); err != nil {
		t.Fatal(err)
	}
	var tmpdirs []string
	for _, entry := range got {
		if strings.HasPrefix(entry, "TMPDIR=") {
			tmpdirs = append(tmpdirs, entry)
		}
	}
	if !reflect.DeepEqual(tmpdirs, []string{"TMPDIR=/role/tmp"}) {
		t.Fatalf("TMPDIR entries %v, want only the role temp directory", tmpdirs)
	}
}
