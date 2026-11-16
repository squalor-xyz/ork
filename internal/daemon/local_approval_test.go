// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

func approvalRun(t *testing.T) (*Daemon, engine.Request) {
	t.Helper()
	d, store, p := runDaemon(t, nil)
	rec := runrec.Record{ID: "permission-run", Project: p.Name, Item: "S1", State: runrec.StateImplement}
	run, err := store.Create(rec)
	if err != nil {
		t.Fatal(err)
	}
	d.runs.runs[rec.ID] = &managedRun{engine: engine.Load(d.runs.deps, p, run)}
	return d, engine.Request{RunID: rec.ID, Role: "implementer", Round: 1, Vendor: "claude-local", Worktree: t.TempDir(), Brief: "work", Allowlist: []string{"Read"}}
}

func waitPermission(t *testing.T, d *Daemon, run string) source.EngineRun {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		for _, r := range d.engineRuns("p") {
			if r.ID == run && r.Question != nil {
				return r
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("permission question not visible on run")
	return source.EngineRun{}
}

func TestApprovalOwnerRoundTrip(t *testing.T) {
	for _, answer := range []string{"allow", "deny"} {
		t.Run(answer, func(t *testing.T) {
			d, req := approvalRun(t)
			token, finish := d.registerApproval(context.Background(), req, claude.ApprovalPolicy{Worktree: req.Worktree, Allowlist: req.Allowlist})
			defer finish()
			reply := make(chan claude.Approval, 1)
			go func() {
				a, _ := approvalRequest(d, context.Background(), ApprovalParams{Session: token, Call: claude.ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make test"}`)}})
				reply <- a
			}()
			visible := waitPermission(t, d, req.RunID)
			if visible.State == runrec.StatePaused || visible.Section([]source.EngineRun{visible}) != source.SectionNeeds || !strings.Contains(visible.Wait, "make test") {
				t.Fatalf("live question state: %+v", visible)
			}
			d.runs.mu.Lock()
			d.runs.runs[req.RunID].busy = true
			d.runs.mu.Unlock()
			_, err := d.ownerRunReply(context.Background(), MethodRunAnswer, RunAction{Run: req.RunID, Text: answer})
			d.runs.mu.Lock()
			d.runs.runs[req.RunID].busy = false
			d.runs.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			select {
			case a := <-reply:
				if a.Decision != answer {
					t.Fatalf("owner decision %+v", a)
				}
			case <-time.After(time.Second):
				t.Fatal("hook did not receive answer")
			}
			for _, r := range d.engineRuns("p") {
				if r.ID == req.RunID && (r.Question != nil || r.Pending == "permission") {
					t.Fatalf("stale permission: %+v", r)
				}
			}
		})
	}
}

func TestApprovalQuestionDetails(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		long              bool
	}{
		{"todo", `{"todos":[{"content":"Review the change","status":"pending"}]}`, `"todos":[{"content":"Review the change"`, false},
		{"pattern", `{"pattern":"needle"}`, `"pattern":"needle"`, false},
		{"path", `{"path":"src"}`, `"path":"src"`, false},
		{"url", `{"url":"https://example.test/doc"}`, `"url":"https://example.test/doc"`, false},
		{"bounded unicode", `{"todos":[{"content":` + quoteJSON(strings.Repeat("界", 250)) + `}]}`, `"todos":[{"content":"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, req := approvalRun(t)
			token, finish := d.registerApproval(context.Background(), req, claude.ApprovalPolicy{Worktree: req.Worktree, Allowlist: req.Allowlist})
			defer finish()
			reply := make(chan claude.Approval, 1)
			go func() {
				out, _ := approvalRequest(d, context.Background(), ApprovalParams{Session: token, Call: claude.ToolCall{ToolName: "TodoWrite", ToolInput: json.RawMessage(tc.input)}})
				reply <- out
			}()
			q := waitPermission(t, d, req.RunID)
			// S370: the title is a preview of at most 120 characters; Detail is
			// the whole input, never cut.
			if !strings.Contains(q.Question.Detail, tc.want) {
				t.Errorf("permission detail lacks tool input: %q", q.Question.Detail)
			}
			if !strings.HasPrefix(q.Question.Title, "Allow TodoWrite: ") || len([]rune(q.Question.Title)) > 120 {
				t.Errorf("permission title is not a short preview: %q", q.Question.Title)
			}
			if tc.long && (len([]rune(q.Question.Detail)) <= 200 || strings.HasSuffix(q.Question.Detail, "...") || !strings.HasSuffix(q.Question.Title, "...? (call 1)")) {
				t.Fatalf("long input: detail %d runes, title %q", len([]rune(q.Question.Detail)), q.Question.Title)
			}
		})
	}
}

func TestApprovalTimeout(t *testing.T) {
	for _, how := range []string{"deadline", "role ends", "request canceled"} {
		t.Run(how, func(t *testing.T) {
			d, req := approvalRun(t)
			token, finish := d.registerApproval(context.Background(), req, claude.ApprovalPolicy{Worktree: req.Worktree})
			defer finish()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reply := make(chan claude.Approval, 1)
			deadline := time.Time{}
			if how == "deadline" {
				deadline = time.Now().Add(100 * time.Millisecond)
			}
			go func() {
				a, _ := approvalRequest(d, ctx, ApprovalParams{Session: token, Deadline: deadline, Call: claude.ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make test"}`)}})
				reply <- a
			}()
			waitPermission(t, d, req.RunID)
			if how == "request canceled" {
				cancel()
			} else if how == "role ends" {
				finish()
			}
			select {
			case a := <-reply:
				if a.Decision != "deny" {
					t.Fatalf("expired hook %+v", a)
				}
			case <-time.After(time.Second):
				t.Fatal("expired hook blocked")
			}
			for _, r := range d.engineRuns("p") {
				if r.ID == req.RunID && r.Question != nil {
					t.Fatal("expired question retained")
				}
			}
		})
	}
}

