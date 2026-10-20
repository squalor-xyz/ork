// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"reflect"
	"testing"

	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/engine"
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
		if err != nil || got.Exit != want || !reflect.DeepEqual(got.Denials, []string{"Write"}) {
			t.Fatalf("class %s: %+v %v", class, got, err)
		}
	}
}
