// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
)

func TestSettingsSetAndShow(t *testing.T) {
	keep := registry.Project{Name: "keep", Path: "/p/keep", Cap: 2, EnvironmentLines: []string{"x"}}
	w := newWorld(t, stubSource{}, keep)
	f, err := registry.Load(w.regPath)
	if err != nil {
		t.Fatal(err)
	}
	f.DaemonIdleMinutes = 15
	if err := registry.Save(w.regPath, f); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := run(t, w.app, "settings", "set", "--global-cap", "6", "--daemon-idle-minutes", "0")
	if code != 0 || stdout != "updated settings: global_cap=6, daemon_idle_minutes=unset\n" {
		t.Fatalf("set: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	saved, err := registry.Load(w.regPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.GlobalCap != 6 || saved.DaemonIdleMinutes != 0 || !reflect.DeepEqual(saved.Projects, []registry.Project{keep}) {
		t.Fatalf("saved: %+v", saved)
	}

	code, stdout, stderr = run(t, w.app, "settings", "show")
	if code != 0 || stdout != "global_cap=6 (effective 6)\ndaemon_idle_minutes=unset (effective 10)\n" {
		t.Fatalf("show: exit %d\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}

	code, stdout, _ = run(t, w.app, "settings", "show", "--json")
	var shown struct{ Settings daemon.SettingsResult }
	mustJSON(t, stdout, &shown)
	if code != 0 || shown.Settings != (daemon.SettingsResult{GlobalCap: 6, EffectiveGlobalCap: 6, EffectiveDaemonIdleMinutes: 10}) {
		t.Fatalf("show --json: exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, `"settings": {`) || !strings.Contains(stdout, `"effective_global_cap": 6`) {
		t.Fatalf("show --json shape: %s", stdout)
	}

	// One field alone leaves the other; --json prints the result of a set.
	code, stdout, _ = run(t, w.app, "settings", "set", "--daemon-idle-minutes", "20", "--json")
	var set struct{ Settings daemon.SettingsResult }
	mustJSON(t, stdout, &set)
	if code != 0 || set.Settings != (daemon.SettingsResult{GlobalCap: 6, DaemonIdleMinutes: 20, EffectiveGlobalCap: 6, EffectiveDaemonIdleMinutes: 20}) {
		t.Fatalf("set --json: exit %d: %s", code, stdout)
	}

	// 0 clears the cap; the default shows as the effective value.
	code, stdout, _ = run(t, w.app, "settings", "set", "--global-cap", "0")
	if code != 0 || stdout != "updated settings: global_cap=unset\n" {
		t.Fatalf("clear: exit %d: %q", code, stdout)
	}
	code, stdout, _ = run(t, w.app, "settings", "show")
	if code != 0 || stdout != "global_cap=unset (effective 4)\ndaemon_idle_minutes=20 (effective 20)\n" {
		t.Fatalf("show after clear: exit %d: %q", code, stdout)
	}
}

func TestSettingsUsage(t *testing.T) {
	w := newWorld(t, stubSource{}, registry.Project{Name: "keep", Path: "/p/keep"})
	starts := w.starts.Load()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"settings"}, "settings needs show or set"},
		{[]string{"settings", "nope"}, `unknown settings command "nope"`},
		{[]string{"settings", "set"}, "needs at least one flag (--global-cap, --daemon-idle-minutes)"},
		{[]string{"settings", "set", "--global-cap", "-1"}, "--global-cap must not be negative"},
		{[]string{"settings", "set", "--daemon-idle-minutes", "-5"}, "--daemon-idle-minutes must not be negative"},
		{[]string{"settings", "set", "--global-cap", "many"}, `invalid value "many" for flag -global-cap`},
		{[]string{"settings", "set", "--nope", "1"}, "flag provided but not defined: -nope"},
		{[]string{"settings", "set", "extra", "--global-cap", "1"}, "settings set takes no arguments"},
		{[]string{"settings", "show", "extra"}, "settings show takes no arguments"},
	} {
		code, _, stderr := run(t, w.app, tc.args...)
		if code != 2 || !strings.Contains(stderr, "settings") || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d: %q, want %q", tc.args, code, stderr, tc.want)
		}
	}
	if w.starts.Load() != starts {
		t.Fatal("a usage error started the daemon")
	}
	if saved, _ := registry.Load(w.regPath); saved.GlobalCap != 0 || saved.DaemonIdleMinutes != 0 {
		t.Fatalf("a usage error changed the registry: %+v", saved)
	}
}
