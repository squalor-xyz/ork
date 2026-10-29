// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

type lineupChain []string

func (c *lineupChain) String() string         { return strings.Join(*c, ",") }
func (c *lineupChain) Set(value string) error { *c = append(*c, value); return nil }

type lineupFlags struct {
	stopAfter                        string
	implementer, reviewer, architect lineupChain
	minutes, tokens                  int
	fs                               *flag.FlagSet
}

func (f *lineupFlags) register(fs *flag.FlagSet) {
	f.fs = fs
	fs.StringVar(&f.stopAfter, "stop-after", "", "land|commit; per-run stop point (default land)")
	fs.Var(&f.implementer, "implementer", "VENDOR:MODEL:EFFORT; repeat for the whole chain")
	fs.Var(&f.reviewer, "reviewer", "VENDOR:MODEL:EFFORT; repeat for the whole chain")
	fs.Var(&f.architect, "architect", "VENDOR:MODEL:EFFORT; repeat for the whole chain, or off")
	fs.IntVar(&f.minutes, "budget-minutes", 0, "run budget; 0 uses project default")
	fs.IntVar(&f.tokens, "budget-tokens", 0, "run budget; 0 uses project default")
}
func (f *lineupFlags) apply(p *daemon.RunParams) error {
	if f.stopAfter != "" && f.stopAfter != runrec.StopAfterLand && f.stopAfter != runrec.StopAfterCommit {
		return fmt.Errorf("--stop-after must be land or commit")
	}
	p.StopAfter = f.stopAfter
	if f.minutes < 0 || f.tokens < 0 {
		return fmt.Errorf("--budget-minutes and --budget-tokens must be zero or more")
	}
	for _, role := range []struct {
		name   string
		values lineupChain
	}{{"implementer", f.implementer}, {"reviewer", f.reviewer}, {"architect", f.architect}} {
		if len(role.values) == 0 {
			continue
		}
		chain := []runrec.LineupEntry{}
		for _, value := range role.values {
			if role.name == "architect" && value == "off" {
				if len(role.values) != 1 {
					return fmt.Errorf("--architect off cannot be combined with an entry")
				}
				continue
			}
			first, last := strings.IndexByte(value, ':'), strings.LastIndexByte(value, ':')
			if first <= 0 || last <= first+1 || last == len(value)-1 {
				form := "VENDOR:MODEL:EFFORT"
				if role.name == "architect" {
					form += " or off"
				}
				return fmt.Errorf("--%s requires %s", role.name, form)
			}
			chain = append(chain, runrec.LineupEntry{Vendor: value[:first], Model: value[first+1 : last], Effort: value[last+1:]})
		}
		if p.Lineup == nil {
			p.Lineup = map[string][]runrec.LineupEntry{}
		}
		p.Lineup[role.name] = chain
	}
	budgetSet := false
	f.fs.Visit(func(v *flag.Flag) {
		if v.Name == "budget-minutes" || v.Name == "budget-tokens" {
			budgetSet = true
		}
	})
	if budgetSet {
		p.Budgets = &registry.Budgets{Minutes: f.minutes, Tokens: f.tokens}
	}
	return nil
}
