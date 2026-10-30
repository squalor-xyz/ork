// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// humanCheckLine matches a [HUMAN CHECK] item: a bullet or numbered line of
// any section of the slice, as the slices' Check sections write them.
var humanCheckLine = regexp.MustCompile(`(?i)^\s*(?:[-*+]\s+|\d+[.)]\s+)?\[HUMAN CHECK\]\s*(\S.*?)\s*$`)

// humanCheckTexts lists the [HUMAN CHECK] items of a slice, in order.
func humanCheckTexts(sections []slicercli.Section) []string {
	var out []string
	for _, s := range sections {
		for _, line := range strings.Split(s.Body, "\n") {
			if m := humanCheckLine.FindStringSubmatch(line); m != nil {
				out = append(out, m[1])
			}
		}
	}
	return out
}

// buildCard collects what the owner needs to approve: the gate's verdict and
// test counts, the diff stat of the worktree against HEAD (ork commits only
// after approval), and the slice's human checks. Marks the owner already made
// on a check with the same text carry over. A diff stat that cannot be read
// is left empty; human checks that cannot be read are named on the card.
func (e *Engine) buildCard(ctx context.Context, rec runrec.Record, rep *Report) *runrec.ApprovalCard {
	card := &runrec.ApprovalCard{Round: rec.Round, Verdict: rep.Verdict, Passed: rep.Passed, Failed: rep.Failed, Deviations: roleSwitches(rec)}
	if out, err := e.gitRead(ctx, rec.Worktree, "diff", "--stat", "HEAD"); err == nil {
		card.DiffStat = strings.TrimSpace(out)
	}
	if out, err := e.gitRead(ctx, rec.Worktree, "ls-files", "--others", "--exclude-standard"); err == nil {
		for _, name := range strings.Split(out, "\n") {
			if name = strings.TrimSpace(name); name != "" {
				card.Untracked = append(card.Untracked, name)
			}
		}
	}
	card.UIRound, card.Renders, card.RendersDropped, card.RendersError = e.uiRenders(rec)
	marks := map[string]string{}
	if rec.Card != nil {
		for _, c := range rec.Card.HumanChecks {
			marks[c.Text] = c.State
		}
	}
	raw, err := e.d.Slicer.ShowRaw(ctx, rec.Worktree, rec.Item)
	var detail slicercli.Detail
	if err == nil {
		err = json.Unmarshal(raw, &detail)
	}
	if err != nil {
		card.ChecksError = err.Error()
		return card
	}
	if detail.Slice != nil {
		for _, text := range humanCheckTexts(detail.Slice.Sections) {
			card.HumanChecks = append(card.HumanChecks, runrec.HumanCheck{Text: text, State: marks[text]})
		}
	}
	return card
}

// openChecks lists the human checks the owner has neither ticked nor marked
// later, as "N. text".
func openChecks(card *runrec.ApprovalCard) []string {
	if card == nil {
		return nil
	}
	var open []string
	for i, c := range card.HumanChecks {
		if !c.Resolved() {
			open = append(open, fmt.Sprintf("%d. %s", i+1, c.Text))
		}
	}
	return open
}

// checksNote is the slicer note that records the owner's human checks, or, when
// the card could not read them, that they could not be read (S141). It is empty
// when the slice has no human checks to record.
func checksNote(item string, card *runrec.ApprovalCard) string {
	if card == nil {
		return ""
	}
	var b strings.Builder
	switch {
	case len(card.HumanChecks) > 0:
		fmt.Fprintf(&b, "Human checks at approval (%s):", item)
		for _, c := range card.HumanChecks {
			fmt.Fprintf(&b, "\n- %s: %s", c.State, c.Text)
		}
	case card.ChecksError != "":
		fmt.Fprintf(&b, "Human checks at approval (%s): the slice's human checks could not be read, so none were marked: %s", item, card.ChecksError)
	}
	return b.String()
}

// writeLog collects the writes of a call, so the caller can tell the call's own
// outcome from the outcome of saving its record.
type writeLog struct{ writes []runrec.Write }

func (l *writeLog) AddWrite(w runrec.Write) error {
	l.writes = append(l.writes, w)
	return nil
}

// writeChecksNote writes the approval's checks note once. The note counts as
// written when the slicer call succeeded, even if the run record then could not
// be saved: the owner approves again and the note is not written twice (S141).
// The caller holds e.mu.
func (e *Engine) writeChecksNote(ctx context.Context, rec runrec.Record, note string) error {
	var log writeLog
	w := e.wr
	w.Sink = &log
	err := e.d.Slicer.Note(ctx, w, rec.Worktree, rec.Item, note)
	var saveErr error
	for _, entry := range log.writes {
		saveErr = errors.Join(saveErr, e.run.AddWrite(entry))
	}
	if err != nil {
		return err
	}
	e.noted = note
	return saveErr
}

