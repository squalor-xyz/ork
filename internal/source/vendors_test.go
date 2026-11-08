// SPDX-License-Identifier: GPL-3.0-or-later

package source

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestVendorOutSaysTheEndFirst(t *testing.T) {
	since := time.Date(2026, 10, 29, 16, 2, 0, 0, time.UTC)
	exit := VendorOut{Vendor: "codex", Reason: "You've hit  your usage\nlimit", Since: since, Until: since.Add(time.Hour)}
	if got, want := exit.SentenceAt(since), "codex is out until 17:02 (since 16:02; You've hit your usage limit)"; got != want {
		t.Errorf("exit mark: %q, want %q", got, want)
	}
	if exit.UntilText() != "17:02" {
		t.Errorf("UntilText %q", exit.UntilText())
	}
	owner := VendorOut{Vendor: "claude", Reason: "credits tomorrow", Since: since}
	if got, want := owner.SentenceAt(since), "claude is out until back (since 16:02; credits tomorrow)"; got != want {
		t.Errorf("owner mark: %q, want %q", got, want)
	}
	if got, want := (VendorOut{Vendor: "codex", Since: since}).SentenceAt(since), "codex is out until back (since 16:02)"; got != want {
		t.Errorf("no reason: %q, want %q", got, want)
	}
	// An owner mark names no end on the wire.
	b, _ := json.Marshal(owner)
	if strings.Contains(string(b), "until") {
		t.Errorf("owner mark JSON names an end: %s", b)
	}
}

func TestVendorOutNamesTheDayWhenTheEndIsNotTheDayItBegan(t *testing.T) {
	since := time.Date(2026, 11, 3, 23, 56, 0, 0, time.UTC)
	owner := VendorOut{Vendor: "codex", Reason: "weekly limit", Since: since, Until: time.Date(2026, 11, 4, 7, 12, 0, 0, time.UTC)}
	if got, want := owner.UntilText(), "4 Nov 07:12"; got != want {
		t.Errorf("UntilText %q, want %q", got, want)
	}
	if got, want := owner.SentenceAt(since), "codex is out until 4 Nov 07:12 (since 23:56; weekly limit)"; got != want {
		t.Errorf("Sentence %q, want %q", got, want)
	}
	// The same day keeps the short form.
	same := VendorOut{Vendor: "codex", Since: since, Until: time.Date(2026, 11, 3, 23, 59, 0, 0, time.UTC)}
	if got := same.UntilText(); got != "23:59" {
		t.Errorf("same-day UntilText %q", got)
	}
	// An end in another zone than the start is compared on the end's own calendar day.
	zone := time.FixedZone("x", 2*3600)
	cross := VendorOut{Vendor: "codex", Since: time.Date(2026, 11, 3, 23, 0, 0, 0, time.UTC), Until: time.Date(2026, 11, 4, 0, 30, 0, 0, zone)}
	if got := cross.UntilText(); got != "00:30" {
		t.Errorf("an end on the same instant-day as the start in the end's zone: %q", got)
	}
	// The wire carries the end of an owner mark that has one.
	b, _ := json.Marshal(owner)
	if !strings.Contains(string(b), `"until":"2026-11-04T07:12:00Z"`) {
		t.Errorf("owner mark JSON lacks its end: %s", b)
	}
}

// The sentence names the day of a start that is not today, and the year of a
// day that is not in the year of its start or of today (S299).
func TestVendorOutSentenceNamesTheDayAndYearWhenTheyAreNotToday(t *testing.T) {
	now := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	yesterday := VendorOut{Vendor: "codex", Reason: "weekly limit", Since: time.Date(2026, 11, 4, 23, 56, 0, 0, time.UTC)}
	if got, want := yesterday.SentenceAt(now), "codex is out until back (since 4 Nov 23:56; weekly limit)"; got != want {
		t.Errorf("a start on another day: %q, want %q", got, want)
	}
	today := VendorOut{Vendor: "codex", Since: time.Date(2026, 11, 5, 8, 30, 0, 0, time.UTC)}
	if got, want := today.SentenceAt(now), "codex is out until back (since 08:30)"; got != want {
		t.Errorf("a start today: %q, want %q", got, want)
	}
	lastYear := VendorOut{Vendor: "codex", Since: time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC)}
	if got, want := lastYear.SentenceAt(now), "codex is out until back (since 31 Dec 2025 23:00)"; got != want {
		t.Errorf("a start in another year: %q, want %q", got, want)
	}
	// An end in the year after its start names the year.
	nye := VendorOut{Vendor: "codex", Since: time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC), Until: time.Date(2027, 1, 1, 7, 0, 0, 0, time.UTC)}
	if got, want := nye.UntilText(), "1 Jan 2027 07:00"; got != want {
		t.Errorf("an end in the next year: %q, want %q", got, want)
	}
	// An end on another day of the year of its start does not.
	if got := (VendorOut{Since: time.Date(2026, 11, 3, 23, 56, 0, 0, time.UTC), Until: time.Date(2026, 11, 4, 7, 12, 0, 0, time.UTC)}).UntilText(); got != "4 Nov 07:12" {
		t.Errorf("an end tomorrow: %q", got)
	}
	// Sentence is SentenceAt on the clock: a start from today has no day.
	nowStart := time.Now()
	if got, want := (VendorOut{Vendor: "codex", Since: nowStart}).Sentence(), "codex is out until back (since "+nowStart.Format("15:04")+")"; got != want {
		t.Errorf("Sentence: %q, want %q", got, want)
	}
	old := nowStart.AddDate(0, 0, -3)
	if got := (VendorOut{Vendor: "codex", Since: old}).Sentence(); !strings.Contains(got, "(since "+old.Format("2 Jan 2006 15:04")+")") && !strings.Contains(got, "(since "+old.Format("2 Jan 15:04")+")") {
		t.Errorf("Sentence of a start three days ago lacks its day: %q", got)
	}
}
