// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
)

func TestFileCommand(t *testing.T) {
	calls := 0
	a := App{Call: func(_ context.Context, method string, params, result any) error {
		calls++
		p, ok := params.(daemon.RunAction)
		if method != "run.file" || !ok || p.Run != "run-1" || p.Finding != 2 {
			t.Fatalf("request %s %+v", method, params)
		}
		*result.(*daemon.RunReply) = daemon.RunReply{Message: "filed as S130"}
		return nil
	}}
	var out, errout bytes.Buffer
	if code := a.Run(context.Background(), []string{"file", "run-1", "2"}, &out, &errout); code != 0 || out.String() != "filed as S130\n" {
		t.Fatalf("code %d out %q err %q", code, out.String(), errout.String())
	}
	for _, args := range [][]string{{"file"}, {"file", "run-1", "0"}, {"file", "run-1", "no"}, {"file", "run-1", "-2"}} {
		if code := a.Run(context.Background(), args, &out, &errout); code != ExitUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid input reached daemon %d times", calls)
	}
}
