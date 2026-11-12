// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/roles"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// SpecQuestion is one open question of a spec check: the parsed block, its
// optional adj decision key and the block as the role wrote it.
type SpecQuestion struct {
	Ask  *OwnerQuestion `json:"ask"`
	Key  string         `json:"key,omitempty"`
	Text string         `json:"text"`
}

// SpecVerdict is the spec check's answer for one slice: ready, or the
// questions the owner must answer first.
type SpecVerdict struct {
	Item      string         `json:"item"`
	Ready     bool           `json:"ready"`
	Questions []SpecQuestion `json:"questions,omitempty"`
}

// SpecCheckConfig describes one spec check: a batch of slices, checked in one
// read-only pass of the spec-check row's chain (adj 164, roles.spec-check).
type SpecCheckConfig struct {
	ledger  *specLedger
	Project registry.Project
	Items   []string
	Lineup  []runrec.LineupEntry // the spec check row, then its fallbacks
	Dir     string               // holds the role's temp directory and its report; the caller owns it
}

// SpecCheckResult holds a verdict for each item, in the order of the config,
// and the fingerprint of the spec each verdict was made against.
type SpecCheckResult struct {
	Verdicts    []SpecVerdict
	Specs       map[string]string
	OwnerNeeded string
}

// ErrNoSpec is the error of SpecFingerprint for an item that has no slice
// spec, so a caller can tell it from a read failure (S283, *proposed*).
var ErrNoSpec = errors.New("the item has no slice spec")

