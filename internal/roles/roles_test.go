// SPDX-License-Identifier: GPL-3.0-or-later

package roles

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// knownPlaceholders are the only {{name}} tokens a prompt may hold. The
// engine fills them (S04); roles.go lists them in its top comment.
var knownPlaceholders = []string{"report_path", "slice_id", "temp_dir", "worktree"}

// has reports whether p holds want, ignoring how the prompt wraps its lines.
func has(p, want string) bool {
	return strings.Contains(strings.Join(strings.Fields(p), " "), want)
}

func prompts(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	if len(Names()) == 0 {
		t.Fatal("Names() is empty, so no prompt would be checked")
	}
	for _, name := range Names() {
		p, err := Prompt(name)
		if err != nil {
			t.Fatalf("Prompt(%q): %v", name, err)
		}
		out[name] = p
	}
	return out
}

// recordedVersion reads the version from a recorded first line of a slicer
// contract command ("Contract: relay v7").
func recordedVersion(t *testing.T, file string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "roles", file))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^Contract: relay v(\d+)\n$`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatalf("%s: first line %q is not \"Contract: relay vN\"", file, data)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNames(t *testing.T) {
	want := []string{"implementer", "reviewer", "gate", "architect"}
	if got := Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

// Names hands out a copy, so a caller cannot reorder the package's list.
func TestNamesIsACopy(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("Names() is empty")
	}
	names[0] = "changed"
	if got := Names()[0]; got != "implementer" {
		t.Fatalf("Names()[0] = %q after a caller changed its copy", got)
	}
}

func TestPromptUnknownName(t *testing.T) {
	for _, name := range []string{"orchestrator", "", "../roles", "implementer.md"} {
		p, err := Prompt(name)
		if err == nil {
			t.Fatalf("Prompt(%q) returned no error", name)
		}
		if p != "" {
			t.Errorf("Prompt(%q) returned text with its error", name)
		}
		if !strings.HasPrefix(err.Error(), "unknown role ") {
			t.Errorf("Prompt(%q) error %q does not start with \"unknown role \"", name, err)
		}
		if !strings.Contains(err.Error(), strconv.Quote(name)) {
			t.Errorf("Prompt(%q) error %q does not name it", name, err)
		}
	}
}

// The embedded prompts are exactly the four named roles, one file each.
func TestEmbeddedFilesMatchNames(t *testing.T) {
	entries, err := promptFS.ReadDir("prompts")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, strings.TrimSuffix(e.Name(), ".md"))
	}
	want := append([]string(nil), Names()...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("embedded prompt files %v, want %v", got, want)
	}
}

func TestContractVersion(t *testing.T) {
	if ContractVersion != 7 {
		t.Fatalf("ContractVersion = %d, want 7", ContractVersion)
	}
	for _, f := range []string{"relay-first-line.txt", "relay-orchestrator-first-line.txt"} {
		if got := recordedVersion(t, f); got != ContractVersion {
			t.Errorf("%s records v%d, ContractVersion is %d", f, got, ContractVersion)
		}
	}
}

func TestFirstLineNamesRoleAndContract(t *testing.T) {
	recorded := recordedVersion(t, "relay-first-line.txt")
	for name, p := range prompts(t) {
		first, _, _ := strings.Cut(p, "\n")
		want := fmt.Sprintf("Role: %s. Contract: relay v%d", name, recorded)
		if first != want {
			t.Errorf("%s: first line %q, want %q", name, first, want)
		}
	}
}

func TestNoClaudeCodeOnlyMechanics(t *testing.T) {
	// Bash is a tool name of one harness; the prompts say "the shell".
	re := regexp.MustCompile(`(?i)Agent tool|SendMessage|subagent|TodoWrite|\bBash\b`)
	for name, p := range prompts(t) {
		if m := re.FindString(p); m != "" {
			t.Errorf("%s mentions %q", name, m)
		}
	}
}

func TestPlaceholders(t *testing.T) {
	known := map[string]bool{}
	for _, k := range knownPlaceholders {
		known[k] = true
	}
	re := regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	for name, p := range prompts(t) {
		for _, want := range []string{"{{report_path}}", "{{temp_dir}}"} {
			if !has(p, want) {
				t.Errorf("%s does not name %s", name, want)
			}
		}
		for _, m := range re.FindAllStringSubmatch(p, -1) {
			if !known[m[1]] {
				t.Errorf("%s holds unknown placeholder %s", name, m[0])
			}
		}
		if strings.Count(p, "{{") != strings.Count(p, "}}") {
			t.Errorf("%s has unbalanced placeholder braces", name)
		}
	}
}

// roles.go lists every placeholder in the comment at its top.
func TestPlaceholdersListedInRolesGo(t *testing.T) {
	src, err := os.ReadFile("roles.go")
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(src), "package roles")
	for _, k := range knownPlaceholders {
		if !strings.Contains(head, "{{"+k+"}}") {
			t.Errorf("roles.go top comment does not list {{%s}}", k)
		}
	}
}

// Gate mode exists on the gate, and on the reviewer and architect, which can
// give a verdict when the brief asks for a gate in one pass.
func TestVerdictFirstLine(t *testing.T) {
	re := regexp.MustCompile("(?s)first line[^.]*exactly\\s+`VERDICT: PASS`\\s+or\\s+`VERDICT: FAIL`")
	ps := prompts(t)
	for _, name := range []string{"gate", "reviewer", "architect"} {
		if !re.MatchString(ps[name]) {
			t.Errorf("%s does not say its first line is exactly `VERDICT: PASS` or `VERDICT: FAIL`", name)
		}
	}
}

func TestGateCriteria(t *testing.T) {
	g := prompts(t)["gate"]
	for _, want := range []string{
		"earlier blockers",
		"proving test",
		"medium severity",
		"tests green",
		"docs",
		"Non-blocking",
	} {
		if !has(g, want) {
			t.Errorf("gate does not mention %q", want)
		}
	}
}

func TestReadOnlyRoles(t *testing.T) {
	ps := prompts(t)
	for _, name := range []string{"reviewer", "gate", "architect"} {
		p := strings.ToLower(ps[name])
		for _, want := range []string{"you are read-only", "must not change the tree", "no git writes", "no tracking writes"} {
			if !has(p, want) {
				t.Errorf("%s does not say %q", name, want)
			}
		}
	}
}

// The implementer writes code, so no prompt of it claims to be read-only.
func TestImplementerIsNotReadOnly(t *testing.T) {
	if p := prompts(t)["implementer"]; strings.Contains(strings.ToLower(p), "you are read-only") {
		t.Error("implementer prompt says it is read-only")
	}
}

func TestImplementerSteps(t *testing.T) {
	p := prompts(t)["implementer"]
	// Handoff, like done, runs only after the gate's PASS (contract v7).
	if has(p, "run it at the end of the round") {
		t.Error("implementer runs slicer handoff before review")
	}
	for _, want := range []string{
		"slicer start {{slice_id}} --render --strict",
		"including **Not in this slice**",
		"re-read **Not in this slice**",
		"watch each fail",
		"Mutation-check every new assertion",
		"slicer config implement_finish",
		"run only after the gate's `VERDICT: PASS`",
		"Run no `slicer note`, `slicer handoff` or `slicer done` before review",
		"`slicer note {{slice_id}} --text \"Describe the verified outcome\" --render`",
		"`slicer done {{slice_id}} --note \"Describe the verified outcome\" --render --check`",
		"`slicer handoff {{slice_id}} --render --check`",
	} {
		if !has(p, want) {
			t.Errorf("implementer does not mention %q", want)
		}
	}
}

