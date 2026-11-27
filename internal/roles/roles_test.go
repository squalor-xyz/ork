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
	"unicode/utf8"
)

// knownPlaceholders are the only {{name}} tokens a prompt may hold. The
// engine fills them (S04); roles.go lists them in its top comment.
var knownPlaceholders = []string{"report_path", "slice_id", "temp_dir", "worktree", "renders_dir"}

func TestImplementerPromptNamesOrkShowForRunReports(t *testing.T) {
	p := prompts(t)["implementer"]
	for _, want := range []string{
		"When the slice names a run report (a run id and a report name)",
		"read it with `ork show RUN NAME`",
		"Do not read ork's state directory directly.",
	} {
		t.Run(want, func(t *testing.T) {
			if !has(p, want) {
				t.Errorf("implementer prompt lacks %q", want)
			}
		})
	}
}

func TestImplementerPromptSaysHowToRename(t *testing.T) {
	p := prompts(t)["implementer"]
	for _, want := range []string{
		"Rename a tracked file with `git mv`; ork's commit picks up the move.",
		"Remove a file the slice moves code out of, or created by mistake, with `git rm` or `rm`; do not leave a stub.",
	} {
		t.Run(want, func(t *testing.T) {
			if !has(p, want) {
				t.Errorf("implementer prompt lacks %q", want)
			}
		})
	}
}

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
	want := []string{"implementer", "reviewer", "gate", "architect", "ui", "spec-check", "lead", "part", "spec-draft"}
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

// The local system prompt is embedded and short (S364, *proposed*): it
// replaces Claude Code's default system prompt, whose size is the point.
func TestLocalSystemPromptIsEmbeddedAndShort(t *testing.T) {
	p := LocalSystemPrompt()
	if strings.TrimSpace(p) == "" {
		t.Fatal("the local system prompt is empty")
	}
	if n := utf8.RuneCountInString(p); n >= 2000 {
		t.Fatalf("the local system prompt has %d characters, want under 2,000", n)
	}
	if _, err := Prompt("local-system"); err == nil {
		t.Error("local-system is not a role, but Prompt returned it")
	}
}

// The carve prompt (S359, *proposed*) is the spec check row's task when a local
// implementer's slice above size S is cut into slivers. It is no role of its
// own: ork's engine briefs it as the spec-check role.
func TestCarvePromptCutsASliceIntoSliversReadOnly(t *testing.T) {
	p := CarvePrompt()
	first, _, _ := strings.Cut(p, "\n")
	if first != "Role: carve. Contract: relay v13" {
		t.Errorf("first line %q", first)
	}
	for _, want := range []string{
		"You are read-only", "## Slivers", "### N. Title", "`Files:`", "`Failing tests:`", "`Check:`",
		"one or two files", "size S and effort 1", "relay rule u6", "## Owner needed", "{{report_path}}", "{{temp_dir}}", "{{slice_id}}", "{{worktree}}",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the carve prompt does not say %q", want)
		}
	}
	for _, bad := range []string{"## Slice ID", "`Ready`", "Question:"} {
		if strings.Contains(p, bad) {
			t.Errorf("the carve prompt holds the spec check's %q", bad)
		}
	}
	for i, r := range p {
		if r > 127 {
			t.Fatalf("the carve prompt holds a non-ASCII character at %d", i)
		}
	}
	if _, err := Prompt("carve"); err == nil {
		t.Error("carve is not a role, but Prompt returned it")
	}
}

// The embedded prompts are exactly the named roles, one file each, plus the
// local system prompt and the carve prompt.
func TestEmbeddedFilesMatchNames(t *testing.T) {
	entries, err := promptFS.ReadDir("prompts")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, strings.TrimSuffix(e.Name(), ".md"))
	}
	want := append(append([]string(nil), Names()...), "local-system", "carve")
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("embedded prompt files %v, want %v", got, want)
	}
}

func TestContractVersion(t *testing.T) {
	if ContractVersion != 13 {
		t.Fatalf("ContractVersion = %d, want 13", ContractVersion)
	}
	for _, f := range []string{"relay-first-line.txt", "relay-orchestrator-first-line.txt"} {
		if got := recordedVersion(t, f); got != ContractVersion {
			t.Errorf("%s records v%d, want v%d", f, got, ContractVersion)
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

func TestEveryRoleShowsTheOwnerNeededHeadingLiterally(t *testing.T) {
	numbered := regexp.MustCompile(`(?m)^\s*(?:[0-9]+\.\s*)?## [0-9]+\.`)
	for name, p := range prompts(t) {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(p, "\n## Owner needed\n") {
				t.Error("missing literal standalone Owner needed heading")
			}
			if numbered.MatchString(p) {
				t.Error("prompt contains a numbered heading")
			}
		})
	}
}

func TestImplementerAndGateShowTheTestsLineLiterally(t *testing.T) {
	ps := prompts(t)
	for _, name := range []string{"implementer", "gate"} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(ps[name], "\nTests: N passed, M failed\n") {
				t.Error("missing literal standalone Tests line")
			}
		})
	}
	for _, want := range []string{
		"with nothing before or after them on that line",
		"put skipped or blocked tests under item 6, not on the Tests line",
	} {
		t.Run(want, func(t *testing.T) {
			if !has(ps["implementer"], want) {
				t.Errorf("implementer missing guidance: %s", want)
			}
		})
	}
}

