// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/agent/codex"
	"github.com/squalor-xyz/ork/internal/agent/grok"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// claudeRunner connects the engine's vendor-neutral request to S16.
type claudeRunner struct {
	round      func(context.Context, claude.Spec) (claude.Round, error)
	fixtureDir string
	env        []string
}

func (r claudeRunner) Run(ctx context.Context, req engine.Request) (engine.Result, error) {
	if req.Vendor != "claude" {
		return engine.Result{}, fmt.Errorf("unsupported vendor %q", req.Vendor)
	}
	call := r.round
	if call == nil {
		call = claude.Run
	}
	live := newLiveRedactor(r.env, req.OnLive)
	var message claude.Usage
	total := func(u claude.Usage) int {
		return int(u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens)
	}
	round, err := call(ctx, claude.Spec{Dir: req.Worktree, Env: claudeEnv(r.env, req.TempDir), Prompt: req.Brief, Model: req.Model, Effort: req.Effort,
		ReadOnly: req.ReadOnly, Allowlist: req.Allowlist, Resume: req.Resume, ReportFile: req.ReportPath, OnStart: req.OnStart, OnEvent: func(ev claude.Event) {
			switch ev.Kind {
			case claude.EventTextDelta:
				live.send(ev.Text, 0, false)
			case claude.EventUsage:
				before := total(message)
				if ev.UsageStart {
					message = ev.Usage
					before = 0
				} else {
					if ev.Usage.InputTokens != 0 {
						message.InputTokens = ev.Usage.InputTokens
					}
					if ev.Usage.CacheReadTokens != 0 {
						message.CacheReadTokens = ev.Usage.CacheReadTokens
					}
					if ev.Usage.CacheCreationTokens != 0 {
						message.CacheCreationTokens = ev.Usage.CacheCreationTokens
					}
					message.OutputTokens = ev.Usage.OutputTokens
				}
				live.usage(total(message) - before)
			}
			if req.OnEvent != nil {
				req.OnEvent()
			}
		}, FixtureDir: r.fixtureDir})
	live.send("", 0, true)
	class := engine.ExitError
	switch round.Class {
	case claude.ClassOK:
		class = engine.ExitOK
	case claude.ClassUnclassified:
		class = engine.ExitUnclassified
	case claude.ClassRateLimited:
		class = engine.ExitRateLimited
	}
	result := engine.Result{Exit: class, Session: round.SessionID, Report: round.Report, CostUSD: round.Usage.CostUSD,
		Tokens: int(round.Usage.InputTokens + round.Usage.OutputTokens + round.Usage.CacheReadTokens + round.Usage.CacheCreationTokens)}
	for _, d := range round.Denials {
		result.Denials = append(result.Denials, runrec.DeniedCommand{Tool: d.ToolName, Command: d.Detail()})
	}
	return result, err
}

// vendorRunner dispatches only to measured adapters.
type vendorRunner struct{ claude, codex, grok engine.Runner }

func supportsVendor(v string) bool { return v == "claude" || v == "codex" || v == "grok" }
func (r vendorRunner) Run(ctx context.Context, req engine.Request) (engine.Result, error) {
	switch req.Vendor {
	case "claude":
		return r.claude.Run(ctx, req)
	case "codex":
		return r.codex.Run(ctx, req)
	case "grok":
		return r.grok.Run(ctx, req)
	default:
		return engine.Result{}, fmt.Errorf("unsupported vendor %q", req.Vendor)
	}
}

type codexRunner struct {
	fixtureDir string
	env        []string
	round      func(context.Context, codex.Spec) (codex.Round, error)
}

func (r codexRunner) Run(ctx context.Context, req engine.Request) (engine.Result, error) {
	call := r.round
	if call == nil {
		call = codex.Run
	}
	live := newLiveRedactor(r.env, req.OnLive)
	round, err := call(ctx, codex.Spec{Dir: req.Worktree, AddDir: req.TempDir, Env: codexEnv(r.env, req.TempDir), Prompt: req.Brief, Model: req.Model, Effort: req.Effort, ReadOnly: req.ReadOnly, Resume: req.Resume, ReportFile: req.ReportPath, OnStart: req.OnStart, OnEvent: func(ev codex.Event) {
		if ev.Type == "item.completed" && ev.Item != nil && ev.Item.Type == "agent_message" {
			live.send(ev.Item.Text+"\n", 0, false)
		}
		if ev.Type == "turn.completed" {
			live.usage(ev.Usage.InputTokens + ev.Usage.OutputTokens)
		}
		if req.OnEvent != nil {
			req.OnEvent()
		}
	}, FixtureDir: r.fixtureDir})
	live.send("", 0, true)
	class := engine.ExitError
	switch round.Exit {
	case codex.ExitOK:
		class = engine.ExitOK
	case codex.ExitUnclassified:
		class = engine.ExitUnclassified
	case codex.ExitOutOfCredits:
		class = engine.ExitOutOfCredits
	case codex.ExitRateLimited:
		class = engine.ExitRateLimited
	}
	return engine.Result{Exit: class, Reason: round.Reason, Session: round.Session, Report: round.Report, Tokens: round.Tokens}, err
}