// SpecFingerprint identifies the spec in the output of `slicer show ID --json
// --lean`: its slice object alone, so a change of status or claim does not
// make a checked spec look new.
func SpecFingerprint(raw []byte) (string, error) {
	var out struct {
		Slice json.RawMessage `json:"slice"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("slicer show: %w", err)
	}
	if len(bytes.TrimSpace(out.Slice)) == 0 || string(bytes.TrimSpace(out.Slice)) == "null" {
		return "", ErrNoSpec
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, out.Slice); err != nil {
		return "", err
	}
	sum := sha256.Sum256(compact.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// gitStatusAt is the read-only fingerprint of a directory (role.readonly-
// fingerprint): `git status --porcelain -- .` includes only paths under dir,
// including its own tracking files when they are visible to git status.
func gitStatusAt(ctx context.Context, d Deps, dir string) (string, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: d.gitBin(), Args: []string{"-C", dir, "status", "--porcelain", "--", "."}, Timeout: 30 * time.Second})
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	return string(res.Stdout), nil
}

var errSpecMainMoved = errors.New("main moved during the spec check")
var errSpecStatusChanged = errors.New("the main checkout changed outside the check")

// SpecCheckRetryError retains the failure and tells clients that the automatic
// retry has been used. It unwraps so callers can still inspect a ShapeError.
// Resumed says that the check's own session was also resumed once with the
// error before it gave up (S266, *proposed*).
type SpecCheckRetryError struct {
	First, Cause error
	Items        []string
	Resumed      bool
}

func (e *SpecCheckRetryError) Unwrap() error { return e.Cause }
func (e *SpecCheckRetryError) Error() string {
	items := strings.Join(e.Items, ", ")
	const safe = "Check again; the check changes nothing, so a rerun is safe."
	if errors.Is(e.First, errSpecMainMoved) && errors.Is(e.Cause, errSpecMainMoved) {
		return fmt.Sprintf("main moved during the check of %s and again on its retry. %s", items, safe)
	}
	if errors.Is(e.First, errSpecStatusChanged) && errors.Is(e.Cause, errSpecStatusChanged) {
		return fmt.Sprintf("the main checkout changed outside the check of %s and again on its retry. %s", items, safe)
	}
	// No "spec check" here: the CLI prefixes "spec check failed: ".
	var denied *specDenialError
	hint := ""
	if errors.As(e.Cause, &denied) {
		hint = " (the role kept using a denied command, such as a pipe)"
	}
	if e.Resumed {
		return fmt.Sprintf("%s failed again after ork gave the role its error once: %v%s. %s", items, e.Cause, hint, safe)
	}
	return fmt.Sprintf("%s failed again on its automatic retry: %v%s. %s", items, e.Cause, hint, safe)
}

// specDenialError is a spec check round that ended with denied commands.
type specDenialError struct{ text string }

func (e *specDenialError) Error() string { return "permission denied: " + e.text }

type specLedger struct {
	current   int
	exhausted map[string]bool
	run       string
	contract  *slicercli.RelayContract
	writer    runrec.Writer
	// resumed is set once the check has resumed its session with an error
	// (S266, *proposed*). It does so once for the whole check, however many
	// times the check starts over; first is the error that earned the resume.
	resumed bool
	first   error
}

// RunSpecCheck checks every item read-only and returns its verdict. A change
// inside the role with HEAD unchanged stops the check. A malformed report or a
// denied command resumes the role's own session once with the error and the
// expected forms (S266, *proposed*). A moving HEAD or a status change outside
// the role gets one retry with fresh specs; so does a malformed report that
// could not be resumed. A second failure says that the retry has already been
// used and that checking again is safe.
func RunSpecCheck(ctx context.Context, d Deps, cfg SpecCheckConfig) (result *SpecCheckResult, finalErr error) {
	if len(cfg.Items) == 0 {
		return nil, errors.New("spec check needs items")
	}
	contract, err := d.Slicer.RelayContract(ctx, cfg.Project.Path)
	if err != nil {
		return nil, fmt.Errorf("slicer ai relay --json: %w", err)
	}
	cfg.ledger = &specLedger{contract: contract, exhausted: map[string]bool{}, writer: runrec.Writer{Now: d.now, Sleep: d.Sleep, Backoff: d.Backoff}}
	if run, err := d.Slicer.RelayBegin(ctx, cfg.ledger.writer, cfg.Project.Path, cfg.Project.Path, cfg.Items...); err == nil {
		cfg.ledger.run = run.ID
	}
	defer func() {
		if cfg.ledger.run != "" {
			outcome, close := "pass", "done"
			if finalErr != nil {
				outcome, close = "stopped", "fail"
			}
			d.Slicer.RelayEnd(context.WithoutCancel(ctx), cfg.ledger.writer, cfg.Project.Path, cfg.ledger.run, outcome, close)
		}
	}()

	var first error
	for attempt := 0; ; attempt++ {
		res, err := runSpecCheckOnce(ctx, d, cfg)
		var shape *ShapeError
		var denied *specDenialError
		isShape, isDenied := errors.As(err, &shape), errors.As(err, &denied)
		// A shape error after the session was resumed once is final: the
		// resume was the retry.
		if attempt == 0 && ctx.Err() == nil && (errors.Is(err, errSpecMainMoved) || errors.Is(err, errSpecStatusChanged) || isShape && !cfg.ledger.resumed) {
			first = err
			continue
		}
		if err != nil && ctx.Err() == nil && (attempt > 0 || cfg.ledger.resumed && (isShape || isDenied)) {
			if first == nil {
				first = cfg.ledger.first
			}
			return nil, &SpecCheckRetryError{First: first, Cause: err, Items: cfg.Items, Resumed: cfg.ledger.resumed}
		}
		return res, err
	}
}

func specHeadAt(ctx context.Context, d Deps, root string) (string, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: d.gitBin(), Args: []string{"-C", root, "rev-parse", "HEAD"}, Timeout: 30 * time.Second})
	if err != nil {
		return "", fmt.Errorf("spec check HEAD: %w", err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func runSpecCheckOnce(ctx context.Context, d Deps, cfg SpecCheckConfig) (*SpecCheckResult, error) {
	if len(cfg.Items) == 0 {
		return nil, errors.New("spec check needs items")
	}
	if len(cfg.Lineup) == 0 {
		return nil, errors.New("the lineup has no spec-check row")
	}
	if d.Runner == nil {
		return nil, errors.New("spec check needs a runner")
	}
	perm, err := ResolvePermission(cfg.Project, RoleSpecCheck)
	if err != nil {
		return nil, err
	}
	root := cfg.Project.Path
	implementerPerm, err := ResolvePermission(cfg.Project, RoleImplementer)
	if err != nil {
		return nil, err
	}
	head, err := specHeadAt(ctx, d, root)
	if err != nil {
		return nil, err
	}
	baseline, err := gitStatusAt(ctx, d, root)
	if err != nil {
		return nil, fmt.Errorf("read-only fingerprint: %w", err)
	}
	specs := map[string]string{}
	var raws []string
	for _, id := range cfg.Items {
		raw, err := d.Slicer.ShowRaw(ctx, root, id)
		if err != nil {
			return nil, fmt.Errorf("slicer show %s: %w", id, err)
		}
		fp, err := SpecFingerprint(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		specs[id] = fp
		raws = append(raws, strings.TrimSpace(string(raw)))
	}
	tempDir := filepath.Join(cfg.Dir, "tmp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return nil, fmt.Errorf("temp directory: %w", err)
	}
	reportPath := filepath.Join(cfg.Dir, "spec-check.md")
	before, err := gitStatusAt(ctx, d, root)
	if err != nil {
		return nil, fmt.Errorf("read-only fingerprint: %w", err)
	}
	if before != baseline {
		return nil, errSpecStatusChanged
	}
	var text string
	exhausted := cfg.ledger.exhausted
	taskBase := strings.Join(cfg.Items, ", ") + " spec-check"
entries:
	for i, entry := range cfg.Lineup {
		if i < cfg.ledger.current || exhausted[entry.Vendor] {
			continue
		}
		if err := os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("old report: %w", err)
		}
		brief, err := BuildBrief(BriefInput{
			Allow: implementerPerm.Allow,
			Role:  RoleSpecCheck, Vendor: entry.Vendor, ReadOnly: true, SliceID: strings.Join(cfg.Items, ", "), Worktree: root, TempDir: tempDir, ReportPath: reportPath,
			SliceJSON: "[" + strings.Join(raws, ",") + "]", EnvLines: cfg.Project.EnvironmentLines,
			Sections: []BriefSection{{Title: "Spec check", Body: "Check these slices in one pass: " + strings.Join(cfg.Items, ", ") + ". Report one `## Slice ID` section for each, in this order. The project checkout is your working directory; you may not change it."}},
		})
		if err != nil {
			return nil, err
		}
		req := Request{
			Role: RoleSpecCheck, Round: 1, Vendor: entry.Vendor, Model: entry.Model, Effort: entry.Effort,
			Brief: brief, Worktree: root, TempDir: tempDir, ReportPath: reportPath,
			Allowlist: perm.Allow, ReadOnly: perm.ReadOnly,
		}
		task := taskBase

		// A round that ends with denied commands or a malformed report is
		// resumed once in its own session (S266, *proposed*); every other
		// outcome leaves this loop.
		for {
			stepID := ""
			if cfg.ledger != nil && cfg.ledger.run != "" {
				step, err := d.Slicer.RelayStep(ctx, cfg.ledger.writer, root, cfg.ledger.run, slicercli.StepSpec{Role: cfg.ledger.contract.LedgerRole(RoleSpecCheck), Agent: entry.Vendor, Vendor: entry.Vendor, Model: entry.Model, Reasoning: entry.Effort, Phase: "start", Task: task})
				if err == nil {
					stepID = step.ID
				}
			}
			res, runErr := d.runSpecRole(ctx, req)

			class := res.Exit
			if runErr != nil || class == "" {
				class = ExitError
			}
			// The nudge, when this round earned one: the same session, the
			// same row, the error and the expected forms. Only an untouched
			// tree and a still HEAD qualify, so the checks below keep their
			// order of precedence for a role that changed something.
			var problem error
			var nudge string
			switch {
			case len(res.Denials) > 0:
				problem = &specDenialError{text: denialTexts(res.Denials)}
				nudge = specDenialNudge(res.Denials, cfg.Items)
			case class == ExitOK:
				if _, perr := ParseSpecCheck(readSpecReport(reportPath, res.Report), cfg.Items); perr != nil {
					problem = perr
					nudge = specShapeNudge(perr, cfg.Items)
				}
			}
			if stepID != "" {
				phase := "done"
				reason := ""
				if problem != nil && runErr == nil && res.Exit == ExitOK {
					// The round ran to its end but its answer is refused: the
					// run tree should explain the second spec-check row.
					phase, reason = "fail", specProblemReason(res.Denials)
				}
				if runErr != nil || res.Exit != ExitOK {
					phase = "fail"
					reason = res.Reason
					if reason == "" && problem != nil {
						reason = specProblemReason(res.Denials)
					}
					if reason == "" {
						reason = string(res.Exit)
					}
					if runErr != nil {
						reason = runErr.Error()
					}
				}
				version, _ := roles.CheckContractLine("Contract: " + cfg.ledger.contract.Contract)
				if version < 10 {
					reason = ""
				}
				d.Slicer.RelayStep(context.WithoutCancel(ctx), cfg.ledger.writer, root, cfg.ledger.run, slicercli.StepSpec{Step: stepID, Phase: phase, Reason: strings.Join(strings.Fields(reason), " ")})
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			after, statusErr := gitStatusAt(ctx, d, root)
			if statusErr != nil {
				return nil, fmt.Errorf("read-only fingerprint: %w", statusErr)
			}
			afterHead, err := specHeadAt(ctx, d, root)
			if err != nil {
				return nil, err
			}
			resumable := class == ExitOK || class == ExitError
			if problem != nil && resumable && !cfg.ledger.resumed && res.Session != "" && afterHead == head && after == before {
				cfg.ledger.resumed, cfg.ledger.first = true, problem
				if err := os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf("old report: %w", err)
				}
				req.Resume, req.Brief, task = res.Session, nudge, taskBase+" resumed with error"
				continue
			}
			if len(res.Denials) > 0 {
				return nil, problem
			}
			if afterHead != head {
				return nil, errSpecMainMoved
			}
			if after != before {
				return nil, errors.New("spec-check changed the tree, and it is a read-only role")
			}
			if class == ExitOK {
				text = readSpecReport(reportPath, res.Report)
				break entries
			}
			if class == ExitOutOfCredits {
				exhausted[entry.Vendor] = true
			}
			next := i + 1
			for next < len(cfg.Lineup) && exhausted[cfg.Lineup[next].Vendor] {
				next++
			}
			if fallbackExit(class) && next < len(cfg.Lineup) {
				cfg.ledger.current = next
				continue entries
			}
			msg := fmt.Sprintf("spec-check (%s %s %s) exited %s", entry.Vendor, entry.Model, entry.Effort, class)
			if runErr != nil {
				msg += ": " + runErr.Error()
			}
			return nil, errors.New(msg)
		}
	}
	verdicts, err := ParseSpecCheck(text, cfg.Items)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(reportPath, []byte(text), 0600); err != nil {
		return nil, fmt.Errorf("save spec report: %w", err)
	}
	owner, _ := section(text, ownerHeading)
	return &SpecCheckResult{Verdicts: verdicts, Specs: specs, OwnerNeeded: SpecOwnerText(owner)}, nil
}

