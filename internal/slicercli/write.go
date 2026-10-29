// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
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
func (c Client) Contract(ctx context.Context, root string) (string, error) {
	res, err := clirun.Run(ctx, c.call("ai", "relay", "--root", root))
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
		{"--waiting-on", s.WaitingOn}, {"--reason", s.Reason},
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

// Render runs `slicer render --json --root ROOT`.
func (c Client) Render(ctx context.Context, w runrec.Writer, root string) error {
	_, err := c.write(ctx, w, root, "render", "--json")
	return err
}

// Land runs `slicer land ID --branch BRANCH --note NOTE --json --root ROOT`.
// ROOT is the checkout that has merged the branch (docs/landing.md of slicer).
func (c Client) Land(ctx context.Context, w runrec.Writer, root, id, branch, note string) error {
	_, err := c.write(ctx, w, root, "land", id, "--branch", branch, "--note", note, "--json")
	return err
}

// Rule is one rule of the relay roles matrix.
type Rule struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// RelayRules runs `slicer relay-roles show --json --root ROOT` and returns the
// matrix's rules.
func (c Client) RelayRules(ctx context.Context, root string) ([]Rule, error) {
	var out struct {
		Rules []Rule `json:"rules"`
	}
	if err := clirun.RunJSON(ctx, c.call("relay-roles", "show", "--json", "--root", root), &out); err != nil {
		return nil, err
	}
	return out.Rules, nil
}

var uiPassRow = regexp.MustCompile(`UI pass row \((r\d+)\)`)

// UIPassRowID finds the matrix row of the UI pass: the id that a rule names as
// "UI pass row (rNN)" (rule u10 today). It is empty when no rule names one.
// The matrix can still use a reviewer row for this separate ledger role.
func UIPassRowID(rules []Rule) string {
	for _, r := range rules {
		if m := uiPassRow.FindStringSubmatch(r.Text); m != nil {
			return m[1]
		}
	}
	return ""
}

var specCheckRow = regexp.MustCompile(`Spec check row \((r\d+)\)`)

// SpecCheckRowID finds the matrix row of the spec check: the id that a rule
// names as "Spec check row (rNN)" (rule u11 today). It is empty when no rule
// names one. The matrix can still use a reviewer row for this separate
// ledger role.
func SpecCheckRowID(rules []Rule) string {
	for _, r := range rules {
		if m := specCheckRow.FindStringSubmatch(r.Text); m != nil {
			return m[1]
		}
	}
	return ""
}

// EditAppend runs `slicer edit ID --section SECTION --append --text TEXT
// --render --json --lean --root ROOT`: it adds TEXT to the section as a new
// paragraph.
func (c Client) EditAppend(ctx context.Context, w runrec.Writer, root, id, section, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("slicer edit %s: nothing to append", id)
	}
	_, err := c.write(ctx, w, root, "edit", id, "--section", section, "--append", "--text", text, "--render", "--json", "--lean")
	return err
}

// Added is the item `slicer add` filed. Existing is true when the key matched
// an item filed before, which the call then returns unchanged.
type Added struct {
	ID       string `json:"id"`
	Existing bool   `json:"existing"`
	HasSlice bool   `json:"has_slice"`
}

// Add runs `slicer add TITLE --discovered-from ID --key KEY --render --json
// --lean --root ROOT`. The key is the filing identity: a retry returns the
// item the first call filed. A leading dash is cut from the title so that
// slicer does not read it as a flag.
func (c Client) Add(ctx context.Context, w runrec.Writer, root, title, discoveredFrom, key string) (*Added, error) {
	title = strings.TrimLeft(title, "- ")
	res, err := c.write(ctx, w, root, "add", title, "--discovered-from", discoveredFrom, "--key", key, "--render", "--json", "--lean")
	if err != nil {
		return nil, err
	}
	var out Added
	if err := json.Unmarshal(bytes.TrimSpace(res.Stdout), &out); err != nil || out.ID == "" {
		return nil, fmt.Errorf("slicer add: no item id in the output")
	}
	return &out, nil
}

// Promote runs `slicer promote ID --file FILE --render --json --lean --root
// ROOT`: it gives an item filed without a spec the sections of a one-item
// outline.
func (c Client) Promote(ctx context.Context, w runrec.Writer, root, id, file string) error {
	_, err := c.write(ctx, w, root, "promote", id, "--file", file, "--render", "--json", "--lean")
	return err
}

// SetScores applies a role's proposed scores to a filed slice.
func (c Client) SetScores(ctx context.Context, w runrec.Writer, root, id string, scores runrec.FindingScores) error {
	_, err := c.write(ctx, w, root, "set", id, "--importance", strconv.Itoa(scores.Importance), "--urgency", strconv.Itoa(scores.Urgency), "--effort", strconv.Itoa(scores.Effort), "--render", "--json", "--lean")
	return err
}

// RelayAsk mirrors a structured blocker question on the orchestrator step.
func (c Client) RelayAsk(ctx context.Context, w runrec.Writer, root, run, step, key string, q runrec.OwnerQuestion) (string, error) {
	argv := []string{"relay-run", "ask", run, "--step", step, "--title", q.Title, "--blocker"}
	for _, o := range q.Options {
		argv = append(argv, "--option", o.Label+":"+o.Summary)
	}
	if q.Recommend > 0 && q.Recommend <= len(q.Options) {
		argv = append(argv, "--recommend", q.Options[q.Recommend-1].Label)
	}
	if key != "" {
		argv = append(argv, "--key", key)
	}
	if q.Reason != "" {
		argv = append(argv, "--reasoning", q.Reason)
	}
	argv = append(argv, "--json")
	res, err := c.write(ctx, w, root, argv...)
	if err != nil {
		return "", err
	}
	var out struct {
		Question struct {
			ID string `json:"id"`
		} `json:"question"`
	}
	if err := json.Unmarshal(res.Stdout, &out); err != nil || out.Question.ID == "" {
		return "", fmt.Errorf("slicer relay-run ask: no question id in output")
	}
	return out.Question.ID, nil
}

// RelayAnswer records the owner's free-text answer, ending that question's wait.
func (c Client) RelayAnswer(ctx context.Context, w runrec.Writer, root, run, question, text string) error {
	_, err := c.write(ctx, w, root, "relay-run", "answer", run, question, "--text", text, "--json")
	return err
}
