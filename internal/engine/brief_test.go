// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/roles"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestSliceCheckCommandWarningsExemptsAFilesPath(t *testing.T) {
	for _, prefix := range []string{"", "./", "A=1 B=2 "} {
		t.Run(prefix, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"slice": map[string]any{"sections": []map[string]string{
				{"heading": "Files", "body": "- `scripts/new-check.sh`\n- `internal/cli:12-20`\n- `./tools/x.sh`"},
				{"heading": "Check", "body": "- `" + prefix + "scripts/new-check.sh --x`\n- `internal/cli`\n- `tools/x.sh`\n- `internal/other`"},
			}}})
			if err != nil {
				t.Fatal(err)
			}
			got := SliceCheckCommandWarnings(string(raw), t.TempDir())
			want := `Check entry "internal/other": first word "internal/other" is not a runnable command`
			if len(got) != 1 || got[0] != want {
				t.Fatalf("warnings %q, want only %q", got, want)
			}
		})
	}
}

func TestReadOnlyBriefsNameTheDeniedForms(t *testing.T) {
	const want = "Run one plain command per call: no pipes, && or ; chains, redirects, command substitution, shell variables, env prefixes or cd. Use literal absolute paths with git -C and go -C."
	for _, vendor := range []string{"claude", "claude-local", "codex", "grok"} {
		for _, role := range []string{RoleReviewer, RoleGate, RoleArchitect, RoleSpecCheck, RoleSpecDraft, RoleUI} {
			t.Run(vendor+"/"+role, func(t *testing.T) {
				in := baseBrief()
				in.Role, in.Vendor, in.ReadOnly = role, vendor, true
				got, err := BuildBrief(in)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Count(got, want) != 1 {
					t.Error("brief must have exactly one shared denied-forms line")
				}
			})
		}
	}
}

func TestImplementerBriefNotesSandboxSocketTests(t *testing.T) {
	const want = "Socket-bound tests may fail in sandbox; report as sandbox-limited. ork's round check is the evidence."
	for _, vendor := range []string{"claude", "claude-local", "codex", "grok"} {
		t.Run(vendor, func(t *testing.T) {
			in := baseBrief()
			in.Role, in.Vendor = RoleImplementer, vendor
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, want) {
				t.Error("implementer brief lacks sandbox socket-test reporting and round-check evidence")
			}
		})
	}
}

