// The herdr probe, against panels built by hand, and the vars Instances
// reports it, against a caller in place of the socket. No herdr and no
// process, so the default layer runs it.
package herdr_test

import (
	"context"
	"testing"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

func TestTheProbeMatchesAPaneHerdrNamedAnAgentIn(t *testing.T) {
	p := &herdr.Probe{}
	for name, c := range map[string]struct {
		panel revier.Panel
		want  bool
	}{
		"an agent":          {revier.Panel{Vars: map[string]string{"herdr_agent": "codex"}}, true},
		"revier's mark":     {revier.Panel{Vars: map[string]string{"revier_target": "agent"}}, false},
		"a codex elsewhere": {revier.Panel{Command: []string{"codex"}}, false},
		"empty":             {revier.Panel{}, false},
	} {
		if got := p.Match(c.panel); got != c.want {
			t.Errorf("%s: Match = %v, want %v", name, got, c.want)
		}
	}
}

// herdr's done is an idle agent nobody has looked at yet, and a state the
// probe does not know is unknown, never a guess.
func TestTheProbeMapsHerdrsStates(t *testing.T) {
	for status, want := range map[string]revier.Status{
		"working": revier.StatusRunning,
		"blocked": revier.StatusAttention,
		"done":    revier.StatusIdle,
		"idle":    revier.StatusIdle,
		"unknown": revier.StatusUnknown,
		"dozing":  revier.StatusUnknown,
		"":        revier.StatusUnknown,
	} {
		got, err := (&herdr.Probe{}).Inspect(context.Background(), revier.Panel{
			Vars: map[string]string{"herdr_agent": "codex", "herdr_agent_status": status},
		})
		if err != nil || got.Harness != "codex" || got.Status != want {
			t.Errorf("%q: Inspect = %+v, %v; want codex %v", status, got, err, want)
		}
	}
}

// A pane herdr named an agent in carries it in its vars, beside its tokens; a
// pane with no agent carries its tokens alone.
func TestAPaneCarriesTheAgentHerdrNamedInIt(t *testing.T) {
	result := `{"snapshot":{"workspaces":[{"workspace_id":"w1","label":"home"}],"panes":[
		{"pane_id":"w1:p1","workspace_id":"w1","tab_id":"w1:t1","agent":"codex","agent_status":"blocked","tokens":{"revier_target":"agent"}},
		{"pane_id":"w1:p2","workspace_id":"w1","tab_id":"w1:t1","agent_status":"unknown","tokens":{"revier_home":"home"}}]}}`
	h := &herdr.Host{}
	h.SetProcRoot(t.TempDir())
	h.SetCaller(func(_ context.Context, method string, _ any) (int, []byte, error) {
		if method != "session.snapshot" {
			return 9, []byte(`{}`), nil
		}
		return 9, []byte(result), nil
	})
	got, err := h.Instances(context.Background())
	if err != nil || len(got) != 1 || len(got[0].Panels) != 2 {
		t.Fatalf("Instances = %+v, %v", got, err)
	}
	agent, shell := got[0].Panels[0].Vars, got[0].Panels[1].Vars
	if agent["herdr_agent"] != "codex" || agent["herdr_agent_status"] != "blocked" || agent["revier_target"] != "agent" {
		t.Errorf("agent pane vars = %v, want codex, blocked and its token", agent)
	}
	if _, ok := shell["herdr_agent"]; ok || shell["revier_home"] != "home" {
		t.Errorf("shell pane vars = %v, want its token alone", shell)
	}
}
