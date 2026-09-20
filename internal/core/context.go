package core

import (
	"context"
	"errors"
	"time"
)

// WithoutDeadline is the caller's cancellation without the caller's deadline:
// a cancel is a decision about this operation and reaches it, a bound that was
// spent on the work before it is not.
//
// A walk of several cold starts - a restore - and a phase of a shutdown both
// need it. Each bounds its own steps, and both run after work that already
// spent most of what the caller allowed, so a context that carried the
// caller's deadline would be done before the first step. Dropping the
// cancellation with it, as context.WithoutCancel alone does, makes the
// operation unstoppable instead, which is worse: Ctrl-C would not reach it and
// a hung host would hold it forever.
func WithoutDeadline(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	follow := func() {
		if errors.Is(parent.Err(), context.Canceled) {
			cancel()
		}
	}
	// A caller that cancelled while earlier work ran has already decided, so
	// that is read here rather than waited for.
	follow()
	stop := context.AfterFunc(parent, follow)
	return ctx, func() { stop(); cancel() }
}

// phaseContext is what one phase of a shutdown runs on: a budget of its own,
// and the caller's cancellation (decisions.md D99).
//
// It does not inherit the caller's deadline. The recheck survey waits on
// every link host and can take most of a caller's bound on ninety projects;
// the save then reaches those hosts again. Whatever runs next would be handed
// a context already done - a save that fails, closes that fail on steps that
// would have closed - so each phase counts its own budget from where it
// starts.
func phaseContext(parent context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	base, stop := WithoutDeadline(parent)
	ctx, cancel := context.WithTimeout(base, budget)
	return ctx, func() { cancel(); stop() }
}