func TestSavedOrientationReachesOnlyLocalImplementer(t *testing.T) {
	for _, vendor := range []string{"claude-local", "claude"} {
		t.Run(vendor, func(t *testing.T) {
			h := newHarness(t)
			h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{{Vendor: vendor, Model: "model"}}
			e, err := New(context.Background(), h.deps, h.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(h.deps.Store.RunDir(e.Snapshot().ID), "orientation.md"), []byte("quoted function and nearest test"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := e.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, req := range h.runner.requests() {
				want := vendor == "claude-local" && req.Role == RoleImplementer
				if strings.Contains(req.Brief, "## Orientation\n\nquoted function and nearest test") != want {
					t.Errorf("orientation in %s %s brief: want %v", vendor, req.Role, want)
				}
			}
		})
	}
}

func TestLocalRunWithoutOrientation(t *testing.T) {
	h := newHarness(t)
	h.cfg.Lineup[RoleImplementer] = []runrec.LineupEntry{{Vendor: "claude-local", Model: "model"}}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	for _, req := range h.runner.requests() {
		if strings.Contains(req.Brief, "## Orientation") {
			t.Fatal("missing pack produced an Orientation section")
		}
	}
}

func TestOrientationBriefSelection(t *testing.T) {
	for _, tc := range []struct {
		role, vendor string
		want         bool
	}{
		{RoleImplementer, "claude-local", true},
		{RoleImplementer, "claude", false},
		{RoleReviewer, "claude-local", false},
	} {
		t.Run(tc.role+"/"+tc.vendor, func(t *testing.T) {
			in := baseBrief()
			in.Role, in.Vendor, in.Orientation = tc.role, tc.vendor, "saved pack"
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(got, "## Orientation\n\nsaved pack") != tc.want {
				t.Fatalf("orientation selection: want %v", tc.want)
			}
		})
	}
}

func TestFirstRowBriefIsUnchanged(t *testing.T) {
	h := fallbackHarness(t, RoleImplementer)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	req := h.runner.requests()[0]
	want, err := BuildBrief(BriefInput{
		Role: req.Role, SliceID: item, Worktree: req.Worktree,
		TempDir: req.TempDir, TMPDir: req.TMPDir, ReportPath: req.ReportPath,
		SliceJSON: sliceJSON(), EnvLines: h.proj.EnvironmentLines,
		CheckCommand: h.proj.CheckCommand,
		Allow:        DefaultImplementerAllowlist(h.proj.CheckCommand),
		Sections:     []BriefSection{{Title: "Round", Body: "This is the first round. The slice is not started: run `slicer start " + item + " --render --strict` first."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Brief != want || strings.Contains(req.Brief, "## Earlier lineup rows") {
		t.Fatal("first row brief changed")
	}
}

func baseBrief() BriefInput {
	return BriefInput{
		Role: RoleImplementer, SliceID: "S04", Worktree: "/p/.worktrees/S04",
		TempDir: "/state/runs/r1/tmp/implementer-1", ReportPath: "/state/runs/r1/implementer-1.md",
		SliceJSON: `{"id":"S04","title":"Engine"}`,
		EnvLines:  []string{"Shell: zsh on darwin. `timeout` is not installed.", "Go 1.27.2, no cd."},
	}
}

func TestResumedBriefUnchangedLine(t *testing.T) {
	for _, n := range []int{0, 2} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			in := baseBrief()
			in.Resumed = true
			in.TreeUnchangedSince = n
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			section, _ := section(got, "## Resumed session")
			if n == 0 && strings.Contains(section, "The tree is unchanged since round") {
				t.Error("unchanged line printed without fingerprint")
			}
			if n == 2 && !strings.Contains(section, "The tree is unchanged since round 2, when you last reported.") {
				t.Error("unchanged line presence wrong")
			}
			if strings.Contains(section, "run git status and git diff") != (n == 0) {
				t.Error("git re-run instruction presence wrong")
			}
			if n == 2 && strings.Contains(got, "run git status and git diff") {
				t.Error("git re-run instruction remains elsewhere in brief")
			}
			if !strings.Contains(section, "read again every file you cite") {
				t.Error("cited-file read instruction missing")
			}
		})
	}
}

func localImplementerBrief(t *testing.T) string {
	t.Helper()
	in := baseBrief()
	in.Vendor = "claude-local"
	in.ProgressFile = "/state/runs/r1/tmp/progress-implementer.md"
	in.Allow = []string{"Read", "Edit", "Write", "Bash(go test*)"}
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestLocalImplementerStartsWithToolsAndNote(t *testing.T) {
	got := localImplementerBrief(t)
	_, rest, _ := strings.Cut(got, "\n")
	t.Run("tool-rules-first", func(t *testing.T) {
		if !strings.HasPrefix(rest, "\n## Tool rules\n\nAllowed forms, verbatim: Read; Edit; Write; Bash(go test*);") {
			t.Fatal("tool rules are not first after the role line")
		}
	})
	for _, phrase := range []string{"one plain command per call", "no pipes", "An env prefix is allowed only where an allowed form starts with one.", "Write your progress note now", "/state/runs/r1/tmp/progress-implementer.md"} {
		t.Run(phrase, func(t *testing.T) {
			prefix := got[:max(0, strings.Index(got, "Read the project's"))]
			if !strings.Contains(prefix, phrase) {
				t.Fatalf("missing early instruction: %s", phrase)
			}
		})
	}
}

func TestCloudImplementerByteBaseline(t *testing.T) {
	for _, vendor := range []string{"claude", "codex", "grok"} {
		t.Run(vendor, func(t *testing.T) {
			in := baseBrief()
			in.Vendor = vendor
			in.Allow = []string{"Read", "Edit", "Write", "Bash(go test*)"}
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			// Normalize the contract marker (S432). S384 adds the plain-v13
			// Outcome report guidance to the implementer prompt.
			// S417 adds guidance for reading named run reports with ork show.
			// S505 adds the sandbox socket-test note to the checklist.
			got = strings.Replace(got, "Role: implementer. Contract: relay v13\n", "Role: implementer. Contract: relay v12\n", 1)
			want := "05d20aa4bf069abea441c2fb89b1583a510a45dfb4324970290465584c156b43"
			switch vendor {
			case "claude":
				want = "095a8e4b4f0785da57f82aafb16c1bf953e860a1794cb46edc8a5f7cb70c4e40"
			case "grok":
				// S305: a grok implementer's checklist names dontAsk mode.
				want = "000c8469ba9e870aa0ee11cf67c9cb01ef77c9184bb670bc854e667dbff742db"
			}
			if fmt.Sprintf("%x", sha256.Sum256([]byte(got))) != want {
				t.Fatalf("%s cloud brief changed %x", vendor, sha256.Sum256([]byte(got)))
			}
		})
	}
}

func TestLocalImplementerTrustsCheckedAnchors(t *testing.T) {
	got := localImplementerBrief(t)
	_, work, _ := strings.Cut(got, "## Work\n")
	work, _, _ = strings.Cut(work, "## Later rounds")
	for _, phrase := range []string{"anchors were checked by the spec check", "trust the symbol names"} {
		t.Run(phrase, func(t *testing.T) {
			if !strings.Contains(work, phrase) {
				t.Fatalf("local Work missing: %s", phrase)
			}
		})
	}
	for _, phrase := range []string{"Re-anchor", "slicer edit", "the files the slice names"} {
		t.Run("omit/"+phrase, func(t *testing.T) {
			if strings.Contains(work, phrase) {
				t.Fatalf("local Work still requests %s", phrase)
			}
		})
	}
}

func TestLocalImplementerOneTestAtATime(t *testing.T) {
	got := localImplementerBrief(t)
	_, got, _ = strings.Cut(got, "## Work\n")
	got, _, _ = strings.Cut(got, "## Later rounds")
	steps := []string{"write one listed test", "see it fail", "write the code", "see it pass", "update the note", "next test", "After the last test", "mutation-check every new assertion", "full test suite"}
	for i, phrase := range steps {
		t.Run(phrase, func(t *testing.T) {
			pos := strings.Index(got, phrase)
			previous := -1
			if i > 0 {
				previous = strings.Index(got, steps[i-1])
			}
			if pos < 0 || pos <= previous || (i > 0 && previous < 0) {
				t.Fatalf("missing or out-of-order local step: %s", phrase)
			}
		})
	}
}

func TestLocalImplementerAnchorLog(t *testing.T) {
	got := localImplementerBrief(t)
	for _, phrase := range []string{"Append an anchor log every few reads", "file:line symbol: purpose"} {
		t.Run(phrase, func(t *testing.T) {
			if !strings.Contains(got, phrase) {
				t.Fatalf("missing note instruction: %s", phrase)
			}
		})
	}
}

func TestLocalImplementerWorkspaceLimitNamesNote(t *testing.T) {
	got := localImplementerBrief(t)
	_, limit, ok := strings.Cut(got, "## Workspace limit\n")
	if !ok {
		t.Fatal("no workspace limit section")
	}
	limit, _, _ = strings.Cut(limit, "\n## ")
	if !strings.Contains(limit, "/state/runs/r1/tmp/progress-implementer.md") {
		t.Fatal("workspace limit does not name the progress note as a write exception")
	}
}

func TestLocalImplementerMutationCheckRunsOnce(t *testing.T) {
	in := baseBrief()
	in.Vendor = "claude-local"
	in.ProgressFile = "/state/runs/r1/tmp/progress-implementer.md"
	in.Allow = []string{"Read", "Edit", "Write", "Bash(go test*)"}
	in.SliceJSON = `{"id":"S04","slice":{"sections":[{"heading":"Failing tests","body":"- ` + "`TestA`" + `: first.\n- ` + "`TestB`" + `: second."}]}}`
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	_, done, ok := strings.Cut(got, "## Done when\n")
	if !ok {
		t.Fatal("no Done when section")
	}
	done, _, _ = strings.Cut(done, "\n## ")
	if n := strings.Count(done, "mutation-check every new assertion"); n != 1 {
		t.Fatalf("Done when names the mutation check %d times, want once", n)
	}
	for _, phrase := range []string{"TestA", "TestB"} {
		if !strings.Contains(done, phrase+"`: first.") && !strings.Contains(done, phrase+"`: second.") {
			t.Fatalf("Done when lost the %s item", phrase)
		}
	}
	if !strings.Contains(done, "After the last listed test, mutation-check every new assertion once") || !strings.Contains(done, "full test suite of each affected project once") {
		t.Fatal("Done when lacks the once-after-the-last-test item")
	}
	cloud := baseBrief()
	cloud.Vendor = "claude"
	cloud.Allow = in.Allow
	cloud.SliceJSON = in.SliceJSON
	cb, err := BuildBrief(cloud)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(cb, "mutation-check every new assertion") != 2 {
		t.Fatal("cloud Done when no longer mutation-checks each failing test")
	}
}

func TestLocalImplementerBriefIsShorter(t *testing.T) {
	local := localImplementerBrief(t)
	in := baseBrief()
	in.Vendor = "claude"
	in.ProgressFile = "/state/runs/r1/tmp/progress-implementer.md"
	in.Allow = []string{"Read", "Edit", "Write", "Bash(go test*)"}
	cloud, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("local=%d bytes cloud=%d bytes", len(local), len(cloud))
	if len(local) >= len(cloud) {
		t.Fatal("local implementer brief is not shorter than cloud")
	}
}

func TestReadOnlyProgressBriefException(t *testing.T) {
	in := baseBrief()
	in.Vendor, in.Role, in.ReadOnly = "claude-local", RoleReviewer, true
	in.ProgressFile = "/state/runs/r1/tmp/progress-reviewer.md"
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "The sole exception to the read-only and workspace rules above is") || !strings.Contains(got, "After each step") || !strings.Contains(got, in.ProgressFile) {
		t.Fatal("read-only brief lacks its scoped progress exception")
	}
}

// Only a reviewer with a process temp dir is told it may write there (S510);
// the runner grants the write under the same condition.
func TestReviewerBriefSaysItMayWriteItsTempDir(t *testing.T) {
	const grant = "You may create and change files under your temp directory with Edit or Write"
	for _, tc := range []struct {
		name, vendor, role, tmp string
		want                    bool
	}{
		{"cloud reviewer", "claude", RoleReviewer, "/state/t/r1/reviewer-1", true},
		{"local reviewer", "claude-local", RoleReviewer, "/state/t/r1/reviewer-1", true},
		{"reviewer without a process temp dir", "claude", RoleReviewer, "", false},
		{"codex reviewer", "codex", RoleReviewer, "/state/t/r1/reviewer-1", false},
		{"grok reviewer", "grok", RoleReviewer, "/state/t/r1/reviewer-1", false},
		{"codex-local reviewer", "codex-local", RoleReviewer, "/state/t/r1/reviewer-1", false},
		{"grok-local reviewer", "grok-local", RoleReviewer, "/state/t/r1/reviewer-1", false},
		{"gate", "claude", RoleGate, "/state/t/r1/gate-1", false},
		{"architect", "claude", RoleArchitect, "/state/t/r1/architect-1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := baseBrief()
			in.Vendor, in.Role, in.ReadOnly, in.TMPDir = tc.vendor, tc.role, true, tc.tmp
			in.ProgressFile = "/state/runs/r1/tmp/progress-reviewer.md"
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(got, grant) != tc.want {
				t.Fatalf("grant sentence present = %v, want %v", !tc.want, tc.want)
			}
			if tc.vendor == "claude-local" && tc.want != strings.Contains(got, "this note and the files in its temp directory are the only files it may change") {
				t.Fatalf("local progress note wording does not match the grant (%v)", tc.want)
			}
		})
	}
}

func TestLocalBriefHasTargetedReads(t *testing.T) {
	for _, vendor := range []string{"claude-local", "codex-local", "grok-local", "claude", "codex", "grok"} {
		for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate, RoleArchitect, RoleSpecCheck, RoleUI} {
			capPhrase := "Never read a whole file over about 500 lines."
			if role == RoleImplementer {
				capPhrase = "Never read a whole file over about 150 lines; Grep for the symbol and Read 150 lines around it."
			}
			for _, phrase := range []string{"Read files in targeted ranges (offset and limit), or grep first.", capPhrase, "Keep reasoning brief."} {
				t.Run(vendor+"/"+role+"/"+phrase, func(t *testing.T) {
					in := baseBrief()
					in.Vendor, in.Role = vendor, role
					got, err := BuildBrief(in)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(got, phrase) != strings.HasSuffix(vendor, "-local") {
						t.Fatalf("brief targeted-read guidance %q: vendor %s", phrase, vendor)
					}
				})
			}
		}
	}
}

func TestLocalBriefKeepsItsAdapterRules(t *testing.T) {
	for _, tc := range []struct {
		vendor, role, want string
		readonly           bool
	}{
		{"claude-local", RoleUI, "Load these skills for the UI pass: tui-design", true},
		{"claude-local", RoleImplementer, "Change files with the Edit and Write tools only", false},
		{"grok-local", RoleReviewer, "Grok plan mode:", true},
	} {
		t.Run(tc.vendor+"/"+tc.role, func(t *testing.T) {
			in := baseBrief()
			in.Role, in.Vendor, in.ReadOnly = tc.role, tc.vendor, tc.readonly
			in.UIPassSkills = []string{"tui-design"}
			in.Allow = DefaultImplementerAllowlist("go test ./...")
			got, err := BuildBrief(in)
			if err != nil || !strings.Contains(got, tc.want) {
				t.Fatalf("local brief missing %q: %v", tc.want, err)
			}
		})
	}
}

var claudeShellRules = []string{
	"Change files with the Edit and Write tools only: no `sed -i`, no scripts (python or others), no heredocs, no redirects into files.",
	"Run one plain command per shell call: no pipes, no `&&` or `;`, no command substitution, no `cd` (use absolute paths, `go -C DIR` and `git -C DIR`).",
	"If a command is denied, use an allowed form; do not retry it another way.",
}

func requireClaudeShellRules(t *testing.T, brief string) {
	t.Helper()
	_, done, ok := strings.Cut(brief, "## Done when\n\n")
	if !ok {
		t.Fatal("missing Done when")
	}
	previous := strings.Index(done, "Allowed forms, verbatim:")
	if previous < 0 {
		t.Fatal("missing resolved allowlist")
	}
	for _, rule := range claudeShellRules {
		pos := strings.Index(done, "- [ ] "+rule+"\n")
		if pos <= previous {
			t.Errorf("missing or misplaced shell rule: %s", rule)
		}
		previous = pos
	}
}

func TestClaudeImplementerBriefHasShellRules(t *testing.T) {
	in := baseBrief()
	in.Vendor = "claude"
	in.Allow = DefaultImplementerAllowlist("go test ./...")
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	requireClaudeShellRules(t, got)
}

func TestShellRulesOnlyForTheClaudeImplementer(t *testing.T) {
	for _, tc := range []struct {
		role, vendor string
		readOnly     bool
	}{
		{RoleImplementer, "codex", false}, {RoleImplementer, "", false},
		{RoleImplementer, "Claude", false}, {RoleImplementer, "claude", true},
		{RoleReviewer, "claude", false}, {RoleGate, "claude", false},
		{RoleArchitect, "claude", false}, {RoleUI, "claude", false},
		{RoleSpecCheck, "claude", false},
	} {
		t.Run(tc.role+"/"+tc.vendor+"/"+fmt.Sprint(tc.readOnly), func(t *testing.T) {
			in := baseBrief()
			in.Role, in.Vendor, in.ReadOnly = tc.role, tc.vendor, tc.readOnly
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			for _, rule := range claudeShellRules {
				if strings.Contains(got, rule) {
					t.Errorf("unexpected shell rule: %s", rule)
				}
			}
		})
	}
}

func TestShellRulesFollowTheRegistryAllowlist(t *testing.T) {
	for _, allow := range [][]string{{"Edit", "Bash(dotnet test*)"}, {"Edit"}, {"Edit", "Bash(check ```\nnext*)"}} {
		in := baseBrief()
		in.Vendor, in.Allow = "claude", allow
		want := "- [ ] Allowed forms, verbatim: " + strings.Join(allow, "; ") + "; any other command is denied; no pipes, no cd, no env prefix unless an entry allows it.\n"
		got, err := BuildBrief(in)
		if err != nil {
			t.Fatal(err)
		}
		requireClaudeShellRules(t, got)
		if strings.Count(got, want) != 1 || strings.Count(got, "Allowed forms, verbatim:") != 1 || strings.Contains(got, "slicer *") || strings.Contains(got, "## Shell commands") {
			t.Error("resolved allowlist changed or duplicated")
		}
	}
}

func TestFinishStepBriefKeepsTheShellRules(t *testing.T) {
	in := baseBrief()
	in.Vendor, in.Finish = "claude", true
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	requireClaudeShellRules(t, got)
}

func TestClaudeFallbackBriefListsTheRealAllowlist(t *testing.T) {
	h := fallbackHarness(t, RoleImplementer)
	h.runner.exits = map[string]ExitClass{"codex": ExitOutOfCredits}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	var vendors []string
	for _, req := range h.runner.requests() {
		if req.Role != RoleImplementer {
			continue
		}
		vendors = append(vendors, req.Vendor)
		if req.Vendor == "claude" {
			requireClaudeShellRules(t, req.Brief)
			want := "- [ ] Allowed forms, verbatim: " + strings.Join(req.Allowlist, "; ") + "; any other command is denied; no pipes, no cd, no env prefix unless an entry allows it.\n"
			if len(req.Allowlist) == 0 || strings.Count(req.Brief, want) != 1 || strings.Count(req.Brief, "Allowed forms, verbatim:") != 1 {
				t.Error("fallback brief does not list the request's real allowlist exactly once")
			}
		} else {
			for _, rule := range claudeShellRules {
				if strings.Contains(req.Brief, rule) {
					t.Errorf("codex brief has shell rule: %s", rule)
				}
			}
		}
	}
	if strings.Join(vendors, ",") != "codex,claude" {
		t.Fatalf("fallback vendors: %v", vendors)
	}
}

func TestSpecCheckFlagsUnrunnableChecks(t *testing.T) {
	in := baseBrief()
	in.Role = RoleSpecCheck
	raw, _ := json.Marshal([]any{map[string]any{"id": "S04", "slice": map[string]any{"sections": []map[string]string{{"heading": "Check", "body": "- `GOOS=linux go vet ./...`\n- `slicer check --root /old/checkout`"}}}}})
	in.SliceJSON = string(raw)
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	// S406: an env prefix is run by ork; a path outside the worktree is the
	// orchestrator's and is listed without a question.
	for _, want := range []string{"## Check commands ork runs\n\n- GOOS=linux go vet ./...\n", "## Orchestrator-run Check commands\n\n- slicer check --root /old/checkout\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("spec-check brief lacks %q", want)
		}
	}
	if strings.Contains(got, "Orchestrator-only") {
		t.Error("spec-check brief still names Orchestrator-only Check commands")
	}
}

