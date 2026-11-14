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
)

func TestApprovalAssignedPathsWired(t *testing.T) {
	for _, tc := range []struct {
		name, tool, path, want, reason string
		readonly                       bool
	}{
		{"temp read", "Read", "temp", "allow", "", false},
		{"temp bash", "Bash", "temp", "allow", "", false},
		{"report write", "Write", "report", "allow", "", false},
		{"report cap", "Read", "report", "deny", "501 lines", false},
		{"report sibling", "Write", "sibling", "deny", "outside", false},
		{"report child", "Write", "child", "deny", "outside", false},
		{"readonly report", "Write", "report", "ask", "allowlist", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, req := approvalRun(t)
			req.TempDir = t.TempDir()
			req.ReportPath = filepath.Join(t.TempDir(), "report.md")
			req.ReadOnly = tc.readonly
			req.Allowlist = []string{"Read", "Write", "Bash(go test*)"}
			if err := os.WriteFile(req.ReportPath, []byte(strings.Repeat("line\n", 501)), 0600); err != nil {
				t.Fatal(err)
			}
			path := map[string]string{"temp": filepath.Join(req.TempDir, "output.txt"), "report": req.ReportPath, "sibling": filepath.Join(filepath.Dir(req.ReportPath), "other.md"), "child": filepath.Join(req.ReportPath, "child")}[tc.path]
			input := json.RawMessage(`{"file_path":` + quoteJSON(path) + `}`)
			if tc.path == "temp" {
				input = json.RawMessage(`{"file_path":` + quoteJSON(path) + `,"limit":1}`)
			}
			if tc.tool == "Bash" {
				input = json.RawMessage(`{"command":` + quoteJSON("go test "+path) + `}`)
			}
			var got claude.Approval
			runner := claudeRunner{approval: d.prepareApproval, round: func(_ context.Context, s claude.Spec) (claude.Round, error) {
				words := strings.Fields(s.HookCommand)
				token := strings.Trim(words[len(words)-1], "'")
				d.runs.mu.Lock()
				policy := d.runs.approvals[token].policy
				d.runs.mu.Unlock()
				got = policy.Evaluate(claude.ToolCall{ToolName: tc.tool, ToolInput: input})
				return claude.Round{Class: claude.ClassOK}, nil
			}}
			if _, err := runner.Run(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if got.Decision != tc.want || !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("assigned path approval %+v; want %s with %q", got, tc.want, tc.reason)
			}
		})
	}
}

func specPermissionRequest(d *Daemon, ctx context.Context, p SpecPermissionParams) (*SpecPermissionResult, error) {
	raw, _ := json.Marshal(p)
	result, err := d.handle(ctx, MethodSpecPermission, raw)
	if err != nil {
		return nil, err
	}
	out, ok := result.(*SpecPermissionResult)
	if !ok {
		return nil, fmt.Errorf("unexpected permission result %T", result)
	}
	return out, nil
}

func waitSpecPermission(t *testing.T, d *Daemon, p SpecPermissionParams) *SpecPermissionResult {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		out, err := specPermissionRequest(d, context.Background(), p)
		if err == nil && out.Question != nil {
			return out
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("spec-check permission question not visible")
	return nil
}

func TestSpecApprovalCompletesCheck(t *testing.T) {
	rig := newSpecRig(t, rulesJSON(u11))
	replies := make(chan claude.Approval, 1)
	runner := claudeRunner{approval: rig.d.prepareApproval, round: func(ctx context.Context, s claude.Spec) (claude.Round, error) {
		words := strings.Fields(s.HookCommand)
		token := strings.Trim(words[len(words)-1], "'")
		answer, err := approvalRequest(rig.d, ctx, ApprovalParams{Session: token, Call: claude.ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make test"}`)}})
		replies <- answer
		if err != nil {
			return claude.Round{}, err
		}
		return claude.Round{Class: claude.ClassOK, Report: "## Slice S1\nReady\n\n## Owner needed\nnone\n"}, nil
	}}
	rig.d.runs.deps.Runner = roundRunner(func(ctx context.Context, req engine.Request) (engine.Result, error) {
		req.Vendor = "claude-local"
		return runner.Run(ctx, req)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		value any
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := rig.d.handle(ctx, MethodSpecCheck, json.RawMessage(`{"project":"p","items":["S1"]}`))
		done <- result{value, err}
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("spec-check job did not stop")
		}
	}()
	p := SpecPermissionParams{Project: "p", Items: []string{"S1"}}
	question := waitSpecPermission(t, rig.d, p)
	if !strings.Contains(question.Question.Title, "make test") {
		t.Fatalf("wrong live question: %+v", question)
	}
	p.ID = question.ID
	p.Text = "allow"
	if _, err := specPermissionRequest(rig.d, ctx, p); err != nil {
		t.Fatal(err)
	}
	select {
	case answer := <-replies:
		if answer.Decision != "allow" {
			t.Fatalf("spec hook reply %+v", answer)
		}
	case <-time.After(time.Second):
		t.Fatal("spec hook remained blocked")
	}
	select {
	case res := <-done:
		done <- res
		out, ok := res.value.(*SpecCheckResult)
		if res.err != nil || !ok || len(out.Items) != 1 || !out.Items[0].Ready {
			t.Fatalf("spec check did not continue to Ready: %+v, %v", res.value, res.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("spec check did not complete")
	}
}

func TestSpecApprovalReplyScope(t *testing.T) {
	for _, mode := range []string{"deny", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			d, _ := approvalRun(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := engine.Request{Role: engine.RoleSpecCheck, SpecProject: "p", SpecItems: []string{"S1"}, Worktree: t.TempDir()}
			token, finish := d.registerApproval(ctx, req, claude.ApprovalPolicy{Worktree: req.Worktree})
			defer finish()
			replies := make(chan claude.Approval, 1)
			go func() {
				out, _ := approvalRequest(d, context.Background(), ApprovalParams{Session: token, Call: claude.ToolCall{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make test"}`)}})
				replies <- out
			}()
			p := SpecPermissionParams{Project: "p", Items: []string{"S1"}}
			question := waitSpecPermission(t, d, p)
			for _, wrong := range []SpecPermissionParams{{Project: "other", Items: p.Items, ID: question.ID, Text: "allow"}, {Project: p.Project, Items: []string{"S2"}, ID: question.ID, Text: "allow"}, {Project: p.Project, Items: p.Items, ID: question.ID + 1, Text: "allow"}} {
				if _, err := specPermissionRequest(d, ctx, wrong); err == nil {
					t.Fatalf("accepted stale or wrong-scope answer: %+v", wrong)
				}
			}
			if mode == "cancel" {
				cancel()
			} else {
				p.ID = question.ID
				p.Text = "deny"
				if _, err := specPermissionRequest(d, ctx, p); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case out := <-replies:
				if out.Decision != "deny" {
					t.Fatalf("spec permission reply %+v", out)
				}
			case <-time.After(time.Second):
				t.Fatal("spec permission remained blocked")
			}
			p.ID = 0
			p.Text = ""
			out, err := specPermissionRequest(d, context.Background(), p)
			if err != nil || out.Question != nil {
				t.Fatalf("stale spec question: %+v, %v", out, err)
			}
		})
	}
}
