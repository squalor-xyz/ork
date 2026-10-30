// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// vendorNames are the vendors ork runs; the daemon accepts the same two.
var vendorNames = []string{"claude", "codex"}

// vendor is `ork vendor [list]`, `ork vendor out NAME [--reason TEXT]` and
// `ork vendor back NAME`: the vendors the daemon remembers as out (S169,
// *proposed*). A vendor stays out for 60 minutes after a rate-limit or
// out-of-credits exit, or until `back`. A mark the owner sets with `out` has no
// expiry and holds until `back`. A daemon restart clears both.
func (a *App) vendor(ctx context.Context, o *output, args []string) int {
	sub, rest := "list", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, rest = args[0], args[1:]
	}
	fs := flag.NewFlagSet("vendor "+sub, flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	switch sub {
	case "list":
		pos, err := parse(fs, rest)
		if err != nil {
			return o.usage("vendor list: %v", err)
		}
		if len(pos) > 0 {
			return o.usage("vendor list takes no arguments, got %q", pos[0])
		}
		return a.withDaemon(ctx, o, func(c *daemon.Client) int {
			res, err := c.Vendors(ctx)
			if err != nil {
				return o.failErr(err)
			}
			if o.json {
				return o.printJSON(res)
			}
			if len(res.VendorsOut) == 0 {
				fmt.Fprintln(o.stdout, "no vendor is out")
			}
			for _, v := range res.VendorsOut {
				fmt.Fprintln(o.stdout, v.Sentence())
			}
			return ExitOK
		})
	case "out", "back":
		var reason *string
		if sub == "out" {
			reason = fs.String("reason", "", "why the vendor is out")
		}
		pos, err := parse(fs, rest)
		if err != nil {
			return o.usage("vendor %s: %v", sub, err)
		}
		if len(pos) == 0 {
			return o.usage("vendor %s needs a vendor name (%s)", sub, strings.Join(vendorNames, " or "))
		}
		if len(pos) > 1 {
			return o.usage("vendor %s takes one vendor name, got %q", sub, strings.Join(pos, " "))
		}
		name := pos[0]
		if name != "claude" && name != "codex" {
			return o.usage("vendor %s: unsupported vendor %q; use %s", sub, name, strings.Join(vendorNames, " or "))
		}
		p := daemon.SetVendorParams{Vendor: name, Out: sub == "out"}
		if reason != nil {
			p.Reason = *reason
		}
		return a.withDaemon(ctx, o, func(c *daemon.Client) int {
			res, err := c.SetVendor(ctx, p)
			if err != nil {
				return o.failErr(err)
			}
			if o.json {
				return o.printJSON(res)
			}
			if !p.Out {
				if res.WasOut {
					fmt.Fprintf(o.stdout, "%s is back\n", name)
				} else {
					fmt.Fprintf(o.stdout, "%s was not out\n", name)
				}
				return ExitOK
			}
			for _, v := range res.VendorsOut {
				if v.Vendor == name {
					fmt.Fprintln(o.stdout, v.Sentence())
				}
			}
			return ExitOK
		})
	}
	return o.usage("unknown vendor command %q", sub)
}