// S406 (adj 454): the brief splits Check lines three ways, in code.
func TestCheckSectionsSplitThreeWays(t *testing.T) {
	in := baseBrief()
	in.Role = RoleSpecCheck
	in.Allow = []string{"Bash(go vet *)"}
	body := "- `go vet ./...`\n- `go test ./internal/x -run 'A|B' -v`\n- `rg -n 'credential' testdata/vendors`\n- `go vet ./... && go test ./...`\n- `go test ./... | tee out`"
	raw, _ := json.Marshal([]any{map[string]any{"id": "S04", "slice": map[string]any{"sections": []map[string]string{{"heading": "Check", "body": body}}}}})
	in.SliceJSON = string(raw)
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	want := "Check command classification for S04:\n\n" +
		"## Implementer-runnable Check commands\n\n- go vet ./...\n\n" +
		"## Check commands ork runs\n\n- go test ./internal/x -run 'A|B' -v\n- rg -n 'credential' testdata/vendors\n- go vet ./... && go test ./...\n\n" +
		"## Orchestrator-run Check commands\n\n- go test ./... | tee out\n\n"
	if !strings.Contains(got, want) {
		_, section, _ := strings.Cut(got, "Check command classification for S04:")
		t.Errorf("check sections are not split three ways; got:\n%s", section)
	}
}

// S406: only an && outside quotes and escapes splits a line.
func TestSplitAndChain(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    []string
	}{
		{"go vet ./...", []string{"go vet ./..."}},
		{"a && b", []string{"a", "b"}},
		{"a  &&  b&&c", []string{"a", "b", "c"}},
		{"rg 'a && b' docs", []string{"rg 'a && b' docs"}},
		{`rg "a && b" docs`, []string{`rg "a && b" docs`}},
		{`rg "a\" && b" x && y`, []string{`rg "a\" && b" x`, "y"}},
		{"a & b", []string{"a & b"}},
		{"a &&", []string{"a", ""}},
	} {
		if got := splitAndChain(tc.command); strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("splitAndChain(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

// S406: ork runs a plain && chain, so checkRunByOrk is true for it and false for
// a line with real shell syntax or a part ork would not run.
func TestCheckRunByOrk(t *testing.T) {
	const worktree = "/p/.worktrees/S04"
	for _, tc := range []struct {
		command string
		want    bool
	}{
		{"go vet ./...", true},
		{"GOOS=linux go vet ./...", true},
		{"go test ./x -run 'A|B' -v", true},
		{"rg -n 'a && b' docs", true},
		{"go vet ./... && go test ./...", true},
		{"go vet ./... && GOOS=linux go test ./... && gofmt -l .", true},
		{"go test ./... | tee out", false},
		{"go test ./... ; go vet ./...", false},
		{"go test ./... & go vet ./...", false},
		{"go vet ./... &&", false},
		{"&& go vet ./...", false},
		{"go vet ./... && && go test ./...", false},
		{"go vet ./... && go test /old/checkout", false},
		{"go vet ./... && go test ./... | tee out", false},
		{"go vet ./... && go test $X", false},
		{"go vet ./... && --yes", false},
		{"slicer check --root /old/checkout", false},
		{"--yes", false},
	} {
		if got := checkRunByOrk(tc.command, worktree); got != tc.want {
			t.Errorf("checkRunByOrk(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

// S407: criteria bullets never reach the spec check as Check commands.
func TestCheckSectionsSkipAcceptanceCriteria(t *testing.T) {
	in := baseBrief()
	in.Role = RoleSpecCheck
	raw, _ := json.Marshal([]any{map[string]any{"id": "S04", "slice": map[string]any{"sections": []map[string]string{{"heading": "Check", "body": "Acceptance criteria:\n- the run prints `friction` once\n\nCommands:\n- `true` exits 0."}}}}})
	in.SliceJSON = string(raw)
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "- true\n") {
		t.Error("spec-check brief lacks the command under Commands:")
	}
	if strings.Contains(got, "- friction\n") {
		t.Error("spec-check brief lists a backticked criterion as a Check command")
	}
}

func TestGateBriefSplitsCheckCommands(t *testing.T) {
	t.Run("prose", func(t *testing.T) {
		in := baseBrief()
		in.Role = RoleGate
		const prose = "No internal/tui change; no UI pass."
		raw, _ := json.Marshal(map[string]any{"slice": map[string]any{"sections": []map[string]string{{"heading": "Check", "body": "- " + prose}}}})
		in.SliceJSON = string(raw)
		got, err := BuildBrief(in)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "- "+prose+"\n") {
			t.Error("prose Check bullet appears in a command list")
		}
	})
	for _, tc := range []struct {
		name, command, group string
	}{
		{"test", "go test ./...", "Implementer-runnable"},
		{"env", "GOOS=linux go vet ./...", "Check commands ork runs"},
		{"outside", "slicer check --root /old/checkout", "Orchestrator-run"},
		{"sibling-prefix", "go -C /p/.worktrees/S040 test ./...", "Orchestrator-run"},
		{"inside", "go -C /p/.worktrees/S04 test ./...", "Implementer-runnable"},
		{"unknown", "make check", "Check commands ork runs"},
		{"custom", "custom-check ./...", "Implementer-runnable"},
		{"compound", "go test ./...; make check", "Orchestrator-run"},
		{"and-chain", "make check && make test", "Check commands ork runs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := baseBrief()
			in.Role = RoleGate
			in.Allow = append(DefaultImplementerAllowlist(""), "Bash(custom-check *)")
			raw, _ := json.Marshal(map[string]any{"slice": map[string]any{"sections": []map[string]string{{"heading": "Check", "body": "- `" + tc.command + "`"}}}})
			in.SliceJSON = string(raw)
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			heading := "## " + tc.group + " Check commands\n\n"
			if tc.group == "Check commands ork runs" {
				heading = "## Check commands ork runs\n\n"
			}
			if !strings.Contains(got, heading+"- "+tc.command+"\n") {
				t.Errorf("command classified incorrectly: want %q", heading+"- "+tc.command)
			}
		})
	}
}

func TestCheckCommandRunnableQuotedPipe(t *testing.T) {
	allow, err := ImplementerPreset("go", "")
	if err != nil {
		t.Fatal(err)
	}
	const worktree = "/p/.worktrees/S409"
	for _, tc := range []struct {
		name, command string
		runnable      bool
	}{
		{"single-quoted pipe", "go test ./internal/daemon -run 'MergedSpecCleanup|MergedCommitStopped' -v", true},
		{"double-quoted pipe", `go test ./internal/cli -run "SpecQuestion|PromptWraps" -v`, true},
		{"quoted redirect and semicolon", `go test ./... -run 'A>B;C&D<E'`, true},
		{"quoted dollar in single quotes", `go test ./... -run 'A$'`, true},
		{"dollar in double quotes", `go test ./... -run "A$B"`, false},
		{"backtick in double quotes", "go test ./... -run \"A`x`\"", false},
		{"unquoted pipe", "go test ./... | tee out", false},
		{"unquoted and", "go test ./... && rm -rf x", false},
		{"unquoted semicolon", "go test ./...; make check", false},
		{"unquoted redirect", "go test ./... > out", false},
		{"unquoted dollar", "go test ./... $X", false},
		{"unquoted pipe after quoted pipe", "go test ./... -run 'A|B' | tee out", false},
		{"unterminated quote", "go test ./... -run 'A|B", false},
		{"newline", "go test ./...\nrm -rf x", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkCommandRunnable(tc.command, worktree, allow); got != tc.runnable {
				t.Errorf("checkCommandRunnable(%q) = %v, want %v", tc.command, got, tc.runnable)
			}
		})
	}
	// An allowlist form that names the whole compound command still allows it.
	if !checkCommandRunnable("go test ./... | tee out", worktree, []string{"Bash(go test ./... | tee out)"}) {
		t.Error("an explicit whole-command form must allow its compound command")
	}
}

// The owner dropped rg from the presets (launch.allowlist-rg, adj 457): an rg
// Check line stays orchestrator-only, even with a quoted pipe.
func TestCheckCommandRunnableRgStaysOrchestratorOnly(t *testing.T) {
	const worktree = "/p/.worktrees/S409"
	lists := map[string][]string{"default": DefaultImplementerAllowlist("")}
	for _, name := range PresetNames() {
		list, err := ImplementerPreset(name, "")
		if err != nil {
			t.Fatal(err)
		}
		lists["preset "+name] = list
	}
	for name, list := range lists {
		for _, command := range []string{`rg -n "adj 416" docs/boundaries.md`, `rg -n 'a|b' docs`} {
			if checkCommandRunnable(command, worktree, list) {
				t.Errorf("%s: %q is runnable, want orchestrator-only", name, command)
			}
		}
	}
}

func TestGatePromptUsesOrkEvidence(t *testing.T) {
	in := baseBrief()
	in.Role = RoleGate
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"must accept ork's round check result", "The slice's own Check rows are required tests-green evidence", "Never fail tests-green solely for an orchestrator-run", "name them from ork's rows", "an earlier part of the chain failed", "Name which Check commands you ran", "Do not attempt commands your read-only mode cannot run"} {
		if !strings.Contains(got, want) {
			t.Errorf("gate brief lacks %q", want)
		}
	}
	if strings.Contains(got, "tests green: run the project's check and test commands yourself") {
		t.Error("gate still requires its own test run")
	}
}

func TestGateReceivesRoundCheckEvidence(t *testing.T) {
	h := newHarness(t)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	for _, req := range h.runner.requests() {
		if req.Role != RoleGate {
			continue
		}
		for _, want := range []string{"## Ork round check result", "checks-1.log", "Exit status: 0", "all packages", "A successful ork round check result is the tests-green evidence. You must accept it and must not run the suite."} {
			if !strings.Contains(req.Brief, want) {
				t.Errorf("gate request lacks %q", want)
			}
		}
		if strings.Contains(req.Brief, "Run the project's check command yourself") {
			t.Error("gate request still instructs the gate to run the project check itself")
		}
		return
	}
	t.Fatal("no gate request")
}

func TestBuildBriefFillsThePromptPlaceholders(t *testing.T) {
	in := baseBrief()
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "{{") {
		i := strings.Index(got, "{{")
		t.Fatalf("a placeholder is left: %q", got[i:min(len(got), i+30)])
	}
	if first, _, _ := strings.Cut(got, "\n"); first != "Role: implementer. Contract: relay v13" {
		t.Errorf("first line %q", first)
	}
	for _, want := range []string{
		"you implement exactly that slice, test-first, in the worktree /p/.worktrees/S04",
		"Put scratch files in /state/runs/r1/tmp/implementer-1",
		"write your final report to /state/runs/r1/implementer-1.md",
		"`slicer show S04`",
	} {
		if !strings.Contains(strings.Join(strings.Fields(got), " "), want) {
			t.Errorf("brief lacks %q", want)
		}
	}
}

