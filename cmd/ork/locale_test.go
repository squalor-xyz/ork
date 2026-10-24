// SPDX-License-Identifier: GPL-3.0-or-later

package main

import "testing"

func TestLocaleSelectsASCIISparkline(t *testing.T) {
	for _, tc := range []struct {
		all, ctype, lang string
		ascii            bool
	}{
		{"", "", "", true}, {"C", "", "en_US.UTF-8", true}, {"", "C", "en_US.UTF-8", true},
		{"", "", "en_US.UTF-8", false}, {"", "en_US.utf8", "C", false}, {"en_US.UTF-8", "C", "C", false},
	} {
		env := map[string]string{"LC_ALL": tc.all, "LC_CTYPE": tc.ctype, "LANG": tc.lang}
		if got := asciiLocale(func(k string) string { return env[k] }); got != tc.ascii {
			t.Fatalf("%+v: %t", tc, got)
		}
	}
}
