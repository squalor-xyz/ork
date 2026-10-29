// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"fmt"
	"maps"
	"slices"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
)

// validateProjectExtras checks the environment_lines, permissions and
// vendor_env values of a projects.set call (S174, *proposed*), before the
// registry lock. Which roles may hold which permission is checked later, on
// the project as it would be saved. A key removed with a nil value is not
// checked, so an entry that a hand edit made odd can still be removed. No
// message quotes a vendor_env value.
func validateProjectExtras(p SetProjectParams) error {
	bad := func(err error) error {
		return &ipc.Error{Code: CodeBadParams, Message: err.Error()}
	}
	if p.EnvironmentLines != nil {
		for _, line := range *p.EnvironmentLines {
			if err := registry.ValidateEnvLine(line); err != nil {
				return bad(err)
			}
		}
	}
	for _, role := range slices.Sorted(maps.Keys(p.Permissions)) {
		if entry := p.Permissions[role]; entry != nil {
			if err := registry.ValidatePermissionEntry(role, *entry); err != nil {
				return bad(err)
			}
		}
	}
	for _, vendor := range slices.Sorted(maps.Keys(p.VendorEnv)) {
		for _, key := range slices.Sorted(maps.Keys(p.VendorEnv[vendor])) {
			if v := p.VendorEnv[vendor][key]; v != nil {
				if err := registry.ValidateVendorEnv(vendor, key, *v); err != nil {
					return bad(err)
				}
			}
		}
	}
	return nil
}

// withProjectExtras returns proj with the environment_lines, permissions and
// vendor_env changes of p applied, on copies: proj is not changed. Every role
// in the permissions patch must resolve (engine.ResolvePermission) in the
// result, so a read-only role cannot get an allowlist and an unknown role is
// refused; the exception is a null entry for a key already in the file. An emptied map is dropped, so the file does not hold `{}`.
func withProjectExtras(proj registry.Project, p SetProjectParams) (registry.Project, error) {
	if p.EnvironmentLines != nil {
		proj.EnvironmentLines = slices.Clone(*p.EnvironmentLines)
		if len(proj.EnvironmentLines) == 0 {
			proj.EnvironmentLines = nil
		}
	}
	if len(p.Permissions) != 0 {
		had := proj.Permissions
		perms := maps.Clone(proj.Permissions)
		if perms == nil {
			perms = map[string]registry.Permission{}
		}
		for role, entry := range p.Permissions {
			if entry == nil {
				delete(perms, role)
				continue
			}
			perms[role] = registry.Permission{Preset: entry.Preset, Allow: slices.Clone(entry.Allow)}
		}
		if len(perms) == 0 {
			perms = nil
		}
		proj.Permissions = perms
		for _, role := range slices.Sorted(maps.Keys(p.Permissions)) {
			// A null entry removes any role key that is in the file, even one
			// ork does not know, so a hand-edited typo can be cleaned up.
			if _, existed := had[role]; existed && p.Permissions[role] == nil {
				continue
			}
			if _, err := engine.ResolvePermission(proj, role); err != nil {
				return registry.Project{}, &ipc.Error{Code: CodeBadParams, Message: err.Error()}
			}
		}
	}
	if len(p.VendorEnv) != 0 {
		env := map[string]map[string]string{}
		for vendor, keys := range proj.VendorEnv {
			env[vendor] = maps.Clone(keys)
		}
		for vendor, keys := range p.VendorEnv {
			for key, value := range keys {
				if value == nil {
					delete(env[vendor], key)
					continue
				}
				if env[vendor] == nil {
					env[vendor] = map[string]string{}
				}
				env[vendor][key] = *value
			}
			if len(env[vendor]) == 0 {
				delete(env, vendor)
			}
		}
		if len(env) == 0 {
			env = nil
		}
		proj.VendorEnv = env
	}
	return proj, nil
}

// settingsResult is the file-level settings of f, stored and effective.
func settingsResult(f *registry.File) SettingsResult {
	res := SettingsResult{
		GlobalCap: f.GlobalCap, DaemonIdleMinutes: f.DaemonIdleMinutes,
		EffectiveGlobalCap: f.GlobalCap, EffectiveDaemonIdleMinutes: f.DaemonIdleMinutes,
	}
	if res.EffectiveGlobalCap == 0 {
		res.EffectiveGlobalCap = defaultGlobalCap
	}
	if res.EffectiveDaemonIdleMinutes == 0 {
		res.EffectiveDaemonIdleMinutes = registry.DefaultDaemonIdleMinutes
	}
	return res
}

// getSettings returns the registry's global_cap and daemon_idle_minutes with
// the values the daemon uses (S174, *proposed*).
func (d *Daemon) getSettings() (SettingsResult, error) {
	f, err := d.loadRegistry()
	if err != nil {
		return SettingsResult{}, err
	}
	return settingsResult(f), nil
}

// setSettings changes global_cap and daemon_idle_minutes (S174, *proposed*).
// Unfinished runs do not block it: the cap is read for every admission and the
// idle minutes each time the idle clock arms. It holds the run lock through
// Save, as setProject does, and then starts the queued runs that now fit, so a
// raised cap takes effect at once. A lowered cap stops nothing.
func (d *Daemon) setSettings(p SetSettingsParams) (SettingsResult, error) {
	bad := func(format string, args ...any) error {
		return &ipc.Error{Code: CodeBadParams, Message: fmt.Sprintf(format, args...)}
	}
	if p.GlobalCap == nil && p.DaemonIdleMinutes == nil {
		return SettingsResult{}, bad("settings.set sets no field")
	}
	for _, n := range []struct {
		name string
		v    *int
	}{{"global_cap", p.GlobalCap}, {"daemon_idle_minutes", p.DaemonIdleMinutes}} {
		if n.v != nil && *n.v < 0 {
			return SettingsResult{}, bad("%s must not be negative, got %d", n.name, *n.v)
		}
	}

	d.regMu.Lock()
	defer d.regMu.Unlock()
	f, err := d.loadRegistry()
	if err != nil {
		return SettingsResult{}, err
	}
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	if p.GlobalCap != nil {
		f.GlobalCap = *p.GlobalCap
	}
	if p.DaemonIdleMinutes != nil {
		f.DaemonIdleMinutes = *p.DaemonIdleMinutes
	}
	if err := registry.Save(d.registryPath(), f); err != nil {
		return SettingsResult{}, &ipc.Error{Code: CodeRegistry, Message: err.Error()}
	}
	d.runs.lastRegistry = f
	d.startQueuedLocked(f)
	return settingsResult(f), nil
}
