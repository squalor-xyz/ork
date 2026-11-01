// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// readLine ends a prompt line when closed input cannot echo a newline.
func readLine(in *bufio.Reader, w io.Writer) (string, error) {
	line, err := in.ReadString('\n')
	if err == io.EOF {
		fmt.Fprintln(w)
	}
	return line, err
}

// wrapLines wraps by rune count and hangs continuation lines beneath the
// first line's indentation. Only words that cannot fit are split.
func wrapLines(text string, width int, hang string) []string {
	if width <= 0 {
		return []string{text}
	}
	leading := text[:len(text)-len(strings.TrimLeft(text, " "))]
	// Preserve the full text when indentation leaves no room for words.
	if utf8.RuneCountInString(leading+hang) >= width {
		return []string{text}
	}
	prefix := leading
	var lines []string
	line := prefix
	for _, word := range strings.Fields(text) {
		if line != prefix {
			if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= width {
				line += " " + word
				continue
			}
			lines = append(lines, line)
			prefix = leading + hang
			line = prefix
		}
		for utf8.RuneCountInString(prefix)+utf8.RuneCountInString(word) > width {
			room := width - utf8.RuneCountInString(prefix)
			runes := []rune(word)
			lines = append(lines, prefix+string(runes[:room]))
			word = string(runes[room:])
			prefix = leading + hang
			line = prefix
		}
		line += word
	}
	if line != prefix || len(lines) == 0 {
		lines = append(lines, line)
	}
	return lines
}

func plainLine(s string) string {
	return strings.Join(strings.Fields(runrec.PlainText(s)), " ")
}

func (o *output) failErrWith(prefix string, err error) int {
	if o.json {
		return o.failErr(err)
	}
	e := ipc.ErrorOf(err)
	if e.Code == daemon.CodeRefused {
		prefix = ""
	}
	return o.fail(ExitFailure, e.Code, prefix+e.Message)
}
