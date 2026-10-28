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
	"github.com/squalor-xyz/ork/internal/runrec"
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
		return "", errors.New("the item has no slice spec")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, out.Slice); err != nil {
		return "", err
	}
	sum := sha256.Sum256(compact.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// gitStatusAt is the read-only fingerprint of a directory (role.readonly-
// fingerprint): the output of `git status --porcelain`.
func gitStatusAt(ctx context.Context, d Deps, dir string) (string, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: d.gitBin(), Args: []string{"-C", dir, "status", "--porcelain"}, Timeout: 30 * time.Second})
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	return string(res.Stdout), nil
}

var errSpecMainMoved = errors.New("main moved during the spec check")
var errSpecStatusChanged = errors.New("main checkout status changed outside the spec-check role")

// SpecCheckRetryError retains the failure and tells clients that the automatic
// retry has been used. It unwraps so callers can still inspect a ShapeError.
type SpecCheckRetryError struct{ First, Cause error }

func (e *SpecCheckRetryError) Unwrap() error { return e.Cause }
func (e *SpecCheckRetryError) Error() string {
	if errors.Is(e.First, errSpecMainMoved) && errors.Is(e.Cause, errSpecMainMoved) {
		return "main moved during the spec check and again on its retry; check again"
	}
	if errors.Is(e.First, errSpecStatusChanged) && errors.Is(e.Cause, errSpecStatusChanged) {
		return "main checkout status changed outside the role during the spec check and again on its retry; check again"
	}
	return fmt.Sprintf("spec check failed on its automatic retry; check again: %v", e.Cause)
}

// RunSpecCheck checks every item read-only and returns its verdict. A change
// inside the role with HEAD unchanged stops the check. A moving HEAD, a
// status change outside the role, or a malformed report gets one retry with
// fresh specs. A second failure says that the retry has already been used.
func RunSpecCheck(ctx context.Context, d Deps, cfg SpecCheckConfig) (*SpecCheckResult, error) {
	var first error
	for attempt := 0; ; attempt++ {
		res, err := runSpecCheckOnce(ctx, d, cfg)
		var shape *ShapeError
		if attempt == 0 && ctx.Err() == nil && (errors.Is(err, errSpecMainMoved) || errors.Is(err, errSpecStatusChanged) || errors.As(err, &shape)) {
			first = err
			continue
		}
		if attempt > 0 && err != nil && ctx.Err() == nil {
			return nil, &SpecCheckRetryError{First: first, Cause: err}
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
	brief, err := BuildBrief(BriefInput{
		Role: RoleSpecCheck, SliceID: strings.Join(cfg.Items, ", "), Worktree: root, TempDir: tempDir, ReportPath: reportPath,
		SliceJSON: "[" + strings.Join(raws, ",") + "]", EnvLines: cfg.Project.EnvironmentLines,
		Sections: []BriefSection{{Title: "Spec check", Body: "Check these slices in one pass: " + strings.Join(cfg.Items, ", ") + ". Report one `## Slice ID` section for each, in this order. The project checkout is your working directory; you may not change it."}},
	})
	if err != nil {
		return nil, err
	}
	before, err := gitStatusAt(ctx, d, root)
	if err != nil {
		return nil, fmt.Errorf("read-only fingerprint: %w", err)
	}
	if before != baseline {
		return nil, errSpecStatusChanged
	}
	var text string
	for i, entry := range cfg.Lineup {
		if err := os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("old report: %w", err)
		}
		res, runErr := d.Runner.Run(ctx, Request{
			Role: RoleSpecCheck, Round: 1, Vendor: entry.Vendor, Model: entry.Model, Effort: entry.Effort,
			Brief: brief, Worktree: root, TempDir: tempDir, ReportPath: reportPath,
			Allowlist: perm.Allow, ReadOnly: perm.ReadOnly,
		})
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
		class := res.Exit
		if runErr != nil || class == "" {
			class = ExitError
		}
		if len(res.Denials) > 0 {
			return nil, fmt.Errorf("spec-check exited %s: permission denied: %s", class, denialTexts(res.Denials))
		}
		if afterHead != head {
			return nil, errSpecMainMoved
		}
		if after != before {
			return nil, errors.New("spec-check changed the tree, and it is a read-only role")
		}
		if class == ExitOK {
			text = readSpecReport(reportPath, res.Report)
			break
		}
		if (class == ExitRateLimited || class == ExitError) && i+1 < len(cfg.Lineup) {
			continue
		}
		msg := fmt.Sprintf("spec-check (%s %s %s) exited %s", entry.Vendor, entry.Model, entry.Effort, class)
		if runErr != nil {
			msg += ": " + runErr.Error()
		}
		return nil, errors.New(msg)
	}
	verdicts, err := ParseSpecCheck(text, cfg.Items)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(reportPath, []byte(text), 0600); err != nil {
		return nil, fmt.Errorf("save spec report: %w", err)
	}
	owner, _ := section(text, ownerHeading)
	if isNone(owner) {
		owner = ""
	}
	return &SpecCheckResult{Verdicts: verdicts, Specs: specs, OwnerNeeded: strings.TrimSpace(owner)}, nil
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
	By        string   // the owner's name for adj (ADJ_DECIDED_BY)
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
	for i, q := range cfg.Questions {
		if q.Key == "" || !cfg.Adj {
			continue
		}
		if strings.TrimSpace(cfg.By) == "" {
			notices = append(notices, "ADJ_DECIDED_BY is unset; not filed in adj: "+q.Key)
			continue
		}
		title := ""
		if q.Ask != nil {
			title = q.Ask.Title
		}
		notice, err := fileDecision(ctx, d, wr, root, q.Key, title, cfg.Item, q.Text, answers[i], cfg.By)
		if err != nil {
			notices = append(notices, "not filed in adj: "+q.Key+": "+err.Error())
			continue
		}
		notices = append(notices, q.Key+": "+notice)
	}
	return para.String(), notices, nil
}
