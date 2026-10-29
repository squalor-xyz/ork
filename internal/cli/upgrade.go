// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"debug/buildinfo"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/roles"
)

// This file is doctor's upgrade help (S178, *proposed*). It reads the local
// facts that say where slicer and adj came from and whether they are behind,
// and it builds the commands that the owner can run. Doctor never runs one of
// them, makes no network call, and starts git only with `rev-parse`,
// `describe` and `rev-list`.

// readBuildInfo reads the Go build information of a binary. Tests replace it.
var readBuildInfo = buildinfo.ReadFile

// The install kinds of toolCheck.Install.
const (
	installCheckout = "checkout"  // slicer: an editable install of a checkout
	installVenv     = "venv"      // slicer: a virtual environment, for example from PyPI
	installGoBinary = "go-binary" // adj: a Go binary with build information
	installUnknown  = "unknown"
)

// checkoutInfo is what local git says about a checkout. Behind counts the
// commits that the upstream branch has and HEAD lacks, as of the last fetch;
// it is -1 when there is no upstream to compare with.
type checkoutInfo struct {
	Dir      string `json:"dir"`
	Describe string `json:"describe,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Behind   int    `json:"behind"`
}

// buildInfo is what a Go binary records about its own build.
type buildInfo struct {
	Module   string `json:"module"`
	Version  string `json:"version,omitempty"`
	Revision string `json:"revision,omitempty"`
	Modified bool   `json:"modified"`
}

// upgradeHint names a tool that is behind or missing (or any tool, with
// --upgrade) and the commands for the owner to run. A hint with no commands
// carries a Note that says what to read instead.
type upgradeHint struct {
	Tool     string   `json:"tool"`
	Reason   string   `json:"reason"`
	Commands []string `json:"commands"`
	Note     string   `json:"note,omitempty"`
}

// gitRead runs one read-only git call in dir and returns the first line of
// its output.
func gitRead(ctx context.Context, git, dir string, args ...string) (string, error) {
	if git == "" {
		git = "git"
	}
	res, err := clirun.Run(ctx, clirun.Call{Bin: git, Args: append([]string{"-C", dir}, args...), Timeout: versionTimeout})
	if err != nil {
		return "", err
	}
	return firstLine(res.Stdout), nil
}

// describeInstall fills Resolved, Install, Checkout and Build of slicer or
// adj from the binary on disk. It only reads: symlinks, the binary's build
// information, and a checkout's local git state. Anything it cannot read
// leaves Install at "unknown"; it never sets Error.
func describeInstall(ctx context.Context, tc *toolCheck, git string) {
	if !tc.Present {
		return
	}
	tc.Install = installUnknown
	path, err := filepath.Abs(tc.Path)
	if err != nil {
		return
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return
	}
	tc.Resolved = resolved
	switch tc.Name {
	case "slicer":
		describeSlicer(ctx, tc, git)
	case "adj":
		if info, err := readBuildInfo(resolved); err == nil && info != nil {
			tc.Install = installGoBinary
			b := &buildInfo{Module: info.Main.Path, Version: info.Main.Version}
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					b.Revision = s.Value
				case "vcs.modified":
					b.Modified = s.Value == "true"
				}
			}
			tc.Build = b
		}
	}
}

// describeSlicer tells an editable install of a checkout (D/.venv/bin/slicer
// with D the root of a git repository) from any other virtual environment
// (V/bin/slicer with V/pyvenv.cfg).
func describeSlicer(ctx context.Context, tc *toolCheck, git string) {
	bin := filepath.Dir(tc.Resolved)
	if filepath.Base(bin) != "bin" {
		return
	}
	venv := filepath.Dir(bin)
	if filepath.Base(venv) == ".venv" {
		dir := filepath.Dir(venv)
		if top, err := gitRead(ctx, git, dir, "rev-parse", "--show-toplevel"); err == nil {
			if real, err := filepath.EvalSymlinks(top); err == nil && real == dir {
				tc.Install = installCheckout
				tc.Checkout = readCheckout(ctx, git, dir)
				return
			}
		}
	}
	if _, err := os.Stat(filepath.Join(venv, "pyvenv.cfg")); err == nil {
		tc.Install = installVenv
	}
}

func readCheckout(ctx context.Context, git, dir string) *checkoutInfo {
	c := &checkoutInfo{Dir: dir, Behind: -1}
	c.Describe, _ = gitRead(ctx, git, dir, "describe", "--tags", "--always")
	c.Branch, _ = gitRead(ctx, git, dir, "rev-parse", "--abbrev-ref", "HEAD")
	// "AHEAD<TAB>BEHIND". It fails when the branch has no upstream.
	if counts, err := gitRead(ctx, git, dir, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); err == nil {
		if f := strings.Fields(counts); len(f) == 2 {
			if n, err := strconv.Atoi(f[1]); err == nil {
				c.Behind = n
				c.Upstream, _ = gitRead(ctx, git, dir, "rev-parse", "--abbrev-ref", "@{upstream}")
			}
		}
	}
	return c
}

// contractDirection compares the version in the contract line that slicer
// printed with the accepted set: -1 when it is older than the lowest accepted
// version, 1 when it is newer than the highest, and 0 otherwise (also when
// there is no contract line to read).
func contractDirection(found string) (dir, version int) {
	v, _ := roles.CheckContractLine(found)
	accepted := roles.AcceptedContracts()
	if v == 0 || len(accepted) == 0 {
		return 0, v
	}
	switch {
	case v < slices.Min(accepted):
		return -1, v
	case v > slices.Max(accepted):
		return 1, v
	}
	return 0, v
}

func acceptedList() string {
	var parts []string
	for _, v := range roles.AcceptedContracts() {
		parts = append(parts, fmt.Sprintf("v%d", v))
	}
	return strings.Join(parts, ", ")
}

// buildHints lists, in the order slicer, adj, ork, the tools that are behind
// or missing, or every one of them when all is set (--upgrade). A hint has the
// commands for the install source that describeInstall found, with
// ADJ_CHECKOUT and ORK_CHECKOUT where only the owner knows the path.
func buildHints(rep DoctorReport, all bool) []upgradeHint {
	hints := []upgradeHint{}
	tool := func(name string) toolCheck {
		for _, t := range rep.Tools {
			if t.Name == name {
				return t
			}
		}
		return toolCheck{Name: name}
	}
	dir, version := contractDirection(rep.Contract.Found)

	slicer := tool("slicer")
	reason := ""
	switch {
	case !slicer.Present:
		reason = "not found on PATH"
	case dir < 0:
		reason = fmt.Sprintf("slicer prints relay v%d, older than ork accepts (%s)", version, acceptedList())
	case slicer.Checkout != nil && slicer.Checkout.Behind > 0:
		c := slicer.Checkout
		upstream := c.Upstream
		if upstream == "" {
			upstream = "its upstream"
		}
		reason = fmt.Sprintf("%d %s behind %s as of its last fetch", c.Behind, plural(c.Behind, "commit"), upstream)
	case all:
		reason = "requested with --upgrade"
	}
	if reason != "" {
		hints = append(hints, slicerHint(slicer, reason))
	}

	adj := tool("adj")
	reason = ""
	switch {
	case !adj.Present:
		reason = "not found on PATH"
	case all:
		reason = "requested with --upgrade"
	}
	if reason != "" {
		hints = append(hints, adjHint(adj, reason))
	}

	reason = ""
	switch {
	case dir > 0:
		reason = fmt.Sprintf("slicer prints relay v%d, newer than ork accepts (%s)", version, acceptedList())
	case all:
		reason = "requested with --upgrade"
	}
	if reason != "" {
		hints = append(hints, orkHint(rep.Binary, reason))
	}
	return hints
}

func slicerHint(tc toolCheck, reason string) upgradeHint {
	h := upgradeHint{Tool: "slicer", Reason: reason, Commands: []string{}}
	switch {
	case tc.Install == installCheckout && tc.Checkout != nil:
		dir := shellWord(tc.Checkout.Dir)
		h.Commands = []string{
			"git -C " + dir + " pull --ff-only",
			shellWord(filepath.Join(tc.Checkout.Dir, ".venv", "bin", "pip")) + " install -e " + dir,
		}
	case tc.Install == installVenv:
		venv := filepath.Dir(filepath.Dir(tc.Resolved))
		h.Commands = []string{shellWord(filepath.Join(venv, "bin", "pip")) + " install --upgrade squalor-slicer"}
	case !tc.Present:
		h.Note = "not installed: read slicer/README.md#install"
	default:
		h.Note = "install source not recognized: read slicer/README.md#install"
	}
	return h
}

func adjHint(tc toolCheck, reason string) upgradeHint {
	h := upgradeHint{Tool: "adj", Reason: reason, Commands: []string{}}
	if !tc.Present {
		h.Note = "not installed: read adjudicate/README.md#install"
		return h
	}
	if tc.Install != installGoBinary {
		h.Note = "install source not recognized: read adjudicate/README.md#install"
		return h
	}
	h.Commands = []string{
		"git -C ADJ_CHECKOUT pull --ff-only",
		"go -C ADJ_CHECKOUT build -o " + shellWord(tc.Resolved) + " ./cmd/adj",
	}
	h.Note = "ADJ_CHECKOUT = your checkout; see adjudicate/README.md#build-from-source"
	return h
}

func orkHint(binary, reason string) upgradeHint {
	h := upgradeHint{Tool: "ork", Reason: reason, Commands: []string{}}
	if binary == "" {
		h.Note = "binary path unknown: build ORK_CHECKOUT/cmd/ork, restart the daemon"
		return h
	}
	bin := shellWord(binary)
	h.Commands = []string{
		"git -C ORK_CHECKOUT pull --ff-only",
		"go -C ORK_CHECKOUT build -o " + bin + " ./cmd/ork",
		bin + " daemon restart",
	}
	h.Note = "ORK_CHECKOUT is your ork checkout"
	return h
}
