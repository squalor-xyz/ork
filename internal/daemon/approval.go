// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

type ApprovalParams struct {
	Session  string          `json:"session"`
	Call     claude.ToolCall `json:"call"`
	Deadline time.Time       `json:"deadline,omitempty"`
}

const MethodToolApprove = "tool.approve"
const MethodSpecPermission = "spec.permission"

type SpecPermissionParams struct {
	Project string   `json:"project"`
	Items   []string `json:"items"`
	ID      uint64   `json:"id,omitempty"`
	Text    string   `json:"text,omitempty"`
}
type SpecPermissionResult struct {
	ID       uint64                `json:"id,omitempty"`
	Question *runrec.OwnerQuestion `json:"question,omitempty"`
}

// specPermission bypasses specMu: the role holding it may be waiting for this
// answer. A call id prevents an old sitting from answering a newer tool call.
func (d *Daemon) specPermission(ctx context.Context, p SpecPermissionParams) (*SpecPermissionResult, error) {
	if p.Project == "" || len(p.Items) == 0 {
		return nil, badParams(fmt.Errorf("spec permission needs a project and items"))
	}
	if p.ID == 0 && p.Text != "" {
		return nil, badParams(fmt.Errorf("spec permission answer needs a call id"))
	}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	if d.runs.closed {
		return nil, fmt.Errorf("run manager is closed")
	}
	var first *permissionWait
	for _, s := range d.runs.approvals {
		if s.req.Role != engine.RoleSpecCheck || s.req.SpecProject != p.Project || len(s.req.SpecItems) == 0 {
			continue
		}
		covered := true
		for _, item := range s.req.SpecItems {
			if !slices.Contains(p.Items, item) {
				covered = false
				break
			}
		}
		if !covered {
			continue
		}
		for _, w := range s.waiting {
			if w.answered {
				continue
			}
			if p.ID != 0 && w.seq != p.ID {
				continue
			}
			if first == nil || w.seq < first.seq {
				first = w
			}
		}
	}
	if p.ID != 0 {
		if first == nil {
			return nil, fmt.Errorf("spec permission question is no longer active")
		}
		answer, err := permissionAnswer(p.Text)
		if err != nil {
			return nil, err
		}
		select {
		case first.reply <- answer:
		default:
			return nil, fmt.Errorf("spec permission answer is already being delivered")
		}
		first.answered = true
		return &SpecPermissionResult{}, nil
	}
	if first == nil {
		return &SpecPermissionResult{}, nil
	}
	return &SpecPermissionResult{ID: first.seq, Question: first.question}, nil
}

const approvalTimeout = 30 * time.Minute

type approvalSession struct {
	ctx     context.Context
	req     engine.Request
	policy  claude.ApprovalPolicy
	done    chan struct{}
	denials []runrec.DeniedCommand
	waiting []*permissionWait
	calls   sync.WaitGroup
}
type permissionWait struct {
	seq      uint64
	answered bool
	question *runrec.OwnerQuestion
	reply    chan claude.Approval
}

// registerApproval binds the policy to one role attempt. The model supplies
// only a tool call, never the allowlist or worktree used to approve it.
func (d *Daemon) registerApproval(ctx context.Context, req engine.Request, p claude.ApprovalPolicy) (string, func() []runrec.DeniedCommand) {
	token := rand.Text()
	s := &approvalSession{ctx: ctx, req: req, policy: p, done: make(chan struct{})}
	d.runs.mu.Lock()
	if d.runs.approvals == nil {
		d.runs.approvals = map[string]*approvalSession{}
	}
	d.runs.approvals[token] = s
	d.runs.mu.Unlock()
	return token, func() []runrec.DeniedCommand {
		d.runs.mu.Lock()
		if d.runs.approvals[token] != nil {
			delete(d.runs.approvals, token)
			close(s.done)
		}
		d.runs.mu.Unlock()
		s.calls.Wait()
		d.runs.mu.Lock()
		defer d.runs.mu.Unlock()
		return append([]runrec.DeniedCommand(nil), s.denials...)
	}
}

