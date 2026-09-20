// Layer L1: what a long walk runs on is a pure question about contexts.
package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
)

// A bound that ran out before the walk started does not reach it: a restore
// of several cold starts, and a phase of a shutdown, each bound their own
// steps and would otherwise be done before the first one.
func TestWithoutDeadlineDropsABoundThatIsSpent(t *testing.T) {
	spent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	ctx, stop := core.WithoutDeadline(spent)
	defer stop()
	if err := ctx.Err(); err != nil {
		t.Errorf("err = %v, want a context still live", err)
	}
	if _, ok := ctx.Deadline(); ok {
		t.Error("the context carries a deadline, want none")
	}
}

// A cancel is a decision about the walk and reaches it, whether it comes
// before or during: Ctrl-C stops a restore, and a hung host does not hold it
// forever.
func TestWithoutDeadlineKeepsTheCancel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before bool
	}{
		{"cancelled first", true},
		{"cancelled during", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.before {
				cancel()
			}
			ctx, stop := core.WithoutDeadline(parent)
			defer stop()
			if !tc.before {
				cancel()
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("the walk was not cancelled")
			}
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Errorf("err = %v, want context.Canceled", ctx.Err())
			}
		})
	}
}
