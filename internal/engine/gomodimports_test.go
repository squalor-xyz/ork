// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

const goModImportsGoMod = `module example.com/secret/mod

go 1.27.2

toolchain go1.27.2

require (
	charm.land/bubbletea/v2 v2.0.0
	charm.land/lipgloss/v2 v2.0.1 // direct
	github.com/mattn/go-isatty v0.0.20 // indirect
	golang.org/x/sys v0.30.0 // indirect
)

require github.com/one/liner v1.2.3

require github.com/one/liner v1.2.3

replace (
	github.com/mattn/go-isatty => ../replacement-target
	github.com/other/replaced v1.0.0 => github.com/fork/replaced v1.0.1
)

exclude github.com/excluded/mod v0.0.1
`

// goModImportsHarness builds a reviewer-bearing harness whose worktree exists on
// disk, then lets setup write files under it before the run starts.
func goModImportsHarness(t *testing.T, setup func(W string)) *harness {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return append(baseOverlayRoutes(W, "", ""), ucsWorktree(P, W)...)
		}
	})
	if err := os.MkdirAll(h.W, 0o755); err != nil {
		t.Fatal(err)
	}
	setup(h.W)
	return h
}

// goModImportsBody returns the body of the brief's Go module imports section.
// The reviewer prompt names the heading too, so cut at the section's own blank
// line.
func goModImportsBody(brief string) (string, bool) {
	_, body, found := strings.Cut(brief, "## Go module imports\n\n")
	if !found {
		return "", false
	}
	body, _, _ = strings.Cut(body, "\n## ")
	return body, true
}

func writeGoMod(t *testing.T, W, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(W, "go.mod"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGoModuleImportsSectionListsRequirePaths(t *testing.T) {
	h := goModImportsHarness(t, func(W string) { writeGoMod(t, W, goModImportsGoMod) })
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	body, found := goModImportsBody(baseOverlayReviewer(t, h).Brief)
	if !found {
		t.Fatal("reviewer brief lacks the Go module imports section")
	}
	var entries []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- ") {
			entries = append(entries, line)
		}
	}
	want := []string{
		"- `charm.land/bubbletea/v2`",
		"- `charm.land/lipgloss/v2`",
		"- `github.com/mattn/go-isatty`",
		"- `golang.org/x/sys`",
		"- `github.com/one/liner`",
	}
	if strings.Join(entries, "\n") != strings.Join(want, "\n") {
		t.Errorf("entries = %q, want %q", entries, want)
	}
	for _, banned := range []string{"v2.0.0", "v0.20", "replacement-target", "example.com/secret/mod", "=>", "github.com/other/replaced", "github.com/fork/replaced", "github.com/excluded/mod"} {
		if strings.Contains(body, banned) {
			t.Errorf("section names %q:\n%s", banned, body)
		}
	}
	if !strings.Contains(body, "Use these exact paths in a probe test's `import` lines.") {
		t.Errorf("section does not tell the reviewer to use these exact paths in import lines:\n%s", body)
	}
}

func TestGoModuleImportsNoSectionWithoutGoMod(t *testing.T) {
	for name, setup := range map[string]func(W string){
		"no go.mod":         func(string) {},
		"no require line":   func(W string) { writeGoMod(t, W, "module example.com/m\n\ngo 1.27.2\n\nreplace a.b/c => ../c\n") },
		"empty require":     func(W string) { writeGoMod(t, W, "module example.com/m\n\nrequire (\n)\n") },
		"commented require": func(W string) { writeGoMod(t, W, "module example.com/m\n\nrequire (\n\t// nothing here\n)\n") },
	} {
		t.Run(name, func(t *testing.T) {
			h := goModImportsHarness(t, setup)
			requireState(t, h.start(), runrec.StateAwaitingApproval)
			if body, found := goModImportsBody(baseOverlayReviewer(t, h).Brief); found {
				t.Errorf("reviewer brief holds a Go module imports section:\n%s", body)
			}
		})
	}
}

func TestGoModuleImportsUnreadableGoModIsAdvisory(t *testing.T) {
	h := goModImportsHarness(t, func(W string) {
		if err := os.Mkdir(filepath.Join(W, "go.mod"), 0o755); err != nil {
			t.Fatal(err)
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	body, found := goModImportsBody(baseOverlayReviewer(t, h).Brief)
	if !found {
		t.Fatal("reviewer brief lacks the Go module imports section")
	}
	for _, want := range []string{"could not be read", "Read `go.mod` yourself"} {
		if !strings.Contains(body, want) {
			t.Errorf("section lacks %q:\n%s", want, body)
		}
	}
}

func TestGoModuleImportsOnlyForTheReviewer(t *testing.T) {
	h := architectHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return append(baseOverlayRoutes(W, "", ""), ucsWorktree(P, W)...)
		}
	})
	if err := os.MkdirAll(h.W, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoMod(t, h.W, goModImportsGoMod)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	seen := map[string]bool{}
	for _, req := range h.runner.requests() {
		seen[req.Role] = true
		_, has := goModImportsBody(req.Brief)
		if want := req.Role == RoleReviewer; has != want {
			t.Errorf("%s brief holds a Go module imports section = %v, want %v", req.Role, has, want)
		}
	}
	for _, role := range []string{RoleReviewer, RoleArchitect, RoleGate} {
		if !seen[role] {
			t.Fatalf("role %s did not run: %v", role, h.runner.roles())
		}
	}
}
