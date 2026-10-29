// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// settings is `ork settings show|set`, the registry's file-level settings
// global_cap and daemon_idle_minutes (S174, *proposed*).
func (a *App) settings(ctx context.Context, o *output, args []string) int {
	if len(args) == 0 {
		return o.usage("settings needs show or set")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("settings "+sub, flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	switch sub {
	case "show":
		pos, err := parse(fs, rest)
		if err != nil {
			return o.usage("settings show: %v", err)
		}
		if len(pos) > 0 {
			return o.usage("settings show takes no arguments, got %q", pos[0])
		}
		return a.withDaemon(ctx, o, func(c *daemon.Client) int {
			res, err := c.Settings(ctx)
			if err != nil {
				return o.failErr(err)
			}
			if o.json {
				return o.printJSON(map[string]any{"settings": res})
			}
			fmt.Fprintf(o.stdout, "global_cap=%s (effective %d)\n", unsetOr(res.GlobalCap), res.EffectiveGlobalCap)
			fmt.Fprintf(o.stdout, "daemon_idle_minutes=%s (effective %d)\n", unsetOr(res.DaemonIdleMinutes), res.EffectiveDaemonIdleMinutes)
			return ExitOK
		})
	case "set":
		globalCap := fs.Int("global-cap", 0, "the most runs that work at once, over all projects; 0 clears it")
		idle := fs.Int("daemon-idle-minutes", 0, "minutes an idle daemon waits before it exits; 0 clears it")
		pos, err := parse(fs, rest)
		if err != nil {
			return o.usage("settings set: %v", err)
		}
		if len(pos) > 0 {
			return o.usage("settings set takes no arguments, got %q", pos[0])
		}
		var p daemon.SetSettingsParams
		given := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
		if given["global-cap"] {
			if *globalCap < 0 {
				return o.usage("settings set: --global-cap must not be negative, got %d", *globalCap)
			}
			p.GlobalCap = globalCap
		}
		if given["daemon-idle-minutes"] {
			if *idle < 0 {
				return o.usage("settings set: --daemon-idle-minutes must not be negative, got %d", *idle)
			}
			p.DaemonIdleMinutes = idle
		}
		if p.GlobalCap == nil && p.DaemonIdleMinutes == nil {
			return o.usage("settings set needs at least one flag (--global-cap, --daemon-idle-minutes)")
		}
		return a.withDaemon(ctx, o, func(c *daemon.Client) int {
			res, err := c.SetSettings(ctx, p)
			if err != nil {
				return o.failErr(err)
			}
			if o.json {
				return o.printJSON(map[string]any{"settings": res})
			}
			// Say what the registry holds now.
			var changed []string
			if p.GlobalCap != nil {
				changed = append(changed, "global_cap="+unsetOr(res.GlobalCap))
			}
			if p.DaemonIdleMinutes != nil {
				changed = append(changed, "daemon_idle_minutes="+unsetOr(res.DaemonIdleMinutes))
			}
			fmt.Fprintf(o.stdout, "updated settings: %s\n", strings.Join(changed, ", "))
			return ExitOK
		})
	}
	return o.usage("unknown settings command %q", sub)
}

// unsetOr prints a stored number, and "unset" for 0.
func unsetOr(n int) string {
	if n == 0 {
		return "unset"
	}
	return fmt.Sprint(n)
}
