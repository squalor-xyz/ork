// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
)

// The recorded refusal: slicer 1.17.0 exits 2 and prints the error object.
func TestFix1DoctorToolErrorOmitsCommand(t *testing.T) {
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", "/fake/project"}, Exit: 2, File: recordedSlicer("error-contract-mismatch.json")})
	cc := checkContract(context.Background(), toolCheck{Present: true, Path: fake.Bin("slicer")}, "/fake/project")
	want := "this project pins relay v10, but this slicer serves relay v11. Install a slicer that serves relay v10, or, once the tools that consume the contract follow v11, run `slicer config relay_contract 11` (contract_mismatch)"
	if cc.OK || cc.Error != want {
		t.Fatalf("doctor error = %q", cc.Error)
	}
}
