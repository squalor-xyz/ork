// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package engine

import (
	"context"
	"errors"
)

func lockMainFile(ctx context.Context, path string) (func(), error) {
	return nil, errors.New("the main lock is supported on Unix systems only")
}
