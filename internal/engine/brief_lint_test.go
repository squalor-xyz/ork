// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

type briefGrantPair struct {
	name, sentence, role, grantSite string
	vendors                         []string
	input                           func(role, vendor string) BriefInput
}

// Add a row for each BuildBrief permission sentence whose runner grant applies
// only to some vendors. Cite the runner site that grants the permission.
var briefGrantPairs = []briefGrantPair{
	{
		name:     "Reviewer temp-write",
		sentence: "You may create and change files under your temp directory with Edit or Write",
		role:     RoleReviewer, grantSite: "internal/daemon/runner.go:57",
		vendors: []string{"claude", "claude-local"},
		input: func(role, vendor string) BriefInput {
			in := baseBrief()
			in.Role, in.Vendor, in.ReadOnly = role, vendor, true
			in.TMPDir = in.TempDir + "/process"
			in.ProgressFile = in.TempDir + "/progress.md"
			return in
		},
	},
	{
		name: "UI renders directory", sentence: "The UI renders directory",
		role: RoleUI, grantSite: "internal/daemon/runner.go:49-52",
		vendors: []string{"claude"},
		input: func(role, vendor string) BriefInput {
			in := baseBrief()
			in.Role, in.Vendor, in.ReadOnly = role, vendor, true
			return in
		},
	},
}

func briefGrantViolations(pair briefGrantPair, build func(BriefInput) (string, error)) []error {
	var violations []error
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck, RoleSpecDraft} {
		for _, vendor := range []string{"claude", "claude-local", "codex", "codex-local", "grok", "grok-local"} {
			brief, err := build(pair.input(role, vendor))
			if err != nil {
				violations = append(violations, fmt.Errorf("sentence %q role %s vendor %s grant %s: %w", pair.sentence, role, vendor, pair.grantSite, err))
				continue
			}
			want := role == pair.role && slices.Contains(pair.vendors, vendor)
			present := strings.Contains(brief, pair.sentence)
			if present != want {
				state := "missing"
				if present {
					state = "present"
				}
				violations = append(violations, fmt.Errorf("sentence %q is %s for role %s vendor %s; grant %s", pair.sentence, state, role, vendor, pair.grantSite))
			}
		}
	}
	return violations
}

func TestBriefGrantLintFlagsASentenceGatedOnRoleOnly(t *testing.T) {
	pair := briefGrantPairs[1]
	violations := briefGrantViolations(pair, func(in BriefInput) (string, error) {
		if in.Role == pair.role {
			return pair.sentence, nil
		}
		return "", nil
	})
	if len(violations) != 5 {
		t.Fatalf("got %d violations, want 5: %v", len(violations), violations)
	}
	for i, vendor := range []string{"claude-local", "codex", "codex-local", "grok", "grok-local"} {
		for _, detail := range []string{pair.sentence, pair.role, vendor, pair.grantSite, "present"} {
			if !strings.Contains(violations[i].Error(), detail) {
				t.Errorf("violation %q missing %q", violations[i], detail)
			}
		}
	}
}

func TestBriefGrantLintPassesAVendorGatedSentence(t *testing.T) {
	pair := briefGrantPairs[1]
	build := func(in BriefInput) (string, error) {
		if in.Role == pair.role && slices.Contains(pair.vendors, in.Vendor) {
			return pair.sentence, nil
		}
		return "", nil
	}
	if violations := briefGrantViolations(pair, build); len(violations) != 0 {
		t.Fatalf("vendor-gated sentence: %v", violations)
	}
	violations := briefGrantViolations(pair, func(BriefInput) (string, error) { return "", nil })
	if len(violations) != 1 {
		t.Fatalf("missing sentence: got %d violations, want 1: %v", len(violations), violations)
	}
	for _, detail := range []string{pair.sentence, pair.role, "claude", pair.grantSite, "missing"} {
		if !strings.Contains(violations[0].Error(), detail) {
			t.Errorf("violation %q missing %q", violations[0], detail)
		}
	}
}

func TestBriefRoleGatedSentencesMatchVendorGatedGrants(t *testing.T) {
	for _, pair := range briefGrantPairs {
		t.Run(pair.name, func(t *testing.T) {
			for _, err := range briefGrantViolations(pair, BuildBrief) {
				t.Error(err)
			}
		})
	}
}

