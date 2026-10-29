// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/brand"
	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/roles"
	"github.com/squalor-xyz/ork/internal/slicercli"
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
	// Resolved, Install, Checkout and Build are set for slicer and adj only
	// (see describeInstall).
	Resolved string        `json:"resolved,omitempty"`
	Install  string        `json:"install,omitempty"`
	Checkout *checkoutInfo `json:"checkout,omitempty"`
	Build    *buildInfo    `json:"build,omitempty"`
}

type contractCheck struct {
	Accepted []int  `json:"accepted"`
	Found    string `json:"found,omitempty"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

type projectCheck struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Findings []string `json:"findings"`
}

// DoctorReport is what `ork doctor --json` prints. OK is false when slicer,
// adj or the contract check fails; a vendor CLI that is missing or broken is
// reported but does not fail the check. Project findings are also advisory.
// Binary is the resolved path of the running ork binary, empty when that
// path cannot be read.
type DoctorReport struct {
	OK            bool           `json:"ok"`
	Tools         []toolCheck    `json:"tools"`
	Contract      contractCheck  `json:"contract"`
	Binary        string         `json:"binary"`
	Projects      []projectCheck `json:"projects"`
	RegistryError string         `json:"registry_error,omitempty"`
	DaemonBuildID string         `json:"daemon_build_id,omitempty"`
	BehindNotice  string         `json:"behind_notice,omitempty"`
	Warning       string         `json:"warning,omitempty"`
	Upgrade       []upgradeHint  `json:"upgrade"`
}

// assignmentPrefix recognizes only literal shell assignment names. Values are
// skipped without expanding them or executing any part of the command.
var assignmentPrefix = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*=`)

// checkCommandWord reads the first literal shell word after assignments.
// Quotes and escapes are removed, but no shell expansion is performed.
func checkCommandWord(command string) string {
	for command = strings.TrimLeft(command, " \t\r\n"); command != ""; command = strings.TrimLeft(command, " \t\r\n") {
		if strings.ContainsRune("#;&|()<>", rune(command[0])) {
			return ""
		}
		assignment := assignmentPrefix.MatchString(command)
		var word strings.Builder
		var quote byte
		i := 0
		for ; i < len(command); i++ {
			c := command[i]
			if quote == 0 && strings.ContainsRune(" \t\r\n;&|()<>", rune(c)) {
				break
			}
			if c == '\\' && quote != '\'' && i+1 < len(command) {
				next := command[i+1]
				if quote == 0 || strings.ContainsRune("$`\\\"\n", rune(next)) {
					if next != '\n' {
						word.WriteByte(next)
					}
					i++
					continue
				}
			}
			if c == '\'' || c == '"' {
				if quote == 0 {
					quote = c
					continue
				}
				if quote == c {
					quote = 0
					continue
				}
			}
			word.WriteByte(c)
		}
		if !assignment {
			value := word.String()
			switch value {
			case "!", "{", "}", "case", "do", "done", "elif", "else", "esac", "fi", "for", "if", "in", "then", "until", "while",
				".", ":", "break", "continue", "eval", "exec", "exit", "export", "readonly", "return", "set", "shift", "times", "trap", "unset", "cd", "command", "time":
				return ""
			}
			if strings.ContainsAny(value, "$`") || strings.HasPrefix(value, "~") {
				return ""
			}
			return value
		}
		command = command[i:]
	}
	return ""
}

