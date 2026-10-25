package app_test

import (
	"os"
	"testing"

	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
)

// TestMain keeps every test off the user's own revier files (testenv.Run).
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}

// ledger is a ledger in memory: the state, and every event recorded.
type ledger struct {
	st     state.State
	events []revier.Event
	notes  []record.Line
}

func (l *ledger) State() *state.State { st := l.st; return &st }

func (l *ledger) Update(apply func(*state.State) bool) *state.State {
	apply(&l.st)
	return l.State()
}

func (l *ledger) Record(e revier.Event) { l.events = append(l.events, e) }

func (l *ledger) Note(n record.Line) {
	if n.By == "" {
		n.By = record.ByDid
	}
	l.notes = append(l.notes, n)
}
