// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestS17UsageNamesDaemonRun(t *testing.T) {
	var out, errs bytes.Buffer
	(&App{}).Run(context.Background(), []string{"help"}, &out, &errs)
	if !strings.Contains(out.String(), "ork daemon run") {
		t.Fatal("usage omits ork daemon run")
	}
}
