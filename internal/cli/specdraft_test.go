// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
)

// ork spec draft PROJECT ID (cli.spec-draft, adj 393; S179) drafts the item's
// spec with spec.draft, prints each section's current text and its new text,
// and asks y or n for each section it can apply. Only a yes calls
// spec.draft-apply, one call for each approved section.

const draftThreeSections = `{"item":"S1","sections":[
{"name":"Why","current":"The old why.","text":"A new why.","command":"slicer edit S1 --section Why --text 'A new why.' --render"},
{"name":"Files","current":"- a.go","text":"- b.go","problems":["b.go is not in the checkout"]},
{"name":"Failing tests","unchanged":true,"current":"- TestA: a."},
{"name":"Implement","current":"1. Old.","text":"1. New.","command":"slicer edit S1 --section Implement --text '1. New.' --render"},
{"name":"Check","current":"- go test","text":"- go test ./...","command":"slicer edit S1 --section Check --text '- go test ./...' --render"}],
"owner_needed":"Question: Which store?"}`

// draftCLI runs ork spec draft with the stub's replies and the owner's answers.
func draftCLI(t *testing.T, draft, answers string, terminal bool, args ...string) (int, string, string, *callLog) {
	t.Helper()
	log := &callLog{reply: func(method string, _ any) (string, error) {
		switch method {
		case daemon.MethodSpecDraft:
			return draft, nil
		case daemon.MethodSpecDraftApply:
			return `{"item":"S1","applied":[]}`, nil
		}
		t.Errorf("unexpected call %s", method)
		return `{}`, nil
	}}
	app := &App{Call: log.call, Input: strings.NewReader(answers), Terminal: func() bool { return terminal }}
	code, out, errs := runCLI(app, append([]string{"spec", "draft", "p", "S1"}, args...)...)
	return code, out, errs, log
}

func TestSpecDraftCLIPrintsCurrentThenNewText(t *testing.T) {
	code, out, errs, log := draftCLI(t, draftThreeSections, "n\nn\nn\n", true)
	if code != ExitOK {
		t.Fatalf("exit %d: %s %s", code, out, errs)
	}
	if len(log.calls) != 1 || log.calls[0] != `spec.draft {"project":"p","item":"S1"}` {
		t.Fatalf("calls %q", log.calls)
	}
	for _, pair := range [][2]string{
		{"The old why.", "A new why."},
		{"1. Old.", "1. New."},
		{"- go test\n", "- go test ./..."},
	} {
		oldAt, newAt := strings.Index(out, pair[0]), strings.Index(out, pair[1])
		if oldAt < 0 || newAt < oldAt {
			t.Errorf("%q must print before %q:\n%s", pair[0], pair[1], out)
		}
	}
	for _, want := range []string{"Why", "Files", "b.go is not in the checkout", "Failing tests", "unchanged", "Which store?", "Apply Why? [y/N]", "Apply Implement? [y/N]", "Apply Check? [y/N]"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// No prompt for a section with a problem or one left unchanged.
	for _, bad := range []string{"Apply Files?", "Apply Failing tests?"} {
		if strings.Contains(out, bad) {
			t.Errorf("output asks %q:\n%s", bad, out)
		}
	}

	// --json prints the draft and asks nothing and applies nothing.
	code, out, _, log = draftCLI(t, draftThreeSections, "", true, "--json")
	var got daemon.SpecDraftResult
	mustJSON(t, out, &got)
	if code != ExitOK || len(got.Sections) != 5 || len(log.calls) != 1 || strings.Contains(out, "Apply ") {
		t.Fatalf("--json: exit %d, %d sections, calls %q:\n%s", code, len(got.Sections), log.calls, out)
	}
}

func TestSpecDraftCLIAppliesOnlyApprovedSections(t *testing.T) {
	code, out, errs, log := draftCLI(t, draftThreeSections, "y\nn\ny\n", true)
	if code != ExitOK {
		t.Fatalf("exit %d: %s %s", code, out, errs)
	}
	want := []string{
		`spec.draft {"project":"p","item":"S1"}`,
		`spec.draft-apply {"project":"p","item":"S1","sections":["Why"]}`,
		`spec.draft-apply {"project":"p","item":"S1","sections":["Check"]}`,
	}
	if strings.Join(log.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(log.calls, "\n"), strings.Join(want, "\n"))
	}
	// Why and Check were approved (y n y over Why, Implement and Check); Implement
	// was answered n.
	for _, line := range []string{"Applied Why", "Applied Check", "Skipped Implement", "2 of 5 sections applied"} {
		if !strings.Contains(out, line) {
			t.Errorf("output lacks %q:\n%s", line, out)
		}
	}
	if strings.Contains(out, "Applied Implement") {
		t.Errorf("an unapproved section was reported applied:\n%s", out)
	}

	// Input that ends is not a yes, and Enter alone is no.
	_, _, _, log = draftCLI(t, draftThreeSections, "\n", true)
	if len(log.calls) != 1 {
		t.Errorf("Enter and end of input must apply nothing: %q", log.calls)
	}
}

func TestSpecDraftCLIRefusesWithoutATerminal(t *testing.T) {
	code, out, errs, log := draftCLI(t, draftThreeSections, "y\ny\ny\n", false)
	if code == ExitOK {
		t.Fatalf("no terminal must fail: %s %s", out, errs)
	}
	if !strings.Contains(errs, "needs a terminal") {
		t.Errorf("stderr must say why: %q", errs)
	}
	for _, call := range log.calls {
		if strings.HasPrefix(call, daemon.MethodSpecDraftApply) {
			t.Errorf("an approval without a terminal: %q", log.calls)
		}
	}
}

func TestSpecDraftCLIWithoutDaemon(t *testing.T) {
	app := &App{Socket: filepath.Join(shortDir(t), "none.sock"), Terminal: func() bool { return true }, Input: strings.NewReader("y\n")}
	code, out, errs := runCLI(app, "spec", "draft", "p", "S1")
	if code != ExitFailure || !strings.Contains(errs, "No daemon connection; action unavailable") || out != "" {
		t.Fatalf("exit %d\nstdout: %q\nstderr: %q", code, out, errs)
	}
}

func TestSpecDraftCLIUsage(t *testing.T) {
	for _, args := range [][]string{{"spec"}, {"spec", "nope"}, {"spec", "draft"}, {"spec", "draft", "p"}, {"spec", "draft", "p", "S1", "extra"}} {
		code, _, errs, log := runWith(t, args)
		if code != ExitUsage || len(log.calls) != 0 {
			t.Errorf("%v: exit %d, calls %q, stderr %q", args, code, log.calls, errs)
		}
	}
}

func runWith(t *testing.T, args []string) (int, string, string, *callLog) {
	t.Helper()
	log := &callLog{}
	app := &App{Call: log.call, Terminal: func() bool { return true }}
	code, out, errs := runCLI(app, args...)
	return code, out, errs, log
}
