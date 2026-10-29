// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"debug/buildinfo"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"runtime/debug"

	"github.com/squalor-xyz/ork/internal/clitest"
)

// upgradeOut is the part of `ork doctor --json` that S178 adds.
type upgradeOut struct {
	OK    bool `json:"ok"`
	Tools []struct {
		Name     string `json:"name"`
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Path     string `json:"path"`
		Resolved string `json:"resolved"`
		Install  string `json:"install"`
		Checkout *struct {
			Dir      string `json:"dir"`
			Describe string `json:"describe"`
			Branch   string `json:"branch"`
			Upstream string `json:"upstream"`
			Behind   int    `json:"behind"`
		} `json:"checkout"`
		Build *struct {
			Module   string `json:"module"`
			Version  string `json:"version"`
			Revision string `json:"revision"`
			Modified bool   `json:"modified"`
		} `json:"build"`
	} `json:"tools"`
	Binary  string `json:"binary"`
	Upgrade []struct {
		Tool     string   `json:"tool"`
		Reason   string   `json:"reason"`
		Commands []string `json:"commands"`
		Note     string   `json:"note"`
	} `json:"upgrade"`
}

// upOpts picks one case of the upgrade tests. Every field has a default.
type upOpts struct {
	contract string // first line of `slicer ai relay`; default relay v10
	slicer   string // checkout, dotvenv-nogit, dotvenv-nested, venv or unknown; default unknown
	behind   string // `rev-list --left-right --count` output; "" means no upstream
	adj      string // build (build info readable) or unreadable; default unreadable
}

// upEnv is a doctor app whose slicer and adj live in a temporary directory.
type upEnv struct {
	app      *App
	fake     *clitest.Fake
	logs     map[string]string
	slicerD  string // the checkout directory, when there is one
	adjPath  string // the resolved path of the fake adj
	adjAsked []string
}