func TestApprovalFakeClaudeDenialContinues(t *testing.T) {
	d, store, project := runDaemon(t, nil)
	runner := claudeRunner{approval: d.prepareApproval, round: func(ctx context.Context, s claude.Spec) (claude.Round, error) {
		path := filepath.Join(s.Dir, "long.go")
		if err := os.MkdirAll(s.Dir, 0700); err != nil {
			return claude.Round{}, err
		}
		if err := os.WriteFile(path, []byte(strings.Repeat("line\n", 501)), 0600); err != nil {
			return claude.Round{}, err
		}
		// The fake CLI takes the session from the hook command it was launched with.
		words := strings.Fields(s.HookCommand)
		if len(words) == 0 {
			return claude.Round{}, fmt.Errorf("local runner did not wire approval hook")
		}
		token := strings.Trim(words[len(words)-1], "'")
		got, err := approvalRequest(d, ctx, ApprovalParams{Session: token, Call: claude.ToolCall{ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":` + quoteJSON(path) + `}`)}})
		if err != nil || got.Decision != "deny" || !strings.Contains(got.Reason, "501") {
			return claude.Round{}, fmt.Errorf("hook result %+v, %v", got, err)
		}
		return claude.Round{Class: claude.ClassOK, Report: "Tests: 1 passed, 0 failed\n\n## Owner needed\nnone\n"}, nil
	}}
	d.runs.deps.Runner = roundRunner(func(ctx context.Context, req engine.Request) (engine.Result, error) {
		if req.Role == engine.RoleImplementer {
			return runner.Run(ctx, req)
		}
		return passRunner()(ctx, req)
	})
	rec, err := d.startRun(context.Background(), RunParams{Project: project.Name, Item: "S1", Lineup: map[string][]runrec.LineupEntry{engine.RoleImplementer: {{Vendor: "claude-local", Model: "local", Effort: "medium"}}}})
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, d, rec.ID)
	stored, err := store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := stored.Snapshot()
	if got.State != runrec.StateAwaitingApproval || len(got.Roles) == 0 || len(got.Roles[0].DeniedCommands) != 1 || got.Roles[0].DeniedCommands[0].Command != filepath.Join(got.Worktree, "long.go") {
		t.Fatalf("successful run lost hook denial: %+v", got)
	}
}

func quoteJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

func approvalRequest(d *Daemon, ctx context.Context, p ApprovalParams) (claude.Approval, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return claude.Approval{}, err
	}
	result, err := d.handle(ctx, MethodToolApprove, raw)
	if err != nil {
		return claude.Approval{}, err
	}
	answer, ok := result.(claude.Approval)
	if !ok {
		return claude.Approval{}, fmt.Errorf("unexpected approval reply %T", result)
	}
	return answer, nil
}
