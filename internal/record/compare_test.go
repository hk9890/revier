package record_test

import (
	"testing"
	"time"

	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// The shape of 2026-10-16: a surface saw twenty agents here and the agents
// of two links, and the shutdown's save asked only dt.
func TestASaveThatAskedOneHostExplainsTheOthers(t *testing.T) {
	root := fixture("shutdown")
	s, err := session.Load(root, "2026-10-16T23-54-55")
	if err != nil {
		t.Fatal(err)
	}

	st, diffs := record.Compare(read(t, root, s.At), s)

	if got := len(machineOf(t, st, "").Agents); got != 20 {
		t.Errorf("agents here at the save = %d, want 20", got)
	}
	var lab []revier.SessionID
	for _, d := range diffs {
		if d.Why != record.NotAsked || d.Machine != "lab" || d.Agent == nil || d.In != "state" {
			t.Errorf("difference %+v, want only lab's agents, not asked by the save", d)
			continue
		}
		lab = append(lab, d.Agent.Session)
	}
	if len(lab) != 2 {
		t.Errorf("lab's agents not asked = %v, want l1 and l2", lab)
	}
}

// saveOf is a save at the moment of seen, with its own snapshot line.
func saveOf(seen record.Seen, s session.Session) []record.Line {
	return []record.Line{
		{Time: s.At, Kind: record.KindSessionSaved, By: record.ByDid, Saved: s.ID},
		{Time: s.At.Add(time.Millisecond), Kind: record.KindSnapshot, By: record.BySaw, Saved: s.ID, Seen: &seen},
	}
}

func TestAnAttachmentAndAnUnnamedAgentAreExplained(t *testing.T) {
	seen := desktop()
	seen.Targets = append(seen.Targets, record.Target{Project: "revier", Host: "kitty", Ref: "9", Attached: true})
	seen.Agents = append(seen.Agents, record.Agent{Project: "revier", Host: "kitty", Ref: "3", Panel: "8", Harness: "claude"})
	s := session.Session{ID: "2026-10-16T09-00-00", At: at("2026-10-16", "09:00:00"), Projects: []session.Project{
		{Name: "revier", Targets: []session.Target{{Name: "workspace", Agents: []session.Agent{
			{Harness: "claude", Session: "s1"}, {Harness: "claude"},
		}}}},
		{Name: "taskmgr", Targets: []session.Target{{Name: "browser"}}},
	}}

	_, diffs := record.Compare(saveOf(seen, s), s)

	if len(diffs) != 1 || diffs[0].Why != record.AttachedByHand || diffs[0].Target == nil || diffs[0].Target.Ref != "9" {
		t.Errorf("differences = %+v, want the attachment alone: an agent with no conversation matches one saved with none", diffs)
	}
}

func TestAConversationTheSaveLeftOutIsExplained(t *testing.T) {
	s := session.Session{ID: "2026-10-16T09-00-00", At: at("2026-10-16", "09:00:00"), Projects: []session.Project{
		{Name: "revier", Targets: []session.Target{{Name: "workspace", Agents: []session.Agent{{Harness: "claude"}}}}},
		{Name: "taskmgr", Targets: []session.Target{{Name: "browser"}}},
	}}

	_, diffs := record.Compare(saveOf(desktop(), s), s)

	if len(diffs) != 1 || diffs[0].Why != record.NoConversation || diffs[0].Agent == nil || diffs[0].Agent.Session != "s1" {
		t.Errorf("differences = %+v, want s1 saved with no conversation", diffs)
	}
}

func TestAHostThatDidNotListAtTheSaveExplainsItsTargets(t *testing.T) {
	before := desktop()
	failing := desktop()
	failing.Hosts = []string{"kitty"}
	failing.Unlisted = map[string]string{"gnome": "wctl list: timeout"}
	failing.Instances, failing.Targets = failing.Instances[:1], failing.Targets[:1]
	s := session.Session{ID: "2026-10-16T09-05-00", At: at("2026-10-16", "09:05:00"), Projects: []session.Project{
		{Name: "revier", Targets: []session.Target{{Name: "workspace", Agents: []session.Agent{{Harness: "claude", Session: "s1"}}}}},
	}}
	lines := append([]record.Line{{Time: at("2026-10-16", "09:00:00"), Kind: record.KindSnapshot, By: record.BySaw, Seen: &before}}, saveOf(failing, s)...)

	_, diffs := record.Compare(lines, s)

	if len(diffs) != 1 || diffs[0].Why != record.UnreachableAtSave || diffs[0].Target == nil || diffs[0].Target.Target != "browser" {
		t.Errorf("differences = %+v, want taskmgr's browser, unreachable at the save", diffs)
	}
}

func TestADifferenceNoRuleExplainsIsUnexplained(t *testing.T) {
	s := session.Session{ID: "2026-10-16T09-00-00", At: at("2026-10-16", "09:00:00"), Projects: []session.Project{
		{Name: "revier", Targets: []session.Target{{Name: "workspace", Agents: []session.Agent{{Harness: "claude", Session: "s1"}}}}},
		{Name: "pi", Targets: []session.Target{{Name: "workspace"}}},
	}}

	_, diffs := record.Compare(saveOf(desktop(), s), s)

	want := map[string]string{"taskmgr:browser": "state", "pi:workspace": "session"}
	if len(diffs) != len(want) {
		t.Fatalf("differences = %+v, want %v", diffs, want)
	}
	for _, d := range diffs {
		name := string(d.Target.Project) + ":" + string(d.Target.Target)
		if d.Why != record.Unexplained || want[name] != d.In {
			t.Errorf("difference %+v, want %s in %s, unexplained", d, name, want[name])
		}
	}
}
