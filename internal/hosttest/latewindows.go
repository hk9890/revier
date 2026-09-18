package hosttest

import (
	"context"

	"github.com/hk9890/revier/pkg/revier"
)

// LateWindows is a window host whose windows appear later than the launch
// that asked for them: Open starts nothing it can list yet, and counts the
// launches it was asked for.
type LateWindows struct {
	*Fake
	Opened int
}

// NewLateWindows is a LateWindows named name.
func NewLateWindows(name string) *LateWindows { return &LateWindows{Fake: New(name)} }

func (l *LateWindows) Open(context.Context, revier.Realization) (revier.TargetRef, error) {
	l.Opened++
	return revier.TargetRef{}, nil
}