// These helpers lint the assembled instructions without changing BuildBrief.
func briefWriteTargets(brief string) []string {
	// "sole exception" opens the Local progress note paragraph, which names the
	// writable note before its Edit or Write sentence.
	cues := regexp.MustCompile(`(?i)\b(?:write|writes|edit|put|save|saves)\b|\bsole exception\b|(?:^|\s)-renders\b`)
	paths := regexp.MustCompile(`(?:^|[ '"(])('/[^']*'|/[^\s'"();]+)`)
	negation := regexp.MustCompile(`(?i)\b(?:never|not|no)\b`)
	var targets []string
	for _, line := range strings.Split(brief, "\n") {
		for _, sentence := range strings.Split(strings.ReplaceAll(line, "`", ""), ". ") {
			seen := map[string]bool{}
			for _, cue := range cues.FindAllStringIndex(sentence, -1) {
				// -renders-extra is another flag, not the render destination flag.
				if cue[1] < len(sentence) && sentence[cue[1]] == '-' {
					continue
				}
				tail := sentence[cue[1]:]
				for _, match := range paths.FindAllStringSubmatchIndex(tail, -1) {
					if negation.MatchString(tail[:match[2]]) {
						continue
					}
					path := strings.TrimRight(strings.Trim(tail[match[2]:match[3]], "'"), ".:,")
					if !seen[path] {
						targets = append(targets, path)
						seen[path] = true
					}
				}
			}
		}
	}
	return targets
}

func briefWritePathViolations(in BriefInput, brief string) []error {
	dirs := []string{in.TempDir}
	if in.TMPDir != "" {
		dirs = append(dirs, in.TMPDir)
	}
	if !in.ReadOnly && (in.Role == RoleImplementer || in.Role == RolePart) {
		dirs = append(dirs, in.Worktree)
	}
	files := []string{in.ReportPath, in.ProgressFile}
	var violations []error
	for _, target := range briefWriteTargets(brief) {
		allowed := slices.Contains(files, target)
		for _, dir := range dirs {
			rel, err := filepath.Rel(dir, target)
			if dir != "" && err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				allowed = true
			}
		}
		if !allowed {
			violations = append(violations, fmt.Errorf("write target %q for role %s vendor %s is outside allowed directories %q and exact files %q", target, in.Role, in.Vendor, dirs, files))
		}
	}
	return violations
}

func briefWriteMatrix() []BriefInput {
	var matrix []BriefInput
	for _, variant := range []struct {
		role        string
		lead, carve bool
	}{
		{RoleImplementer, false, false}, {RoleImplementer, true, false},
		{RolePart, false, false}, {RoleReviewer, false, false},
		{RoleGate, false, false}, {RoleArchitect, false, false},
		{RoleUI, false, false}, {RoleSpecCheck, false, false},
		{RoleSpecCheck, false, true}, {RoleSpecDraft, false, false},
	} {
		for _, vendor := range []string{"claude", "claude-local", "codex", "codex-local", "grok", "grok-local"} {
			in := baseBrief()
			in.Role, in.Vendor = variant.role, vendor
			in.Lead, in.Carve = variant.lead, variant.carve
			in.TMPDir, in.ProgressFile = "/state/runs/r1/tmp-role/process", "/state/runs/r1/tmp/progress.md"
			in.ReadOnly = true
			matrix = append(matrix, in)
			if in.Role == RoleUI {
				in.RenderCLI, in.RenderTUI = true, true
				matrix = append(matrix, in)
			}
			if in.Role == RoleImplementer || in.Role == RolePart {
				in.ReadOnly = false
				matrix = append(matrix, in)
			}
		}
	}
	return matrix
}

