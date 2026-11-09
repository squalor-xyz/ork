// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// vendorNames are the cloud CLIs and their local launcher identities.
var vendorNames = []string{"claude", "codex", "grok", "claude-local", "codex-local", "grok-local"}

// vendorList lists the supported vendor identities.
func vendorList() string {
	return "claude, codex or grok, or their -local launchers"
}

// vendor is `ork vendor [list]`, `ork vendor out NAME [--reason TEXT]
// [--until TIME]` and `ork vendor back NAME`: the vendors the daemon remembers
// as out (S169, *proposed*). A vendor stays out for 60 minutes after a
// rate-limit or out-of-credits exit, or until `back`; a daemon restart clears
// that mark. A mark the owner sets with `out` holds until `back`, or until TIME
// when --until is given, and a restart keeps it (S299, *proposed*).
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
		var reason, until *string
		if sub == "out" {
			reason = fs.String("reason", "", "why the vendor is out")
			until = fs.String("until", "", "when the mark ends: HH:MM, YYYY-MM-DD HH:MM or a full date and zone")
		}
		pos, err := parse(fs, rest)
		if err != nil {
			return o.usage("vendor %s: %v", sub, err)
		}
		if len(pos) == 0 {
			return o.usage("vendor %s needs a vendor name (%s)", sub, vendorList())
		}
		if len(pos) > 1 {
			return o.usage("vendor %s takes one vendor name, got %q", sub, strings.Join(pos, " "))
		}
		name := pos[0]
		if !slices.Contains(vendorNames, name) {
			return o.usage("vendor %s: unsupported vendor %q; use %s", sub, name, vendorList())
		}
		p := daemon.SetVendorParams{Vendor: name, Out: sub == "out"}
		if reason != nil {
			p.Reason = *reason
		}
		untilGiven := false
		fs.Visit(func(f *flag.Flag) { untilGiven = untilGiven || f.Name == "until" })
		if untilGiven {
			now := time.Now()
			if a.Now != nil {
				now = a.Now()
			}
			end, err := parseVendorUntil(*until, now)
			if err != nil {
				return o.usage("vendor out: %v", err)
			}
			p.Until = end
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

// parseVendorUntil reads the end of `ork vendor out --until`: a clock time
// ("07:12", the next time the clock shows it), a day and time ("2026-11-04
// 07:12", in the zone of now), or an RFC 3339 time. The end must be after now.
func parseVendorUntil(text string, now time.Time) (time.Time, error) {
	text = strings.TrimSpace(text)
	var end time.Time
	if clock, err := time.ParseInLocation("15:04", text, now.Location()); err == nil {
		end = time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
		if !end.After(now) {
			end = end.AddDate(0, 0, 1)
		}
	} else if day, err := time.ParseInLocation("2006-01-02 15:04", text, now.Location()); err == nil {
		end = day
	} else if stamp, err := time.Parse(time.RFC3339, text); err == nil {
		end = stamp
	} else {
		return time.Time{}, fmt.Errorf("--until: cannot read %q; use HH:MM (07:12), YYYY-MM-DD HH:MM (2026-11-04 07:12) or a full date and zone (2026-11-04T07:12:00+01:00)", text)
	}
	if !end.After(now) {
		return time.Time{}, fmt.Errorf("--until: %s is not in the future", text)
	}
	return end, nil
}
