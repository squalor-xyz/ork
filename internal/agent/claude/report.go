// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"os"
	"strings"
)

// ReadReport returns the role's report: the report file when it exists and is
// not blank, else the result text of a result that is not an error
// (role.report-io). It returns no report and no source when neither holds
// text. The caller gives each round its own report path; an old file from an
// earlier round would be read as this round's.
func ReadReport(path string, r *Result) (string, ReportSource) {
	if path != "" {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) != "" {
			return string(data), ReportFromFile
		}
	}
	if r != nil && !r.IsError && strings.TrimSpace(r.Text) != "" {
		return r.Text, ReportFromResult
	}
	return "", ""
}
