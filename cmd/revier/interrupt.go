package main

import (
	"context"
	"os"
	"os/signal"
)

// interruptible is ctx without its deadline, cancelled by the first
// interrupt: ctrl+c on a shutdown or a restore. The walk reads the cancel
// between two steps, so the step that runs finishes and the rest is not
// reached. The handler goes with that first interrupt, so a second one ends
// the process at once, as it would with no handler: a step that hangs is not
// waited out.
//
// The deadline is dropped because each step bounds its own, and a cancel that
// comes after a deadline is not seen (core.WithoutDeadline). It is a variable
// so a test can cancel the walk without signalling its own process.
var interruptible = func(ctx context.Context) (context.Context, context.CancelFunc) {
	walk, stop := signal.NotifyContext(context.WithoutCancel(ctx), os.Interrupt)
	context.AfterFunc(walk, stop)
	return walk, stop
}
