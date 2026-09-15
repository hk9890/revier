package tui

import (
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// A launch a restore writes reaches the surface on its update loop, before
// the next survey: a press on that target in between must find it coming up,
// not launch a second copy (decisions.md D21).
func TestARestoresLaunchReachesTheSurfaceAtOnce(t *testing.T) {
	root := t.TempDir()
	if err := (&state.State{}).Save(root); err != nil {
		t.Fatal(err)
	}
	written := make(chan struct{}, 1)
	ledger := stateLedger{root: root, written: written}
	m := Model{stateRoot: root}

	ledger.Launched("work", "home", time.Now())
	msg, ok := waitLedger(written)().(ledgerMsg)
	if !ok || !msg.ok {
		t.Fatalf("msg = %+v, want the write said", msg)
	}
	next, again := m.ledgerWritten(msg)
	if !next.(Model).pending.Pending("work", "home", core.BindWindow) {
		t.Errorf("pending = %+v, want the restore's launch", next.(Model).pending)
	}
	if again == nil {
		t.Error("no wait for the restore's next write")
	}

	ref := revier.TargetRef{Host: "rt", ID: "1"}
	ledger.Landed("work", "home", ref)
	close(written)
	msg = waitLedger(written)().(ledgerMsg)
	next, _ = next.(Model).ledgerWritten(msg)
	if got := next.(Model).bound["work"]["home"]; got != ref {
		t.Errorf("bound = %+v, want the restore's landing", got)
	}
	if msg = waitLedger(written)().(ledgerMsg); msg.ok {
		t.Error("the wait did not end with the restore")
	}
}