// MarkCheck sets the owner's mark on the n-th (one-based) human check of the
// approval card: runrec.CheckTicked, runrec.CheckLater, or "" to take a mark
// back. Approve needs every check ticked or marked later.
func (e *Engine) MarkCheck(n int, state string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.run.Snapshot()
	if rec.State != runrec.StateAwaitingApproval {
		return refusef("the slice is %s, not awaiting approval", rec.State)
	}
	if rec.Card == nil || n < 1 || n > len(rec.Card.HumanChecks) {
		return refusef("the slice has no human check %d", n)
	}
	switch state {
	case "", runrec.CheckTicked, runrec.CheckLater:
	default:
		return refusef("a human check is ticked, later or open, not %q", state)
	}
	return e.run.Update(func(r *runrec.Record) { r.Card.HumanChecks[n-1].State = state })
}

// Fix sends the owner's note from the approval card to a fix round: the
// implementer, resumed, acts on the note; then the checks and the gate run,
// and the slice returns to awaiting approval. No review runs in between.
func (e *Engine) Fix(ctx context.Context, note string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.run.Snapshot()
	if rec.State != runrec.StateAwaitingApproval {
		return refusef("the slice is %s, not awaiting approval", rec.State)
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return refusef("the fix note is empty")
	}
	if rec.Round+1 > e.d.maxRounds() {
		return refusef("the round limit of %d implementer rounds was reached; approve or stop the slice", e.d.maxRounds())
	}
	name := fmt.Sprintf("owner-fix-%d.md", rec.Round+1)
	path := filepath.Join(e.run.Dir(), name)
	if err := os.WriteFile(path, []byte(note+"\n"), 0o600); err != nil {
		return fmt.Errorf("fix note file: %w", err)
	}
	err := e.run.Update(func(r *runrec.Record) {
		r.Round++
		r.Fixes = append(r.Fixes, runrec.OwnerFix{Round: r.Round, Note: note, At: e.d.now()})
		r.Feedback = &runrec.Feedback{Kind: "owner", File: name}
		r.State = runrec.StateImplement
		r.Reason = ""
	})
	if err != nil {
		return err
	}
	e.orchestratorPhase(ctx, "start", "")
	return e.advance(ctx)
}

// What the card keeps of the UI pass's renders (S121, *proposed*).
const (
	rendersDir     = "renders" // in the UI pass's temp directory
	maxRenders     = 24        // files kept; the rest are counted
	maxRenderBytes = 24 << 10  // bytes kept of one render
)

// rendersPath is where the UI pass of a round writes its renders, under the
// run directory (the proposed storage path): tmp/ui-N/renders.
func rendersPath(runDir string, round int) string {
	return filepath.Join(runDir, "tmp", fmt.Sprintf("%s-%d", RoleUI, round), rendersDir)
}

// listRenders lists the renders directory as the card shows it: the regular
// .txt files, in card order. The directory is the role's own, so a missing
// directory, a link in its place and every entry that is not a regular .txt
// file (a link, a directory) are left out without a word. A directory that is
// there but cannot be read is an error (S147), so the card can say why it has
// no renders.
func listRenders(dir string) ([]fs.DirEntry, error) {
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var kept []fs.DirEntry
	for _, ent := range entries {
		if ent.Type().IsRegular() && strings.HasSuffix(ent.Name(), ".txt") {
			kept = append(kept, ent)
		}
	}
	sortRenders(kept)
	return kept, nil
}

