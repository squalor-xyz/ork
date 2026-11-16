// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/daemon"
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
	return registry.Project{}, o.fail(ExitFailure, daemon.CodeNotFound, fmt.Sprintf("no project named %q; run 'ork projects list'", name)), false
}

// projectsPin is `ork projects pin NAME [--relay N] [--adj M]` (*proposed*):
// it sets the project's slicer relay_contract pin, its adj contract pin, or
// both (adj 97). At least one flag is required. N and M must be in the
// accepted sets. With both flags the slicer pin runs first, and the adj pin
// runs only when that call succeeded. Each pin is reported from the read-back
// the tool then prints.
func (a *App) projectsPin(ctx context.Context, o *output, fs *flag.FlagSet, args []string) int {
	relay := fs.Int("relay", 0, "the relay contract version to pin")
	adjV := fs.Int("adj", 0, "the adj contract version to pin")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("projects pin: %v", err)
	}
	if len(pos) != 1 {
		return o.usage("projects pin takes one NAME, got %d arguments", len(pos))
	}
	relayGiven, adjGiven := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "relay":
			relayGiven = true
		case "adj":
			adjGiven = true
		}
	})
	if !relayGiven && !adjGiven {
		return o.usage("projects pin needs --relay N (%s) or --adj M (%s)", acceptedList(), roles.AcceptedAdjContractsPhrase())
	}
	if relayGiven && !roles.ContractAccepted(*relay) {
		return o.usage("projects pin: --relay %d is not a version ork accepts (%s)", *relay, acceptedList())
	}
	if adjGiven && !roles.AdjContractAccepted(*adjV) {
		return o.usage("projects pin: --adj %d is not a version ork accepts (%s)", *adjV, roles.AcceptedAdjContractsPhrase())
	}
	p, code, ok := a.registeredProject(o, pos[0])
	if !ok {
		return code
	}
	if adjGiven && !p.Adj {
		return o.usage("projects pin: adj is off for %s", p.Name)
	}
	slicer := slicercli.Client{Bin: a.Tools.Slicer}
	adj := adjcli.Client{Bin: a.Tools.Adj}
	if relayGiven {
		if err := slicer.SetRelayPin(ctx, p.Path, *relay); err != nil {
			return o.failErr(err)
		}
	}
	if adjGiven {
		if err := adj.SetPin(ctx, p.Path, *adjV); err != nil {
			if relayGiven {
				e := ipc.ErrorOf(err)
				return o.fail(ExitFailure, e.Code, fmt.Sprintf("pinned %s at relay v%d, but adj failed: %s. The relay pin stays set", p.Name, *relay, e.Message))
			}
			return o.failErr(err)
		}
	}
	var relayPinned *int
	if relayGiven {
		// Say what slicer holds now. A slicer that serves another version refuses
		// here, and the error names both versions; the pin stays set.
		contract, err := slicer.ReadContract(ctx, p.Path)
		if err != nil {
			e := ipc.ErrorOf(err)
			return o.fail(ExitFailure, e.Code, fmt.Sprintf("pinned %s at relay v%d, but slicer refuses it: %s. The pin stays set. Run `ork projects pin %s --relay N` with the version slicer serves, or %s", p.Name, *relay, e.Message, p.Name, slicerUnpinStep(p)))
		}
		if contract.Pinned == nil || *contract.Pinned != *relay {
			return o.fail(ExitFailure, "pin", fmt.Sprintf("slicer did not keep the pin: it prints %s", pinText(contract.Pinned)))
		}
		relayPinned = contract.Pinned
	}
	var spoken adjcli.Contract
	if adjGiven {
		got, err := adj.Contract(ctx, p.Path)
		if err != nil {
			e := ipc.ErrorOf(err)
			return o.fail(ExitFailure, e.Code, fmt.Sprintf("pinned %s at adj-json v%d, but adj refuses it: %s. The pin stays set. Run `ork projects pin %s --adj N` with the version adj serves, or `ork projects unpin %s --adj`", p.Name, *adjV, e.Message, p.Name, p.Name))
		}
		if got.Pinned == nil || *got.Pinned != *adjV {
			return o.fail(ExitFailure, "pin", fmt.Sprintf("adj did not keep the pin: it prints %s", pinText(got.Pinned)))
		}
		if err := roles.CheckAdjContract(got.Contract, got.Version, got.Pinned); err != nil {
			return o.fail(ExitFailure, "pin", fmt.Sprintf("pinned %s at adj-json v%d, but %s. The pin stays set. Run %s", p.Name, *got.Pinned, err.Error(), adjPinOrUnpin(p.Name)))
		}
		spoken = got
	}
	if o.json {
		body := map[string]any{"project": p.Name}
		if relayGiven {
			body["pinned"] = *relayPinned
		}
		if adjGiven {
			body["adj_pinned"] = *spoken.Pinned
		}
		return o.printJSON(body)
	}
	adjLabel := ""
	if adjGiven {
		adjLabel = fmt.Sprintf("adj-json v%d", *spoken.Pinned)
	}
	switch {
	case relayGiven && adjGiven:
		fmt.Fprintf(o.stdout, "pinned %s at relay v%d and %s\n", p.Name, *relayPinned, adjLabel)
	case relayGiven:
		fmt.Fprintf(o.stdout, "pinned %s at relay v%d\n", p.Name, *relayPinned)
	default:
		fmt.Fprintf(o.stdout, "pinned %s at %s\n", p.Name, adjLabel)
	}
	return ExitOK
}

