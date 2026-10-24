// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

// StaleBuild reports a differing or unknown daemon build.
func StaleBuild(own, theirs string) bool {
	if own == "" {
		own = "dev"
	}
	return theirs == "" || theirs == "unknown" || theirs != own
}
