// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"context"
	"reflect"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
)

func TestEditReplaceReplacesASection(t *testing.T) {
	text := "- `go test ./...`\n- [HUMAN CHECK] look"
	args := []string{"edit", "S179", "--section", "Check", "--text", text, "--render", "--json", "--lean", "--root", root}
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: args, Stdout: `{"id":"S179"}`})
	w, sink := writer()
	if err := c.EditReplace(context.Background(), w, root, "S179", "Check", text); err != nil {
		t.Fatal(err)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, args) {
		t.Fatalf("calls %+v", calls)
	}
	if len(sink.writes) != 1 || sink.writes[0].Argv[0] != "edit" || sink.writes[0].Root != root {
		t.Errorf("writes %+v", sink.writes)
	}
	if got := EditReplaceArgs("S179", "Check", text); !reflect.DeepEqual(got, []string{"edit", "S179", "--section", "Check", "--text", text}) {
		t.Errorf("preview args %q", got)
	}
}

// Empty text would clear the section: a draft never asks for that.
func TestEditReplaceRefusesEmptyText(t *testing.T) {
	c, fake := client(t)
	w, _ := writer()
	if err := c.EditReplace(context.Background(), w, root, "S179", "Check", " \n"); err == nil {
		t.Error("empty text would clear the section")
	}
	if len(fake.Calls()) != 0 {
		t.Errorf("calls: %+v", fake.Calls())
	}
}
