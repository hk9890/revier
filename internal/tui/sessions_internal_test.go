package tui

import (
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/ledger"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// An agent that starts empty with a recorded id never got a prompt, so the
// plan says so rather than show the id as a conversation it resumes; one with
// no id recorded needs no word.
func TestAnUnfiledAgentStartsEmptyWithItsReason(t *testing.T) {
	r := core.RestoreResult{
		RestoreStep: core.RestoreStep{Action: core.RestoreLaunch, Resumes: []core.Resume{
			{Harness: "claude", Session: "never-prompted", Unfiled: true}, {Harness: "claude"},
		}},
		Agents: []core.AgentOutcome{core.AgentEmpty, core.AgentEmpty},
	}
	if _, reason := agentOp(theme.Default(), r, 0); reason != "empty: it never got a prompt" {
		t.Errorf("reason with an id = %q, want it never got a prompt", reason)
	}
	if _, reason := agentOp(theme.Default(), r, 1); reason != "" {
		t.Errorf("reason with no id = %q, want none", reason)
	}
}

// A launch a restore writes reaches the surface on its update loop, before
// the next survey: a press on that target in between must find it coming up,
// not launch a second copy.
func TestARestoresLaunchReachesTheSurfaceAtOnce(t *testing.T) {
	root := t.TempDir()
	if err := (&state.State{}).Save(root); err != nil {
		t.Fatal(err)
	}
	written := make(chan struct{}, 1)
	restoring := toldLedger{Ledger: ledger.File{Root: root}, written: written}
	m := bare(Model{stateRoot: root, core: &core.Core{Ledger: ledger.File{Root: root}}})

	restoring.Update(func(st *state.State) bool {
		st.Launched("work", "home", "", time.Now())
		return true
	})
	msg, ok := waitLedger(written)().(ledgerMsg)
	if !ok || !msg.ok {
		t.Fatalf("msg = %+v, want the write said", msg)
	}
	next, again := m.ledgerWritten(msg)
	if p := next.(Model).pending; !p.Pending(time.Now()) || p.Project != "work" || p.Target != "home" {
		t.Errorf("pending = %+v, want the restore's launch", next.(Model).pending)
	}
	if again == nil {
		t.Error("no wait for the restore's next write")
	}

	ref := revier.TargetRef{Host: "rt", ID: "1"}
	restoring.Update(func(st *state.State) bool {
		st.Landed("work", "home", ref)
		return true
	})
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

// A write while the surface has not yet read the last one's signal does not
// wait for it: the one signal stands for both.
func TestAToldLedgerDoesNotBlockOnAnUnreadWrite(t *testing.T) {
	written := make(chan struct{}, 1)
	l := toldLedger{Ledger: ledger.File{Root: t.TempDir()}, written: written}
	for range 2 {
		l.Update(func(st *state.State) bool {
			st.Launched("work", "home", "", time.Now())
			return true
		})
	}
	if len(written) != 1 {
		t.Errorf("signals = %d, want 1", len(written))
	}
}
