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

// The S10 note: ork shows the owner question to the owner and files it to adj
// only when the toggle is on (owner.questions), so no prompt says ork files it.
func TestOwnerQuestionWording(t *testing.T) {
	for name, p := range prompts(t) {
		if has(p, "ork files the question") {
			t.Errorf("%s still says ork files the question", name)
		}
		if !has(p, "ork shows the question to the owner (and may file it)") {
			t.Errorf("%s does not say ork shows the question to the owner (and may file it)", name)
		}
	}
}

func TestAcceptedContracts(t *testing.T) {
	if got := AcceptedContracts(); !reflect.DeepEqual(got, []int{7, 8}) {
		t.Fatalf("AcceptedContracts() = %v, want [7 8]", got)
	}
	got := AcceptedContracts()
	got[0] = 99
	if again := AcceptedContracts(); again[0] != 7 {
		t.Fatalf("AcceptedContracts hands out its own slice: %v", again)
	}
	if !ContractAccepted(ContractVersion) {
		t.Errorf("the prompts' own version v%d is not accepted", ContractVersion)
	}
}

func TestCheckContractLineAcceptsV8(t *testing.T) {
	if v, err := CheckContractLine("Contract: relay v8"); err != nil || v != 8 {
		t.Fatalf("v8: %d, %v", v, err)
	}
	if v, err := CheckContractLine("Contract: relay v7 x"); err == nil || v != 0 {
		t.Fatalf("malformed v7: %d, %v", v, err)
	}
}

func TestPromptsStayAtV7(t *testing.T) {
	if ContractVersion != 7 {
		t.Errorf("ContractVersion = %d, want 7", ContractVersion)
	}
	for name, p := range prompts(t) {
		first, _, _ := strings.Cut(p, "\n")
		if want := "Role: " + name + ". Contract: relay v7"; first != want {
			t.Errorf("%s: first line %q, want %q", name, first, want)
		}
	}
}

func TestCheckContractLine(t *testing.T) {
	if v, err := CheckContractLine("Contract: relay v7"); err != nil || v != 7 {
		t.Fatalf("v7: %d, %v", v, err)
	}
	if v, err := CheckContractLine("Contract: relay v7  "); err != nil || v != 7 {
		t.Fatalf("v7 with trailing spaces: %d, %v", v, err)
	}
	v, err := CheckContractLine("Contract: relay v9")
	if err == nil || v != 9 || err.Error() != "ork accepts relay v7, v8 and slicer prints v9" {
		t.Fatalf("v9: %d, %v", v, err)
	}
	if v, err := CheckContractLine("Contract: relay v6"); err == nil || v != 6 {
		t.Fatalf("v6: %d, %v", v, err)
	}
	for _, bad := range []string{"", "something else entirely", "Contract: relay v", "Contract: relay v7 extra"} {
		if v, err := CheckContractLine(bad); err == nil || v != 0 {
			t.Errorf("%q: %d, %v", bad, v, err)
		}
	}
}

// The check is set membership, not an exact pin and not a minimum.
func TestCheckContractLineAcceptsEveryVersionInTheSet(t *testing.T) {
	saved := acceptedContracts
	defer func() { acceptedContracts = saved }()
	acceptedContracts = []int{7, 9}
	for v, wantOK := range map[int]bool{6: false, 7: true, 8: false, 9: true, 10: false} {
		got, err := CheckContractLine(fmt.Sprintf("Contract: relay v%d", v))
		if (err == nil) != wantOK || got != v {
			t.Errorf("v%d: %d, %v (accepted %v)", v, got, err, wantOK)
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

func TestImplementerSearchAndDenialRules(t *testing.T) {
	p := prompts(t)["implementer"]
	for _, want := range []string{
		"Never search `.slicer/` or `decisions/`",
		"One denied command does not mean every command is denied",
		"or say in the report why not",
		"re-grep each cited file:line in its own file",
		"Skip that step, finish the rest of the work",
		"put the denial's exact text under item 6 of the report (deviations)",
		"ork records the denial and a successful round goes on",
		"In the finish step, a denied `slicer note`, `done` or `handoff` goes under `## Owner needed`.",
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
			"Decision key: area.topic",
			"lowercase dotted decision key",
			"Never change directory",
			"Never open, search, parse or edit tracking files",
			"foreground",
			"Never commit",
			"Never read vendor credential or login files",
			"Do not run `adj ask` yourself; ork shows the question to the owner (and may file it)",
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
