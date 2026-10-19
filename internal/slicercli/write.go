// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// longTimeout is the deadline of a call that renders or checks, when the
// client sets none.
const longTimeout = time.Minute

func (c Client) longCall(args ...string) clirun.Call {
	call := c.call(args...)
	if c.Timeout == 0 {
		call.Timeout = longTimeout
	}
	return call
}

// Contract runs `slicer ai relay` and returns its first line.
func (c Client) Contract(ctx context.Context) (string, error) {
	res, err := clirun.Run(ctx, c.call("ai", "relay"))
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(res.Stdout)), "\n")
	return strings.TrimSpace(first), nil
}

// ShowRaw runs `slicer show ID --json --lean --root ROOT` and returns stdout
// as printed.
func (c Client) ShowRaw(ctx context.Context, root, id string) ([]byte, error) {
	res, err := clirun.Run(ctx, c.call("show", id, "--json", "--lean", "--root", root))
	if err != nil {
		return nil, err
	}
	return res.Stdout, nil
}

// Config runs `slicer config KEY --json --root ROOT` and returns the value.
func (c Client) Config(ctx context.Context, root, key string) (string, error) {
	var out struct {
		Value string `json:"value"`
	}
	if err := clirun.RunJSON(ctx, c.call("config", key, "--json", "--root", root), &out); err != nil {
		return "", err
	}
	return out.Value, nil
}

// Check runs `slicer check --json --root ROOT`. A failed check (exit 1) is a
// result with OK false, not an error.
func (c Client) Check(ctx context.Context, root string) (*CheckResult, error) {
	res, err := clirun.Run(ctx, c.longCall("check", "--json", "--root", root))
	var exitErr *clirun.ExitError
	if err != nil && !(errors.As(err, &exitErr) && exitErr.Tool == nil && res.ExitCode == 1) {
		return nil, err
	}
	var out CheckResult
	if jerr := json.Unmarshal(bytes.TrimSpace(res.Stdout), &out); jerr != nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("slicer check: decode output: %w", jerr)
	}
	if err != nil {
		out.OK = false
	}
	out.Output = string(res.Stdout)
	return &out, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// RecommendRoles runs `slicer relay-roles recommend ID --well-specified yes|no
// --split yes|no --json --root ROOT`.
func (c Client) RecommendRoles(ctx context.Context, root, id string, wellSpecified, split bool) (*Recommendation, error) {
	var rec Recommendation
	args := []string{"relay-roles", "recommend", id, "--well-specified", yesNo(wellSpecified), "--split", yesNo(split), "--json", "--root", root}
	if err := clirun.RunJSON(ctx, c.call(args...), &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// write runs one slicer write through the writer: argv plus --root ROOT.
func (c Client) write(ctx context.Context, w runrec.Writer, root string, argv ...string) (clirun.Result, error) {
	call := c.longCall()
	bin := call.Bin
	return w.Run(ctx, runrec.Cmd{
		Tool: "slicer", Bin: bin, Root: root, Argv: argv,
		Args:    append(append([]string{}, argv...), "--root", root),
		Timeout: call.Timeout,
	})
}

// RelayBegin runs `slicer relay-run begin ID... --worktree PATH --json`.
func (c Client) RelayBegin(ctx context.Context, w runrec.Writer, root, worktree string, ids ...string) (*RelayRun, error) {
	argv := append([]string{"relay-run", "begin"}, ids...)
	argv = append(argv, "--worktree", worktree, "--json")
	res, err := c.write(ctx, w, root, argv...)
	if err != nil {
		return nil, err
	}
	var out struct {
		Run RelayRun `json:"run"`
	}
	if err := json.Unmarshal(res.Stdout, &out); err != nil || out.Run.ID == "" {
		return nil, fmt.Errorf("slicer relay-run begin: no run id in the output")
	}
	return &out.Run, nil
}

// RelayStep runs `slicer relay-run step RUN ... --json` and returns the step.
func (c Client) RelayStep(ctx context.Context, w runrec.Writer, root, run string, s StepSpec) (*Step, error) {
	argv := []string{"relay-run", "step", run}
	for _, f := range []struct{ flag, val string }{
		{"--step", s.Step}, {"--role", s.Role}, {"--agent", s.Agent}, {"--vendor", s.Vendor},
		{"--model", s.Model}, {"--reasoning", s.Reasoning}, {"--parent", s.Parent},
		{"--phase", s.Phase}, {"--task", s.Task},
	} {
		if f.val != "" {
			argv = append(argv, f.flag, f.val)
		}
	}
	argv = append(argv, "--json")
	res, err := c.write(ctx, w, root, argv...)
	if err != nil {
		return nil, err
	}
	var out struct {
		Step Step `json:"step"`
	}
	if err := json.Unmarshal(res.Stdout, &out); err != nil || out.Step.ID == "" {
		return nil, fmt.Errorf("slicer relay-run step: no step id in the output")
	}
	return &out.Step, nil
}

// RelayEnd runs `slicer relay-run end RUN --outcome OUTCOME [--close-steps X] --json`.
func (c Client) RelayEnd(ctx context.Context, w runrec.Writer, root, run, outcome, closeSteps string) error {
	argv := []string{"relay-run", "end", run, "--outcome", outcome}
	if closeSteps != "" {
		argv = append(argv, "--close-steps", closeSteps)
	}
	argv = append(argv, "--json")
	_, err := c.write(ctx, w, root, argv...)
	return err
}

// Note runs `slicer note ID --text TEXT --render --json`.
func (c Client) Note(ctx context.Context, w runrec.Writer, root, id, text string) error {
	_, err := c.write(ctx, w, root, "note", id, "--text", text, "--render", "--json")
	return err
}