func TestBuildBriefCarriesTheBriefParts(t *testing.T) {
	in := baseBrief()
	in.Sections = []BriefSection{{Title: "Checks failed", Body: "FAIL TestX: wrong"}, {Title: "Round", Body: "fix round 1"}}
	in.Earlier = []EarlierReport{{Name: "implementer-1", Text: "Tests: 3 passed, 0 failed"}}
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	order := []string{
		"Role: implementer.",
		"# Brief",
		`{"id":"S04","title":"Engine"}`,
		"Do not open, search, parse, or edit tracking files",
		"stay inside the workspace",
		"Shell: zsh on darwin. `timeout` is not installed.\n",
		"Go 1.27.2, no cd.",
		"/state/runs/r1/tmp/implementer-1",
		"/state/runs/r1/implementer-1.md",
		"## Checks failed\n\nFAIL TestX: wrong",
		"## Round\n\nfix round 1",
		"## Earlier reports",
		"### implementer-1\n\nTests: 3 passed, 0 failed",
	}
	pos := 0
	for _, want := range order {
		i := strings.Index(got[pos:], want)
		if i < 0 {
			t.Fatalf("%q is missing or out of order after offset %d", want, pos)
		}
		pos += i + len(want)
	}
}

func TestBuildBriefWithoutEarlierReportsOrSections(t *testing.T) {
	got, err := BuildBrief(baseBrief())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Earlier reports") {
		t.Error("an Earlier reports heading with nothing under it")
	}
}

func TestBuildBriefEveryRoleAndItsErrors(t *testing.T) {
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate, "architect"} {
		in := baseBrief()
		in.Role = role
		got, err := BuildBrief(in)
		if err != nil || !strings.HasPrefix(got, "Role: "+role+". Contract: relay v") {
			t.Errorf("%s: %v", role, err)
		}
	}
	in := baseBrief()
	in.Role = "janitor"
	if _, err := BuildBrief(in); err == nil {
		t.Error("unknown role accepted")
	}
	for field, edit := range map[string]func(*BriefInput){
		"SliceID":    func(b *BriefInput) { b.SliceID = "" },
		"Worktree":   func(b *BriefInput) { b.Worktree = "" },
		"TempDir":    func(b *BriefInput) { b.TempDir = "" },
		"ReportPath": func(b *BriefInput) { b.ReportPath = "" },
		"SliceJSON":  func(b *BriefInput) { b.SliceJSON = "" },
	} {
		in := baseBrief()
		edit(&in)
		if _, err := BuildBrief(in); err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("empty %s: %v", field, err)
		}
	}
}

func TestBriefNamesCheckCommand(t *testing.T) {
	checkBriefInstructionsOnlyForImplementer(t)
	for _, tc := range []struct{ name, command, want string }{
		{"configured", "go test -race ./...", "ork runs `go test -race ./...` and `slicer check` after the round, outside your sandbox."},
		{"empty", "", "ork runs `slicer check` after the round, outside your sandbox."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) { o.project = func(p *registry.Project) { p.CheckCommand = tc.command } })
			e := h.start()
			for _, req := range h.runner.requests() {
				_, brief, _ := strings.Cut(req.Brief, "\n# Brief\n")
				if req.Role == RoleImplementer {
					if !strings.Contains(brief, "## Checks ork runs") || !strings.Contains(brief, tc.want) {
						t.Errorf("implementer brief lacks %q", tc.want)
					}
				} else if strings.Contains(brief, "## Checks ork runs") {
					t.Errorf("%s brief includes implementer check instructions", req.Role)
				}
			}
			if err := e.Approve(context.Background()); err != nil {
				t.Fatal(err)
			}
			reqs := h.runner.requests()
			finish := reqs[len(reqs)-1]
			if finish.Role != RoleImplementer || !strings.Contains(finish.Brief, "## Finish step") {
				t.Fatal("last request must be the implementer finish step")
			}
			if strings.Contains(finish.Brief, "## Checks ork runs") {
				t.Error("finish brief includes checks that do not run after the finish step")
			}
		})
	}
}

func checkBriefInstructionsOnlyForImplementer(t *testing.T) {
	for _, role := range []string{RoleReviewer, RoleGate, "architect"} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Role = role
			in.CheckCommand = "go test -race ./..."
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(got, "## Checks ork runs") {
				t.Error("non-implementer brief includes check instructions")
			}
		})
	}
}

func TestUIPassBriefNamesTheRenderCommand(t *testing.T) {
	for _, tc := range []struct {
		name, tmpDir, want string
	}{
		{"TMPDir", "/state/process-temp", "/state/process-temp"},
		{"TempDir fallback", "", "'/state/role scratch/ui-1'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := baseBrief()
			in.Role = RoleUI
			in.TempDir = "/state/role scratch/ui-1"
			in.TMPDir = tc.tmpDir
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			want := "go test ./internal/cli -run TestRenderCLIViews -renders " + tc.want
			if !strings.Contains(got, want) {
				t.Fatalf("UI brief lacks render command %q", want)
			}
		})
	}
}

// The UI pass brief says that -renders alone writes five files and names the
// -render-fixture flag with the eight fixtures it adds (S552, adj 542).
func TestUIPassBriefNamesRenderFixtures(t *testing.T) {
	in := baseBrief()
	in.Role = RoleUI
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"This writes status.txt, status-summary.txt, status-summary-long.txt, show.txt and help.txt into your temp directory",
		"`-render-fixture NAME[,NAME...]`",
		"writes only NAME.txt for each name",
		"projects-set, projects-set-refusals, wave-start, status-queued-held, status-engine, summary-real, projects-setup and fake-reply",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("UI brief lacks %q", want)
		}
	}
	if strings.Contains(got, "This writes status.txt, show.txt and help.txt") {
		t.Error("UI brief still says -renders writes three files")
	}
	in.Role = RoleImplementer
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "-render-fixture") {
		t.Error("non-UI brief names -render-fixture")
	}
}

// The UI pass brief names the project's design doc from the registry field
// ui_doc, or tells the pass to find the project's own UI docs (S136).
func TestUIBriefNamesTheProjectDesignDoc(t *testing.T) {
	in := baseBrief()
	in.Role = RoleUI
	in.UIDoc = "docs/ui.md"
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "## UI design doc\n\nThe project's UI design doc is `docs/ui.md`.") {
		t.Errorf("brief does not name the doc:\n%s", got)
	}
	in.UIDoc = ""
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "## UI design doc\n\nThe registry names no UI design doc for this project. Find the project's own UI docs") || strings.Contains(got, "docs/ui.md") {
		t.Errorf("brief does not tell the pass to look:\n%s", got)
	}
	// Other roles get no such section.
	in = baseBrief()
	in.UIDoc = "docs/ui.md"
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "UI design doc") {
		t.Errorf("implementer brief names the UI doc:\n%s", got)
	}
}

func TestUIBriefSkillsOnlyForClaude(t *testing.T) {
	for _, tc := range []struct {
		name, role, vendor string
		skills             []string
		want               bool
	}{
		{"claude", RoleUI, "claude", []string{"tui-design", "frontend-design"}, true},
		{"unset", RoleUI, "claude", nil, false},
		{"empty", RoleUI, "claude", []string{}, false},
		{"codex", RoleUI, "codex", []string{"tui-design", "frontend-design"}, false},
		{"case-sensitive", RoleUI, "Claude", []string{"tui-design"}, false},
		{"implementer", RoleImplementer, "claude", []string{"tui-design"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := baseBrief()
			in.Role = tc.role
			in.Vendor = tc.vendor
			in.UIPassSkills = tc.skills
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			line := "## Skills\n\nLoad these skills for the UI pass: tui-design, frontend-design. If one is not available, say so in your report and go on.\n\n## UI design doc"
			if strings.Contains(got, line) != tc.want {
				t.Fatalf("skills line present=%v, want %v", strings.Contains(got, line), tc.want)
			}
			if !tc.want && (strings.Contains(got, "## Skills") || strings.Contains(got, "Load these skills") || strings.Contains(got, "tui-design") || strings.Contains(got, "frontend-design")) {
				t.Fatal("skills leaked to brief")
			}
		})
	}
}

func TestBriefFencesCheckCommandWithBackticks(t *testing.T) {
	for _, command := range []string{"echo `pwd`\ngo test ./...", "echo `pwd`", "go test ./...\nslicer check"} {
		assertCheckFence(t, command, "```")
	}
}