// shellWord quotes s for a shell only when it needs it.
func shellWord(s string) string {
	plain := s != ""
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-/", r)) {
			plain = false
		}
	}
	if plain {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// addFinding keeps every stored finding on a single text line.
func (p *projectCheck) addFinding(format string, args ...any) {
	p.Findings = append(p.Findings, strings.Join(strings.Fields(fmt.Sprintf(format, args...)), " "))
}

var shellErrorPrefix = regexp.MustCompile(`^.*?sh: (?:-c: line [0-9]+: |[0-9]+: )`)

func checkProject(ctx context.Context, p registry.Project) projectCheck {
	check := projectCheck{Name: p.Name, Path: p.Path, Findings: []string{}}
	info, err := os.Stat(p.Path)
	if os.IsNotExist(err) {
		check.addFinding("path: does not exist")
	} else if err != nil {
		check.addFinding("path: %v", err)
	} else if !info.IsDir() {
		check.addFinding("path: not a directory")
	}
	if p.CheckCommand != "" {
		// -n parses only: even substitutions and redirects must never execute.
		res, err := clirun.Run(ctx, clirun.Call{Bin: "sh", Args: []string{"-n", "-c", p.CheckCommand}, Timeout: versionTimeout})
		if err != nil {
			message := shellErrorPrefix.ReplaceAllString(firstLine(res.Stderr), "")
			if message == "" {
				message = "could not check shell syntax"
			}
			check.addFinding("check_command: %s", message)
		} else if word := checkCommandWord(p.CheckCommand); word != "" {
			bin := word
			if strings.ContainsRune(bin, '/') && !filepath.IsAbs(bin) {
				bin = filepath.Join(p.Path, bin)
			}
			if _, err := exec.LookPath(bin); err != nil {
				detail := "not found on PATH"
				if strings.ContainsRune(word, '/') {
					detail = "not executable"
					if _, statErr := os.Stat(bin); os.IsNotExist(statErr) {
						detail = "not found"
						if !filepath.IsAbs(word) {
							detail = "not found in project directory"
						}
					}
				}
				check.addFinding("check_command: %q %s", word, detail)
			}
		}
	}
	for _, pattern := range []struct{ name, value string }{
		{"worktree_pattern", p.WorktreePattern}, {"branch_pattern", p.BranchPattern},
	} {
		if pattern.value != "" && !strings.Contains(pattern.value, "{id}") && !strings.Contains(pattern.value, "{id_lower}") {
			check.addFinding("%s %q has no {id} or {id_lower} (fix: ork projects set %s --%s PATTERN)",
				pattern.name, pattern.value, shellWord(p.Name), strings.ReplaceAll(pattern.name, "_", "-"))
		}
	}
	return check
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
func checkContract(ctx context.Context, slicer toolCheck, root string) contractCheck {
	cc := contractCheck{Accepted: roles.AcceptedContracts()}
	if !slicer.Present {
		cc.Error = "slicer is not available"
		return cc
	}
	client := slicercli.Client{Bin: slicer.Path, Timeout: versionTimeout}
	line, err := client.Contract(ctx, root)
	cc.Found = line
	if err != nil {
		cc.Error = contractError(err)
		return cc
	}
	if _, err := roles.CheckContractLine(cc.Found); err != nil {
		cc.Error = contractError(err)
		return cc
	}
	if _, err := client.RelayContract(ctx, root); err != nil {
		cc.Error = contractError(err)
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
	all := fs.Bool("upgrade", false, "print upgrade commands for slicer, adj and ork even when none is behind")
	pos, err := parse(fs, args)
	if err != nil {
		return o.usage("doctor: %v", err)
	}
	if len(pos) > 0 {
		return o.usage("doctor takes no arguments, got %q", pos[0])
	}

	t := a.Tools
	rep := DoctorReport{OK: true, Projects: []projectCheck{}, Upgrade: []upgradeHint{}}
	registryPath, registryErr := registry.DefaultPath()
	if registryErr == nil {
		var file *registry.File
		file, registryErr = registry.Load(registryPath)
		if registryErr == nil {
			for _, p := range file.Projects {
				rep.Projects = append(rep.Projects, checkProject(ctx, p))
			}
		}
	}
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
		if c.name == "slicer" || c.name == "adj" {
			describeInstall(ctx, &tc, t.Git)
		}
		rep.Tools = append(rep.Tools, tc)
		if c.required && !tc.OK {
			rep.OK = false
		}
	}
	root, rootErr := os.Getwd()
	if rootErr != nil {
		rep.Contract = contractCheck{Accepted: roles.AcceptedContracts(), Error: rootErr.Error()}
	} else {
		rep.Contract = checkContract(ctx, rep.Tools[0], root)
	}
	if !rep.Contract.OK {
		rep.OK = false
	}
	// Hints are printed, never run, and never change the exit code.
	rep.Upgrade = buildHints(rep, *all)

	if registryErr != nil {
		rep.RegistryError = registryErr.Error()
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
	for _, h := range rep.Upgrade {
		fmt.Fprintf(o.stdout, "[upgrade] %s  %s\n", h.Tool, h.Reason)
		if h.Note != "" {
			fmt.Fprintf(o.stdout, "  note: %s\n", h.Note)
		}
		for _, command := range h.Commands {
			fmt.Fprintf(o.stdout, "  %s\n", command)
		}
	}
	if rep.RegistryError != "" {
		// The error text already begins with "registry"; the label says it once.
		fmt.Fprintf(o.stdout, "[WARN] registry  %s\n", strings.TrimPrefix(rep.RegistryError, "registry "))
	} else if len(rep.Projects) == 0 {
		fmt.Fprintln(o.stdout, "[--] projects  none registered: run ork projects add PATH")
	}
	for _, p := range rep.Projects {
		status := "ok"
		if len(p.Findings) > 0 {
			status = "WARN"
		}
		fmt.Fprintf(o.stdout, "[%s] project %s  %s\n", status, p.Name, p.Path)
		for _, finding := range p.Findings {
			fmt.Fprintf(o.stdout, "  %s\n", finding)
		}
	}
	return code
}

func contractError(err error) string {
	var tool *clirun.ToolError
	if errors.As(err, &tool) {
		return fmt.Sprintf("%s (%s)", tool.Message, tool.Code)
	}
	return err.Error()
}
