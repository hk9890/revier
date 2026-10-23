package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
)

// interruptible is ctx without its deadline, cancelled by the first
// interrupt: ctrl+c on a shutdown or a restore. The walk reads the cancel
// between two steps, so the step that runs finishes and the rest is not
// reached. The host commands a step runs are in a process group of their
// own, so the interrupt the terminal sends to revier's group does not end
// them too.
//
// A second interrupt ends the process at once, for a step that hangs: it
// runs abandon first, when set, then exits as an interrupt would. abandon is
// what the walk would have done on its cancel had it got to its end, such as
// taking a full shutdown off the record. The handler goes before abandon
// runs, so a third interrupt ends the process even when abandon waits on the
// state's lock.
//
// The deadline is dropped because each step bounds its own, and a cancel that
// comes after a deadline is not seen (core.WithoutDeadline). It is a variable
// so a test can cancel the walk without signalling its own process.
var interruptible = func(ctx context.Context, abandon func()) (context.Context, context.CancelFunc) {
	walk, cancel := context.WithCancel(context.WithoutCancel(ctx))
	interrupts := make(chan os.Signal, 2)
	signal.Notify(interrupts, os.Interrupt)
	done := make(chan struct{})
	interrupt := func() bool {
		select {
		case <-done:
			return false
		case <-interrupts:
			return true
		}
	}
	go func() {
		if !interrupt() {
			return
		}
		cancel()
		if !interrupt() {
			return
		}
		signal.Stop(interrupts)
		if abandon != nil {
			abandon()
		}
		interrupted()
	}()
	var once sync.Once
	return walk, func() {
		once.Do(func() {
			signal.Stop(interrupts)
			close(done)
			cancel()
		})
	}
}

// interrupted ends the process as the second interrupt asks: with the status
// a shell gives a command that an interrupt killed. A variable, so a test
// sees the end without its process ending.
var interrupted = func() { os.Exit(130) }