func TestBriefKeepsAPlainCheckCommandInline(t *testing.T) {
	in := baseBrief()
	in.CheckCommand = "go test -race ./..."
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "ork runs `go test -race ./...` and `slicer check` after the round, outside your sandbox.") {
		t.Fatal("plain check instructions changed")
	}
}

func TestBriefFenceGrowsWithTheCommand(t *testing.T) {
	assertCheckFence(t, "echo ```code```", "````")
}

func assertCheckFence(t *testing.T, command, fence string) {
	t.Helper()
	in := baseBrief()
	in.CheckCommand = command
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	want := "ork runs this command, then `slicer check`, after the round, outside your sandbox:\n\n" + fence + "\n" + command + "\n" + fence + "\n"
	if !strings.Contains(got, want) {
		t.Fatalf("brief lacks fenced instructions %q", want)
	}
	if strings.Contains(got, "ork runs `"+command+"`") {
		t.Fatal("command still wrapped inline")
	}
}

func yourFilesSection(t *testing.T, brief string) string {
	t.Helper()
	_, rest, ok := strings.Cut(brief, "## Your files\n\n")
	if !ok {
		t.Fatalf("no Your files section:\n%s", brief)
	}
	section, _, _ := strings.Cut(rest, "\n## ")
	return section
}

// S416: a cloud Claude UI pass runs on an allowlist, so its brief names the
// forms it may run. The list is the one the runner sends (CloudUIAllowlist).
func TestCloudUIPassDoneWhenListsItsAllowedForms(t *testing.T) {
	item := func(t *testing.T, in BriefInput) string {
		t.Helper()
		got, err := BuildBrief(in)
		if err != nil {
			t.Fatal(err)
		}
		_, rest, ok := strings.Cut(got, "## Done when\n")
		if !ok {
			t.Fatalf("no Done when section:\n%s", got)
		}
		done, _, _ := strings.Cut(rest, "\n## ")
		for _, line := range strings.Split(done, "\n") {
			if strings.Contains(line, "Allowed forms") {
				return line
			}
		}
		return ""
	}
	in := baseBrief()
	in.Role, in.Vendor, in.ReadOnly = RoleUI, "claude", true
	line := item(t, in)
	if line == "" {
		t.Fatal("cloud UI pass Done when has no Allowed forms item")
	}
	for _, entry := range CloudUIAllowlist() {
		if !strings.Contains(line, entry) {
			t.Errorf("Allowed forms lacks %q", entry)
		}
	}
	for _, want := range []string{"Skill", "Edit and Write under " + in.TempDir, "any other command is denied", "one plain command"} {
		if !strings.Contains(line, want) {
			t.Errorf("Allowed forms lacks %q", want)
		}
	}
	// No other role or vendor gets it: the reviewer's brief has no such item.
	for _, other := range []struct{ role, vendor string }{{RoleReviewer, "claude"}, {RoleUI, "codex"}, {RoleUI, "claude-local"}} {
		o := baseBrief()
		o.Role, o.Vendor, o.ReadOnly = other.role, other.vendor, true
		if got := item(t, o); got != "" {
			t.Errorf("%s %s brief has an Allowed forms item: %s", other.role, other.vendor, got)
		}
	}
}

func TestClaudeReadOnlyBriefHasNoPlanModeText(t *testing.T) {
	for _, role := range []string{RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck, RoleSpecDraft} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Vendor, in.Role, in.ReadOnly = "claude", role, true
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"plan mode", "ExitPlanMode"} {
				if strings.Contains(strings.ToLower(got), strings.ToLower(forbidden)) {
					t.Errorf("Claude brief contains %q", forbidden)
				}
			}
		})
	}
}

func TestReadOnlyReportSentenceRejectsDrift(t *testing.T) {
	got, err := readOnlyReportPrompt(RoleReviewer, "When you finish, write a report elsewhere.")
	if err == nil {
		t.Fatal("drifted report sentence accepted")
	}
	if !strings.Contains(err.Error(), RoleReviewer) {
		t.Errorf("error does not name role: %v", err)
	}
	if got != "" {
		t.Errorf("drift returned a usable prompt: %q", got)
	}
}

func requireOneReadOnlyReportRule(t *testing.T, in BriefInput) {
	t.Helper()
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "write your final report to") {
		t.Error("read-only brief still tells the role to write its report")
	}
	_, report, found := strings.Cut(got, "\n## Report\n")
	if !found {
		t.Fatal("missing Report section")
	}
	report, _, _ = strings.Cut(report, "\n# Brief\n")
	if !strings.Contains(report, "give your whole report as your final message and exit; ork saves it to "+in.ReportPath+".") {
		t.Error("Report section lacks the final-message rule with the report path")
	}
	if n := strings.Count(report, in.ReportPath); n != 1 {
		t.Errorf("Report section names the report path %d times, want 1", n)
	}
}

func TestReadOnlyBriefHasOneReportRule(t *testing.T) {
	for _, role := range []string{RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck, RoleSpecDraft} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Role, in.ReadOnly = role, true
			requireOneReadOnlyReportRule(t, in)
		})
	}
}

func TestReadOnlyPresetImplementerBriefHasOneReportRule(t *testing.T) {
	in := baseBrief()
	in.ReadOnly = true
	requireOneReadOnlyReportRule(t, in)
}

func TestWriteRoleBriefKeepsTheReportPathRule(t *testing.T) {
	in := baseBrief()
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "write your final report to "+in.ReportPath) {
		t.Fatal("write-role brief lost its report-path rule")
	}
}

func TestReadOnlyReportSentenceIsInEveryPrompt(t *testing.T) {
	for _, name := range roles.Names() {
		t.Run(name, func(t *testing.T) {
			prompt, err := roles.Prompt(name)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(prompt, originalReportSentence) {
				t.Fatal("prompt lacks the exact report sentence replaced for read-only roles")
			}
		})
	}
	t.Run("carve", func(t *testing.T) {
		if !strings.Contains(roles.CarvePrompt(), originalReportSentence) {
			t.Fatal("carve prompt lacks the exact report sentence replaced for read-only roles")
		}
	})
}

func TestReadOnlyBriefSaysTheReportIsTheFinalMessage(t *testing.T) {
	const pathLine = "Your report path: /state/runs/r1/implementer-1.md (ork saves your final message there; you cannot write it)\n"
	const finalLine = "Give your whole report as your final message, as plain text. Do not wrap it or any part of it in a code fence.\n"
	for _, role := range []string{RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Role = role
			in.ReadOnly = true
			got, err := BuildBrief(in)
			if err != nil {
				t.Fatal(err)
			}
			section := yourFilesSection(t, got)
			if !strings.Contains(section, pathLine) || !strings.Contains(section, finalLine) {
				t.Errorf("Your files section:\n%s", section)
			}
		})
	}
	got, err := BuildBrief(baseBrief())
	if err != nil {
		t.Fatal(err)
	}
	if section := yourFilesSection(t, got); strings.Contains(section, "you cannot write it") || strings.Contains(section, "code fence") || !strings.Contains(section, "Your report path: /state/runs/r1/implementer-1.md\n") {
		t.Errorf("an implementer brief changed:\n%s", section)
	}
}

func TestResumedBriefAsksToReReadTheTree(t *testing.T) {
	const body = "ork resumed your earlier session. The worktree may have changed since your last report. Before you judge or change anything, run git status and git diff again and read again every file you cite. Do not repeat an earlier finding, verdict or answer without checking it against the tree as it is now."
	in := baseBrief()
	in.Resumed = true
	in.Tree = " M a.go\n?? b.go\n"
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	want := "## Your files\n\nYour temp directory: /state/runs/r1/tmp/implementer-1\nYour report path: /state/runs/r1/implementer-1.md\n\n" +
		"## Resumed session\n\n" + body + "\n\ngit status --porcelain when this round started:\n\n```\n M a.go\n?? b.go\n```\n"
	if !strings.Contains(got, want) {
		t.Errorf("resumed section with a tree:\n%s", got)
	}

	in.Tree = ""
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	want = "## Resumed session\n\n" + body + "\n\nThe tree had no uncommitted changes when this round started.\n"
	if !strings.Contains(got, want) || strings.Contains(got, "git status --porcelain when this round started") {
		t.Errorf("resumed section with a clean tree:\n%s", got)
	}

	// A listing that holds a backtick run gets a longer fence.
	in.Tree = "?? a```b.go\n"
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "\n````\n?? a```b.go\n````\n") {
		t.Errorf("fence is not longer than the listing's backticks:\n%s", got)
	}

	// The section sits after Your files and before the round's own sections.
	in.Sections = []BriefSection{{Title: "Round", Body: "fix round 1"}}
	got, err = BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if files, resumed, round := strings.Index(got, "## Your files"), strings.Index(got, "## Resumed session"), strings.Index(got, "## Round"); files < 0 || files > resumed || resumed > round {
		t.Errorf("section order:\n%s", got)
	}
}

func TestFreshBriefHasNoResumedSection(t *testing.T) {
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck} {
		in := baseBrief()
		in.Role = role
		in.Tree = " M a.go\n" // a tree alone does not make a resumed brief
		got, err := BuildBrief(in)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "## Resumed session") || strings.Contains(got, "git status --porcelain when") {
			t.Errorf("%s: a fresh brief has a resumed section", role)
		}
	}
}

// Inspect only the generated checklist, never matching text elsewhere in the prompt.
func checklist(t *testing.T, in BriefInput) string {
	t.Helper()
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(got, "\n## Done when\n\n")
	if !ok {
		t.Fatal("missing Done when section")
	}
	section, _, _ := strings.Cut(rest, "\n\n## ")
	return section
}

func requireChecklistText(t *testing.T, in BriefInput, wants ...string) {
	t.Helper()
	got := checklist(t, in)
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("checklist lacks %q", want)
		}
	}
}

func TestEveryRoleBriefHasADoneWhenSection(t *testing.T) {
	for _, role := range []string{RoleImplementer, RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Role = role
			got := checklist(t, in)
			if strings.Count(got, "- [ ] ") < 3 {
				t.Fatal("fewer than three checklist items")
			}
			if len(strings.Split(got, "\n")) >= 40 {
				t.Fatal("checklist reaches 40 lines")
			}
		})
	}
}

func TestGrokReadOnlyDoneWhenNamesTheFormsThatRun(t *testing.T) {
	for _, role := range []string{RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Role, in.Vendor = role, "grok"
			in.Allow = []string{"Bash(unverified *)"}
			requireChecklistText(t, in, "git status --short", "git diff -- PATH", "git diff --check -- PATH", "read_file", "grep", "list_dir", "one command per call", "&&", ";", "pipe", "echo", "redirect", "python3 -c", "needs approval ends the run", "loses the report")
			got := checklist(t, in)
			if !strings.Contains(got, "Use one command per call. Do not use &&, ;, a pipe, echo, a redirect or python3 -c.") {
				t.Error("Grok lacks the single-command restrictions")
			}
			if strings.Contains(got, "Bash(") || strings.Contains(got, "Allowed forms") {
				t.Errorf("Grok got Allow entries: %s", got)
			}
		})
	}
}

