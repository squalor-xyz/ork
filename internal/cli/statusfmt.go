// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// The text helpers of ork status (S240, proposed). They are pure: the caller
// gives the clock.

// clockText is t in the local zone as HH:MM when it falls on the local day of
// now, and as MM-DD HH:MM on another day.
func clockText(t, now time.Time) string {
	t, now = t.In(time.Local), now.In(time.Local)
	ty, tm, td := t.Date()
	ny, nm, nd := now.Date()
	if ty == ny && tm == nm && td == nd {
		return t.Format("15:04")
	}
	return t.Format("01-02 15:04")
}

// ageText is a span as Ns under a minute, Nm under an hour, HhMMm under a day
// and NdHHh above. A negative span (clocks that disagree) prints as zero.
func ageText(d time.Duration) string {
	d = max(d, 0)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	}
	return fmt.Sprintf("%dd%02dh", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
}

// stateTime says when the run's state began: the time it began to wait for the
// owner, or the time its record was last written (the entry into the state
// for checks, which writes nothing while it runs). Nothing for a zero time.
func stateTime(r source.EngineRun, now time.Time) string {
	switch r.State {
	case runrec.StatePaused, runrec.StateAwaitingApproval:
		if r.WaitStartedAt.IsZero() {
			return ""
		}
		return fmt.Sprintf(", waiting since %s (%s)", clockText(r.WaitStartedAt, now), ageText(now.Sub(r.WaitStartedAt)))
	}
	if r.UpdatedAt.IsZero() {
		return ""
	}
	return fmt.Sprintf(", updated %s (%s ago)", clockText(r.UpdatedAt, now), ageText(now.Sub(r.UpdatedAt)))
}

// nextSuffix is what the daemon appends to a wait to name the owner's next
// command (internal/daemon/runs.go).
const nextSuffix = "; next: "

// splitWait cleans a wait of terminal controls and splits it into its
// non-empty lines and the command the daemon's "; next: " suffix names. The
// suffix ends the last line; with none, next is empty.
func splitWait(wait string) (lines []string, next string) {
	for _, line := range strings.Split(runrec.PlainText(wait), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil, ""
	}
	last := len(lines) - 1
	if i := strings.LastIndex(lines[last], nextSuffix); i >= 0 && strings.TrimSpace(lines[last][i+len(nextSuffix):]) != "" {
		next = strings.TrimSpace(lines[last][i+len(nextSuffix):])
		lines[last] = lines[last][:i]
		if strings.TrimSpace(lines[last]) == "" {
			lines = lines[:last:last]
			if len(lines) == 0 {
				lines = nil
			}
		}
	}
	return lines, next
}

// findingLines is the lines for the findings the needs-you count includes,
// for every run of the project, hidden or not (adj 125, ui.status-list). An
// owner finding is one line. A later finding is two: the finding, then its
// file command on its own line, indented two more columns, so a long run id
// never wraps inside the command. The caller indents every line by two.
func findingLines(runs []source.EngineRun) []string {
	var out []string
	for _, r := range runs {
		for _, f := range r.OpenFindings(runs...) {
			head := fmt.Sprintf("finding %s %d %s: %s", r.ID, f.N, f.Kind, plainLine(f.Title))
			if f.Kind == runrec.FindingOwner {
				out = append(out, head+" (answer it in the TUI run pane)")
				continue
			}
			out = append(out, head, fmt.Sprintf("  file it: ork file %s %d", r.ID, f.N))
		}
	}
	return out
}