// execScript writes a regular file that runs the fake. A regular file keeps
// filepath.EvalSymlinks from following a link into the test binary.
func execScript(t *testing.T, path, fake string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec '"+fake+"' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func resolvedTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func setupUpgrade(t *testing.T, o upOpts) *upEnv {
	t.Helper()
	if o.contract == "" {
		o.contract = "Contract: relay v10"
	}
	if o.slicer == "" {
		o.slicer = "unknown"
	}
	tmp := resolvedTemp(t)
	env := &upEnv{}
	var routes []clitest.Route
	var slicerReal string
	switch o.slicer {
	case "checkout", "dotvenv-nogit", "dotvenv-nested":
		env.slicerD = filepath.Join(tmp, "slicer")
		slicerReal = filepath.Join(env.slicerD, ".venv", "bin", "slicer")
		g := func(args ...string) []string { return append([]string{"-C", env.slicerD}, args...) }
		if o.slicer != "checkout" {
			// The .venv is a plain virtual environment: git says there is no
			// repository, or the repository is a larger one around the directory.
			top := clitest.Route{Tool: "git", Args: g("rev-parse", "--show-toplevel"), Exit: 128, Stderr: "fatal: not a git repository\n"}
			if o.slicer == "dotvenv-nested" {
				top = clitest.Route{Tool: "git", Args: g("rev-parse", "--show-toplevel"), Stdout: tmp + "\n"}
			}
			routes = append(routes, top)
			if err := os.MkdirAll(filepath.Join(env.slicerD, ".venv"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(env.slicerD, ".venv", "pyvenv.cfg"), []byte("home = /usr/bin\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			routes = append(routes,
				clitest.Route{Tool: "git", Args: g("rev-parse", "--show-toplevel"), Stdout: env.slicerD + "\n"},
				clitest.Route{Tool: "git", Args: g("describe", "--tags", "--always"), Stdout: "v1.15.0-3-g0a1b2c3\n"},
				clitest.Route{Tool: "git", Args: g("rev-parse", "--abbrev-ref", "HEAD"), Stdout: "main\n"},
			)
			if o.behind == "" {
				routes = append(routes, clitest.Route{Tool: "git", Args: g("rev-list", "--left-right", "--count", "HEAD...@{upstream}"), Exit: 128, Stderr: "fatal: no upstream configured for branch 'main'\n"})
			} else {
				routes = append(routes,
					clitest.Route{Tool: "git", Args: g("rev-list", "--left-right", "--count", "HEAD...@{upstream}"), Stdout: o.behind + "\n"},
					clitest.Route{Tool: "git", Args: g("rev-parse", "--abbrev-ref", "@{upstream}"), Stdout: "origin/main\n"},
				)
			}
		}
	case "venv":
		venv := filepath.Join(tmp, "slicer-venv")
		slicerReal = filepath.Join(venv, "bin", "slicer")
		if err := os.MkdirAll(venv, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(venv, "pyvenv.cfg"), []byte("home = /usr/bin\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	default:
		slicerReal = filepath.Join(tmp, "other", "bin", "slicer")
	}
	app, fake, logs := doctorAppWith(t, o.contract, routes...)
	env.app, env.fake, env.logs = app, fake, logs
	execScript(t, slicerReal, fake.Bin("slicer"))
	link := filepath.Join(tmp, "bin", "slicer")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(slicerReal, link); err != nil {
		t.Fatal(err)
	}
	app.Tools.Slicer = link

	env.adjPath = filepath.Join(tmp, "adjbin", "adj")
	execScript(t, env.adjPath, fake.Bin("adj"))
	app.Tools.Adj = env.adjPath
	old := readBuildInfo
	t.Cleanup(func() { readBuildInfo = old })
	readBuildInfo = func(name string) (*buildinfo.BuildInfo, error) {
		env.adjAsked = append(env.adjAsked, name)
		if o.adj != "build" {
			return nil, errors.New("not a Go binary")
		}
		return &debug.BuildInfo{
			Main: debug.Module{Path: "github.com/squalor-xyz/adjudicate", Version: "v0.1.0+dirty"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "656a7c1deadbeef"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, nil
	}
	return env
}

func (e *upEnv) json(t *testing.T, args ...string) (int, upgradeOut, string) {
	t.Helper()
	code, stdout, stderr := run(t, e.app, append([]string{"doctor", "--json"}, args...)...)
	var d upgradeOut
	mustJSON(t, stdout, &d)
	if stderr != "" {
		t.Errorf("stderr: %s", stderr)
	}
	return code, d, stdout
}

// gitVerbs are the only git subcommands doctor may start.
var gitVerbs = []string{"rev-parse", "describe", "rev-list"}

func gitCalls(f *clitest.Fake) (verbs []string) {
	for _, c := range f.CallsTo("git") {
		if len(c.Args) >= 3 && c.Args[0] == "-C" {
			verbs = append(verbs, c.Args[2])
		} else {
			verbs = append(verbs, strings.Join(c.Args, " "))
		}
	}
	return verbs
}

func TestDoctorResolvesToolPathsAndInstallKind(t *testing.T) {
	for _, tc := range []struct {
		name, slicer, install string
		git                   bool
	}{
		{"checkout", "checkout", "checkout", true},
		{"venv with a .venv name but no git root", "dotvenv-nogit", "venv", true},
		{"venv inside a larger repository", "dotvenv-nested", "venv", true},
		{"venv outside a checkout", "venv", "venv", false},
		{"anything else", "unknown", "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupUpgrade(t, upOpts{slicer: tc.slicer})
			code, d, raw := env.json(t)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, raw)
			}
			s := d.Tools[0]
			if s.Name != "slicer" || s.Install != tc.install {
				t.Fatalf("slicer: %+v", s)
			}
			if !strings.HasSuffix(s.Path, "/bin/slicer") || s.Resolved == "" || s.Resolved == s.Path {
				t.Errorf("path %q resolved %q: the symlink was not followed", s.Path, s.Resolved)
			}
			if tc.install == "checkout" {
				if s.Checkout == nil || s.Checkout.Dir != env.slicerD || s.Resolved != filepath.Join(env.slicerD, ".venv", "bin", "slicer") {
					t.Errorf("checkout: %+v resolved %q", s.Checkout, s.Resolved)
				}
			} else if s.Checkout != nil {
				t.Errorf("checkout reported for a %s install: %+v", tc.install, s.Checkout)
			}
			if got := len(gitCalls(env.fake)) > 0; got != tc.git {
				t.Errorf("git called = %v, want %v: %v", got, tc.git, gitCalls(env.fake))
			}
			// Vendor CLIs and adj's slicer-only fields stay out of it.
			for _, tool := range d.Tools[2:] {
				if tool.Install != "" || tool.Resolved != "" || tool.Checkout != nil || tool.Build != nil {
					t.Errorf("%s: %+v", tool.Name, tool)
				}
			}
		})
	}
}

func TestDoctorReadsCheckoutStateWithLocalGitOnly(t *testing.T) {
	t.Run("with an upstream", func(t *testing.T) {
		env := setupUpgrade(t, upOpts{slicer: "checkout", behind: "1\t3"})
		code, d, raw := env.json(t)
		if code != 0 {
			t.Fatalf("exit %d: %s", code, raw)
		}
		c := d.Tools[0].Checkout
		if c == nil || c.Dir != env.slicerD || c.Describe != "v1.15.0-3-g0a1b2c3" || c.Branch != "main" || c.Upstream != "origin/main" || c.Behind != 3 {
			t.Fatalf("checkout: %+v", c)
		}
		assertReadOnlyGit(t, env.fake)
	})
	t.Run("without an upstream", func(t *testing.T) {
		env := setupUpgrade(t, upOpts{slicer: "checkout"})
		code, d, raw := env.json(t)
		if code != 0 {
			t.Fatalf("exit %d: %s", code, raw)
		}
		c := d.Tools[0].Checkout
		if c == nil || c.Behind != -1 || c.Upstream != "" || c.Describe != "v1.15.0-3-g0a1b2c3" || c.Branch != "main" {
			t.Fatalf("checkout: %+v", c)
		}
		assertReadOnlyGit(t, env.fake)
	})
}

func assertReadOnlyGit(t *testing.T, f *clitest.Fake) {
	t.Helper()
	calls := f.CallsTo("git")
	if len(calls) == 0 {
		t.Fatal("git was never called")
	}
	for _, c := range calls {
		if len(c.Args) < 3 || c.Args[0] != "-C" || !slices.Contains(gitVerbs, c.Args[2]) {
			t.Errorf("git call outside the read-only set: %v", c.Args)
		}
		for _, a := range c.Args {
			if slices.Contains([]string{"fetch", "pull", "clone", "checkout", "merge"}, a) {
				t.Errorf("git call changes or fetches: %v", c.Args)
			}
		}
	}
}

func TestDoctorReadsAGoBinarysBuildInfo(t *testing.T) {
	t.Run("readable", func(t *testing.T) {
		env := setupUpgrade(t, upOpts{adj: "build"})
		code, d, raw := env.json(t)
		if code != 0 {
			t.Fatalf("exit %d: %s", code, raw)
		}
		a := d.Tools[1]
		if a.Name != "adj" || a.Install != "go-binary" || a.Build == nil {
			t.Fatalf("adj: %+v", a)
		}
		b := a.Build
		if b.Module != "github.com/squalor-xyz/adjudicate" || b.Version != "v0.1.0+dirty" || b.Revision != "656a7c1deadbeef" || !b.Modified {
			t.Errorf("build: %+v", b)
		}
		if a.Resolved != env.adjPath || !slices.Equal(env.adjAsked, []string{env.adjPath}) {
			t.Errorf("resolved %q, build info asked of %v", a.Resolved, env.adjAsked)
		}
		// slicer is never read as a Go binary.
		if len(env.adjAsked) != 1 {
			t.Errorf("readBuildInfo calls: %v", env.adjAsked)
		}
	})
	t.Run("unreadable", func(t *testing.T) {
		env := setupUpgrade(t, upOpts{adj: "unreadable"})
		code, d, raw := env.json(t)
		a := d.Tools[1]
		if code != 0 || !d.OK || !a.OK || a.Error != "" || a.Install != "unknown" || a.Build != nil {
			t.Fatalf("exit %d adj %+v\n%s", code, a, raw)
		}
	})
}

func TestDoctorHintsWhenSlicerContractIsOlder(t *testing.T) {
	env := setupUpgrade(t, upOpts{slicer: "checkout", contract: "Contract: relay v8", behind: "0\t0"})
	code, stdout, _ := run(t, env.app, "doctor")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (the contract check failed)\n%s", code, stdout)
	}
	d := env.slicerD
	for _, want := range []string{
		"[upgrade] slicer  ",
		"\n  git -C " + shellWord(d) + " pull --ff-only\n",
		"\n  " + shellWord(d+"/.venv/bin/pip") + " install -e " + shellWord(d) + "\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "[upgrade] ork") || strings.Contains(stdout, "[upgrade] adj") {
		t.Errorf("unexpected hint:\n%s", stdout)
	}
	if strings.Contains(stdout, "$ ") {
		t.Errorf("a command has a prompt:\n%s", stdout)
	}
	// The hint comes after the contract line.
	if strings.Index(stdout, "[upgrade] slicer") < strings.Index(stdout, "[FAIL] contract") {
		t.Errorf("hint before the contract line:\n%s", stdout)
	}
}

func TestDoctorQuotesPathsInHints(t *testing.T) {
	// A checkout directory with a space and a quote must be shell-quoted.
	tmp := resolvedTemp(t)
	d := filepath.Join(tmp, "my slicer's")
	real := filepath.Join(d, ".venv", "bin", "slicer")
	g := func(args ...string) []string { return append([]string{"-C", d}, args...) }
	app, fake, _ := doctorAppWith(t, "Contract: relay v8",
		clitest.Route{Tool: "git", Args: g("rev-parse", "--show-toplevel"), Stdout: d + "\n"},
		clitest.Route{Tool: "git", Args: g("describe", "--tags", "--always"), Stdout: "v1\n"},
		clitest.Route{Tool: "git", Args: g("rev-parse", "--abbrev-ref", "HEAD"), Stdout: "main\n"},
		clitest.Route{Tool: "git", Args: g("rev-list", "--left-right", "--count", "HEAD...@{upstream}"), Exit: 128},
	)
	execScript(t, real, fake.Bin("slicer"))
	app.Tools.Slicer = real
	_, stdout, _ := run(t, app, "doctor")
	for _, want := range []string{
		"\n  git -C '" + d[:strings.Index(d, "'")] + `'"'"'s' pull --ff-only` + "\n",
		" install -e '",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "git -C "+d+" ") {
		t.Errorf("unquoted path:\n%s", stdout)
	}
}

func TestDoctorHintsWhenSlicerContractIsNewer(t *testing.T) {
	env := setupUpgrade(t, upOpts{contract: "Contract: relay v11"})
	code, stdout, _ := run(t, env.app, "doctor")
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, stdout)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[upgrade] ork  ",
		"\n  go -C ORK_CHECKOUT build -o " + shellWord(bin) + " ./cmd/ork\n",
		"\n  " + shellWord(bin) + " daemon restart\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "[upgrade] slicer") || strings.Contains(stdout, "[upgrade] adj") {
		t.Errorf("unexpected hint:\n%s", stdout)
	}
}

func TestDoctorHintsWhenTheCheckoutIsBehindItsUpstream(t *testing.T) {
	env := setupUpgrade(t, upOpts{slicer: "checkout", behind: "0\t3"})
	code, stdout, _ := run(t, env.app, "doctor")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "[upgrade] slicer  3 commits behind origin/main as of its last fetch\n") {
		t.Errorf("no reason line:\n%s", stdout)
	}
	if !strings.Contains(stdout, "\n  git -C "+shellWord(env.slicerD)+" pull --ff-only\n") {
		t.Errorf("no pull line:\n%s", stdout)
	}
	if strings.Contains(stdout, "[upgrade] ork") || strings.Contains(stdout, "[upgrade] adj") {
		t.Errorf("unexpected hint:\n%s", stdout)
	}
	// One commit says "commit".
	env = setupUpgrade(t, upOpts{slicer: "checkout", behind: "0\t1"})
	_, stdout, _ = run(t, env.app, "doctor")
	if !strings.Contains(stdout, "1 commit behind origin/main as of its last fetch\n") {
		t.Errorf("singular reason:\n%s", stdout)
	}
}