// readReason is the cause of a read error without the path: the card shows it
// beside a name, and the path is the run directory's own.
func readReason(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// uiRenders lists the renders of the latest UI pass that ended well: the round
// it ran in (0 when none did), each render's name and size, and how many files
// past the limit were left out. The text is not kept; ReadRender reads it.
// When the directory, or a listed file, exists and cannot be read, readErr says
// why (the first reason), so the card does not report it as "wrote none".
func (e *Engine) uiRenders(rec runrec.Record) (round int, renders []runrec.Render, dropped int, readErr string) {
	for _, r := range rec.Roles {
		if r.Role == RoleUI && r.Exit == string(ExitOK) {
			round = r.Round
		}
	}
	if round == 0 {
		return 0, nil, 0, ""
	}
	dir := rendersPath(e.run.Dir(), round)
	ents, err := listRenders(dir)
	if err != nil {
		readErr = readReason(err)
	}
	for _, ent := range ents {
		if len(renders) == maxRenders {
			dropped++
			continue
		}
		text, err := readRender(dir, ent)
		if err != nil {
			if readErr == "" {
				readErr = ent.Name() + ": " + readReason(err)
			}
			continue
		}
		renders = append(renders, runrec.Render{Name: ent.Name(), Size: len(text)})
	}
	return round, renders, dropped, readErr
}

// ErrNoRender is the error of ReadRender for a render that is not there.
var ErrNoRender = errors.New("no such render")

// ReadRender reads the text of one render of a run, by its name, as the card
// listed it (the daemon's run.render, S121, *proposed*). The name must be a
// regular .txt file of the round's renders directory; a link, a directory, a
// path, or a file that is not there is ErrNoRender. The text is plain text of
// at most 24 KiB.
func ReadRender(runDir string, round int, name string) (string, error) {
	dir := rendersPath(runDir, round)
	ents, _ := listRenders(dir) // a directory that cannot be read lists nothing
	for _, ent := range ents {
		if ent.Name() != name {
			continue
		}
		if text, err := readRender(dir, ent); err == nil {
			return text, nil
		}
		break
	}
	return "", ErrNoRender
}

// errNotThatFile is the error of readRender for a file that is no longer the
// one the listing named (a link put in its place).
var errNotThatFile = errors.New("not the listed file")

// readRender reads one render file as plain text of bounded size. It returns
// the error of an open or a read that failed, or errNotThatFile.
func readRender(dir string, ent fs.DirEntry) (string, error) {
	f, err := os.Open(filepath.Join(dir, ent.Name()))
	if err != nil {
		return "", err
	}
	defer f.Close()
	// The listing said a regular file; the open must reach that same file,
	// not a link put in its place since.
	info, err := ent.Info()
	opened, err2 := f.Stat()
	if err != nil || err2 != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return "", errNotThatFile
	}
	b, err := io.ReadAll(io.LimitReader(f, maxRenderBytes+1))
	if err != nil {
		return "", err
	}
	text := runrec.PlainText(string(b))
	if len(b) > maxRenderBytes {
		text = runrec.PlainText(string(b[:maxRenderBytes]))
		if i := strings.LastIndex(text, "\n"); i >= 0 {
			text = text[:i]
		}
		text += "\n(cut: over 24 KiB)"
	}
	return text, nil
}

// viewSizes is the order of a view's renders on the card.
var viewSizes = []string{"80x24", "120x40", "ascii"}

// renderParts splits "VIEW-SIZE.txt" into the view and the rank of its size;
// ok is false for any other name.
func renderParts(name string) (view string, rank int, ok bool) {
	base := strings.TrimSuffix(name, ".txt")
	for i, size := range viewSizes {
		if v, found := strings.CutSuffix(base, "-"+size); found && v != "" {
			return v, i, true
		}
	}
	return "", 0, false
}

// sortRenders orders the renders as the card lists them: each view's
// 80x24, 120x40 and ascii files, the views by name, then every other file by
// name (docs/dashboard.md).
func sortRenders(ents []fs.DirEntry) {
	sort.SliceStable(ents, func(i, j int) bool {
		vi, ri, oki := renderParts(ents[i].Name())
		vj, rj, okj := renderParts(ents[j].Name())
		switch {
		case oki != okj:
			return oki
		case !oki:
			return ents[i].Name() < ents[j].Name()
		case vi != vj:
			return vi < vj
		}
		return ri < rj
	})
}

// roleSwitches names each fallback on the approval card. A row's vendor, model
// and effort identify it in ork's recorded lineup.
func roleSwitches(rec runrec.Record) []string {
	var out []string
	pending := map[string]runrec.RoleRecord{}
	for _, r := range rec.Roles {
		if prev, ok := pending[r.Role]; ok {
			out = append(out, fmt.Sprintf("role switched: %s %s %s %s to %s %s %s (%s)", r.Role, prev.Vendor, prev.Model, prev.Effort, r.Vendor, r.Model, r.Effort, switchReason(prev.Exit)))
			delete(pending, r.Role)
		}
		if fallbackExit(ExitClass(r.Exit)) {
			pending[r.Role] = r
		}
	}
	return out
}

func switchReason(exit string) string {
	switch ExitClass(exit) {
	case ExitOutOfCredits:
		return "out of credits"
	case ExitRateLimited:
		return "rate limited"
	case ExitError:
		return "error exit"
	default:
		return exit
	}
}