func TestGatePromptNamesTheRelianceLine(t *testing.T) {
	p := prompts(t)["gate"]
	for _, want := range []string{"`Tests: N passed, M failed`", "Tests: relied on ork round results", "ran no tests and relied on ork's round check"} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(p, want) {
				t.Fatalf("gate prompt lacks %q", want)
			}
		})
	}
}

func TestPromptsTellRolesListNumbersAreNotHeadings(t *testing.T) {
	if !has(prompts(t)["implementer"], "They are not part of the report: never number a heading.") {
		t.Error("implementer does not explain list numbers are not part of the report and headings are never numbered")
	}
}

func TestAcceptedAdjContractsIsOne(t *testing.T) {
	if got := AcceptedAdjContracts(); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("AcceptedAdjContracts() = %v, want [1]", got)
	}
	got := AcceptedAdjContracts()
	got[0] = 99
	if again := AcceptedAdjContracts(); again[0] != 1 {
		t.Fatalf("AcceptedAdjContracts hands out its own slice: %v", again)
	}
	if AdjContractAccepted(2) {
		t.Fatal("AdjContractAccepted(2) = true, want false")
	}
	if !AdjContractAccepted(1) {
		t.Fatal("AdjContractAccepted(1) = false")
	}
	if AcceptedAdjContractsPhrase() != "adj-json v1" {
		t.Fatalf("phrase %q", AcceptedAdjContractsPhrase())
	}
}

func TestAcceptedContractsAreV12AndV13(t *testing.T) {
	if got := AcceptedContracts(); !reflect.DeepEqual(got, []int{12, 13}) {
		t.Fatalf("AcceptedContracts() = %v, want [12 13]", got)
	}
	got := AcceptedContracts()
	got[0] = 99
	if again := AcceptedContracts(); again[0] != 12 {
		t.Fatalf("AcceptedContracts hands out its own slice: %v", again)
	}
	if !ContractAccepted(ContractVersion) {
		t.Errorf("the prompts' own version v%d is not accepted", ContractVersion)
	}
}

func TestPromptsFirstLineNamesTheirContract(t *testing.T) {
	if ContractVersion != 13 {
		t.Errorf("ContractVersion = %d, want 13", ContractVersion)
	}
	for name, p := range prompts(t) {
		first, _, _ := strings.Cut(p, "\n")
		if want := "Role: " + name + ". Contract: relay v13"; first != want {
			t.Errorf("%s: first line %q, want %q", name, first, want)
		}
	}
}

func TestCheckContractLine(t *testing.T) {
	for _, v := range []int{7, 8, 9, 10, 11, 12, 13} {
		got, err := CheckContractLine(fmt.Sprintf("Contract: relay v%d", v))
		if got != v || (err == nil) != (v == 12 || v == 13) {
			t.Errorf("v%d: %d, %v", v, got, err)
		}
		if err != nil && !strings.Contains(err.Error(), "relay v12 or v13 and slicer prints v"+strconv.Itoa(v)) {
			t.Errorf("v%d: message %q does not name the set {12, 13}", v, err)
		}
	}
	for _, bad := range []string{"", "something else", "Contract: relay v", "Contract: relay v12 extra"} {
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
	recorded := ContractVersion
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
		"no `[fix]` finding",
		"no `blocker` owner finding",
		"tests green",
		"docs",
		"Non-blocking",
	} {
		if !has(g, want) {
			t.Errorf("gate does not mention %q", want)
		}
	}
}

// runLinesHoldNoPipe reports each line of text that holds -run and a pipe.
func runLinesHoldNoPipe(t *testing.T, name, text string) {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "-run") && strings.Contains(line, "|") {
			t.Errorf("%s has a -run line with a pipe: %s", name, line)
		}
	}
}

// S571: the gate runs a focused test as one command per test, because a pipe in
// a -run pattern makes a compound command that the read-only allowlist denies.
func TestGatePromptSaysOneTestPerCommand(t *testing.T) {
	g := prompts(t)["gate"]
	for _, want := range []string{
		"one test per command",
		"-run NAME PKG",
		"a pipe makes a compound command that the read-only allowlist denies",
	} {
		if !has(g, want) {
			t.Errorf("gate does not say %q", want)
		}
	}
	runLinesHoldNoPipe(t, "gate prompt", g)
}

