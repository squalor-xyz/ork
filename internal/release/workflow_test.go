// SPDX-License-Identifier: GPL-3.0-or-later

package release

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// reporter is the part of *testing.T the guards use, so a mutation test can
// run a guard on a changed copy and record its failures.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

type workflowCheck struct {
	name  string
	check func(reporter, string)
}

// This intentionally guards the workflow as text without a YAML dependency.
// The checks run on normalize(workflow), which gives the text one canonical
// layout: two spaces per level, no comment lines or blank lines outside block
// scalars, and `tags: ['v*']`.
var workflowChecks = []workflowCheck{
	{"triggers", func(t reporter, s string) {
		require(t, s, "on:\n  push:\n    tags: ['v*']\n  workflow_dispatch:")
	}},
	{"pinned_actions", func(t reporter, s string) {
		seen := map[string]bool{}
		pin := regexp.MustCompile(`^actions/(checkout|setup-go|upload-artifact|download-artifact)@[0-9a-f]{40} # v[0-9.]+$`)
		for _, line := range strings.Split(s, "\n") {
			if _, action, ok := strings.Cut(line, "uses:"); ok {
				action = strings.TrimSpace(action)
				if !pin.MatchString(action) {
					t.Errorf("unapproved or unpinned action: %s", action)
				}
				seen[strings.Split(action, "@")[0]] = true
			}
		}
		if len(seen) != 4 {
			t.Errorf("got %d distinct actions, want exactly four", len(seen))
		}
	}},
	{"targets", func(t reporter, s string) {
		build := job(t, s, "build")
		require(t, build, "matrix:\n        include:\n          - os: darwin\n            arch: arm64\n          - os: darwin\n            arch: amd64\n          - os: linux\n            arch: amd64\n    steps:")
	}},
	{"version_and_vcs", func(t reporter, s string) {
		require(t, job(t, s, "build"), `go build -trimpath -ldflags "-X main.version=$TAG" -o ork ./cmd/ork`, `TAG="dev-$(git rev-parse --short HEAD)"`, `TAG="$GITHUB_REF_NAME"`)
		if strings.Contains(s, "-buildvcs=false") {
			t.Errorf("VCS stamping disabled")
		}
		if strings.Count(s, "go-version-file: go.mod") != 2 {
			t.Errorf("test and build must use go.mod")
		}
	}},
	{"job_order", func(t reporter, s string) {
		require(t, job(t, s, "build"), "needs: test")
		require(t, job(t, s, "publish"), "needs: build")
		require(t, job(t, s, "test"), "os: [ubuntu-latest, macos-latest]", "go test -race ./...", "go vet ./...", "gofmt -l .")
	}},
	{"permissions", func(t reporter, s string) {
		require(t, s, "\npermissions:\n  contents: read\n")
		if strings.Count(s, "contents: write") != 1 {
			t.Errorf("want one contents write grant")
		}
		require(t, job(t, s, "publish"), "permissions:\n      contents: write")
		for _, name := range []string{"test", "build"} {
			if strings.Contains(job(t, s, name), "permissions:") {
				t.Errorf("%s must inherit read-only permissions", name)
			}
		}
	}},
	{"persist_credentials", func(t reporter, s string) {
		for _, name := range []string{"test", "build"} {
			require(t, step(t, job(t, s, name), "actions/checkout@"), "\n        with:\n          persist-credentials: false")
		}
		// The publish job fetches the tag object with the token.
		if strings.Contains(step(t, job(t, s, "publish"), "actions/checkout@"), "persist-credentials") {
			t.Errorf("publish checkout must keep the default credentials")
		}
	}},
	{"setup_go_cache", func(t reporter, s string) {
		require(t, step(t, job(t, s, "build"), "actions/setup-go@"), "\n          go-version-file: go.mod", "\n          cache: false")
	}},
	{"dispatch_dry_run", func(t reporter, s string) {
		publish := job(t, s, "publish")
		require(t, job(t, s, "build"), "actions/upload-artifact@", "path: dist/*.tar.gz")
		require(t, job(t, s, "publish"), "sha256sum ork_*.tar.gz > SHA256SUMS", "path: dist/SHA256SUMS")
		// No release command may escape the guarded step.
		before, guarded, ok := strings.Cut(publish, "      - name: Publish release\n")
		if !ok || strings.Contains(before, "gh release") {
			t.Errorf("release commands must be in the guarded step")
		}
		require(t, guarded, "        if: github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v')\n")
		if strings.Contains(guarded, "\n      - ") {
			t.Errorf("release step must remain last")
		}
		require(t, guarded, "gh release view", "gh release upload", "--clobber", "gh release create", "--notes-file")
	}},
	{"tag_object", func(t reporter, s string) {
		release := publishStep(t, s)
		fetch := strings.Index(release, "            git fetch --force --no-tags origin \"+refs/tags/$TAG:refs/tags/$TAG\"\n")
		check := strings.Index(release, `git cat-file -t "refs/tags/$TAG"`)
		if fetch < 0 || check < 0 || fetch >= check {
			t.Errorf("restore the tag object before checking its type")
		}
	}},
	{"changelog_section", func(t reporter, s string) {
		release := publishStep(t, s)
		view := strings.Index(release, "gh release view")
		upload := strings.Index(release, `gh release upload "$TAG" dist/*.tar.gz dist/SHA256SUMS --clobber`)
		end := strings.Index(release, "END { exit !found }")
		errLine := strings.Index(release, `echo "::error::CHANGELOG.md has no section headed '## $TAG'"`)
		exit1 := -1
		if errLine >= 0 {
			if i := strings.Index(release[errLine:], "exit 1"); i >= 0 {
				exit1 = errLine + i
			}
		}
		create := strings.Index(release, "gh release create")
		if view < 0 || upload < 0 || end < 0 || errLine < 0 || exit1 < 0 || create < 0 {
			t.Errorf("missing a changelog guard part: view %d upload %d END %d error %d exit %d create %d", view, upload, end, errLine, exit1, create)
			return
		}
		if !(view < upload && upload < end && end < errLine && errLine < exit1 && exit1 < create) {
			t.Errorf("changelog guard out of order: view %d upload %d END %d error %d exit %d create %d", view, upload, end, errLine, exit1, create)
		}
		require(t, release, `if ! awk -v tag="$TAG" '`)
	}},
	{"tag_signature", func(t reporter, s string) {
		release := publishStep(t, s)
		require(t, release, "%(contents:signature)", `${msg%"$sig"}`)
		for _, line := range strings.Split(release, "\n") {
			if strings.Contains(line, "%(contents)") && strings.Contains(line, "release-notes.md") {
				t.Errorf("tag message must not go to the notes unfiltered: %s", strings.TrimSpace(line))
			}
		}
	}},
	{"secrets", func(t reporter, s string) {
		for _, secret := range regexp.MustCompile(`secrets\.[A-Za-z0-9_]+`).FindAllString(s, -1) {
			if secret != "secrets.GITHUB_TOKEN" {
				t.Errorf("unexpected secret %s", secret)
			}
		}
		if strings.Contains(s, "secrets[") || strings.Contains(s, "toJSON(secrets") {
			t.Errorf("indirect secret access")
		}
	}},
}