// SpecOwnerText is the Owner needed text of a spec check that ork shows (S264,
// *proposed*): the body of the section without its code fence lines, trimmed.
// It is empty when the first non-blank line says none, so prose the role
// writes after a none does not reach the screen. The saved report keeps it all.
func SpecOwnerText(body string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		kept = append(kept, line)
	}
	text := strings.TrimSpace(strings.Join(kept, "\n"))
	first, _, _ := strings.Cut(text, "\n")
	if isNone(first) {
		return ""
	}
	return text
}

// runSpecRole applies the shared pool and elapsed limit to a local spec
// check, which has no Engine run. Cloud spec checks retain their existing
// behavior. Cancellation is joined before the slot is released.
func (d Deps) runSpecRole(ctx context.Context, req Request) (Result, error) {
	if !IsLocalVendor(req.Vendor) {
		return d.Runner.Run(ctx, req)
	}
	release, _, err := d.Vendors.Acquire(ctx, req.Vendor)
	if err != nil {
		return Result{}, err
	}
	defer release()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	e := Engine{d: d}
	watch, stop, expired := e.watchRole(runCtx, cancel, req.Vendor)
	defer stop()
	req.OnEvent = watch.touch
	req.OnLive = func(Live) { watch.touch() }
	res, runErr := d.Runner.Run(runCtx, req)
	stop()
	if ctx.Err() == nil && expired() && runErr != nil {
		res.Reason = "local role exceeded " + formatLimit(LocalRoleAfter)
		runErr = fmt.Errorf("%s: %w", res.Reason, runErr)
	}
	return res, runErr
}

