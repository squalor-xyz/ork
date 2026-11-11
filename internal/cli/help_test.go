// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// S290: every command prints its own block of the usage text on --help or -h.
// The tests below read usageText on their own, apart from commandUsage, so the
// two cannot agree by sharing a mistake.

// helpApp is an App that fails the test when a command starts or calls a
// daemon. Unlike noDaemon it makes no temporary directory, so it does not skip
// when TMPDIR is too long for a socket path: help never opens one.
func helpApp(t *testing.T) *App {
	return &App{
		Socket: "/nonexistent/ork-help.sock",
		Start:  func(context.Context) error { t.Error("a daemon was started for help"); return nil },
		Call: func(_ context.Context, method string, _, _ any) error {
			t.Errorf("the daemon was called with %s", method)
			return nil
		},
	}
}

var lowerWord = regexp.MustCompile(`^[a-z][a-z-]*$`)

// usageBlock is one command's block of usageText: its first line, which starts
// with "  ork ", and the lines after it that are indented 30 columns.
type usageBlock struct {
	words []string // the command words of the first line, up to the first argument or [
	text  string   // the whole block, each line ending in a newline
}

func usageBlocks(t *testing.T) []usageBlock {
	t.Helper()
	var blocks []usageBlock
	for _, line := range strings.Split(strings.TrimSuffix(usageText, "\n"), "\n") {
		if strings.HasPrefix(line, "  ork ") {
			// The description starts at column 30, so the command is in the
			// first 30 columns.
			var words []string
			for _, tok := range strings.Fields(line[:min(len(line), 30)])[1:] {
				if !lowerWord.MatchString(tok) {
					break
				}
				words = append(words, tok)
			}
			if len(words) == 0 {
				t.Fatalf("no command words in %q", line)
			}
			blocks = append(blocks, usageBlock{words: words, text: line + "\n"})
		} else if strings.HasPrefix(line, strings.Repeat(" ", 30)) && len(blocks) > 0 {
			blocks[len(blocks)-1].text += line + "\n"
		}
	}
	if len(blocks) < 20 {
		t.Fatalf("read only %d blocks from the usage text", len(blocks))
	}
	return blocks
}

// commandWords is the command table: each distinct command, in usage order.
func commandWords(t *testing.T) [][]string {
	t.Helper()
	var table [][]string
	for _, b := range usageBlocks(t) {
		if !slices.ContainsFunc(table, func(w []string) bool { return slices.Equal(w, b.words) }) {
			table = append(table, b.words)
		}
	}
	return table
}

// wantBlocks is the text of every block whose command words start with words.
func wantBlocks(t *testing.T, words ...string) string {
	t.Helper()
	var want strings.Builder
	for _, b := range usageBlocks(t) {
		if len(b.words) >= len(words) && slices.Equal(b.words[:len(words)], words) {
			want.WriteString(b.text)
		}
	}
	if want.Len() == 0 {
		t.Fatalf("no block for %q", words)
	}
	return want.String()
}

// daemonRunHelp is ork daemon run's help: it is the daemon's entry point and
// keeps its own usage line, which cmd/ork prints too, instead of its block.
const daemonRunHelp = "usage: ork daemon run\n"

// wantHelp is what ork WORDS --help prints: the blocks of the command, or the
// usage line of ork daemon run.
func wantHelp(t *testing.T, words ...string) string {
	t.Helper()
	if slices.Equal(words, []string{"daemon", "run"}) {
		return daemonRunHelp
	}
	return wantBlocks(t, words...)
}

func TestEveryCommandPrintsItsOwnHelp(t *testing.T) {
	table := commandWords(t)
	for _, words := range table {
		want := wantHelp(t, words...)
		for _, flag := range []string{"--help", "-h"} {
			name := strings.Join(words, " ") + " " + flag
			t.Run(name, func(t *testing.T) {
				code, stdout, stderr := run(t, helpApp(t), append(slices.Clone(words), flag)...)
				if code != 0 || stderr != "" {
					t.Fatalf("exit %d, stderr %q; want exit 0 and no stderr", code, stderr)
				}
				if stdout != want {
					t.Errorf("stdout:\n%s\nwant the blocks of ork %s:\n%s", stdout, strings.Join(words, " "), want)
				}
				// No other command's block: every first line is for this command.
				prefix := "  ork " + strings.Join(words, " ")
				for _, line := range strings.Split(stdout, "\n") {
					if strings.HasPrefix(line, "  ork ") && !strings.HasPrefix(line, prefix) {
						t.Errorf("another command's block in the help of ork %s: %q", strings.Join(words, " "), line)
					}
				}
			})
		}
	}
}

func TestGroupHelpListsItsSubcommands(t *testing.T) {
	for _, group := range []string{"projects", "wave", "adj", "settings", "vendor", "roles", "daemon"} {
		want := wantBlocks(t, group)
		if n := strings.Count(want, "\n  ork ") + 1; n < 2 {
			t.Fatalf("group %s has %d blocks in the usage text", group, n)
		}
		for _, flag := range []string{"--help", "-h"} {
			t.Run(group+" "+flag, func(t *testing.T) {
				code, stdout, stderr := run(t, helpApp(t), group, flag)
				if code != 0 || stderr != "" {
					t.Fatalf("exit %d, stderr %q; want exit 0 and no stderr", code, stderr)
				}
				if stdout != want {
					t.Errorf("stdout:\n%s\nwant every ork %s block:\n%s", stdout, group, want)
				}
			})
		}
	}
}