// v11: every `fix` finding fails the gate, with no severity scale, and a minor
// item is tagged `later`.
func TestGateHasNoSeverityScale(t *testing.T) {
	g := prompts(t)["gate"]
	for _, bad := range []string{"severity", "medium"} {
		if has(strings.ToLower(g), bad) {
			t.Errorf("gate prompt still mentions %q", bad)
		}
	}
	if !has(g, "tag a minor item `[later]` instead") {
		t.Error("gate prompt does not say a minor item is tagged `[later]`")
	}
}

// v11: no role changes directory, the read-only ones included.
func TestEveryRoleForbidsCd(t *testing.T) {
	for name, p := range prompts(t) {
		if !has(p, "Use absolute paths. Never change directory.") {
			t.Errorf("%s does not forbid changing directory", name)
		}
	}
}

func TestReadOnlyRoles(t *testing.T) {
	ps := prompts(t)
	for _, name := range []string{"reviewer", "gate", "architect", "ui", "spec-check", "spec-draft"} {
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

// S332 (adj 404): a slice's changelog entry is written with slicer, and a spec
// step that names the changelog file is read as that command (adj 445).
func TestImplementerPromptWritesTheChangelogEntryWithSlicer(t *testing.T) {
	ps := prompts(t)
	for _, want := range []string{
		"slicer changelog add {{slice_id}} --text",
		"slicer config changelog_path",
		"never edit the changelog file",
	} {
		if !has(ps["implementer"], want) {
			t.Errorf("implementer does not mention %q", want)
		}
	}
	for name, p := range ps {
		if strings.Contains(p, "CHANGELOG.md") {
			t.Errorf("%s prompt names CHANGELOG.md", name)
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
			"Run your own shell commands in the foreground and background none.",
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

func TestImplementerPromptReanchorsFirst(t *testing.T) {
	p := prompts(t)["implementer"]
	reanchor, testFirst := strings.Index(p, "Re-anchor"), strings.Index(p, "Test-first")
	if reanchor < 0 || testFirst < 0 || reanchor >= testFirst {
		t.Error("implementer must Re-anchor before Test-first")
	}
}

// S383 (*proposed*): ork reads the goals and the queue once and gives the
// reviewer and the architect a Direction section. The prompts name it, tell the
// role how to read one item, and no longer require the whole queue.
func requirePromptNamesTheDigest(t *testing.T, role string) {
	t.Helper()
	p := prompts(t)[role]
	for _, want := range []string{
		"Direction section",
		"slicer show ID --json --lean",
		"could not build",
	} {
		if !has(p, want) {
			t.Errorf("%s prompt lacks %q", role, want)
		}
	}
	for _, gone := range []string{"slicer list --json --lean", "the queue,", "the queue `"} {
		if has(p, gone) {
			t.Errorf("%s prompt still requires the whole queue: %q", role, gone)
		}
	}
}

func TestReviewerPromptNamesTheDigest(t *testing.T) { requirePromptNamesTheDigest(t, "reviewer") }

func TestArchitectPromptNamesTheDigest(t *testing.T) { requirePromptNamesTheDigest(t, "architect") }

// S554 (adj 560): a reviewer that may write its temp directory (S510) can run a
// probe test through go test -overlay without touching the tree.
func TestReviewerPromptCarriesTheProbeRecipe(t *testing.T) {
	p := prompts(t)["reviewer"]
	for _, want := range []string{
		"## Probe tests",
		"-overlay",
		"probe test file",
		// The closing braces are spaced: TestPlaceholders counts "}}" as a placeholder end.
		`{"Replace": {"{{worktree}}/PKGDIR/probe_test.go": "{{temp_dir}}/probe_test.go"} }`,
		"only when the brief says you may write your temp directory",
		"## Base overlays",
		"## Go module imports",
	} {
		if !has(p, want) {
			t.Errorf("reviewer prompt lacks %q", want)
		}
	}
	command := "go -C {{worktree}} test -overlay {{temp_dir}}/overlay.json -run TestProbe ./PKGDIR"
	if n := strings.Count(p, command); n != 1 {
		t.Errorf("reviewer prompt holds the plain overlay command %d times, want 1", n)
	}
	for i, r := range p {
		if r > 127 {
			t.Fatalf("reviewer prompt holds a non-ASCII character at %d", i)
		}
	}
	known := map[string]bool{}
	for _, k := range knownPlaceholders {
		known[k] = true
	}
	for _, m := range regexp.MustCompile(`\{\{([^{}]*)\}\}`).FindAllStringSubmatch(p, -1) {
		if !known[m[1]] {
			t.Errorf("reviewer prompt holds unknown placeholder %s", m[0])
		}
	}
}

func TestGatePromptPointsAtBaseOverlays(t *testing.T) {
	p := prompts(t)["gate"]
	for _, want := range []string{"## Base overlays", "one changed Go file reverted to the base", "as a plain command", "ork wrote those overlay files for this round"} {
		if !has(p, want) {
			t.Errorf("gate prompt lacks %q", want)
		}
	}
	for _, line := range strings.Split(p, "\n") {
		if strings.Contains(line, "-run") && strings.Contains(line, "|") {
			t.Errorf("gate prompt joins test names: %s", line)
		}
	}
	for i, r := range p {
		if r > 127 {
			t.Errorf("gate prompt holds a non-ASCII character at %d", i)
		}
	}
	for _, word := range []string{"severity", "medium"} {
		if strings.Contains(strings.ToLower(p), word) {
			t.Errorf("gate prompt contains %q", word)
		}
	}
}

func TestImplementerPromptSelfCheck(t *testing.T) {
	p := prompts(t)["implementer"]
	selfCheck, report := strings.Index(p, "\n## Self-check\n"), strings.Index(p, "\n## Report\n")
	if selfCheck < 0 || report < 0 || selfCheck >= report {
		t.Fatal("implementer must self-check before the Report section")
	}
	section := p[selfCheck:report]
	for _, want := range []string{
		"concurrency",
		"handle lifetime",
		"docs updated with code",
		"test that fails when the wiring is removed",
		"fix rounds included",
		"`n/a`",
	} {
		t.Run(want, func(t *testing.T) {
			if !has(section, want) {
				t.Errorf("self-check does not mention %q", want)
			}
		})
	}
}

func TestImplementerPromptSpecEdits(t *testing.T) {
	_, report, _ := strings.Cut(prompts(t)["implementer"], "\n## Report\n")
	deviations := strings.Index(report, "6. Deviations")
	specEdits := strings.Index(report, "Spec edits:")
	owner := strings.Index(report, "\n7. ")
	if deviations < 0 || specEdits < 0 || owner < 0 || specEdits <= deviations || specEdits >= owner {
		t.Error("implementer Report must contain Spec edits: after 6. Deviations and before 7. ")
	}
}

func TestImplementerPromptSaysEngineRunsChecks(t *testing.T) {
	p := prompts(t)["implementer"]
	owner, rest, _ := strings.Cut(p, "\n## Self-check\n")
	for _, want := range []string{
		"ork runs the project check command and `slicer check` after the round, outside your sandbox.",
		"A check you could not run because of your sandbox belongs under deviations (item 6), not `## Owner needed`.",
	} {
		if !has(owner, want) {
			t.Errorf("Owner questions lacks %q", want)
		}
	}
	_, report, _ := strings.Cut(rest, "\n## Report\n")
	deviations, ownerNeeded, _ := strings.Cut(report, "\n7. ")
	if !has(deviations, "Include checks you could not run because of your sandbox.") {
		t.Error("report item 6 must include sandbox-limited checks")
	}
	if !has(ownerNeeded, "Do not put sandbox-limited checks here.") {
		t.Error("report item 7 must exclude sandbox-limited checks")
	}
}

// Relay v11: a Check line marked [AFTER LAND] needs something main does not
// have yet. The implementer and the gate do not run it; ork lists it for the
// owner on the approval card (S204, *proposed*).
func TestImplementerAndGatePromptsSkipAfterLandLines(t *testing.T) {
	p := prompts(t)
	for _, want := range []string{
		"A Check line marked `[AFTER LAND]` needs something main does not have yet.",
		"Do not run it; ork lists it for the owner.",
	} {
		if !has(p["implementer"], want) {
			t.Errorf("implementer prompt lacks %q", want)
		}
	}
	if want := "an `[AFTER LAND]` Check line is not run and is not a failure;"; !has(p["gate"], want) {
		t.Errorf("gate prompt lacks %q", want)
	}
}

func TestStructuredOwnerQuestionPrompt(t *testing.T) {
	for name, p := range prompts(t) {
		for _, want := range []string{"Question: ONE LINE TITLE", "Options:", "1. LABEL: summary", "2. LABEL: summary", "Recommend: 1 - one-line reasoning", "2 to 4"} {
			if !strings.Contains(p, want) {
				t.Errorf("%s missing %q", name, want)
			}
		}
	}
}

// The UI pass is ork's own text (roles.prompts, adj 165): it names what it
// checks, takes no plugin skill, and gives no verdict.
func TestUIPrompt(t *testing.T) {
	p := prompts(t)["ui"]
	for _, want := range []string{
		"80x24",
		"ASCII and mono",
		"Clipping",
		"Key hints",
		"You give no verdict: the gate decides.",
		"(A) Defects the owner would see",
		"(B) Key hints and interaction",
		"(C) Notes for later",
		"(D) Doc accuracy",
		"UI design doc",
		"CONFIRMED",
		"PLAUSIBLE",
	} {
		if !has(p, want) {
			t.Errorf("ui does not say %q", want)
		}
	}
	// Vendor-neutral: no plugin skill and no harness's tool names.
	for _, bad := range []string{"frontend-design", "skill", "Skill", "VERDICT"} {
		if strings.Contains(p, bad) {
			t.Errorf("ui prompt mentions %q", bad)
		}
	}
	if !ascii(p) {
		t.Error("ui prompt holds a non-ASCII character")
	}
}

// The UI pass runs in any project, so its prompt names no path of ork's own:
// the project's design doc comes from the brief (S136).
func TestUIPromptHasNoOrkPaths(t *testing.T) {
	p := prompts(t)["ui"]
	for _, bad := range []string{"docs/dashboard.md", "dashboard.md", "internal/", "testdata/", "golden"} {
		if strings.Contains(p, bad) {
			t.Errorf("ui prompt names an ork path %q", bad)
		}
	}
}

// S416 (adj 505), S538 (adj 644): the UI pass renders outside the tree. Helper
// files go in its temp dir by the Write tool, each shell call is one plain
// command, TUI views come from the TUI render command of the brief (no test file
// of the pass's own), and CLI views come from the render flag of S475.
func TestUIPromptNamesTheOutOfTreeRenderRoute(t *testing.T) {
	p := prompts(t)["ui"]
	rules, renders, ok := strings.Cut(p, "\n## Renders\n")
	if !ok {
		t.Fatal("ui prompt has no Renders section")
	}
	rules, _, _ = strings.Cut(rules, "\n## UI pass\n")
	renders, _, _ = strings.Cut(renders, "\n## Finding kinds\n")
	for _, want := range []string{
		"Write helper files in {{temp_dir}} with the Write tool.",
		"Run one plain command per shell call: no `&&`, `;`, pipe, redirect or env prefix.",
	} {
		if !has(rules, want) {
			t.Errorf("ui rules lack %q", want)
		}
	}
	for _, want := range []string{
		"TUI views come from the TUI render command in the brief's `## TUI renders` section",
		"{{renders_dir}}/",
		"CLI views come from the render flag",
	} {
		if !has(renders, want) {
			t.Errorf("ui Renders lacks %q", want)
		}
	}
	for _, bad := range []string{"-overlay", "a test file kept outside the tree"} {
		if has(renders, bad) {
			t.Errorf("ui Renders still names %q", bad)
		}
	}
}

// S597 (adj 595): when the changed behavior only shows below 80x24, the UI pass
// also renders at the height the tests use for that view and names the size.
func TestUIPromptNamesAShortFixtureHeight(t *testing.T) {
	p := prompts(t)["ui"]
	_, renders, ok := strings.Cut(p, "\n## Renders\n")
	if !ok {
		t.Fatal("ui prompt has no Renders section")
	}
	renders, _, _ = strings.Cut(renders, "\n## Finding kinds\n")
	for _, want := range []string{
		"only shows below 80x24",
		"the height the tests use for that view",
		"name the size in your report",
		"VIEW-WxH.txt",
	} {
		if !has(renders, want) {
			t.Errorf("ui Renders lacks %q", want)
		}
	}
}

func ascii(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// The spec check is ork's own role (roles.spec-check, adj 164): one read-only
// pass over a batch of slices, with a ready or structured questions answer
// for each.
func TestSpecCheckPrompt(t *testing.T) {
	p := prompts(t)["spec-check"]
	for _, want := range []string{
		"## Slice S10",
		"`Ready`",
		"Question: Which store holds the cache index?",
		"one section for each slice in your brief",
		"the whole batch in one pass",
		"Put a blank line between the blocks of one slice",
		"A decision the owner recorded is settled",
		"Before inventing a decision key, use `adj check TEXT` to look up decisions by topic and reuse an existing key for the same topic.",
		"Never use `adj find`.",
		"A gap you can close by reading the code is not a question",
	} {
		if !has(p, want) {
			t.Errorf("spec-check does not say %q", want)
		}
	}
	for _, bad := range []string{"VERDICT", "[owner]", "[fix]"} {
		if strings.Contains(p, bad) {
			t.Errorf("spec-check prompt mentions %q", bad)
		}
	}
	if !ascii(p) {
		t.Error("spec-check prompt holds a non-ASCII character")
	}
}

func TestSpecCheckPromptJudgesNotesRecordedAfterTheSpec(t *testing.T) {
	p := prompts(t)["spec-check"]
	for _, want := range []string{
		"Notes recorded after the spec", "read each listed note against the slice's Implement and Failing tests, and its Files and Check",
		"contradict, or do not carry out", "Ask one question for the slice, with exactly two options",
		"`follow the note`", "`follow the spec`", "Omit the Decision key line for this question",
		"Ask nothing for a note that does not conflict",
	} {
		t.Run(want, func(t *testing.T) {
			if !has(p, want) {
				t.Errorf("spec-check does not say %q", want)
			}
		})
	}
	if !ascii(p) {
		t.Error("spec-check prompt holds a non-ASCII character")
	}
}

func TestSpecCheckPromptRequiresVerifiableCriteria(t *testing.T) {
	p := prompts(t)["spec-check"]
	for _, want := range []string{
		"its `Failing tests` and `Check` each say how to tell it is done",
		"a command the implementer's sandbox can run with an expected result",
		"counts, exit status, output text or an observable file or state",
		"a `[HUMAN CHECK]` item says what the owner looks at and what they should see",
		"A slice is not ready when its criteria have any of these gaps",
		"no command and no observable result",
		"a failing test with no name or no stated assertion",
	} {
		t.Run(want, func(t *testing.T) {
			if !has(p, want) {
				t.Errorf("spec-check does not say %q", want)
			}
		})
	}
	// S406: a command the sandbox cannot run is no gap; ork or the
	// orchestrator runs it.
	if has(p, "a command the implementer's sandbox cannot run") {
		t.Error("spec-check still treats a command the implementer's sandbox cannot run as a gap")
	}
}

// S406 (adj 454): the brief splits Check lines three ways; the spec check
// raises nothing for implementer-runnable or ork-run lines and lists the
// orchestrator-run lines without a question.
func TestSpecCheckPromptAsksOnlyAboutOrchestratorLines(t *testing.T) {
	p := prompts(t)["spec-check"]
	for _, want := range []string{
		"Raise no finding and ask no question for an implementer-runnable line or a line ork runs itself",
		"List each orchestrator-run line in the report",
		"`## Orchestrator-run Check commands` section",
		"and ask nothing about it",
		"Raise a finding for an orchestrator-run line only when its command looks wrong or risky",
	} {
		t.Run(want, func(t *testing.T) {
			if !has(p, want) {
				t.Errorf("spec-check does not say %q", want)
			}
		})
	}
	for _, bad := range []string{"Ask the owner to replace it with a runnable form", "orchestrator-only"} {
		if has(p, bad) {
			t.Errorf("spec-check still says %q", bad)
		}
	}
}

func TestSpecCheckPromptProposesTheWording(t *testing.T) {
	p := prompts(t)["spec-check"]
	for _, want := range []string{
		"ask one question for that slice, not one for each vague line",
		"Offer exactly three options: `tighten`, `human check` and `as written`",
		"Put a concrete proposed command and expected result in the `tighten` summary and recommend `tighten`",
		"Omit the Decision key line: the answer belongs to this slice",
	} {
		t.Run(want, func(t *testing.T) {
			if !has(p, want) {
				t.Errorf("spec-check does not say %q", want)
			}
		})
	}
}

func TestSpecCheckPromptDoesNotAskWhatTheCodeSettles(t *testing.T) {
	p := prompts(t)["spec-check"]
	for _, want := range []string{
		"Read the slice, its Files and the project's `AGENTS.md` for the command and result first",
		"When they make the command and result obvious (for example the project's test command), close the gap and call the slice ready",
		"Ask about criteria only when the command or result cannot be read from them",
	} {
		t.Run(want, func(t *testing.T) {
			if !has(p, want) {
				t.Errorf("spec-check does not say %q", want)
			}
		})
	}
}

// The spec draft is ork's own read-only role (S179, scope.spec-drafting): it
// proposes the text of a slice's spec sections and ork writes nothing until the
// owner approves each one.
func TestSpecDraftPrompt(t *testing.T) {
	p := prompts(t)["spec-draft"]
	for _, want := range []string{
		"## Section Why",
		"## Section Files",
		"## Section Failing tests",
		"## Section Implement",
		"## Section Check",
		"A section you leave as it is has the single word `unchanged` under its heading",
		"You may draft every section from the title alone",
		"Trace each failing test to the file that must change",
		"Every path under `Files` must exist in the checkout, or the line says `(new)`",
		"Use no level-two heading inside a section",
		"ork writes nothing until the owner approves each section",
		"Before inventing a decision key, use `adj check TEXT` to look up decisions by topic and reuse an existing key for the same topic.",
		"Never use `adj find`.",
	} {
		if !has(p, want) {
			t.Errorf("spec-draft does not say %q", want)
		}
	}
	for _, bad := range []string{"VERDICT", "[owner]", "[fix]", "## Slice S10"} {
		if strings.Contains(p, bad) {
			t.Errorf("spec-draft prompt mentions %q", bad)
		}
	}
	if !ascii(p) {
		t.Error("spec-draft prompt holds a non-ASCII character")
	}
}

// S571: the spec draft writes each focused test command in Check as one
// command per test, never a -run pattern that joins names.
func TestSpecDraftPromptSaysOneTestPerCommand(t *testing.T) {
	p := prompts(t)["spec-draft"]
	for _, want := range []string{
		"Write each focused test command in **Check** as `go -C DIR test -run NAME PKG`, one command for each test",
		"Never write a `-run` pattern that joins names",
	} {
		if !has(p, want) {
			t.Errorf("spec-draft does not say %q", want)
		}
	}
	runLinesHoldNoPipe(t, "spec-draft prompt", p)
	if !ascii(p) {
		t.Error("spec-draft prompt holds a non-ASCII character")
	}
}

func TestAcceptedContractsPhrase(t *testing.T) {
	for _, tc := range []struct {
		versions []int
		want     string
	}{{[]int{11}, "v11"}, {[]int{11, 12}, "v11 or v12"}, {[]int{11, 12, 13}, "v11, v12 or v13"}} {
		if got := phrase(tc.versions); got != tc.want {
			t.Errorf("phrase(%v) = %q, want %q", tc.versions, got, tc.want)
		}
	}
	if got := AcceptedContractsPhrase(); got != "v12 or v13" {
		t.Errorf("accepted phrase: %q", got)
	}
	if _, err := CheckContractLine("Contract: relay v9"); err == nil || err.Error() != "ork accepts relay v12 or v13 and slicer prints v9" {
		t.Errorf("contract error: %v", err)
	}
}

func TestGatePromptNoFixCriterionIsShort(t *testing.T) {
	g := prompts(t)["gate"]
	if !strings.Contains(g, "   - no `[fix]` finding (tag a minor item `[later]` instead);\n") || strings.Contains(g, "however small") {
		t.Error("gate no-fix criterion is not the short fragment")
	}
	criteria := strings.Split(strings.Split(g, "3. Judge these criteria")[1], "4. Cite")[0]
	for _, line := range strings.Split(criteria, "\n") {
		if strings.HasPrefix(line, "   - ") && !strings.HasSuffix(line, ";") && !strings.HasSuffix(line, ".") {
			t.Errorf("criterion is not a one-line fragment: %s", line)
		}
	}
}

func TestPromptsPointAtDoneWhen(t *testing.T) {
	for name, p := range prompts(t) {
		if !strings.Contains(p, "brief's `## Done when` section") {
			t.Errorf("%s lacks Done when pointer", name)
		}
		if !strings.Contains(p, "overrides nothing in the report parts") {
			t.Errorf("%s lacks report precedence", name)
		}
	}
}

// S288: the three prompts that tag findings say what an [owner] body holds,
// and show one refused example and the same finding accepted.
func TestOwnerFindingRuleIsInEveryFindingPrompt(t *testing.T) {
	all := prompts(t)
	for _, name := range []string{"reviewer", "gate", "ui"} {
		p := all[name]
		for _, want := range []string{
			"holds only `Options:`, the numbered options, `Recommend:` and an optional `Decision key:` line",
			"no list marker before `Options:`, `Recommend:` or `Decision key:`",
			"shows it with the question only for `[owner blocker]`",
			"Refused (",
			"Accepted (",
		} {
			if !has(p, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
	}
}

// S288 (owner decision 2026-11-07): the Owner needed block is read as leniently
// as an owner finding, so no prompt says the block refuses extra context.
func TestOwnerNeededBlockWordingMatchesTheParser(t *testing.T) {
	n := 0
	for name, p := range prompts(t) {
		if !has(p, "From `Question:` onward") {
			continue
		}
		n++
		if has(p, "strict shape") || has(p, "no extra context lines inside or after it") {
			t.Errorf("%s still says the Owner needed block refuses extra context", name)
		}
		if !has(p, "no list marker before `Options:`, `Recommend:` or `Decision key:`, and no line between the options") {
			t.Errorf("%s lacks the Owner needed block rule", name)
		}
		// The spec check's context is kept and filed in adj, not shown; every
		// other block shows it with the question.
		want := "ork keeps any other line as context and shows it with the question"
		if name == "spec-check" {
			want = "ork keeps any other line in the block's text and files it with the adj record when the block has a Decision key, but does not show it"
		}
		if !has(p, want) {
			t.Errorf("%s lacks %q", name, want)
		}
	}
	if n != 9 {
		t.Errorf("%d prompts carry the Owner needed block rule, want 9", n)
	}
}

// S271: ork sets TMPDIR to the role's temp directory for Claude and Codex, so
// a role needs no "env TMPDIR=DIR" prefix and no mkdir (neither matches an
// allowlist pattern). Every prompt says so.
func TestPromptsSayTMPDIRAlreadyPointsAtTheTempDir(t *testing.T) {
	for name, p := range prompts(t) {
		for _, want := range []string{
			"ork sets TMPDIR to {{temp_dir}} for Claude and Codex roles",
			"Do not prefix commands with `env TMPDIR=` and do not create the directory",
		} {
			if !has(p, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
	}
}

// S61 (carried from S10): the lead's and the part's prompts say, as the
// implementer's does, that no note, handoff or done runs before review.
func TestSplitPromptsSayNoHandoffBeforeReview(t *testing.T) {
	ps := prompts(t)
	for _, name := range []string{"implementer", "lead", "part"} {
		if !has(ps[name], "Run no `slicer note`, `slicer handoff` or `slicer done` before review") {
			t.Errorf("%s lacks the no handoff before review rule", name)
		}
	}
}

// S61: a part changes only its own files and runs no slicer command that
// writes (slicer ai.py, Roles); the lead owns the tracking and names the parts.
func TestPartPromptChangesOnlyItsOwnFilesAndWritesNoTracking(t *testing.T) {
	part := prompts(t)["part"]
	for _, want := range []string{
		"Change only the files your part lists in the brief.",
		"Run no slicer command that writes: not `slicer start`, `edit`, `note`, `handoff` or `done`.",
		"Do not run the full suite",
	} {
		if !has(part, want) {
			t.Errorf("part prompt lacks %q", want)
		}
	}
	if strings.Contains(strings.ToLower(part), "you are read-only") {
		t.Error("part prompt says it is read-only")
	}
}

// S61 (owner decision, adj 385): the lead may hand a file's findings back to
// the part that owns the file; the part prompt says it may receive them.
func TestLeadPromptNamesTheHandBackSectionAndPartPromptTheHandedFindings(t *testing.T) {
	ps := prompts(t)
	for _, want := range []string{
		"hand a file's findings back to the part that owns the file",
		"Add the section `## Hand back` before `## Owner needed`",
		"- P2: internal/b/b.go:3 the nil case is wrong; return the error.",
		"do not change a file you hand back",
	} {
		if !has(ps["lead"], want) {
			t.Errorf("lead prompt lacks %q", want)
		}
	}
	if !has(ps["part"], "findings the lead handed back to your part") {
		t.Error("part prompt does not say the lead may hand findings back")
	}
}

func TestLeadPromptNamesTheOnePartsSectionOrkReads(t *testing.T) {
	lead := prompts(t)["lead"]
	for _, want := range []string{
		"add the section `## Parts` before `## Owner needed`",
		"- P1: files internal/a/a.go, internal/a/a_test.go; tests TestA, TestAEdge",
		"You alone run the slicer commands that write; the parts run none.",
		"ork runs parts that list a common file one after another",
	} {
		if !has(lead, want) {
			t.Errorf("lead prompt lacks %q", want)
		}
	}
}

// Each prompt lets its role end the report with up to 3 tool feedback lines,
// in the run report's shape, and says they are advisory (adj 325,
// relay.tool-feedback; relay v12).
func TestPromptsAllowAdvisoryToolFeedback(t *testing.T) {
	for name, p := range prompts(t) {
		t.Run(name, func(t *testing.T) {
			for _, want := range []string{
				"up to 3 tool feedback lines",
				"tool feedback: N. " + name + " | tool: slicer|ork|NAME | kind: friction|feature|deterministic-check | proposal: TEXT | evidence: TEXT",
				"Tool feedback is advisory: it is not a finding",
			} {
				if !has(p, want) {
					t.Errorf("%s prompt does not say %q", name, want)
				}
			}
		})
	}
}

func TestCheckContractLineAcceptsV13(t *testing.T) {
	v, err := CheckContractLine("Contract: relay v13")
	if v != 13 || err != nil {
		t.Fatalf("v13: %d, %v", v, err)
	}
	for _, v := range []int{11, 14} {
		got, err := CheckContractLine(fmt.Sprintf("Contract: relay v%d", v))
		if got != v || err == nil || !strings.Contains(err.Error(), "v12 or v13") {
			t.Errorf("v%d: %d, %v", v, got, err)
		}
	}
}

func TestCheckContractLineRefusesV11(t *testing.T) {
	v, err := CheckContractLine("Contract: relay v11")
	if v != 11 || err == nil || !strings.Contains(err.Error(), "v12 or v13") {
		t.Fatalf("v11: %d, %v", v, err)
	}
}