func TestDoctorHintsForAVenvSlicer(t *testing.T) {
	env := setupUpgrade(t, upOpts{slicer: "venv", contract: "Contract: relay v8"})
	_, d, _ := env.json(t)
	if len(d.Upgrade) != 1 || d.Upgrade[0].Tool != "slicer" {
		t.Fatalf("upgrade: %+v", d.Upgrade)
	}
	venv := filepath.Dir(filepath.Dir(d.Tools[0].Resolved))
	want := []string{shellWord(venv+"/bin/pip") + " install --upgrade squalor-slicer"}
	if !slices.Equal(d.Upgrade[0].Commands, want) {
		t.Errorf("commands %q, want %q", d.Upgrade[0].Commands, want)
	}
}

func TestDoctorPrintsNoHintWhenNothingIsBehind(t *testing.T) {
	// A checkout that is level with its upstream, and a current contract.
	env := setupUpgrade(t, upOpts{slicer: "checkout", behind: "2\t0"})
	code, stdout, _ := run(t, env.app, "doctor")
	if code != 0 || strings.Contains(stdout, "[upgrade]") {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	env = setupUpgrade(t, upOpts{slicer: "checkout", behind: "2\t0"})
	code, jsonOut, _ := run(t, env.app, "doctor", "--json")
	if code != 0 || !strings.Contains(jsonOut, "\"upgrade\": []") {
		t.Fatalf("exit %d, want \"upgrade\": []\n%s", code, jsonOut)
	}
}

func TestDoctorUpgradeFlagPrintsEveryTool(t *testing.T) {
	t.Run("recognized sources", func(t *testing.T) {
		env := setupUpgrade(t, upOpts{slicer: "checkout", behind: "0\t0", adj: "build"})
		code, stdout, _ := run(t, env.app, "doctor", "--upgrade")
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, stdout)
		}
		for _, want := range []string{
			"[upgrade] slicer  ", "[upgrade] adj  ", "[upgrade] ork  ",
			"\n  git -C ADJ_CHECKOUT pull --ff-only\n",
			"\n  go -C ADJ_CHECKOUT build -o " + shellWord(env.adjPath) + " ./cmd/adj\n",
			"\n  go -C ORK_CHECKOUT build -o ",
			" daemon restart\n",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("output lacks %q:\n%s", want, stdout)
			}
		}
		if strings.Index(stdout, "[upgrade] slicer") > strings.Index(stdout, "[upgrade] adj") || strings.Index(stdout, "[upgrade] adj") > strings.Index(stdout, "[upgrade] ork") {
			t.Errorf("hints out of order:\n%s", stdout)
		}
	})
	t.Run("unknown source", func(t *testing.T) {
		env := setupUpgrade(t, upOpts{slicer: "unknown", adj: "unreadable"})
		code, d, raw := env.json(t, "--upgrade")
		if code != 0 || len(d.Upgrade) != 3 {
			t.Fatalf("exit %d\n%s", code, raw)
		}
		for i, tool := range []string{"slicer", "adj", "ork"} {
			if d.Upgrade[i].Tool != tool || d.Upgrade[i].Reason == "" {
				t.Errorf("hint %d: %+v", i, d.Upgrade[i])
			}
		}
		for i, readme := range []string{"slicer/README.md#install", "adjudicate/README.md#install"} {
			h := d.Upgrade[i]
			if len(h.Commands) != 0 || !strings.Contains(h.Note, readme) {
				t.Errorf("%s: want no commands and a note naming %s: %+v", h.Tool, readme, h)
			}
		}
		_, stdout, _ := run(t, env.app, "doctor", "--upgrade")
		if !strings.Contains(stdout, "slicer/README.md#install") || !strings.Contains(stdout, "adjudicate/README.md#install") {
			t.Errorf("text lacks the README notes:\n%s", stdout)
		}
	})
}

