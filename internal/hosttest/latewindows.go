package hosttest

import (
	"context"

	"github.com/hk9890/revier/pkg/revier"
)

// LateWindows is a window host whose windows appear later than the launch
// that asked for them: Open records the realization in Opened, as Fake.Open
// does, and starts nothing it can list yet.
type LateWindows struct{ *Fake }

// NewLateWindows is a LateWindows named name.
func NewLateWindows(name string) *LateWindows { return &LateWindows{Fake: New(name)} }

func (l *LateWindows) Open(_ context.Context, r revier.Realization) (revier.TargetRef, error) {
	l.mu.Lock()
	l.Opened = append(l.Opened, r)
	l.mu.Unlock()
	return revier.TargetRef{}, nil
}
