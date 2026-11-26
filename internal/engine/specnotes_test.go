// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

const laterNote = `{"id":"late","created_at":"2026-11-26T03:00:00Z","by":"Jon","kind":"decision","text":"list each path"}`
const noteFixture = `{"note_records":[{"id":"early","created_at":"2026-11-26T01:00:00Z"},{"id":"equal","created_at":"2026-11-26T02:00:00Z"},` + laterNote + `,{"id":"retracted","created_at":"2026-11-26T04:00:00Z","retracted_at":"2026-11-26T05:00:00Z"}]}`

func TestNotesAfterSpecWriteKeepsOnlyLaterNotes(t *testing.T) {
	wrote, _ := time.Parse(time.RFC3339, "2026-11-26T02:00:00Z")
	got, err := NotesAfterSpecWrite([]byte(noteFixture), wrote)
	want := []FlaggedNote{{ID: "late", CreatedAt: "2026-11-26T03:00:00Z", By: "Jon", Kind: "decision", Text: "list each path"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("notes = %#v, %v; want %#v", got, err, want)
	}
	_, err = NotesAfterSpecWrite([]byte(`{"note_records":[{"id":"bad-date","created_at":"bad"}]}`), wrote)
	if err == nil || !strings.Contains(err.Error(), "bad-date") {
		t.Fatalf("invalid date error = %v", err)
	}
}

func TestSpecNotesAfterWriteSkipsTheLogWithoutNotes(t *testing.T) {
	for _, tc := range []struct {
		name, raw, log string
		want           int
		calls          int
	}{
		{"absent", `{}`, `[]`, 0, 0},
		{"retracted", `{"note_records":[{"id":"gone","retracted_at":"now"}]}`, `[]`, 0, 0},
		{"no write", noteFixture, `[]`, 0, 1},
		{"write", noteFixture, `[{"when":"2026-11-26T02:00:00Z"}]`, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"log"}, Prefix: true, Stdout: tc.log})
			got, err := SpecNotesAfterWrite(context.Background(), Deps{Slicer: slicercli.Client{Bin: fake.Bin("slicer")}}, "/project", "S10", []byte(tc.raw))
			if err != nil || len(got) != tc.want {
				t.Fatalf("notes = %#v, %v", got, err)
			}
			if tc.want == 1 && got[0].ID != "late" {
				t.Fatalf("flagged note = %s; want late", got[0].ID)
			}
			if n := len(fake.CallsTo("slicer")); n != tc.calls {
				t.Fatalf("log calls = %d; want %d", n, tc.calls)
			}
		})
	}
}
