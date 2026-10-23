package herdr

import (
	"context"

	"github.com/hk9890/revier/pkg/revier"
)

// The panel vars the host reports herdr's own agent detection in: the agent
// herdr names in a pane, and its state. herdr reads each pane's process and
// screen against rules of its own for more harnesses than revier has probes
// for, as kitty reports the user vars a harness's hook sets.
const (
	agentVar  = "herdr_agent"
	statusVar = "herdr_agent_status"
)

// Probe reads the state herdr detected for an agent in one of its panes. It
// knows no harness itself: it takes herdr's word, so it is placed after the
// probes that read a harness directly, a user's included.
type Probe struct{}

func (p *Probe) Name() string { return "herdr" }

// Match recognises a pane herdr named an agent in.
func (p *Probe) Match(panel revier.Panel) bool { return panel.Vars[agentVar] != "" }

// Inspect maps herdr's state onto revier's. herdr's done is an idle agent
// whose turn nobody has looked at yet; revier has no such state, so it is
// idle.
func (p *Probe) Inspect(_ context.Context, panel revier.Panel) (revier.AgentState, error) {
	return revier.AgentState{Harness: panel.Vars[agentVar], Status: statuses[panel.Vars[statusVar]]}, nil
}

// statuses is herdr's agent states as revier's; any other is unknown.
var statuses = map[string]revier.Status{
	"working": revier.StatusRunning,
	"blocked": revier.StatusAttention,
	"done":    revier.StatusIdle,
	"idle":    revier.StatusIdle,
}
