// SPDX-License-Identifier: GPL-3.0-or-later

package release

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// This intentionally guards the workflow as text without a YAML dependency.
// Job boundaries and security-sensitive fields have a fixed indentation.
func TestWorkflow(t *testing.T) {
	tests := []struct {
		name  string
		check func(*testing.T, string)
	}{
		{"triggers", func(t *testing.T, s string) {
			require(t, s, "on:\n  push:\n    tags: ['v*']\n  workflow_dispatch:")
		}},
		{"pinned_actions", func(t *testing.T, s string) {
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
		{"targets", func(t *testing.T, s string) {
			build := job(t, s, "build")
			require(t, build, "matrix:\n        include:\n          - os: darwin\n            arch: arm64\n          - os: darwin\n            arch: amd64\n          - os: linux\n            arch: amd64\n    steps:")
		}},
		{"version_and_vcs", func(t *testing.T, s string) {
			require(t, job(t, s, "build"), `go build -trimpath -ldflags "-X main.version=$TAG" -o ork ./cmd/ork`, `TAG="dev-$(git rev-parse --short HEAD)"`, `TAG="$GITHUB_REF_NAME"`)
			if strings.Contains(s, "-buildvcs=false") {
				t.Error("VCS stamping disabled")
			}
			if strings.Count(s, "go-version-file: go.mod") != 2 {
				t.Error("test and build must use go.mod")
			}
		}},
		{"job_order", func(t *testing.T, s string) {
			require(t, job(t, s, "build"), "needs: test")
			require(t, job(t, s, "publish"), "needs: build")
			require(t, job(t, s, "test"), "os: [ubuntu-latest, macos-latest]", "go test -race ./...", "go vet ./...", "gofmt -l .")
		}},
		{"permissions", func(t *testing.T, s string) {
			require(t, s, "\npermissions:\n  contents: read\n")
			if strings.Count(s, "contents: write") != 1 {
				t.Error("want one contents write grant")
			}
			require(t, job(t, s, "publish"), "permissions:\n      contents: write")
			for _, name := range []string{"test", "build"} {
				if strings.Contains(job(t, s, name), "permissions:") {
					t.Errorf("%s must inherit read-only permissions", name)
				}
			}
		}},
		{"dispatch_dry_run", func(t *testing.T, s string) {
			publish := job(t, s, "publish")
			require(t, job(t, s, "build"), "actions/upload-artifact@", "path: dist/*.tar.gz")
			require(t, job(t, s, "publish"), "sha256sum ork_*.tar.gz > SHA256SUMS", "path: dist/SHA256SUMS")
			// No release command may escape the guarded step.
			before, guarded, ok := strings.Cut(publish, "      - name: Publish release\n")
			if !ok || strings.Contains(before, "gh release") {
				t.Error("release commands must be in the guarded step")
			}
			require(t, guarded, "        if: github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v')\n")
			if strings.Contains(guarded, "\n      - ") {
				t.Error("release step must remain last")
			}
			require(t, guarded, "gh release view", "gh release upload", "--clobber", "gh release create", "--notes-file")
		}},
		{"tag_object", func(t *testing.T, s string) {
			_, release, ok := strings.Cut(job(t, s, "publish"), "      - name: Publish release\n")
			if !ok {
				t.Fatal("missing Publish release step")
			}
			fetch := strings.Index(release, "            git fetch --force --no-tags origin \"+refs/tags/$TAG:refs/tags/$TAG\"\n")
			check := strings.Index(release, `git cat-file -t "refs/tags/$TAG"`)
			if fetch < 0 || check < 0 || fetch >= check {
				t.Error("restore the tag object before checking its type")
			}
		}},
		{"secrets", func(t *testing.T, s string) {
			for _, secret := range regexp.MustCompile(`secrets\.[A-Za-z0-9_]+`).FindAllString(s, -1) {
				if secret != "secrets.GITHUB_TOKEN" {
					t.Errorf("unexpected secret %s", secret)
				}
			}
			if strings.Contains(s, "secrets[") || strings.Contains(s, "toJSON(secrets") {
				t.Error("indirect secret access")
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile("../../.github/workflows/release.yml")
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, string(data))
		})
	}
}

func require(t *testing.T, s string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(s, part) {
			t.Errorf("missing %q", part)
		}
	}
}

func job(t *testing.T, s, name string) string {
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