// Rules every role carries: stay in the worktree, tracking through slicer
// only, long commands in the foreground, never commit and no credentials.
func TestSharedRules(t *testing.T) {
	for name, p := range prompts(t) {
		for _, want := range []string{
			"{{worktree}}",
			"Never change directory",
			"Never open, search, parse or edit tracking files",
			"foreground",
			"Never commit",
			"Never read vendor credential or login files",
			"Do not run `adj ask` yourself; ork files the question from your report.",
		} {
			if !has(p, want) {
				t.Errorf("%s does not say %q", name, want)
			}
		}
	}
}

// Every role ends with a report whose shape ork validates: the gate's first
// line, and a Tests line for roles that run the suite.
func TestReportShape(t *testing.T) {
	ps := prompts(t)
	for _, name := range []string{"implementer", "gate"} {
		if !has(ps[name], "`Tests: N passed, M failed`") {
			t.Errorf("%s does not name the Tests line", name)
		}
	}
	for name, p := range ps {
		_, report, found := strings.Cut(p, "\n## Report\n")
		if !found {
			t.Errorf("%s has no Report section", name)
			continue
		}
		if !has(report, "write your final report to {{report_path}}") {
			t.Errorf("%s does not say where its report goes", name)
		}
		if !has(report, "`## Owner needed`") {
			t.Errorf("%s: its Report section does not ask for `## Owner needed`", name)
		}
	}
}