// specProblemReason is the short ledger reason of a refused spec check round.
func specProblemReason(denials []runrec.DeniedCommand) string {
	if len(denials) == 0 {
		return "report shape"
	}
	reason := "denied: " + denialTexts(denials)
	if r := []rune(reason); len(r) > 80 {
		reason = string(r[:77]) + "..."
	}
	return reason
}

// specShapeNudge is the prompt of a spec check session resumed after a
// malformed report: the shape problems and the forms the report may take.
func specShapeNudge(err error, ids []string) string {
	return "ork checked your report and refused it: " + err.Error() + "\n\n" +
		"Write the whole report again as your final message, as plain text with no code fence. " +
		"It has one `## Slice ID` section for each of " + strings.Join(ids, ", ") + ", in that order. " +
		"Under each, write `Ready` alone, or one or more question blocks that each start with `Question:` " +
		"(then `Options:` with lines `1. label: summary`, `Recommend: N - reason`, and an optional `Decision key:` line). " +
		"End with the shared `## Owner needed` section (`none` when nothing is open). " +
		"You may only read the tree. Do not start the check over; fix the report from what you already read.\n"
}

// specDenialNudge is the prompt of a spec check session resumed after denied
// commands: what was denied, how to read without it, and the report forms.
func specDenialNudge(denials []runrec.DeniedCommand, ids []string) string {
	var b strings.Builder
	b.WriteString("These commands were denied and did not run:\n\n")
	const shown = 10
	for i, d := range denials {
		if i == shown {
			fmt.Fprintf(&b, "- and %d more\n", len(denials)-shown)
			break
		}
		b.WriteString("- " + d.Text() + "\n")
	}
	b.WriteString("\nYou may only read the tree. Use one command per call: no pipe, no `&&`, no `;`, no redirect, no subshell. " +
		"Do not repeat a denied command; use the built-in read, search and list tools, or a plainer single command. " +
		"Read what you still need, then give the whole report as your final message, as plain text with no code fence: " +
		"one `## Slice ID` section for each of " + strings.Join(ids, ", ") + ", in that order, holding `Ready` alone or question blocks that start with `Question:`, " +
		"then the shared `## Owner needed` section.\n")
	return b.String()
}

