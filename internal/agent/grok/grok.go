// SPDX-License-Identifier: GPL-3.0-or-later

// Package grok runs Grok role rounds on the grok CLI (S72, *proposed*). Grok
// ignores engine.Request.Allowlist: --tools takes tool names, not the
// claude-style patterns. Only read-only roles run, in --permission-mode plan
// (testdata/vendors/grok/05-plan-mode-edit). A headless run refuses every write
// under dontAsk and acceptEdits (probes 10 to 12), and the owner rules out
// bypassPermissions and --always-approve (2026-11-03), so Args refuses a write role.
package grok

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
	ExitOK           Exit = "ok"
	ExitError        Exit = "error" // the process did not run to an exit: launch failure or cancel
	ExitUnclassified Exit = "unclassified"
	ExitOutOfCredits Exit = "out_of_credits"
)

// creditsText is in the 403 error of testdata/vendors/grok/07-out-of-credits-403.
// No rate-limit sample exists, so no rate-limit class is mapped (adj 77).
const creditsText = "used all available credits"

// Spec describes one CLI round. Env is the full environment; nil inherits.
// A Model of "" or "latest" passes no model flag, so the CLI default applies.
type Spec struct {
	Bin, Dir, Prompt, Model, Effort, Resume, ReportFile, FixtureDir string
	Env                                                             []string
	ReadOnly                                                        bool
	OnStart                                                         func(int)
	OnEvent                                                         func(Event)
}

// Args follows probes 02, 03 and 05: streamed output, so every line is an
// event for the watchdog, resume by id, and plan mode. Run also sets the
// process working directory.
func (s Spec) Args() ([]string, error) {
	if strings.TrimSpace(s.Prompt) == "" {
		return nil, errors.New("grok: empty prompt")
	}
	if strings.HasPrefix(s.Prompt, "-") {
		return nil, errors.New("grok: prompt starts with '-'")
	}
	if strings.HasPrefix(s.Resume, "-") {
		return nil, errors.New("grok: resume id starts with '-'")
	}
	if !s.ReadOnly {
		return nil, errors.New("grok: write roles are not supported: a headless grok run cannot write under any permission mode the owner allows")
	}
	args := []string{"-p", s.Prompt, "--output-format", "streaming-messages-json", "--include-partial-messages"}
	if s.Model != "" && !strings.EqualFold(s.Model, "latest") {
		args = append(args, "-m", s.Model)
	}
	if s.Effort != "" {
		args = append(args, "--reasoning-effort", s.Effort)
	}
	if s.Resume != "" {
		args = append(args, "--resume", s.Resume)
	}
	args = append(args, "--permission-mode", "plan")
	if s.Dir != "" {
		args = append(args, "--cwd", s.Dir)
	}
	return args, nil
}

// Usage holds the token counts of a result. The counts add up: in
// 01-plain-json total_tokens (19190) is input (18014) plus cache read (1152)
// plus output (24), and reasoning tokens are part of the output.
type Usage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	ReasoningTokens     int `json:"reasoning_tokens"`
}

func (u Usage) Total() int {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens
}

// Event is one line of streaming-messages-json. Unknown types are kept for
// OnEvent; the watchdog needs only that a line arrived.
type Event struct {
	Type        string          `json:"type"`
	Subtype     string          `json:"subtype"`
	SessionID   string          `json:"session_id"`
	IsError     bool            `json:"is_error"`
	Result      string          `json:"result"`
	Message     json.RawMessage `json:"message"` // a string in an error line, an object in an assistant line
	Usage       *Usage          `json:"usage"`
	StreamEvent *struct {
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
}

func ParseLine(line []byte) (Event, error) {
	var e Event
	err := json.Unmarshal(line, &e)
	if err == nil && e.Type == "" {
		err = errors.New("missing event type")
	}
	return e, err
}

// TextDelta returns the assistant text of a partial-message event.
func (e Event) TextDelta() (string, bool) {
	if e.Type == "stream_event" && e.StreamEvent != nil && e.StreamEvent.Delta != nil && e.StreamEvent.Delta.Type == "text_delta" {
		return e.StreamEvent.Delta.Text, true
	}
	return "", false
}

func (e Event) errorMessage() string {
	var s string
	if json.Unmarshal(e.Message, &s) == nil {
		return s
	}
	return ""
}

// Round carries the vendor result. Tokens is Usage.Total.
type Round struct {
	Reason         string // classified vendor failure message
	Exit           Exit
	Session        string
	Report         string
	StopReason     string // plain JSON result only
	Tokens         int
	Usage          Usage
	ExitCode       int
	Stdout, Stderr []byte
	Command        []string
	Candidate      string
	CandidateErr   error
}

// ParsePlain reads the single JSON result of --output-format json
// (01-plain-json, 03-resume-by-id). It refuses an error line.
func ParsePlain(b []byte) (Round, error) {
	var p struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stopReason"`
		SessionID  string `json:"sessionId"`
		Usage      *Usage `json:"usage"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return Round{}, err
	}
	if p.Type != "" || p.StopReason == "" || p.SessionID == "" || p.Usage == nil {
		return Round{}, errors.New("not a grok json result")
	}
	return Round{Session: p.SessionID, Report: p.Text, StopReason: p.StopReason, Usage: *p.Usage, Tokens: p.Usage.Total()}, nil
}

type stream struct {
	pending                    []byte
	raw                        bytes.Buffer
	round                      Round
	completed, failed, credits bool
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
	case "system":
		if e.Subtype == "init" && e.SessionID != "" {
			r.round.Session = e.SessionID
		}
	case "result":
		r.completed = true
		if e.SessionID != "" {
			r.round.Session = e.SessionID
		}
		if e.Usage != nil {
			r.round.Usage = *e.Usage
			r.round.Tokens = e.Usage.Total()
		}
		r.round.Report = e.Result
		if e.IsError {
			r.fail(e.Result)
		}
	case "error":
		r.fail(e.errorMessage())
	}
	if r.onEvent != nil {
		r.onEvent(e)
	}
}

// fail records a failed round. The reason is fixed text, not the vendor
// message, which names the team.
func (r *stream) fail(message string) {
	r.failed = true
	if strings.Contains(message, creditsText) {
		r.credits = true
		r.round.Reason = "grok: the team has used all available credits or reached its spending limit"
	}
}

// Run owns the process group and kills descendants on cancellation and exit.
// A process that ran and did not succeed is classified (out of credits,
// else unclassified); launch and cancellation errors return an error with the
// partial round, like the other adapters.
func Run(ctx context.Context, s Spec) (round Round, err error) {
	args, err := s.Args()
	if err != nil {
		return Round{}, err
	}
	bin := s.Bin
	if bin == "" {
		bin = "grok"
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
	if !out.completed {
		// --output-format json prints one pretty JSON document, not lines.
		if plain, perr := ParsePlain(out.raw.Bytes()); perr == nil && plain.StopReason == "end_turn" {
			out.round.Session, out.round.Report, out.round.Usage, out.round.Tokens = plain.Session, plain.Report, plain.Usage, plain.Tokens
			out.completed, out.failed = true, false
		}
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
		return round, fmt.Errorf("grok: start %s: %w", bin, runErr)
	}
	round.ExitCode = cmd.ProcessState.ExitCode()
	if ctx.Err() != nil && round.ExitCode == -1 {
		return round, fmt.Errorf("grok: %w", ctx.Err())
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) && !errors.Is(runErr, exec.ErrWaitDelay) {
		return round, fmt.Errorf("grok: %w", runErr)
	}
	round.Exit = ExitUnclassified
	if out.credits {
		round.Exit = ExitOutOfCredits
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
