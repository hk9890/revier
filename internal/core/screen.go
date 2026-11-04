package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoScreen means nothing here can say what the agent's panel shows: the
// runtime cannot read a panel, or no panel of this machine's runtime shows
// the agent. It is a normal outcome, and the mirror says so.
var ErrNoScreen = errors.New("the agent's panel cannot be read")

// Screen is what the panel that shows the agent shows, with its colours, and
// with its scrollback when asked: what both panes mirror (decisions.md D111,
// D125, D137), and what `revier agent read --screen` prints as text (D116).
// It is the panel here, so a link's agent is read as this machine's terminal
// draws it and nothing is asked of its host. An agent this machine serves to
// a terminal elsewhere is read there, and the refusal names that machine.
func (c *Core) Screen(ctx context.Context, a revier.AgentView, scrollback bool) (string, error) {
	if c.Served != nil && a.Ref.Host == c.Served.Name() {
		return "", fmt.Errorf("agent %s: %w: its panel is on %s; read it there", a.Panel, ErrNoScreen, machineOf(a.Panel))
	}
	r, ok := c.Runtime.(revier.PanelReader)
	if !ok || !c.here(a) {
		return "", fmt.Errorf("agent %s: %w", a.Panel, ErrNoScreen)
	}
	return r.ReadPanel(ctx, a.Ref, a.Panel, scrollback)
}