func TestBriefWriteTargetsFindsEachWriteInstruction(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       []string
	}{
		{"renders", "go test -renders /tmp/renders", []string{"/tmp/renders"}},
		{"quoted", "go test -renders '/tmp/render files'", []string{"/tmp/render files"}},
		{"directory", "Write the files into the renders directory `/tmp/renders`.", []string{"/tmp/renders"}},
		{"progress", "Write your progress note now to /tmp/progress.md", []string{"/tmp/progress.md"}},
		{"tools", "Edit and Write under /tmp/tools", []string{"/tmp/tools"}},
		{"saved", "ork saves it to /tmp/report.md.", []string{"/tmp/report.md"}},
		{"scratch", "Put scratch files in /tmp/process, never in a shared system temp directory such as /tmp", []string{"/tmp/process"}},
		{"before", "Work only in /wt and do not read or write outside it", nil},
		{"negations", "Write never in /a. Write not in /b. Write no files in /c", nil},
		{"case", "wRiTeS /a. SAVE /b. eDiT /c. PUT /d", []string{"/a", "/b", "/c", "/d"}},
		{"whole words", "rewrite /a. output /b. saved /c. -renders-extra /d", nil},
		{"boundaries", "Write elsewhere. Read /a\n/a write elsewhere\n{\"path\":\"/b\"}", nil},
		{"punctuation", "Write (/a, '/b' /c: /d.", []string{"/a", "/b", "/c", "/d"}},
		{"embedded", "Write https://example.com/a x/b relative/path", nil},
		{"semicolon", "Edit and Write under /tmp/ui; any other command is denied", []string{"/tmp/ui"}},
		{"progress exception", "The sole exception to the read-only and workspace rules above is `/tmp/progress.md`. After each step, replace this note. Update the note with Edit or Write.", []string{"/tmp/progress.md"}},
		{"progress exception with temp", "Besides your temp directory, the sole exception to the read-only and workspace rules above is `/tmp/progress.md`. Update the note with Edit or Write.", []string{"/tmp/progress.md"}},
		{"exception negated", "The sole exception is not /a", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := briefWriteTargets(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("targets = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBriefWritePathsOutsideTheAllowedSetAreFlagged(t *testing.T) {
	in := baseBrief()
	in.Role, in.Vendor = RoleUI, "codex"
	in.TMPDir, in.ProgressFile = "/process", "/notes/progress.md"
	for _, tc := range []struct {
		path string
		bad  bool
	}{
		{"/outside/renders", true},
		{in.TempDir + "0", true},
		{in.TempDir + "/../outside", true},
		{in.ReportPath + "/child", true},
		{in.ProgressFile + "/child", true},
		{in.TempDir + "/renders", false},
		{in.TempDir, false},
		{in.TMPDir, false},
		{in.TMPDir + "/child", false},
		{in.ReportPath, false},
		{in.ProgressFile, false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			got := briefWritePathViolations(in, "go test -renders "+tc.path)
			if !tc.bad {
				if len(got) != 0 {
					t.Fatalf("allowed target: %v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("violations = %v, want one", got)
			}
			for _, want := range []string{tc.path, in.Role, in.Vendor, in.TempDir, in.TMPDir, in.ReportPath, in.ProgressFile} {
				if !strings.Contains(got[0].Error(), want) {
					t.Errorf("violation %q lacks %q", got[0], want)
				}
			}
		})
	}
	for _, in := range briefWriteMatrix() {
		t.Run(fmt.Sprintf("worktree/%s/%s/lead=%t/readonly=%t", in.Role, in.Vendor, in.Lead, in.ReadOnly), func(t *testing.T) {
			got := briefWritePathViolations(in, "Write under "+in.Worktree+"/file.go")
			want := 1
			if !in.ReadOnly && (in.Role == RoleImplementer || in.Role == RolePart) {
				want = 0
			}
			if len(got) != want {
				t.Fatalf("worktree violations = %v, want %d", got, want)
			}
		})
	}
}

func TestBriefWriteTargetsAreInsideTheAllowedSetForEveryRole(t *testing.T) {
	for _, in := range briefWriteMatrix() {
		name := fmt.Sprintf("%s/%s/lead=%t/carve=%t/readonly=%t/renders=%t", in.Role, in.Vendor, in.Lead, in.Carve, in.ReadOnly, in.RenderCLI)
		t.Run(name, func(t *testing.T) {
			brief, err := BuildBrief(in)
			if err != nil {
				t.Fatalf("BuildBrief role=%s vendor=%s: %v", in.Role, in.Vendor, err)
			}
			for _, err := range briefWritePathViolations(in, brief) {
				t.Error(err)
			}
			targets := briefWriteTargets(brief)
			want := []string{in.ReportPath}
			if in.Role == RoleUI {
				want = append(want, filepath.Join(in.TempDir, "renders"))
			}
			if strings.Contains(brief, "## Note first") || strings.Contains(brief, "## Local progress note") {
				want = append(want, in.ProgressFile)
			}
			for _, path := range want {
				if !slices.Contains(targets, path) {
					t.Errorf("extractor missed required target %q; got %q", path, targets)
				}
			}
		})
	}
}
