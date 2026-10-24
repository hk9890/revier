//go:build live

// Layer L4: herdr's own agent detection, read by the herdr probe, against a
// real herdr server, headless.
package herdr_test

import (
	"testing"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

// herdr names the agent in a pane by its process, for a harness revier has no
// probe of its own for, and the herdr probe reads it with its state.
func TestTheProbeReadsTheAgentHerdrNamed(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "home", Launch: []string{"bash", "-c", "exec -a codex sleep 600"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var panel revier.Panel
	waitFor(t, h, func(got []revier.Instance) bool {
		if len(got) != 1 || got[0].Ref != ref || len(got[0].Panels) != 1 {
			return false
		}
		panel = got[0].Panels[0]
		return panel.Vars["herdr_agent"] != ""
	})
	p := &herdr.Probe{}
	if !p.Match(panel) {
		t.Fatalf("the probe does not match %+v", panel)
	}
	state, err := p.Inspect(c, panel)
	if err != nil || state.Harness != "codex" {
		t.Errorf("Inspect = %+v, %v; want codex", state, err)
	}
	t.Logf("herdr reports %q for codex, read as %v", panel.Vars["herdr_agent_status"], state.Status)
}