// A grok implementer runs in dontAsk mode with allow rules (S305, adj 373): the
// checklist keeps the allowed forms and adds what grok does with them.
func TestGrokWriteDoneWhenNamesTheDontAskLimits(t *testing.T) {
	for _, vendor := range []string{"grok", "grok-local"} {
		t.Run(vendor, func(t *testing.T) {
			in := baseBrief()
			in.Role, in.Vendor, in.ReadOnly = RoleImplementer, vendor, false
			in.Allow = DefaultImplementerAllowlist("go test ./...")
			got := checklist(t, in)
			for _, want := range []string{
				"Allowed forms, verbatim: " + strings.Join(in.Allow, "; "),
				"Grok dontAsk mode: change files with the write and search_replace tools only",
				"Use one command per call. Do not use &&, ;, a pipe, echo, a redirect or python3 -c.",
				"A command that needs approval ends the run",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("Grok write checklist lacks %q:\n%s", want, got)
				}
			}
			for _, bad := range []string{"Grok plan mode", "You may only read the tree", "sed -i"} {
				if strings.Contains(got, bad) {
					t.Errorf("Grok write checklist has %q", bad)
				}
			}
		})
	}
}

func TestClaudeAndCodexReadOnlyDoneWhen(t *testing.T) {
	wantByRole := map[string]string{
		RoleReviewer:  "- [ ] You may only read the tree; no git or tracking writes. Give the whole report as your final message when the report path is read-only.\n- [ ] Each [fix] has file:line and a reproduction or a test; each [later] has proposed Importance, Urgency and Effort scores (1 to 3). Apply owner answers and keep findings within the slice.\n- [ ] A tracking step denied to the implementer is a Deviation for the orchestrator, never a [fix]: one untagged line under section (C), not a finding or a reason to start a fix round.\n- [ ] Complete sections (A)-(D), marking findings CONFIRMED or PLAUSIBLE; end with the exact unnumbered heading ## Owner needed and questions or none. In gate mode use the gate's first line and one evidence line per criterion; put denied tracking in a Deviation for the orchestrator evidence line.\n- [ ] Report blocked checks with the verbatim command and denial text, apart from test counts; never claim an unrun check passed.\n",
		RoleGate:      "- [ ] You may only read the tree; no git or tracking writes. Give the whole report as your final message when the report path is read-only.\n- [ ] Each [fix] has file:line and a reproduction or a test; each [later] has proposed Importance, Urgency and Effort scores (1 to 3). Apply owner answers and keep findings within the slice.\n- [ ] Report first line exactly VERDICT: PASS or VERDICT: FAIL, with nothing else on that line; one evidence line per criterion. PASS requires resolved earlier blockers with proving tests, no [fix], no blocker owner finding, tests green, and docs match the code; otherwise FAIL.\n- [ ] For a tracking step denied to the implementer, add one Deviation for the orchestrator evidence line beside the criterion lines, never a [fix]; it is not a finding and starts no fix round.\n- [ ] Include the exact standalone line Tests: N passed, M failed with integer counts of tests actually run and nothing after the counts; end with the exact unnumbered heading ## Owner needed and questions or none.\n- [ ] Report blocked checks with the verbatim command and denial text, apart from test counts; never claim an unrun check passed.\n",
		RoleArchitect: "- [ ] You may only read the tree; no git or tracking writes. Give the whole report as your final message when the report path is read-only.\n- [ ] Support placement, ownership, rollback, test-shape and direction findings with file:line and CONFIRMED or PLAUSIBLE; separate change-now from later. On second check, mark every earlier finding CONFIRMED-FIXED, PARTIAL or NOT-FIXED with evidence.\n- [ ] End the review with blockers or no blockers, then the exact unnumbered heading ## Owner needed with questions or none; in gate mode use its first line and evidence per criterion.\n- [ ] Name blocked checks and their verbatim command and denial text; do not claim unrun checks.\n",
		RoleUI:        "- [ ] You may only read the tree; no git or tracking writes. Give the whole report as your final message when the report path is read-only.\n- [ ] Complete sections (A)-(D): findings name the view, size, visible defect, file:line, concrete edit and CONFIRMED or PLAUSIBLE. Produce the required renders or explain each blocked view; list the files produced.\n- [ ] End with the exact unnumbered heading ## Owner needed and questions or none; give no verdict. Each [later] has proposed Importance, Urgency and Effort scores (1 to 3).\n- [ ] Name checks or views you could not run or see, with the verbatim command and denial text when blocked.\n",
		RoleSpecCheck: "- [ ] You may only read the tree; no git or tracking writes. Give the whole report as your final message when the report path is read-only.\n- [ ] Give one ## Slice ID section per slice in order, with Ready or structured owner questions. Only an unresolved owner decision is a question; close code-readable gaps yourself and respect settled decisions.\n- [ ] End with the exact unnumbered heading ## Owner needed and none beneath it; questions belong under their slices. Use that final section only for a whole-check blocker.\n- [ ] State any unreadable slice or blocked read with its verbatim command and denial text; never invent readiness.\n",
	}
	for _, vendor := range []string{"claude", "codex"} {
		for _, role := range []string{RoleReviewer, RoleGate, RoleArchitect, RoleUI, RoleSpecCheck} {
			in := baseBrief()
			in.Role, in.Vendor = role, vendor
			got := checklist(t, in)
			if vendor == "claude" && role == RoleUI {
				// S416: a cloud Claude UI pass adds one Allowed forms item;
				// TestCloudUIPassDoneWhenListsItsAllowedForms checks its text.
				var kept []string
				for _, line := range strings.SplitAfter(got, "\n") {
					if !strings.HasPrefix(line, "- [ ] Allowed forms, verbatim: ") {
						kept = append(kept, line)
					}
				}
				if len(kept) == strings.Count(got, "\n")+1 {
					t.Error("claude ui checklist lacks the Allowed forms item")
				}
				got = strings.Join(kept, "")
			}
			want := wantByRole[role]
			want = strings.Replace(want, "\n", "\n- [ ] Run one plain command per call: no pipes, && or ; chains, redirects, command substitution, shell variables, env prefixes or cd. Use literal absolute paths with git -C and go -C.\n", 1)
			if got != want {
				t.Errorf("%s %s checklist changed", vendor, role)
			}
			if !strings.HasPrefix(got, "- [ ] You may only read the tree; no git or tracking writes. Give the whole report as your final message when the report path is read-only.\n") {
				t.Errorf("read-only instruction changed: %s", got)
			}
		}
	}
}

func TestImplementerDoneWhenNamesTheExactReportLines(t *testing.T) {
	requireChecklistText(t, baseBrief(), "Tests: N passed, M failed", "## Owner needed", "nothing may follow the counts", "skipped tests go under item 6", "report parts in the prompt", "docs")
}

func TestImplementerDoneWhenListsTheSlicesFailingTestsAndChecks(t *testing.T) {
	in := baseBrief()
	in.SliceJSON = `{"slice":{"sections":[{"heading":"Failing tests","body":"- TestA\n- TestB"},{"heading":"Check","body":"- go test ./alpha\n- go test ./beta\n- [HUMAN CHECK] look"}]}}`
	requireChecklistText(t, in, "TestA: red before the code, green after", "TestB: red before the code, green after", "go test ./alpha", "go test ./beta")
	if strings.Contains(checklist(t, in), "HUMAN CHECK") {
		t.Fatal("human check required in sandbox")
	}
	requireChecklistText(t, baseBrief(), "Failing tests: the slice lists none", "Check: the slice lists none")
}

func TestImplementerDoneWhenShowsTheAllowedCommands(t *testing.T) {
	in := baseBrief()
	in.Allow = DefaultImplementerAllowlist("python3 -m unittest")
	requireChecklistText(t, in, append(in.Allow, "any other command is denied", "no pipes, no cd, no env prefix unless an entry allows it")...)
	in.Allow = []string{"Bash(go  test*)"}
	requireChecklistText(t, in, "Bash(go  test*)")
	in.ReadOnly = true
	requireChecklistText(t, in, "may only read")
}

// S368: an owner answer that changes the slice's text (S241: "tighten" a Check
// line) is applied to the slice before the Check lines run.
func TestImplementerBriefSaysToApplySpecAnswersFirst(t *testing.T) {
	h := newHarness(t)
	h.cfg.Decisions = "Owner decisions (2026-11-16): Check question -> tighten: drop the `--yes` flags from the Check."
	h.start()
	implementers, reviewers := 0, 0
	for _, r := range h.runner.requests() {
		decisions := strings.Index(r.Brief, "## Owner decisions\n\n")
		apply := strings.Index(r.Brief, "## Apply the owner's spec answers first\n\n")
		confirm := strings.Index(r.Brief, "## Confirm the owner's spec answers were applied\n\n")
		switch r.Role {
		case RoleImplementer:
			implementers++
			if apply < 0 || decisions < 0 || apply < decisions {
				t.Errorf("implementer brief: apply heading at %d, decisions at %d", apply, decisions)
				continue
			}
			_, body, _ := strings.Cut(r.Brief[apply:], "\n\n")
			body, _, _ = strings.Cut(body, "\n\n## ")
			for _, want := range []string{"first step", "slicer edit " + item + " --section", "--render --strict", "Spec edits", "dropped backticked flag is recorded as not a command and is not run", "any other dropped command is still run by ork and can pause the run"} {
				if !strings.Contains(body, want) {
					t.Errorf("apply section lacks %q: %s", want, body)
				}
			}
		case RoleReviewer:
			reviewers++
			if confirm < 0 || apply >= 0 {
				t.Errorf("reviewer brief: confirm heading at %d, apply heading at %d", confirm, apply)
			}
		default:
			if apply >= 0 || confirm >= 0 {
				t.Errorf("%s brief has an apply or confirm heading", r.Role)
			}
		}
	}
	if implementers == 0 || reviewers == 0 {
		t.Fatalf("implementer rounds %d, reviewer rounds %d", implementers, reviewers)
	}
}

func TestARunWithoutDecisionsHasNoApplyAnswersSection(t *testing.T) {
	h := newHarness(t)
	h.start()
	for _, r := range h.runner.requests() {
		if strings.Contains(r.Brief, "spec answers") {
			t.Errorf("%s brief names spec answers", r.Role)
		}
	}
}

