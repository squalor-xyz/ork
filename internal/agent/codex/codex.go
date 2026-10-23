// SPDX-License-Identifier: GPL-3.0-or-later

// Package codex runs Codex role rounds. Codex ignores engine.Request.Allowlist:
// permissions come from the read-only or workspace-write sandbox (decision 100).
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

type Exit string

const (
	ExitOK          Exit = "ok"
	ExitError       Exit = "error"
	ExitRateLimited Exit = "rate_limited"
)

// Spec describes one CLI round. Env is the full environment; nil inherits.
type Spec struct {
	Bin, Dir, Prompt, Model, Effort, Resume, ReportFile, FixtureDir string
	Env                                                             []string
	ReadOnly                                                        bool
	OnStart                                                         func(int)
	OnEvent                                                         func(Event)
}

// Args follows the measured exec and resume shapes in probes 05 and 08.
// Resume has no -s or -C; Run also sets the process working directory.
func (s Spec) Args() ([]string, error) {
	if strings.TrimSpace(s.Prompt) == "" {
		return nil, errors.New("codex: empty prompt")
	}
	if strings.HasPrefix(s.Prompt, "-") {
		return nil, errors.New("codex: prompt starts with '-'")
	}
	if strings.HasPrefix(s.Resume, "-") {
		return nil, errors.New("codex: resume id starts with '-'")
	}
	mode := "workspace-write"
	if s.ReadOnly {
		mode = "read-only"
	}
	args := []string{"exec"}
	if s.Resume != "" {
		args = append(args, "resume", s.Resume)
	} else {
		args = append(args, "--json")
	}
	if s.Model != "" {
		args = append(args, "-m", s.Model)
	}
	if s.Resume != "" {
		args = append(args, "--json")
	}
	if s.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+s.Effort)
	}
	if s.Resume != "" {
		args = append(args, "-c", `sandbox_mode="`+mode+`"`)
	} else {
		args = append(args, "-s", mode)
		if s.Dir != "" {
			args = append(args, "-C", s.Dir)
		}
	}
	return append(args, s.Prompt), nil
}

// Command holds item events, including both command lifecycle states.
type Command struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Text     string `json:"text"`
	Command  string `json:"command"`
	Output   string `json:"aggregated_output"`
	ExitCode *int   `json:"exit_code"`
	Status   string `json:"status"`
}
type Usage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	CacheWriteInputTokens int `json:"cache_write_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

// Event is one JSONL event. Unknown event types are retained for OnEvent.
type Event struct {
	Type     string   `json:"type"`
	ThreadID string   `json:"thread_id"`
	Item     *Command `json:"item"`
	Usage    Usage    `json:"usage"`
	Message  string   `json:"message"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func ParseLine(line []byte) (Event, error) {
	var e Event
	err := json.Unmarshal(line, &e)
	if err == nil && e.Type == "" {
		err = errors.New("missing event type")
	}
	return e, err
}

// Round carries the vendor result. Tokens sums input and output. Whether cached
// input and reasoning output are subsets of those counts is unverified.
type Round struct {
	Exit            Exit
	Session, Report string
	Tokens          int
	Usage           Usage
	ExitCode        int
	Stdout, Stderr  []byte
	Command         []string
	Candidate       string
	CandidateErr    error
}
type stream struct {
	pending                    []byte
	raw                        bytes.Buffer
	round                      Round
	completed, failed, limited bool
	onEvent                    func(Event)
}

func (r *stream) Write(p []byte) (int, error) {
	r.raw.Write(p)
	r.pending = append(r.pending, p...)
	for {
		i := bytes.IndexByte(r.pending, '\n')
		if i < 0 {
			break
		}
		r.line(r.pending[:i])
		r.pending = r.pending[i+1:]
	}
	if len(r.pending) == 0 {
		r.pending = nil
	}
	return len(p), nil
}
func (r *stream) line(line []byte) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	e, err := ParseLine(line)
	if err != nil {
		r.failed = true
		return
	}
	switch e.Type {
	case "thread.started":
		r.round.Session = e.ThreadID
	case "item.completed":
		if e.Item != nil && e.Item.Type == "agent_message" {
			r.round.Report = e.Item.Text
		}
	case "turn.completed":
		r.completed = true
		r.round.Usage = e.Usage
		r.round.Tokens = e.Usage.InputTokens + e.Usage.OutputTokens
	case "turn.failed", "error":
		r.failed = true
		message := e.Message
		if e.Error != nil {
			message = e.Error.Message
		}
		if strings.Contains(message, "You've hit your usage limit") || (strings.Contains(message, "429") && strings.Contains(message, "rate_limit_exceeded")) {
			r.limited = true
		}
	}
	if r.onEvent != nil {
		r.onEvent(e)
	}
}

// Run owns the process group and kills descendants on cancellation and exit.
// Nonzero process exits are classified; launch and cancellation errors return
// an error with the partial round, like the Claude adapter.
func Run(ctx context.Context, s Spec) (round Round, err error) {
	args, err := s.Args()
	if err != nil {
		return Round{}, err
	}
	bin := s.Bin
	if bin == "" {
		bin = "codex"
	}
	out := &stream{onEvent: s.OnEvent}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	cmd.Stdout = out
	cmd.Stderr = &stderr
	cmd.WaitDelay = 2 * time.Second
	clirun.OwnProcessGroup(cmd)
	runErr := cmd.Start()
	if runErr == nil {
		if s.OnStart != nil {
			s.OnStart(cmd.Process.Pid)
		}
		runErr = cmd.Wait()
	}
	if cmd.ProcessState != nil {
		clirun.KillGroup(cmd)
		if errors.Is(runErr, exec.ErrWaitDelay) && cmd.ProcessState.Success() {
			runErr = nil
		}
	}
	if len(out.pending) > 0 {
		out.line(out.pending)
	}
	round = out.round
	round.Command = append([]string{bin}, args...)
	defer func() {
		if round.Exit != ExitOK {
			round.Candidate, round.CandidateErr = saveCandidate(s.FixtureDir, round)
		}
	}()
	round.Exit = ExitError
	round.ExitCode = -1
	round.Stdout = out.raw.Bytes()
	round.Stderr = stderr.Bytes()
	if cmd.ProcessState == nil {
		return round, fmt.Errorf("codex: start %s: %w", bin, runErr)
	}
	round.ExitCode = cmd.ProcessState.ExitCode()
	if ctx.Err() != nil && round.ExitCode == -1 {
		return round, fmt.Errorf("codex: %w", ctx.Err())
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) && !errors.Is(runErr, exec.ErrWaitDelay) {
		return round, fmt.Errorf("codex: %w", runErr)
	}
	if out.limited {
		round.Exit = ExitRateLimited
	} else if round.ExitCode == 0 && out.completed && !out.failed {
		round.Exit = ExitOK
	}
	if round.Exit != ExitOK {
		round.Report = ""
	}
	if s.ReportFile != "" {
		if b, err := os.ReadFile(s.ReportFile); err == nil && strings.TrimSpace(string(b)) != "" {
			round.Report = string(b)
		}
	}
	return round, nil
}
