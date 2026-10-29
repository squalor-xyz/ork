// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"github.com/squalor-xyz/ork/internal/clitest"
	"testing"
)

func TestFix1DoctorToolErrorOmitsCommand(t *testing.T) {
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--root", "/fake/project"}, Exit: 3, Stdout: `{"error":{"code":"contract_mismatch","message":"pinned relay v9; installed relay v10","command":"ai relay"}}`})
	cc := checkContract(context.Background(), toolCheck{Present: true, Path: fake.Bin("slicer")}, "/fake/project")
	if cc.OK || cc.Error != "pinned relay v9; installed relay v10 (contract_mismatch)" {
		t.Fatalf("doctor error = %q", cc.Error)
	}
}
