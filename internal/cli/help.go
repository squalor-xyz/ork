// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"errors"
	"flag"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The usage text has one block per command. A block starts at a line "  ork
// CMD ..." and goes on over the lines indented 30 columns, where the
// descriptions start (S290).
const (
	blockStart  = "  ork "
	blockIndent = 30
)

var (
	commandWord    = regexp.MustCompile(`^[a-z][a-z-]*$`)
	optionalWord   = regexp.MustCompile(`^\[([a-z][a-z-]*)\]$`)
	helpFlagWords  = []string{"-h", "--help"}
	groupCommands  = []string{"adj", "daemon", "projects", "roles", "settings", "vendor", "wave"}
	errHelpMessage = flag.ErrHelp.Error()
)

// isHelpFlag reports whether arg asks for help.
func isHelpFlag(arg string) bool { return slices.Contains(helpFlagWords, arg) }

// blockNames are the command words a block answers to: the words of its first
// line up to the first argument, and with an optional word in square brackets,
// as in "ork vendor [list]", both without and with it.
func blockNames(first string) [][]string {
	end := min(len(first), blockIndent)
	fields := strings.Fields(first[:end])[1:] // drop "ork"
	var words []string
	for _, f := range fields {
		if commandWord.MatchString(f) {
			words = append(words, f)
			continue
		}
		if m := optionalWord.FindStringSubmatch(f); m != nil {
			return [][]string{slices.Clone(words), append(slices.Clone(words), m[1])}
		}
		break
	}
	return [][]string{words}
}

// DaemonRunUsage is what ork daemon run --help and ork help daemon run print.
// ork daemon run is the daemon's entry point and keeps its own usage line, so
// cmd/ork and this package print the same text for it.
const DaemonRunUsage = "usage: ork daemon run\n"

// commandUsage returns every block of the usage text for the command that
// words name, as the usage prints them. words may be a group ("projects"), and
// then the result holds all of the group's blocks. It reports false when no
// command matches. ork daemon run is the exception: it returns DaemonRunUsage.
func commandUsage(words []string) (string, bool) {
	if len(words) == 0 {
		return "", false
	}
	if slices.Equal(words, []string{"daemon", "run"}) {
		return DaemonRunUsage, true
	}
	var out strings.Builder
	var names [][]string
	for _, line := range strings.Split(strings.TrimSuffix(usageText, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, blockStart):
			names = blockNames(line)
			if matchesWords(names, words) {
				out.WriteString(line + "\n")
			}
		case strings.HasPrefix(line, strings.Repeat(" ", blockIndent)) && names != nil:
			if matchesWords(names, words) {
				out.WriteString(line + "\n")
			}
		default:
			names = nil
		}
	}
	return out.String(), out.Len() > 0
}

// matchesWords reports whether words are the start of one of names.
func matchesWords(names [][]string, words []string) bool {
	return slices.ContainsFunc(names, func(name []string) bool {
		return len(name) >= len(words) && slices.Equal(name[:len(words)], words)
	})
}

// printHelp prints the usage of the command words names and returns ExitOK. A
// name that matches no command is a usage error.
func (o *output) printHelp(words ...string) int {
	text, ok := commandUsage(words)
	if !ok {
		return o.usage("unknown command %q", strings.Join(words, " "))
	}
	fmt.Fprint(o.stdout, text)
	return ExitOK
}

// helpRequested reports the command words of a usage error that a command made
// of the flag package's help request. Every command words its parse failure as
// "CMD: " followed by the error, and o.usage is where they all pass.
func helpRequested(msg string, a []any) ([]string, bool) {
	for _, v := range a {
		if err, ok := v.(error); ok && errors.Is(err, flag.ErrHelp) {
			name := strings.TrimSuffix(msg, ": "+errHelpMessage)
			if name == msg {
				return nil, false
			}
			return strings.Fields(name), true
		}
	}
	return nil, false
}
