// SPDX-License-Identifier: GPL-3.0-or-later

package poll

import (
	"reflect"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)

func at(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

// refresh runs one full call for key at second s.
func refresh(t *testing.T, p *Poller, key string, s int) {
	t.Helper()
	if !p.Start(key) {
		t.Fatalf("Start(%q) at %ds refused", key, s)
	}
	p.Finish(key, at(s))
}

func newPoller(t *testing.T) *Poller {
	t.Helper()
	p := New(DefaultSchedule, "a", "b", "c")
	p.SetVisible("a")
	return p
}

func TestEveryKeyIsDueBeforeItsFirstRefresh(t *testing.T) {
	p := newPoller(t)
	if got := p.Due(at(0)); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("Due = %v", got)
	}
}

func TestVisibleEvery5sOthersEvery30s(t *testing.T) {
	p := newPoller(t)
	for _, k := range []string{"a", "b", "c"} {
		refresh(t, p, k, 0)
	}
	cases := []struct {
		s    int
		want []string
	}{
		{0, nil},
		{4, nil},
		{5, []string{"a"}},
		{29, []string{"a"}},
		{30, []string{"a", "b", "c"}},
	}
	for _, c := range cases {
		if got := p.Due(at(c.s)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Due at %ds = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestIntervalRunsFromWhenTheCallReturned(t *testing.T) {
	p := newPoller(t)
	if !p.Start("a") {
		t.Fatal("Start refused")
	}
	p.Finish("a", at(3)) // a slow call: started at 0, back at 3
	if got := p.Due(at(7)); contains(got, "a") {
		t.Errorf("a due 4s after it returned: %v", got)
	}
	if got := p.Due(at(8)); !contains(got, "a") {
		t.Errorf("a not due 5s after it returned: %v", got)
	}
}

func TestACallInFlightIsNeverStartedTwice(t *testing.T) {
	p := newPoller(t)
	if !p.Start("a") {
		t.Fatal("first Start refused")
	}
	if p.Start("a") {
		t.Fatal("second Start accepted while the first is in flight")
	}
	if got := p.Due(at(100)); contains(got, "a") {
		t.Fatalf("a in flight but reported due: %v", got)
	}
	if !p.InFlight("a") || p.Pending() != 1 {
		t.Fatalf("InFlight %v, Pending %d", p.InFlight("a"), p.Pending())
	}
	p.Finish("a", at(100))
	if p.InFlight("a") || p.Pending() != 0 {
		t.Fatalf("after Finish: InFlight %v, Pending %d", p.InFlight("a"), p.Pending())
	}
	if !p.Start("a") {
		t.Fatal("Start refused after Finish")
	}
}

func TestStartRefusesAnUnknownKey(t *testing.T) {
	p := newPoller(t)
	if p.Start("zzz") {
		t.Fatal("Start accepted an unknown key")
	}
	if p.Pending() != 0 {
		t.Fatalf("Pending = %d", p.Pending())
	}
}

func TestSwitchingTheVisibleKeyChangesItsInterval(t *testing.T) {
	p := newPoller(t)
	for _, k := range []string{"a", "b", "c"} {
		refresh(t, p, k, 0)
	}
	p.SetVisible("b")
	if got := p.Due(at(5)); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("Due at 5s = %v, want [b]", got)
	}
}

func contains(keys []string, k string) bool {
	for _, x := range keys {
		if x == k {
			return true
		}
	}
	return false
}