func TestHelpSubcommand(t *testing.T) {
	code, stdout, stderr := run(t, helpApp(t), "help", "retry")
	if want := wantBlocks(t, "retry"); code != 0 || stderr != "" || stdout != want {
		t.Errorf("help retry: exit %d, stderr %q, stdout:\n%s\nwant only:\n%s", code, stderr, stdout, want)
	}
	// A group prints all its blocks, as ork projects --help does.
	code, stdout, stderr = run(t, helpApp(t), "help", "projects")
	if want := wantBlocks(t, "projects"); code != 0 || stderr != "" || stdout != want {
		t.Errorf("help projects: exit %d, stderr %q, stdout:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	// vendor list is the optional word of "ork vendor [list]".
	var vendorList string
	for _, b := range usageBlocks(t) {
		if strings.HasPrefix(b.text, "  ork vendor [list]") {
			vendorList = b.text
		}
	}
	if vendorList == "" {
		t.Fatal("no ork vendor [list] block in the usage text")
	}
	for _, args := range [][]string{{"help", "vendor", "list"}, {"vendor", "list", "--help"}} {
		code, stdout, stderr = run(t, helpApp(t), args...)
		if code != 0 || stderr != "" || stdout != vendorList {
			t.Errorf("%v: exit %d, stderr %q, stdout:\n%s\nwant only:\n%s", args, code, stderr, stdout, vendorList)
		}
	}
	code, stdout, stderr = run(t, helpApp(t), "help", "nosuch")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "unknown command") {
		t.Errorf("help nosuch: exit %d, stdout %q, stderr %q; want exit 2 and unknown command", code, stdout, stderr)
	}
	// A help flag among the words of help changes nothing: ork help retry
	// --help is ork help retry.
	for _, args := range [][]string{{"help", "retry", "--help"}, {"help", "retry", "-h"}, {"help", "--help", "retry"}} {
		code, stdout, stderr = run(t, helpApp(t), args...)
		if want := wantBlocks(t, "retry"); code != 0 || stderr != "" || stdout != want {
			t.Errorf("%v: exit %d, stderr %q, stdout:\n%s\nwant only:\n%s", args, code, stderr, stdout, want)
		}
	}
	// help with no argument, with --json or with only a help flag prints the
	// whole usage.
	for _, args := range [][]string{{"help"}, {"help", "--json"}, {"help", "-h"}, {"help", "--help"}} {
		code, stdout, stderr = run(t, helpApp(t), args...)
		if code != 0 || stderr != "" || stdout != usageText {
			t.Errorf("%v: exit %d, stderr %q, stdout %q; want the whole usage", args, code, stderr, stdout)
		}
	}
}

// ork daemon run is served by cmd/ork, which prints the same line for its
// --help. help daemon run and daemon run --help must not differ.
func TestHelpDaemonRunPrintsItsUsageLine(t *testing.T) {
	for _, args := range [][]string{{"help", "daemon", "run"}, {"daemon", "run", "--help"}, {"daemon", "run", "-h"}} {
		code, stdout, stderr := run(t, helpApp(t), args...)
		if code != 0 || stderr != "" || stdout != daemonRunHelp {
			t.Errorf("%v: exit %d, stderr %q, stdout %q; want %q", args, code, stderr, stdout, daemonRunHelp)
		}
	}
}

func TestHelpFlagAfterArgumentsStillHelps(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		words []string
	}{
		{[]string{"retry", "RUN", "--help"}, []string{"retry"}},
		{[]string{"retry", "RUN", "-h"}, []string{"retry"}},
		{[]string{"projects", "add", "/some/path", "--help"}, []string{"projects", "add"}},
		{[]string{"note", "P", "ITEM", "some text", "--help"}, []string{"note"}},
		{[]string{"fix", "RUN", "--help"}, []string{"fix"}},
	} {
		code, stdout, stderr := run(t, helpApp(t), tc.args...)
		if want := wantBlocks(t, tc.words...); code != 0 || stderr != "" || stdout != want {
			t.Errorf("%q: exit %d, stderr %q, stdout:\n%s\nwant:\n%s", tc.args, code, stderr, stdout, want)
		}
	}
}

// Every command word of the usage text reaches its own flag parser through
// App.Run, so the table and the dispatcher stay in step. A word the dispatcher
// does not know ends in an "unknown" or "needs" usage error instead.
func TestCommandTableMatchesTheDispatcher(t *testing.T) {
	// ork daemon run is the daemon's entry point; cmd/ork serves it, not App.Run.
	servedByMain := map[string]bool{"daemon run": true}
	for _, words := range commandWords(t) {
		name := strings.Join(words, " ")
		if servedByMain[name] {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if text, ok := commandUsage(words); !ok || text != wantBlocks(t, words...) {
				t.Errorf("commandUsage(%q) = %q, %v", words, text, ok)
			}
			_, _, stderr := run(t, helpApp(t), append(slices.Clone(words), "--no-such-flag")...)
			if !strings.Contains(stderr, "flag provided but not defined: -no-such-flag") {
				t.Errorf("ork %s did not reach its flag parser: %q", name, stderr)
			}
		})
	}
}
