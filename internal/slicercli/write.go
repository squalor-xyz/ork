// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
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

// Contract runs `slicer ai relay --json --root ROOT` and returns its contract
// string, for example "relay v11".
func (c Client) Contract(ctx context.Context, root string) (string, error) {
	out, err := c.ReadContract(ctx, root)
	if err != nil {
		return "", err
	}
	return out.Contract, nil
}

// SetRelayPin runs `slicer config relay_contract N --root ROOT`, which pins
// the project's relay contract in its own slicer config (adj 97).
func (c Client) SetRelayPin(ctx context.Context, root string, n int) error {
	_, err := clirun.Run(ctx, c.call("config", "relay_contract", strconv.Itoa(n), "--root", root))
	return err
}

// UnsetRelayPin runs `slicer config relay_contract --unset --root ROOT`.
func (c Client) UnsetRelayPin(ctx context.Context, root string) error {
	_, err := clirun.Run(ctx, c.call("config", "relay_contract", "--unset", "--root", root))
	return err
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
		{"--waiting-on", s.WaitingOn}, {"--reason", s.Reason}, {"--verdict", s.Verdict},
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
	if strings.TrimSpace(id) == "" {
		return errors.New("item must not be empty")
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("Note must not be empty")
	}
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

// Matrix is `slicer relay-roles show --json`: slicer's own text of the matrix,
// its rows and its rules.
type Matrix struct {
	Markdown string `json:"relay_roles"`
	Rows     []Row  `json:"rows"`
	Rules    []Rule `json:"rules"`
}

// RelayMatrix runs `slicer relay-roles show --json --root ROOT`.
func (c Client) RelayMatrix(ctx context.Context, root string) (*Matrix, error) {
	var out Matrix
	if err := clirun.RunJSON(ctx, c.call("relay-roles", "show", "--json", "--root", root), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RelayRules runs `slicer relay-roles show --json --root ROOT` and returns the
// matrix's rules.
func (c Client) RelayRules(ctx context.Context, root string) ([]Rule, error) {
	m, err := c.RelayMatrix(ctx, root)
	if err != nil {
		return nil, err
	}
	return m.Rules, nil
}

// RowFields maps a `slicer relay-roles add|set` flag name (without the dashes)
// to its text value (S173, *proposed*). A name that is absent is not passed; a
// name with an empty value clears that field.
type RowFields map[string]string

// rowFieldNames are the flags that `relay-roles add` and `set` take.
var rowFieldNames = []string{"role", "size", "effort", "importance", "urgency", "well-specified", "split", "sliver", "agent", "vendor", "model", "reasoning", "note"}

// RowArgs checks one relay-roles write and returns its arguments, from
// "relay-roles" up to the last value, without --render, --json or --root. op is
// add, set, remove, move or rule-edit. The client calls below and the daemon's
// preview line both come from here, so they cannot drift.
//
// Every field value is attached (--name=VALUE): slicer's parser reads a
// separate value that starts with a dash and has no space as an option. The
// rule text of rule-edit is a positional and Client.write puts --root after
// it, so a text that starts with a dash and has no space is refused. Fields go
// in name order, with role first.
func RowArgs(op, id string, f RowFields, before string, last bool, text string) ([]string, error) {
	switch op {
	case "add", "set", "remove", "move", "rule-edit":
	default:
		return nil, fmt.Errorf("relay-roles: unknown op %q", op)
	}
	if op != "add" && strings.TrimSpace(id) == "" {
		return nil, errors.New("row id must not be empty")
	}
	if op != "add" && op != "set" && len(f) > 0 {
		return nil, fmt.Errorf("relay-roles %s takes no fields", op)
	}
	args := []string{"relay-roles", op}
	if op != "add" {
		args = append(args, id)
	}
	switch op {
	case "add", "set":
		for name := range f {
			if !slices.Contains(rowFieldNames, name) {
				return nil, fmt.Errorf("relay-roles %s: unknown field %q", op, name)
			}
		}
		if op == "add" && strings.TrimSpace(f["role"]) == "" {
			return nil, errors.New("relay-roles add needs a role")
		}
		if op == "set" && len(f) == 0 {
			return nil, errors.New("relay-roles set needs at least one field")
		}
		names := make([]string, 0, len(f))
		for name := range f {
			names = append(names, name)
		}
		slices.Sort(names)
		if i := slices.Index(names, "role"); i > 0 {
			names = slices.Insert(slices.Delete(names, i, i+1), 0, "role")
		}
		for _, name := range names {
			args = append(args, "--"+name+"="+f[name])
		}
	case "move":
		given := strings.TrimSpace(before) != ""
		if given == last {
			return nil, errors.New("relay-roles move needs exactly one of before and last")
		}
		if last {
			args = append(args, "--last")
		} else {
			args = append(args, "--before="+before)
		}
	case "rule-edit":
		if strings.TrimSpace(text) == "" {
			return nil, errors.New("rule text must not be empty")
		}
		if strings.HasPrefix(text, "-") && !strings.Contains(text, " ") {
			return nil, errors.New("rule text must not start with a dash unless it holds a space: slicer would read it as a flag")
		}
		args = append(args, text)
	}
	return args, nil
}

// relayRowWrite runs the arguments of RowArgs as one write, with --render and
// --json. The change is left uncommitted in root.
func (c Client) relayRowWrite(ctx context.Context, w runrec.Writer, root, op, id string, f RowFields, before string, last bool, text string) (clirun.Result, error) {
	args, err := RowArgs(op, id, f, before, last, text)
	if err != nil {
		return clirun.Result{}, err
	}
	return c.write(ctx, w, root, append(args, "--render", "--json")...)
}

// RelayRowAdd runs `slicer relay-roles add --role=ROLE [--NAME=VALUE ...]
// --render --json --root ROOT` and returns the new row's id. f must hold a role.
func (c Client) RelayRowAdd(ctx context.Context, w runrec.Writer, root string, f RowFields) (string, error) {
	res, err := c.relayRowWrite(ctx, w, root, "add", "", f, "", false, "")
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(res.Stdout), &out); err != nil || out.ID == "" {
		return "", errors.New("slicer relay-roles add: no row id in the output")
	}
	return out.ID, nil
}

// RelayRowSet runs `slicer relay-roles set ID --NAME=VALUE ... --render --json
// --root ROOT`. It passes only the fields in f.
func (c Client) RelayRowSet(ctx context.Context, w runrec.Writer, root, id string, f RowFields) error {
	_, err := c.relayRowWrite(ctx, w, root, "set", id, f, "", false, "")
	return err
}

// RelayRowRemove runs `slicer relay-roles remove ID --render --json --root ROOT`.
func (c Client) RelayRowRemove(ctx context.Context, w runrec.Writer, root, id string) error {
	_, err := c.relayRowWrite(ctx, w, root, "remove", id, nil, "", false, "")
	return err
}

// RelayRowMove runs `slicer relay-roles move ID --before=BEFORE|--last
// --render --json --root ROOT`. Exactly one of before and last is given.
func (c Client) RelayRowMove(ctx context.Context, w runrec.Writer, root, id, before string, last bool) error {
	_, err := c.relayRowWrite(ctx, w, root, "move", id, nil, before, last, "")
	return err
}

// RelayRuleEdit runs `slicer relay-roles rule-edit ID TEXT --render --json
// --root ROOT`.
func (c Client) RelayRuleEdit(ctx context.Context, w runrec.Writer, root, id, text string) error {
	_, err := c.relayRowWrite(ctx, w, root, "rule-edit", id, nil, "", false, text)
	return err
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
	if ids := SpecCheckRowIDs(rules); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// SpecCheckRowIDs lists every row that a rule names as "Spec check row (rNN)",
// in rule order, each once (S258, *proposed*). SpecCheckRowID is the first.
func SpecCheckRowIDs(rules []Rule) []string {
	var ids []string
	for _, r := range rules {
		for _, m := range specCheckRow.FindAllStringSubmatch(r.Text, -1) {
			if !slices.Contains(ids, m[1]) {
				ids = append(ids, m[1])
			}
		}
	}
	return ids
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

// RelayAsk records a structured owner question on the orchestrator step. With
// blocker the step waits on the owner until it is answered; without, slicer
// records the question and the step keeps running (S223, proposed).
func (c Client) RelayAsk(ctx context.Context, w runrec.Writer, root, run, step, key string, q runrec.OwnerQuestion, blocker bool) (string, error) {
	argv := []string{"relay-run", "ask", run, "--step", step, "--title", q.Title}
	if blocker {
		argv = append(argv, "--blocker")
	}
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

// RelayAnswerSpec selects an option or supplies a free-text owner answer.
type RelayAnswerSpec struct{ Option, Text, By string }

// RelayAnswer records an answer, ending that question's wait.
func (c Client) RelayAnswer(ctx context.Context, w runrec.Writer, root, run, question string, spec RelayAnswerSpec) error {
	args := []string{"relay-run", "answer", run, question}
	if spec.Option != "" {
		args = append(args, "--option="+spec.Option)
	} else {
		args = append(args, "--text="+spec.Text)
	}
	if spec.By != "" {
		args = append(args, "--by="+spec.By)
	}
	args = append(args, "--json")
	_, err := c.write(ctx, w, root, args...)
	return err
}

// RelayAnswerGet runs `slicer relay-run answer RUN QUESTION_ID --get --json
// --root ROOT` and returns the question. It is a read: slicer takes no lock and
// writes nothing, and --get takes no other flag. Answer is nil while the
// question is open (S223, proposed).
func (c Client) RelayAnswerGet(ctx context.Context, root, run, question string) (*LedgerQuestion, error) {
	var out struct {
		Question *LedgerQuestion `json:"question"`
	}
	if err := clirun.RunJSON(ctx, c.call("relay-run", "answer", run, question, "--get", "--json", "--root", root), &out); err != nil {
		return nil, err
	}
	if out.Question == nil || out.Question.ID == "" {
		return nil, errors.New("slicer relay-run answer --get: no question in the output")
	}
	return out.Question, nil
}

// AddSpec contains the optional fields of an owner-filed item.
type AddSpec struct {
	Title, DiscoveredFrom, Key, Size string
	Importance, Urgency, Effort      int
}

// AddItem files an item, leaving unspecified fields to slicer.
func (c Client) AddItem(ctx context.Context, w runrec.Writer, root string, spec AddSpec) (*Added, error) {
	args := []string{"add", strings.TrimLeft(spec.Title, "- ")}
	for _, field := range []struct{ name, value string }{{"discovered-from", spec.DiscoveredFrom}, {"key", spec.Key}, {"size", spec.Size}} {
		if field.value != "" {
			args = append(args, "--"+field.name, field.value)
		}
	}
	for _, field := range []struct {
		name  string
		value int
	}{{"importance", spec.Importance}, {"urgency", spec.Urgency}, {"effort", spec.Effort}} {
		if field.value != 0 {
			args = append(args, "--"+field.name, strconv.Itoa(field.value))
		}
	}
	args = append(args, "--render", "--json", "--lean")
	res, err := c.write(ctx, w, root, args...)
	if err != nil {
		return nil, err
	}
	var out Added
	if err := json.Unmarshal(bytes.TrimSpace(res.Stdout), &out); err != nil || out.ID == "" {
		return nil, fmt.Errorf("slicer add: no item id in the output")
	}
	return &out, nil
}

// Park runs `slicer park ID --render --json`.
func (c Client) Park(ctx context.Context, w runrec.Writer, root, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("item must not be empty")
	}
	_, err := c.write(ctx, w, root, "park", id, "--render", "--json")
	return err
}

// Unpark runs `slicer unpark ID --render --json`.
func (c Client) Unpark(ctx context.Context, w runrec.Writer, root, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("item must not be empty")
	}
	_, err := c.write(ctx, w, root, "unpark", id, "--render", "--json")
	return err
}

// Move changes the stored queue position before or after another item.
func (c Client) Move(ctx context.Context, w runrec.Writer, root, id, other, position string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(other) == "" {
		return errors.New("item and other must not be empty")
	}
	if position != "before" && position != "after" {
		return errors.New("position must be before or after")
	}
	_, err := c.write(ctx, w, root, "move", id, "--"+position, other, "--render", "--json")
	return err
}
