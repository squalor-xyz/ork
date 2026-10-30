// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
)

func (a *App) note(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("note", flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parseNote(fs, args)
	if err != nil {
		return o.usage("note: %v", err)
	}
	if len(pos) != 3 {
		return o.usage("note: takes PROJECT ITEM TEXT, got %d arguments; quote TEXT", len(pos))
	}
	if strings.TrimSpace(pos[1]) == "" {
		return o.usage("note: ITEM must not be empty")
	}
	text := pos[2]
	if text == "-" {
		input := a.Input
		if input == nil {
			input = os.Stdin
		}
		b, err := io.ReadAll(input)
		if err != nil {
			return o.failErr(err)
		}
		text = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	}
	if strings.TrimSpace(text) == "" {
		return o.usage("note: Note must not be empty")
	}
	var out daemon.ItemNoteResult
	if code := a.call(ctx, o, daemon.MethodItemNote, daemon.ItemNoteParams{Project: pos[0], Item: pos[1], Text: text}, &out); code != 0 {
		return code
	}
	if o.json {
		return o.printJSON(struct {
			Item  string `json:"item"`
			Noted bool   `json:"noted"`
		}{out.Item, true})
	}
	fmt.Fprintln(o.stdout, "noted "+out.Item)
	return ExitOK
}

// parseNote treats the argument after ITEM as literal text, even when it starts
// with a dash. Flags can precede the positions or follow TEXT; -- before TEXT
// is optional. Other commands keep the shared parser's behavior.
func parseNote(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for len(pos) < 2 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return pos, nil
	}
	pos = append(pos, args[0])
	if err := fs.Parse(args[1:]); err != nil {
		return nil, err
	}
	return append(pos, fs.Args()...), nil
}