func shellQuoteHook(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func (d *Daemon) prepareApproval(ctx context.Context, req engine.Request, allow []string, addDir string) (string, func() []runrec.DeniedCommand, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	scratch := ""
	if req.ProjectRoot != "" {
		scratch = filepath.Join(req.ProjectRoot, ".scratch")
	}
	token, finish := d.registerApproval(ctx, req, claude.ApprovalPolicy{Worktree: req.Worktree, Scratch: scratch, AddDir: addDir, TempDir: req.TempDir, TMPDir: req.TMPDir, ReportFile: req.ReportPath, Allowlist: append([]string(nil), allow...), Role: req.Role})
	return shellQuoteHook(exe) + " hook pretool --socket " + shellQuoteHook(d.cfg.Paths.Socket) + " --session " + shellQuoteHook(token), finish, nil
}

// firstPermissionLocked projects a live question without pausing the engine.
func (d *Daemon) firstPermissionLocked(run string) *permissionWait {
	var first *permissionWait
	for _, s := range d.runs.approvals {
		if s.req.RunID == run {
			for _, w := range s.waiting {
				if !w.answered && (first == nil || w.seq < first.seq) {
					first = w
				}
			}
		}
	}
	return first
}

func (d *Daemon) approveTool(ctx context.Context, p ApprovalParams) (claude.Approval, error) {
	d.runs.mu.Lock()
	s := d.runs.approvals[p.Session]
	if s != nil {
		s.calls.Add(1)
	}
	d.runs.mu.Unlock()
	if s == nil {
		return claude.Approval{Decision: "deny", Reason: "local approval attempt is no longer active"}, nil
	}
	defer s.calls.Done()
	policy := s.policy
	// Only this session's persisted Bash output is an exception. A transcript
	// itself, and another session's output, remain outside the worktree.
	home, _ := os.UserHomeDir()
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		config = filepath.Join(home, ".claude")
	}
	project := strings.Map(func(r rune) rune {
		if r == '/' || r == '.' || r == '_' {
			return '-'
		}
		return r
	}, s.req.Worktree)
	expected := filepath.Join(config, "projects", project, p.Call.SessionID+".jsonl")
	if p.Call.SessionID != "" && filepath.Base(p.Call.SessionID) == p.Call.SessionID && p.Call.TranscriptPath == expected {
		policy.ToolResults = filepath.Join(strings.TrimSuffix(expected, ".jsonl"), "tool-results")
	}
	answer := policy.Evaluate(p.Call)
	if answer.Decision == "ask" {
		detail := p.Call.ApprovalDetail()
		wait := &permissionWait{reply: make(chan claude.Approval, 1), question: &runrec.OwnerQuestion{Detail: detail, Options: []runrec.Option{{Label: "Allow", Summary: "Allow this call once"}, {Label: "Deny", Summary: "Deny this call"}}, Recommend: 2, Reason: answer.Reason}}
		d.runs.mu.Lock()
		active := d.runs.approvals[p.Session] == s
		run := d.runs.runs[s.req.RunID]
		isSpec := s.req.Role == engine.RoleSpecCheck && s.req.SpecProject != "" && len(s.req.SpecItems) > 0
		if active && (run != nil && run.engine != nil || isSpec) {
			d.runs.permissionSeq++
			wait.seq = d.runs.permissionSeq
			wait.question.Title = approvalTitle(p.Call.ToolName, detail, wait.seq)
			s.waiting = append(s.waiting, wait)
		} else {
			active = false
		}
		d.runs.mu.Unlock()
		if !active {
			answer = claude.Approval{Decision: "deny", Reason: "no active run for an owner permission question"}
		} else {
			remaining := approvalTimeout
			if !p.Deadline.IsZero() {
				remaining = min(remaining, time.Until(p.Deadline))
			}
			timer := time.NewTimer(remaining)
			select {
			case answer = <-wait.reply:
			case <-timer.C:
				answer = claude.Approval{Decision: "deny", Reason: "owner permission answer timed out"}
			case <-ctx.Done():
				answer = claude.Approval{Decision: "deny", Reason: "owner permission request canceled"}
			case <-s.ctx.Done():
				answer = claude.Approval{Decision: "deny", Reason: "role ended while awaiting permission"}
			case <-s.done:
				answer = claude.Approval{Decision: "deny", Reason: "role ended while awaiting permission"}
			}
			timer.Stop()
			d.runs.mu.Lock()
			for i, w := range s.waiting {
				if w == wait {
					s.waiting = append(s.waiting[:i], s.waiting[i+1:]...)
					break
				}
			}
			d.runs.mu.Unlock()
		}
	}
	if answer.Decision == "allow" && s.req.ReadLog != "" {
		// Best effort: a failed write costs the next session its reminder, not
		// the call. The call carries all the log has (S371).
		_ = engine.AppendReadLog(s.req.ReadLog, policy.ReadLogEntry(p.Call))
	}
	if answer.Decision == "deny" {
		d.runs.mu.Lock()
		s.denials = append(s.denials, runrec.DeniedCommand{Tool: p.Call.ToolName, Command: (claude.Denial{ToolInput: p.Call.ToolInput}).Detail()})
		d.runs.mu.Unlock()
	}
	return answer, nil
}

// approvalTitleMax is the most characters a permission question's title holds.
// The whole tool input is in OwnerQuestion.Detail (S370).
const approvalTitleMax = 120

// approvalTitle is a one-line preview: the tool, the start of the input and
// the call number. The preview is cut so the title fits approvalTitleMax.
func approvalTitle(tool, detail string, seq uint64) string {
	if r := []rune(tool); len(r) > 40 {
		tool = string(r[:37]) + "..."
	}
	prefix := "Allow " + tool + ": "
	suffix := fmt.Sprintf("? (call %d)", seq)
	room := approvalTitleMax - len([]rune(prefix)) - len([]rune(suffix))
	preview := strings.Join(strings.Fields(detail), " ")
	if r := []rune(preview); len(r) > room {
		preview = string(r[:room-3]) + "..."
	}
	return prefix + preview + suffix
}

func permissionAnswer(text string) (claude.Approval, error) {
	text, _, _ = strings.Cut(text, ":")
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "allow", "1":
		return claude.Approval{Decision: "allow", Reason: "owner allowed this call once"}, nil
	case "deny", "2":
		return claude.Approval{Decision: "deny", Reason: "owner denied this call"}, nil
	default:
		return claude.Approval{}, fmt.Errorf("permission answer must be allow or deny")
	}
}
