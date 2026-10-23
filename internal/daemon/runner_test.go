// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/agent/codex"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

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
	for _, vendor := range []string{"claude", "codex", "grok"} {
		called := ""
		r := vendorRunner{claude: roundRunner(func(context.Context, engine.Request) (engine.Result, error) {
			called = "claude"
			return engine.Result{Session: "claude"}, nil
		}), codex: roundRunner(func(context.Context, engine.Request) (engine.Result, error) {
			called = "codex"
			return engine.Result{Session: "codex"}, nil
		})}
		got, err := r.Run(context.Background(), engine.Request{Vendor: vendor})
		if vendor == "grok" {
			if err == nil || !strings.Contains(err.Error(), "unsupported vendor") || called != "" {
				t.Fatalf("%+v %v called %s", got, err, called)
			}
		} else if err != nil || called != vendor || got.Session != vendor {
			t.Fatalf("%+v %v called %s", got, err, called)
		}
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