// grokRunner connects the engine's vendor-neutral request to the grok adapter
// (S72, *proposed*). Request.Allowlist and TempDir are not used: grok runs
// read-only roles in plan mode and refuses write roles.
type grokRunner struct {
	fixtureDir string
	env        []string
	round      func(context.Context, grok.Spec) (grok.Round, error)
}

func (r grokRunner) Run(ctx context.Context, req engine.Request) (engine.Result, error) {
	call := r.round
	if call == nil {
		call = grok.Run
	}
	live := newLiveRedactor(r.env, req.OnLive)
	round, err := call(ctx, grok.Spec{Dir: req.Worktree, Env: r.env, Prompt: req.Brief, Model: req.Model, Effort: req.Effort, ReadOnly: req.ReadOnly, Resume: req.Resume, ReportFile: req.ReportPath, OnStart: req.OnStart, OnEvent: func(ev grok.Event) {
		if text, ok := ev.TextDelta(); ok {
			live.send(text, 0, false)
		}
		if ev.Type == "result" && ev.Usage != nil {
			live.usage(ev.Usage.Total())
		}
		if req.OnEvent != nil {
			req.OnEvent()
		}
	}, FixtureDir: r.fixtureDir})
	live.send("", 0, true)
	class := engine.ExitError
	switch round.Exit {
	case grok.ExitOK:
		class = engine.ExitOK
	case grok.ExitUnclassified:
		class = engine.ExitUnclassified
	case grok.ExitOutOfCredits:
		class = engine.ExitOutOfCredits
	}
	return engine.Result{Exit: class, Reason: round.Reason, Session: round.Session, Report: round.Report, Tokens: round.Tokens}, err
}

// roleEnv returns base (the process environment when nil) with each named
// variable set to the given value, replacing every inherited entry of that
// name. Without a temp directory it returns base unchanged. base is not
// modified.
func roleEnv(base []string, tempDir string, set map[string]string) []string {
	if tempDir == "" {
		return base
	}
	if base == nil {
		base = os.Environ()
	}
	env := make([]string, 0, len(base)+len(set))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := set[name]; !replaced {
			env = append(env, entry)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(set)) {
		env = append(env, name+"="+set[name])
	}
	return env
}

// claudeEnv points the role's TMPDIR at its own temp directory (S271,
// *proposed*), so it needs no "env TMPDIR=DIR" prefix or mkdir. The role's
// directory wins over a vendor_env TMPDIR.
func claudeEnv(base []string, tempDir string) []string {
	return roleEnv(base, tempDir, map[string]string{"TMPDIR": tempDir})
}

// codexEnv keeps the role's Go build cache inside its writable temp directory
// (S87) and points TMPDIR there too (S271, *proposed*).
func codexEnv(base []string, tempDir string) []string {
	return roleEnv(base, tempDir, map[string]string{"GOCACHE": filepath.Join(tempDir, "gocache"), "TMPDIR": tempDir})
}

// liveRedactor withholds a possible secret prefix across assistant text events.
// Only redacted text is passed to the engine's in-memory ring.
type liveRedactor struct {
	secrets []string
	pending string
	emit    func(engine.Live)
}

func newLiveRedactor(env []string, emit func(engine.Live)) *liveRedactor {
	r := &liveRedactor{emit: emit}
	if env == nil {
		env = os.Environ()
	}
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || utf8.RuneCountInString(value) < 8 {
			continue
		}
		name = strings.ToUpper(name)
		for _, word := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL"} {
			if strings.Contains(name, word) {
				r.secrets = append(r.secrets, value)
				break
			}
		}
	}
	// Prefer the longest matching value when secrets share a prefix.
	sort.Slice(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
	return r
}
func (r *liveRedactor) send(text string, tokens int, flush bool) {
	if r.emit == nil {
		return
	}
	r.pending += text
	var out strings.Builder
	for len(r.pending) > 0 {
		match := ""
		wait := false
		for _, secret := range r.secrets {
			if len(r.pending) < len(secret) && strings.HasPrefix(secret, r.pending) {
				if flush {
					match = r.pending
				} else {
					wait = true
				}
				break
			}
			if strings.HasPrefix(r.pending, secret) {
				match = secret
				break
			}
		}
		if wait {
			break
		}
		if match != "" {
			out.WriteString("***")
			r.pending = r.pending[len(match):]
		} else {
			out.WriteByte(r.pending[0])
			r.pending = r.pending[1:]
		}
	}
	if out.Len() > 0 || tokens != 0 {
		r.emit(engine.Live{Text: out.String(), Tokens: tokens})
	}
}

func (r *liveRedactor) usage(tokens int) {
	if r.emit != nil {
		r.emit(engine.Live{Tokens: tokens, UsageKnown: true})
	}
}
