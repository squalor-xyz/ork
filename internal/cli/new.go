// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
)

func (a *App) newSlice(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	p := daemon.ItemNewParams{}
	var fromNote string
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	fs.StringVar(&p.Why, "why", "", "description, or - to read stdin")
	fs.StringVar(&p.From, "from", "", "source item")
	fs.StringVar(&fromNote, "from-note", "", "source note number (one-based)")
	fs.StringVar(&p.Size, "size", "", "S, M or L")
	fs.IntVar(&p.Importance, "importance", 0, "1-3")
	fs.IntVar(&p.Urgency, "urgency", 0, "1-3")
	fs.IntVar(&p.Effort, "effort", 0, "1-3")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("new: %v", err)
	}
	if len(pos) != 2 {
		return o.usage("new: takes PROJECT TITLE; quote TITLE")
	}
	p.Project, p.Title = pos[0], pos[1]
	noteSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "from-note" {
			noteSet = true
		}
	})
	if noteSet {
		n, err := strconv.Atoi(fromNote)
		if err != nil || n < 1 {
			return o.usage("new: --from-note must be a positive number")
		}
		if strings.TrimSpace(p.From) == "" {
			return o.usage("new: --from-note needs --from ITEM")
		}
		p.FromNote = n
	}
	if err := engine.ValidateNewSlice(engine.NewSliceConfig{Title: p.Title, From: p.From, FromNote: p.FromNote, Size: p.Size, Importance: p.Importance, Urgency: p.Urgency, Effort: p.Effort}); err != nil {
		return o.usage("new: %v", err)
	}
	if p.Why == "-" {
		input := a.Input
		if input == nil {
			input = os.Stdin
		}
		b, err := io.ReadAll(input)
		if err != nil {
			return o.failErr(err)
		}
		p.Why = string(b)
	}
	var out daemon.ItemNewResult
	if code := a.call(ctx, o, daemon.MethodItemNew, p, &out); code != 0 {
		return code
	}
	if o.json {
		return o.printJSON(out)
	}
	if out.Existing {
		fmt.Fprintf(o.stdout, "%s is already filed\n", out.ID)
	} else {
		fmt.Fprintf(o.stdout, "filed %s\n", out.ID)
	}
	return ExitOK
}