// readSpecReport returns the report file's text, else the vendor's final
// result text (role.report-io).
func readSpecReport(path, fallback string) string {
	if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
		return string(b)
	}
	return fallback
}

var sliceHeading = regexp.MustCompile(`^## Slice (\S+)\s*$`)

// ParseSpecCheck reads a spec check report: one `## Slice ID` section for
// each id, holding `Ready` or question blocks, and the shared `## Owner
// needed` section. The verdicts come back in the order of ids. Every way the
// report breaks the shape is listed in the *ShapeError.
func ParseSpecCheck(text string, ids []string) ([]SpecVerdict, error) {
	var problems []string
	type part struct {
		id    string
		lines []string
	}
	var parts []*part
	var cur *part
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if m := sliceHeading.FindStringSubmatch(line); m != nil {
			cur = nil
			id := m[1]
			// The role may change an id's case (S374 for s374); the batch
			// id is the one that counts.
			if i := slices.IndexFunc(ids, func(b string) bool { return strings.EqualFold(b, id) }); i >= 0 {
				id = ids[i]
			}
			switch {
			case !slices.Contains(ids, id):
				problems = append(problems, fmt.Sprintf("a section for %s, which is not in the batch", id))
			case slices.ContainsFunc(parts, func(p *part) bool { return p.id == id }):
				problems = append(problems, fmt.Sprintf("two sections for %s", id))
			default:
				cur = &part{id: id}
				parts = append(parts, cur)
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			cur = nil
			continue
		}
		if cur != nil {
			cur.lines = append(cur.lines, line)
		}
	}
	if _, ok := section(text, ownerHeading); !ok {
		problems = append(problems, fmt.Sprintf("no %q section", ownerHeading))
	}
	var verdicts []SpecVerdict
	for _, id := range ids {
		i := slices.IndexFunc(parts, func(p *part) bool { return p.id == id })
		if i < 0 {
			problems = append(problems, fmt.Sprintf("no section for %s", id))
			continue
		}
		body := strings.TrimSpace(strings.Join(parts[i].lines, "\n"))
		lower := strings.ToLower(strings.TrimSpace(strings.TrimRight(body, ".")))
		switch {
		case lower == "ready":
			verdicts = append(verdicts, SpecVerdict{Item: id, Ready: true})
		case strings.HasPrefix(body, "Question:"):
			qs, err := ParseOwnerQuestions(body)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", id, err))
				continue
			}
			verdicts = append(verdicts, SpecVerdict{Item: id, Questions: qs})
		default:
			problems = append(problems, fmt.Sprintf("%s: write Ready, or question blocks that start with Question:", id))
		}
	}
	if len(problems) > 0 {
		return nil, &ShapeError{Problems: problems}
	}
	return verdicts, nil
}

