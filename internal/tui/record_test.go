package tui_test

import (
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/pkg/revier"
)

// recordingState gives the test a state record of its own under root, and
// returns what is in it when called.
func recordingState(t *testing.T, root string) func() []record.Line {
	t.Helper()
	record.Setup(root, "tui")
	t.Cleanup(func() { record.Setup("", "") })
	return func() []record.Line {
		t.Helper()
		lines, err := record.Read(root, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return lines
	}
}

// The surface records what its survey saw: the first survey whole, and
// after it what changed, such as a workspace closed.
func TestASurveyRecordsWhatChanged(t *testing.T) {
	root := t.TempDir()
	lines := recordingState(t, root)
	m, rt := seenWorld(t, root)
	m = seenSurvey(m)
	if got := lines(); len(got) != 1 || got[0].Kind != record.KindSnapshot || got[0].By != record.BySaw {
		t.Fatalf("lines = %+v, want the first survey as a snapshot", got)
	}

	rt.Remove(revier.TargetRef{Host: "rt", ID: "2"})
	seenSurvey(m)
	got := lines()
	closed := slices.IndexFunc(got, func(l record.Line) bool {
		return l.Kind == record.KindInstanceClosed && l.Host == "rt" && l.Ref == "2"
	})
	if closed < 0 {
		t.Errorf("lines = %+v, want the workspace's instance closed", got)
	}
}

// The state record and what the surface last saw are written from one
// survey, so a replay of the record has the targets and the agents the
// snapshot holds.
func TestTheReplayAgreesWithTheSnapshot(t *testing.T) {
	root := t.TempDir()
	lines := recordingState(t, root)
	m, rt := seenWorld(t, root)
	rt.Add("session:project-00", "kitty", revier.Panel{ID: "5", Kind: revier.PanelTool, Title: "claude"})
	seenSurvey(seenSurvey(m))

	seen, ok := seenOnDisk(t, root)
	if !ok {
		t.Fatal("the surface wrote no snapshot")
	}
	st := record.Replay(lines(), time.Now())
	var replayed, saved []string
	agents := 0
	for _, m := range st.Machines {
		for _, tg := range m.Targets {
			replayed = append(replayed, string(tg.Project)+":"+string(tg.Target))
		}
		agents += len(m.Agents)
	}
	for _, p := range seen.Projects {
		for _, tg := range p.Targets {
			saved = append(saved, string(p.Name)+":"+string(tg.Name))
		}
	}
	slices.Sort(replayed)
	slices.Sort(saved)
	if len(saved) != 2 || !slices.Equal(replayed, saved) {
		t.Errorf("replayed targets = %v, want the snapshot's %v", replayed, saved)
	}
	if agents != seen.Agents() || agents != 1 {
		t.Errorf("replayed agents = %d, want the snapshot's %d", agents, seen.Agents())
	}
}
