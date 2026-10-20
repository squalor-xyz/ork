// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRetryCallsDaemon(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			var out, errs bytes.Buffer
			app := &App{Call: func(_ context.Context, method string, params, result any) error {
				b, err := json.Marshal(params)
				if err != nil || method != "run.retry" || string(b) != `{"run":"r1"}` {
					t.Fatalf("retry request: %s %s (%v)", method, b, err)
				}
				return json.Unmarshal([]byte(`{"id":"r1","state":"awaiting-approval"}`), result)
			}}
			args := []string{"retry", "r1"}
			want := "r1 awaiting-approval\n"
			if format == "json" {
				args = append(args, "--json")
				want = `"state": "awaiting-approval"`
			}
			if code := app.Run(context.Background(), args, &out, &errs); code != ExitOK || !strings.Contains(out.String(), want) {
				t.Fatalf("retry result: code %d, stdout %q, stderr %q", code, &out, &errs)
			}
		})
	}
}

func TestRetryTakesOneRun(t *testing.T) {
	for _, args := range [][]string{{"retry"}, {"retry", "r1", "extra"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			var out, errs bytes.Buffer
			code := (&App{}).Run(context.Background(), args, &out, &errs)
			if code != ExitUsage || !strings.Contains(errs.String(), "retry takes RUN") {
				t.Fatalf("retry usage: code %d, stderr %q", code, &errs)
			}
		})
	}
}

func TestUsageNamesRetry(t *testing.T) {
	var out, errs bytes.Buffer
	(&App{}).Run(context.Background(), []string{"help"}, &out, &errs)
	if !strings.Contains(out.String(), "ork retry RUN") {
		t.Fatalf("help omits retry: %s", &out)
	}
}
