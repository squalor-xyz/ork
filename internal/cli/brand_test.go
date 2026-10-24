// SPDX-License-Identifier: GPL-3.0-or-later
package cli

import (
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/brand"
)

func TestDoctorTextStartsWithMark(t *testing.T) {
	for _, locale := range []string{"C", "en_US.UTF-8"} {
		app, _, _ := doctorApp(t, "Contract: relay v7")
		app.Getenv = func(k string) string {
			if k == "LANG" {
				return locale
			}
			return ""
		}
		code, out, _ := run(t, app, "doctor")
		want := strings.Join(brand.Mark(false, locale == "C"), "\n") + "\n[ok] ork  "
		if code != 0 || !strings.HasPrefix(out, want) {
			t.Fatalf("%d: %q", code, out)
		}
	}
}
func TestDoctorJSONHasNoMark(t *testing.T) {
	app, _, _ := doctorApp(t, "Contract: relay v7")
	var previous string
	for _, locale := range []string{"C", "en_US.UTF-8"} {
		app.Getenv = func(k string) string {
			if k == "LANG" {
				return locale
			}
			return ""
		}
		code, out, _ := run(t, app, "doctor", "--json")
		var d doctorOut
		mustJSON(t, out, &d)
		if code != 0 || !strings.HasPrefix(out, "{") || strings.Contains(out, "▄") {
			t.Fatal(out)
		}
		if previous != "" && out != previous {
			t.Fatal("JSON depends on mark mode")
		}
		previous = out
	}
}
