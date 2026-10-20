// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/agent/claude"
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
		if class == claude.ClassRateLimited {
			want = engine.ExitRateLimited
		}
		if err != nil || got.Exit != want || !reflect.DeepEqual(got.Denials, []runrec.DeniedCommand{{Tool: "Write"}}) {
			t.Fatalf("class %s: %+v %v", class, got, err)
		}
	}
}