func TestDoctorHintsForAMissingTool(t *testing.T) {
	env := setupUpgrade(t, upOpts{})
	env.app.Tools.Adj = filepath.Join(resolvedTemp(t), "no-such-adj")
	env.app.Tools.Slicer = filepath.Join(resolvedTemp(t), "no-such-slicer")
	code, d, _ := env.json(t)
	if code != 1 || len(d.Upgrade) != 2 {
		t.Fatalf("exit %d upgrade %+v", code, d.Upgrade)
	}
	for i, tc := range []struct{ tool, readme string }{{"slicer", "slicer/README.md#install"}, {"adj", "adjudicate/README.md#install"}} {
		h := d.Upgrade[i]
		if h.Tool != tc.tool || len(h.Commands) != 0 || h.Note != "not installed: read "+tc.readme || strings.Contains(h.Note, "not recognized") {
			t.Errorf("%s hint: %+v", tc.tool, h)
		}
	}
}

// A present tool whose source is not recognized keeps the "not recognized" note.
func TestDoctorNotesForAnUnrecognizedSource(t *testing.T) {
	env := setupUpgrade(t, upOpts{})
	_, d, _ := env.json(t, "--upgrade")
	for i, readme := range []string{"slicer/README.md#install", "adjudicate/README.md#install"} {
		if want := "install source not recognized: read " + readme; d.Upgrade[i].Note != want {
			t.Errorf("%s note %q, want %q", d.Upgrade[i].Tool, d.Upgrade[i].Note, want)
		}
	}
}

