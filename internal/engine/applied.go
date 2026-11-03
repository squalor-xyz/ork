// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"sync"
)

type appliedKey struct{}

// WithApplied returns a context that makes Answer and Retry call fn once,
// after the owner's action is durably recorded and before the role or step
// runs again (S237). fn is not called when the action is refused or cannot be
// recorded. It runs with the engine's lock held, so it must not call back into
// the engine except Snapshot.
func WithApplied(ctx context.Context, fn func()) context.Context {
	var once sync.Once
	return context.WithValue(ctx, appliedKey{}, func() { once.Do(fn) })
}

// applied returns the call that WithApplied put in ctx, or a no-op.
func applied(ctx context.Context) func() {
	if fn, ok := ctx.Value(appliedKey{}).(func()); ok {
		return fn
	}
	return func() {}
}
