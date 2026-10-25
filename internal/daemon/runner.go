// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/agent/codex"
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
	round, err := call(ctx, claude.Spec{Dir: req.Worktree, Env: r.env, Prompt: req.Brief, Model: req.Model, Effort: req.Effort,
		ReadOnly: req.ReadOnly, Allowlist: req.Allowlist, Resume: req.Resume, ReportFile: req.ReportPath, OnStart: req.OnStart, OnEvent: func(_ claude.Event) {
			if req.OnEvent != nil {
				req.OnEvent()
			}
		}, FixtureDir: r.fixtureDir})
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
type vendorRunner struct{ claude, codex engine.Runner }

func supportsVendor(v string) bool { return v == "claude" || v == "codex" }
func (r vendorRunner) Run(ctx context.Context, req engine.Request) (engine.Result, error) {
	switch req.Vendor {
	case "claude":
		return r.claude.Run(ctx, req)
	case "codex":
		return r.codex.Run(ctx, req)
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
	round, err := call(ctx, codex.Spec{Dir: req.Worktree, AddDir: req.TempDir, Env: codexEnv(r.env, req.TempDir), Prompt: req.Brief, Model: req.Model, Effort: req.Effort, ReadOnly: req.ReadOnly, Resume: req.Resume, ReportFile: req.ReportPath, OnStart: req.OnStart, OnEvent: func(_ codex.Event) {
		if req.OnEvent != nil {
			req.OnEvent()
		}
	}, FixtureDir: r.fixtureDir})
	class := engine.ExitError
	switch round.Exit {
	case codex.ExitOK:
		class = engine.ExitOK
	case codex.ExitUnclassified:
		class = engine.ExitUnclassified
	case codex.ExitRateLimited:
		class = engine.ExitRateLimited
	}
	return engine.Result{Exit: class, Session: round.Session, Report: round.Report, Tokens: round.Tokens}, err
}

// codexEnv keeps the role's Go build cache inside its writable temp directory.
func codexEnv(base []string, tempDir string) []string {
	if tempDir == "" {
		return base
	}
	if base == nil {
		base = os.Environ()
	}
	env := make([]string, 0, len(base)+1)
	for _, entry := range base {
		if !strings.HasPrefix(entry, "GOCACHE=") {
			env = append(env, entry)
		}
	}
	return append(env, "GOCACHE="+filepath.Join(tempDir, "gocache"))
}