// Every note prints as "  note: TEXT" and must fit in 80 columns. Commands may be longer.
func TestDoctorNoteLinesFitEightyColumns(t *testing.T) {
	check := func(t *testing.T, h upgradeHint) {
		t.Helper()
		if line := "  note: " + h.Note; h.Note != "" && len(line) > 80 {
			t.Errorf("%s note line is %d columns: %q", h.Tool, len(line), line)
		}
	}
	t.Run("printed by doctor", func(t *testing.T) {
		for _, o := range []upOpts{
			{slicer: "checkout", behind: "0\t0", adj: "build"},
			{slicer: "venv", adj: "build"},
			{slicer: "unknown", adj: "unreadable"},
		} {
			env := setupUpgrade(t, o)
			_, stdout, _ := run(t, env.app, "doctor", "--upgrade")
			notes := 0
			for _, line := range strings.Split(stdout, "\n") {
				if strings.HasPrefix(line, "  note: ") {
					notes++
					if len(line) > 80 {
						t.Errorf("%d columns: %q", len(line), line)
					}
				}
			}
			if notes == 0 {
				t.Errorf("no note printed:\n%s", stdout)
			}
		}
	})
	t.Run("hints built directly", func(t *testing.T) {
		check(t, orkHint("", "r"))
		check(t, orkHint("/x/ork", "r"))
		check(t, adjHint(toolCheck{Name: "adj", Present: true, Install: installGoBinary, Resolved: "/x/adj"}, "r"))
		check(t, adjHint(toolCheck{Name: "adj", Present: true, Install: installUnknown}, "r"))
		check(t, adjHint(toolCheck{Name: "adj"}, "r"))
		check(t, slicerHint(toolCheck{Name: "slicer"}, "r"))
		check(t, slicerHint(toolCheck{Name: "slicer", Present: true, Install: installUnknown}, "r"))
	})
	t.Run("adj note names the build section", func(t *testing.T) {
		h := adjHint(toolCheck{Name: "adj", Present: true, Install: installGoBinary, Resolved: "/x/adj"}, "r")
		if !strings.Contains(h.Note, "adjudicate/README.md#build-from-source") || !strings.Contains(h.Note, "ADJ_CHECKOUT") {
			t.Errorf("note: %q", h.Note)
		}
	})
}