// SpecAnswerConfig is the owner's answers to one slice's spec questions.
type SpecAnswerConfig struct {
	Project   registry.Project
	Item      string
	Questions []SpecQuestion
	Answers   []string // one for each question: an option as "Label: summary", or free text
	Adj       bool     // file the keyed questions in adj
	By        string   // the owner's name for adj, from ResolveOwner
}

// RecordSpecAnswers writes the answers into the slice's spec as one
// "Owner decisions (date)" paragraph appended to its Implement section, then
// files each answered question that has a decision key in adj through the
// answer path of a run (S28). It returns the paragraph, which the run of the
// item carries into its briefs (a worktree does not see the main checkout's
// edit until it lands), and notices that say what went to adj, or why not.
func RecordSpecAnswers(ctx context.Context, d Deps, cfg SpecAnswerConfig) (string, []string, error) {
	if len(cfg.Questions) == 0 || len(cfg.Answers) != len(cfg.Questions) {
		return "", nil, fmt.Errorf("%s needs one answer for each of its %d questions", cfg.Item, len(cfg.Questions))
	}
	answers := make([]string, len(cfg.Answers))
	for i, a := range cfg.Answers {
		if answers[i] = strings.Join(strings.Fields(a), " "); answers[i] == "" {
			return "", nil, fmt.Errorf("answer %d of %s is empty", i+1, cfg.Item)
		}
	}
	date := d.now().Format("2006-01-02")
	var para strings.Builder
	fmt.Fprintf(&para, "Owner decisions (%s):", date)
	for i, q := range cfg.Questions {
		title := ""
		if q.Ask != nil {
			title = q.Ask.Title
		}
		fmt.Fprintf(&para, " %s -> %s", title, answers[i])
		if q.Key != "" {
			fmt.Fprintf(&para, " [%s]", q.Key)
		}
		para.WriteString(".")
	}
	root := cfg.Project.Path
	wr := runrec.Writer{Now: d.now, Sleep: d.Sleep, Backoff: d.Backoff}
	if err := d.Slicer.EditAppend(ctx, wr, root, cfg.Item, "Implement", para.String()); err != nil {
		return "", nil, err
	}
	var notices []string
	if !cfg.Adj {
		return para.String(), notices, nil
	}
	if strings.TrimSpace(cfg.By) == "" {
		// One notice for the whole item, not one for each key (S314).
		var keys []string
		for _, q := range cfg.Questions {
			if q.Key != "" {
				keys = append(keys, q.Key)
			}
		}
		if len(keys) > 0 {
			notices = append(notices, "no owner set; run ork settings set --owner NAME (not filed in adj: "+strings.Join(keys, ", ")+")")
		}
		return para.String(), notices, nil
	}
	for i, q := range cfg.Questions {
		if q.Key == "" {
			continue
		}
		title := ""
		if q.Ask != nil {
			title = q.Ask.Title
		}
		// Adj's rule: only an option the owner picked is an option decision.
		// Free text is recorded in the slice and not filed.
		if !pickedOption(q, answers[i]) {
			notices = append(notices, "not filed in adj: "+q.Key+" (free text answer, not one of the options)")
			continue
		}
		// The why names the date and the slice the question came from (the
		// answer comes before any run of it).
		why := "Answered in ork on " + date + ": " + title + " (" + cfg.Item + ")"
		id, already, err := decideInAdj(ctx, d, wr, root, q.Key, title, cfg.Item, q.Text, answers[i], why, cfg.By)
		switch {
		case err != nil:
			notices = append(notices, "not filed in adj: "+q.Key+": "+err.Error())
		case already:
			notices = append(notices, fmt.Sprintf("already decided in adj: %s (adj %d); not changed", q.Key, id))
		default:
			notices = append(notices, fmt.Sprintf("filed in adj: %s (adj %d)", q.Key, id))
		}
	}
	return para.String(), notices, nil
}

// pickedOption reports whether the answer is one of the question's options as
// the clients write it: "Label: summary".
func pickedOption(q SpecQuestion, answer string) bool {
	if q.Ask == nil {
		return false
	}
	for _, o := range q.Ask.Options {
		if answer == strings.Join(strings.Fields(o.Label+": "+o.Summary), " ") {
			return true
		}
	}
	return false
}