func TestWorkflow(t *testing.T) {
	s := normalize(readWorkflow(t))
	for _, c := range workflowChecks {
		t.Run(c.name, func(t *testing.T) { c.check(t, s) })
	}
}

func readWorkflow(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func require(t reporter, s string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(s, part) {
			t.Errorf("missing %q", part)
		}
	}
}

func job(t reporter, s, name string) string {
	t.Helper()
	_, tail, ok := strings.Cut(s, "\n  "+name+":\n")
	if !ok {
		t.Fatalf("missing job %s", name)
	}
	if end := regexp.MustCompile(`\n  [a-z]+:\n`).FindStringIndex(tail); end != nil {
		tail = tail[:end[0]]
	}
	return tail
}

// step returns the text of the first step in the job text j whose line is
// "- uses: " followed by uses, up to the next step.
func step(t reporter, j, uses string) string {
	t.Helper()
	_, tail, ok := strings.Cut(j, "\n      - uses: "+uses)
	if !ok {
		t.Fatalf("missing step uses: %s", uses)
	}
	if end := strings.Index(tail, "\n      - "); end >= 0 {
		tail = tail[:end]
	}
	return tail
}

// publishStep returns the text after the "Publish release" step name line.
func publishStep(t reporter, s string) string {
	t.Helper()
	_, release, ok := strings.Cut(job(t, s, "publish"), "      - name: Publish release\n")
	if !ok {
		t.Fatalf("missing Publish release step")
	}
	return release
}