func TestOwnerDecisionKeyChecksReachBriefs(t *testing.T) {
	for _, state := range []string{"not found", "decided", "open", "closed"} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t, func(o *harnessOpts) {
				o.project = func(p *registry.Project) { p.Adj = true }
				o.routesFor = func(p, w string) []clitest.Route {
					found := `{}`
					if state != "not found" {
						found = fmt.Sprintf(`{%q:[{"id":42,"key":"brief.target"}]}`, state)
					}
					return []clitest.Route{
						{Tool: "adj", Args: []string{"contract", "--json", "--root", p}, Stdout: `{"contract":"adj-json v1","version":1}`},
						{Tool: "adj", Args: []string{"check", "brief.target", "--json", "--root", p}, Stdout: found},
						{Tool: "adj", Args: []string{"check"}, Prefix: true, Stdout: `{}`},
					}
				}
			})
			h.deps.Adj = adjcli.Client{Bin: h.fake.Bin("adj")}
			h.cfg.Decisions = "Unrelated [brief.outside].\n\nOwner decisions (2026-11-25):\nAlways missing -> x [brief.missing].\nTarget -> y [brief.target].\nReused -> z [brief.reused, adj 7].\n\nOther paragraph [brief.after]."
			h.start()
			for _, r := range h.runner.requests() {
				_, body, ok := strings.Cut(r.Brief, "## Owner decisions\n\n")
				if !ok {
					t.Fatal("brief lacks owner decisions section")
				}
				body, _, _ = strings.Cut(body, "\n\n## ")
				if !strings.Contains(body, "adj check brief.missing: not found") {
					t.Fatal("brief lacks missing-key annotation")
				}
				if got := strings.Contains(body, "adj check brief.target: not found"); got != (state == "not found") {
					t.Fatalf("target annotation = %t for %s", got, state)
				}
				for _, key := range []string{"brief.outside", "brief.reused", "brief.after"} {
					if strings.Contains(body, "adj check "+key+":") {
						t.Fatalf("unexpected annotation for %s", key)
					}
				}
			}
			for _, call := range h.fake.CallsTo("adj") {
				if len(call.Args) > 0 && call.Args[0] == "contract" {
					continue
				}
				if len(call.Args) < 2 || (call.Args[1] != "brief.missing" && call.Args[1] != "brief.target") {
					t.Fatalf("unexpected adj call: %v", call.Args)
				}
			}
		})
	}
}

const ownerDecisionKeyText = "Owner decisions (2026-11-25): Which store? -> memory: rebuilt [brief.target]."

func TestOwnerDecisionKeysAreNotCheckedWhenAdjIsOff(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.project = func(p *registry.Project) { p.Adj = false }
		o.routes = []clitest.Route{{Tool: "adj", Args: []string{"check"}, Prefix: true, Exit: 1}}
	})
	h.deps.Adj = adjcli.Client{Bin: h.fake.Bin("adj")}
	h.cfg.Decisions = ownerDecisionKeyText
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	if n := len(h.fake.CallsTo("adj")); n != 0 {
		t.Fatalf("%d adj calls with adj off", n)
	}
	for _, r := range h.runner.requests() {
		if strings.Contains(r.Brief, "adj check brief.target") {
			t.Errorf("%s brief names an adj check", r.Role)
		}
	}
}

func TestAFailedOwnerDecisionKeyCheckAddsASoftLine(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) {
		o.project = func(p *registry.Project) { p.Adj = true }
		o.routesFor = func(p, w string) []clitest.Route {
			return []clitest.Route{
				{Tool: "adj", Args: []string{"contract", "--json", "--root", p}, Stdout: `{"contract":"adj-json v1","version":1}`},
				{Tool: "adj", Args: []string{"check"}, Prefix: true, Exit: 1},
			}
		}
	})
	h.deps.Adj = adjcli.Client{Bin: h.fake.Bin("adj")}
	h.cfg.Decisions = ownerDecisionKeyText
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	reqs := h.runner.requests()
	if len(reqs) == 0 {
		t.Fatal("no role ran")
	}
	for _, r := range reqs {
		if !strings.Contains(r.Brief, "adj check brief.target: could not be read") {
			t.Errorf("%s brief lacks the soft line", r.Role)
		}
		if strings.Contains(r.Brief, "adj check brief.target: not found") {
			t.Errorf("%s brief says not found for an unreadable check", r.Role)
		}
	}
}

func TestDoneWhenSaysWhatToWriteWhenACheckCannotRun(t *testing.T) {
	requireChecklistText(t, baseBrief(), "denied or impossible check", "item 6", "verbatim command and denial text", "counts of tests actually run", "never a made-up zero", "ork re-runs the checks outside the sandbox")
}

func TestFixRoundDoneWhenListsEachFinding(t *testing.T) {
	for _, title := range []string{"Review findings", "Gate FAIL", "Architect findings"} {
		t.Run(title, func(t *testing.T) {
			in := baseBrief()
			in.Sections = []BriefSection{{Title: title, Body: "- [fix] alpha.go:1 loses error\n  Fix: return it\n- [later] polish\n- [fix] beta.go:2 races"}}
			requireChecklistText(t, in, "alpha.go:1 loses error: proving test or a stated reason it needs none", "beta.go:2 races: proving test or a stated reason it needs none")
			if strings.Contains(checklist(t, in), "polish") {
				t.Fatal("later finding included")
			}
		})
	}
}

func TestDoneWhenSeparatesBulletsAcrossProse(t *testing.T) {
	for _, tc := range []struct{ heading, first, second string }{
		{"Failing tests", "- [ ] First item continued: red before the code, green after", "- [ ] Second item: red before the code, green after"},
		{"Check", "- [ ] Run or report blocked: First item continued\n", "- [ ] Run or report blocked: Second item"},
	} {
		t.Run(tc.heading, func(t *testing.T) {
			in := baseBrief()
			raw, err := json.Marshal(map[string]any{"slice": map[string]any{"sections": []map[string]string{{"heading": tc.heading, "body": "- First item\n  continued\nIn other_test.go:\n  Detached prose\n- Second item"}}}})
			if err != nil {
				t.Fatal(err)
			}
			in.SliceJSON = string(raw)
			got := checklist(t, in)
			if !strings.Contains(got, tc.first) {
				t.Errorf("first bullet does not retain only its indented continuation: %s", got)
			}
			if !strings.Contains(got, tc.second) {
				t.Errorf("second bullet missing: %s", got)
			}
			for _, unwanted := range []string{"In other_test.go:", "Detached prose"} {
				if strings.Contains(got, unwanted) {
					t.Errorf("prose leaked into checklist: %q", unwanted)
				}
			}
		})
	}
}

// directionItem is one open item of the direction fixture. Files are the paths
// its `slicer show` lists; an item with no spec (noSpec) has none and is never
// shown.
type directionItem struct {
	id, title, status string
	deps, files       []string
	noSpec            bool
	showFails         bool
}

const directionGoals = `{"records":{"goal":[{"id":"C01","kind":"goal","title":"One clear owner for each relay step","status":"active"},{"id":"C02","kind":"goal","title":"Keep the outer session small","status":"active"}],"non_goal":[{"id":"C05","kind":"non_goal","title":"No unattended runs","status":"active"},{"id":"C06","kind":"non_goal","title":"No tracking state of its own","status":"active"}]}}`

