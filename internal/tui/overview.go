// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// Words and symbols for each state. A state never relies on colour alone.
const (
	kindDecision = "? decision"
	kindHeld     = "# held"
	kindError    = "! error"
	kindPending  = "~ pending"
	kindExternal = "> started elsewhere"

	kindWidth = len(kindDecision)
	indent    = "  "
)

// refreshing reports whether a project that has data is being read again.
func (m *Model) refreshing(name string) bool {
	ps := m.byName[name]
	return ps != nil && ps.loaded && m.poller.InFlight(name)
}

// overview is the Overview screen for all projects: what needs the owner,
// what is running, and what is ready to land.
func (m *Model) overview(w int) []string {
	th := m.cfg.Theme
	nameW := 0
	for _, p := range m.projects {
		nameW = max(nameW, len(p.reg.Name))
	}
	row := func(style lipgloss.Style, kind string, kindW int, name, detail string, wrap bool) []string {
		// A loaded project with a refresh in flight is marked "~" before its
		// name; the status line says "pending".
		mark := " "
		if m.refreshing(name) {
			mark = th.Pending.Render("~")
		}
		prefix := indent + style.Render(fmt.Sprintf("%-*s", kindW, kind)) + " " + mark + fmt.Sprintf("%-*s", nameW, name) + "  "
		room := w - lipgloss.Width(prefix)
		if room < 10 {
			room = 10
		}
		if !wrap {
			return []string{prefix + clip(detail, room)}
		}
		// Errors are wrapped, never cut, so the code and message stay whole.
		chunks := wrapWords(detail, room)
		out := []string{prefix + chunks[0]}
		pad := strings.Repeat(" ", lipgloss.Width(prefix))
		for _, c := range chunks[1:] {
			out = append(out, pad+c)
		}
		return out
	}

	var needs, running []string
	for _, p := range m.projects {
		if !p.loaded {
			needs = append(needs, row(th.Pending, kindPending, kindWidth, p.reg.Name, "waiting for the first reply", false)...)
			running = append(running, row(th.Pending, kindPending, kindWidth, p.reg.Name, "waiting for the first reply", false)...)
			continue
		}
		s := p.snap
		if s.Items.Err != nil {
			needs = append(needs, row(th.Error, kindError, kindWidth, p.reg.Name, "items: "+errText(s.Items.Err), true)...)
		}
		if s.Open.Err != nil {
			needs = append(needs, row(th.Error, kindError, kindWidth, p.reg.Name, "inbox: "+errText(s.Open.Err), true)...)
		}
		for _, r := range s.Open.Value {
			needs = append(needs, row(th.Decision, kindDecision, kindWidth, p.reg.Name,
				fmt.Sprintf("#%d %s  %s", r.ID, r.Key, r.Title), false)...)
		}
		for _, it := range s.Items.Value {
			if isHeld(it) {
				needs = append(needs, row(th.Held, kindHeld, kindWidth, p.reg.Name,
					fmt.Sprintf("%s  %s  (%s)", it.ID, it.Title, holdFlag), false)...)
			}
		}
		if s.Runs.Err != nil {
			running = append(running, row(th.Error, kindError, kindWidth, p.reg.Name, "relay runs: "+errText(s.Runs.Err), true)...)
		}
		for _, r := range s.Runs.Value {
			running = append(running, row(th.External, kindExternal, len(kindExternal), p.reg.Name, runDetail(r, m.cfg.Now()), true)...)
		}
	}
	if len(needs) == 0 {
		needs = []string{indent + th.Dim.Render("nothing needs you")}
	}
	if len(running) == 0 {
		running = []string{indent + th.Dim.Render("nothing running")}
	}

	var lines []string
	lines = append(lines, th.Heading.Render("Needs you"))
	lines = append(lines, needs...)
	lines = append(lines, "", th.Heading.Render("Running"))
	lines = append(lines, running...)
	lines = append(lines, "", th.Heading.Render("Ready to land"))
	lines = append(lines, indent+th.Dim.Render("none yet: the list of branches to land comes with slice S09"))
	return lines
}

// runDetail describes a relay run started outside ork. ork only shows it. The
// run ID comes before the elapsed time, and the row wraps, so the ID is whole.
func runDetail(r slicercli.RelayRun, now time.Time) string {
	state := r.State
	if r.Counts.Steps > 0 {
		state = fmt.Sprintf("%s, %d of %d steps running", r.State, r.Counts.Running, r.Counts.Steps)
	}
	parts := []string{strings.Join(r.Items, ","), state, r.ID}
	if t, err := time.Parse(time.RFC3339, r.BeganAt); err == nil {
		parts = append(parts, elapsed(now.Sub(t)))
	}
	return strings.Join(parts, "  ")
}

func elapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// errText is a failed call as the tool printed it: the error code and message
// of a tool error, else the error text. Line breaks become spaces; nothing is
// reworded.
func errText(err error) string {
	var s string
	var te *clirun.ToolError
	if errors.As(err, &te) {
		s = te.Code + ": " + te.Message
	} else {
		s = err.Error()
	}
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
}

// clip cuts s to n columns, marking the cut with "...".
func clip(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+3 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "..."
}

// wrapWords breaks s into lines of at most n columns at spaces, keeping every
// character but the spaces at a break. A word longer than n is cut.
func wrapWords(s string, n int) []string {
	var out []string
	cur := ""
	flush := func() { out = append(out, strings.TrimRight(cur, " ")) }
	for i, word := range strings.Split(s, " ") {
		for lipgloss.Width(word) > n {
			if cur != "" {
				flush()
				cur = ""
			}
			head, rest := splitAt(word, n)
			out, word = append(out, head), rest
		}
		switch {
		case i == 0 || cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= n:
			cur += " " + word
		default:
			flush()
			cur = word
		}
	}
	flush()
	return out
}

// splitAt cuts s after the longest prefix that is at most n columns wide.
func splitAt(s string, n int) (head, rest string) {
	r := []rune(s)
	i := len(r)
	for i > 1 && lipgloss.Width(string(r[:i])) > n {
		i--
	}
	return string(r[:i]), string(r[i:])
}
