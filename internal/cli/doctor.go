// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/roles"
)

// versionTimeout bounds each doctor call.
const versionTimeout = 10 * time.Second

// contractLine matches the first line of `slicer ai relay` (boundary.contract).
var contractLine = regexp.MustCompile(`^Contract: relay v(\d+)\s*$`)

type toolCheck struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Present  bool   `json:"present"`
	Path     string `json:"path,omitempty"`
	Version  string `json:"version,omitempty"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

type contractCheck struct {
	Pin   int    `json:"pin"`
	Found string `json:"found,omitempty"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// DoctorReport is what `ork doctor --json` prints. OK is false when slicer,
// adj or the contract pin fails; a vendor CLI that is missing or broken is
// reported but does not fail the check.
type DoctorReport struct {
	OK       bool          `json:"ok"`
	Tools    []toolCheck   `json:"tools"`
	Contract contractCheck `json:"contract"`
}

func firstLine(b []byte) string {
	for _, line := range strings.Split(string(bytes.TrimSpace(b)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func checkTool(ctx context.Context, name, bin string, required bool) toolCheck {
	if bin == "" {
		bin = name
	}
	tc := toolCheck{Name: name, Required: required}
	path, err := exec.LookPath(bin)
	if err != nil {
		tc.Error = "not found"
		return tc
	}
	tc.Present, tc.Path = true, path
	res, err := clirun.Run(ctx, clirun.Call{Bin: path, Args: []string{"--version"}, Timeout: versionTimeout})
	tc.Version = firstLine(res.Stdout)
	if tc.Version == "" {
		tc.Version = firstLine(res.Stderr)
	}
	if err != nil {
		tc.Error = err.Error()
		return tc
	}
	tc.OK = true
	return tc
}

// checkContract compares the first line of `slicer ai relay` with the pin.
func checkContract(ctx context.Context, slicer toolCheck) contractCheck {
	cc := contractCheck{Pin: roles.ContractVersion}
	if !slicer.Present {
		cc.Error = "slicer is not available"
		return cc
	}
	res, err := clirun.Run(ctx, clirun.Call{Bin: slicer.Path, Args: []string{"ai", "relay"}, Timeout: versionTimeout})
	cc.Found = firstLine(res.Stdout)
	if err != nil {
		cc.Error = err.Error()
		return cc
	}
	m := contractLine.FindStringSubmatch(cc.Found)
	if m == nil {
		cc.Error = "the first line of `slicer ai relay` is not a contract line"
		return cc
	}
	got, _ := strconv.Atoi(m[1])
	if got != roles.ContractVersion {
		cc.Error = fmt.Sprintf("ork pins relay v%d and slicer prints v%d", roles.ContractVersion, got)
		return cc
	}
	cc.OK = true
	return cc
}

// doctor checks the tools ork calls. It runs `--version` on each and
// `slicer ai relay`; it makes no vendor model call and needs no daemon.
func (a *App) doctor(ctx context.Context, o *output, args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.BoolVar(&o.json, "json", o.json, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("doctor: %v", err)
	}
	if len(pos) > 0 {
		return o.usage("doctor takes no arguments, got %q", pos[0])
	}

	t := a.Tools
	rep := DoctorReport{OK: true}
	for _, c := range []struct {
		name, bin string
		required  bool
	}{
		{"slicer", t.Slicer, true}, {"adj", t.Adj, true},
		{"claude", t.Claude, false}, {"codex", t.Codex, false}, {"grok", t.Grok, false},
	} {
		tc := checkTool(ctx, c.name, c.bin, c.required)
		rep.Tools = append(rep.Tools, tc)
		if c.required && !tc.OK {
			rep.OK = false
		}
	}
	rep.Contract = checkContract(ctx, rep.Tools[0])
	if !rep.Contract.OK {
		rep.OK = false
	}

	code := ExitOK
	if !rep.OK {
		code = ExitFailure
	}
	if o.json {
		o.printJSON(rep)
		return code
	}
	for _, tc := range rep.Tools {
		switch {
		case tc.OK:
			detail := tc.Version
			if detail == "" {
				detail = "no version output"
			}
			fmt.Fprintf(o.stdout, "[ok] %s  %s\n", tc.Name, detail)
		case !tc.Present && !tc.Required:
			fmt.Fprintf(o.stdout, "[--] %s  %s (optional)\n", tc.Name, tc.Error)
		default:
			fmt.Fprintf(o.stdout, "[FAIL] %s  %s\n", tc.Name, tc.Error)
		}
	}
	c := rep.Contract
	found := ""
	if c.Found != "" {
		found = fmt.Sprintf(", slicer prints %q", c.Found)
	}
	if c.OK {
		fmt.Fprintf(o.stdout, "[ok] contract  pin v%d%s\n", c.Pin, found)
	} else {
		fmt.Fprintf(o.stdout, "[FAIL] contract  pin v%d%s: %s\n", c.Pin, found, c.Error)
	}
	return code
}
