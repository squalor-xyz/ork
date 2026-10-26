// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/brand"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/roles"
)

// versionTimeout bounds each doctor call.
const versionTimeout = 10 * time.Second

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
	Accepted []int  `json:"accepted"`
	Found    string `json:"found,omitempty"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

// DoctorReport is what `ork doctor --json` prints. OK is false when slicer,
// adj or the contract check fails; a vendor CLI that is missing or broken is
// reported but does not fail the check. Binary is the resolved path of the
// running ork binary, empty when that path cannot be read.
type DoctorReport struct {
	OK            bool          `json:"ok"`
	Tools         []toolCheck   `json:"tools"`
	Contract      contractCheck `json:"contract"`
	Binary        string        `json:"binary"`
	DaemonBuildID string        `json:"daemon_build_id,omitempty"`
	BehindNotice  string        `json:"behind_notice,omitempty"`
	Warning       string        `json:"warning,omitempty"`
}

// ownBinary returns the path of the running ork binary with symlinks resolved.
func ownBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
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

// checkContract compares the first line of `slicer ai relay` with the
// accepted set of contract versions (boundary.contract).
func checkContract(ctx context.Context, slicer toolCheck) contractCheck {
	cc := contractCheck{Accepted: roles.AcceptedContracts()}
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
	if _, err := roles.CheckContractLine(cc.Found); err != nil {
		cc.Error = err.Error()
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
	// Probe only an existing daemon. Doctor must not start one.
	probeCtx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	var build string
	var probeErr error
	probeStatus := func(call func(context.Context, string, any, any) error) {
		var status daemon.StatusResult
		if err := call(probeCtx, daemon.MethodStatus, daemon.StatusParams{}, &status); err == nil {
			rep.BehindNotice = daemon.BehindWarning(&status)
		}
	}
	if a.Call != nil {
		build, probeErr = daemonBuild(probeCtx, a.Call)
		if probeErr == nil {
			probeStatus(a.Call)
		}
	} else {
		c, err := daemon.Dial(probeCtx, a.Socket)
		probeErr = err
		if err == nil {
			build, probeErr = daemonBuild(probeCtx, c.Call)
			if probeErr == nil {
				probeStatus(c.Call)
			}
			c.Close()
		}
	}
	if probeErr == nil {
		rep.DaemonBuildID = build
		rep.Warning = a.staleWarning(build)
	}
	self, selfErr := ownBinary()
	rep.Binary = self
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
	getenv := a.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	for _, row := range brand.Mark(brand.ColourWanted(o.stdout, getenv), brand.ASCIILocale(getenv)) {
		fmt.Fprintln(o.stdout, row)
	}
	if selfErr != nil {
		fmt.Fprintf(o.stdout, "[--] ork  path unknown: %v\n", selfErr)
	} else {
		fmt.Fprintf(o.stdout, "[ok] ork  %s\n", self)
	}
	if rep.Warning != "" {
		fmt.Fprintf(o.stdout, "[WARN] daemon  %s\n", rep.Warning)
	}
	if rep.BehindNotice != "" {
		fmt.Fprintf(o.stdout, "[WARN] %s\n", rep.BehindNotice)
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
	accepts := make([]string, len(c.Accepted))
	for i, v := range c.Accepted {
		accepts[i] = fmt.Sprintf("v%d", v)
	}
	accepted := strings.Join(accepts, ", ")
	found := ""
	if c.Found != "" {
		found = fmt.Sprintf(", slicer prints %q", c.Found)
	}
	if c.OK {
		fmt.Fprintf(o.stdout, "[ok] contract  accepts %s%s\n", accepted, found)
	} else {
		fmt.Fprintf(o.stdout, "[FAIL] contract  accepts %s%s: %s\n", accepted, found, c.Error)
	}
	return code
}