func TestUsageTextNamesTheUpgradeFlagAndFitsEightyColumns(t *testing.T) {
	if !strings.Contains(usageText, "(slicer, adj and ork with --upgrade)") {
		t.Errorf("usage lacks the --upgrade wording:\n%s", usageText)
	}
	for _, line := range strings.Split(usageText, "\n") {
		if len(line) > 80 {
			t.Errorf("usage line of %d columns: %q", len(line), line)
		}
	}
}

func TestDoctorNeverRunsAnUpgradeCommand(t *testing.T) {
	for _, o := range []struct {
		name  string
		opts  upOpts
		args  []string
		check string
	}{
		{"older contract", upOpts{slicer: "checkout", contract: "Contract: relay v8", behind: "0\t0"}, nil, ""},
		{"newer contract", upOpts{contract: "Contract: relay v11"}, nil, ""},
		{"behind", upOpts{slicer: "checkout", behind: "0\t3"}, nil, ""},
		{"nothing behind", upOpts{slicer: "checkout", behind: "0\t0"}, nil, ""},
		{"upgrade flag", upOpts{slicer: "checkout", behind: "0\t0", adj: "build"}, []string{"--upgrade"}, ""},
		{"upgrade flag, venv", upOpts{slicer: "venv", adj: "build"}, []string{"--upgrade"}, ""},
		{"upgrade flag, unknown", upOpts{}, []string{"--upgrade"}, ""},
	} {
		t.Run(o.name, func(t *testing.T) {
			env := setupUpgrade(t, o.opts)
			run(t, env.app, append([]string{"doctor"}, o.args...)...)
			for _, c := range env.fake.CallsTo("slicer") {
				if len(c.Args) == 0 || (c.Args[0] != "--version" && !(len(c.Args) >= 2 && c.Args[0] == "ai" && c.Args[1] == "relay")) {
					t.Errorf("slicer call: %v", c.Args)
				}
			}
			for _, c := range env.fake.CallsTo("adj") {
				if fmt.Sprint(c.Args) != "[--version]" {
					t.Errorf("adj call: %v", c.Args)
				}
			}
			assertGitReadOnlyOrNone(t, env.fake)
			for name, log := range env.logs {
				if got := readLog(t, log); len(got) != 1 || got[0] != "--version" {
					t.Errorf("%s was called with %q", name, got)
				}
			}
			// Only the fakes of this test exist, so a started pip, go, curl,
			// install or cp could not have run: none is on any path doctor uses.
			for _, c := range env.fake.Calls() {
				if !slices.Contains([]string{"slicer", "adj", "git"}, c.Tool) {
					t.Errorf("unexpected tool: %v", c)
				}
			}
		})
	}
}

