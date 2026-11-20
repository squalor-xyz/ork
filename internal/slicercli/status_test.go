// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"context"
	"github.com/squalor-xyz/ork/internal/clitest"
	"reflect"
	"testing"
)

func TestStatusPreservesSlicerBlockedList(t *testing.T) {
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"status", "--json", "--lean", "--root", root}, Stdout: `{"next":{"id":"S7","title":"First"},"blocked":[{"id":"S8","waiting_on":["S6","S5"]}]}`})
	got, err := c.Status(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Next == nil || got.Next.ID != "S7" || got.Next.Title != "First" {
		t.Fatalf("next: %+v", got.Next)
	}
	if len(got.Blocked) != 1 || got.Blocked[0].ID != "S8" || !reflect.DeepEqual(got.Blocked[0].WaitingOn, []string{"S6", "S5"}) {
		t.Fatalf("blocked: %+v", got.Blocked)
	}
}
