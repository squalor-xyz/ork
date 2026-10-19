// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import "fmt"

// Classify turns the exit code and the result event (nil when the stream had
// none) into a class and a one-line reason.
//
// Only measured or documented signals count (vendor.ratelimit-detection,
// vendor.unsampled-classes):
//   - permission_denials non-empty: denied, even with exit 0
//     (testdata/vendors/claude/04-allowlist-denies-write.stdout).
//   - exit 0, subtype "success", is_error false: ok.
//   - everything else is unclassified, including the bad-model run (exit 1,
//     api_error_status 404), any 401, 429 or 503, and a rejected
//     rate_limit_event. No auth failure, bad-model class or rate limit has a
//     real sample; the owner marks one and it becomes a fixture.
func Classify(exit int, r *Result) (Class, string) {
	if r != nil && len(r.Denials) > 0 {
		return ClassDenied, fmt.Sprintf("permission denied for %d tool call(s), first %s", len(r.Denials), r.Denials[0].ToolName)
	}
	if r == nil {
		return ClassUnclassified, fmt.Sprintf("exit %d and no result event", exit)
	}
	if exit == 0 && r.Subtype == "success" && !r.IsError {
		return ClassOK, "result event is a success"
	}
	status := "none"
	if r.APIErrorStatus != nil {
		status = fmt.Sprint(*r.APIErrorStatus)
	}
	return ClassUnclassified, fmt.Sprintf("exit %d, subtype %q, is_error %v, api_error_status %s", exit, r.Subtype, r.IsError, status)
}
