// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"errors"
	"fmt"
)

// refusal is the error of an owner action the engine refused before it changed
// anything: the slice is in the wrong state, a check is open, the note is
// empty. The caller reads the text in the reply; it is not a failure of the
// run, so the daemon does not keep it as one (S141).
type refusal struct{ msg string }

func (r *refusal) Error() string { return r.msg }

// refusef builds a refusal.
func refusef(format string, args ...any) error {
	return &refusal{msg: fmt.Sprintf(format, args...)}
}

// IsRefusal reports whether err is an owner action the engine refused.
func IsRefusal(err error) bool {
	var r *refusal
	return errors.As(err, &r)
}