// directionFixture installs fake slicer and git answers for one review of S383
// whose diff changes the given paths, and returns the engine and run record to
// build the Direction section from. With listFails, `slicer list` exits non-zero.
func directionFixture(t *testing.T, items []directionItem, changed []string, listFails bool, why ...string) (*Engine, runrec.Record) {
	t.Helper()
	P := t.TempDir()
	W := filepath.Join(P, ".worktrees", "S383")
	var rows []string
	var routes []clitest.Route
	for _, it := range items {
		rows = append(rows, fmt.Sprintf(`{"id":%q,"title":%q,"status":%q,"has_slice":%t,"depends_on":%s,"fields":{}}`, it.id, it.title, it.status, !it.noSpec, jsonList(it.deps)))
		if it.noSpec || it.id == "S383" {
			continue
		}
		show := clitest.Route{Tool: "slicer", Args: []string{"show", it.id, "--json", "--lean", "--root", W}, Stdout: fmt.Sprintf(`{"id":%q,"title":%q,"status":%q,"paths":%s}`, it.id, it.title, it.status, jsonList(it.files))}
		if it.showFails {
			show.Stdout, show.Exit = "", 1
		}
		routes = append(routes, show)
	}
	body := ""
	if len(why) > 0 {
		body = why[0]
	}
	routes = append(routes, clitest.Route{Tool: "slicer", Args: []string{"show", "S383", "--json", "--lean", "--root", W}, Stdout: fmt.Sprintf(`{"id":"S383","slice":{"sections":[{"heading":"Why","body":%q},{"heading":"Implement","body":"unrelated title must not match"}]}}`, body)})
	list := clitest.Route{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", W}, Stdout: "[" + strings.Join(rows, ",") + "]"}
	if listFails {
		list.Stdout, list.Exit = "", 1
	}
	routes = append(routes, list,
		clitest.Route{Tool: "slicer", Args: []string{"goals", "--json", "--lean", "--root", W}, Stdout: directionGoals},
		clitest.Route{Tool: "git", Args: []string{"-C", P, "symbolic-ref", "--short", "HEAD"}, Stdout: "main\n"},
		clitest.Route{Tool: "git", Args: []string{"-C", W, "merge-base", "main", "HEAD"}, Stdout: "abc123\n"},
		clitest.Route{Tool: "git", Args: []string{"-C", W, "diff", "--name-only", "-z", "--no-renames", "--no-relative", "abc123"}, Stdout: strings.Join(changed, "\x00") + "\x00"},
		clitest.Route{Tool: "git", Args: []string{"-C", W, "ls-files", "--others", "--exclude-standard", "-z", "--full-name"}},
	)
	fake := clitest.Install(t, routes...)
	e := &Engine{d: Deps{Slicer: slicercli.Client{Bin: fake.Bin("slicer")}, GitBin: fake.Bin("git")}, proj: registry.Project{Path: P}}
	return e, runrec.Record{Item: "S383", Worktree: W}
}

func jsonList(s []string) string {
	if s == nil {
		return "[]"
	}
	b, _ := json.Marshal(s)
	return string(b)
}

func directionFixtureItems() []directionItem {
	return []directionItem{
		{id: "S383", title: "This slice itself", status: "started", files: []string{"internal/engine/brief.go"}},
		{id: "S10", title: "Touch the brief file", status: "open", deps: []string{"S09", "S08"}, files: []string{"internal/engine/brief.go", "docs/x.md"}},
		{id: "S11", title: "Touch the engine directory", status: "started", files: []string{"internal/engine"}},
		{id: "S12", title: "Unrelated tui work", status: "open", files: []string{"internal/tui/model.go"}},
		{id: "S13", title: "Not specified yet", status: "open", noSpec: true},
		{id: "S14", title: "Already done", status: "done", files: []string{"internal/engine/brief.go"}},
		{id: "S15", title: "Already retired", status: "retired", noSpec: true},
		{id: "S16", title: "Files cannot be read", status: "open", showFails: true},
	}
}

// splitDirection cuts a Direction body at the heading of the other open items.
func splitDirection(t *testing.T, body string) (touching, rest string) {
	t.Helper()
	i := strings.Index(body, "Other open items")
	if i < 0 {
		t.Fatalf("no Other open items heading in:\n%s", body)
	}
	return body[:i], body[i:]
}

func TestDirectionDigestNamesOverlappingItems(t *testing.T) {
	e, rec := directionFixture(t, directionFixtureItems(), []string{"internal/engine/brief.go"}, false)
	section := e.directionSection(context.Background(), rec)
	if section.Title != "Direction" {
		t.Fatalf("title %q", section.Title)
	}
	touching, rest := splitDirection(t, section.Body)
	// S10 names the changed file; S11 names its directory.
	for _, want := range []string{"S10", "Touch the brief file", "open", "S09", "S08", "S11", "Touch the engine directory", "started"} {
		if !strings.Contains(touching, want) {
			t.Errorf("touching part lacks %q:\n%s", want, touching)
		}
	}
	for _, id := range []string{"S12", "S13", "S14", "S15", "S16", "S383"} {
		if strings.Contains(touching, id+" ") {
			t.Errorf("touching part names %s:\n%s", id, touching)
		}
	}
	// An item whose Files could not be read is not claimed to overlap, and the
	// digest says so.
	if !strings.Contains(rest, "S16") || !strings.Contains(section.Body, "Files unread for 1 item") {
		t.Errorf("an item with unreadable Files is not reported:\n%s", section.Body)
	}
}

func TestDirectionDigestLikelyDuplicates(t *testing.T) {
	const title = "name -render-fixture and the fixture list in the UI pass brief's CLI text renders section"
	items := []directionItem{
		{id: "S550", title: title, status: "open", noSpec: true},
		{id: "S383", title: title, status: "started"},
		{id: "S551", title: title, status: "done"},
		{id: "S552", title: title, status: "retired"},
		{id: "S553", title: "unrelated title must not match", status: "open", noSpec: true},
	}
	for _, tc := range []struct {
		name, why string
		want      bool
	}{
		{"match", "Evidence: S550 \"" + title + "\" duplicates the requested scope. Extra prose does not dilute containment.", true},
		{"below", "fixture unrelated", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, rec := directionFixture(t, items, nil, false, tc.why)
			body := e.directionSection(context.Background(), rec).Body
			start := strings.Index(body, "Likely duplicates")
			end := strings.Index(body, "Open items that touch")
			if start < 0 || end < start {
				t.Fatalf("missing likely duplicate group:\n%s", body)
			}
			if got := strings.Contains(body[start:end], "- S550 "+title+"\n"); got != tc.want {
				t.Fatalf("likely duplicate S550 = %v, want %v:\n%s", got, tc.want, body)
			}
			if strings.Contains(body[start:end], "S553") || strings.Contains(body, "S383") || strings.Contains(body, "S551") || strings.Contains(body, "S552") {
				t.Fatalf("unmatched or excluded item in duplicate output:\n%s", body)
			}
			if strings.Count(body, "- S550 ") != 1 || !strings.Contains(body, "- S553 unrelated title must not match\n") || !strings.Contains(body, "0 open items left out of 2.") {
				t.Fatalf("items duplicated, lost, or miscounted:\n%s", body)
			}
		})
	}
}

func TestDirectionDigestListsTheRestByTitle(t *testing.T) {
	e, rec := directionFixture(t, directionFixtureItems(), []string{"internal/engine/brief.go"}, false)
	section := e.directionSection(context.Background(), rec)
	_, rest := splitDirection(t, section.Body)
	for _, line := range []string{"- S12 Unrelated tui work\n", "- S13 Not specified yet\n"} {
		if !strings.Contains(rest, line) {
			t.Errorf("rest lacks the exact line %q:\n%s", line, rest)
		}
	}
	for _, gone := range []string{"S14", "Already done", "S15", "Already retired"} {
		if strings.Contains(section.Body, gone) {
			t.Errorf("digest holds a done or retired item (%q):\n%s", gone, section.Body)
		}
	}
	// The slice under review is not its own direction.
	if strings.Contains(section.Body, "S383") || strings.Contains(section.Body, "This slice itself") {
		t.Errorf("digest names the slice under review:\n%s", section.Body)
	}
}

func TestDirectionDigestHoldsTheGoals(t *testing.T) {
	e, rec := directionFixture(t, directionFixtureItems(), []string{"internal/engine/brief.go"}, false)
	body := e.directionSection(context.Background(), rec).Body
	for _, title := range []string{"One clear owner for each relay step", "Keep the outer session small", "No unattended runs", "No tracking state of its own"} {
		if !strings.Contains(body, title) {
			t.Errorf("digest lacks goal %q:\n%s", title, body)
		}
	}
}

func TestDirectionDigestIsBounded(t *testing.T) {
	var items []directionItem
	for i := 1; i <= 300; i++ {
		items = append(items, directionItem{id: fmt.Sprintf("S%d", 1000+i), title: fmt.Sprintf("Open item number %d with a title of ordinary length", i), status: "open", noSpec: true})
	}
	e, rec := directionFixture(t, items, []string{"internal/engine/brief.go"}, false)
	body := e.directionSection(context.Background(), rec).Body
	if len(body) >= 8000 {
		t.Fatalf("digest is %d bytes, want under 8000", len(body))
	}
	m := regexp.MustCompile(`(\d+) open items? left out`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no line says how many items were left out:\n%s", body)
	}
	left, _ := strconv.Atoi(m[1])
	listed := strings.Count(body, "\n- S1")
	if left == 0 || listed+left != 300 {
		t.Errorf("%d listed and %d left out, want 300 in all:\n%s", listed, left, body)
	}
}

func TestDirectionDigestFailureKeepsTheOldRead(t *testing.T) {
	e, rec := directionFixture(t, directionFixtureItems(), []string{"internal/engine/brief.go"}, true)
	section := e.directionSection(context.Background(), rec)
	if section.Title != "Direction" {
		t.Fatalf("title %q: the section is not in the brief", section.Title)
	}
	// The failed call's own error text names the command, so look for the
	// instruction, which backticks both commands.
	for _, want := range []string{"run `slicer goals --json --lean` and `slicer list --json --lean`"} {
		if !strings.Contains(section.Body, want) {
			t.Errorf("fallback lacks %q:\n%s", want, section.Body)
		}
	}
	for _, digest := range []string{"One clear owner", "Touch the brief file", "S12"} {
		if strings.Contains(section.Body, digest) {
			t.Errorf("fallback holds digest text %q:\n%s", digest, section.Body)
		}
	}
}

// The reviewer and the architect get the section in a real round; the
// implementer does not (S383, Not in this slice: the gate and implementer briefs).
func TestReviewerAndArchitectBriefsCarryTheDirectionSection(t *testing.T) {
	h := architectHarness(t, func(o *harnessOpts) {
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{
				{Tool: "slicer", Args: []string{"goals", "--json", "--lean", "--root", W}, Stdout: directionGoals},
				{Tool: "slicer", Args: []string{"list", "--json", "--lean", "--root", W}, Stdout: `[{"id":"S99","title":"Later work","status":"open","has_slice":false,"fields":{}}]`},
				{Tool: "git", Args: []string{"-C", W, "merge-base", "main", "HEAD"}, Stdout: "abc123\n"},
				{Tool: "git", Args: []string{"-C", W, "diff", "--name-only", "-z", "--no-renames", "--no-relative", "abc123"}, Stdout: "internal/engine/brief.go\x00"},
				{Tool: "git", Args: []string{"-C", W, "ls-files", "--others", "--exclude-standard", "-z", "--full-name"}},
			}
		}
	})
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	seen := map[string]bool{}
	for _, req := range h.runner.requests() {
		carries := strings.Contains(req.Brief, "## Direction\n") && strings.Contains(req.Brief, "No unattended runs") && strings.Contains(req.Brief, "- S99 Later work")
		switch req.Role {
		case RoleReviewer, RoleArchitect:
			seen[req.Role] = true
			if !carries {
				t.Errorf("%s brief lacks the Direction section", req.Role)
			}
		case RoleImplementer:
			if strings.Contains(req.Brief, "## Direction") {
				t.Errorf("%s brief holds a Direction section", req.Role)
			}
		}
	}
	if !seen[RoleReviewer] || !seen[RoleArchitect] {
		t.Fatalf("roles run: %v", h.runner.roles())
	}
}

func TestResumedBriefRepeatsDoneWhen(t *testing.T) {
	in := baseBrief()
	in.Resumed = true
	got, err := BuildBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(got, "\n## Done when\n") <= strings.Index(got, "\n## Resumed session\n") {
		t.Fatal("Done when missing after Resumed session")
	}
	requireChecklistText(t, in, "First re-run git status and git diff")
}

func TestFinishStepDoneWhen(t *testing.T) {
	in := baseBrief()
	in.Finish = true
	in.Sections = []BriefSection{{Title: "Finish step", Body: "1. `slicer note S04 --text \"outcome\" --render`\n2. `slicer handoff S04 --render --check`"}}
	got := checklist(t, in)
	requireChecklistText(t, in, "slicer note S04", "slicer handoff S04", "in order", "denied", "## Owner needed", "orchestrator", "engine.denials", "not retried")
	if strings.Index(got, "slicer note S04") > strings.Index(got, "slicer handoff S04") {
		t.Fatal("finish commands out of order")
	}
	if strings.Contains(got, "Tests:") {
		t.Fatal("finish checklist requires Tests line")
	}
}

func TestReviewerAndGateDoneWhen(t *testing.T) {
	for _, role := range []string{RoleReviewer, RoleGate} {
		t.Run(role, func(t *testing.T) {
			in := baseBrief()
			in.Role = role
			in.ReadOnly = true
			requireChecklistText(t, in, "file:line", "reproduction or a test", "Deviation", "never a [fix]", "Importance", "Urgency", "Effort", "## Owner needed", "may only read")
			if role == RoleReviewer {
				requireChecklistText(t, in, "untagged line under section (C)")
			} else {
				requireChecklistText(t, in, "first line exactly VERDICT: PASS or VERDICT: FAIL", "one evidence line per criterion", "Tests: N passed, M failed", "Deviation for the orchestrator", "no [fix]", "no blocker", "tests green", "docs match")
			}
		})
	}
}