// slicerUnpinStep is the recovery command after a slicer pin problem. On a
// project with adj on, that command also clears the adj pin.
func slicerUnpinStep(p registry.Project) string {
	step := fmt.Sprintf("`ork projects unpin %s`", p.Name)
	if p.Adj {
		return step + " clears the slicer pin and the adj pin"
	}
	return step
}

// adjPinOrUnpin names the adj pin commands ork accepts. The version is the
// accepted set, not a placeholder.
func adjPinOrUnpin(name string) string {
	var parts []string
	for _, v := range roles.AcceptedAdjContracts() {
		parts = append(parts, fmt.Sprintf("`ork projects pin %s --adj %d`", name, v))
	}
	return fmt.Sprintf("%s, or `ork projects unpin %s --adj`", strings.Join(parts, " or "), name)
}

func pinText(pinned *int) string {
	if pinned == nil {
		return "no pin"
	}
	return fmt.Sprintf("pin v%d", *pinned)
}

// projectsUnpin is `ork projects unpin NAME [--adj]` (*proposed*). With no
// flag it clears the slicer pin and, when the project has adj on, the adj pin
// (owner 2026-11-16, projects.unpin-default). The slicer unpin runs first and
// the adj unpin only if it succeeded. `--adj` clears only the adj pin. A
// project with adj off keeps today's slicer-only unpin.
func (a *App) projectsUnpin(ctx context.Context, o *output, fs *flag.FlagSet, args []string) int {
	adjOnly := fs.Bool("adj", false, "clear only the adj pin")
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
	if *adjOnly {
		if !p.Adj {
			return o.usage("projects unpin: adj is off for %s", p.Name)
		}
		if err := (adjcli.Client{Bin: a.Tools.Adj}).UnsetPin(ctx, p.Path); err != nil {
			return o.failErr(err)
		}
		if o.json {
			return o.printJSON(map[string]any{"project": p.Name, "adj_pinned": nil})
		}
		fmt.Fprintf(o.stdout, "unpinned %s (adj)\n", p.Name)
		return ExitOK
	}
	if err := (slicercli.Client{Bin: a.Tools.Slicer}).UnsetRelayPin(ctx, p.Path); err != nil {
		return o.failErr(err)
	}
	clearedAdj := false
	if p.Adj {
		if err := (adjcli.Client{Bin: a.Tools.Adj}).UnsetPin(ctx, p.Path); err != nil {
			e := ipc.ErrorOf(err)
			return o.fail(ExitFailure, e.Code, fmt.Sprintf("cleared %s's relay pin, but adj failed: %s. The adj pin is unchanged; run ork projects unpin %s --adj", p.Name, e.Message, p.Name))
		}
		clearedAdj = true
	}
	if o.json {
		body := map[string]any{"project": p.Name, "pinned": nil}
		if clearedAdj {
			body["adj_pinned"] = nil
		}
		return o.printJSON(body)
	}
	if clearedAdj {
		fmt.Fprintf(o.stdout, "unpinned %s (relay and adj)\n", p.Name)
		return ExitOK
	}
	fmt.Fprintf(o.stdout, "unpinned %s\n", p.Name)
	return ExitOK
}