func assertGitReadOnlyOrNone(t *testing.T, f *clitest.Fake) {
	t.Helper()
	for _, c := range f.CallsTo("git") {
		if len(c.Args) < 3 || c.Args[0] != "-C" || !slices.Contains(gitVerbs, c.Args[2]) {
			t.Errorf("git call outside the read-only set: %v", c.Args)
		}
	}
}

func TestDoctorJSONUpgradeShape(t *testing.T) {
	env := setupUpgrade(t, upOpts{slicer: "checkout", behind: "0\t3", adj: "build"})
	_, stdout, _ := run(t, env.app, "doctor", "--json", "--upgrade")
	var raw map[string]any
	mustJSON(t, stdout, &raw)
	for _, key := range []string{"ok", "tools", "contract", "binary", "projects", "upgrade"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("report lacks %q", key)
		}
	}
	hints, ok := raw["upgrade"].([]any)
	if !ok || len(hints) != 3 {
		t.Fatalf("upgrade: %v", raw["upgrade"])
	}
	for _, h := range hints {
		m := h.(map[string]any)
		for _, key := range []string{"tool", "reason", "commands"} {
			if _, ok := m[key]; !ok {
				t.Errorf("hint lacks %q: %v", key, m)
			}
		}
		if cmds, ok := m["commands"].([]any); !ok || len(cmds) == 0 {
			t.Errorf("commands: %v", m["commands"])
		}
	}
	// Existing tool fields keep their names.
	tool := raw["tools"].([]any)[0].(map[string]any)
	for _, key := range []string{"name", "required", "present", "path", "version", "ok"} {
		if _, ok := tool[key]; !ok {
			t.Errorf("tool lacks %q: %v", key, tool)
		}
	}
}

func TestDoctorUpgradeFlagTakesNoValue(t *testing.T) {
	env := setupUpgrade(t, upOpts{})
	code, _, stderr := run(t, env.app, "doctor", "--upgrade", "extra")
	if code != 2 || !strings.Contains(stderr, "takes no arguments") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}
