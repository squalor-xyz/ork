// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
)

// ownerWorld is a doctor app whose registry holds the owner settings and whose
// environment has the given ADJ_DECIDED_BY (S314, *proposed*).
func ownerWorld(t *testing.T, owner string, fileAnswers *bool, env string) *App {
	t.Helper()
	P := t.TempDir()
	app, _, regPath := pinWorld(t, nil, registry.Project{Name: "p", Path: P})
	f, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	f.Owner, f.FileAnswers = owner, fileAnswers
	if err := registry.Save(regPath, f); err != nil {
		t.Fatal(err)
	}
	app.Getenv = func(k string) string {
		if k == "ADJ_DECIDED_BY" {
			return env
		}
		return ""
	}
	return app
}

type doctorOwnerOut struct {
	Owner       string `json:"owner"`
	OwnerSource string `json:"owner_source"`
	FileAnswers bool   `json:"file_answers"`
}

func TestDoctorShowsTheOwnerAndWhetherAnswersAreFiled(t *testing.T) {
	off := false
	for _, tc := range []struct {
		name        string
		owner, env  string
		fileAnswers *bool
		want        doctorOwnerOut
		text        string
	}{
		{"setting", "Jon Setting", "", nil, doctorOwnerOut{"Jon Setting", "setting", true},
			"[ok] owner  Jon Setting (setting); spec answers are filed in adj\n"},
		{"environment wins", "Jon Setting", "Jon Env", nil, doctorOwnerOut{"Jon Env", "environment", true},
			"[ok] owner  Jon Env (ADJ_DECIDED_BY); spec answers are filed in adj\n"},
		{"file-answers off", "Jon Setting", "", &off, doctorOwnerOut{"Jon Setting", "setting", false},
			"[ok] owner  Jon Setting (setting); spec answers not filed (file_answers off)\n"},
		{"none", "", "", nil, doctorOwnerOut{"", "", true},
			"[--] owner  not set; spec answers not filed: ork settings set --owner NAME\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := ownerWorld(t, tc.owner, tc.fileAnswers, tc.env)
			code, out, _ := run(t, app, "doctor", "--json")
			var got doctorOwnerOut
			mustJSON(t, out, &got)
			if code != 0 || got != tc.want {
				t.Fatalf("exit %d: got %+v, want %+v\n%s", code, got, tc.want, out)
			}
			app = ownerWorld(t, tc.owner, tc.fileAnswers, tc.env)
			code, out, _ = run(t, app, "doctor")
			if code != 0 || !strings.Contains(out, tc.text) {
				t.Fatalf("exit %d: the text lacks %q:\n%s", code, tc.text, out)
			}
			// The line fits an 80 column terminal, so it does not wrap.
			if n := len(strings.TrimSuffix(tc.text, "\n")); n > 80 {
				t.Fatalf("the owner line is %d columns: %q", n, tc.text)
			}
		})
	}
}
