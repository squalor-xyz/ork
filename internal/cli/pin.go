// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/roles"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// registeredProject resolves NAME through the registry file, as doctor does,
// without the daemon. It prints the failure and returns false when NAME is not
// registered or the registry cannot be read.
func (a *App) registeredProject(o *output, name string) (registry.Project, int, bool) {
	path, err := registry.DefaultPath()
	if err != nil {
		return registry.Project{}, o.fail(ExitFailure, "registry", err.Error()), false
	}
	file, err := registry.Load(path)
	if err != nil {
		return registry.Project{}, o.fail(ExitFailure, "registry", err.Error()), false
	}
	for _, p := range file.Projects {
		if p.Name == name {
			return p, ExitOK, true
		}
	}
	return registry.Project{}, o.usage("no project named %q", name), false
}

// projectsPin is `ork projects pin NAME --relay N`: it sets the project's
// slicer relay_contract pin through slicer (adj 97) and reports the pin that
// `slicer ai relay --json` then prints. N must be in the accepted set.
func (a *App) projectsPin(ctx context.Context, o *output, fs *flag.FlagSet, args []string) int {
	relay := fs.Int("relay", 0, "the relay contract version to pin")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("projects pin: %v", err)
	}
	if len(pos) != 1 {
		return o.usage("projects pin takes one NAME, got %d arguments", len(pos))
	}
	given := false
	fs.Visit(func(f *flag.Flag) { given = given || f.Name == "relay" })
	if !given {
		return o.usage("projects pin needs --relay N (relay %s)", acceptedList())
	}
	if !roles.ContractAccepted(*relay) {
		return o.usage("projects pin: --relay %d is not a version ork accepts (relay %s)", *relay, acceptedList())
	}
	p, code, ok := a.registeredProject(o, pos[0])
	if !ok {
		return code
	}
	client := slicercli.Client{Bin: a.Tools.Slicer}
	if err := client.SetRelayPin(ctx, p.Path, *relay); err != nil {
		return o.failErr(err)
	}
	// Say what slicer holds now. A slicer that serves another version refuses
	// here, and the error names both versions; the pin stays set.
	contract, err := client.ReadContract(ctx, p.Path)
	if err != nil {
		e := ipc.ErrorOf(err)
		return o.fail(ExitFailure, e.Code, fmt.Sprintf("pinned %s at relay v%d, but slicer refuses it: %s. The pin stays set. Run `ork projects pin %s --relay N` with the version slicer serves, or `ork projects unpin %s`", p.Name, *relay, e.Message, p.Name, p.Name))
	}
	if contract.Pinned == nil || *contract.Pinned != *relay {
		return o.fail(ExitFailure, "pin", fmt.Sprintf("slicer did not keep the pin: it prints %s", pinText(contract.Pinned)))
	}
	if o.json {
		return o.printJSON(map[string]any{"project": p.Name, "pinned": *contract.Pinned})
	}
	fmt.Fprintf(o.stdout, "pinned %s at relay v%d\n", p.Name, *contract.Pinned)
	return ExitOK
}

func pinText(pinned *int) string {
	if pinned == nil {
		return "no pin"
	}
	return fmt.Sprintf("pin v%d", *pinned)
}

// projectsUnpin is `ork projects unpin NAME`: it clears the pin through slicer.
func (a *App) projectsUnpin(ctx context.Context, o *output, fs *flag.FlagSet, args []string) int {
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("projects unpin: %v", err)
	}
	if len(pos) != 1 {
		return o.usage("projects unpin takes one NAME, got %d arguments", len(pos))
	}
	p, code, ok := a.registeredProject(o, pos[0])
	if !ok {
		return code
	}
	if err := (slicercli.Client{Bin: a.Tools.Slicer}).UnsetRelayPin(ctx, p.Path); err != nil {
		return o.failErr(err)
	}
	if o.json {
		return o.printJSON(map[string]any{"project": p.Name, "pinned": nil})
	}
	fmt.Fprintf(o.stdout, "unpinned %s\n", p.Name)
	return ExitOK
}
